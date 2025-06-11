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
	Valid       bool                    `json:"valid"`
	Errors      []config.ValidationError   `json:"errors,omitempty"`
	Warnings    []config.ValidationWarning `json:"warnings,omitempty"`
	Summary     string                  `json:"summary"`
	Timestamp   time.Time               `json:"timestamp"`
}

// ReloadResponse represents a configuration reload response
type ReloadResponse struct {
	Success     bool                   `json:"success"`
	Event       *config.ReloadEvent    `json:"event,omitempty"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
}

// ReloadStatusResponse represents reload status
type ReloadStatusResponse struct {
	Enabled      bool      `json:"enabled"`
	LastReload   time.Time `json:"last_reload,omitempty"`
	CurrentConfig string   `json:"current_config_path"`
	Message      string    `json:"message,omitempty"`
}

// GetConfigHandler handles configuration retrieval requests
func (h *ConfigHandlers) GetConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current configuration
	currentConfig := h.hotReloadManager.GetCurrentConfig()

	response := ConfigResponse{
		Config:    currentConfig,
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
	if err := json.NewDecoder(r.Body).Decode(&configToValidate); err != nil {
		h.logger.WithError(err).Error("Failed to decode configuration for validation")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Create validator and validate
	validator := config.NewConfigValidator(h.logger)
	validationResult := validator.ValidateConfig(&configToValidate)

	response := ValidationResponse{
		Valid:     validationResult.Valid,
		Errors:    validationResult.Errors,
		Warnings:  validationResult.Warnings,
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
		Event:     event,
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

// GetConfigValidationRules returns the validation rules for configuration
func (h *ConfigHandlers) GetConfigValidationRules() map[string]interface{} {
	return map[string]interface{}{
		"sip_ports": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 65535},
			"minItems":    1,
			"description": "SIP ports for listening (TCP/UDP)",
		},
		"http_port": map[string]interface{}{
			"type":        "integer",
			"minimum":     1,
			"maximum":     65535,
			"description": "HTTP server port",
		},
		"rtp_port_min": map[string]interface{}{
			"type":        "integer",
			"minimum":     1024,
			"maximum":     65535,
			"description": "Minimum RTP port range",
		},
		"rtp_port_max": map[string]interface{}{
			"type":        "integer",
			"minimum":     1024,
			"maximum":     65535,
			"description": "Maximum RTP port range",
		},
		"external_ip": map[string]interface{}{
			"type":        "string",
			"pattern":     "^(auto|([0-9]{1,3}\\.){3}[0-9]{1,3})$",
			"description": "External IP address or 'auto'",
		},
		"recording_dir": map[string]interface{}{
			"type":        "string",
			"minLength":   1,
			"description": "Directory for storing recordings",
		},
		"stt_vendors": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string", "enum": []string{"google", "azure", "aws", "deepgram", "openai", "mock"}},
			"description": "Supported STT vendors",
		},
		"log_level": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"},
			"description": "Logging level",
		},
		"max_concurrent_calls": map[string]interface{}{
			"type":        "integer",
			"minimum":     1,
			"maximum":     100000,
			"description": "Maximum number of concurrent calls",
		},
	}
}

// HealthCheckConfig returns configuration system health information
func (h *ConfigHandlers) HealthCheckConfig() map[string]interface{} {
	health := map[string]interface{}{
		"hot_reload_enabled": h.hotReloadManager.IsEnabled(),
		"status":            "healthy",
		"message":           "Configuration system operational",
	}

	// Add more health metrics as needed
	if !h.hotReloadManager.IsEnabled() {
		health["status"] = "degraded"
		health["message"] = "Hot-reload disabled"
	}

	return health
}