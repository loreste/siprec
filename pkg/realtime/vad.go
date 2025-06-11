package realtime

import (
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// VoiceActivityDetector detects speech segments in audio streams
type VoiceActivityDetector struct {
	logger *logrus.Entry
	
	// Detection thresholds
	energyThreshold      float64
	zeroCrossingThreshold float64
	spectralThreshold    float64
	
	// Timing constraints
	minSpeechDuration    time.Duration
	maxSilenceDuration   time.Duration
	
	// State tracking
	isVoiceActive        bool
	speechStartTime      time.Time
	lastActivity         time.Time
	silenceStartTime     time.Time
	
	// Signal history for smoothing
	energyHistory        []float64
	zcHistory            []float64
	historySize          int
	historyIndex         int
	
	// Adaptive thresholds
	backgroundNoise      float64
	noiseEstimateCount   int64
	adaptiveThreshold    float64
	
	// Performance optimization
	frameSize            int
	hopSize              int
	
	// Thread safety
	mutex                sync.RWMutex
	
	// Statistics
	stats                *VADStats
}

// VADStats tracks voice activity detection performance
type VADStats struct {
	mutex              sync.RWMutex
	TotalFrames        int64     `json:"total_frames"`
	VoiceFrames        int64     `json:"voice_frames"`
	SilenceFrames      int64     `json:"silence_frames"`
	SpeechSegments     int64     `json:"speech_segments"`
	TotalSpeechTime    int64     `json:"total_speech_time_ms"`
	AverageSegmentTime int64     `json:"average_segment_time_ms"`
	ProcessingTime     int64     `json:"processing_time_ms"`
	FalsePositives     int64     `json:"false_positives"`
	FalseNegatives     int64     `json:"false_negatives"`
	LastReset          time.Time `json:"last_reset"`
}

// NewVoiceActivityDetector creates a new voice activity detector
func NewVoiceActivityDetector() *VoiceActivityDetector {
	logger := logrus.New()
	vad := &VoiceActivityDetector{
		logger:               logger.WithField("component", "vad"),
		energyThreshold:      0.005,  // Base energy threshold
		zeroCrossingThreshold: 0.1,   // Zero crossing rate threshold
		spectralThreshold:    0.3,    // Spectral activity threshold
		minSpeechDuration:    100 * time.Millisecond,
		maxSilenceDuration:   500 * time.Millisecond,
		historySize:          10,     // 10 frame history for smoothing
		frameSize:            512,    // 512 samples per frame
		hopSize:              256,    // 50% overlap
		backgroundNoise:      0.001,  // Initial noise estimate
		adaptiveThreshold:    0.005,  // Initial adaptive threshold
		stats:                &VADStats{LastReset: time.Now()},
	}
	
	// Initialize history buffers
	vad.energyHistory = make([]float64, vad.historySize)
	vad.zcHistory = make([]float64, vad.historySize)
	
	return vad
}

// ProcessSamples processes audio samples and returns voice activity status
func (vad *VoiceActivityDetector) ProcessSamples(samples []float64) bool {
	if len(samples) == 0 {
		return false
	}
	
	startTime := time.Now()
	defer func() {
		vad.stats.mutex.Lock()
		vad.stats.ProcessingTime += time.Since(startTime).Nanoseconds() / 1e6
		vad.stats.TotalFrames++
		vad.stats.mutex.Unlock()
	}()
	
	vad.mutex.Lock()
	defer vad.mutex.Unlock()
	
	// Extract features from audio
	features := vad.extractFeatures(samples)
	
	// Update background noise estimate
	vad.updateNoiseEstimate(features.energy)
	
	// Apply multiple detection criteria
	energyActive := vad.isEnergyActive(features.energy)
	spectralActive := vad.isSpectralActive(features)
	temporalActive := vad.isTemporalActive(features.zeroCrossingRate)
	
	// Combine criteria with weights
	confidence := vad.calculateConfidence(energyActive, spectralActive, temporalActive)
	isActive := confidence > 0.5
	
	// Apply temporal smoothing
	isActive = vad.applyTemporalSmoothing(isActive)
	
	// Update state and statistics
	vad.updateState(isActive)
	
	return vad.isVoiceActive
}

// AudioFeatures represents extracted audio features for VAD
type AudioFeatures struct {
	energy            float64
	zeroCrossingRate  float64
	spectralCentroid  float64
	spectralEntropy   float64
	autocorrelation   float64
	spectralFlatness  float64
}

// extractFeatures extracts audio features for voice activity detection
func (vad *VoiceActivityDetector) extractFeatures(samples []float64) *AudioFeatures {
	features := &AudioFeatures{}
	
	// Calculate RMS energy
	features.energy = vad.calculateEnergy(samples)
	
	// Calculate zero crossing rate
	features.zeroCrossingRate = vad.calculateZeroCrossingRate(samples)
	
	// Calculate spectral features (simplified)
	features.spectralCentroid = vad.calculateSpectralCentroid(samples)
	features.spectralEntropy = vad.calculateSpectralEntropy(samples)
	features.spectralFlatness = vad.calculateSpectralFlatness(samples)
	
	// Calculate autocorrelation for periodicity detection
	features.autocorrelation = vad.calculateAutocorrelation(samples)
	
	return features
}

// calculateEnergy calculates RMS energy of the signal
func (vad *VoiceActivityDetector) calculateEnergy(samples []float64) float64 {
	if len(samples) == 0 {
		return 0.0
	}
	
	sumSquares := 0.0
	for _, sample := range samples {
		sumSquares += sample * sample
	}
	
	return math.Sqrt(sumSquares / float64(len(samples)))
}

// calculateZeroCrossingRate calculates the zero crossing rate
func (vad *VoiceActivityDetector) calculateZeroCrossingRate(samples []float64) float64 {
	if len(samples) < 2 {
		return 0.0
	}
	
	crossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] >= 0) != (samples[i] >= 0) {
			crossings++
		}
	}
	
	return float64(crossings) / float64(len(samples)-1)
}

