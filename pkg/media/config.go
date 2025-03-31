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
	
	// Speech-to-text configuration
	DefaultVendor string
}