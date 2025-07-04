package media

// Config holds media-related configuration
type Config struct {
	// RTP configuration
	RTPPortMin int
	RTPPortMax int
	EnableSRTP bool

	// Recording configuration
	RecordingDir string

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
