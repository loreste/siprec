package stt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// LanguagePersistenceService manages language preferences and history across call segments
type LanguagePersistenceService struct {
	logger            *logrus.Logger
	callProfiles      map[string]*CallLanguageProfile
	globalProfile     *GlobalLanguageProfile
	persistenceConfig PersistenceConfig
	profileMutex      sync.RWMutex
	redisClient       redis.UniversalClient
	redisKeyPrefix    string
	redisTimeout      time.Duration
	fileDirectory     string
	fileMutex         sync.Mutex
}

// CallLanguageProfile tracks language preferences and patterns for a specific call
type CallLanguageProfile struct {
	CallUUID           string                   `json:"call_uuid"`
	CallerID           string                   `json:"caller_id,omitempty"`
	PreferredLanguages []LanguagePreference     `json:"preferred_languages"`
	LanguageHistory    []LanguageHistoryEntry   `json:"language_history"`
	PrimaryLanguage    string                   `json:"primary_language"`
	SecondaryLanguages []string                 `json:"secondary_languages"`
	SwitchingPattern   LanguageSwitchingPattern `json:"switching_pattern"`
	QualityMetrics     LanguageQualityMetrics   `json:"quality_metrics"`
	CallDuration       time.Duration            `json:"call_duration"`
	LastActivity       time.Time                `json:"last_activity"`
	SegmentCount       int                      `json:"segment_count"`
	AdaptationEnabled  bool                     `json:"adaptation_enabled"`
	UserPreferences    UserLanguagePreferences  `json:"user_preferences"`
}

// LanguagePreference represents a language preference with confidence and usage stats
type LanguagePreference struct {
	Language           string        `json:"language"`
	Confidence         float64       `json:"confidence"`
	UsageFrequency     float64       `json:"usage_frequency"`
	QualityScore       float64       `json:"quality_score"`
	LastUsed           time.Time     `json:"last_used"`
	TotalUsageTime     time.Duration `json:"total_usage_time"`
	SuccessfulSwitches int           `json:"successful_switches"`
	FailedSwitches     int           `json:"failed_switches"`
	PreferenceSource   string        `json:"preference_source"` // "detected", "user_specified", "learned"
}

// LanguageHistoryEntry tracks language usage history within a call
type LanguageHistoryEntry struct {
	Timestamp       time.Time     `json:"timestamp"`
	Language        string        `json:"language"`
	Duration        time.Duration `json:"duration"`
	ConfidenceAvg   float64       `json:"confidence_avg"`
	WordCount       int           `json:"word_count"`
	SegmentCount    int           `json:"segment_count"`
	SwitchReason    string        `json:"switch_reason"`
	QualityScore    float64       `json:"quality_score"`
	ContextualScore float64       `json:"contextual_score"`
}

// LanguageSwitchingPattern analyzes switching behavior patterns
type LanguageSwitchingPattern struct {
	AverageSwitchInterval time.Duration      `json:"avg_switch_interval"`
	MaxSegmentDuration    time.Duration      `json:"max_segment_duration"`
	MinSegmentDuration    time.Duration      `json:"min_segment_duration"`
	SwitchFrequency       float64            `json:"switch_frequency"` // switches per minute
	PredictableSwitches   int                `json:"predictable_switches"`
	UnexpectedSwitches    int                `json:"unexpected_switches"`
	DominantLanguage      string             `json:"dominant_language"`
	LanguageDistribution  map[string]float64 `json:"language_distribution"`
	ContextualTriggers    []string           `json:"contextual_triggers"`
}

// LanguageQualityMetrics tracks quality metrics for language detection and switching
type LanguageQualityMetrics struct {
	OverallConfidence   float64            `json:"overall_confidence"`
	LanguageConfidences map[string]float64 `json:"language_confidences"`
	SwitchAccuracy      float64            `json:"switch_accuracy"`
	FalsePositiveRate   float64            `json:"false_positive_rate"`
	StabilityScore      float64            `json:"stability_score"`
	ConsistencyScore    float64            `json:"consistency_score"`
	LatencyMetrics      LatencyMetrics     `json:"latency_metrics"`
	ErrorRate           float64            `json:"error_rate"`
}

// LatencyMetrics tracks timing performance for language processing
type LatencyMetrics struct {
	AvgDetectionLatency time.Duration `json:"avg_detection_latency"`
	AvgSwitchLatency    time.Duration `json:"avg_switch_latency"`
	MaxDetectionLatency time.Duration `json:"max_detection_latency"`
	LatencyVariability  time.Duration `json:"latency_variability"`
}

// UserLanguagePreferences represents user-specified language preferences
type UserLanguagePreferences struct {
	PreferredLanguages    []string  `json:"preferred_languages"`
	FallbackLanguage      string    `json:"fallback_language"`
	SwitchingSensitivity  string    `json:"switching_sensitivity"` // "low", "medium", "high"
	AutoDetectionEnabled  bool      `json:"auto_detection_enabled"`
	ManualOverrideEnabled bool      `json:"manual_override_enabled"`
	LearningEnabled       bool      `json:"learning_enabled"`
	PreferenceUpdatedAt   time.Time `json:"preference_updated_at"`
}

