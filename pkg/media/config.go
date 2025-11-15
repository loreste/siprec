package media

import (
	"time"

	"siprec-server/pkg/audio"
)

// Config holds media-related configuration
type Config struct {
	// RTP configuration
	RTPPortMin  int
	RTPPortMax  int
	RTPTimeout  time.Duration // Timeout for RTP inactivity before closing forwarder (default: 30s)
	RTPBindIP   string        // Specific IP to bind RTP listener to (default: 0.0.0.0 - all interfaces)
	EnableSRTP  bool
	RequireSRTP bool

	// Recording configuration
	RecordingDir      string
	RecordingStorage  RecordingStorage
	EncryptedRecorder *audio.EncryptedRecordingManager
	CombineLegs       bool

	// NAT configuration
	BehindNAT  bool
	InternalIP string
	ExternalIP string

	// SIP NAT port configuration
	SIPInternalPort int
	SIPExternalPort int

	// Speech-to-text configuration
	DefaultVendor string

	// Audio processing configuration
	AudioProcessing AudioProcessingConfig

	// PII detection configuration
	PIIAudioEnabled bool

	// Audio metrics publishing
	AudioMetricsListener AudioMetricsListener
	AudioMetricsInterval time.Duration
}

// AudioProcessingConfig holds audio processing settings
type AudioProcessingConfig struct {
	// General settings
	Enabled bool

	// Voice Activity Detection
	EnableVAD     bool
	VADThreshold  float64
	VADHoldTimeMs int

	// Noise Reduction
	EnableNoiseReduction bool
	NoiseReductionLevel  float64

	// Multi-channel
	ChannelCount int
	MixChannels  bool
}
