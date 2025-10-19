package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"siprec-server/pkg/config"

	"github.com/sirupsen/logrus"
)

type mockGDPRService struct {
	exportPath string
	exportErr  error
	eraseErr   error
	lastExport string
	lastErase  string
}

func (m *mockGDPRService) ExportCallData(callID string) (string, error) {
	m.lastExport = callID
	return m.exportPath, m.exportErr
}

func (m *mockGDPRService) EraseCallData(callID string) error {
	m.lastErase = callID
	return m.eraseErr
}

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(bytes.NewBuffer(nil))
	logger.SetLevel(logrus.DebugLevel)
	return logger
}

func TestComplianceStatus(t *testing.T) {
	cfg := &config.Config{}
	cfg.Compliance.PCI.Enabled = true
	cfg.PII.Enabled = true
	cfg.PII.ApplyToTranscriptions = true
	cfg.PII.ApplyToRecordings = true
	cfg.Encryption.EnableRecordingEncryption = true
	cfg.Network.EnableTLS = true
	cfg.Network.RequireTLSOnly = true
	cfg.Network.EnableSRTP = true
	cfg.Network.RequireSRTP = true
	cfg.Compliance.GDPR.Enabled = true
	cfg.Compliance.GDPR.ExportDir = "/tmp"
	cfg.Compliance.Audit.TamperProof = true
	cfg.Compliance.Audit.LogPath = "/logs/audit.log"

	handler := NewComplianceHandler(newTestLogger(), &mockGDPRService{}, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/compliance/status", nil)
	rr := httptest.NewRecorder()
	handler.handleComplianceStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 got %d", rr.Code)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	if pci, ok := payload["pci"].(map[string]interface{}); !ok || !pci["enabled"].(bool) {
		t.Fatalf("expected pci status in response: %v", payload)
	}
	if gdpr, ok := payload["gdpr"].(map[string]interface{}); !ok || !gdpr["enabled"].(bool) {
		t.Fatalf("expected gdpr status in response: %v", payload)
	}
	if audit, ok := payload["audit"].(map[string]interface{}); !ok || !audit["tamper_proof"].(bool) {
		t.Fatalf("expected audit status in response: %v", payload)
	}
}

func TestGDPRExportUnavailable(t *testing.T) {
	handler := NewComplianceHandler(newTestLogger(), nil, &config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/compliance/gdpr/export", bytes.NewBufferString(`{"call_id":"abc"}`))
	rr := httptest.NewRecorder()
	handler.handleGDPRExport(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503 got %d", rr.Code)
	}
}

func TestGDPRExportSuccess(t *testing.T) {
	mock := &mockGDPRService{exportPath: "/exports/abc.json"}
	handler := NewComplianceHandler(newTestLogger(), mock, &config.Config{})

	req := httptest.NewRequest(http.MethodPost, "/api/compliance/gdpr/export", bytes.NewBufferString(`{"call_id":"call-1"}`))
	rr := httptest.NewRecorder()
	handler.handleGDPRExport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200 got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}

	if resp["status"] != "exported" {
		t.Fatalf("expected status exported, got %v", resp["status"])
	}

	if mock.lastExport != "call-1" {
		t.Fatalf("expected export for call-1, got %s", mock.lastExport)
	}
}

func TestGDPREraseFailure(t *testing.T) {
	mock := &mockGDPRService{eraseErr: assertAnError(t)}
	handler := NewComplianceHandler(newTestLogger(), mock, &config.Config{})

	req := httptest.NewRequest(http.MethodDelete, "/api/compliance/gdpr/erase", bytes.NewBufferString(`{"call_id":"call-2"}`))
	rr := httptest.NewRecorder()
	handler.handleGDPRErase(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 got %d", rr.Code)
	}
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func assertAnError(t *testing.T) error {
	t.Helper()
	return simpleError("boom")
}
