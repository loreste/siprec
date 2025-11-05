package sip

import (
	"io"
	"strings"
	"testing"

	"siprec-server/pkg/media"

	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
)

func TestGenerateSDPAdvancedRespondsWithRecvOnlyAndPreservesMedia(t *testing.T) {
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:11585
a=ptime:20
m=audio 15682 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:1
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:15683
a=ptime:20
`

	received := &sdp.SessionDescription{}
	if err := received.Unmarshal([]byte(offer)); err != nil {
		t.Fatalf("failed to parse offer: %v", err)
	}

	logger := logrus.New()
	logger.Out = io.Discard

	handler := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{},
		},
	}

	options := &media.SDPOptions{
		IPAddress: "127.0.0.1",
		RTPPort:   4000,
	}

	answer := handler.generateSDPAdvanced(received, options)
	if answer == nil {
		t.Fatal("expected SDP answer, got nil")
	}

	if got, want := len(answer.MediaDescriptions), 2; got != want {
		t.Fatalf("expected %d media descriptions, got %d", want, got)
	}

	for idx, md := range answer.MediaDescriptions {
		if !hasAttribute(md.Attributes, "recvonly") {
			t.Fatalf("media %d missing recvonly attribute", idx)
		}
		if hasAttribute(md.Attributes, "sendonly") || hasAttribute(md.Attributes, "sendrecv") {
			t.Fatalf("media %d should not advertise sendonly/sendrecv", idx)
		}
		if md.MediaName.Formats[0] != "8" {
			t.Fatalf("media %d first codec should be 8 (PCMA), got %s", idx, md.MediaName.Formats[0])
		}
		if !attributeContains(md.Attributes, "rtpmap", "telephone-event") {
			t.Fatalf("media %d missing telephone-event rtpmap", idx)
		}
	}

	if !hasAttribute(answer.Attributes, "recording-session") {
		t.Fatalf("expected session-level recording-session attribute, got %+v", answer.Attributes)
	}
}

func hasAttribute(attrs []sdp.Attribute, key string) bool {
	for _, attr := range attrs {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func attributeContains(attrs []sdp.Attribute, key, needle string) bool {
	for _, attr := range attrs {
		if attr.Key == key && strings.Contains(attr.Value, needle) {
			return true
		}
	}
	return false
}

func TestSDPMarshalOutput(t *testing.T) {
	// Test with the exact INVITE from the user's report
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:11585
a=ptime:20
m=audio 15682 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:1
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:15683
a=ptime:20
`

	received := &sdp.SessionDescription{}
	if err := received.Unmarshal([]byte(offer)); err != nil {
		t.Fatalf("failed to parse offer: %v", err)
	}

	logger := logrus.New()
	logger.Out = io.Discard

	handler := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{},
		},
	}

	options := &media.SDPOptions{
		IPAddress: "127.0.0.1",
		MediaPortPairs: []media.PortPair{
			{RTPPort: 16384, RTCPPort: 16385},
			{RTPPort: 20000, RTCPPort: 20001},
		},
	}

	answer := handler.generateSDPAdvanced(received, options)
	if answer == nil {
		t.Fatal("expected SDP answer, got nil")
	}

	// Marshal and check the output
	marshaled, err := answer.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal SDP: %v", err)
	}

	output := string(marshaled)
	t.Logf("Generated SDP:\n%s", output)

	// Check for issues mentioned by the user

	// 1. Should have 2 m=audio lines
	audioCount := strings.Count(output, "m=audio")
	if audioCount != 2 {
		t.Errorf("expected 2 m=audio lines, got %d", audioCount)
	}

	// 2. Should have a=recvonly, not a=sendrecv
	if strings.Contains(output, "a=sendrecv") {
		t.Errorf("output contains a=sendrecv, should be a=recvonly")
	}
	recvonlyCount := strings.Count(output, "a=recvonly")
	if recvonlyCount != 2 {
		t.Errorf("expected 2 a=recvonly attributes (one per media), got %d", recvonlyCount)
	}

	if !strings.Contains(output, "m=audio 16384") || !strings.Contains(output, "m=audio 20000") {
		t.Fatalf("expected distinct ports for each m=audio line, got:\n%s", output)
	}

	// 3. First codec in each m=audio line should be from the offer (8 or 108)
	// Check that PCMU (0) is not the first codec since it wasn't offered
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "m=audio") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Format: m=audio <port> <proto> <fmt1> <fmt2> ...
				firstCodec := parts[3]
				if firstCodec != "8" && firstCodec != "108" {
					t.Errorf("first codec in m=audio line should be 8 or 108 (from offer), got %s", firstCodec)
				}
				// Check that codec 0 (PCMU) is not in the list since it wasn't offered
				if strings.Contains(line, " 0 ") || strings.HasSuffix(line, " 0") {
					t.Errorf("m=audio line contains codec 0 (PCMU) which was not in the offer: %s", line)
				}
			}
		}
	}

	// 4. Check for a=a:recording-session typo (should be just a=recording-session)
	if strings.Contains(output, "a=a:recording-session") {
		t.Errorf("output contains typo 'a=a:recording-session', should be 'a=recording-session'")
	}
	if !strings.Contains(output, "a=recording-session") {
		t.Errorf("output missing 'a=recording-session' attribute")
	}
}

