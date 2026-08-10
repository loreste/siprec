package auth

import (
	"errors"
	"io"
	"strings"
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

	// Values that would not fit the supported 32-bit deployment bound should fail.
	if auth.validateNonce(nonce, "10.0.0.1", "80000000") {
		t.Fatal("nonce count above 1<<31-1 should be rejected")
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

	// Force the entropy source to fail. Challenge generation must fail closed
	// instead of issuing a predictable or empty nonce.
	auth.randomReader = failingReader{}
	challenge := auth.generateChallenge("10.0.0.1")
	if challenge != "" {
		t.Fatal("challenge should be empty when secure randomness fails")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy unavailable")
}

func TestFailedDigestDoesNotConsumeNonceCount(t *testing.T) {
	auth := newTestAuthenticator()
	uri := "sip:test@example.com"
	nonce, err := auth.generateNonce("10.0.0.1")
	if err != nil {
		t.Fatalf("generateNonce failed: %v", err)
	}

	creds := &DigestCredentials{
		Username:  "alice",
		Realm:     "test",
		Nonce:     nonce,
		URI:       uri,
		Algorithm: "MD5",
		QOP:       "auth",
		NC:        "00000001",
		CNonce:    "client-nonce",
	}
	validResponse := auth.calculateResponse("secret123", "REGISTER", uri, creds)
	header := `Digest username="alice", realm="test", nonce="` + nonce + `", uri="` + uri + `", response="bad", algorithm=MD5, qop=auth, nc=00000001, cnonce="client-nonce"`
	if result := auth.Authenticate(header, "REGISTER", uri, "10.0.0.1"); result.Success {
		t.Fatal("invalid digest response should fail")
	}

	header = `Digest username="alice", realm="test", nonce="` + nonce + `", uri="` + uri + `", response="` + validResponse + `", algorithm=MD5, qop=auth, nc=00000001, cnonce="client-nonce"`
	if result := auth.Authenticate(header, "REGISTER", uri, "10.0.0.1"); !result.Success {
		t.Fatalf("valid response should still be accepted after failed attempt: %s", result.Reason)
	}
}

func TestDigestParserEnforcesRequestBinding(t *testing.T) {
	auth := newTestAuthenticator()
	uri := "sip:test@example.com"
	base := `Digest username="alice", realm="test", nonce="nonce", uri="` + uri + `", response="response", algorithm=MD5, qop=auth, nc=00000001, cnonce="cnonce"`

	if _, err := auth.parseDigestAuth(base, "sip:other@example.com"); err == nil {
		t.Fatal("digest URI mismatch should be rejected")
	}
	if _, err := auth.parseDigestAuth(strings.Replace(base, "algorithm=MD5", "algorithm=SHA-256", 1), uri); err == nil {
		t.Fatal("unsupported algorithm should be rejected")
	}
	if _, err := auth.parseDigestAuth(strings.Replace(base, "qop=auth", "qop=auth-int", 1), uri); err == nil {
		t.Fatal("unsupported qop should be rejected")
	}
	if _, err := auth.parseDigestAuth(strings.Replace(base, "nc=00000001", "nc=1", 1), uri); err == nil {
		t.Fatal("nonce count with fewer than eight digits should be rejected")
	}
	if _, err := auth.parseDigestAuth(strings.Replace(base, ", cnonce=\"cnonce\"", "", 1), uri); err == nil {
		t.Fatal("missing cnonce should be rejected")
	}
	if _, err := auth.parseDigestAuth(strings.Replace(base, "realm=\"test\"", "realm=\"other\"", 1), uri); err == nil {
		t.Fatal("realm mismatch should be rejected")
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
