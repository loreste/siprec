package warnings

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Severity levels for warnings
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// String returns the string representation of severity
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// Warning represents a system warning
type Warning struct {
	ID            string
	Category      string
	Severity      Severity
	Message       string
	Details       map[string]interface{}
	FirstSeen     time.Time
	LastSeen      time.Time
	Count         int
	Resolved      bool
	ResolvedAt    time.Time
	Suppressed    bool
	SuppressUntil time.Time
	Actions       []string // Recommended actions
}

// WarningCollector collects and manages warnings
type WarningCollector struct {
	logger       *logrus.Logger
	warnings     map[string]*Warning
	mu           sync.RWMutex
	maxWarnings  int
	handlers     []WarningHandler
	suppressions map[string]time.Duration
}

// WarningHandler handles warnings
type WarningHandler interface {
	HandleWarning(warning *Warning)
}

// LogWarningHandler logs warnings
type LogWarningHandler struct {
	logger *logrus.Logger
}

// HandleWarning logs the warning
func (h *LogWarningHandler) HandleWarning(warning *Warning) {
	fields := logrus.Fields{
		"warning_id": warning.ID,
		"category":   warning.Category,
		"severity":   warning.Severity.String(),
		"count":      warning.Count,
	}

	for k, v := range warning.Details {
		fields[k] = v
	}

	switch warning.Severity {
	case SeverityCritical:
		h.logger.WithFields(fields).Error(warning.Message)
	case SeverityHigh:
		h.logger.WithFields(fields).Warn(warning.Message)
	case SeverityMedium:
		h.logger.WithFields(fields).Info(warning.Message)
	default:
		h.logger.WithFields(fields).Debug(warning.Message)
	}
}

// MetricsWarningHandler records warning metrics
type MetricsWarningHandler struct{}

// HandleWarning records warning metrics
func (h *MetricsWarningHandler) HandleWarning(warning *Warning) {
	// Record metrics - would integrate with metrics package
}

// NewWarningCollector creates a new warning collector
func NewWarningCollector(logger *logrus.Logger) *WarningCollector {
	wc := &WarningCollector{
		logger:       logger,
		warnings:     make(map[string]*Warning),
		maxWarnings:  1000,
		handlers:     []WarningHandler{},
		suppressions: make(map[string]time.Duration),
	}

	// Add default handlers
	wc.AddHandler(&LogWarningHandler{logger: logger})
	wc.AddHandler(&MetricsWarningHandler{})

	// Start cleanup goroutine
	go wc.cleanupLoop()

	return wc
}

// AddHandler adds a warning handler
func (wc *WarningCollector) AddHandler(handler WarningHandler) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.handlers = append(wc.handlers, handler)
}

// pruneOldWarnings removes old resolved warnings
func (wc *WarningCollector) pruneOldWarnings() {
	cutoff := time.Now().Add(-24 * time.Hour)

	for id, warning := range wc.warnings {
		if warning.Resolved && warning.ResolvedAt.Before(cutoff) {
			delete(wc.warnings, id)
		}
	}
}

// cleanupLoop periodically cleans up old warnings
func (wc *WarningCollector) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		wc.mu.Lock()
		wc.pruneOldWarnings()

		// Clear expired suppressions
		now := time.Now()
		for id, warning := range wc.warnings {
			if warning.Suppressed && now.After(warning.SuppressUntil) {
				warning.Suppressed = false
				delete(wc.suppressions, id)
			}
		}
		wc.mu.Unlock()
	}
}

// Common warning categories
const (
	CategorySTTProvider   = "stt_provider"
	CategoryAudioQuality  = "audio_quality"
	CategoryResource      = "resource"
	CategoryConfiguration = "configuration"
	CategoryPerformance   = "performance"
	CategorySecurity      = "security"
	CategoryNetwork       = "network"
)

// GlobalCollector is the global warning collector instance
var GlobalCollector *WarningCollector

// InitGlobalCollector initializes the global warning collector
func InitGlobalCollector(logger *logrus.Logger) {
	GlobalCollector = NewWarningCollector(logger)
}
