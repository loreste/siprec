package sip

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestNATRewriter_RewriteViaHeaders(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config := &NATConfig{
		BehindNAT:      true,
		InternalIP:     "192.168.1.100",
		ExternalIP:     "203.0.113.10",
		InternalPort:   5060,
		ExternalPort:   5060,
		RewriteVia:     true,
		RewriteContact: true,
		ForceRewrite:   false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create NAT rewriter: %v", err)
	}

	// Create a test SIP message
	message := &SIPMessage{
		Method:     "INVITE",
		RequestURI: "sip:test@example.com",
		Version:    "SIP/2.0",
		Headers: map[string][]string{
			"Via":     {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"},
			"Contact": {"<sip:user@192.168.1.100:5060>"},
			"From":    {"<sip:caller@192.168.1.100:5060>;tag=abc"},
			"To":      {"<sip:callee@example.com>"},
			"Call-ID": {"test-call-id"},
			"CSeq":    {"1 INVITE"},
		},
		CallID: "test-call-id",
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	// Test rewriting outgoing message
	err = rewriter.RewriteOutgoingMessage(message)
	if err != nil {
		t.Fatalf("Failed to rewrite outgoing message: %v", err)
	}

	// Check Via header was rewritten
	viaHeaders, exists := message.Headers["Via"]
	if !exists || len(viaHeaders) == 0 {
		t.Fatal("Via header is missing")
	}
	viaValue := viaHeaders[0]
	if !strings.Contains(viaValue, "203.0.113.10") {
		t.Errorf("Via header not rewritten correctly, got: %s", viaValue)
	}
	if strings.Contains(viaValue, "192.168.1.100") {
		t.Errorf("Via header still contains internal IP: %s", viaValue)
	}

	// Check Contact header was rewritten
	contactHeaders, exists := message.Headers["Contact"]
	if !exists || len(contactHeaders) == 0 {
		t.Fatal("Contact header is missing")
	}
	contactValue := contactHeaders[0]
	if !strings.Contains(contactValue, "203.0.113.10") {
		t.Errorf("Contact header not rewritten correctly, got: %s", contactValue)
	}
	if strings.Contains(contactValue, "192.168.1.100") {
		t.Errorf("Contact header still contains internal IP: %s", contactValue)
	}
}

func TestNATRewriter_RewriteSDPContent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config := &NATConfig{
		BehindNAT:    true,
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create NAT rewriter: %v", err)
	}

	// Test SDP content with internal IP
	originalSDP := `v=0
o=user 123456 654321 IN IP4 192.168.1.100
s=Test Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000`

	rewrittenSDP := rewriter.RewriteSDPContent(originalSDP)

	// Check that internal IP was replaced with external IP
	if strings.Contains(rewrittenSDP, "192.168.1.100") {
		t.Errorf("SDP still contains internal IP: %s", rewrittenSDP)
	}
	if !strings.Contains(rewrittenSDP, "203.0.113.10") {
		t.Errorf("SDP does not contain external IP: %s", rewrittenSDP)
	}

	// Check specific lines
	lines := strings.Split(rewrittenSDP, "\n")
	foundOriginLine := false
	foundConnectionLine := false

	for _, line := range lines {
		if strings.HasPrefix(line, "o=") && strings.Contains(line, "203.0.113.10") {
			foundOriginLine = true
		}
		if strings.HasPrefix(line, "c=") && strings.Contains(line, "203.0.113.10") {
			foundConnectionLine = true
		}
	}

	if !foundOriginLine {
		t.Error("Origin line (o=) was not rewritten correctly")
	}
	if !foundConnectionLine {
		t.Error("Connection line (c=) was not rewritten correctly")
	}
}

func TestNATRewriter_PrivateIPDetection(t *testing.T) {
	testCases := []struct {
		ip       string
		expected bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"127.0.0.1", true},
		{"203.0.113.1", false},
		{"8.8.8.8", false},
		{"172.32.0.1", false},
		{"192.169.1.1", false},
		{"invalid-ip", false},
	}

	for _, tc := range testCases {
		result := IsPrivateNetwork(tc.ip)
		if result != tc.expected {
			t.Errorf("IsPrivateNetwork(%s) = %v, expected %v", tc.ip, result, tc.expected)
		}
	}
}

func TestNATRewriter_DisabledNAT(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config := &NATConfig{
		BehindNAT:    false, // NAT disabled
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create NAT rewriter: %v", err)
	}

	// Create a test SIP message
	originalVia := "SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"
	message := &SIPMessage{
		Method:     "INVITE",
		RequestURI: "sip:test@example.com",
		Version:    "SIP/2.0",
		Headers: map[string][]string{
			"Via":     {originalVia},
			"Call-ID": {"test-call-id"},
			"From":    {"<sip:test@test.com>"},
			"To":      {"<sip:test@test.com>"},
			"CSeq":    {"1 INVITE"},
		},
		CallID: "test-call-id",
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	// Test rewriting - should not modify anything when NAT is disabled
	err = rewriter.RewriteOutgoingMessage(message)
	if err != nil {
		t.Fatalf("Failed to rewrite outgoing message: %v", err)
	}

	// Check Via header was NOT rewritten
	viaHeaders, exists := message.Headers["Via"]
	if !exists || len(viaHeaders) == 0 {
		t.Fatal("Via header is missing")
	}
	viaValue := viaHeaders[0]
	if viaValue != originalVia {
		t.Errorf("Via header was modified when NAT is disabled, got: %s, expected: %s", viaValue, originalVia)
	}
}

func TestNATRewriter_MessageBody(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config := &NATConfig{
		BehindNAT:    true,
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create NAT rewriter: %v", err)
	}

	// Test SIP message with SDP body
	sdpBody := `v=0
o=user 123456 654321 IN IP4 192.168.1.100
s=Test Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0`

	message := &SIPMessage{
		Method:     "INVITE",
		RequestURI: "sip:test@example.com",
		Version:    "SIP/2.0",
		Headers: map[string][]string{
			"Via":          {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"},
			"Content-Type": {"application/sdp"},
		},
		Body:   []byte(sdpBody),
		CallID: "test-call-id",
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	// Test rewriting
	err = rewriter.RewriteOutgoingMessage(message)
	if err != nil {
		t.Fatalf("Failed to rewrite outgoing message: %v", err)
	}

	// Check that the SDP body was rewritten
	rewrittenBody := string(message.Body)
	if strings.Contains(rewrittenBody, "192.168.1.100") {
		t.Errorf("Message body still contains internal IP: %s", rewrittenBody)
	}
	if !strings.Contains(rewrittenBody, "203.0.113.10") {
		t.Errorf("Message body does not contain external IP: %s", rewrittenBody)
	}
}
