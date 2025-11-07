package sip

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"siprec-server/pkg/errors"
	"siprec-server/pkg/media"

	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
)

// ValidateSDP performs validation on SDP content
func ValidateSDP(sdpData []byte) error {
	if len(sdpData) == 0 {
		return errors.New("empty SDP content")
	}

	// Parse the SDP to validate it
	parsedSDP, err := ParseSDPTolerant(sdpData, nil)
	if err != nil {
		return errors.Wrap(err, "invalid SDP format")
	}

	// Validate required fields
	if parsedSDP.Origin.Username == "" {
		return errors.New("missing origin username in SDP")
	}

	if parsedSDP.Origin.SessionID == 0 {
		return errors.New("missing session ID in SDP")
	}

	if parsedSDP.SessionName == "" {
		return errors.New("missing session name in SDP")
	}

	// Validate media sections
	if len(parsedSDP.MediaDescriptions) == 0 {
		return errors.New("SDP contains no media sections")
	}

	// Validate at least one audio section exists
	hasAudio := false
	for _, md := range parsedSDP.MediaDescriptions {
		if md.MediaName.Media == "audio" {
			hasAudio = true

			// Validate port structure (in newer pion/sdp versions this is a struct)
			// Access as Port.Value or first make sure it's not nil
			portValue := 0
			if md.MediaName.Port.Value > 0 {
				portValue = md.MediaName.Port.Value
			}

			if portValue == 0 {
				return errors.New("audio section has port 0")
			}

			// Validate formats
			if len(md.MediaName.Formats) == 0 {
				return errors.New("audio section has no codecs specified")
			}

			break
		}
	}

	if !hasAudio {
		return errors.New("SDP contains no audio section")
	}

	return nil
}

// ParseSDPTolerant attempts to parse an SDP payload while tolerating
// non-compliant attributes that frequently appear in vendor implementations.
// Attributes with malformed field names are dropped so the remaining structure
// can be processed using the canonical parser.
func ParseSDPTolerant(sdpData []byte, logger *logrus.Logger) (*sdp.SessionDescription, error) {
	parsed := &sdp.SessionDescription{}
	parseErr := parsed.Unmarshal(sdpData)
	if parseErr == nil {
		sanitized, dropped := sanitizeSDPAttributes(sdpData)
		if dropped == 0 {
			return parsed, nil
		}

		cleanParsed := &sdp.SessionDescription{}
		if cleanErr := cleanParsed.Unmarshal(sanitized); cleanErr == nil {
			if logger != nil {
				logger.WithFields(logrus.Fields{
					"dropped_attributes": dropped,
				}).Warn("Removed non-compliant SDP attributes prior to processing")
			}
			return cleanParsed, nil
		}

		// Fall back to original parse if sanitization fails unexpectedly
		return parsed, nil
	}

	sanitized, dropped := sanitizeSDPAttributes(sdpData)
	if dropped == 0 {
		return nil, parseErr
	}

	parsed = &sdp.SessionDescription{}
	if err := parsed.Unmarshal(sanitized); err != nil {
		return nil, errors.Wrap(parseErr, "failed to parse SDP after sanitization")
	}

	if logger != nil {
		logger.WithError(parseErr).WithFields(logrus.Fields{
			"dropped_attributes": dropped,
		}).Warn("Parsed SDP after removing non-compliant attributes")
	}

	return parsed, nil
}

func sanitizeSDPAttributes(data []byte) ([]byte, int) {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	cleaned := make([]string, 0, len(lines))
	dropped := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "a=") {
			attribute := strings.TrimSpace(trimmed[2:])
			key := attribute
			if idx := strings.Index(attribute, ":"); idx != -1 {
				key = attribute[:idx]
			}
			if !isValidSDPAttributeKey(strings.TrimSpace(key)) {
				dropped++
				continue
			}
		}
		cleaned = append(cleaned, line)
	}

	return []byte(strings.Join(cleaned, "\r\n")), dropped
}

func isValidSDPAttributeKey(key string) bool {
	if key == "" {
		return false
	}

	for i, r := range key {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.') {
			return false
		}
		if i == 0 && !(unicode.IsLetter(r) || r == '_') {
			return false
		}
	}

	return true
}

