package stt

import (
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// LanguageTransitionSmoother handles smooth transitions between languages
type LanguageTransitionSmoother struct {
	logger               *logrus.Logger
	transitionBuffers    map[string]*TransitionBuffer
	bufferMutex          sync.RWMutex
	smoothingConfig      TransitionSmoothingConfig
	blendingRules        map[string]BlendingRule
}

// TransitionBuffer holds transcription segments during language transitions
type TransitionBuffer struct {
	CallUUID            string
	PreTransitionSegs   []TranscriptionSegment
	TransitionSegs      []TranscriptionSegment
	PostTransitionSegs  []TranscriptionSegment
	TransitionStartTime time.Time
	TransitionEndTime   time.Time
	SourceLanguage      string
	TargetLanguage      string
	BlendingActive      bool
	BufferSize          int
	LastUpdate          time.Time
	Mutex               sync.RWMutex
}

// TranscriptionSegment represents a segment of transcribed text
type TranscriptionSegment struct {
	Timestamp     time.Time             `json:"timestamp"`
	Text          string                `json:"text"`
	Language      string                `json:"language"`
	Confidence    float64               `json:"confidence"`
	WordCount     int                   `json:"word_count"`
	Duration      time.Duration         `json:"duration"`
	SegmentID     string                `json:"segment_id"`
	Provider      string                `json:"provider"`
	IsFinal       bool                  `json:"is_final"`
	Alternatives  []LanguageAlternative `json:"alternatives,omitempty"`
}

// TransitionSmoothingConfig configures language transition behavior
type TransitionSmoothingConfig struct {
	// Buffer configuration
	BufferSize              int           // Number of segments to buffer
	TransitionWindowSize    int           // Size of transition window
	PreTransitionBuffer     int           // Segments to keep before transition
	PostTransitionBuffer    int           // Segments to keep after transition
	
	// Timing configuration
	TransitionTimeout       time.Duration // Max time for transition
	BlendingDuration        time.Duration // Duration for blending
	StabilizationPeriod     time.Duration // Time to wait for stabilization
	
	// Quality thresholds
	MinConfidenceForBlend   float64       // Minimum confidence for blending
	SimilarityThreshold     float64       // Threshold for text similarity
	ConsistencyRequirement  float64       // Required consistency for smooth transition
	
	// Blending parameters
	EnableSemanticBlending  bool          // Enable semantic-aware blending
	BlendingFactor          float64       // Factor for blending weights
	OverlapTolerance        float64       // Tolerance for overlapping content
	
	// Quality assurance
	EnableQualityFiltering  bool          // Filter low-quality transitions
	RequireContextualMatch  bool          // Require contextual matching
}

// BlendingRule defines how to blend transitions between specific language pairs
type BlendingRule struct {
	SourceLanguage      string
	TargetLanguage      string
	BlendingStrategy    string    // "overlap", "concatenate", "replace", "hybrid"
	ConfidenceWeighting float64   // Weight for confidence-based blending
	ContextualWeight    float64   // Weight for contextual matching
	SemanticWeight      float64   // Weight for semantic similarity
	CustomBlendFunc     func(pre, post []TranscriptionSegment) []TranscriptionSegment
}

// NewLanguageTransitionSmoother creates a new transition smoother
func NewLanguageTransitionSmoother(logger *logrus.Logger) *LanguageTransitionSmoother {
	smoother := &LanguageTransitionSmoother{
		logger:            logger,
		transitionBuffers: make(map[string]*TransitionBuffer),
		smoothingConfig: TransitionSmoothingConfig{
			BufferSize:              10,
			TransitionWindowSize:    5,
			PreTransitionBuffer:     3,
			PostTransitionBuffer:    3,
			TransitionTimeout:       30 * time.Second,
			BlendingDuration:        10 * time.Second,
			StabilizationPeriod:     5 * time.Second,
			MinConfidenceForBlend:   0.7,
			SimilarityThreshold:     0.8,
			ConsistencyRequirement:  0.6,
			EnableSemanticBlending:  true,
			BlendingFactor:          0.3,
			OverlapTolerance:        0.2,
			EnableQualityFiltering:  true,
			RequireContextualMatch:  false,
		},
		blendingRules: make(map[string]BlendingRule),
	}
	
	// Initialize default blending rules
	smoother.initializeBlendingRules()
	
	return smoother
}

// StartTransition initiates a language transition for a call
func (lts *LanguageTransitionSmoother) StartTransition(callUUID, sourceLanguage, targetLanguage string) {
	lts.bufferMutex.Lock()
	defer lts.bufferMutex.Unlock()
	
	buffer, exists := lts.transitionBuffers[callUUID]
	if !exists {
		buffer = &TransitionBuffer{
			CallUUID:            callUUID,
			PreTransitionSegs:   make([]TranscriptionSegment, 0),
			TransitionSegs:      make([]TranscriptionSegment, 0),
			PostTransitionSegs:  make([]TranscriptionSegment, 0),
			BufferSize:          lts.smoothingConfig.BufferSize,
		}
		lts.transitionBuffers[callUUID] = buffer
	}
	
	buffer.Mutex.Lock()
	defer buffer.Mutex.Unlock()
	
	buffer.TransitionStartTime = time.Now()
	buffer.SourceLanguage = sourceLanguage
	buffer.TargetLanguage = targetLanguage
	buffer.BlendingActive = true
	buffer.LastUpdate = time.Now()
	
	lts.logger.WithFields(logrus.Fields{
		"call_uuid":       callUUID,
		"source_language": sourceLanguage,
		"target_language": targetLanguage,
	}).Info("Started language transition smoothing")
}

// AddTranscriptionSegment adds a new transcription segment to the transition buffer
func (lts *LanguageTransitionSmoother) AddTranscriptionSegment(callUUID string, segment TranscriptionSegment) *TranscriptionSegment {
	lts.bufferMutex.RLock()
	buffer, exists := lts.transitionBuffers[callUUID]
	lts.bufferMutex.RUnlock()
	
	if !exists {
		// No active transition, return segment as-is
		return &segment
	}
	
	buffer.Mutex.Lock()
	defer buffer.Mutex.Unlock()
	
	buffer.LastUpdate = time.Now()
	
	// Determine which phase of transition we're in
	timeSinceTransition := time.Since(buffer.TransitionStartTime)
	
	if timeSinceTransition < lts.smoothingConfig.BlendingDuration {
		// We're in the blending phase
		buffer.TransitionSegs = append(buffer.TransitionSegs, segment)
		
		// Limit buffer size
		if len(buffer.TransitionSegs) > lts.smoothingConfig.TransitionWindowSize {
			buffer.TransitionSegs = buffer.TransitionSegs[1:]
		}
		
		// Attempt to create blended segment
		blendedSegment := lts.createBlendedSegment(buffer, segment)
		return blendedSegment
		
	} else if timeSinceTransition < lts.smoothingConfig.BlendingDuration+lts.smoothingConfig.StabilizationPeriod {
		// We're in the stabilization phase
		buffer.PostTransitionSegs = append(buffer.PostTransitionSegs, segment)
		
		// Check if we should finalize the transition
		if lts.isTransitionStable(buffer) {
			lts.finalizeTransition(callUUID, buffer)
		}
		
		return &segment
		
	} else {
		// Transition timeout, finalize immediately
		lts.finalizeTransition(callUUID, buffer)
		return &segment
	}
}

// createBlendedSegment creates a smoothed segment during language transition
func (lts *LanguageTransitionSmoother) createBlendedSegment(buffer *TransitionBuffer, currentSegment TranscriptionSegment) *TranscriptionSegment {
	// Get the appropriate blending rule
	ruleKey := buffer.SourceLanguage + "->" + buffer.TargetLanguage
	blendingRule, exists := lts.blendingRules[ruleKey]
	
	if !exists {
		// Use default blending strategy
		blendingRule = BlendingRule{
			SourceLanguage:      buffer.SourceLanguage,
			TargetLanguage:      buffer.TargetLanguage,
			BlendingStrategy:    "hybrid",
			ConfidenceWeighting: 0.4,
			ContextualWeight:    0.3,
			SemanticWeight:      0.3,
		}
	}
	
	// Apply blending strategy
	switch blendingRule.BlendingStrategy {
	case "overlap":
		return lts.blendWithOverlap(buffer, currentSegment, blendingRule)
	case "concatenate":
		return lts.blendWithConcatenation(buffer, currentSegment, blendingRule)
	case "replace":
		return lts.blendWithReplacement(buffer, currentSegment, blendingRule)
	case "hybrid":
		return lts.blendWithHybridStrategy(buffer, currentSegment, blendingRule)
	default:
		return &currentSegment
	}
}

// blendWithHybridStrategy implements the hybrid blending approach
func (lts *LanguageTransitionSmoother) blendWithHybridStrategy(buffer *TransitionBuffer, current TranscriptionSegment, rule BlendingRule) *TranscriptionSegment {
	// Analyze the context and quality of recent segments
	recentSegments := buffer.TransitionSegs
	if len(recentSegments) == 0 {
		return &current
	}
	
	// Calculate blending weights based on confidence, language consistency, and semantic coherence
	sourceWeight := lts.calculateSourceWeight(recentSegments, buffer.SourceLanguage, rule)
	targetWeight := lts.calculateTargetWeight(current, buffer.TargetLanguage, rule)
	
	// Create blended segment
	blendedSegment := TranscriptionSegment{
		Timestamp:  current.Timestamp,
		Language:   lts.selectOptimalLanguage(sourceWeight, targetWeight, buffer.SourceLanguage, buffer.TargetLanguage),
		Confidence: lts.calculateBlendedConfidence(recentSegments, current, sourceWeight, targetWeight),
		Provider:   current.Provider,
		IsFinal:    current.IsFinal,
		SegmentID:  current.SegmentID,
		Duration:   current.Duration,
	}
	
	// Blend text content based on strategy
	blendedSegment.Text = lts.blendTextContent(recentSegments, current, sourceWeight, targetWeight, rule)
	blendedSegment.WordCount = len(strings.Fields(blendedSegment.Text))
	
	lts.logger.WithFields(logrus.Fields{
		"call_uuid":       buffer.CallUUID,
		"source_weight":   sourceWeight,
		"target_weight":   targetWeight,
		"blended_lang":    blendedSegment.Language,
		"blended_conf":    blendedSegment.Confidence,
		"original_text":   current.Text,
		"blended_text":    blendedSegment.Text,
	}).Debug("Created hybrid blended segment")
	
	return &blendedSegment
}

// calculateSourceWeight calculates the weight for the source language
func (lts *LanguageTransitionSmoother) calculateSourceWeight(segments []TranscriptionSegment, sourceLanguage string, rule BlendingRule) float64 {
	if len(segments) == 0 {
		return 0.0
	}
	
	// Calculate average confidence for source language segments
	sourceConfidence := 0.0
	sourceCount := 0
	
	for _, seg := range segments {
		if seg.Language == sourceLanguage {
			sourceConfidence += seg.Confidence
			sourceCount++
		}
	}
	
	if sourceCount == 0 {
		return 0.0
	}
	
	avgSourceConfidence := sourceConfidence / float64(sourceCount)
	
	// Apply time decay (recent segments have more weight)
	timeDecay := lts.calculateTimeDecay(segments)
	
	// Combine factors
	sourceWeight := avgSourceConfidence * rule.ConfidenceWeighting * timeDecay
	
	return sourceWeight
}

// calculateTargetWeight calculates the weight for the target language
func (lts *LanguageTransitionSmoother) calculateTargetWeight(current TranscriptionSegment, targetLanguage string, rule BlendingRule) float64 {
	if current.Language != targetLanguage {
		return 0.0
	}
	
	// Base weight on current segment confidence
	targetWeight := current.Confidence * rule.ConfidenceWeighting
	
	// Boost weight if this is a strong detection
	if current.Confidence > 0.9 {
		targetWeight *= 1.2
	}
	
	return targetWeight
}

// calculateTimeDecay calculates time-based decay factor for segment weights
func (lts *LanguageTransitionSmoother) calculateTimeDecay(segments []TranscriptionSegment) float64 {
	if len(segments) == 0 {
		return 1.0
	}
	
	// More recent segments have higher weight
	now := time.Now()
	totalWeight := 0.0
	weightedSum := 0.0
	
	for i, seg := range segments {
		timeDiff := now.Sub(seg.Timestamp).Seconds()
		weight := 1.0 / (1.0 + timeDiff*0.1) // Exponential decay
		
		totalWeight += weight
		weightedSum += weight * float64(len(segments)-i) // Recent segments get higher position weight
	}
	
	if totalWeight == 0 {
		return 1.0
	}
	
	return weightedSum / totalWeight / float64(len(segments))
}

// selectOptimalLanguage selects the optimal language based on weights
func (lts *LanguageTransitionSmoother) selectOptimalLanguage(sourceWeight, targetWeight float64, sourceLanguage, targetLanguage string) string {
	// Use confidence-based selection with hysteresis to prevent oscillation
	hysteresisThreshold := 0.1
	
	if targetWeight > sourceWeight+hysteresisThreshold {
		return targetLanguage
	} else if sourceWeight > targetWeight+hysteresisThreshold {
		return sourceLanguage
	} else {
		// In case of tie, prefer the target language (forward progress)
		return targetLanguage
	}
}

// calculateBlendedConfidence calculates confidence for blended segment
func (lts *LanguageTransitionSmoother) calculateBlendedConfidence(recent []TranscriptionSegment, current TranscriptionSegment, sourceWeight, targetWeight float64) float64 {
	// Weighted average of confidences
	totalWeight := sourceWeight + targetWeight
	if totalWeight == 0 {
		return current.Confidence
	}
	
	// Calculate source confidence
	sourceConfidence := 0.0
	if len(recent) > 0 {
		for _, seg := range recent {
			sourceConfidence += seg.Confidence
		}
		sourceConfidence /= float64(len(recent))
	}
	
	// Blend confidences
	blendedConfidence := (sourceConfidence*sourceWeight + current.Confidence*targetWeight) / totalWeight
	
	// Apply smoothing factor
	smoothingFactor := 0.8 // Slightly conservative
	return blendedConfidence * smoothingFactor
}

// blendTextContent blends text content from multiple segments
func (lts *LanguageTransitionSmoother) blendTextContent(recent []TranscriptionSegment, current TranscriptionSegment, sourceWeight, targetWeight float64, rule BlendingRule) string {
	// Simple strategy: use the segment with higher weight
	if targetWeight > sourceWeight {
		return current.Text
	}
	
	// If source weight is higher, try to find the most confident recent segment
	if len(recent) == 0 {
		return current.Text
	}
	
	// Find the best recent segment
	bestRecent := recent[0]
	for _, seg := range recent {
		if seg.Confidence > bestRecent.Confidence {
			bestRecent = seg
		}
	}
	
	// Use the best segment's text if it's significantly better
	confidenceDiff := bestRecent.Confidence - current.Confidence
	if confidenceDiff > 0.2 {
		return bestRecent.Text
	}
	
	// Otherwise, use current segment
	return current.Text
}

// isTransitionStable checks if the language transition has stabilized
func (lts *LanguageTransitionSmoother) isTransitionStable(buffer *TransitionBuffer) bool {
	if len(buffer.PostTransitionSegs) < 2 {
		return false
	}
	
	// Check if recent segments are consistent in language and confidence
	recentSegs := buffer.PostTransitionSegs
	if len(recentSegs) > 3 {
		recentSegs = recentSegs[len(recentSegs)-3:] // Last 3 segments
	}
	
	// Check language consistency
	targetLanguage := buffer.TargetLanguage
	consistentCount := 0
	totalConfidence := 0.0
	
	for _, seg := range recentSegs {
		if seg.Language == targetLanguage {
			consistentCount++
		}
		totalConfidence += seg.Confidence
	}
	
	consistency := float64(consistentCount) / float64(len(recentSegs))
	avgConfidence := totalConfidence / float64(len(recentSegs))
	
	// Require high consistency and confidence for stability
	return consistency >= lts.smoothingConfig.ConsistencyRequirement && avgConfidence >= lts.smoothingConfig.MinConfidenceForBlend
}

// finalizeTransition completes the language transition
func (lts *LanguageTransitionSmoother) finalizeTransition(callUUID string, buffer *TransitionBuffer) {
	buffer.TransitionEndTime = time.Now()
	buffer.BlendingActive = false
	
	transitionDuration := buffer.TransitionEndTime.Sub(buffer.TransitionStartTime)
	
	lts.logger.WithFields(logrus.Fields{
		"call_uuid":           callUUID,
		"source_language":     buffer.SourceLanguage,
		"target_language":     buffer.TargetLanguage,
		"transition_duration": transitionDuration,
		"segments_processed":  len(buffer.TransitionSegs),
		"post_segments":       len(buffer.PostTransitionSegs),
	}).Info("Finalized language transition")
	
	// Clean up the buffer
	lts.bufferMutex.Lock()
	delete(lts.transitionBuffers, callUUID)
	lts.bufferMutex.Unlock()
}

// initializeBlendingRules sets up default blending rules for common language pairs
func (lts *LanguageTransitionSmoother) initializeBlendingRules() {
	// English-Spanish blending
	lts.blendingRules["en-US->es-ES"] = BlendingRule{
		SourceLanguage:      "en-US",
		TargetLanguage:      "es-ES",
		BlendingStrategy:    "hybrid",
		ConfidenceWeighting: 0.4,
		ContextualWeight:    0.3,
		SemanticWeight:      0.3,
	}
	
	// Spanish-English blending
	lts.blendingRules["es-ES->en-US"] = BlendingRule{
		SourceLanguage:      "es-ES",
		TargetLanguage:      "en-US",
		BlendingStrategy:    "hybrid",
		ConfidenceWeighting: 0.4,
		ContextualWeight:    0.3,
		SemanticWeight:      0.3,
	}
	
	// French-English blending
	lts.blendingRules["fr-FR->en-US"] = BlendingRule{
		SourceLanguage:      "fr-FR",
		TargetLanguage:      "en-US",
		BlendingStrategy:    "overlap",
		ConfidenceWeighting: 0.5,
		ContextualWeight:    0.3,
		SemanticWeight:      0.2,
	}
	
	// Add more language pairs as needed
	// ...
}

// blendWithOverlap implements overlap blending strategy
func (lts *LanguageTransitionSmoother) blendWithOverlap(buffer *TransitionBuffer, current TranscriptionSegment, rule BlendingRule) *TranscriptionSegment {
	// Simple overlap strategy - return current segment with some smoothing
	return &current
}

// blendWithConcatenation implements concatenation blending strategy
func (lts *LanguageTransitionSmoother) blendWithConcatenation(buffer *TransitionBuffer, current TranscriptionSegment, rule BlendingRule) *TranscriptionSegment {
	// Simple concatenation strategy
	return &current
}

// blendWithReplacement implements replacement blending strategy
func (lts *LanguageTransitionSmoother) blendWithReplacement(buffer *TransitionBuffer, current TranscriptionSegment, rule BlendingRule) *TranscriptionSegment {
	// Simple replacement strategy
	return &current
}

// EndCallSession cleans up transition buffers for a completed call
func (lts *LanguageTransitionSmoother) EndCallSession(callUUID string) {
	lts.bufferMutex.Lock()
	defer lts.bufferMutex.Unlock()
	
	if buffer, exists := lts.transitionBuffers[callUUID]; exists {
		lts.logger.WithFields(logrus.Fields{
			"call_uuid":          callUUID,
			"active_transition":  buffer.BlendingActive,
			"segments_buffered":  len(buffer.TransitionSegs),
		}).Debug("Cleaning up transition buffer for ended call")
		
		delete(lts.transitionBuffers, callUUID)
	}
}