// calculateSpectralCentroid calculates spectral centroid (simplified)
func (vad *VoiceActivityDetector) calculateSpectralCentroid(samples []float64) float64 {
	// Simplified spectral centroid calculation
	// In a full implementation, this would use FFT
	
	weightedSum := 0.0
	totalMagnitude := 0.0
	
	// Use a simple approximation with time-domain analysis
	for i, sample := range samples {
		magnitude := math.Abs(sample)
		freq := float64(i) // Simplified frequency approximation
		weightedSum += freq * magnitude
		totalMagnitude += magnitude
	}
	
	if totalMagnitude > 0 {
		return weightedSum / totalMagnitude
	}
	return 0.0
}

// calculateSpectralEntropy calculates spectral entropy (simplified)
func (vad *VoiceActivityDetector) calculateSpectralEntropy(samples []float64) float64 {
	// Simplified entropy calculation
	if len(samples) == 0 {
		return 0.0
	}
	
	// Calculate energy distribution
	energySum := 0.0
	for _, sample := range samples {
		energySum += sample * sample
	}
	
	if energySum == 0 {
		return 0.0
	}
	
	// Calculate normalized energy distribution entropy
	entropy := 0.0
	for _, sample := range samples {
		energy := (sample * sample) / energySum
		if energy > 0 {
			entropy -= energy * math.Log2(energy)
		}
	}
	
	return entropy
}

// calculateSpectralFlatness calculates spectral flatness measure
func (vad *VoiceActivityDetector) calculateSpectralFlatness(samples []float64) float64 {
	// Simplified spectral flatness (Wiener entropy)
	if len(samples) == 0 {
		return 0.0
	}
	
	geometricMean := 1.0
	arithmeticMean := 0.0
	validSamples := 0
	
	for _, sample := range samples {
		magnitude := math.Abs(sample)
		if magnitude > 1e-10 { // Avoid log(0)
			geometricMean *= magnitude
			arithmeticMean += magnitude
			validSamples++
		}
	}
	
	if validSamples == 0 {
		return 0.0
	}
	
	geometricMean = math.Pow(geometricMean, 1.0/float64(validSamples))
	arithmeticMean /= float64(validSamples)
	
	if arithmeticMean > 0 {
		return geometricMean / arithmeticMean
	}
	return 0.0
}

