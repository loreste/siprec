package lawfulintercept

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogger(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create temp directory
	tempDir, err := os.MkdirTemp("", "audit-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	auditPath := filepath.Join(tempDir, "audit.log")

	// Create audit logger
	al, err := NewAuditLogger(auditPath, logger)
	require.NoError(t, err)
	require.NotNil(t, al)

	// Log some events
	al.Log(AuditEventSystemStart, "system", "System started", nil)
	al.Log(AuditEventInterceptStarted, "warrant-123", "Intercept started", map[string]interface{}{
		"target_id": "+15551234567",
	})
	al.Log(AuditEventContentDelivered, "warrant-123", "Content delivered", map[string]interface{}{
		"intercept_id": "li-123",
		"bytes":        1024,
	})

	// Close logger (flushes buffer)
	err = al.Close()
	require.NoError(t, err)

	// Verify file exists and has content
	data, err := os.ReadFile(auditPath)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify we can parse the entries
	lines := splitLines(data)
	assert.GreaterOrEqual(t, len(lines), 3)

	var entry AuditEntry
	err = json.Unmarshal([]byte(lines[0]), &entry)
	require.NoError(t, err)
	assert.Equal(t, AuditEventSystemStart, entry.EventType)
}

func TestAuditLoggerImmediate(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	tempDir, err := os.MkdirTemp("", "audit-immediate-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	auditPath := filepath.Join(tempDir, "audit.log")

	al, err := NewAuditLogger(auditPath, logger)
	require.NoError(t, err)

	// Log immediate event
	err = al.LogImmediate(AuditEventWarrantVerified, "warrant-456", "Warrant verified", nil)
	require.NoError(t, err)

	// Should be written immediately
	data, err := os.ReadFile(auditPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "warrant_verified")

	al.Close()
}

func TestAuditLoggerRotation(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	tempDir, err := os.MkdirTemp("", "audit-rotate-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	auditPath := filepath.Join(tempDir, "audit.log")

	al, err := NewAuditLogger(auditPath, logger)
	require.NoError(t, err)

	// Write some entries
	al.LogImmediate(AuditEventSystemStart, "system", "Before rotation", nil)

	// Rotate
	err = al.Rotate()
	require.NoError(t, err)

	// Write after rotation
	al.LogImmediate(AuditEventSystemStop, "system", "After rotation", nil)

	al.Close()

	// Verify rotated file exists
	files, err := filepath.Glob(filepath.Join(tempDir, "audit.log.*"))
	require.NoError(t, err)
	assert.Len(t, files, 1)
}

func TestDeliveryClient(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create test server with mutex-protected slice
	var mu sync.Mutex
	receivedRequests := make([]DeliveryRequest, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req DeliveryRequest
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		receivedRequests = append(receivedRequests, req)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeliveryResponse{
			Status:    "accepted",
			MessageID: "msg-123",
		})
	}))
	defer server.Close()

	// Create delivery client
	dc := NewDeliveryClient(DeliveryConfig{
		Endpoint:      server.URL,
		Timeout:       5 * time.Second,
		MaxRetries:    2,
		BatchSize:     2,
		FlushInterval: 100 * time.Millisecond,
	}, logger)

	// Deliver some content
	err := dc.Deliver(context.Background(), "warrant-123", "li-456", []byte("test payload"))
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Verify request was sent
	mu.Lock()
	reqLen := len(receivedRequests)
	var firstReq DeliveryRequest
	if reqLen > 0 {
		firstReq = receivedRequests[0]
	}
	mu.Unlock()

	assert.Equal(t, 1, reqLen)
	assert.Equal(t, "warrant-123", firstReq.WarrantID)
	assert.Equal(t, "li-456", firstReq.InterceptID)

	dc.Close()
}

func TestDeliveryClientImmediate(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	receivedCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeliveryResponse{Status: "ok"})
	}))
	defer server.Close()

	dc := NewDeliveryClient(DeliveryConfig{
		Endpoint: server.URL,
		Timeout:  5 * time.Second,
	}, logger)

	err := dc.DeliverImmediate(context.Background(), "warrant-1", "li-1", []byte("immediate"))
	require.NoError(t, err)
	assert.Equal(t, 1, receivedCount)

	dc.Close()
}

func TestDeliveryClientRetry(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(DeliveryResponse{Status: "ok"})
	}))
	defer server.Close()

	dc := NewDeliveryClient(DeliveryConfig{
		Endpoint:   server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 5,
	}, logger)

	err := dc.DeliverImmediate(context.Background(), "warrant-1", "li-1", []byte("retry test"))
	require.NoError(t, err)
	assert.Equal(t, 3, attemptCount)

	dc.Close()
}