// GlobalLanguageProfile tracks system-wide language usage patterns
type GlobalLanguageProfile struct {
	TotalCalls           int                 `json:"total_calls"`
	LanguagePopularity   map[string]int      `json:"language_popularity"`
	SuccessfulSwitches   int                 `json:"successful_switches"`
	FailedSwitches       int                 `json:"failed_switches"`
	AverageQuality       map[string]float64  `json:"average_quality"`
	CommonSwitchPatterns []SwitchPattern     `json:"common_switch_patterns"`
	LastUpdated          time.Time           `json:"last_updated"`
	ConfigurationTuning  ConfigurationTuning `json:"configuration_tuning"`
}

// SwitchPattern represents a common language switching pattern
type SwitchPattern struct {
	FromLanguage    string        `json:"from_language"`
	ToLanguage      string        `json:"to_language"`
	Frequency       int           `json:"frequency"`
	AvgConfidence   float64       `json:"avg_confidence"`
	SuccessRate     float64       `json:"success_rate"`
	Context         []string      `json:"context"`
	TypicalDuration time.Duration `json:"typical_duration"`
}

// ConfigurationTuning holds dynamically tuned configuration parameters
type ConfigurationTuning struct {
	OptimalThresholds      map[string]float64                `json:"optimal_thresholds"`
	LanguageSpecificTuning map[string]LanguageSpecificConfig `json:"language_specific_tuning"`
	AdaptiveParameters     AdaptiveParameters                `json:"adaptive_parameters"`
	LastTuningUpdate       time.Time                         `json:"last_tuning_update"`
}

// LanguageSpecificConfig holds configuration specific to a language
type LanguageSpecificConfig struct {
	ConfidenceThreshold  float64                `json:"confidence_threshold"`
	SwitchCooldown       time.Duration          `json:"switch_cooldown"`
	StabilityRequirement float64                `json:"stability_requirement"`
	OptimalModelSettings map[string]interface{} `json:"optimal_model_settings"`
}

// AdaptiveParameters holds parameters that adapt based on usage patterns
type AdaptiveParameters struct {
	DynamicThresholds     bool          `json:"dynamic_thresholds"`
	LearningRate          float64       `json:"learning_rate"`
	AdaptationInterval    time.Duration `json:"adaptation_interval"`
	MinSampleSize         int           `json:"min_sample_size"`
	ConfidenceDecayFactor float64       `json:"confidence_decay_factor"`
}

// PersistenceConfig configures language persistence behavior
type PersistenceConfig struct {
	EnablePersistence    bool          `json:"enable_persistence"`
	ProfileRetentionTime time.Duration `json:"profile_retention_time"`
	HistoryMaxEntries    int           `json:"history_max_entries"`
	AdaptationEnabled    bool          `json:"adaptation_enabled"`
	LearningEnabled      bool          `json:"learning_enabled"`
	MinCallDuration      time.Duration `json:"min_call_duration"`
	QualityThreshold     float64       `json:"quality_threshold"`
	UpdateInterval       time.Duration `json:"update_interval"`
	PersistenceStorage   string        `json:"persistence_storage"` // "memory", "redis", "file"
	RedisKeyPrefix       string        `json:"redis_key_prefix"`
	RedisWriteTimeout    time.Duration `json:"redis_write_timeout"`
	FileDirectory        string        `json:"file_directory"`
}

// NewLanguagePersistenceService creates a new language persistence service
func NewLanguagePersistenceService(logger *logrus.Logger) *LanguagePersistenceService {
	return &LanguagePersistenceService{
		logger:       logger,
		callProfiles: make(map[string]*CallLanguageProfile),
		globalProfile: &GlobalLanguageProfile{
			LanguagePopularity:   make(map[string]int),
			AverageQuality:       make(map[string]float64),
			CommonSwitchPatterns: make([]SwitchPattern, 0),
			LastUpdated:          time.Now(),
			ConfigurationTuning: ConfigurationTuning{
				OptimalThresholds:      make(map[string]float64),
				LanguageSpecificTuning: make(map[string]LanguageSpecificConfig),
				AdaptiveParameters: AdaptiveParameters{
					DynamicThresholds:     true,
					LearningRate:          0.1,
					AdaptationInterval:    time.Hour,
					MinSampleSize:         10,
					ConfidenceDecayFactor: 0.95,
				},
			},
		},
		persistenceConfig: PersistenceConfig{
			EnablePersistence:    true,
			ProfileRetentionTime: 24 * time.Hour,
			HistoryMaxEntries:    100,
			AdaptationEnabled:    true,
			LearningEnabled:      true,
			MinCallDuration:      30 * time.Second,
			QualityThreshold:     0.6,
			UpdateInterval:       5 * time.Minute,
			PersistenceStorage:   "memory",
			RedisKeyPrefix:       "siprec:lang_profiles",
			RedisWriteTimeout:    5 * time.Second,
			FileDirectory:        filepath.Join("data", "language_profiles"),
		},
		redisKeyPrefix: "siprec:lang_profiles",
		redisTimeout:   5 * time.Second,
		fileDirectory:  filepath.Join("data", "language_profiles"),
	}
}

