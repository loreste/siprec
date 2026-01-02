package sip

import (
	"testing"

	"siprec-server/pkg/media"

	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGenerateSDPWithICE(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	h := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{
				BehindNAT: true,
			},
		},
	}

	receivedSDP := &sdp.SessionDescription{
		Origin: sdp.Origin{
			Username:       "alice",
			SessionID:      123456,
			SessionVersion: 1,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: "192.168.1.10",
		},
		SessionName: "Test Session",
		MediaDescriptions: []*sdp.MediaDescription{
			{
				MediaName: sdp.MediaName{
					Media:   "audio",
					Port:    sdp.RangedPort{Value: 10000},
					Protos:  []string{"RTP/AVP"},
					Formats: []string{"0"},
				},
				Attributes: []sdp.Attribute{
					{Key: "sendrecv"},
				},
			},
		},
	}

	options := &media.SDPOptions{
		IPAddress:  "10.0.0.1",
		BehindNAT:  true,
		InternalIP: "10.0.0.1",
		ExternalIP: "203.0.113.5",
		IncludeICE: true,
		RTPPort:    20000,
		RTCPPort:   20001,
		UseRTCPMux: false,
	}

	generatedSDP := h.generateSDPAdvanced(receivedSDP, options)

	// Convert to string for checking
	sdpBytes, err := generatedSDP.Marshal()
	assert.NoError(t, err)
	sdpStr := string(sdpBytes)

	t.Logf("Generated SDP:\n%s", sdpStr)

	// Check for Host Candidate
	// candidate:1 1 UDP 2130706431 10.0.0.1 20000 typ host
	assert.Contains(t, sdpStr, "candidate:1 1 UDP 2130706431 10.0.0.1 20000 typ host")

	// Check for Srflx Candidate
	// candidate:2 1 UDP 1694498815 203.0.113.5 20000 typ srflx raddr 10.0.0.1 rport 20000
	assert.Contains(t, sdpStr, "candidate:2 1 UDP 1694498815 203.0.113.5 20000 typ srflx raddr 10.0.0.1 rport 20000")

	// Check RTCP candidates (since UseRTCPMux is false)
	assert.Contains(t, sdpStr, "candidate:1 2 UDP 2130706430 10.0.0.1 20001 typ host")
	assert.Contains(t, sdpStr, "candidate:2 2 UDP 1694498814 203.0.113.5 20001 typ srflx raddr 10.0.0.1 rport 20001")
}

func TestGenerateSDPWithICEAndRTCPMux(t *testing.T) {
	logger := logrus.New()

	h := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{
				BehindNAT: true,
			},
		},
	}

	receivedSDP := &sdp.SessionDescription{
		MediaDescriptions: []*sdp.MediaDescription{
			{
				MediaName: sdp.MediaName{
					Media:   "audio",
					Port:    sdp.RangedPort{Value: 10000},
					Protos:  []string{"RTP/AVP"},
					Formats: []string{"0"},
				},
			},
		},
	}

	options := &media.SDPOptions{
		IPAddress:  "10.0.0.1",
		BehindNAT:  true,
		InternalIP: "10.0.0.1",
		ExternalIP: "203.0.113.5",
		IncludeICE: true,
		RTPPort:    20000,
		RTCPPort:   20001,
		UseRTCPMux: true,
	}

	generatedSDP := h.generateSDPAdvanced(receivedSDP, options)

	sdpBytes, _ := generatedSDP.Marshal()
	sdpStr := string(sdpBytes)

	// Check for rtcp-mux attribute
	assert.Contains(t, sdpStr, "a=rtcp-mux")

	// Host Candidate for RTP (Component 1)
	assert.Contains(t, sdpStr, "candidate:1 1 UDP 2130706431 10.0.0.1 20000 typ host")

	// Should NOT have RTCP candidates (Component 2) because of rtcp-mux
	assert.NotContains(t, sdpStr, "candidate:1 2 UDP")
	assert.NotContains(t, sdpStr, "candidate:2 2 UDP")
}
