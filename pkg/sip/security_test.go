package sip

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestNotifier() *MetadataNotifier {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return NewMetadataNotifier(logger, nil, time.Second)
}

func parseIP(s string) net.IP {
	return net.ParseIP(s)
}

// --- SSRF callback URL validation ---

func TestCallbackBlocksPrivateIPs(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1/callback",
		"http://10.0.0.1/callback",
		"http://172.16.0.1/callback",
		"http://192.168.1.1/callback",
		"http://[::1]/callback",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"http://0.0.0.0/callback",
	}
	for _, u := range blocked {
		if err := validateCallbackURL(u); err == nil {
			t.Errorf("expected %s to be blocked", u)
		}
	}
}

func TestCallbackBlocksInternalHostnames(t *testing.T) {
	blocked := []string{
		"http://localhost/callback",
		"http://LOCALHOST/callback",
		"http://service.local/callback",
		"http://db.internal/callback",
		"http://metadata.google.internal/computeMetadata/v1/",
	}
	for _, u := range blocked {
		if err := validateCallbackURL(u); err == nil {
			t.Errorf("expected %s to be blocked", u)
		}
	}
}

func TestCallbackBlocksBadSchemes(t *testing.T) {
	blocked := []string{
		"ftp://example.com/callback",
		"file:///etc/passwd",
		"gopher://evil.com/",
		"javascript:alert(1)",
	}
	for _, u := range blocked {
		if err := validateCallbackURL(u); err == nil {
			t.Errorf("expected %s to be blocked", u)
		}
	}
}

func TestCallbackBlocksCredentialsInURL(t *testing.T) {
	if err := validateCallbackURL("http://user:pass@example.com/callback"); err == nil {
		t.Error("expected credentials in URL to be blocked")
	}
}

func TestCallbackBlocksEmptyHostname(t *testing.T) {
	if err := validateCallbackURL("http:///callback"); err == nil {
		t.Error("expected empty hostname to be blocked")
	}
}

func TestCallbackAllowsPublicIPs(t *testing.T) {
	// Use literal public IPs to avoid DNS dependency in tests
	allowed := []string{
		"https://203.0.113.50/notify",
		"http://198.51.100.1:8080/callback",
	}
	for _, u := range allowed {
		if err := validateCallbackURL(u); err != nil {
			t.Errorf("expected %s to be allowed, got: %v", u, err)
		}
	}
}

// --- Endpoint limit ---

func TestMaxEndpointsPerCallEnforced(t *testing.T) {
	n := newTestNotifier()
	callID := "test-call-limit"

	// Register up to the limit using unique public IPs
	for i := 0; i < maxEndpointsPerCall; i++ {
		ip := net.IPv4(203, 0, 113, byte(i+1)).String()
		n.RegisterCallEndpoint(callID, "https://"+ip+"/cb")
	}

	// One more should be silently dropped
	n.RegisterCallEndpoint(callID, "https://203.0.113.200/overflow")

	n.mu.RLock()
	count := len(n.perCall[callID])
	n.mu.RUnlock()

	if count != maxEndpointsPerCall {
		t.Errorf("expected %d endpoints, got %d", maxEndpointsPerCall, count)
	}
}

// --- checkBlockedIP ---

func TestCheckBlockedIPCoversAllRanges(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"192.168.0.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fd00:ec2::254", true},
		{"8.8.8.8", false},
		{"203.0.113.1", false},
	}
	for _, tc := range cases {
		ip := parseIP(tc.ip)
		err := checkBlockedIP(ip)
		if tc.blocked && err == nil {
			t.Errorf("expected %s to be blocked", tc.ip)
		}
		if !tc.blocked && err != nil {
			t.Errorf("expected %s to be allowed, got: %v", tc.ip, err)
		}
	}
}