// SetPersistenceConfig updates persistence configuration and derived options.
func (lps *LanguagePersistenceService) SetPersistenceConfig(config PersistenceConfig) {
	lps.persistenceConfig = config

	if config.RedisKeyPrefix != "" {
		lps.redisKeyPrefix = config.RedisKeyPrefix
	}

	if config.RedisWriteTimeout > 0 {
		lps.redisTimeout = config.RedisWriteTimeout
	}

	if config.FileDirectory != "" {
		lps.fileDirectory = filepath.Clean(config.FileDirectory)
	}
}

// ConfigureRedis enables Redis-backed persistence for language profiles.
func (lps *LanguagePersistenceService) ConfigureRedis(client redis.UniversalClient, keyPrefix string, timeout time.Duration) {
	lps.redisClient = client

	if keyPrefix != "" {
		lps.redisKeyPrefix = strings.TrimSuffix(keyPrefix, ":")
	}

	if timeout > 0 {
		lps.redisTimeout = timeout
	}
}

// ConfigureFileStorage overrides the target directory for file persistence.
func (lps *LanguagePersistenceService) ConfigureFileStorage(directory string) {
	if directory == "" {
		return
	}

	lps.fileDirectory = filepath.Clean(directory)
}

// StartCallProfile creates or retrieves a language profile for a call
func (lps *LanguagePersistenceService) StartCallProfile(callUUID, callerID string, userPreferences *UserLanguagePreferences) *CallLanguageProfile {
	lps.profileMutex.Lock()
	defer lps.profileMutex.Unlock()

	profile := &CallLanguageProfile{
		CallUUID:           callUUID,
		CallerID:           callerID,
		PreferredLanguages: make([]LanguagePreference, 0),
		LanguageHistory:    make([]LanguageHistoryEntry, 0),
		SecondaryLanguages: make([]string, 0),
		SwitchingPattern: LanguageSwitchingPattern{
			LanguageDistribution: make(map[string]float64),
			ContextualTriggers:   make([]string, 0),
		},
		QualityMetrics: LanguageQualityMetrics{
			LanguageConfidences: make(map[string]float64),
			LatencyMetrics:      LatencyMetrics{},
		},
		LastActivity:      time.Now(),
		AdaptationEnabled: lps.persistenceConfig.AdaptationEnabled,
	}

	// Apply user preferences if provided
	if userPreferences != nil {
		profile.UserPreferences = *userPreferences

		// Initialize preferred languages from user preferences
		for _, lang := range userPreferences.PreferredLanguages {
			profile.PreferredLanguages = append(profile.PreferredLanguages, LanguagePreference{
				Language:         lang,
				Confidence:       0.8, // Default confidence for user-specified languages
				PreferenceSource: "user_specified",
				LastUsed:         time.Now(),
			})
		}

		if userPreferences.FallbackLanguage != "" {
			profile.PrimaryLanguage = userPreferences.FallbackLanguage
		}
	}

	// Load historical preferences for this caller if available
	if callerID != "" {
		lps.loadHistoricalPreferences(profile, callerID)
	}

	lps.callProfiles[callUUID] = profile

	lps.logger.WithFields(logrus.Fields{
		"call_uuid":           callUUID,
		"caller_id":           callerID,
		"preferred_languages": len(profile.PreferredLanguages),
		"adaptation_enabled":  profile.AdaptationEnabled,
	}).Info("Started language persistence profile")

	return profile
}

// UpdateLanguageUsage updates language usage statistics for a call
func (lps *LanguagePersistenceService) UpdateLanguageUsage(callUUID string, usage LanguageUsageUpdate) {
	lps.profileMutex.RLock()
	profile, exists := lps.callProfiles[callUUID]
	lps.profileMutex.RUnlock()

	if !exists {
		return
	}

	profile.LastActivity = time.Now()
	profile.SegmentCount++

	// Update language history
	historyEntry := LanguageHistoryEntry{
		Timestamp:       usage.Timestamp,
		Language:        usage.Language,
		Duration:        usage.Duration,
		ConfidenceAvg:   usage.Confidence,
		WordCount:       usage.WordCount,
		SegmentCount:    1,
		SwitchReason:    usage.SwitchReason,
		QualityScore:    usage.QualityScore,
		ContextualScore: usage.ContextualScore,
	}

	profile.LanguageHistory = append(profile.LanguageHistory, historyEntry)

	// Limit history size
	if len(profile.LanguageHistory) > lps.persistenceConfig.HistoryMaxEntries {
		profile.LanguageHistory = profile.LanguageHistory[1:]
	}

	// Update language preferences
	lps.updateLanguagePreference(profile, usage)

	// Update switching patterns
	lps.updateSwitchingPattern(profile, usage)

	// Update quality metrics
	lps.updateQualityMetrics(profile, usage)

	lps.logger.WithFields(logrus.Fields{
		"call_uuid":     callUUID,
		"language":      usage.Language,
		"confidence":    usage.Confidence,
		"segment_count": profile.SegmentCount,
	}).Debug("Updated language usage")
}

