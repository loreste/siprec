package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"siprec-server/pkg/config"
	"siprec-server/pkg/errors"
	"siprec-server/pkg/metrics"

	"github.com/sirupsen/logrus"
)

// PauseResumeService defines the interface for pause/resume operations
type PauseResumeService interface {
	// PauseSession pauses recording and/or transcription for a session
	PauseSession(sessionID string, pauseRecording, pauseTranscription bool) error
	
	// ResumeSession resumes recording and/or transcription for a session
	ResumeSession(sessionID string) error
	
	// PauseAll pauses all active sessions
	PauseAll(pauseRecording, pauseTranscription bool) error
	
	// ResumeAll resumes all paused sessions
	ResumeAll() error
	
	// GetPauseStatus returns the pause status for a session
	GetPauseStatus(sessionID string) (*PauseStatus, error)
	
	// GetAllPauseStatuses returns pause status for all sessions
	GetAllPauseStatuses() (map[string]*PauseStatus, error)
}

// PauseStatus represents the pause state of a session
type PauseStatus struct {
	SessionID            string        `json:"session_id"`
	IsPaused            bool          `json:"is_paused"`
	RecordingPaused     bool          `json:"recording_paused"`
	TranscriptionPaused bool          `json:"transcription_paused"`
	PausedAt            *time.Time    `json:"paused_at,omitempty"`
	PauseDuration       time.Duration `json:"pause_duration,omitempty"`
	AutoResumeAt        *time.Time    `json:"auto_resume_at,omitempty"`
}

// PauseResumeHandler handles pause/resume API requests
type PauseResumeHandler struct {
	logger  *logrus.Logger
	config  *config.PauseResumeConfig
	service PauseResumeService
}

// NewPauseResumeHandler creates a new pause/resume handler
func NewPauseResumeHandler(logger *logrus.Logger, config *config.PauseResumeConfig, service PauseResumeService) *PauseResumeHandler {
	return &PauseResumeHandler{
		logger:  logger,
		config:  config,
		service: service,
	}
}

// RegisterHandlers registers pause/resume handlers with the HTTP server
func (h *PauseResumeHandler) RegisterHandlers(server *Server) {
	if !h.config.Enabled {
		h.logger.Debug("Pause/Resume API is disabled, not registering handlers")
		return
	}

	// Register individual session endpoints
	if h.config.PerSession {
		server.RegisterHandler("/api/sessions/{id}/pause", h.authMiddleware(h.handlePauseSession))
		server.RegisterHandler("/api/sessions/{id}/resume", h.authMiddleware(h.handleResumeSession))
		server.RegisterHandler("/api/sessions/{id}/pause-status", h.authMiddleware(h.handleGetPauseStatus))
	}
	
	// Register global endpoints
	server.RegisterHandler("/api/sessions/pause-all", h.authMiddleware(h.handlePauseAll))
	server.RegisterHandler("/api/sessions/resume-all", h.authMiddleware(h.handleResumeAll))
	server.RegisterHandler("/api/sessions/pause-status", h.authMiddleware(h.handleGetAllPauseStatuses))
	
	h.logger.Info("Pause/Resume API handlers registered")
}

// authMiddleware checks API key if authentication is required
func (h *PauseResumeHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.config.RequireAuth {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				apiKey = r.URL.Query().Get("api_key")
			}
			
			if apiKey != h.config.APIKey {
				h.logger.Warn("Unauthorized pause/resume API request")
				writeJSONError(w, errors.New("unauthorized"), http.StatusUnauthorized)
				return
			}
		}
		
		next(w, r)
	}
}

