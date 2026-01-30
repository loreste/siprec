package media

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/audio"
	"siprec-server/pkg/siprec"

	"github.com/sirupsen/logrus"
)

// RTPForwarder handles RTP packet forwarding and recording
type RTPForwarder struct {
	LocalPort        int // RTP port (even)
	RTCPPort         int // RTCP port (RTP + 1, odd)
	Conn             *net.UDPConn
	RTCPConn         *net.UDPConn
	StopChan         chan struct{}
	CallUUID         string
	TranscriptChan   chan string
	RecordingFile    *os.File                 // Used to store the recorded media stream
	LastRTPTime      time.Time                // Tracks the last time an RTP packet was received
	lastRTPMutex     sync.RWMutex             // Mutex to protect LastRTPTime access
	Timeout          time.Duration            // Timeout duration for inactive RTP streams
	RecordingSession *siprec.RecordingSession // SIPREC session information
	RecordingPaused  bool                     // Flag to indicate if recording is paused
	Logger           *logrus.Logger
	isCleanedUp      bool       // Flag to track if resources have been cleaned up
	CleanupMutex     sync.Mutex // Mutex to protect cleanup operations (exported for external access)
	stopOnce         sync.Once  // Ensures StopChan is closed only once
	RecordingPath    string
	Storage          RecordingStorage

	// Codec / audio format information
	CodecPayloadType byte
	CodecName        string
	SampleRate       int
	Channels         int

	// WAV writer handles PCM containerization
	WAVWriter *WAVWriter
	// Encrypted recording support
	EncryptedRecorder  *audio.EncryptedRecordingManager
	EncryptedSessionID string

	// Pause/Resume state
	TranscriptionPaused bool            // Flag to indicate if transcription is paused
	PausedAt            *time.Time      // When the session was paused
	pauseMutex          sync.RWMutex    // Mutex for pause state
	recordingWriter     *PausableWriter // Pausable writer for recording
	transcriptionReader *PausableReader // Pausable reader for transcription

	// Mute state - controls which audio streams are silenced
	InboundMuted  bool       // If true, caller audio (inbound) is muted/silenced
	OutboundMuted bool       // If true, agent/TTS audio (outbound) is muted/silenced
	MutedAt       *time.Time // When the mute was applied
	muteMutex     sync.RWMutex

	// SRTP-related fields
	SRTPEnabled     bool   // Whether SRTP is enabled for this forwarder
	SRTPMasterKey   []byte // SRTP master key for crypto attribute in SDP
	SRTPMasterSalt  []byte // SRTP master salt for crypto attribute in SDP
	SRTPKeyLifetime int    // SRTP key lifetime in packets (optional)
	SRTPProfile     string // SRTP crypto profile (e.g., AES_CM_128_HMAC_SHA1_80)

	// Audio processing
	AudioProcessor interface{} // Audio processing manager (will be *audio.ProcessingManager)

	// Audio format encoding
	AudioEncoder     *audio.AudioEncoder // Encoder for converting WAV to other formats
	TargetFormat     string              // Target recording format (wav, mp3, opus, etc.)

	// PII audio tracking
	PIIAudioMarker *PIIAudioMarker // Tracks PII detection events for audio redaction

	// Remote party addressing
	RemoteRTPAddr          *net.UDPAddr
	RemoteRTCPAddr         *net.UDPAddr
	ExpectedRemoteRTCPPort int
	UseRTCPMux             bool

	// Cleanup tracking
	MarkedForCleanup bool // Flag indicating if this forwarder has been marked for cleanup

	// RTP/RTCP statistics
	LocalSSRC  uint32
	RemoteSSRC uint32
	RTPStats   *rtpStreamStats

	rtcpStopChan chan struct{}
	remoteMutex  sync.Mutex
}

// SDPOptions defines options for SDP generation
type SDPOptions struct {
	// IP Address to use in SDP
	IPAddress string

	// Whether the server is behind NAT
	BehindNAT bool

	// Internal IP address (for ICE candidates)
	InternalIP string

	// External IP address (for ICE candidates)
	ExternalIP string

	// Whether to include ICE candidates
	IncludeICE bool

	// RTP port to use
	RTPPort int

	// RTCP port to use (RFC 3550 - typically RTP + 1)
	RTCPPort int

	// Whether to use rtcp-mux (RFC 5761 - both RTP and RTCP on same port)
	UseRTCPMux bool

	// Whether SRTP is enabled
	EnableSRTP bool

	// SRTP key information
	SRTPKeyInfo *SRTPKeyInfo

	// MediaPortPairs allows specifying per-media RTP/RTCP port assignments for multi-stream sessions.
	// Required for RFC 7865 §7.1 compliance in SIPREC scenarios with multiple media streams.
	// Each entry corresponds to a media description in the SDP offer, in the same order.
	// If provided, the slice length should match the number of media descriptions.
	// When empty/nil for multi-stream sessions, all streams will use RTPPort (RFC violation warning logged).
	// Example for 2-stream SIPREC:
	//   MediaPortPairs: []PortPair{
	//       {RTPPort: 16384, RTCPPort: 16385},  // Stream 1
	//       {RTPPort: 20000, RTCPPort: 20001},  // Stream 2
	//   }
	MediaPortPairs []PortPair
}