// LanguageUsageUpdate represents an update to language usage statistics
type LanguageUsageUpdate struct {
	Timestamp       time.Time
	Language        string
	Duration        time.Duration
	Confidence      float64
	WordCount       int
	SwitchReason    string
	QualityScore    float64
	ContextualScore float64
	Latency         time.Duration
}

// updateLanguagePreference updates the preference for a specific language
func (lps *LanguagePersistenceService) updateLanguagePreference(profile *CallLanguageProfile, usage LanguageUsageUpdate) {
	// Find existing preference or create new one
	var preference *LanguagePreference
	for i := range profile.PreferredLanguages {
		if profile.PreferredLanguages[i].Language == usage.Language {
			preference = &profile.PreferredLanguages[i]
			break
		}
	}

	if preference == nil {
		// Create new preference
		newPreference := LanguagePreference{
			Language:         usage.Language,
			Confidence:       usage.Confidence,
			PreferenceSource: "detected",
			LastUsed:         usage.Timestamp,
		}
		profile.PreferredLanguages = append(profile.PreferredLanguages, newPreference)
		preference = &profile.PreferredLanguages[len(profile.PreferredLanguages)-1]
	}

	// Update preference statistics with exponential moving average
	alpha := 0.3 // Learning rate
	preference.Confidence = (1-alpha)*preference.Confidence + alpha*usage.Confidence
	preference.QualityScore = (1-alpha)*preference.QualityScore + alpha*usage.QualityScore
	preference.UsageFrequency += 1.0
	preference.TotalUsageTime += usage.Duration
	preference.LastUsed = usage.Timestamp
}

// updateSwitchingPattern updates switching behavior patterns
func (lps *LanguagePersistenceService) updateSwitchingPattern(profile *CallLanguageProfile, usage LanguageUsageUpdate) {
	// Update language distribution
	currentDist := profile.SwitchingPattern.LanguageDistribution[usage.Language]
	profile.SwitchingPattern.LanguageDistribution[usage.Language] = currentDist + usage.Duration.Seconds()

	// Calculate switch frequency
	if len(profile.LanguageHistory) > 1 {
		totalDuration := time.Since(profile.LanguageHistory[0].Timestamp)
		switches := lps.countLanguageSwitches(profile.LanguageHistory)
		profile.SwitchingPattern.SwitchFrequency = float64(switches) / totalDuration.Minutes()
	}

	// Determine dominant language
	maxDuration := 0.0
	dominantLang := ""
	for lang, duration := range profile.SwitchingPattern.LanguageDistribution {
		if duration > maxDuration {
			maxDuration = duration
			dominantLang = lang
		}
	}
	profile.SwitchingPattern.DominantLanguage = dominantLang
}

// updateQualityMetrics updates quality metrics for the call
func (lps *LanguagePersistenceService) updateQualityMetrics(profile *CallLanguageProfile, usage LanguageUsageUpdate) {
	// Update language-specific confidence
	currentConf := profile.QualityMetrics.LanguageConfidences[usage.Language]
	alpha := 0.2
	profile.QualityMetrics.LanguageConfidences[usage.Language] = (1-alpha)*currentConf + alpha*usage.Confidence

	// Update overall confidence
	totalConf := 0.0
	count := 0
	for _, conf := range profile.QualityMetrics.LanguageConfidences {
		totalConf += conf
		count++
	}
	if count > 0 {
		profile.QualityMetrics.OverallConfidence = totalConf / float64(count)
	}

	// Update latency metrics
	if usage.Latency > 0 {
		currentAvg := profile.QualityMetrics.LatencyMetrics.AvgDetectionLatency
		profile.QualityMetrics.LatencyMetrics.AvgDetectionLatency =
			time.Duration((1-alpha)*float64(currentAvg) + alpha*float64(usage.Latency))

		if usage.Latency > profile.QualityMetrics.LatencyMetrics.MaxDetectionLatency {
			profile.QualityMetrics.LatencyMetrics.MaxDetectionLatency = usage.Latency
		}
	}
}

// countLanguageSwitches counts the number of language switches in history
func (lps *LanguagePersistenceService) countLanguageSwitches(history []LanguageHistoryEntry) int {
	if len(history) < 2 {
		return 0
	}

	switches := 0
	for i := 1; i < len(history); i++ {
		if history[i].Language != history[i-1].Language {
			switches++
		}
	}

	return switches
}

// GetOptimalLanguageForCall returns the recommended language settings for a call
func (lps *LanguagePersistenceService) GetOptimalLanguageForCall(callUUID string) *OptimalLanguageSettings {
	lps.profileMutex.RLock()
	profile, exists := lps.callProfiles[callUUID]
	lps.profileMutex.RUnlock()

	if !exists {
		return nil
	}

	settings := &OptimalLanguageSettings{
		PrimaryLanguage:     profile.PrimaryLanguage,
		SecondaryLanguages:  profile.SecondaryLanguages,
		ConfidenceThreshold: lps.calculateOptimalThreshold(profile),
		SwitchCooldown:      lps.calculateOptimalCooldown(profile),
		PreferredOrder:      lps.getLanguagePreferenceOrder(profile),
		AdaptiveSettings:    lps.getAdaptiveSettings(profile),
	}

	return settings
}

