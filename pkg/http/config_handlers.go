package http

import (
	"encoding/json"
	"net/http"
	"time"

	"siprec-server/pkg/config"

	"github.com/sirupsen/logrus"
)

// ConfigHandlers manages HTTP endpoints for configuration operations
type ConfigHandlers struct {
	hotReloadManager *config.HotReloadManager
	logger           *logrus.Logger
}

// NewConfigHandlers creates new configuration HTTP handlers
func NewConfigHandlers(hotReloadManager *config.HotReloadManager, logger *logrus.Logger) *ConfigHandlers {
	return &ConfigHandlers{
		hotReloadManager: hotReloadManager,
		logger:           logger,
	}
}

// RegisterConfigEndpoints registers configuration endpoints on the server
func (s *Server) RegisterConfigEndpoints(handlers *ConfigHandlers) {
	s.RegisterHandler("/api/config", handlers.GetConfigHandler)
	s.RegisterHandler("/api/config/validate", handlers.ValidateConfigHandler)
	s.RegisterHandler("/api/config/reload", handlers.ReloadConfigHandler)
	s.RegisterHandler("/api/config/reload/status", handlers.ReloadStatusHandler)
}

// ConfigResponse represents a configuration response
type ConfigResponse struct {
	Config    *config.Config `json:"config"`
	Timestamp time.Time      `json:"timestamp"`
	Message   string         `json:"message,omitempty"`
}

// ValidationResponse represents a configuration validation response
type ValidationResponse struct {
	Valid     bool                       `json:"valid"`
	Errors    []config.ValidationError   `json:"errors,omitempty"`
	Warnings  []config.ValidationWarning `json:"warnings,omitempty"`
	Summary   string                     `json:"summary"`
	Timestamp time.Time                  `json:"timestamp"`
}

// ReloadResponse represents a configuration reload response
type ReloadResponse struct {
	Success   bool                `json:"success"`
	Event     *config.ReloadEvent `json:"event,omitempty"`
	Message   string              `json:"message"`
	Timestamp time.Time           `json:"timestamp"`
}

// ReloadStatusResponse represents reload status
type ReloadStatusResponse struct {
	Enabled       bool      `json:"enabled"`
	LastReload    time.Time `json:"last_reload,omitempty"`
	CurrentConfig string    `json:"current_config_path"`
	Message       string    `json:"message,omitempty"`
}

// GetConfigHandler handles configuration retrieval requests
func (h *ConfigHandlers) GetConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current configuration and redact secrets before serializing
	currentConfig := h.hotReloadManager.GetCurrentConfig()

	response := ConfigResponse{
		Config:    redactConfigSecrets(currentConfig),
		Timestamp: time.Now(),
		Message:   "Configuration retrieved successfully",
	}

	h.logger.Debug("Configuration retrieved via API")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ValidateConfigHandler handles configuration validation requests
func (h *ConfigHandlers) ValidateConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the configuration from request body
	var configToValidate config.Config
	limitJSONBody(w, r)
	if err := json.NewDecoder(r.Body).Decode(&configToValidate); err != nil {
		h.logger.WithError(err).Error("Failed to decode configuration for validation")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Create validator and validate
	validator := config.NewConfigValidator(h.logger)
	validationResult := validator.ValidateConfig(&configToValidate)

	// Redact secret values echoed back in validation errors/warnings
	sanitizedErrors := make([]config.ValidationError, len(validationResult.Errors))
	for i, validationError := range validationResult.Errors {
		validationError.Value = redactValidationValue(validationError.Field, validationError.Value)
		sanitizedErrors[i] = validationError
	}
	sanitizedWarnings := make([]config.ValidationWarning, len(validationResult.Warnings))
	for i, validationWarning := range validationResult.Warnings {
		validationWarning.Value = redactValidationValue(validationWarning.Field, validationWarning.Value)
		sanitizedWarnings[i] = validationWarning
	}

	response := ValidationResponse{
		Valid:     validationResult.Valid,
		Errors:    sanitizedErrors,
		Warnings:  sanitizedWarnings,
		Summary:   validationResult.Summary,
		Timestamp: time.Now(),
	}

	h.logger.WithFields(logrus.Fields{
		"valid":    validationResult.Valid,
		"errors":   len(validationResult.Errors),
		"warnings": len(validationResult.Warnings),
	}).Info("Configuration validation requested via API")

	// Set appropriate status code
	statusCode := http.StatusOK
	if !validationResult.Valid {
		statusCode = http.StatusBadRequest
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// ReloadConfigHandler handles configuration reload requests
func (h *ConfigHandlers) ReloadConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if hot-reload is enabled
	if !h.hotReloadManager.IsEnabled() {
		response := ReloadResponse{
			Success:   false,
			Message:   "Hot-reload is not enabled",
			Timestamp: time.Now(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(response)
		return
	}

	h.logger.Info("Configuration reload requested via API")

	// Trigger reload
	event, err := h.hotReloadManager.TriggerReload()

	response := ReloadResponse{
		Success:   err == nil && event.Success,
		Event:     redactReloadEvent(event),
		Timestamp: time.Now(),
	}

	if err != nil {
		response.Message = "Reload failed: " + err.Error()
		h.logger.WithError(err).Error("Configuration reload failed")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	if event.Success {
		response.Message = "Configuration reloaded successfully"
		h.logger.WithFields(logrus.Fields{
			"changes":     len(event.Changes),
			"reload_time": event.ReloadTime,
		}).Info("Configuration reload completed successfully")
	} else {
		response.Message = "Configuration reload completed with errors"
		h.logger.WithField("errors", len(event.Errors)).Warning("Configuration reload completed with errors")
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ReloadStatusHandler handles reload status requests
func (h *ConfigHandlers) ReloadStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := ReloadStatusResponse{
		Enabled:       h.hotReloadManager.IsEnabled(),
		CurrentConfig: "config.yaml", // This should come from the manager
		Message:       "Reload status retrieved successfully",
	}

	if response.Enabled {
		response.Message = "Hot-reload is enabled and monitoring configuration changes"
	} else {
		response.Message = "Hot-reload is disabled"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}