func TestParseSDPTolerantDropsInvalidAttributes(t *testing.T) {
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=3gOoBTC
a=sendonly
a=rtcp:11585
a=ptime:20
`

	logger := logrus.New()
	logger.Out = io.Discard

	parsed, err := ParseSDPTolerant([]byte(offer), logger)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if len(parsed.MediaDescriptions) != 1 {
		t.Fatalf("expected 1 media description, got %d", len(parsed.MediaDescriptions))
	}

	marshaled, err := parsed.Marshal()
	if err != nil {
		t.Fatalf("failed to marshal sanitized SDP: %v", err)
	}

	if strings.Contains(string(marshaled), "3gOoBTC") {
		t.Fatalf("sanitized SDP should not contain invalid attribute, got %s", string(marshaled))
	}
}

func TestGenerateSDPResponseForForwardersUsesProvidedIP(t *testing.T) {
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:11585
a=ptime:20
m=audio 15682 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:1
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:15683
a=ptime:20
`

	received := &sdp.SessionDescription{}
	if err := received.Unmarshal([]byte(offer)); err != nil {
		t.Fatalf("failed to parse offer: %v", err)
	}

	forwarders := []*media.RTPForwarder{
		{LocalPort: 16384, RTCPPort: 16385},
		{LocalPort: 20000, RTCPPort: 20001},
	}

	logger := logrus.New()
	logger.Out = io.Discard
	handler := &Handler{
		Logger: logger,
		Config: &Config{MediaConfig: &media.Config{}},
	}

	const advertisedIP = "203.0.113.10"
	answer := handler.generateSDPResponseForForwarders(received, advertisedIP, forwarders)
	if answer == nil {
		t.Fatal("expected SDP answer")
	}

	if answer.ConnectionInformation == nil || answer.ConnectionInformation.Address == nil {
		t.Fatalf("missing session connection information: %+v", answer.ConnectionInformation)
	}

	if got := answer.ConnectionInformation.Address.Address; got != advertisedIP {
		t.Fatalf("session-level connection address mismatch: want %s got %s", advertisedIP, got)
	}

	for idx, md := range answer.MediaDescriptions {
		if md.ConnectionInformation == nil || md.ConnectionInformation.Address == nil {
			t.Fatalf("media %d missing connection info", idx)
		}
		if got := md.ConnectionInformation.Address.Address; got != advertisedIP {
			t.Fatalf("media %d connection address mismatch: want %s got %s", idx, advertisedIP, got)
		}
	}
}

func TestGenerateSDPAdvancedHonorsMediaPortPairs(t *testing.T) {
	received := &sdp.SessionDescription{}
	const offer = `v=0
o=ATS99 399418590 399418590 IN IP4 192.168.22.133
s=SipCall
t=0 0
m=audio 11584 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:0
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:11585
a=ptime:20
m=audio 15682 RTP/AVP 8 108
c=IN IP4 192.168.82.21
a=label:1
a=rtpmap:8 PCMA/8000
a=rtpmap:108 telephone-event/8000
a=sendonly
a=rtcp:15683
a=ptime:20
`

	if err := received.Unmarshal([]byte(offer)); err != nil {
		t.Fatalf("failed to parse offer: %v", err)
	}

	h := &Handler{Config: &Config{MediaConfig: &media.Config{}}, Logger: logrus.New()}
	options := &media.SDPOptions{
		IPAddress:      "127.0.0.1",
		MediaPortPairs: []media.PortPair{{RTPPort: 20000, RTCPPort: 20001}, {RTPPort: 22000, RTCPPort: 22001}},
	}

	answer := h.generateSDPAdvanced(received, options)
	if answer == nil {
		t.Fatal("expected SDP answer, got nil")
	}

	if got := answer.MediaDescriptions[0].MediaName.Port.Value; got != 20000 {
		t.Fatalf("expected first media port 20000, got %d", got)
	}
	if got := answer.MediaDescriptions[1].MediaName.Port.Value; got != 22000 {
		t.Fatalf("expected second media port 22000, got %d", got)
	}
}
