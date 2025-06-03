package media

import (
	"net"
	"os"
	"sync"
	"time"

	"siprec-server/pkg/siprec"

	"github.com/sirupsen/logrus"
)

// RTPForwarder handles RTP packet forwarding and recording
type RTPForwarder struct {
	LocalPort        int
	Conn             *net.UDPConn
	StopChan         chan struct{}
	TranscriptChan   chan string
	RecordingFile    *os.File                 // Used to store the recorded media stream
	LastRTPTime      time.Time                // Tracks the last time an RTP packet was received
	Timeout          time.Duration            // Timeout duration for inactive RTP streams
	RecordingSession *siprec.RecordingSession // SIPREC session information
	RecordingPaused  bool                     // Flag to indicate if recording is paused
	Logger           *logrus.Logger
	isCleanedUp      bool       // Flag to track if resources have been cleaned up
	cleanupMutex     sync.Mutex // Mutex to protect cleanup operations

	// SRTP-related fields
	SRTPEnabled     bool   // Whether SRTP is enabled for this forwarder
	SRTPMasterKey   []byte // SRTP master key for crypto attribute in SDP
	SRTPMasterSalt  []byte // SRTP master salt for crypto attribute in SDP
	SRTPKeyLifetime int    // SRTP key lifetime in packets (optional)
	SRTPProfile     string // SRTP crypto profile (e.g., AES_CM_128_HMAC_SHA1_80)

	// Audio processing
	AudioProcessor interface{} // Audio processing manager (will be *audio.ProcessingManager)

	// Cleanup tracking
	MarkedForCleanup bool // Flag indicating if this forwarder has been marked for cleanup
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

	// Whether SRTP is enabled
	EnableSRTP bool

	// SRTP key information
	SRTPKeyInfo *SRTPKeyInfo
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
		SRTPKeyLifetime:  2 ^ 31,                    // Default lifetime from RFC 3711
		AudioProcessor:   nil,                       // Will be initialized in StartRTPForwarding
		isCleanedUp:      false,                     // Not cleaned up initially
		MarkedForCleanup: false,                     // Not marked for cleanup initially
	}, nil
}

// Cleanup performs a thorough cleanup of all resources used by the RTPForwarder
// It ensures resources are only released once to prevent memory leaks
func (f *RTPForwarder) Cleanup() {
	// Use mutex to ensure thread safety
	f.cleanupMutex.Lock()
	defer f.cleanupMutex.Unlock()

	// Check if already cleaned up
	if f.isCleanedUp {
		return
	}

	// Mark as cleaned up to prevent duplicate cleanup
	f.isCleanedUp = true

	// Get the port manager and release the port
	pm := GetPortManager()
	pm.ReleasePort(f.LocalPort)
	f.Logger.WithField("port", f.LocalPort).Debug("Released RTP port during cleanup")

	// Close UDP connection if open
	if f.Conn != nil {
		f.Conn.Close()
		f.Conn = nil
	}

	// Close recording file if open
	if f.RecordingFile != nil {
		f.RecordingFile.Close()
		f.RecordingFile = nil
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
