package sip

import (
	"strings"
	
	"github.com/pion/sdp/v3"
	"siprec-server/pkg/media"
)

// generateSDP creates an SDP description based on options
// This is a legacy function that delegates to generateSDPAdvanced for consistency
func (h *Handler) generateSDP(receivedSDP *sdp.SessionDescription, options SDPOptions) *sdp.SessionDescription {
	return h.generateSDPAdvanced(receivedSDP, options)
}

// generateSDPResponse generates an SDP response for the initial INVITE
// This is a wrapper around the central generateSDP function
func (h *Handler) generateSDPResponse(receivedSDP *sdp.SessionDescription, ipToUse string) *sdp.SessionDescription {
	options := SDPOptions{
		IPAddress:  ipToUse,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		IncludeICE: true,
		RTPPort:    0, // Will use dynamic ports
		EnableSRTP: h.Config.MediaConfig.EnableSRTP,
	}
	
	// Add SRTP information if SRTP is enabled
	if h.Config.MediaConfig.EnableSRTP {
		options.SRTPKeyInfo = &SRTPKeyInfo{
			Profile:      "AES_CM_128_HMAC_SHA1_80",
			KeyLifetime:  2147483647, // 2^31 per RFC 3711
			// Note: actual keys will be populated by the caller
		}
	}
	
	return h.generateSDPAdvanced(receivedSDP, options)
}

// generateSDPResponseWithPort generates an SDP response with a specific port (for re-INVITEs)
// This is a wrapper around the central generateSDP function
func (h *Handler) generateSDPResponseWithPort(receivedSDP *sdp.SessionDescription, ipToUse string, rtpPort int, rtpForwarder *media.RTPForwarder) *sdp.SessionDescription {
	options := SDPOptions{
		IPAddress:  ipToUse,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		IncludeICE: false, // Usually not needed for re-INVITEs
		RTPPort:    rtpPort,
		EnableSRTP: h.Config.MediaConfig.EnableSRTP && rtpForwarder != nil && rtpForwarder.SRTPEnabled,
	}
	
	// Add SRTP information if SRTP is enabled and we have keys
	if options.EnableSRTP && rtpForwarder != nil && 
	   rtpForwarder.SRTPMasterKey != nil && rtpForwarder.SRTPMasterSalt != nil {
		options.SRTPKeyInfo = &SRTPKeyInfo{
			MasterKey:    rtpForwarder.SRTPMasterKey,
			MasterSalt:   rtpForwarder.SRTPMasterSalt,
			Profile:      rtpForwarder.SRTPProfile,
			KeyLifetime:  rtpForwarder.SRTPKeyLifetime,
		}
	}
	
	return h.generateSDPAdvanced(receivedSDP, options)
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}