// handlePauseSession handles pause requests for a specific session
func (h *PauseResumeHandler) handlePauseSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		writeJSONError(w, errors.New("invalid session ID"), http.StatusBadRequest)
		return
	}

	// Parse request body
	var req struct {
		PauseRecording     *bool `json:"pause_recording,omitempty"`
		PauseTranscription *bool `json:"pause_transcription,omitempty"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, use defaults from config
		req.PauseRecording = &h.config.PauseRecording
		req.PauseTranscription = &h.config.PauseTranscription
	}

	// Use config defaults if not specified
	pauseRecording := h.config.PauseRecording
	if req.PauseRecording != nil {
		pauseRecording = *req.PauseRecording
	}
	
	pauseTranscription := h.config.PauseTranscription
	if req.PauseTranscription != nil {
		pauseTranscription = *req.PauseTranscription
	}

	// Pause the session
	if err := h.service.PauseSession(sessionID, pauseRecording, pauseTranscription); err != nil {
		h.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to pause session")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Record metrics
	if metrics.IsMetricsEnabled() {
		pauseType := "both"
		if pauseRecording && !pauseTranscription {
			pauseType = "recording"
		} else if !pauseRecording && pauseTranscription {
			pauseType = "transcription"
		}
		metrics.RecordSessionPaused(sessionID, pauseType)
	}

	// Get updated status
	status, err := h.service.GetPauseStatus(sessionID)
	if err != nil {
		h.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to get pause status")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, status, http.StatusOK)
	
	h.logger.WithFields(logrus.Fields{
		"session_id":          sessionID,
		"pause_recording":     pauseRecording,
		"pause_transcription": pauseTranscription,
	}).Info("Session paused")
}

// handleResumeSession handles resume requests for a specific session
func (h *PauseResumeHandler) handleResumeSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		writeJSONError(w, errors.New("invalid session ID"), http.StatusBadRequest)
		return
	}

	// Resume the session
	if err := h.service.ResumeSession(sessionID); err != nil {
		h.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to resume session")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Record metrics
	if metrics.IsMetricsEnabled() {
		metrics.RecordSessionResumed(sessionID)
	}

	// Get updated status
	status, err := h.service.GetPauseStatus(sessionID)
	if err != nil {
		h.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to get pause status")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, status, http.StatusOK)
	
	h.logger.WithField("session_id", sessionID).Info("Session resumed")
}

// handleGetPauseStatus handles requests for pause status of a specific session
func (h *PauseResumeHandler) handleGetPauseStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL
	sessionID := extractSessionID(r.URL.Path)
	if sessionID == "" {
		writeJSONError(w, errors.New("invalid session ID"), http.StatusBadRequest)
		return
	}

	// Get pause status
	status, err := h.service.GetPauseStatus(sessionID)
	if err != nil {
		h.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to get pause status")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, status, http.StatusOK)
}

// handlePauseAll handles pause requests for all sessions
func (h *PauseResumeHandler) handlePauseAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req struct {
		PauseRecording     *bool `json:"pause_recording,omitempty"`
		PauseTranscription *bool `json:"pause_transcription,omitempty"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// If no body, use defaults from config
		req.PauseRecording = &h.config.PauseRecording
		req.PauseTranscription = &h.config.PauseTranscription
	}

	// Use config defaults if not specified
	pauseRecording := h.config.PauseRecording
	if req.PauseRecording != nil {
		pauseRecording = *req.PauseRecording
	}
	
	pauseTranscription := h.config.PauseTranscription
	if req.PauseTranscription != nil {
		pauseTranscription = *req.PauseTranscription
	}

	// Pause all sessions
	if err := h.service.PauseAll(pauseRecording, pauseTranscription); err != nil {
		h.logger.WithError(err).Error("Failed to pause all sessions")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Get all statuses
	statuses, err := h.service.GetAllPauseStatuses()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get pause statuses")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Record metrics
	if metrics.IsMetricsEnabled() {
		pauseType := "both"
		if pauseRecording && !pauseTranscription {
			pauseType = "recording"
		} else if !pauseRecording && pauseTranscription {
			pauseType = "transcription"
		}
		for sessionID := range statuses {
			metrics.RecordSessionPaused(sessionID, pauseType)
		}
	}

	writeJSONResponse(w, map[string]interface{}{
		"message": "All sessions paused",
		"statuses": statuses,
	}, http.StatusOK)
	
	h.logger.WithFields(logrus.Fields{
		"pause_recording":     pauseRecording,
		"pause_transcription": pauseTranscription,
		"session_count":       len(statuses),
	}).Info("All sessions paused")
}

// handleResumeAll handles resume requests for all sessions
func (h *PauseResumeHandler) handleResumeAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Resume all sessions
	if err := h.service.ResumeAll(); err != nil {
		h.logger.WithError(err).Error("Failed to resume all sessions")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Get all statuses
	statuses, err := h.service.GetAllPauseStatuses()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get pause statuses")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	// Record metrics
	if metrics.IsMetricsEnabled() {
		for sessionID := range statuses {
			metrics.RecordSessionResumed(sessionID)
		}
	}

	writeJSONResponse(w, map[string]interface{}{
		"message": "All sessions resumed",
		"statuses": statuses,
	}, http.StatusOK)
	
	h.logger.WithField("session_count", len(statuses)).Info("All sessions resumed")
}

// handleGetAllPauseStatuses handles requests for pause status of all sessions
func (h *PauseResumeHandler) handleGetAllPauseStatuses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, errors.New("method not allowed"), http.StatusMethodNotAllowed)
		return
	}

	// Get all pause statuses
	statuses, err := h.service.GetAllPauseStatuses()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get pause statuses")
		writeJSONError(w, err, http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, statuses, http.StatusOK)
}

// Helper functions

// extractSessionID extracts session ID from URL path like /api/sessions/{id}/pause
func extractSessionID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "api" && parts[1] == "sessions" && parts[2] != "" {
		return parts[2]
	}
	return ""
}

// writeJSONResponse writes a JSON response
func writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logrus.WithError(err).Error("Failed to encode JSON response")
	}
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, err error, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	response := map[string]interface{}{
		"error": err.Error(),
		"code":  statusCode,
	}
	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		logrus.WithError(encErr).Error("Failed to encode JSON error response")
	}
}