// generateSDP creates an SDP description based on options
// This is a legacy function that delegates to generateSDPAdvanced for consistency
func (h *Handler) generateSDP(receivedSDP *sdp.SessionDescription, options *media.SDPOptions) *sdp.SessionDescription {
	return h.generateSDPAdvanced(receivedSDP, options)
}

// generateSDPResponse generates an SDP response for the initial INVITE
// This is a wrapper around the central generateSDP function
func (h *Handler) generateSDPResponse(receivedSDP *sdp.SessionDescription, ipToUse string) *sdp.SessionDescription {
	options := &media.SDPOptions{
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
		options.SRTPKeyInfo = &media.SRTPKeyInfo{
			Profile:     "AES_CM_128_HMAC_SHA1_80",
			KeyLifetime: 2147483647, // 2^31 per RFC 3711
			// Note: actual keys will be populated by the caller
		}
	}

	return h.generateSDPAdvanced(receivedSDP, options)
}

// generateSDPResponseWithPort generates an SDP response with a specific port (for re-INVITEs)
// This is a wrapper around the central generateSDP function
func (h *Handler) generateSDPResponseWithPort(receivedSDP *sdp.SessionDescription, ipToUse string, rtpPort int, rtpForwarder *media.RTPForwarder) *sdp.SessionDescription {
	// Determine RTCP configuration
	rtcpPort := 0
	useRTCPMux := false
	if rtpForwarder != nil && rtpForwarder.RTCPPort > 0 {
		rtcpPort = rtpForwarder.RTCPPort
		// Use rtcp-mux if behind NAT for better NAT traversal, otherwise use separate ports
		useRTCPMux = h.Config.MediaConfig.BehindNAT
	}

	options := &media.SDPOptions{
		IPAddress:  ipToUse,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		IncludeICE: false, // Usually not needed for re-INVITEs
		RTPPort:    rtpPort,
		RTCPPort:   rtcpPort,
		UseRTCPMux: useRTCPMux,
		EnableSRTP: h.Config.MediaConfig.EnableSRTP && rtpForwarder != nil && rtpForwarder.SRTPEnabled,
	}

	// Add SRTP information if SRTP is enabled and we have keys
	if options.EnableSRTP && rtpForwarder != nil &&
		rtpForwarder.SRTPMasterKey != nil && rtpForwarder.SRTPMasterSalt != nil {
		options.SRTPKeyInfo = &media.SRTPKeyInfo{
			MasterKey:   rtpForwarder.SRTPMasterKey,
			MasterSalt:  rtpForwarder.SRTPMasterSalt,
			Profile:     rtpForwarder.SRTPProfile,
			KeyLifetime: rtpForwarder.SRTPKeyLifetime,
		}
	}

	options.MediaPortPairs = []media.PortPair{
		{
			RTPPort:  rtpPort,
			RTCPPort: rtcpPort,
		},
	}

	return h.generateSDPAdvanced(receivedSDP, options)
}

// generateSDPResponseForForwarders generates an SDP response that maps each audio
// media description to a dedicated RTP forwarder.
func (h *Handler) generateSDPResponseForForwarders(receivedSDP *sdp.SessionDescription, ipToUse string, forwarders []*media.RTPForwarder) *sdp.SessionDescription {
	options := &media.SDPOptions{
		IPAddress:  ipToUse,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		IncludeICE: true,
	}

	if receivedSDP == nil {
		return h.generateDefaultSDP(options)
	}

	if len(forwarders) > 0 {
		options.RTPPort = forwarders[0].LocalPort
		options.RTCPPort = forwarders[0].RTCPPort
		options.MediaPortPairs = make([]media.PortPair, len(receivedSDP.MediaDescriptions))

		if h.Config.MediaConfig.EnableSRTP && forwarders[0].SRTPEnabled &&
			len(forwarders[0].SRTPMasterKey) > 0 && len(forwarders[0].SRTPMasterSalt) > 0 {
			options.EnableSRTP = true
			options.SRTPKeyInfo = &media.SRTPKeyInfo{
				MasterKey:   forwarders[0].SRTPMasterKey,
				MasterSalt:  forwarders[0].SRTPMasterSalt,
				Profile:     forwarders[0].SRTPProfile,
				KeyLifetime: forwarders[0].SRTPKeyLifetime,
			}
		}
	}

	audioIdx := 0
	for i, md := range receivedSDP.MediaDescriptions {
		if md.MediaName.Media != "audio" {
			continue
		}
		if audioIdx >= len(forwarders) {
			break
		}
		fwd := forwarders[audioIdx]
		options.MediaPortPairs[i] = media.PortPair{
			RTPPort:  fwd.LocalPort,
			RTCPPort: fwd.RTCPPort,
		}
		audioIdx++
	}

	return h.generateSDPAdvanced(receivedSDP, options)
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}