// OptimalLanguageSettings represents optimal language configuration for a call
type OptimalLanguageSettings struct {
	PrimaryLanguage     string                 `json:"primary_language"`
	SecondaryLanguages  []string               `json:"secondary_languages"`
	ConfidenceThreshold float64                `json:"confidence_threshold"`
	SwitchCooldown      time.Duration          `json:"switch_cooldown"`
	PreferredOrder      []string               `json:"preferred_order"`
	AdaptiveSettings    map[string]interface{} `json:"adaptive_settings"`
}

// calculateOptimalThreshold calculates the optimal confidence threshold for a call
func (lps *LanguagePersistenceService) calculateOptimalThreshold(profile *CallLanguageProfile) float64 {
	if !profile.AdaptationEnabled {
		return 0.7 // Default threshold
	}

	// Analyze historical performance to determine optimal threshold
	successfulSwitches := 0
	totalSwitches := 0

	for _, pref := range profile.PreferredLanguages {
		successfulSwitches += pref.SuccessfulSwitches
		totalSwitches += pref.SuccessfulSwitches + pref.FailedSwitches
	}

	if totalSwitches == 0 {
		return 0.7
	}

	successRate := float64(successfulSwitches) / float64(totalSwitches)

	// Adjust threshold based on success rate
	if successRate > 0.9 {
		return 0.6 // Lower threshold for high success rate
	} else if successRate < 0.7 {
		return 0.8 // Higher threshold for low success rate
	}

	return 0.7 // Default
}

// calculateOptimalCooldown calculates the optimal switch cooldown for a call
func (lps *LanguagePersistenceService) calculateOptimalCooldown(profile *CallLanguageProfile) time.Duration {
	avgInterval := profile.SwitchingPattern.AverageSwitchInterval

	if avgInterval == 0 {
		return 3 * time.Second // Default cooldown
	}

	// Set cooldown to be 1/3 of average switch interval, with bounds
	cooldown := avgInterval / 3
	if cooldown < 1*time.Second {
		cooldown = 1 * time.Second
	} else if cooldown > 10*time.Second {
		cooldown = 10 * time.Second
	}

	return cooldown
}

// getLanguagePreferenceOrder returns languages ordered by preference
func (lps *LanguagePersistenceService) getLanguagePreferenceOrder(profile *CallLanguageProfile) []string {
	// Sort languages by combined score of confidence, quality, and usage
	type languageScore struct {
		language string
		score    float64
	}

	scores := make([]languageScore, 0)

	for _, pref := range profile.PreferredLanguages {
		// Calculate combined score
		score := (pref.Confidence * 0.4) +
			(pref.QualityScore * 0.3) +
			(pref.UsageFrequency * 0.2) +
			(float64(pref.TotalUsageTime.Seconds()) * 0.1)

		scores = append(scores, languageScore{
			language: pref.Language,
			score:    score,
		})
	}

	// Sort by score (descending)
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	// Extract language order
	order := make([]string, len(scores))
	for i, score := range scores {
		order[i] = score.language
	}

	return order
}

// getAdaptiveSettings returns adaptive configuration settings
func (lps *LanguagePersistenceService) getAdaptiveSettings(profile *CallLanguageProfile) map[string]interface{} {
	settings := make(map[string]interface{})

	settings["stability_requirement"] = lps.calculateStabilityRequirement(profile)
	settings["detection_sensitivity"] = lps.calculateDetectionSensitivity(profile)
	settings["transition_smoothing"] = profile.QualityMetrics.StabilityScore < 0.8

	return settings
}

// calculateStabilityRequirement calculates required stability for switching
func (lps *LanguagePersistenceService) calculateStabilityRequirement(profile *CallLanguageProfile) float64 {
	// Higher stability requirement if we've had many failed switches
	errorRate := profile.QualityMetrics.ErrorRate

	if errorRate > 0.2 {
		return 0.8 // High stability requirement
	} else if errorRate < 0.1 {
		return 0.6 // Lower stability requirement
	}

	return 0.7 // Default
}

// calculateDetectionSensitivity calculates optimal detection sensitivity
func (lps *LanguagePersistenceService) calculateDetectionSensitivity(profile *CallLanguageProfile) string {
	switchFreq := profile.SwitchingPattern.SwitchFrequency

	if switchFreq > 2.0 { // More than 2 switches per minute
		return "low" // Reduce sensitivity to avoid over-switching
	} else if switchFreq < 0.5 { // Less than 0.5 switches per minute
		return "high" // Increase sensitivity to catch language changes
	}

	return "medium"
}

