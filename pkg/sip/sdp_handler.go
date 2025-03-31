package sip

import (
	"strings"
	
	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
)

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
}

// generateSDP generates an SDP response based on the provided options
// This consolidates the duplicate logic from generateSDPResponse and generateSDPResponseWithPort
func (h *Handler) generateSDP(receivedSDP *sdp.SessionDescription, options SDPOptions) *sdp.SessionDescription {
	mediaStreams := make([]*sdp.MediaDescription, len(receivedSDP.MediaDescriptions))
	
	// Handle NAT traversal for SDP
	connectionAddr := options.IPAddress
	if options.BehindNAT {
		// Use external IP for connection address
		connectionAddr = options.ExternalIP
		
		// Log NAT traversal
		h.Logger.WithFields(logrus.Fields{
			"internal_ip": options.InternalIP,
			"external_ip": options.ExternalIP,
		}).Debug("Using external IP for SDP due to NAT")
	}
	
	for i, media := range receivedSDP.MediaDescriptions {
		// Determine the RTP port to use
		rtpPort := options.RTPPort
		if rtpPort <= 0 {
			// Use a dynamic port if not specified
			rtpPort = 10000 + i
		}
		
		// Create new attributes, handling direction and NAT
		newAttributes := []sdp.Attribute{}
		foundDirectionAttr := false
		
		for _, attr := range media.Attributes {
			// Process direction attributes
			switch attr.Key {
			case "sendonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
				foundDirectionAttr = true
			case "sendrecv":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "inactive":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "recvonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "sendonly"})
				foundDirectionAttr = true
			default:
				// Don't forward local network attributes in NAT scenarios
				if options.BehindNAT && (attr.Key == "candidate" && strings.Contains(attr.Value, options.InternalIP)) {
					continue
				}
				
				newAttributes = append(newAttributes, attr)
			}
		}
		
		// If no direction attribute found, default to recvonly
		if !foundDirectionAttr {
			newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
		}
		
		// Add NAT-specific attributes if needed
		if options.BehindNAT && options.IncludeICE {
			// Add ICE attributes for NAT traversal
			newAttributes = append(newAttributes, sdp.Attribute{Key: "rtcp-mux", Value: ""})
		}
		
		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   media.MediaName.Media,
				Port:    sdp.RangedPort{Value: rtpPort},
				Protos:  media.MediaName.Protos,
				Formats: prioritizeCodecs(media.MediaName.Formats),
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: connectionAddr}, // Use NAT-aware address
			},
			Attributes: appendCodecAttributes(newAttributes, prioritizeCodecs(media.MediaName.Formats)),
		}
		mediaStreams[i] = newMedia
	}
	
	// Create the complete session description
	sessionDesc := &sdp.SessionDescription{
		Origin: sdp.Origin{
			Username:       receivedSDP.Origin.Username,
			SessionID:      receivedSDP.Origin.SessionID,
			SessionVersion: receivedSDP.Origin.SessionVersion,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: connectionAddr, // Use NAT-aware address
		},
		SessionName: receivedSDP.SessionName,
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: connectionAddr}, // Use NAT-aware address
		},
		TimeDescriptions:  receivedSDP.TimeDescriptions,
		MediaDescriptions: mediaStreams,
		Attributes:        []sdp.Attribute{{Key: "a", Value: "recording-session"}},
	}
	
	return sessionDesc
}

// Helper function to prioritize G.711 and G.722 codecs
func prioritizeCodecs(formats []string) []string {
	// G.711 μ-law (PCMU) payload type is 0
	// G.711 a-law (PCMA) payload type is 8
	// G.722 payload type is 9
	
	// Create a map for easy lookup
	formatMap := make(map[string]bool)
	for _, format := range formats {
		formatMap[format] = true
	}
	
	prioritized := []string{}
	
	// Add G.711 codecs first as highest priority
	preferredG711 := []string{"0", "8"} // PCMU, PCMA (G.711 variants)
	for _, codec := range preferredG711 {
		if formatMap[codec] {
			prioritized = append(prioritized, codec)
			delete(formatMap, codec) // Remove to avoid duplicates
		}
	}
	
	// Then add G.722 if available
	if formatMap["9"] { // G.722
		prioritized = append(prioritized, "9")
		delete(formatMap, "9")
	}
	
	// Add any remaining formats
	for _, format := range formats {
		if formatMap[format] {
			prioritized = append(prioritized, format)
		}
	}
	
	return prioritized
}

// Helper function to add codec-specific SDP attributes
func appendCodecAttributes(attributes []sdp.Attribute, formats []string) []sdp.Attribute {
	// Keep existing attributes that are not related to codecs
	filteredAttributes := []sdp.Attribute{}
	for _, attr := range attributes {
		if !strings.HasPrefix(attr.Key, "rtpmap") && !strings.HasPrefix(attr.Key, "fmtp") {
			filteredAttributes = append(filteredAttributes, attr)
		}
	}
	
	// Add attributes for prioritized codecs
	for _, format := range formats {
		switch format {
		case "0": // G.711 PCMU
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "0 PCMU/8000",
			})
		case "8": // G.711 PCMA
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "8 PCMA/8000",
			})
		case "9": // G.722
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "9 G722/8000",
			})
		}
	}
	
	return filteredAttributes
}