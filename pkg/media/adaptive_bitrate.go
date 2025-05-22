package media

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// AdaptiveBitrateController manages dynamic bitrate adjustment based on network conditions
type AdaptiveBitrateController struct {
	// Current configuration
	currentBitrate int    // Current bitrate in bps
	targetBitrate  int    // Target bitrate in bps
	maxBitrate     int    // Maximum allowed bitrate
	minBitrate     int    // Minimum allowed bitrate
	codecType      string // Codec type for bitrate constraints

	// Network condition monitoring
	qualityMetrics   *QualityMetrics
	lastAdjustment   time.Time
	adjustmentPeriod time.Duration // Minimum time between adjustments

	// Adaptation algorithm state
	consecutiveGoodPeriods int  // Consecutive periods with good quality
	consecutiveBadPeriods  int  // Consecutive periods with poor quality
	emergencyMode          bool // Whether in emergency low-bitrate mode

	// Configuration
	config *AdaptiveBitrateConfig

	// Thread safety
	mutex sync.RWMutex

	// Callbacks
	bitrateChangeCallback func(oldBitrate, newBitrate int, reason string)
}

// AdaptiveBitrateConfig configures the adaptive bitrate behavior
type AdaptiveBitrateConfig struct {
	// Quality thresholds
	GoodQualityMOSThreshold float64 // MOS threshold for "good" quality
	PoorQualityMOSThreshold float64 // MOS threshold for "poor" quality
	MaxPacketLossRate       float64 // Maximum acceptable packet loss rate
	MaxJitterMs             float64 // Maximum acceptable jitter in ms
	MaxRTTMs                float64 // Maximum acceptable RTT in ms

	// Adaptation behavior
	AggressiveMode           bool          // Whether to adapt aggressively
	ConservativeMode         bool          // Whether to be conservative with increases
	MinAdjustmentPeriod      time.Duration // Minimum time between adjustments
	MaxAdjustmentStepPercent int           // Maximum adjustment step as percentage

	// Emergency handling
	EnableEmergencyMode    bool          // Whether to enable emergency mode
	EmergencyModeThreshold float64       // MOS threshold to trigger emergency mode
	EmergencyModeDuration  time.Duration // How long to stay in emergency mode

	// Bitrate step configuration
	IncreaseStepPercent      int // Percentage increase per step
	DecreaseStepPercent      int // Percentage decrease per step
	EmergencyDecreasePercent int // Emergency decrease percentage
}

// DefaultAdaptiveBitrateConfig returns a default configuration
func DefaultAdaptiveBitrateConfig() *AdaptiveBitrateConfig {
	return &AdaptiveBitrateConfig{
		GoodQualityMOSThreshold:  4.0,
		PoorQualityMOSThreshold:  3.0,
		MaxPacketLossRate:        0.02, // 2%
		MaxJitterMs:              30.0,
		MaxRTTMs:                 200.0,
		AggressiveMode:           false,
		ConservativeMode:         true,
		MinAdjustmentPeriod:      time.Second * 5,
		MaxAdjustmentStepPercent: 25,
		EnableEmergencyMode:      true,
		EmergencyModeThreshold:   2.0,
		EmergencyModeDuration:    time.Second * 30,
		IncreaseStepPercent:      10,
		DecreaseStepPercent:      15,
		EmergencyDecreasePercent: 30,
	}
}

// NewAdaptiveBitrateController creates a new adaptive bitrate controller
func NewAdaptiveBitrateController(codecType string, initialBitrate, minBitrate, maxBitrate int, qualityMetrics *QualityMetrics, config *AdaptiveBitrateConfig) *AdaptiveBitrateController {
	if config == nil {
		config = DefaultAdaptiveBitrateConfig()
	}

	return &AdaptiveBitrateController{
		currentBitrate:   initialBitrate,
		targetBitrate:    initialBitrate,
		maxBitrate:       maxBitrate,
		minBitrate:       minBitrate,
		codecType:        codecType,
		qualityMetrics:   qualityMetrics,
		adjustmentPeriod: config.MinAdjustmentPeriod,
		config:           config,
		lastAdjustment:   time.Now(),
	}
}