// loadHistoricalPreferences loads the most recent stored profile for a caller
// from the configured persistence backend and seeds the new call profile with
// the previously learned language preferences.
func (lps *LanguagePersistenceService) loadHistoricalPreferences(profile *CallLanguageProfile, callerID string) {
	if !lps.persistenceConfig.EnablePersistence {
		return
	}

	var historical *CallLanguageProfile
	var err error

	switch lps.persistenceConfig.PersistenceStorage {
	case "redis":
		historical, err = lps.loadCallerProfileRedis(callerID)
	case "file":
		historical, err = lps.loadCallerProfileFile(callerID)
	default:
		// Memory-only storage: profiles are removed at call end, so there is
		// no historical data to load.
		return
	}

	if err != nil {
		lps.logger.WithError(err).WithFields(logrus.Fields{
			"caller_id": callerID,
			"call_uuid": profile.CallUUID,
			"storage":   lps.persistenceConfig.PersistenceStorage,
		}).Warning("Failed to load historical language preferences")
		return
	}

	if historical == nil {
		lps.logger.WithFields(logrus.Fields{
			"caller_id": callerID,
			"call_uuid": profile.CallUUID,
		}).Debug("No historical language preferences found for caller")
		return
	}

	lps.applyHistoricalPreferences(profile, historical)

	lps.logger.WithFields(logrus.Fields{
		"caller_id":           callerID,
		"call_uuid":           profile.CallUUID,
		"historical_call":     historical.CallUUID,
		"preferred_languages": len(profile.PreferredLanguages),
	}).Info("Loaded historical language preferences for caller")
}

// applyHistoricalPreferences merges a previously stored profile into a freshly
// started call profile without overriding explicit user preferences.
func (lps *LanguagePersistenceService) applyHistoricalPreferences(profile, historical *CallLanguageProfile) {
	existing := make(map[string]bool, len(profile.PreferredLanguages))
	for _, pref := range profile.PreferredLanguages {
		existing[pref.Language] = true
	}

	for _, pref := range historical.PreferredLanguages {
		if pref.Language == "" || existing[pref.Language] {
			continue
		}
		learned := pref
		learned.PreferenceSource = "learned"
		profile.PreferredLanguages = append(profile.PreferredLanguages, learned)
		existing[pref.Language] = true
	}

	if profile.PrimaryLanguage == "" {
		if historical.PrimaryLanguage != "" {
			profile.PrimaryLanguage = historical.PrimaryLanguage
		} else {
			profile.PrimaryLanguage = historical.SwitchingPattern.DominantLanguage
		}
	}

	for _, lang := range historical.SecondaryLanguages {
		if lang == "" {
			continue
		}
		found := false
		for _, current := range profile.SecondaryLanguages {
			if current == lang {
				found = true
				break
			}
		}
		if !found {
			profile.SecondaryLanguages = append(profile.SecondaryLanguages, lang)
		}
	}
}

// EndCallProfile finalizes a call profile and updates global statistics
func (lps *LanguagePersistenceService) EndCallProfile(callUUID string) {
	lps.profileMutex.Lock()
	defer lps.profileMutex.Unlock()

	profile, exists := lps.callProfiles[callUUID]
	if !exists {
		return
	}

	profile.CallDuration = time.Since(profile.LastActivity)

	// Update global statistics
	lps.updateGlobalProfile(profile)

	// Store profile for historical reference (if enabled)
	if lps.persistenceConfig.EnablePersistence {
		lps.storeProfile(profile)
	}

	lps.logger.WithFields(logrus.Fields{
		"call_uuid":          callUUID,
		"call_duration":      profile.CallDuration,
		"languages_used":     len(profile.PreferredLanguages),
		"segments_processed": profile.SegmentCount,
		"switch_count":       lps.countLanguageSwitches(profile.LanguageHistory),
	}).Info("Ended language persistence profile")

	delete(lps.callProfiles, callUUID)
}

// LoadCallProfile returns the language profile for a call. The in-memory
// cache of active calls is consulted first, followed by the configured
// persistence backend (Redis or file storage).
func (lps *LanguagePersistenceService) LoadCallProfile(callUUID string) (*CallLanguageProfile, error) {
	lps.profileMutex.RLock()
	profile, exists := lps.callProfiles[callUUID]
	lps.profileMutex.RUnlock()

	if exists {
		return profile, nil
	}

	if !lps.persistenceConfig.EnablePersistence {
		return nil, fmt.Errorf("language profile for call %s not found", callUUID)
	}

	var loaded *CallLanguageProfile
	var err error

	switch lps.persistenceConfig.PersistenceStorage {
	case "redis":
		loaded, err = lps.loadProfileRedis(callUUID)
	case "file":
		loaded, err = lps.loadProfileFile(callUUID)
	case "memory", "":
		// Memory storage only retains active calls, which were checked above.
	default:
		return nil, fmt.Errorf("unknown persistence storage type %q", lps.persistenceConfig.PersistenceStorage)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to load language profile for call %s: %w", callUUID, err)
	}

	if loaded == nil {
		return nil, fmt.Errorf("language profile for call %s not found", callUUID)
	}

	lps.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"storage":   lps.persistenceConfig.PersistenceStorage,
	}).Debug("Loaded language profile from persistence backend")

	return loaded, nil
}