// SRTPKeyInfo holds SRTP key information
type SRTPKeyInfo struct {
	// SRTP master key
	MasterKey []byte

	// SRTP master salt
	MasterSalt []byte

	// SRTP profile (e.g., "AES_CM_128_HMAC_SHA1_80")
	Profile string

	// SRTP key lifetime
	KeyLifetime int
}

// InitPortManager initializes the port manager with the configured port range
func InitPortManager(minPort, maxPort int) {
	portManagerOnce.Do(func() {
		portManager = NewPortManager(minPort, maxPort)
	})
}

// GetPortManager returns the global port manager instance, initializing it if necessary
func GetPortManager() *PortManager {
	portManagerOnce.Do(func() {
		// Initialize with default values if not already initialized
		portManager = NewPortManager(10000, 20000)
	})
	return portManager
}

func generateRandomSSRC() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to time-based value if crypto source unavailable
		return uint32(time.Now().UnixNano())
	}
	return binary.BigEndian.Uint32(buf[:])
}

// NewRTPForwarder creates a new RTP forwarder using RFC 3550 compliant RTP/RTCP port pairs
func NewRTPForwarder(timeout time.Duration, recordingSession *siprec.RecordingSession, logger *logrus.Logger, piiAudioEnabled bool, encryptedRecorder *audio.EncryptedRecordingManager) (*RTPForwarder, error) {
	// Get an RTP/RTCP port pair from the port manager (RFC 3550 compliant)
	pm := GetPortManager()
	portPair, err := pm.AllocatePortPair()
	if err != nil {
		return nil, err
	}

	// Initialize PII audio marker if enabled
	var piiAudioMarker *PIIAudioMarker
	if piiAudioEnabled && recordingSession != nil {
		piiAudioMarker = NewPIIAudioMarker(logger, recordingSession.ID, true)
	}

	return &RTPForwarder{
		LocalPort:         portPair.RTPPort,  // Even port for RTP
		RTCPPort:          portPair.RTCPPort, // Odd port for RTCP
		StopChan:          make(chan struct{}),
		TranscriptChan:    make(chan string, 10), // Buffer up to 10 transcriptions
		Timeout:           timeout,
		RecordingSession:  recordingSession,
		Logger:            logger,
		SRTPEnabled:       false,
		SRTPProfile:       "AES_CM_128_HMAC_SHA1_80", // Default profile
		SRTPKeyLifetime:   1 << 31,                   // Default lifetime from RFC 3711
		AudioProcessor:    nil,                       // Will be initialized in StartRTPForwarding
		PIIAudioMarker:    piiAudioMarker,            // PII audio tracking
		EncryptedRecorder: encryptedRecorder,
		isCleanedUp:       false, // Not cleaned up initially
		MarkedForCleanup:  false, // Not marked for cleanup initially
		LocalSSRC:         generateRandomSSRC(),
		RTPStats:          newRTPStreamStats(),
		rtcpStopChan:      make(chan struct{}, 1),
	}, nil
}

// SetCodecInfo configures payload format information used for recording.
func (f *RTPForwarder) SetCodecInfo(payloadType byte, codecName string, sampleRate, channels int) {
	f.CodecPayloadType = payloadType
	f.CodecName = strings.ToUpper(codecName)
	f.SampleRate = sampleRate
	f.Channels = channels
	if f.SampleRate <= 0 {
		f.SampleRate = 8000
	}
	if f.Channels <= 0 {
		f.Channels = 1
	}
	if f.RTPStats != nil && sampleRate > 0 {
		f.RTPStats.SetClockRate(sampleRate)
	}
}