// SetBitrateChangeCallback sets a callback to be called when bitrate changes
func (abc *AdaptiveBitrateController) SetBitrateChangeCallback(callback func(oldBitrate, newBitrate int, reason string)) {
	abc.mutex.Lock()
	defer abc.mutex.Unlock()
	abc.bitrateChangeCallback = callback
}

// Update performs periodic quality assessment and bitrate adjustment
func (abc *AdaptiveBitrateController) Update() {
	abc.mutex.Lock()
	defer abc.mutex.Unlock()

	// Check if enough time has passed since last adjustment
	if time.Since(abc.lastAdjustment) < abc.adjustmentPeriod {
		return
	}

	// Perform quality assessment
	decision := abc.assessQuality()

	// Apply bitrate adjustment based on decision
	abc.applyBitrateAdjustment(decision)

	abc.lastAdjustment = time.Now()
}

// QualityDecision represents the result of quality assessment
type QualityDecision struct {
	Action          string // "increase", "decrease", "maintain", "emergency"
	Severity        int    // 1-5, higher means more severe action needed
	Reason          string // Human-readable reason for the decision
	RecommendedStep int    // Recommended adjustment percentage
}

// assessQuality analyzes current network conditions and determines action
func (abc *AdaptiveBitrateController) assessQuality() QualityDecision {
	// Get current quality metrics
	summary := abc.qualityMetrics.GetMetricsSummary()

	mosScore := summary["mos_score"].(float64)
	packetLossRate := summary["packet_loss_rate"].(float64)
	jitterMs := summary["jitter_ms"].(float64)
	rttMs := summary["rtt_ms"].(float64)

	// Check for emergency conditions first
	if abc.config.EnableEmergencyMode && mosScore < abc.config.EmergencyModeThreshold {
		abc.emergencyMode = true
		abc.consecutiveBadPeriods++
		abc.consecutiveGoodPeriods = 0

		return QualityDecision{
			Action:          "emergency",
			Severity:        5,
			Reason:          fmt.Sprintf("Emergency: MOS %.2f below threshold %.2f", mosScore, abc.config.EmergencyModeThreshold),
			RecommendedStep: abc.config.EmergencyDecreasePercent,
		}
	}

	// Reset emergency mode if quality has recovered
	if abc.emergencyMode && mosScore >= abc.config.GoodQualityMOSThreshold {
		abc.emergencyMode = false
	}

	// Evaluate individual quality metrics
	var issues []string
	severity := 1

	if packetLossRate > abc.config.MaxPacketLossRate {
		issues = append(issues, fmt.Sprintf("packet loss %.2f%%", packetLossRate*100))
		severity = int(math.Max(float64(severity), 3))
	}

	if jitterMs > abc.config.MaxJitterMs {
		issues = append(issues, fmt.Sprintf("jitter %.1fms", jitterMs))
		severity = int(math.Max(float64(severity), 2))
	}

	if rttMs > abc.config.MaxRTTMs {
		issues = append(issues, fmt.Sprintf("RTT %.1fms", rttMs))
		severity = int(math.Max(float64(severity), 2))
	}

	// Make decision based on overall MOS score and specific issues
	if mosScore < abc.config.PoorQualityMOSThreshold || len(issues) > 0 {
		abc.consecutiveBadPeriods++
		abc.consecutiveGoodPeriods = 0

		// More aggressive decrease if multiple consecutive bad periods
		stepPercent := abc.config.DecreaseStepPercent
		if abc.consecutiveBadPeriods > 2 {
			stepPercent = int(float64(stepPercent) * 1.5)
		}

		reason := fmt.Sprintf("Poor quality: MOS %.2f", mosScore)
		if len(issues) > 0 {
			reason += fmt.Sprintf(", issues: %v", issues)
		}

		return QualityDecision{
			Action:          "decrease",
			Severity:        severity,
			Reason:          reason,
			RecommendedStep: stepPercent,
		}
	}

	if mosScore >= abc.config.GoodQualityMOSThreshold {
		abc.consecutiveGoodPeriods++
		abc.consecutiveBadPeriods = 0

		// Only increase if we've had consistently good quality
		if abc.consecutiveGoodPeriods >= 3 && abc.currentBitrate < abc.maxBitrate {
			stepPercent := abc.config.IncreaseStepPercent

			// Be more conservative if configured
			if abc.config.ConservativeMode {
				stepPercent = stepPercent / 2
			}

			// Be more aggressive if configured and conditions are very good
			if abc.config.AggressiveMode && mosScore > 4.5 && packetLossRate < 0.001 {
				stepPercent = int(float64(stepPercent) * 1.5)
			}

			return QualityDecision{
				Action:          "increase",
				Severity:        1,
				Reason:          fmt.Sprintf("Good quality: MOS %.2f, %d consecutive good periods", mosScore, abc.consecutiveGoodPeriods),
				RecommendedStep: stepPercent,
			}
		}
	}

	// Default: maintain current bitrate
	return QualityDecision{
		Action:          "maintain",
		Severity:        1,
		Reason:          fmt.Sprintf("Maintaining: MOS %.2f", mosScore),
		RecommendedStep: 0,
	}
}