// updateGlobalProfile updates global language usage statistics
func (lps *LanguagePersistenceService) updateGlobalProfile(profile *CallLanguageProfile) {
	lps.globalProfile.TotalCalls++
	lps.globalProfile.LastUpdated = time.Now()

	// Update language popularity
	for _, pref := range profile.PreferredLanguages {
		lps.globalProfile.LanguagePopularity[pref.Language]++

		// Update average quality
		currentQuality := lps.globalProfile.AverageQuality[pref.Language]
		alpha := 0.1
		lps.globalProfile.AverageQuality[pref.Language] =
			(1-alpha)*currentQuality + alpha*pref.QualityScore
	}

	// Update global switch statistics
	switches := lps.countLanguageSwitches(profile.LanguageHistory)
	if switches > 0 {
		successRate := profile.QualityMetrics.SwitchAccuracy
		if successRate > 0.8 {
			lps.globalProfile.SuccessfulSwitches += switches
		} else {
			lps.globalProfile.FailedSwitches += switches
		}
	}
}

// storeProfile stores a profile for persistence
func (lps *LanguagePersistenceService) storeProfile(profile *CallLanguageProfile) {
	// Implementation would depend on persistence storage type
	switch lps.persistenceConfig.PersistenceStorage {
	case "redis":
		lps.storeProfileRedis(profile)
	case "file":
		lps.storeProfileFile(profile)
	case "memory":
		// Already in memory
	default:
		lps.logger.Warn("Unknown persistence storage type")
	}
}

// storeProfileRedis stores the profile in Redis as JSON with the configured
// retention TTL and maintains a per-caller index of recent calls.
func (lps *LanguagePersistenceService) storeProfileRedis(profile *CallLanguageProfile) {
	if lps.redisClient == nil {
		lps.logger.WithField("call_uuid", profile.CallUUID).
			Warn("Redis persistence selected but Redis client is not configured")
		return
	}

	ser, err := json.Marshal(profile)
	if err != nil {
		lps.logger.WithError(err).WithField("call_uuid", profile.CallUUID).
			Error("Failed to serialize language profile for Redis persistence")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), lps.redisTimeout)
	defer cancel()

	retention := lps.persistenceConfig.ProfileRetentionTime
	if retention <= 0 {
		retention = 24 * time.Hour
	}

	pipe := lps.redisClient.TxPipeline()
	pipe.Set(ctx, lps.redisProfileKey(profile.CallUUID), ser, retention)

	if profile.CallerID != "" {
		callerKey := lps.redisCallerIndexKey(profile.CallerID)
		pipe.ZAdd(ctx, callerKey, redis.Z{Score: float64(time.Now().Unix()), Member: profile.CallUUID})
		pipe.Expire(ctx, callerKey, retention)

		if maxEntries := lps.persistenceConfig.HistoryMaxEntries; maxEntries > 0 {
			cutoff := int64(-maxEntries - 1)
			pipe.ZRemRangeByRank(ctx, callerKey, 0, cutoff)
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		lps.logger.WithError(err).WithFields(logrus.Fields{
			"call_uuid": profile.CallUUID,
			"storage":   "redis",
		}).Error("Failed to persist language profile to Redis")
		return
	}

	lps.logger.WithFields(logrus.Fields{
		"call_uuid": profile.CallUUID,
		"caller_id": profile.CallerID,
		"storage":   "redis",
	}).Debug("Persisted language profile to Redis")
}

// storeProfileFile persists a profile as JSON on disk using an atomic
// temp-file + rename so readers never observe partial writes.
func (lps *LanguagePersistenceService) storeProfileFile(profile *CallLanguageProfile) {
	dir := lps.profileDirectory()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		lps.logger.WithError(err).WithField("directory", dir).
			Error("Failed to ensure language profile directory exists")
		return
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		lps.logger.WithError(err).WithField("call_uuid", profile.CallUUID).
			Error("Failed to serialize language profile for file storage")
		return
	}
	data = append(data, '\n')

	filePath := filepath.Join(dir, lps.profileFileName(profile.CallUUID))

	lps.fileMutex.Lock()
	defer lps.fileMutex.Unlock()

	if err := writeFileAtomic(filePath, data); err != nil {
		lps.logger.WithError(err).WithField("file", filePath).
			Error("Failed to write language profile to file")
		return
	}

	// Maintain a per-caller copy so historical preferences can be loaded
	// without scanning the whole directory.
	if profile.CallerID != "" {
		callerPath := filepath.Join(dir, lps.callerProfileFileName(profile.CallerID))
		if err := writeFileAtomic(callerPath, data); err != nil {
			lps.logger.WithError(err).WithField("file", callerPath).
				Warning("Failed to write caller-indexed language profile to file")
		}
	}

	lps.logger.WithFields(logrus.Fields{
		"call_uuid": profile.CallUUID,
		"caller_id": profile.CallerID,
		"path":      filePath,
		"storage":   "file",
	}).Debug("Persisted language profile to file")
}

// writeFileAtomic writes data to path via a temporary file in the same
// directory followed by a rename, with 0600 permissions.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary profile file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set profile file permissions: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write profile data: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync profile file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temporary profile file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to finalize profile file: %w", err)
	}

	return nil
}

// profileDirectory returns the configured file storage directory with a sane default.
func (lps *LanguagePersistenceService) profileDirectory() string {
	if lps.fileDirectory != "" {
		return lps.fileDirectory
	}
	return filepath.Join("data", "language_profiles")
}

