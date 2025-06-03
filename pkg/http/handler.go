package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"siprec-server/pkg/errors"

	"github.com/sirupsen/logrus"
)

// SessionHandler handles HTTP requests related to recording sessions
type SessionHandler struct {
	logger  *logrus.Logger
	service SessionService
}

// SessionService defines the interface for session-related operations
type SessionService interface {
	// GetSessionByID returns session info by ID
	GetSessionByID(id string) (interface{}, error)

	// GetAllSessions returns information about all active sessions
	GetAllSessions() ([]interface{}, error)

	// GetSessionStatistics returns session statistics
	GetSessionStatistics() map[string]interface{}
}

// NewSessionHandler creates a new session handler
func NewSessionHandler(logger *logrus.Logger, service SessionService) *SessionHandler {
	return &SessionHandler{
		logger:  logger,
		service: service,
	}
}

// RegisterHandlers registers all session-related handlers with the HTTP server
func (h *SessionHandler) RegisterHandlers(server *Server) {
	server.RegisterHandler("/api/sessions", h.handleGetSessions)
	server.RegisterHandler("/api/sessions/stats", h.handleSessionStats)
}

// handleGetSessions handles GET requests for sessions
func (h *SessionHandler) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if a specific session ID was requested
	sessionID := r.URL.Query().Get("id")
	if sessionID != "" {
		h.handleGetSessionByID(w, r, sessionID)
		return
	}

	// Get all sessions
	sessions, err := h.service.GetAllSessions()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get sessions")
		errors.WriteError(w, errors.Wrap(err, "Failed to get sessions"))
		return
	}

	// Return the sessions
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// handleGetSessionByID handles GET requests for a specific session
func (h *SessionHandler) handleGetSessionByID(w http.ResponseWriter, r *http.Request, id string) {
	session, err := h.service.GetSessionByID(id)
	if err != nil {
		h.logger.WithError(err).WithField("session_id", id).Error("Failed to get session")
		errors.WriteError(w, errors.Wrap(err, fmt.Sprintf("Failed to get session %s", id)))
		return
	}

	// Return the session
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

// handleSessionStats handles GET requests for session statistics
func (h *SessionHandler) handleSessionStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get session statistics
	stats := h.service.GetSessionStatistics()

	// Return the statistics
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}