// Stop safely stops the RTP forwarder by closing the stop channel
func (f *RTPForwarder) Stop() {
	f.stopOnce.Do(func() {
		f.Logger.WithField("call_uuid", f.CallUUID).Info("RTPForwarder.Stop() called - closing StopChan and connections")
		close(f.StopChan)

		// Also close connections immediately to unblock any pending reads
		// This ensures goroutines exit quickly instead of waiting for timeouts
		f.CleanupMutex.Lock()
		if f.Conn != nil {
			f.Logger.WithField("call_uuid", f.CallUUID).Info("Closing UDP connection in Stop()")
			err := f.Conn.Close()
			if err != nil {
				f.Logger.WithError(err).Warn("Error closing UDP connection")
			}
		}
		if f.RTCPConn != nil {
			f.RTCPConn.Close()
		}
		f.CleanupMutex.Unlock()
		f.Logger.WithField("call_uuid", f.CallUUID).Info("RTPForwarder.Stop() completed")
	})
}

// Pause pauses recording and/or transcription
func (f *RTPForwarder) Pause(pauseRecording, pauseTranscription bool) {
	f.pauseMutex.Lock()
	defer f.pauseMutex.Unlock()

	if pauseRecording {
		f.RecordingPaused = true
		// Pause the recording writer if available
		if f.recordingWriter != nil {
			f.recordingWriter.Pause()
		}
	}

	if pauseTranscription {
		f.TranscriptionPaused = true
		// Pause the transcription reader if available
		if f.transcriptionReader != nil {
			f.transcriptionReader.Pause()
		}
	}

	// Set pause timestamp if either is paused
	if f.RecordingPaused || f.TranscriptionPaused {
		now := time.Now()
		f.PausedAt = &now

		if f.Logger != nil {
			f.Logger.WithFields(logrus.Fields{
				"recording_paused":     f.RecordingPaused,
				"transcription_paused": f.TranscriptionPaused,
				"session_id":           f.RecordingSession.ID,
			}).Info("RTP forwarder paused")
		}
	}
}

// Resume resumes recording and transcription
func (f *RTPForwarder) Resume() {
	f.pauseMutex.Lock()
	defer f.pauseMutex.Unlock()

	wasRecordingPaused := f.RecordingPaused
	wasTranscriptionPaused := f.TranscriptionPaused

	f.RecordingPaused = false
	f.TranscriptionPaused = false
	f.PausedAt = nil

	// Resume the recording writer if it was paused
	if wasRecordingPaused && f.recordingWriter != nil {
		f.recordingWriter.Resume()
	}

	// Resume the transcription reader if it was paused
	if wasTranscriptionPaused && f.transcriptionReader != nil {
		f.transcriptionReader.Resume()
	}

	if f.Logger != nil && (wasRecordingPaused || wasTranscriptionPaused) {
		f.Logger.WithFields(logrus.Fields{
			"was_recording_paused":     wasRecordingPaused,
			"was_transcription_paused": wasTranscriptionPaused,
			"session_id":               f.RecordingSession.ID,
		}).Info("RTP forwarder resumed")
	}
}

// IsPaused returns whether recording or transcription is paused
func (f *RTPForwarder) IsPaused() bool {
	f.pauseMutex.RLock()
	defer f.pauseMutex.RUnlock()
	return f.RecordingPaused || f.TranscriptionPaused
}

// GetPauseStatus returns the current pause status
func (f *RTPForwarder) GetPauseStatus() (recordingPaused, transcriptionPaused bool, pausedAt *time.Time) {
	f.pauseMutex.RLock()
	defer f.pauseMutex.RUnlock()
	return f.RecordingPaused, f.TranscriptionPaused, f.PausedAt
}

// Mute mutes the specified audio streams (inbound = caller, outbound = agent/TTS)
// When muted, the audio stream is replaced with silence instead of being stopped
func (f *RTPForwarder) Mute(muteInbound, muteOutbound bool) {
	f.muteMutex.Lock()
	defer f.muteMutex.Unlock()

	wasInboundMuted := f.InboundMuted
	wasOutboundMuted := f.OutboundMuted

	if muteInbound {
		f.InboundMuted = true
	}

	if muteOutbound {
		f.OutboundMuted = true
	}

	// Set mute timestamp if either is now muted and wasn't before
	if (f.InboundMuted || f.OutboundMuted) && f.MutedAt == nil {
		now := time.Now()
		f.MutedAt = &now
	}

	if f.Logger != nil && (f.InboundMuted != wasInboundMuted || f.OutboundMuted != wasOutboundMuted) {
		f.Logger.WithFields(logrus.Fields{
			"inbound_muted":  f.InboundMuted,
			"outbound_muted": f.OutboundMuted,
			"session_id":     f.RecordingSession.ID,
		}).Info("RTP forwarder muted")
	}
}