// generateSDPAdvanced generates an SDP response based on the provided options
func (h *Handler) generateSDPAdvanced(receivedSDP *sdp.SessionDescription, options *media.SDPOptions) *sdp.SessionDescription {
	// Handle the case where receivedSDP is nil
	if receivedSDP == nil {
		return h.generateDefaultSDP(options)
	}

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

	// Track ports for duplicate detection (RFC 7865 §7.1 compliance)
	usedPorts := make(map[int]int) // port -> media index

	for idx, m := range receivedSDP.MediaDescriptions {
		// Determine the RTP port to use
		rtpPort := options.RTPPort
		rtcpPort := options.RTCPPort

		if idx < len(options.MediaPortPairs) {
			pair := options.MediaPortPairs[idx]
			if pair.RTPPort > 0 {
				rtpPort = pair.RTPPort
			}
			if pair.RTCPPort > 0 {
				rtcpPort = pair.RTCPPort
			}
		}

		if rtpPort <= 0 {
			// No port specified - caller must allocate and pass in a valid RTP port
			h.Logger.Warn("No RTP port specified in options - port allocation should be handled by caller")
		}
		if rtcpPort <= 0 && rtpPort > 0 {
			// Derive RTCP port using RFC 3550 convention when not explicitly provided
			rtcpPort = rtpPort + 1
		}

		// Validate unique ports for multi-stream sessions (RFC 7865 §7.1)
		if len(receivedSDP.MediaDescriptions) > 1 && rtpPort > 0 {
			if prevIdx, exists := usedPorts[rtpPort]; exists {
				h.Logger.WithFields(logrus.Fields{
					"port":               rtpPort,
					"current_media_idx":  idx,
					"previous_media_idx": prevIdx,
					"media_type":         m.MediaName.Media,
				}).Warn("RFC 7865 violation: Multiple media streams using same RTP port - Recording Server must receive streams on different ports")
			} else {
				usedPorts[rtpPort] = idx
			}
		}

		// Create new attributes, handling direction and NAT
		newAttributes := []sdp.Attribute{}
		foundDirectionAttr := false
		var directionAttr sdp.Attribute

		for _, attr := range m.Attributes {
			// Process direction attributes
			switch attr.Key {
			case "sendonly":
				directionAttr = sdp.Attribute{Key: "recvonly"}
				foundDirectionAttr = true
				continue
			case "sendrecv":
				directionAttr = sdp.Attribute{Key: "recvonly"}
				foundDirectionAttr = true
				continue
			case "inactive":
				directionAttr = sdp.Attribute{Key: "inactive"}
				foundDirectionAttr = true
				continue
			case "recvonly":
				directionAttr = sdp.Attribute{Key: "recvonly"}
				foundDirectionAttr = true
				continue
			case "rtcp":
				// Skip RTCP attributes from offer - we'll add our own based on allocated ports
				continue
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
			directionAttr = sdp.Attribute{Key: "recvonly"}
		}
		newAttributes = append(newAttributes, directionAttr)

		// Add RTCP-related attributes
		if options.UseRTCPMux {
			// RFC 5761 - Use rtcp-mux for NAT traversal (both RTP and RTCP on same port)
			newAttributes = append(newAttributes, sdp.Attribute{Key: "rtcp-mux", Value: ""})
		} else if rtcpPort > 0 && rtcpPort != rtpPort {
			// RFC 3550 - Add explicit RTCP port attribute
			connectionAddr := options.IPAddress
			if options.BehindNAT {
				connectionAddr = options.ExternalIP
			}
			newAttributes = append(newAttributes, sdp.Attribute{
				Key:   "rtcp",
				Value: fmt.Sprintf("%d IN IP4 %s", rtcpPort, connectionAddr),
			})
		}

		// Add NAT-specific attributes if needed
		if options.BehindNAT && options.IncludeICE {
			// Add ICE attributes for NAT traversal
			newAttributes = append(newAttributes, sdp.Attribute{Key: "ice-ufrag", Value: "randomufrag"})
			newAttributes = append(newAttributes, sdp.Attribute{Key: "ice-pwd", Value: "randomicepwd"})
		}

		// Add SRTP crypto attributes if SRTP is enabled
		if options.EnableSRTP && options.SRTPKeyInfo != nil {
			// Add 'RTP/SAVP' transport if not already present
			protoUpdated := false
			for j, proto := range m.MediaName.Protos {
				if proto == "RTP/AVP" {
					m.MediaName.Protos[j] = "RTP/SAVP"
					protoUpdated = true
					break
				}
			}

			if !protoUpdated && len(m.MediaName.Protos) > 0 {
				// If we couldn't update an existing proto, just set the first one
				m.MediaName.Protos[0] = "RTP/SAVP"
			}

			// Add crypto attribute (RFC 4568 format: tag AES_CM_128_HMAC_SHA1_80 inline:Base64Key|Base64Salt|lifetime|MKI
			// Base64 encode the key material
			base64KeySalt := base64.StdEncoding.EncodeToString(append(options.SRTPKeyInfo.MasterKey, options.SRTPKeyInfo.MasterSalt...))
			cryptoLine := fmt.Sprintf("1 %s inline:%s", options.SRTPKeyInfo.Profile, base64KeySalt)

			// Add lifetime if specified
			if options.SRTPKeyInfo.KeyLifetime > 0 {
				cryptoLine += fmt.Sprintf("|2^%d", options.SRTPKeyInfo.KeyLifetime)
			}

			newAttributes = append(newAttributes, sdp.Attribute{Key: "crypto", Value: cryptoLine})

			// Log the crypto addition
			h.Logger.WithFields(logrus.Fields{
				"profile": options.SRTPKeyInfo.Profile,
				"media":   m.MediaName.Media,
			}).Debug("Added SRTP crypto attribute to SDP")
		}

		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   m.MediaName.Media,
				Port:    sdp.RangedPort{Value: rtpPort},
				Protos:  m.MediaName.Protos,
				Formats: prioritizeCodecs(m.MediaName.Formats),
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: connectionAddr}, // Use NAT-aware address
			},
			Attributes: newAttributes,
		}
		mediaStreams[idx] = newMedia
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
		Attributes:        []sdp.Attribute{{Key: "recording-session"}},
	}

	// Log the generated media descriptions for debugging
	h.Logger.WithFields(logrus.Fields{
		"input_media_count":  len(receivedSDP.MediaDescriptions),
		"output_media_count": len(sessionDesc.MediaDescriptions),
	}).Debug("Generated SDP response")

	return sessionDesc
}

