package stt

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// LanguageSwitcher manages dynamic language detection and switching during calls
type LanguageSwitcher struct {
	logger         *logrus.Logger
	config         *config.DeepgramSTTConfig
	callSessions   map[string]*CallLanguageSession
	sessionMutex   sync.RWMutex
	switchMetrics  *LanguageSwitchingMetrics
	ctx            context.Context
	cancel         context.CancelFunc
}

// CallLanguageSession tracks language state for a specific call
type CallLanguageSession struct {
	CallUUID               string
	CurrentLanguage        string
	LanguageConfidence     float64
	DetectionHistory       []LanguageDetection
	LanguageSwitchCount    int
	LastSwitchTime         time.Time
	SegmentStartTime       time.Time
	StabilityScore         float64
	PreferredLanguages     []string
	FallbackLanguage       string
	SwitchingEnabled       bool
	TransitionSmoothing    bool
	ConsecutiveDetections  map[string]int
	LastDetectionTime      time.Time
	MinSegmentDuration     time.Duration
	SwitchCooldownPeriod   time.Duration
	Mutex                  sync.RWMutex
}

// LanguageDetection represents a single language detection event
type LanguageDetection struct {
	Timestamp       time.Time             `json:"timestamp"`
	Language        string                `json:"language"`
	Confidence      float64               `json:"confidence"`
	AudioDuration   time.Duration         `json:"audio_duration"`
	Provider        string                `json:"provider"`
	Alternatives    []LanguageAlternative `json:"alternatives,omitempty"`
	SegmentID       string                `json:"segment_id"`
	TranscriptText  string                `json:"transcript_text,omitempty"`
	WordCount       int                   `json:"word_count"`
}

// LanguageAlternative represents alternative language detection results
type LanguageAlternative struct {
	Language   string  `json:"language"`
	Confidence float64 `json:"confidence"`
}

// LanguageSwitchingMetrics tracks language switching performance
type LanguageSwitchingMetrics struct {
	TotalSwitches        int64
	SuccessfulSwitches   int64
	FailedSwitches       int64
	AverageConfidence    float64
	LanguageDistribution map[string]int64
	SwitchLatency        time.Duration
	LastUpdate           time.Time
	Mutex                sync.RWMutex
}

// LanguageSwitchingConfig holds configuration for language switching behavior
type LanguageSwitchingConfig struct {
	// Detection parameters
	ConfidenceThreshold     float64       // Minimum confidence to trigger switch
	ConsecutiveDetections   int           // Required consecutive detections before switch
	DetectionWindow         time.Duration // Time window for consecutive detections
	
	// Switching behavior
	MinSegmentDuration      time.Duration // Minimum time before allowing switch
	SwitchCooldownPeriod    time.Duration // Cooldown period between switches
	MaxSwitchesPerCall      int           // Maximum language switches per call
	
	// Stability parameters
	StabilityRequirement    float64       // Required stability score for switching
	StabilityWindow         time.Duration // Time window for stability calculation
	
	// Smoothing and transition
	EnableTransitionSmooth  bool          // Enable smooth transitions
	TransitionBufferSize    int           // Buffer size for transition smoothing
	BlendingFactor          float64       // Factor for blending transition results
}

// NewLanguageSwitcher creates a new language switching service
func NewLanguageSwitcher(logger *logrus.Logger, config *config.DeepgramSTTConfig) *LanguageSwitcher {
	ctx, cancel := context.WithCancel(context.Background())
	
	switcher := &LanguageSwitcher{
		logger:        logger,
		config:        config,
		callSessions:  make(map[string]*CallLanguageSession),
		switchMetrics: &LanguageSwitchingMetrics{
			LanguageDistribution: make(map[string]int64),
		},
		ctx:    ctx,
		cancel: cancel,
	}
	
	// Start monitoring goroutine
	go switcher.monitorSessions()
	
	return switcher
}

// StartCallSession initializes language tracking for a new call
func (ls *LanguageSwitcher) StartCallSession(callUUID string) *CallLanguageSession {
	ls.sessionMutex.Lock()
	defer ls.sessionMutex.Unlock()
	
	session := &CallLanguageSession{
		CallUUID:               callUUID,
		CurrentLanguage:        ls.config.FallbackLanguage,
		LanguageConfidence:     0.0,
		DetectionHistory:       make([]LanguageDetection, 0),
		LanguageSwitchCount:    0,
		LastSwitchTime:         time.Now(),
		SegmentStartTime:       time.Now(),
		StabilityScore:         1.0,
		PreferredLanguages:     ls.config.SupportedLanguages,
		FallbackLanguage:       ls.config.FallbackLanguage,
		SwitchingEnabled:       ls.config.RealtimeLanguageSwitching,
		TransitionSmoothing:    true,
		ConsecutiveDetections:  make(map[string]int),
		LastDetectionTime:      time.Now(),
		MinSegmentDuration:     time.Duration(ls.config.LanguageSwitchingInterval) * time.Second,
		SwitchCooldownPeriod:   5 * time.Second,
	}
	
	ls.callSessions[callUUID] = session
	
	ls.logger.WithFields(logrus.Fields{
		"call_uuid":         callUUID,
		"initial_language":  session.CurrentLanguage,
		"switching_enabled": session.SwitchingEnabled,
		"supported_langs":   len(session.PreferredLanguages),
	}).Info("Started language switching session")
	
	return session
}