// Unmute unmutes all audio streams or specific streams based on parameters
func (f *RTPForwarder) Unmute(unmuteInbound, unmuteOutbound bool) {
	f.muteMutex.Lock()
	defer f.muteMutex.Unlock()

	wasInboundMuted := f.InboundMuted
	wasOutboundMuted := f.OutboundMuted

	if unmuteInbound {
		f.InboundMuted = false
	}

	if unmuteOutbound {
		f.OutboundMuted = false
	}

	// Clear mute timestamp if both are unmuted
	if !f.InboundMuted && !f.OutboundMuted {
		f.MutedAt = nil
	}

	if f.Logger != nil && (f.InboundMuted != wasInboundMuted || f.OutboundMuted != wasOutboundMuted) {
		f.Logger.WithFields(logrus.Fields{
			"inbound_muted":      f.InboundMuted,
			"outbound_muted":     f.OutboundMuted,
			"was_inbound_muted":  wasInboundMuted,
			"was_outbound_muted": wasOutboundMuted,
			"session_id":         f.RecordingSession.ID,
		}).Info("RTP forwarder unmuted")
	}
}

// UnmuteAll unmutes all audio streams
func (f *RTPForwarder) UnmuteAll() {
	f.Unmute(true, true)
}

// IsMuted returns whether any audio stream is muted
func (f *RTPForwarder) IsMuted() bool {
	f.muteMutex.RLock()
	defer f.muteMutex.RUnlock()
	return f.InboundMuted || f.OutboundMuted
}

// GetMuteStatus returns the current mute status
func (f *RTPForwarder) GetMuteStatus() (inboundMuted, outboundMuted bool, mutedAt *time.Time) {
	f.muteMutex.RLock()
	defer f.muteMutex.RUnlock()
	return f.InboundMuted, f.OutboundMuted, f.MutedAt
}

// IsInboundMuted returns whether inbound (caller) audio is muted
func (f *RTPForwarder) IsInboundMuted() bool {
	f.muteMutex.RLock()
	defer f.muteMutex.RUnlock()
	return f.InboundMuted
}

// IsOutboundMuted returns whether outbound (agent/TTS) audio is muted
func (f *RTPForwarder) IsOutboundMuted() bool {
	f.muteMutex.RLock()
	defer f.muteMutex.RUnlock()
	return f.OutboundMuted
}

