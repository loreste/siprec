package realtime

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SpeakerChangeDetector detects when the active speaker changes
type SpeakerChangeDetector struct {
	logger *logrus.Entry

	// Detection thresholds
	similarityThreshold float64       // Minimum similarity to consider same speaker
	changeThreshold     float64       // Threshold for declaring speaker change
	minSegmentDuration  time.Duration // Minimum duration before allowing change

	// Feature comparison
	lastFeatures  *VoiceFeatures
	featureWindow []*VoiceFeatures
	windowSize    int
	windowIndex   int

	// Change detection state
	changeCandidate       bool
	candidateStartTime    time.Time
	candidateFeatures     *VoiceFeatures
	confidenceAccumulator float64
	stableFrameCount      int

	// Adaptive parameters
	adaptiveThreshold  float64
	recentSimilarities []float64
	similarityHistory  int

	// Performance optimization
	comparisonCache map[string]float64
	cacheMaxSize    int
	lastCleanup     time.Time

	// Thread safety
	mutex sync.RWMutex

	// Statistics
	stats *ChangeDetectionStats
}

// ChangeDetectionStats tracks speaker change detection performance
type ChangeDetectionStats struct {
	mutex             sync.RWMutex
	TotalComparisons  int64     `json:"total_comparisons"`
	DetectedChanges   int64     `json:"detected_changes"`
	FalsePositives    int64     `json:"false_positives"`
	FalseNegatives    int64     `json:"false_negatives"`
	AverageSimilarity float64   `json:"average_similarity"`
	ProcessingTime    int64     `json:"processing_time_ms"`
	CacheHits         int64     `json:"cache_hits"`
	CacheMisses       int64     `json:"cache_misses"`
	LastReset         time.Time `json:"last_reset"`
}

// NewSpeakerChangeDetector creates a new speaker change detector
func NewSpeakerChangeDetector() *SpeakerChangeDetector {
	scd := &SpeakerChangeDetector{
		logger:              logrus.WithField("component", "speaker_change_detector"),
		similarityThreshold: 0.7,             // 70% similarity to consider same speaker
		changeThreshold:     0.3,             // 30% threshold for change detection
		minSegmentDuration:  2 * time.Second, // Minimum 2 seconds before change
		windowSize:          5,               // 5 feature vectors for smoothing
		similarityHistory:   20,              // Track last 20 similarities for adaptation
		cacheMaxSize:        1000,            // Cache up to 1000 comparisons
		lastCleanup:         time.Now(),
		stats:               &ChangeDetectionStats{LastReset: time.Now()},
	}

	// Initialize feature window
	scd.featureWindow = make([]*VoiceFeatures, scd.windowSize)
	scd.recentSimilarities = make([]float64, 0, scd.similarityHistory)
	scd.comparisonCache = make(map[string]float64)
	scd.adaptiveThreshold = scd.similarityThreshold

	return scd
}

// ProcessFeatures processes new voice features and detects speaker changes
func (scd *SpeakerChangeDetector) ProcessFeatures(features *VoiceFeatures) bool {
	if features == nil {
		return false
	}

	startTime := time.Now()
	defer func() {
		scd.stats.mutex.Lock()
		scd.stats.ProcessingTime += time.Since(startTime).Nanoseconds() / 1e6
		scd.stats.TotalComparisons++
		scd.stats.mutex.Unlock()
	}()

	scd.mutex.Lock()
	defer scd.mutex.Unlock()

	// First features - no comparison possible
	if scd.lastFeatures == nil {
		scd.lastFeatures = scd.copyFeatures(features)
		scd.addToWindow(features)
		return false
	}

	// Calculate similarity with previous features
	similarity := scd.calculateSimilarity(scd.lastFeatures, features)
	scd.updateSimilarityHistory(similarity)

	// Update adaptive threshold
	scd.updateAdaptiveThreshold()

	// Add to feature window
	scd.addToWindow(features)

	// Determine if this indicates a speaker change
	changeDetected := scd.detectChange(similarity, features)

	// Update state
	if changeDetected {
		scd.handleChangeDetection(features)
	} else {
		scd.handleNoChange(features)
	}

	// Periodic cleanup
	scd.performPeriodicCleanup()

	return changeDetected
}

