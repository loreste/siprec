package sip

import (
	"siprec-server/pkg/media"
)

// NewNATConfigFromMediaConfig creates a NAT configuration from media configuration
func NewNATConfigFromMediaConfig(mediaConfig *media.Config) *NATConfig {
	if mediaConfig == nil {
		return nil
	}

	// Only create NAT config if the media config indicates we're behind NAT
	if !mediaConfig.BehindNAT {
		return nil
	}

	natConfig := &NATConfig{
		BehindNAT:            mediaConfig.BehindNAT,
		InternalIP:           mediaConfig.InternalIP,
		ExternalIP:           mediaConfig.ExternalIP,
		InternalPort:         mediaConfig.SIPInternalPort,
		ExternalPort:         mediaConfig.SIPExternalPort,
		RewriteVia:           true,
		RewriteContact:       true,
		RewriteRecordRoute:   true,
		AutoDetectExternalIP: mediaConfig.ExternalIP == "", // Auto-detect if no external IP provided
		STUNServer:           "stun.l.google.com:19302",
		ForceRewrite:         false,
	}

	// If no external IP is provided, enable auto-detection
	if natConfig.ExternalIP == "" {
		natConfig.AutoDetectExternalIP = true
	}

	// Set default ports if not specified
	if natConfig.InternalPort == 0 {
		natConfig.InternalPort = 5060 // Default SIP port
	}
	if natConfig.ExternalPort == 0 {
		natConfig.ExternalPort = natConfig.InternalPort // Use same port if not specified
	}

	return natConfig
}

// UpdateNATConfigWithExternalIP updates the NAT config with a detected external IP
func UpdateNATConfigWithExternalIP(config *NATConfig, externalIP string) {
	if config != nil && externalIP != "" {
		config.ExternalIP = externalIP
	}
}
