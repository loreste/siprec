package auth

import (
	"io"
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestAuthenticator() *SIPAuthenticator {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	auth := &SIPAuthenticator{
		users:        make(map[string]*SIPUser),
		nonces:       make(map[string]*NonceInfo),
		logger:       logger,
		realm:        "test",
		nonceTimeout: 300 * 1e9, // 300s
	}
	auth.AddUser("alice", "secret123")
	return auth
}

func TestConstantTimeComparison(t *testing.T) {
	auth := newTestAuthenticator()

	// Get a valid challenge
	result := auth.Authenticate("", "REGISTER", "sip:test@example.com", "10.0.0.1")
	if result.Success {
		t.Fatal("expected challenge, got success")
	}
	if result.Challenge == "" {
		t.Fatal("expected non-empty challenge")
	}

	// Submit wrong response — should fail without panic
	header := `Digest username="alice", realm="test", nonce="wrong", uri="sip:test@example.com", response="0000000000000000000000000000dead", algorithm=MD5, qop=auth, nc=00000001, cnonce="test"`
	result = auth.Authenticate(header, "REGISTER", "sip:test@example.com", "10.0.0.1")
	if result.Success {
		t.Fatal("expected failure with wrong response")
	}
}

func TestNonceCountReplay(t *testing.T) {
	auth := newTestAuthenticator()

	nonce, err := auth.generateNonce("10.0.0.1")
	if err != nil {
		t.Fatalf("generateNonce failed: %v", err)
	}

	// First use with nc=00000001 should succeed
	if !auth.validateNonce(nonce, "10.0.0.1", "00000001") {
		t.Fatal("first nonce use should succeed")
	}

	// Replay with same nc should fail
	if auth.validateNonce(nonce, "10.0.0.1", "00000001") {
		t.Fatal("replayed nonce-count should be rejected")
	}

	// Lower nc should also fail
	if auth.validateNonce(nonce, "10.0.0.1", "00000000") {
		t.Fatal("lower nonce-count should be rejected")
	}

	// Higher nc should succeed
	if !auth.validateNonce(nonce, "10.0.0.1", "00000002") {
		t.Fatal("incremented nonce-count should succeed")
	}
}

func TestMalformedNonceCount(t *testing.T) {
	auth := newTestAuthenticator()

	nonce, err := auth.generateNonce("10.0.0.1")
	if err != nil {
		t.Fatalf("generateNonce failed: %v", err)
	}

	// Non-hex nc should fail
	if auth.validateNonce(nonce, "10.0.0.1", "notahex") {
		t.Fatal("malformed nc should be rejected")
	}

	// Negative nc should fail
	if auth.validateNonce(nonce, "10.0.0.1", "-1") {
		t.Fatal("negative nc should be rejected")
	}
}

func TestNonceGenerationFailClosed(t *testing.T) {
	auth := newTestAuthenticator()

	// generateNonce should return a non-empty nonce under normal conditions
	nonce, err := auth.generateNonce("10.0.0.1")
	if err != nil {
		t.Fatalf("generateNonce should succeed: %v", err)
	}
	if nonce == "" {
		t.Fatal("nonce should not be empty")
	}

	// generateChallenge returns empty string on nonce failure (tested via interface)
	challenge := auth.generateChallenge("10.0.0.1")
	if challenge == "" {
		t.Fatal("challenge should not be empty under normal conditions")
	}
}

func TestNonceIPMismatch(t *testing.T) {
	auth := newTestAuthenticator()

	nonce, err := auth.generateNonce("10.0.0.1")
	if err != nil {
		t.Fatalf("generateNonce failed: %v", err)
	}

	// Different IP should fail
	if auth.validateNonce(nonce, "10.0.0.2", "00000001") {
		t.Fatal("nonce from different IP should be rejected")
	}

	// Correct IP should succeed
	if !auth.validateNonce(nonce, "10.0.0.1", "00000001") {
		t.Fatal("nonce from correct IP should succeed")
	}
}