// calculateSimilarity calculates similarity between two feature sets
func (scd *SpeakerChangeDetector) calculateSimilarity(features1, features2 *VoiceFeatures) float64 {
	// Create cache key
	key := scd.createCacheKey(features1, features2)

	// Check cache first
	if cached, exists := scd.comparisonCache[key]; exists {
		scd.stats.mutex.Lock()
		scd.stats.CacheHits++
		scd.stats.mutex.Unlock()
		return cached
	}

	scd.stats.mutex.Lock()
	scd.stats.CacheMisses++
	scd.stats.mutex.Unlock()

	// Calculate weighted similarity across multiple features
	similarities := []float64{
		scd.calculateF0Similarity(features1, features2),       // 35% weight
		scd.calculateSpectralSimilarity(features1, features2), // 30% weight
		scd.calculateMFCCSimilarity(features1, features2),     // 25% weight
		scd.calculateFormantSimilarity(features1, features2),  // 10% weight
	}

	weights := []float64{0.35, 0.30, 0.25, 0.10}

	totalSimilarity := 0.0
	totalWeight := 0.0

	for i, sim := range similarities {
		if sim >= 0 { // Only include valid similarities
			totalSimilarity += sim * weights[i]
			totalWeight += weights[i]
		}
	}

	if totalWeight > 0 {
		totalSimilarity /= totalWeight
	}

	// Cache the result
	if len(scd.comparisonCache) < scd.cacheMaxSize {
		scd.comparisonCache[key] = totalSimilarity
	}

	return totalSimilarity
}

// calculateF0Similarity calculates fundamental frequency similarity
func (scd *SpeakerChangeDetector) calculateF0Similarity(f1, f2 *VoiceFeatures) float64 {
	if f1.F0Mean == 0 || f2.F0Mean == 0 {
		return -1 // Invalid
	}

	// Calculate relative differences
	meanDiff := math.Abs(f1.F0Mean-f2.F0Mean) / math.Max(f1.F0Mean, f2.F0Mean)
	stdDiff := math.Abs(f1.F0Std-f2.F0Std) / math.Max(f1.F0Std, f2.F0Std)
	rangeDiff := math.Abs(f1.F0Range-f2.F0Range) / math.Max(f1.F0Range, f2.F0Range)

	// Combine with equal weights
	avgDiff := (meanDiff + stdDiff + rangeDiff) / 3.0

	// Convert to similarity (0-1)
	return math.Max(0, 1.0-avgDiff)
}

// calculateSpectralSimilarity calculates spectral feature similarity
func (scd *SpeakerChangeDetector) calculateSpectralSimilarity(f1, f2 *VoiceFeatures) float64 {
	if f1.SpectralCentroid == 0 || f2.SpectralCentroid == 0 {
		return -1 // Invalid
	}

	// Calculate relative differences
	centroidDiff := math.Abs(f1.SpectralCentroid-f2.SpectralCentroid) /
		math.Max(f1.SpectralCentroid, f2.SpectralCentroid)
	rolloffDiff := math.Abs(f1.SpectralRolloff-f2.SpectralRolloff) /
		math.Max(f1.SpectralRolloff, f2.SpectralRolloff)
	fluxDiff := math.Abs(f1.SpectralFlux-f2.SpectralFlux) /
		math.Max(f1.SpectralFlux, f2.SpectralFlux)

	// Combine with weights (centroid is most important)
	avgDiff := (centroidDiff*0.5 + rolloffDiff*0.3 + fluxDiff*0.2)

	return math.Max(0, 1.0-avgDiff)
}