// profileFileName returns the sanitized file name for a call profile.
func (lps *LanguagePersistenceService) profileFileName(callUUID string) string {
	return sanitizeProfileKey(callUUID) + ".json"
}

// callerProfileFileName returns the sanitized file name for a caller-indexed profile.
func (lps *LanguagePersistenceService) callerProfileFileName(callerID string) string {
	return "caller_" + sanitizeProfileKey(callerID) + ".json"
}

// sanitizeProfileKey converts an arbitrary profile key (call UUID or caller ID)
// into a filesystem-safe name: only alphanumerics, '-', '_' and '.' are kept,
// leading/trailing dots are stripped, and the length is capped.
func sanitizeProfileKey(key string) string {
	key = strings.TrimSpace(key)

	var builder strings.Builder
	builder.Grow(len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}

	sanitized := strings.Trim(builder.String(), ".")
	if len(sanitized) > 128 {
		sanitized = sanitized[:128]
	}
	if sanitized == "" {
		sanitized = "profile"
	}

	return sanitized
}

// loadProfileFile loads a call profile from file storage. It returns
// (nil, nil) when no profile exists for the call.
func (lps *LanguagePersistenceService) loadProfileFile(callUUID string) (*CallLanguageProfile, error) {
	return lps.readProfileFile(lps.profileFileName(callUUID))
}

// loadCallerProfileFile loads the most recent caller-indexed profile from file
// storage. It returns (nil, nil) when no profile exists for the caller.
func (lps *LanguagePersistenceService) loadCallerProfileFile(callerID string) (*CallLanguageProfile, error) {
	return lps.readProfileFile(lps.callerProfileFileName(callerID))
}

// readProfileFile reads and decodes a profile JSON file from the configured directory.
func (lps *LanguagePersistenceService) readProfileFile(fileName string) (*CallLanguageProfile, error) {
	path := filepath.Join(lps.profileDirectory(), fileName)

	lps.fileMutex.Lock()
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from a sanitized key within the configured directory
	lps.fileMutex.Unlock()

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read language profile %s: %w", path, err)
	}

	var profile CallLanguageProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to decode language profile %s: %w", path, err)
	}

	return &profile, nil
}

// loadProfileRedis loads a call profile from Redis. It returns (nil, nil)
// when no profile exists for the call.
func (lps *LanguagePersistenceService) loadProfileRedis(callUUID string) (*CallLanguageProfile, error) {
	if lps.redisClient == nil {
		return nil, errors.New("redis client is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), lps.redisTimeout)
	defer cancel()

	data, err := lps.redisClient.Get(ctx, lps.redisProfileKey(callUUID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load language profile for call %s from Redis: %w", callUUID, err)
	}

	var profile CallLanguageProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to decode language profile for call %s from Redis: %w", callUUID, err)
	}

	return &profile, nil
}

// loadCallerProfileRedis loads the most recent profile stored for a caller via
// the Redis caller index. It returns (nil, nil) when none exists.
func (lps *LanguagePersistenceService) loadCallerProfileRedis(callerID string) (*CallLanguageProfile, error) {
	if lps.redisClient == nil {
		return nil, errors.New("redis client is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), lps.redisTimeout)
	defer cancel()

	// Most recent call UUIDs first; profiles may have expired, so check a few.
	callUUIDs, err := lps.redisClient.ZRevRange(ctx, lps.redisCallerIndexKey(callerID), 0, 4).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load caller index for %s from Redis: %w", callerID, err)
	}

	for _, callUUID := range callUUIDs {
		profile, err := lps.loadProfileRedis(callUUID)
		if err != nil {
			return nil, err
		}
		if profile != nil {
			return profile, nil
		}
	}

	return nil, nil
}

func (lps *LanguagePersistenceService) redisProfileKey(callUUID string) string {
	prefix := lps.redisKeyPrefix
	if prefix == "" {
		prefix = "siprec:lang_profiles"
	}

	return fmt.Sprintf("%s:call:%s", prefix, callUUID)
}

func (lps *LanguagePersistenceService) redisCallerIndexKey(callerID string) string {
	prefix := lps.redisKeyPrefix
	if prefix == "" {
		prefix = "siprec:lang_profiles"
	}

	sanitized := strings.ToLower(strings.TrimSpace(callerID))
	sanitized = strings.ReplaceAll(sanitized, " ", "_")

	return fmt.Sprintf("%s:caller:%s", prefix, sanitized)
}

// GetGlobalLanguageStatistics returns global language usage statistics
func (lps *LanguagePersistenceService) GetGlobalLanguageStatistics() *GlobalLanguageProfile {
	// Return a copy to prevent external modification
	globalCopy := *lps.globalProfile

	// Deep copy maps
	globalCopy.LanguagePopularity = make(map[string]int)
	for k, v := range lps.globalProfile.LanguagePopularity {
		globalCopy.LanguagePopularity[k] = v
	}

	globalCopy.AverageQuality = make(map[string]float64)
	for k, v := range lps.globalProfile.AverageQuality {
		globalCopy.AverageQuality[k] = v
	}

	return &globalCopy
}
