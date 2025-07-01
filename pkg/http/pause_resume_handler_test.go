package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"siprec-server/pkg/config"
	"siprec-server/pkg/metrics"

	"github.com/sirupsen/logrus"
)

// MockPauseResumeService implements PauseResumeService for testing
type MockPauseResumeService struct {
	sessions map[string]*PauseStatus
}

func NewMockPauseResumeService() *MockPauseResumeService {
	return &MockPauseResumeService{
		sessions: make(map[string]*PauseStatus),
	}
}

func (m *MockPauseResumeService) PauseSession(sessionID string, pauseRecording, pauseTranscription bool) error {
	now := time.Now()
	m.sessions[sessionID] = &PauseStatus{
		SessionID:            sessionID,
		IsPaused:            pauseRecording || pauseTranscription,
		RecordingPaused:     pauseRecording,
		TranscriptionPaused: pauseTranscription,
		PausedAt:            &now,
	}
	return nil
}

func (m *MockPauseResumeService) ResumeSession(sessionID string) error {
	if status, exists := m.sessions[sessionID]; exists {
		status.IsPaused = false
		status.RecordingPaused = false
		status.TranscriptionPaused = false
		status.PausedAt = nil
		status.PauseDuration = 0
	}
	return nil
}

func (m *MockPauseResumeService) PauseAll(pauseRecording, pauseTranscription bool) error {
	for _, status := range m.sessions {
		status.IsPaused = pauseRecording || pauseTranscription
		status.RecordingPaused = pauseRecording
		status.TranscriptionPaused = pauseTranscription
		now := time.Now()
		status.PausedAt = &now
	}
	return nil
}

func (m *MockPauseResumeService) ResumeAll() error {
	for _, status := range m.sessions {
		status.IsPaused = false
		status.RecordingPaused = false
		status.TranscriptionPaused = false
		status.PausedAt = nil
		status.PauseDuration = 0
	}
	return nil
}

func (m *MockPauseResumeService) GetPauseStatus(sessionID string) (*PauseStatus, error) {
	if status, exists := m.sessions[sessionID]; exists {
		// Calculate duration if paused
		if status.IsPaused && status.PausedAt != nil {
			status.PauseDuration = time.Since(*status.PausedAt)
		}
		return status, nil
	}
	return nil, &MockError{"session not found"}
}

func (m *MockPauseResumeService) GetAllPauseStatuses() (map[string]*PauseStatus, error) {
	result := make(map[string]*PauseStatus)
	for k, v := range m.sessions {
		// Calculate duration if paused
		if v.IsPaused && v.PausedAt != nil {
			v.PauseDuration = time.Since(*v.PausedAt)
		}
		result[k] = v
	}
	return result, nil
}

type MockError struct {
	message string
}

func (e *MockError) Error() string {
	return e.message
}

// MockServer implements the Server interface for testing
type MockServer struct {
	handlers map[string]http.HandlerFunc
}

func NewMockServer() *MockServer {
	return &MockServer{
		handlers: make(map[string]http.HandlerFunc),
	}
}

func (m *MockServer) RegisterHandler(path string, handler http.HandlerFunc) {
	m.handlers[path] = handler
}