// calculateMFCCSimilarity calculates MFCC similarity using cosine similarity
func (scd *SpeakerChangeDetector) calculateMFCCSimilarity(f1, f2 *VoiceFeatures) float64 {
	if len(f1.MFCC) != len(f2.MFCC) || len(f1.MFCC) == 0 {
		return -1 // Invalid
	}

	// Cosine similarity
	dotProduct := 0.0
	norm1 := 0.0
	norm2 := 0.0

	for i := 0; i < len(f1.MFCC); i++ {
		dotProduct += f1.MFCC[i] * f2.MFCC[i]
		norm1 += f1.MFCC[i] * f1.MFCC[i]
		norm2 += f2.MFCC[i] * f2.MFCC[i]
	}

	if norm1 == 0 || norm2 == 0 {
		return -1
	}

	similarity := dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))

	// Convert from [-1,1] to [0,1]
	return (similarity + 1.0) / 2.0
}

// calculateFormantSimilarity calculates formant frequency similarity
func (scd *SpeakerChangeDetector) calculateFormantSimilarity(f1, f2 *VoiceFeatures) float64 {
	if f1.F1 == 0 || f1.F2 == 0 || f2.F1 == 0 || f2.F2 == 0 {
		return -1 // Invalid
	}

	// Calculate relative differences for each formant
	f1Diff := math.Abs(f1.F1-f2.F1) / math.Max(f1.F1, f2.F1)
	f2Diff := math.Abs(f1.F2-f2.F2) / math.Max(f1.F2, f2.F2)
	f3Diff := math.Abs(f1.F3-f2.F3) / math.Max(f1.F3, f2.F3)

	// Weight F1 and F2 more heavily as they're more speaker-distinctive
	avgDiff := (f1Diff*0.4 + f2Diff*0.4 + f3Diff*0.2)

	return math.Max(0, 1.0-avgDiff)
}

// detectChange determines if current features indicate a speaker change
func (scd *SpeakerChangeDetector) detectChange(similarity float64, features *VoiceFeatures) bool {
	now := time.Now()

	// Check if similarity is below threshold
	belowThreshold := similarity < scd.adaptiveThreshold

	if belowThreshold {
		if !scd.changeCandidate {
			// Start new change candidate
			scd.changeCandidate = true
			scd.candidateStartTime = now
			scd.candidateFeatures = scd.copyFeatures(features)
			scd.confidenceAccumulator = 1.0 - similarity
			scd.stableFrameCount = 1
		} else {
			// Continue existing candidate
			scd.confidenceAccumulator += (1.0 - similarity)
			scd.stableFrameCount++

			// Update candidate features with weighted average
			scd.updateCandidateFeatures(features)
		}

		// Check if candidate is stable enough to declare change
		duration := now.Sub(scd.candidateStartTime)
		avgConfidence := scd.confidenceAccumulator / float64(scd.stableFrameCount)

		if duration >= scd.minSegmentDuration && avgConfidence > scd.changeThreshold {
			return true
		}
	} else {
		// Similarity above threshold - reset candidate
		scd.changeCandidate = false
		scd.confidenceAccumulator = 0.0
		scd.stableFrameCount = 0
	}

	return false
}

// handleChangeDetection handles when a change is detected
func (scd *SpeakerChangeDetector) handleChangeDetection(features *VoiceFeatures) {
	scd.stats.mutex.Lock()
	scd.stats.DetectedChanges++
	scd.stats.mutex.Unlock()

	// Update last features to the candidate features (smoother transition)
	if scd.candidateFeatures != nil {
		scd.lastFeatures = scd.candidateFeatures
	} else {
		scd.lastFeatures = scd.copyFeatures(features)
	}

	// Reset change detection state
	scd.changeCandidate = false
	scd.confidenceAccumulator = 0.0
	scd.stableFrameCount = 0

	// Clear feature window for fresh start
	for i := range scd.featureWindow {
		scd.featureWindow[i] = nil
	}
	scd.windowIndex = 0

	scd.logger.WithFields(logrus.Fields{
		"confidence": scd.confidenceAccumulator / float64(scd.stableFrameCount),
		"duration":   time.Since(scd.candidateStartTime),
	}).Debug("Speaker change detected")
}

// handleNoChange handles when no change is detected
func (scd *SpeakerChangeDetector) handleNoChange(features *VoiceFeatures) {
	// Update last features with exponential moving average for stability
	alpha := 0.1 // Learning rate
	scd.updateFeaturesEMA(scd.lastFeatures, features, alpha)
}