// calculateAutocorrelation calculates autocorrelation for periodicity
func (vad *VoiceActivityDetector) calculateAutocorrelation(samples []float64) float64 {
	if len(samples) < 2 {
		return 0.0
	}
	
	// Calculate autocorrelation at lag 1 (simplified)
	correlation := 0.0
	norm1 := 0.0
	norm2 := 0.0
	
	for i := 0; i < len(samples)-1; i++ {
		correlation += samples[i] * samples[i+1]
		norm1 += samples[i] * samples[i]
		norm2 += samples[i+1] * samples[i+1]
	}
	
	normProduct := math.Sqrt(norm1 * norm2)
	if normProduct > 0 {
		return correlation / normProduct
	}
	return 0.0
}

// updateNoiseEstimate updates the background noise estimate
func (vad *VoiceActivityDetector) updateNoiseEstimate(energy float64) {
	// Only update noise estimate when not currently active
	if !vad.isVoiceActive {
		alpha := 0.1 // Learning rate
		vad.backgroundNoise = vad.backgroundNoise*(1-alpha) + energy*alpha
		vad.noiseEstimateCount++
		
		// Update adaptive threshold based on noise level
		vad.adaptiveThreshold = vad.backgroundNoise * 3.0 // 3x noise level
		if vad.adaptiveThreshold < vad.energyThreshold {
			vad.adaptiveThreshold = vad.energyThreshold
		}
	}
}

// isEnergyActive checks if energy indicates voice activity
func (vad *VoiceActivityDetector) isEnergyActive(energy float64) bool {
	return energy > vad.adaptiveThreshold
}

// isSpectralActive checks if spectral features indicate voice activity
func (vad *VoiceActivityDetector) isSpectralActive(features *AudioFeatures) bool {
	// Voice typically has:
	// - Higher spectral entropy (more complex spectrum)
	// - Lower spectral flatness (more harmonic structure)
	// - Moderate autocorrelation (periodic structure)
	
	entropyActive := features.spectralEntropy > 2.0
	flatnessActive := features.spectralFlatness < 0.5
	correlationActive := math.Abs(features.autocorrelation) > 0.1
	
	// At least 2 out of 3 criteria should be met
	activeCount := 0
	if entropyActive {
		activeCount++
	}
	if flatnessActive {
		activeCount++
	}
	if correlationActive {
		activeCount++
	}
	
	return activeCount >= 2
}

// isTemporalActive checks if temporal features indicate voice activity
func (vad *VoiceActivityDetector) isTemporalActive(zcr float64) bool {
	// Voice typically has moderate zero crossing rate
	// Too high = noise, too low = silence/tone
	return zcr > 0.01 && zcr < vad.zeroCrossingThreshold
}

// calculateConfidence calculates overall voice activity confidence
func (vad *VoiceActivityDetector) calculateConfidence(energyActive, spectralActive, temporalActive bool) float64 {
	// Weighted combination of criteria
	weights := []float64{0.5, 0.3, 0.2} // Energy has highest weight
	activities := []bool{energyActive, spectralActive, temporalActive}
	
	confidence := 0.0
	for i, active := range activities {
		if active {
			confidence += weights[i]
		}
	}
	
	return confidence
}

// applyTemporalSmoothing applies temporal smoothing to reduce false triggers
func (vad *VoiceActivityDetector) applyTemporalSmoothing(currentActive bool) bool {
	// Update history
	vad.energyHistory[vad.historyIndex] = 0.0
	if currentActive {
		vad.energyHistory[vad.historyIndex] = 1.0
	}
	vad.historyIndex = (vad.historyIndex + 1) % vad.historySize
	
	// Calculate smoothed activity
	activeCount := 0
	for _, active := range vad.energyHistory {
		if active > 0.5 {
			activeCount++
		}
	}
	
	// Require majority vote for activity
	return float64(activeCount) / float64(vad.historySize) > 0.4
}

