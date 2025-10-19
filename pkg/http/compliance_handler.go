package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"siprec-server/pkg/compliance"
	"siprec-server/pkg/config"
	"siprec-server/pkg/errors"

	"github.com/sirupsen/logrus"
)

// GDPRService exposes the GDPR export/erase operations.
type GDPRService interface {
	ExportCallData(callID string) (string, error)
	EraseCallData(callID string) error
}

// ComplianceHandler wires compliance APIs (GDPR export/erase, status).
type ComplianceHandler struct {
	logger      *logrus.Logger
	gdprService GDPRService
	config      *config.Config
}

// NewComplianceHandler creates a new compliance handler.
func NewComplianceHandler(logger *logrus.Logger, gdprService GDPRService, cfg *config.Config) *ComplianceHandler {
	return &ComplianceHandler{
		logger:      logger,
		gdprService: gdprService,
		config:      cfg,
	}
}

// RegisterHandlers registers compliance-related endpoints with the server.
func (h *ComplianceHandler) RegisterHandlers(server *Server) {
	if server == nil {
		return
	}

	server.RegisterHandler("/api/compliance/status", h.handleComplianceStatus)

	if h.config != nil && h.config.Compliance.GDPR.Enabled {
		server.RegisterHandler("/api/compliance/gdpr/export", h.handleGDPRExport)
		server.RegisterHandler("/api/compliance/gdpr/erase", h.handleGDPRErase)
	}
}

type gdprRequest struct {
	CallID string `json:"call_id"`
}

func (h *ComplianceHandler) handleGDPRExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.gdprService == nil {
		http.Error(w, "GDPR export service unavailable", http.StatusServiceUnavailable)
		return
	}

	var payload gdprRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.WithError(err).Warn("Failed to decode GDPR export request")
		errors.WriteError(w, errors.Wrap(err, "invalid request payload"))
		return
	}

	callID := strings.TrimSpace(payload.CallID)
	if callID == "" {
		http.Error(w, "call_id is required", http.StatusBadRequest)
		return
	}

	path, err := h.gdprService.ExportCallData(callID)
	if err != nil {
		h.logger.WithError(err).WithField("call_id", callID).Error("GDPR export failed")
		errors.WriteError(w, errors.Wrap(err, "failed to export call data"))
		return
	}

	h.logger.WithField("call_id", callID).Info("GDPR export completed")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"call_id": callID,
		"path":    path,
		"status":  "exported",
	})
}

func (h *ComplianceHandler) handleGDPRErase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.gdprService == nil {
		http.Error(w, "GDPR erase service unavailable", http.StatusServiceUnavailable)
		return
	}

	var payload gdprRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.logger.WithError(err).Warn("Failed to decode GDPR erase request")
		errors.WriteError(w, errors.Wrap(err, "invalid request payload"))
		return
	}

	callID := strings.TrimSpace(payload.CallID)
	if callID == "" {
		http.Error(w, "call_id is required", http.StatusBadRequest)
		return
	}

	if err := h.gdprService.EraseCallData(callID); err != nil {
		h.logger.WithError(err).WithField("call_id", callID).Error("GDPR erase failed")
		errors.WriteError(w, errors.Wrap(err, "failed to erase call data"))
		return
	}

	h.logger.WithField("call_id", callID).Info("GDPR erase completed")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"call_id": callID,
		"status":  "erased",
	})
}

func (h *ComplianceHandler) handleComplianceStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cfg config.Config
	if h.config != nil {
		cfg = *h.config
	}

	status := map[string]interface{}{
		"pci": map[string]interface{}{
			"enabled":                     cfg.Compliance.PCI.Enabled,
			"pii_detection_enabled":       cfg.PII.Enabled,
			"pii_apply_to_transcriptions": cfg.PII.ApplyToTranscriptions,
			"pii_apply_to_recordings":     cfg.PII.ApplyToRecordings,
			"recording_encryption":        cfg.Encryption.EnableRecordingEncryption,
			"tls_enabled":                 cfg.Network.EnableTLS,
			"tls_only":                    cfg.Network.RequireTLSOnly,
			"srtp_enabled":                cfg.Network.EnableSRTP,
			"srtp_required":               cfg.Network.RequireSRTP,
		},
		"gdpr": map[string]interface{}{
			"enabled":           cfg.Compliance.GDPR.Enabled,
			"export_dir":        cfg.Compliance.GDPR.ExportDir,
			"service_available": h.gdprService != nil,
		},
		"audit": map[string]interface{}{
			"tamper_proof": cfg.Compliance.Audit.TamperProof,
			"log_path":     cfg.Compliance.Audit.LogPath,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Ensure the concrete GDPR service satisfies the interface at compile time.
var _ GDPRService = (*compliance.GDPRService)(nil)