// updateCandidateFeatures updates candidate features with weighted average
func (scd *SpeakerChangeDetector) updateCandidateFeatures(features *VoiceFeatures) {
	if scd.candidateFeatures == nil {
		scd.candidateFeatures = scd.copyFeatures(features)
		return
	}

	// Use exponential moving average with higher learning rate for candidates
	alpha := 0.3
	scd.updateFeaturesEMA(scd.candidateFeatures, features, alpha)
}

// updateFeaturesEMA updates features using exponential moving average
func (scd *SpeakerChangeDetector) updateFeaturesEMA(target, source *VoiceFeatures, alpha float64) {
	if target == nil || source == nil {
		return
	}

	target.F0Mean = target.F0Mean*(1-alpha) + source.F0Mean*alpha
	target.F0Std = target.F0Std*(1-alpha) + source.F0Std*alpha
	target.F0Range = target.F0Range*(1-alpha) + source.F0Range*alpha
	target.SpectralCentroid = target.SpectralCentroid*(1-alpha) + source.SpectralCentroid*alpha
	target.SpectralRolloff = target.SpectralRolloff*(1-alpha) + source.SpectralRolloff*alpha
	target.SpectralFlux = target.SpectralFlux*(1-alpha) + source.SpectralFlux*alpha
	target.Energy = target.Energy*(1-alpha) + source.Energy*alpha
	target.ZeroCrossingRate = target.ZeroCrossingRate*(1-alpha) + source.ZeroCrossingRate*alpha

	// Update MFCC coefficients
	if len(target.MFCC) == len(source.MFCC) {
		for i := range target.MFCC {
			target.MFCC[i] = target.MFCC[i]*(1-alpha) + source.MFCC[i]*alpha
		}
	}

	// Update formants
	target.F1 = target.F1*(1-alpha) + source.F1*alpha
	target.F2 = target.F2*(1-alpha) + source.F2*alpha
	target.F3 = target.F3*(1-alpha) + source.F3*alpha

	target.UpdateCount++
	target.LastUpdate = time.Now()
}

// addToWindow adds features to the sliding window
func (scd *SpeakerChangeDetector) addToWindow(features *VoiceFeatures) {
	scd.featureWindow[scd.windowIndex] = scd.copyFeatures(features)
	scd.windowIndex = (scd.windowIndex + 1) % scd.windowSize
}

// updateSimilarityHistory updates the recent similarity history for adaptation
func (scd *SpeakerChangeDetector) updateSimilarityHistory(similarity float64) {
	scd.recentSimilarities = append(scd.recentSimilarities, similarity)
	if len(scd.recentSimilarities) > scd.similarityHistory {
		scd.recentSimilarities = scd.recentSimilarities[1:]
	}

	// Update average similarity statistic
	if len(scd.recentSimilarities) > 0 {
		sum := 0.0
		for _, sim := range scd.recentSimilarities {
			sum += sim
		}
		avgSim := sum / float64(len(scd.recentSimilarities))

		scd.stats.mutex.Lock()
		scd.stats.AverageSimilarity = avgSim
		scd.stats.mutex.Unlock()
	}
}

// updateAdaptiveThreshold adapts the similarity threshold based on recent history
func (scd *SpeakerChangeDetector) updateAdaptiveThreshold() {
	if len(scd.recentSimilarities) < 10 {
		return // Need enough history
	}

	// Calculate statistics of recent similarities
	sum := 0.0
	min := 1.0
	max := 0.0

	for _, sim := range scd.recentSimilarities {
		sum += sim
		if sim < min {
			min = sim
		}
		if sim > max {
			max = sim
		}
	}

	mean := sum / float64(len(scd.recentSimilarities))

	// Calculate standard deviation
	variance := 0.0
	for _, sim := range scd.recentSimilarities {
		diff := sim - mean
		variance += diff * diff
	}
	stddev := math.Sqrt(variance / float64(len(scd.recentSimilarities)))

	// Adaptive threshold: mean - 1.5 * stddev (more sensitive when similarities vary)
	newThreshold := mean - 1.5*stddev

	// Clamp to reasonable bounds
	if newThreshold < 0.3 {
		newThreshold = 0.3
	}
	if newThreshold > 0.8 {
		newThreshold = 0.8
	}

	// Smooth threshold changes
	alpha := 0.1
	scd.adaptiveThreshold = scd.adaptiveThreshold*(1-alpha) + newThreshold*alpha
}