// updateState updates VAD state and statistics
func (vad *VoiceActivityDetector) updateState(isActive bool) {
	now := time.Now()
	
	// State transition logic
	if isActive && !vad.isVoiceActive {
		// Transition to active
		if now.Sub(vad.silenceStartTime) > vad.maxSilenceDuration {
			vad.isVoiceActive = true
			vad.speechStartTime = now
			vad.lastActivity = now
			
			vad.stats.mutex.Lock()
			vad.stats.SpeechSegments++
			vad.stats.mutex.Unlock()
		}
	} else if !isActive && vad.isVoiceActive {
		// Transition to inactive
		if now.Sub(vad.lastActivity) > vad.minSpeechDuration {
			vad.isVoiceActive = false
			vad.silenceStartTime = now
			
			// Update speech time statistics
			speechDuration := now.Sub(vad.speechStartTime)
			vad.stats.mutex.Lock()
			vad.stats.TotalSpeechTime += speechDuration.Nanoseconds() / 1e6
			if vad.stats.SpeechSegments > 0 {
				vad.stats.AverageSegmentTime = vad.stats.TotalSpeechTime / vad.stats.SpeechSegments
			}
			vad.stats.mutex.Unlock()
		}
	} else if isActive && vad.isVoiceActive {
		// Continue active
		vad.lastActivity = now
	} else if !isActive && !vad.isVoiceActive {
		// Continue inactive (silence)
		// No action needed
	}
	
	// Update frame statistics
	vad.stats.mutex.Lock()
	if isActive {
		vad.stats.VoiceFrames++
	} else {
		vad.stats.SilenceFrames++
	}
	vad.stats.mutex.Unlock()
}

// IsVoiceActive returns current voice activity status
func (vad *VoiceActivityDetector) IsVoiceActive() bool {
	vad.mutex.RLock()
	defer vad.mutex.RUnlock()
	return vad.isVoiceActive
}

// GetStats returns VAD statistics
func (vad *VoiceActivityDetector) GetStats() *VADStats {
	vad.stats.mutex.RLock()
	defer vad.stats.mutex.RUnlock()
	
	statsCopy := *vad.stats
	return &statsCopy
}

// SetThresholds updates detection thresholds
func (vad *VoiceActivityDetector) SetThresholds(energy, zcr, spectral float64) {
	vad.mutex.Lock()
	defer vad.mutex.Unlock()
	
	if energy > 0 {
		vad.energyThreshold = energy
	}
	if zcr > 0 {
		vad.zeroCrossingThreshold = zcr
	}
	if spectral > 0 {
		vad.spectralThreshold = spectral
	}
}

// Reset resets the VAD state and statistics
func (vad *VoiceActivityDetector) Reset() {
	vad.mutex.Lock()
	defer vad.mutex.Unlock()
	
	vad.isVoiceActive = false
	vad.speechStartTime = time.Time{}
	vad.lastActivity = time.Time{}
	vad.silenceStartTime = time.Now()
	
	// Clear history
	for i := range vad.energyHistory {
		vad.energyHistory[i] = 0.0
	}
	for i := range vad.zcHistory {
		vad.zcHistory[i] = 0.0
	}
	vad.historyIndex = 0
	
	// Reset statistics
	vad.stats.mutex.Lock()
	vad.stats = &VADStats{LastReset: time.Now()}
	vad.stats.mutex.Unlock()
}

// Cleanup performs cleanup operations
func (vad *VoiceActivityDetector) Cleanup() {
	vad.mutex.Lock()
	defer vad.mutex.Unlock()
	
	// Clear buffers
	vad.energyHistory = nil
	vad.zcHistory = nil
	
	vad.logger.Debug("VAD cleaned up")
}