// ProcessLanguageDetection handles a new language detection result
func (ls *LanguageSwitcher) ProcessLanguageDetection(callUUID string, detection LanguageDetection) (shouldSwitch bool, newLanguage string, confidence float64) {
	ls.sessionMutex.RLock()
	session, exists := ls.callSessions[callUUID]
	ls.sessionMutex.RUnlock()
	
	if !exists {
		session = ls.StartCallSession(callUUID)
	}
	
	session.Mutex.Lock()
	defer session.Mutex.Unlock()
	
	// Add detection to history
	session.DetectionHistory = append(session.DetectionHistory, detection)
	session.LastDetectionTime = time.Now()
	
	// Limit history size
	if len(session.DetectionHistory) > 100 {
		session.DetectionHistory = session.DetectionHistory[1:]
	}
	
	// Update consecutive detection count
	if detection.Language != session.CurrentLanguage {
		session.ConsecutiveDetections[detection.Language]++
		
		// Reset other language counts
		for lang := range session.ConsecutiveDetections {
			if lang != detection.Language {
				session.ConsecutiveDetections[lang] = 0
			}
		}
	}
	
	// Check if switching conditions are met
	shouldSwitch, newLanguage, confidence = ls.evaluateSwitchingConditions(session, detection)
	
	if shouldSwitch {
		ls.performLanguageSwitch(session, newLanguage, confidence)
	}
	
	return shouldSwitch, newLanguage, confidence
}