// createCacheKey creates a cache key for similarity comparison
func (scd *SpeakerChangeDetector) createCacheKey(f1, f2 *VoiceFeatures) string {
	// Create a simple hash-like key from features
	// This is simplified - in production, use proper hashing
	return fmt.Sprintf("%.2f_%.2f_%.2f_%.2f", f1.F0Mean, f1.SpectralCentroid, f2.F0Mean, f2.SpectralCentroid)
}

// copyFeatures creates a deep copy of voice features
func (scd *SpeakerChangeDetector) copyFeatures(features *VoiceFeatures) *VoiceFeatures {
	if features == nil {
		return nil
	}

	featuresCopy := *features

	// Deep copy MFCC slice
	if len(features.MFCC) > 0 {
		featuresCopy.MFCC = make([]float64, len(features.MFCC))
		copy(featuresCopy.MFCC, features.MFCC)
	}

	return &featuresCopy
}

// performPeriodicCleanup performs periodic cleanup of cache and statistics
func (scd *SpeakerChangeDetector) performPeriodicCleanup() {
	now := time.Now()
	if now.Sub(scd.lastCleanup) < 30*time.Second {
		return
	}

	scd.lastCleanup = now

	// Clean up cache if it's getting too large
	if len(scd.comparisonCache) > scd.cacheMaxSize*2 {
		// Clear half of the cache randomly
		count := 0
		target := len(scd.comparisonCache) / 2
		for key := range scd.comparisonCache {
			delete(scd.comparisonCache, key)
			count++
			if count >= target {
				break
			}
		}
	}
}

// GetStats returns change detection statistics
func (scd *SpeakerChangeDetector) GetStats() *ChangeDetectionStats {
	scd.stats.mutex.RLock()
	defer scd.stats.mutex.RUnlock()

	statsCopy := &ChangeDetectionStats{
		TotalComparisons:  scd.stats.TotalComparisons,
		DetectedChanges:   scd.stats.DetectedChanges,
		FalsePositives:    scd.stats.FalsePositives,
		FalseNegatives:    scd.stats.FalseNegatives,
		AverageSimilarity: scd.stats.AverageSimilarity,
		ProcessingTime:    scd.stats.ProcessingTime,
		CacheHits:         scd.stats.CacheHits,
		CacheMisses:       scd.stats.CacheMisses,
		LastReset:         scd.stats.LastReset,
	}
	return statsCopy
}

// Reset resets the detector state
func (scd *SpeakerChangeDetector) Reset() {
	scd.mutex.Lock()
	defer scd.mutex.Unlock()

	scd.lastFeatures = nil
	scd.changeCandidate = false
	scd.confidenceAccumulator = 0.0
	scd.stableFrameCount = 0
	scd.candidateFeatures = nil

	// Clear feature window
	for i := range scd.featureWindow {
		scd.featureWindow[i] = nil
	}
	scd.windowIndex = 0

	// Clear similarity history
	scd.recentSimilarities = scd.recentSimilarities[:0]
	scd.adaptiveThreshold = scd.similarityThreshold

	// Clear cache
	scd.comparisonCache = make(map[string]float64)

	// Reset statistics
	scd.stats.mutex.Lock()
	scd.stats = &ChangeDetectionStats{LastReset: time.Now()}
	scd.stats.mutex.Unlock()
}

// Cleanup performs cleanup operations
func (scd *SpeakerChangeDetector) Cleanup() {
	scd.mutex.Lock()
	defer scd.mutex.Unlock()

	// Clear all data structures
	scd.featureWindow = nil
	scd.recentSimilarities = nil
	scd.comparisonCache = nil
	scd.lastFeatures = nil
	scd.candidateFeatures = nil

	scd.logger.Debug("Speaker change detector cleaned up")
}