// generateDefaultSDP creates a default SDP response when no receivedSDP is provided
func (h *Handler) generateDefaultSDP(options *media.SDPOptions) *sdp.SessionDescription {
	// Determine the connection address accounting for NAT
	connectionAddr := options.IPAddress
	if options.BehindNAT {
		connectionAddr = options.ExternalIP

		h.Logger.WithFields(logrus.Fields{
			"internal_ip": options.InternalIP,
			"external_ip": options.ExternalIP,
		}).Debug("Using external IP for default SDP due to NAT")
	}

	// Create a new SDP description
	sdpResponse := &sdp.SessionDescription{
		Origin: sdp.Origin{
			Username:       "siprec",
			SessionID:      uint64(time.Now().Unix()),
			SessionVersion: 1,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: connectionAddr,
		},
		SessionName: sdp.SessionName("SIPREC Media Session"),
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: connectionAddr},
		},
		TimeDescriptions: []sdp.TimeDescription{
			{
				Timing: sdp.Timing{
					StartTime: 0,
					StopTime:  0,
				},
			},
		},
		Attributes: []sdp.Attribute{
			{Key: "recording-session"},
		},
	}

	// Determine the RTP port to use
	rtpPort := options.RTPPort
	if rtpPort <= 0 {
		// Port allocation should be handled by caller - setting to 0 to indicate allocation needed
		rtpPort = 0
		h.Logger.Warn("No RTP port specified in options for default SDP - port allocation should be handled by caller")
	}

	// Create audio media description
	formats := []string{"0", "8", "9"} // PCMU, PCMA, G722
	audioMedia := &sdp.MediaDescription{
		MediaName: sdp.MediaName{
			Media:   "audio",
			Port:    sdp.RangedPort{Value: rtpPort},
			Protos:  []string{"RTP/AVP"},
			Formats: formats,
		},
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: connectionAddr},
		},
	}

	// Add attributes
	attributes := []sdp.Attribute{
		{Key: "rtpmap", Value: "0 PCMU/8000"},
		{Key: "rtpmap", Value: "8 PCMA/8000"},
		{Key: "rtpmap", Value: "9 G722/8000"},
		{Key: "ptime", Value: "20"},
		{Key: "recvonly", Value: ""},
	}

	// Add SRTP crypto attributes if enabled
	if options.EnableSRTP && options.SRTPKeyInfo != nil {
		// Change transport from RTP/AVP to RTP/SAVP for SRTP
		audioMedia.MediaName.Protos = []string{"RTP/SAVP"}

		// Format the master key and salt for the crypto line
		var cryptoLine string

		// If we have real key material, use it
		if options.SRTPKeyInfo.MasterKey != nil && options.SRTPKeyInfo.MasterSalt != nil {
			// Base64 encode the key material
			base64KeySalt := base64.StdEncoding.EncodeToString(
				append(options.SRTPKeyInfo.MasterKey, options.SRTPKeyInfo.MasterSalt...))

			cryptoLine = fmt.Sprintf("1 %s inline:%s",
				options.SRTPKeyInfo.Profile, base64KeySalt)

			// Add lifetime if specified
			if options.SRTPKeyInfo.KeyLifetime > 0 {
				cryptoLine += fmt.Sprintf("|2^%d", options.SRTPKeyInfo.KeyLifetime)
			}
		} else {
			// Fallback to a placeholder (for testing)
			cryptoLine = "1 AES_CM_128_HMAC_SHA1_80 inline:c2VjcmV0a2V5c2VjcmV0a2V5c2VjcmU="
		}

		attributes = append(attributes, sdp.Attribute{Key: "crypto", Value: cryptoLine})

		h.Logger.WithFields(logrus.Fields{
			"profile": options.SRTPKeyInfo.Profile,
		}).Debug("Added SRTP crypto attribute to default SDP")
	}

	audioMedia.Attributes = attributes
	sdpResponse.MediaDescriptions = []*sdp.MediaDescription{audioMedia}

	return sdpResponse
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
		default:
			// Check if it's a dynamic payload type (96-127) typically used for OPUS
			if pt, err := strconv.Atoi(format); err == nil && pt >= 96 && pt <= 127 {
				// Add OPUS rtpmap
				filteredAttributes = append(filteredAttributes, sdp.Attribute{
					Key:   "rtpmap",
					Value: fmt.Sprintf("%s opus/48000/2", format),
				})
				// Add OPUS fmtp parameters
				filteredAttributes = append(filteredAttributes, sdp.Attribute{
					Key:   "fmtp",
					Value: fmt.Sprintf("%s maxplaybackrate=48000;sprop-maxcapturerate=48000;maxaveragebitrate=64000;stereo=0;useinbandfec=1;usedtx=0", format),
				})
			}
		}
	}

	return filteredAttributes
}
