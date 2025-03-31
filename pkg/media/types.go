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
}

// NewRTPForwarder creates a new RTP forwarder
func NewRTPForwarder(port int, timeout time.Duration, recordingSession *siprec.RecordingSession, logger *logrus.Logger) *RTPForwarder {
	return &RTPForwarder{
		LocalPort:        port,
		StopChan:         make(chan struct{}),
		TranscriptChan:   make(chan string, 10), // Buffer up to 10 transcriptions
		Timeout:          timeout,
		RecordingSession: recordingSession,
		Logger:           logger,
	}
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
	PortMutex          sync.Mutex
	UsedRTPPorts       = make(map[int]bool) // Keeps track of used RTP ports
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