// applyBitrateAdjustment applies the decided bitrate adjustment
func (abc *AdaptiveBitrateController) applyBitrateAdjustment(decision QualityDecision) {
	oldBitrate := abc.currentBitrate
	newBitrate := abc.currentBitrate

	switch decision.Action {
	case "increase":
		adjustment := float64(abc.currentBitrate) * float64(decision.RecommendedStep) / 100.0
		newBitrate = abc.currentBitrate + int(adjustment)

		// Apply maximum step limit
		maxStep := float64(abc.currentBitrate) * float64(abc.config.MaxAdjustmentStepPercent) / 100.0
		if adjustment > maxStep {
			newBitrate = abc.currentBitrate + int(maxStep)
		}

		// Respect maximum bitrate
		if newBitrate > abc.maxBitrate {
			newBitrate = abc.maxBitrate
		}

	case "decrease", "emergency":
		adjustment := float64(abc.currentBitrate) * float64(decision.RecommendedStep) / 100.0
		newBitrate = abc.currentBitrate - int(adjustment)

		// Apply maximum step limit (except in emergency)
		if decision.Action != "emergency" {
			maxStep := float64(abc.currentBitrate) * float64(abc.config.MaxAdjustmentStepPercent) / 100.0
			if adjustment > maxStep {
				newBitrate = abc.currentBitrate - int(maxStep)
			}
		}

		// Respect minimum bitrate
		if newBitrate < abc.minBitrate {
			newBitrate = abc.minBitrate
		}

	case "maintain":
		// No change
		return
	}

	// Apply codec-specific constraints
	newBitrate = abc.applyCodecConstraints(newBitrate)

	// Update bitrate if it changed
	if newBitrate != abc.currentBitrate {
		abc.currentBitrate = newBitrate
		abc.targetBitrate = newBitrate

		// Call callback if set
		if abc.bitrateChangeCallback != nil {
			abc.bitrateChangeCallback(oldBitrate, newBitrate, decision.Reason)
		}
	}
}