func TestWarrantVerifier(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req WarrantRequest
		json.NewDecoder(r.Body).Decode(&req)

		resp := WarrantResponse{
			Valid:     req.WarrantID == "valid-warrant",
			WarrantID: req.WarrantID,
			TargetID:  req.TargetID,
			Authority: "Test Court",
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}

		if !resp.Valid {
			resp.Error = "warrant not found"
			resp.ErrorCode = "WARRANT_NOT_FOUND"
		}

		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	wv := NewWarrantVerifier(server.URL, nil, logger)

	// Test valid warrant
	valid, err := wv.Verify(context.Background(), "valid-warrant", "target-123")
	require.NoError(t, err)
	assert.True(t, valid)

	// Test invalid warrant
	valid, err = wv.Verify(context.Background(), "invalid-warrant", "target-123")
	require.NoError(t, err)
	assert.False(t, valid)

	// Test caching
	valid, err = wv.Verify(context.Background(), "valid-warrant", "target-123")
	require.NoError(t, err)
	assert.True(t, valid) // Should be from cache

	// Verify cache size
	size, _ := wv.GetCacheStats()
	assert.Equal(t, 2, size)
}

func TestWarrantVerifierDetails(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WarrantResponse{
			Valid:      true,
			WarrantID:  "warrant-123",
			TargetID:   "target-456",
			Authority:  "District Court",
			CaseNumber: "2024-CV-1234",
			Scope:      []string{"audio", "metadata"},
			ExpiresAt:  time.Now().Add(30 * 24 * time.Hour),
		})
	}))
	defer server.Close()

	wv := NewWarrantVerifier(server.URL, nil, logger)

	resp, err := wv.VerifyWithDetails(context.Background(), "warrant-123", "target-456")
	require.NoError(t, err)
	assert.True(t, resp.Valid)
	assert.Equal(t, "District Court", resp.Authority)
	assert.Equal(t, "2024-CV-1234", resp.CaseNumber)
	assert.Contains(t, resp.Scope, "audio")
}

func TestContentEncryptor(t *testing.T) {
	// Generate test RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	encryptor := NewContentEncryptorFromKey(&privateKey.PublicKey, "test-key-id")

	content := &InterceptedContent{
		InterceptID:    "li-123",
		WarrantID:      "warrant-456",
		CallID:         "call-789",
		Timestamp:      time.Now(),
		ContentType:    "audio",
		Direction:      "inbound",
		SourceURI:      "sip:alice@example.com",
		DestinationURI: "sip:bob@example.com",
		AudioData:      []byte("test audio data"),
	}

	// Encrypt
	encrypted, err := encryptor.Encrypt(content)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	// Verify encrypted content structure
	var ec EncryptedContent
	err = json.Unmarshal(encrypted, &ec)
	require.NoError(t, err)
	assert.Equal(t, "1.0", ec.Version)
	assert.Equal(t, "test-key-id", ec.KeyID)
	assert.Equal(t, "RSA-OAEP-AES-256-GCM", ec.Algorithm)
	assert.NotEmpty(t, ec.EncryptedKey)
	assert.NotEmpty(t, ec.Nonce)
	assert.NotEmpty(t, ec.Ciphertext)
	assert.NotEmpty(t, ec.ContentHash)
}

func TestContentEncryptorRaw(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	encryptor := NewContentEncryptorFromKey(&privateKey.PublicKey, "key-1")

	testData := []byte("raw data to encrypt")
	encrypted, err := encryptor.EncryptRaw(testData)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	var ec EncryptedContent
	err = json.Unmarshal(encrypted, &ec)
	require.NoError(t, err)
	assert.NotEmpty(t, ec.Ciphertext)
}

func TestManager(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create test server for delivery
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(DeliveryResponse{Status: "ok"})
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "li-manager-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := Config{
		Enabled:          true,
		DeliveryEndpoint: server.URL,
		AuditLogPath:     filepath.Join(tempDir, "audit.log"),
		DeliveryTimeout:  5 * time.Second,
		MaxRetries:       1,
	}

	m, err := NewManager(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, m)

	// Register intercept
	intercept, err := m.RegisterIntercept("warrant-123", "+15551234567", "phone", nil)
	require.NoError(t, err)
	require.NotNil(t, intercept)
	assert.Equal(t, InterceptStatusActive, intercept.Status)

	// Check target
	matches := m.CheckTarget("+15551234567")
	assert.Len(t, matches, 1)

	// Deliver content
	content := &InterceptedContent{
		InterceptID:    intercept.ID,
		WarrantID:      intercept.WarrantID,
		CallID:         "call-123",
		Timestamp:      time.Now(),
		ContentType:    "audio",
		AudioData:      []byte("test audio"),
	}
	err = m.DeliverContent(intercept.ID, content)
	require.NoError(t, err)

	// Check stats
	stats := m.GetStats()
	assert.Equal(t, int64(1), stats.TotalIntercepts)
	assert.Equal(t, int64(1), stats.ActiveIntercepts)

	// Revoke intercept
	err = m.RevokeIntercept(intercept.ID, "test revocation")
	require.NoError(t, err)

	// Verify revoked
	active := m.GetActiveIntercepts()
	assert.Len(t, active, 0)

	// Close manager
	err = m.Close()
	require.NoError(t, err)
}

func TestManagerDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		Enabled: false,
	}

	m, err := NewManager(cfg, logger)
	require.NoError(t, err)
	assert.Nil(t, m) // Should return nil when disabled
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
