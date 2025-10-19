package media

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
)

// ConfigureForwarderFromSDP extracts codec details from the offered SDP and applies them to the RTP forwarder.
func ConfigureForwarderFromSDP(forwarder *RTPForwarder, sdpDesc *sdp.SessionDescription, logger *logrus.Logger) {
	if forwarder == nil || sdpDesc == nil {
		return
	}

	for _, attr := range sdpDesc.Attributes {
		switch attr.Key {
		case "rtcp-mux":
			forwarder.UseRTCPMux = true
		case "rtcp":
			fields := strings.Fields(attr.Value)
			if len(fields) > 0 {
				if port, err := strconv.Atoi(fields[0]); err == nil {
					forwarder.ExpectedRemoteRTCPPort = port
				}
			}
		case "crypto":
			parseSRTPAttributes(forwarder, attr.Value, logger)
		}
	}

	for _, md := range sdpDesc.MediaDescriptions {
		if md.MediaName.Media != "audio" {
			continue
		}

		for _, attr := range md.Attributes {
			switch attr.Key {
			case "rtcp-mux":
				forwarder.UseRTCPMux = true
			case "rtcp":
				fields := strings.Fields(attr.Value)
				if len(fields) > 0 {
					if port, err := strconv.Atoi(fields[0]); err == nil {
						forwarder.ExpectedRemoteRTCPPort = port
					}
				}
			case "crypto":
				parseSRTPAttributes(forwarder, attr.Value, logger)
			}
		}

		for _, format := range md.MediaName.Formats {
			pt, err := strconv.Atoi(strings.TrimSpace(format))
			if err != nil {
				continue
			}

			codecName, sampleRate, channels := codecDetailsFromAttributes(md.Attributes, format)
			if codecName == "" {
				if info, ok := GetCodecInfo(byte(pt)); ok {
					codecName = info.Name
					if sampleRate == 0 {
						sampleRate = info.SampleRate
					}
					if channels == 0 {
						channels = info.Channels
					}
				}
			}

			forwarder.SetCodecInfo(byte(pt), codecName, sampleRate, channels)
			if logger != nil {
				logger.WithFields(logrus.Fields{
					"payload_type": pt,
					"codec":        forwarder.CodecName,
					"sample_rate":  forwarder.SampleRate,
					"channels":     forwarder.Channels,
				}).Info("Configured RTP forwarder codec from SDP offer")
			}
			return
		}
	}
}

func codecDetailsFromAttributes(attrs []sdp.Attribute, format string) (string, int, int) {
	var codecName string
	var sampleRate int
	channels := 1

	for _, attr := range attrs {
		if attr.Key != "rtpmap" {
			continue
		}

		parts := strings.Fields(attr.Value)
		if len(parts) != 2 {
			continue
		}
		if parts[0] != format {
			continue
		}

		rtpmapParts := strings.Split(parts[1], "/")
		if len(rtpmapParts) > 0 {
			codecName = strings.ToUpper(rtpmapParts[0])
		}
		if len(rtpmapParts) > 1 {
			if sr, err := strconv.Atoi(rtpmapParts[1]); err == nil {
				sampleRate = sr
			}
		}
		if len(rtpmapParts) > 2 {
			if ch, err := strconv.Atoi(rtpmapParts[2]); err == nil && ch > 0 {
				channels = ch
			}
		}
		break
	}

	return codecName, sampleRate, channels
}

func parseSRTPAttributes(forwarder *RTPForwarder, value string, logger *logrus.Logger) {
	if forwarder == nil {
		return
	}

	fields := strings.Fields(value)
	if len(fields) < 2 {
		return
	}

	profile := fields[1]
	var inline string
	for _, field := range fields[2:] {
		if strings.HasPrefix(field, "inline:") {
			inline = strings.TrimPrefix(field, "inline:")
			break
		}
	}
	if inline == "" {
		return
	}

	parts := strings.Split(inline, "|")
	rawKey, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		if logger != nil {
			logger.WithError(err).Warn("Failed to decode SRTP inline key material")
		}
		return
	}

	if len(rawKey) < 30 {
		if logger != nil {
			logger.Warn("SRTP keying material shorter than expected 30 bytes")
		}
		return
	}

	masterKey := rawKey[:16]
	masterSalt := rawKey[16:30]

	forwarder.SRTPMasterKey = append([]byte(nil), masterKey...)
	forwarder.SRTPMasterSalt = append([]byte(nil), masterSalt...)
	forwarder.SRTPProfile = profile

	if len(parts) > 1 {
		for _, param := range parts[1:] {
			if strings.HasPrefix(param, "2^") {
				if exp, err := strconv.Atoi(strings.TrimPrefix(param, "2^")); err == nil {
					forwarder.SRTPKeyLifetime = 1 << exp
				}
			}
		}
	}

	if logger != nil {
		logger.WithFields(logrus.Fields{
			"profile": profile,
		}).Debug("Parsed SRTP crypto attribute from SDP")
	}
}
