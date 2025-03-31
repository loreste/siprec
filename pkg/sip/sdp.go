package sip

import (
	"strings"
	
	"github.com/pion/sdp/v3"
)

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
	}
	
	return h.generateSDP(receivedSDP, options)
}

// generateSDPResponseWithPort generates an SDP response with a specific port (for re-INVITEs)
// This is a wrapper around the central generateSDP function
func (h *Handler) generateSDPResponseWithPort(receivedSDP *sdp.SessionDescription, ipToUse string, rtpPort int) *sdp.SessionDescription {
	options := SDPOptions{
		IPAddress:  ipToUse,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		IncludeICE: false, // Usually not needed for re-INVITEs
		RTPPort:    rtpPort,
	}
	
	return h.generateSDP(receivedSDP, options)
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}