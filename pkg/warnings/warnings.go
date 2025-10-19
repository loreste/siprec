package warnings

import (
	"fmt"
	"strings"
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
	ID          string
	Category    string
	Severity    Severity
	Message     string
	Details     map[string]interface{}
	FirstSeen   time.Time
	LastSeen    time.Time
	Count       int
	Resolved    bool
	ResolvedAt  time.Time
	Suppressed  bool
	SuppressUntil time.Time
	Actions     []string // Recommended actions
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

// AddWarning adds a warning
func (wc *WarningCollector) AddWarning(category string, severity Severity, message string, details map[string]interface{}) string {
	warningID := wc.generateWarningID(category, message)
	
	wc.mu.Lock()
	defer wc.mu.Unlock()
	
	// Check if suppressed
	if suppressUntil, exists := wc.suppressions[warningID]; exists {
		if time.Now().Before(time.Now().Add(suppressUntil)) {
			return warningID
		}
	}
	
	if existing, exists := wc.warnings[warningID]; exists {
		// Update existing warning
		existing.Count++
		existing.LastSeen = time.Now()
		existing.Severity = severity // Update severity if changed
		existing.Details = details
		
		// Notify handlers only if not suppressed
		if !existing.Suppressed {
			for _, handler := range wc.handlers {
				handler.HandleWarning(existing)
			}
		}
	} else {
		// Create new warning
		warning := &Warning{
			ID:        warningID,
			Category:  category,
			Severity:  severity,
			Message:   message,
			Details:   details,
			FirstSeen: time.Now(),
			LastSeen:  time.Now(),
			Count:     1,
			Resolved:  false,
			Actions:   wc.getRecommendedActions(category, severity),
		}
		
		// Check max warnings
		if len(wc.warnings) >= wc.maxWarnings {
			wc.pruneOldWarnings()
		}
		
		wc.warnings[warningID] = warning
		
		// Notify handlers
		for _, handler := range wc.handlers {
			handler.HandleWarning(warning)
		}
	}
	
	return warningID
}

// ResolveWarning marks a warning as resolved
func (wc *WarningCollector) ResolveWarning(warningID string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	
	if warning, exists := wc.warnings[warningID]; exists {
		warning.Resolved = true
		warning.ResolvedAt = time.Now()
		
		wc.logger.WithFields(logrus.Fields{
			"warning_id": warningID,
			"category":   warning.Category,
		}).Info("Warning resolved")
	}
}

// SuppressWarning suppresses a warning for a duration
func (wc *WarningCollector) SuppressWarning(warningID string, duration time.Duration) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	
	wc.suppressions[warningID] = duration
	
	if warning, exists := wc.warnings[warningID]; exists {
		warning.Suppressed = true
		warning.SuppressUntil = time.Now().Add(duration)
	}
}

// GetWarning returns a specific warning
func (wc *WarningCollector) GetWarning(warningID string) (*Warning, bool) {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	
	warning, exists := wc.warnings[warningID]
	if !exists {
		return nil, false
	}
	
	// Return a copy
	warningCopy := *warning
	return &warningCopy, true
}

// GetActiveWarnings returns all active (unresolved) warnings
func (wc *WarningCollector) GetActiveWarnings() []*Warning {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	
	var active []*Warning
	for _, warning := range wc.warnings {
		if !warning.Resolved && !warning.Suppressed {
			warningCopy := *warning
			active = append(active, &warningCopy)
		}
	}
	
	return active
}

// GetWarningsByCategory returns warnings for a specific category
func (wc *WarningCollector) GetWarningsByCategory(category string) []*Warning {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	
	var categoryWarnings []*Warning
	for _, warning := range wc.warnings {
		if warning.Category == category {
			warningCopy := *warning
			categoryWarnings = append(categoryWarnings, &warningCopy)
		}
	}
	
	return categoryWarnings
}

// GetWarningsBySeverity returns warnings of a specific severity or higher
func (wc *WarningCollector) GetWarningsBySeverity(minSeverity Severity) []*Warning {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	
	var severityWarnings []*Warning
	for _, warning := range wc.warnings {
		if warning.Severity >= minSeverity && !warning.Resolved {
			warningCopy := *warning
			severityWarnings = append(severityWarnings, &warningCopy)
		}
	}
	
	return severityWarnings
}

// GetStatistics returns warning statistics
func (wc *WarningCollector) GetStatistics() map[string]interface{} {
	wc.mu.RLock()
	defer wc.mu.RUnlock()
	
	stats := make(map[string]interface{})
	
	total := len(wc.warnings)
	active := 0
	resolved := 0
	suppressed := 0
	
	severityCounts := make(map[string]int)
	categoryCounts := make(map[string]int)
	
	for _, warning := range wc.warnings {
		if warning.Resolved {
			resolved++
		} else if warning.Suppressed {
			suppressed++
		} else {
			active++
		}
		
		severityStr := warning.Severity.String()
		severityCounts[severityStr]++
		categoryCounts[warning.Category]++
	}
	
	stats["total"] = total
	stats["active"] = active
	stats["resolved"] = resolved
	stats["suppressed"] = suppressed
	stats["by_severity"] = severityCounts
	stats["by_category"] = categoryCounts
	
	return stats
}

// generateWarningID generates a unique warning ID
func (wc *WarningCollector) generateWarningID(category, message string) string {
	// Create a deterministic ID based on category and message
	key := fmt.Sprintf("%s:%s", category, strings.ToLower(message))
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, ":", "_")
	return key
}

// getRecommendedActions returns recommended actions for a warning
func (wc *WarningCollector) getRecommendedActions(category string, severity Severity) []string {
	actions := []string{}
	
	switch category {
	case "stt_provider":
		actions = append(actions, "Check STT provider configuration")
		actions = append(actions, "Verify API credentials")
		if severity >= SeverityHigh {
			actions = append(actions, "Consider enabling fallback providers")
		}
		
	case "audio_quality":
		actions = append(actions, "Review audio enhancement settings")
		actions = append(actions, "Check network conditions")
		
	case "resource":
		actions = append(actions, "Monitor system resources")
		if severity >= SeverityHigh {
			actions = append(actions, "Consider scaling resources")
		}
		
	case "configuration":
		actions = append(actions, "Review configuration files")
		actions = append(actions, "Check environment variables")
		
	case "performance":
		actions = append(actions, "Review performance metrics")
		if severity >= SeverityMedium {
			actions = append(actions, "Consider performance tuning")
		}
	}
	
	if severity == SeverityCritical {
		actions = append(actions, "Immediate attention required")
	}
	
	return actions
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

// AddGlobalWarning adds a warning to the global collector
func AddGlobalWarning(category string, severity Severity, message string, details map[string]interface{}) string {
	if GlobalCollector == nil {
		return ""
	}
	return GlobalCollector.AddWarning(category, severity, message, details)
}

// ResolveGlobalWarning resolves a warning in the global collector
func ResolveGlobalWarning(warningID string) {
	if GlobalCollector != nil {
		GlobalCollector.ResolveWarning(warningID)
	}
}