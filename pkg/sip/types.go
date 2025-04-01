package sip

// SRTPKeyInfo holds SRTP key information for SDP crypto attributes
type SRTPKeyInfo struct {
	// Master key for SRTP (16 bytes for AES-128)
	MasterKey []byte
	
	// Master salt for SRTP (14 bytes recommended)
	MasterSalt []byte
	
	// SRTP crypto profile (e.g., AES_CM_128_HMAC_SHA1_80)
	Profile string
	
	// Key lifetime in packets (optional)
	KeyLifetime int
}

// SDPOptions holds the options for SDP generation
type SDPOptions struct {
	// IP address to use in the SDP
	IPAddress string
	
	// Whether the server is behind NAT
	BehindNAT bool
	
	// Internal IP address (for NAT)
	InternalIP string
	
	// External IP address (for NAT)
	ExternalIP string
	
	// Whether to include ICE candidates
	IncludeICE bool
	
	// Specific port to use, or 0 for dynamic port allocation
	RTPPort int
	
	// Whether to enable SRTP
	EnableSRTP bool
	
	// SRTP key information for SDP crypto attributes
	SRTPKeyInfo *SRTPKeyInfo
}