// applyCodecConstraints applies codec-specific bitrate constraints
func (abc *AdaptiveBitrateController) applyCodecConstraints(bitrate int) int {
	switch abc.codecType {
	case "PCMU", "PCMA": // G.711
		// G.711 is fixed at 64 kbps
		return 64000

	case "G722":
		// G.722 supports 48, 56, 64 kbps
		if bitrate <= 48000 {
			return 48000
		} else if bitrate <= 56000 {
			return 56000
		} else {
			return 64000
		}

	case "OPUS":
		// Opus supports 6-510 kbps, but practical range for speech is 6-64 kbps
		if bitrate < 6000 {
			return 6000
		} else if bitrate > 64000 {
			return 64000
		}
		// Round to nearest kbps
		return (bitrate / 1000) * 1000

	case "EVS":
		// EVS supports various bitrates: 5.9, 7.2, 8, 9.6, 13.2, 16.4, 24.4, 32, 48, 64, 96, 128 kbps
		validRates := []int{5900, 7200, 8000, 9600, 13200, 16400, 24400, 32000, 48000, 64000, 96000, 128000}

		// Find closest valid rate
		closest := validRates[0]
		minDiff := int(math.Abs(float64(bitrate - closest)))

		for _, rate := range validRates {
			diff := int(math.Abs(float64(bitrate - rate)))
			if diff < minDiff {
				minDiff = diff
				closest = rate
			}
		}

		return closest

	default:
		// Unknown codec, return as-is
		return bitrate
	}
}

// GetCurrentBitrate returns the current bitrate
func (abc *AdaptiveBitrateController) GetCurrentBitrate() int {
	abc.mutex.RLock()
	defer abc.mutex.RUnlock()
	return abc.currentBitrate
}

// GetTargetBitrate returns the target bitrate
func (abc *AdaptiveBitrateController) GetTargetBitrate() int {
	abc.mutex.RLock()
	defer abc.mutex.RUnlock()
	return abc.targetBitrate
}

// IsInEmergencyMode returns whether the controller is in emergency mode
func (abc *AdaptiveBitrateController) IsInEmergencyMode() bool {
	abc.mutex.RLock()
	defer abc.mutex.RUnlock()
	return abc.emergencyMode
}

// SetBitrateRange updates the allowed bitrate range
func (abc *AdaptiveBitrateController) SetBitrateRange(minBitrate, maxBitrate int) {
	abc.mutex.Lock()
	defer abc.mutex.Unlock()

	abc.minBitrate = minBitrate
	abc.maxBitrate = maxBitrate

	// Adjust current bitrate if it's outside the new range
	if abc.currentBitrate < minBitrate {
		abc.currentBitrate = minBitrate
		abc.targetBitrate = minBitrate
	} else if abc.currentBitrate > maxBitrate {
		abc.currentBitrate = maxBitrate
		abc.targetBitrate = maxBitrate
	}
}

// GetAdaptationStats returns statistics about the adaptation behavior
func (abc *AdaptiveBitrateController) GetAdaptationStats() map[string]interface{} {
	abc.mutex.RLock()
	defer abc.mutex.RUnlock()

	return map[string]interface{}{
		"current_bitrate":           abc.currentBitrate,
		"target_bitrate":            abc.targetBitrate,
		"min_bitrate":               abc.minBitrate,
		"max_bitrate":               abc.maxBitrate,
		"codec_type":                abc.codecType,
		"emergency_mode":            abc.emergencyMode,
		"consecutive_good_periods":  abc.consecutiveGoodPeriods,
		"consecutive_bad_periods":   abc.consecutiveBadPeriods,
		"last_adjustment":           abc.lastAdjustment,
		"adjustment_period_seconds": abc.adjustmentPeriod.Seconds(),
	}
}

// ForceAdjustment forces an immediate bitrate adjustment (bypasses timing restrictions)
func (abc *AdaptiveBitrateController) ForceAdjustment() {
	abc.mutex.Lock()
	abc.lastAdjustment = time.Time{} // Reset timer
	abc.mutex.Unlock()

	abc.Update()
}

// Reset resets the adaptation state
func (abc *AdaptiveBitrateController) Reset() {
	abc.mutex.Lock()
	defer abc.mutex.Unlock()

	abc.consecutiveGoodPeriods = 0
	abc.consecutiveBadPeriods = 0
	abc.emergencyMode = false
	abc.lastAdjustment = time.Now()
}