func TestPauseResumeHandler(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise in tests

	// Disable metrics for testing to avoid initialization issues
	metrics.EnableMetrics(false)

	t.Run("pause session endpoint", func(t *testing.T) {
		mockService := NewMockPauseResumeService()
		mockService.sessions["test-session"] = &PauseStatus{
			SessionID: "test-session",
		}

		config := &config.PauseResumeConfig{
			Enabled:            true,
			PerSession:         true,
			PauseRecording:     true,
			PauseTranscription: true,
			RequireAuth:        false,
		}

		handler := NewPauseResumeHandler(logger, config, mockService)

		// Create request
		reqBody := map[string]bool{
			"pause_recording":     true,
			"pause_transcription": false,
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/api/sessions/test-session/pause", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Call handler
		handler.handlePauseSession(w, req)

		// Check response
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var response PauseStatus
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !response.RecordingPaused {
			t.Fatal("expected recording to be paused")
		}
		if response.TranscriptionPaused {
			t.Fatal("expected transcription to NOT be paused")
		}
	})

	t.Run("resume session endpoint", func(t *testing.T) {
		mockService := NewMockPauseResumeService()
		// Set up a paused session
		mockService.PauseSession("test-session", true, true)

		config := &config.PauseResumeConfig{
			Enabled:     true,
			PerSession:  true,
			RequireAuth: false,
		}

		handler := NewPauseResumeHandler(logger, config, mockService)

		req := httptest.NewRequest("POST", "/api/sessions/test-session/resume", nil)
		w := httptest.NewRecorder()

		// Call handler
		handler.handleResumeSession(w, req)

		// Check response
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var response PauseStatus
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.IsPaused {
			t.Fatal("expected session to be resumed")
		}
	})

	t.Run("get pause status endpoint", func(t *testing.T) {
		mockService := NewMockPauseResumeService()
		mockService.sessions["test-session"] = &PauseStatus{
			SessionID:            "test-session",
			IsPaused:            true,
			RecordingPaused:     true,
			TranscriptionPaused: false,
		}

		config := &config.PauseResumeConfig{
			Enabled:     true,
			PerSession:  true,
			RequireAuth: false,
		}

		handler := NewPauseResumeHandler(logger, config, mockService)

		req := httptest.NewRequest("GET", "/api/sessions/test-session/pause-status", nil)
		w := httptest.NewRecorder()

		// Call handler
		handler.handleGetPauseStatus(w, req)

		// Check response
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var response PauseStatus
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if response.SessionID != "test-session" {
			t.Fatalf("expected session ID 'test-session', got '%s'", response.SessionID)
		}
		if !response.IsPaused {
			t.Fatal("expected session to be paused")
		}
	})

	t.Run("pause all sessions endpoint", func(t *testing.T) {
		mockService := NewMockPauseResumeService()
		// Add some test sessions
		mockService.sessions["session-1"] = &PauseStatus{SessionID: "session-1"}
		mockService.sessions["session-2"] = &PauseStatus{SessionID: "session-2"}

		config := &config.PauseResumeConfig{
			Enabled:            true,
			PauseRecording:     true,
			PauseTranscription: false,
			RequireAuth:        false,
		}

		handler := NewPauseResumeHandler(logger, config, mockService)

		req := httptest.NewRequest("POST", "/api/sessions/pause-all", nil)
		w := httptest.NewRecorder()

		// Call handler
		handler.handlePauseAll(w, req)

		// Check response
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", w.Code)
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		statuses, ok := response["statuses"].(map[string]interface{})
		if !ok {
			t.Fatal("expected statuses in response")
		}

		if len(statuses) != 2 {
			t.Fatalf("expected 2 statuses, got %d", len(statuses))
		}
	})

	t.Run("authentication required", func(t *testing.T) {
		mockService := NewMockPauseResumeService()

		config := &config.PauseResumeConfig{
			Enabled:     true,
			RequireAuth: true,
			APIKey:      "test-api-key",
		}

		handler := NewPauseResumeHandler(logger, config, mockService)

		// Request without API key
		req := httptest.NewRequest("POST", "/api/sessions/test/pause", nil)
		w := httptest.NewRecorder()

		authHandler := handler.authMiddleware(handler.handlePauseSession)
		authHandler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected status 401, got %d", w.Code)
		}

		// Request with correct API key
		req = httptest.NewRequest("POST", "/api/sessions/test/pause", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w = httptest.NewRecorder()

		authHandler(w, req)

		// Should pass through to the actual handler (which may fail for other reasons)
		if w.Code == http.StatusUnauthorized {
			t.Fatal("request with valid API key should not be unauthorized")
		}
	})

	t.Run("extract session ID", func(t *testing.T) {
		tests := []struct {
			path     string
			expected string
		}{
			{"/api/sessions/test-123/pause", "test-123"},
			{"/api/sessions/session-456/resume", "session-456"},
			{"/api/sessions/abc-def-ghi/pause-status", "abc-def-ghi"},
			{"/api/sessions//pause", ""},
			{"/invalid/path", ""},
			{"", ""},
		}

		for _, test := range tests {
			result := extractSessionID(test.path)
			if result != test.expected {
				t.Fatalf("path '%s': expected '%s', got '%s'", test.path, test.expected, result)
			}
		}
	})
}