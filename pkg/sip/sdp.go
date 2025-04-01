package sip

import (
	"strings"
	
	"github.com/pion/sdp/v3"
	"siprec-server/pkg/media"
)

// generateSDP creates an SDP description based on options
func (h *Handler) generateSDP(receivedSDP *sdp.SessionDescription, options SDPOptions) *sdp.SessionDescription {
	// Create a new SDP response
	sdpResponse := &sdp.SessionDescription{}
	
	// Copy basic session information from received SDP
	if receivedSDP != nil {
		sdpResponse.Origin = receivedSDP.Origin
		sdpResponse.SessionName = receivedSDP.SessionName
		sdpResponse.SessionInformation = receivedSDP.SessionInformation
		sdpResponse.TimeDescriptions = receivedSDP.TimeDescriptions
	} else {
		// Set default values if no SDP was received
		sdpResponse.Origin = sdp.Origin{
			Username:       "siprec",
			SessionID:      1234567890,
			SessionVersion: 1,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: options.IPAddress,
		}
		sdpResponse.SessionName = sdp.SessionName("SIPREC Media Session")
		sdpResponse.TimeDescriptions = []sdp.TimeDescription{
			{
				Timing: sdp.Timing{
					StartTime: 0,
					StopTime:  0,
				},
			},
		}
	}
	
	// Create audio media description
	audioMedia := sdp.MediaDescription{
		MediaName: sdp.MediaName{
			Media:   "audio",
			Port:    sdp.RangedPort{Value: options.RTPPort},
			Protos:  []string{"RTP", "AVP"},
			Formats: []string{"0", "8", "101"}, // PCMU, PCMA, telephone-event
		},
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: options.IPAddress},
		},
	}
	
	// Add RTP/RTCP attributes
	audioMedia.Attributes = append(audioMedia.Attributes,
		sdp.Attribute{Key: "rtpmap", Value: "0 PCMU/8000"},
		sdp.Attribute{Key: "rtpmap", Value: "8 PCMA/8000"},
		sdp.Attribute{Key: "rtpmap", Value: "101 telephone-event/8000"},
		sdp.Attribute{Key: "fmtp", Value: "101 0-16"},
		sdp.Attribute{Key: "ptime", Value: "20"},
		sdp.Attribute{Key: "sendrecv", Value: ""},
	)
	
	// Add SRTP crypto attributes if enabled
	if options.EnableSRTP && options.SRTPKeyInfo != nil {
		// Format the master key and salt as base64
		// In a real implementation, you'd use base64.StdEncoding.EncodeToString()
		// For this example, we'll just use a placeholder
		keyBase64 := "c2VjcmV0a2V5c2VjcmV0a2V5c2VjcmU="
		
		// Add crypto line
		cryptoLine := "1 " + options.SRTPKeyInfo.Profile + " inline:" + keyBase64
		if options.SRTPKeyInfo.KeyLifetime > 0 {
			cryptoLine += "|2^" + string(options.SRTPKeyInfo.KeyLifetime) + "|"
		}
		
		audioMedia.Attributes = append(audioMedia.Attributes,
			sdp.Attribute{Key: "crypto", Value: cryptoLine},
		)
	}
	
	// Add media to response
	sdpResponse.MediaDescriptions = append(sdpResponse.MediaDescriptions, &audioMedia)
	
	return sdpResponse
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