// Cleanup performs a thorough cleanup of all resources used by the RTPForwarder
// It ensures resources are only released once to prevent memory leaks
func (f *RTPForwarder) Cleanup() {
	// Use mutex to ensure thread safety
	f.CleanupMutex.Lock()
	defer f.CleanupMutex.Unlock()

	// Check if already cleaned up
	if f.isCleanedUp {
		return
	}

	// Mark as cleaned up to prevent duplicate cleanup
	f.isCleanedUp = true

	// Stop RTCP sender loop before closing sockets
	if f.rtcpStopChan != nil {
		select {
		case f.rtcpStopChan <- struct{}{}:
		default:
		}
	}

	// Send RTCP BYE before tearing down sockets
	if f.RemoteRTCPAddr != nil {
		sendRTCPBye(f)
	}

	// Get the port manager and release the port(s)
	pm := GetPortManager()
	if f.RTCPPort > 0 {
		// Release port pair (RFC 3550 compliant mode)
		portPair := &PortPair{RTPPort: f.LocalPort, RTCPPort: f.RTCPPort}
		pm.ReleasePortPair(portPair)
		f.Logger.WithFields(logrus.Fields{
			"rtp_port":  f.LocalPort,
			"rtcp_port": f.RTCPPort,
		}).Debug("Released RTP/RTCP port pair during cleanup")
	} else {
		// Release single port (legacy mode)
		pm.ReleasePort(f.LocalPort)
		f.Logger.WithField("port", f.LocalPort).Debug("Released RTP port during cleanup")
	}

	// Close UDP connection if open
	if f.Conn != nil {
		f.Conn.Close()
		f.Conn = nil
	}

	if f.RTCPConn != nil {
		f.RTCPConn.Close()
		f.RTCPConn = nil
	}

	// Close recording file if open
	if f.RecordingFile != nil {
		if f.WAVWriter != nil {
			if err := f.WAVWriter.Finalize(); err != nil && f.Logger != nil {
				f.Logger.WithError(err).Warn("Failed to finalize WAV header during cleanup")
			}
			f.WAVWriter = nil
		}
		f.RecordingFile.Close()
		f.RecordingFile = nil
	}

	// Convert recording to target format if encoder is configured and format is not WAV
	if f.AudioEncoder != nil && f.RecordingPath != "" && f.TargetFormat != "" && f.TargetFormat != "wav" {
		outputPath := strings.TrimSuffix(f.RecordingPath, ".wav") + "." + f.TargetFormat
		if err := f.AudioEncoder.EncodeFile(f.RecordingPath, outputPath); err != nil {
			f.Logger.WithError(err).WithFields(logrus.Fields{
				"input":  f.RecordingPath,
				"output": outputPath,
				"format": f.TargetFormat,
			}).Warn("Failed to convert recording to target format, keeping WAV")
		} else {
			// Remove original WAV file after successful conversion
			if err := os.Remove(f.RecordingPath); err != nil {
				f.Logger.WithError(err).WithField("path", f.RecordingPath).Warn("Failed to remove original WAV after conversion")
			}
			// Update recording path to point to converted file
			f.RecordingPath = outputPath
			f.Logger.WithFields(logrus.Fields{
				"format": f.TargetFormat,
				"path":   outputPath,
			}).Info("Recording converted to target format")
		}
	}

	if f.EncryptedRecorder != nil && f.EncryptedSessionID != "" {
		if err := f.EncryptedRecorder.StopRecording(f.EncryptedSessionID); err != nil && f.Logger != nil {
			f.Logger.WithError(err).WithFields(logrus.Fields{
				"session_id": f.EncryptedSessionID,
				"call_uuid":  f.CallUUID,
			}).Warn("Failed to stop encrypted recording session during cleanup")
		}
		f.EncryptedSessionID = ""
	}

	// Upload recording to external storage if configured
	if f.Storage != nil && f.RecordingPath != "" {
		if err := f.Storage.Upload(f.CallUUID, f.RecordingSession, f.RecordingPath); err != nil {
			f.Logger.WithError(err).WithField("path", f.RecordingPath).Warn("Failed to upload recording to storage backend")
		} else if !f.Storage.KeepLocalCopy() {
			RemoveLocalRecording(f.Logger, f.RecordingPath)
		}
	}

	// Close audio processor if it implements a Close method
	if f.AudioProcessor != nil {
		if closer, ok := f.AudioProcessor.(interface{ Close() error }); ok {
			closer.Close()
		}
		f.AudioProcessor = nil
	}

	// Clean up SRTP resources
	f.SRTPMasterKey = nil
	f.SRTPMasterSalt = nil

	f.Logger.Debug("RTP forwarder resources have been cleaned up")
}

// Define multiple buffer pools for different sizes to optimize memory usage
var (
	// Small buffer pool for control packets (up to 128 bytes)
	SmallBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 128)
		},
	}

	// Medium buffer pool for typical RTP packets (up to 1024 bytes)
	MediumBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 1024)
		},
	}

	// Large buffer pool for larger RTP packets with many CSRC identifiers, etc. (up to 1500 bytes)
	LargeBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 1500)
		},
	}

	// Very large buffer pool for processing chunks (up to 4096 bytes)
	VeryLargeBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 4096)
		},
	}

	// For backward compatibility - defaults to medium size buffer
	BufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 1024)
		},
	}
)

// GetPacketBuffer returns an appropriately sized buffer for the given size
// This helps optimize memory usage by using the right pool
func GetPacketBuffer(size int) ([]byte, func(interface{})) {
	var buffer interface{}
	var pool *sync.Pool

	switch {
	case size <= 128:
		buffer = SmallBufferPool.Get()
		pool = &SmallBufferPool
	case size <= 1024:
		buffer = MediumBufferPool.Get()
		pool = &MediumBufferPool
	case size <= 1500:
		buffer = LargeBufferPool.Get()
		pool = &LargeBufferPool
	default:
		buffer = VeryLargeBufferPool.Get()
		pool = &VeryLargeBufferPool
	}

	// Return the buffer and a function to return it to the pool
	return buffer.([]byte), func(b interface{}) {
		pool.Put(b)
	}
}

// Global port manager instance
var (
	portManager     *PortManager
	portManagerOnce sync.Once
)

// GetPortManagerStats returns statistics about port usage
func GetPortManagerStats() (available int, total int) {
	pm := GetPortManager()
	if pm == nil {
		return 0, 0
	}
	stats := pm.GetStats()
	return stats.AvailablePorts, stats.TotalPorts
}
