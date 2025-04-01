package media

import (
	"net"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/siprec"
)

// RTPForwarder handles RTP packet forwarding and recording
type RTPForwarder struct {
	LocalPort        int
	Conn             *net.UDPConn
	StopChan         chan struct{}
	TranscriptChan   chan string
	RecordingFile    *os.File                // Used to store the recorded media stream
	LastRTPTime      time.Time               // Tracks the last time an RTP packet was received
	Timeout          time.Duration           // Timeout duration for inactive RTP streams
	RecordingSession *siprec.RecordingSession // SIPREC session information
	RecordingPaused  bool                     // Flag to indicate if recording is paused
	Logger           *logrus.Logger
	
	// SRTP-related fields
	SRTPEnabled    bool       // Whether SRTP is enabled for this forwarder
	SRTPMasterKey  []byte     // SRTP master key for crypto attribute in SDP
	SRTPMasterSalt []byte     // SRTP master salt for crypto attribute in SDP
	SRTPKeyLifetime int       // SRTP key lifetime in packets (optional)
	SRTPProfile    string     // SRTP crypto profile (e.g., AES_CM_128_HMAC_SHA1_80)
	
	// Audio processing
	AudioProcessor  interface{} // Audio processing manager (will be *audio.ProcessingManager)
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

// NewRTPForwarder creates a new RTP forwarder using a port allocated from the port manager
func NewRTPForwarder(timeout time.Duration, recordingSession *siprec.RecordingSession, logger *logrus.Logger) (*RTPForwarder, error) {
	// Get a port from the port manager
	pm := GetPortManager()
	port, err := pm.AllocatePort()
	if err != nil {
		return nil, err
	}
	
	return &RTPForwarder{
		LocalPort:        port,
		StopChan:         make(chan struct{}),
		TranscriptChan:   make(chan string, 10), // Buffer up to 10 transcriptions
		Timeout:          timeout,
		RecordingSession: recordingSession,
		Logger:           logger,
		SRTPEnabled:      false,
		SRTPProfile:      "AES_CM_128_HMAC_SHA1_80", // Default profile
		SRTPKeyLifetime:  2^31,                      // Default lifetime from RFC 3711
		AudioProcessor:   nil,                        // Will be initialized in StartRTPForwarding
	}, nil
}

// Buffer pool for RTP packets to reduce GC pressure
var BufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1500)
	},
}

// Global metrics - in a production environment, these would be proper metrics
var (
	RTPPacketsReceived uint64
	RTPBytesReceived   uint64
	
	// Global port manager instance
	portManager *PortManager
	
	// Init function to ensure portManager is initialized
	portManagerOnce sync.Once
)

// CodecInfo holds information about an audio codec
type CodecInfo struct {
	Name       string
	PayloadType byte
	SampleRate int
	Channels   int
}

// Known codecs
var (
	CodecPCMU = CodecInfo{Name: "PCMU", PayloadType: 0, SampleRate: 8000, Channels: 1}
	CodecPCMA = CodecInfo{Name: "PCMA", PayloadType: 8, SampleRate: 8000, Channels: 1}
	CodecG722 = CodecInfo{Name: "G722", PayloadType: 9, SampleRate: 16000, Channels: 1}
)