// evaluateSwitchingConditions determines if a language switch should occur
func (ls *LanguageSwitcher) evaluateSwitchingConditions(session *CallLanguageSession, detection LanguageDetection) (bool, string, float64) {
	// Check if switching is enabled
	if !session.SwitchingEnabled {
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Check if it's the same language
	if detection.Language == session.CurrentLanguage {
		return false, session.CurrentLanguage, detection.Confidence
	}
	
	// Check confidence threshold
	if detection.Confidence < ls.config.LanguageConfidenceThreshold {
		ls.logger.WithFields(logrus.Fields{
			"call_uuid":           session.CallUUID,
			"detected_language":   detection.Language,
			"confidence":          detection.Confidence,
			"threshold":           ls.config.LanguageConfidenceThreshold,
		}).Debug("Language detection confidence below threshold")
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Check if language is supported
	isSupported := false
	for _, lang := range session.PreferredLanguages {
		if lang == detection.Language {
			isSupported = true
			break
		}
	}
	
	if !isSupported {
		ls.logger.WithFields(logrus.Fields{
			"call_uuid":         session.CallUUID,
			"detected_language": detection.Language,
			"supported_langs":   session.PreferredLanguages,
		}).Debug("Detected language not in supported list")
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Check minimum segment duration
	timeSinceLastSwitch := time.Since(session.LastSwitchTime)
	if timeSinceLastSwitch < session.MinSegmentDuration {
		ls.logger.WithFields(logrus.Fields{
			"call_uuid":             session.CallUUID,
			"time_since_switch":     timeSinceLastSwitch,
			"min_segment_duration":  session.MinSegmentDuration,
		}).Debug("Too soon for language switch")
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Check cooldown period
	if timeSinceLastSwitch < session.SwitchCooldownPeriod {
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Check consecutive detections requirement
	consecutiveCount := session.ConsecutiveDetections[detection.Language]
	requiredConsecutive := 2 // Require at least 2 consecutive detections
	
	if consecutiveCount < requiredConsecutive {
		ls.logger.WithFields(logrus.Fields{
			"call_uuid":         session.CallUUID,
			"language":          detection.Language,
			"consecutive_count": consecutiveCount,
			"required":          requiredConsecutive,
		}).Debug("Insufficient consecutive detections for switch")
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	// Calculate stability score
	stabilityScore := ls.calculateStabilityScore(session, detection.Language)
	
	// Require minimum stability for switching
	minStability := 0.7
	if stabilityScore < minStability {
		ls.logger.WithFields(logrus.Fields{
			"call_uuid":       session.CallUUID,
			"language":        detection.Language,
			"stability_score": stabilityScore,
			"min_stability":   minStability,
		}).Debug("Language stability insufficient for switch")
		return false, session.CurrentLanguage, session.LanguageConfidence
	}
	
	return true, detection.Language, detection.Confidence
}

// performLanguageSwitch executes a language switch
func (ls *LanguageSwitcher) performLanguageSwitch(session *CallLanguageSession, newLanguage string, confidence float64) {
	oldLanguage := session.CurrentLanguage
	
	session.CurrentLanguage = newLanguage
	session.LanguageConfidence = confidence
	session.LanguageSwitchCount++
	session.LastSwitchTime = time.Now()
	session.SegmentStartTime = time.Now()
	
	// Reset consecutive detections
	session.ConsecutiveDetections = make(map[string]int)
	
	// Update metrics
	ls.updateSwitchingMetrics(oldLanguage, newLanguage, true)
	
	ls.logger.WithFields(logrus.Fields{
		"call_uuid":        session.CallUUID,
		"old_language":     oldLanguage,
		"new_language":     newLanguage,
		"confidence":       confidence,
		"switch_count":     session.LanguageSwitchCount,
		"stability_score":  session.StabilityScore,
	}).Info("Language switched successfully")
}

// calculateStabilityScore calculates language stability for switching decisions
func (ls *LanguageSwitcher) calculateStabilityScore(session *CallLanguageSession, targetLanguage string) float64 {
	if len(session.DetectionHistory) < 3 {
		return 0.5 // Neutral stability for insufficient data
	}
	
	// Look at recent detections (last 10 or less)
	recentCount := 10
	if len(session.DetectionHistory) < recentCount {
		recentCount = len(session.DetectionHistory)
	}
	
	recentDetections := session.DetectionHistory[len(session.DetectionHistory)-recentCount:]
	
	// Count detections for target language
	targetCount := 0
	totalConfidence := 0.0
	
	for _, detection := range recentDetections {
		if detection.Language == targetLanguage {
			targetCount++
			totalConfidence += detection.Confidence
		}
	}
	
	if targetCount == 0 {
		return 0.0
	}
	
	// Calculate stability as combination of frequency and confidence
	frequency := float64(targetCount) / float64(recentCount)
	avgConfidence := totalConfidence / float64(targetCount)
	
	// Weighted combination
	stabilityScore := (frequency * 0.6) + (avgConfidence * 0.4)
	
	return stabilityScore
}

// updateSwitchingMetrics updates language switching performance metrics
func (ls *LanguageSwitcher) updateSwitchingMetrics(oldLang, newLang string, success bool) {
	ls.switchMetrics.Mutex.Lock()
	defer ls.switchMetrics.Mutex.Unlock()
	
	ls.switchMetrics.TotalSwitches++
	
	if success {
		ls.switchMetrics.SuccessfulSwitches++
		ls.switchMetrics.LanguageDistribution[newLang]++
	} else {
		ls.switchMetrics.FailedSwitches++
	}
	
	ls.switchMetrics.LastUpdate = time.Now()
}

// GetSessionInfo returns current session information
func (ls *LanguageSwitcher) GetSessionInfo(callUUID string) (*CallLanguageSession, bool) {
	ls.sessionMutex.RLock()
	defer ls.sessionMutex.RUnlock()
	
	session, exists := ls.callSessions[callUUID]
	if !exists {
		return nil, false
	}
	
	// Return a copy to avoid race conditions
	sessionCopy := *session
	return &sessionCopy, true
}

// EndCallSession cleans up language tracking for a completed call
func (ls *LanguageSwitcher) EndCallSession(callUUID string) {
	ls.sessionMutex.Lock()
	defer ls.sessionMutex.Unlock()
	
	session, exists := ls.callSessions[callUUID]
	if !exists {
		return
	}
	
	ls.logger.WithFields(logrus.Fields{
		"call_uuid":      callUUID,
		"final_language": session.CurrentLanguage,
		"switch_count":   session.LanguageSwitchCount,
		"call_duration":  time.Since(session.SegmentStartTime),
	}).Info("Ended language switching session")
	
	delete(ls.callSessions, callUUID)
}

// GetSwitchingMetrics returns current switching performance metrics
func (ls *LanguageSwitcher) GetSwitchingMetrics() *LanguageSwitchingMetrics {
	ls.switchMetrics.Mutex.RLock()
	defer ls.switchMetrics.Mutex.RUnlock()
	
	// Return a copy
	metricsCopy := *ls.switchMetrics
	metricsCopy.LanguageDistribution = make(map[string]int64)
	for k, v := range ls.switchMetrics.LanguageDistribution {
		metricsCopy.LanguageDistribution[k] = v
	}
	
	return &metricsCopy
}

// monitorSessions periodically cleans up stale sessions
func (ls *LanguageSwitcher) monitorSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ls.ctx.Done():
			return
		case <-ticker.C:
			ls.cleanupStaleSessions()
		}
	}
}

// cleanupStaleSessions removes sessions that haven't been active
func (ls *LanguageSwitcher) cleanupStaleSessions() {
	ls.sessionMutex.Lock()
	defer ls.sessionMutex.Unlock()
	
	cutoff := time.Now().Add(-1 * time.Hour) // Remove sessions older than 1 hour
	
	for callUUID, session := range ls.callSessions {
		if session.LastDetectionTime.Before(cutoff) {
			ls.logger.WithField("call_uuid", callUUID).Debug("Cleaning up stale language session")
			delete(ls.callSessions, callUUID)
		}
	}
}

// Shutdown gracefully shuts down the language switcher
func (ls *LanguageSwitcher) Shutdown() {
	ls.cancel()
	
	ls.sessionMutex.Lock()
	defer ls.sessionMutex.Unlock()
	
	ls.logger.WithField("active_sessions", len(ls.callSessions)).Info("Shutting down language switcher")
	ls.callSessions = make(map[string]*CallLanguageSession)
}