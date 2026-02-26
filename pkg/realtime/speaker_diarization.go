package realtime

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SpeakerDiarizer performs real-time speaker identification and diarization
type SpeakerDiarizer struct {
	logger      *logrus.Entry
	maxSpeakers int

	// Speaker profiles and features
	speakers       map[string]*SpeakerProfile
	speakersMux    sync.RWMutex
	currentSpeaker string

	// Audio feature extraction
	featureExtractor *AudioFeatureExtractor

	// Voice activity detection
	vadProcessor *VoiceActivityDetector

	// Speaker change detection
	changeDetector *SpeakerChangeDetector

	// Performance optimization
	processingPool *WorkerPool
	bufferPool     sync.Pool

	// Memory management
	lastCleanup     time.Time
	cleanupInterval time.Duration
	maxProfileAge   time.Duration

	// Statistics
	stats *DiarizationStats
}

// SpeakerProfile represents a speaker's voice characteristics
type SpeakerProfile struct {
	ID              string                 `json:"id"`
	Label           string                 `json:"label"`
	Features        *VoiceFeatures         `json:"features"`
	FirstSeen       time.Time              `json:"first_seen"`
	LastSeen        time.Time              `json:"last_seen"`
	TotalSpeechTime time.Duration          `json:"total_speech_time"`
	SegmentCount    int                    `json:"segment_count"`
	Confidence      float64                `json:"confidence"`
	Metadata        map[string]interface{} `json:"metadata"`

	// Thread safety
	mutex sync.RWMutex
}

// VoiceFeatures represents extracted voice characteristics
type VoiceFeatures struct {
	// Fundamental frequency features
	F0Mean  float64 `json:"f0_mean"`
	F0Std   float64 `json:"f0_std"`
	F0Range float64 `json:"f0_range"`

	// Spectral features
	SpectralCentroid float64   `json:"spectral_centroid"`
	SpectralRolloff  float64   `json:"spectral_rolloff"`
	SpectralFlux     float64   `json:"spectral_flux"`
	MFCC             []float64 `json:"mfcc"` // Mel-frequency cepstral coefficients

	// Prosodic features
	SpeechRate    float64 `json:"speech_rate"`
	PauseDuration float64 `json:"pause_duration"`
	VoicedRatio   float64 `json:"voiced_ratio"`

	// Formant frequencies
	F1 float64 `json:"f1"` // First formant
	F2 float64 `json:"f2"` // Second formant
	F3 float64 `json:"f3"` // Third formant

	// Energy and dynamics
	Energy           float64 `json:"energy"`
	ZeroCrossingRate float64 `json:"zero_crossing_rate"`

	// Quality metrics
	Confidence  float64   `json:"confidence"`
	UpdateCount int       `json:"update_count"`
	LastUpdate  time.Time `json:"last_update"`
}

// DiarizationStats tracks speaker diarization performance
type DiarizationStats struct {
	mutex                sync.RWMutex
	TotalSegments        int64     `json:"total_segments"`
	SpeakerChanges       int64     `json:"speaker_changes"`
	ProcessingTime       int64     `json:"processing_time_ms"`
	FeaturesExtracted    int64     `json:"features_extracted"`
	IdentificationErrors int64     `json:"identification_errors"`
	MemoryUsage          int64     `json:"memory_usage_bytes"`
	LastReset            time.Time `json:"last_reset"`
}

// AudioFeatureExtractor extracts voice features from audio data
type AudioFeatureExtractor struct {
	sampleRate int
	frameSize  int
	hopSize    int
	windowType string

	// FFT and analysis
	fftSize   int
	melBanks  int
	mfccCount int

	// Buffers for processing
	window     []float64
	fftBuffer  []complex128
	melFilters [][]float64

	// Thread safety
	mutex sync.Mutex
}

// NewSpeakerDiarizer creates a new speaker diarization system
func NewSpeakerDiarizer(maxSpeakers int, logger *logrus.Logger) *SpeakerDiarizer {
	sd := &SpeakerDiarizer{
		logger:          logger.WithField("component", "speaker_diarizer"),
		maxSpeakers:     maxSpeakers,
		speakers:        make(map[string]*SpeakerProfile),
		cleanupInterval: 5 * time.Minute,
		maxProfileAge:   30 * time.Minute,
		lastCleanup:     time.Now(),
		stats:           &DiarizationStats{LastReset: time.Now()},
	}

	// Initialize components
	sd.featureExtractor = NewAudioFeatureExtractor(16000) // 16kHz sample rate
	sd.vadProcessor = NewVoiceActivityDetector()
	sd.changeDetector = NewSpeakerChangeDetector()
	sd.processingPool = NewWorkerPool(2, logger) // 2 workers for feature extraction

	// Initialize buffer pool for memory efficiency
	sd.bufferPool = sync.Pool{
		New: func() interface{} {
			return make([]float64, 1024) // 1024 sample buffer
		},
	}

	return sd
}

// ProcessAudio processes audio data for speaker diarization
func (sd *SpeakerDiarizer) ProcessAudio(audioData []byte, transcript *TranscriptionEventData) {
	if len(audioData) == 0 {
		return
	}

	startTime := time.Now()
	defer func() {
		sd.stats.mutex.Lock()
		sd.stats.ProcessingTime += time.Since(startTime).Nanoseconds() / 1e6
		sd.stats.TotalSegments++
		sd.stats.mutex.Unlock()
	}()

	// Convert audio data to float samples
	samples := sd.bytesToFloat64(audioData)
	if len(samples) == 0 {
		return
	}

	// Voice activity detection
	isVoice := sd.vadProcessor.ProcessSamples(samples)
	if !isVoice {
		return // Skip non-speech segments
	}

	// Extract voice features asynchronously
	sd.processingPool.Submit(func() {
		sd.extractAndProcessFeatures(samples, transcript)
	})

	// Periodic cleanup
	sd.performPeriodicCleanup()
}

// extractAndProcessFeatures extracts features and processes speaker identification
func (sd *SpeakerDiarizer) extractAndProcessFeatures(samples []float64, transcript *TranscriptionEventData) {
	defer func() {
		if r := recover(); r != nil {
			sd.logger.WithField("panic", r).Error("Panic in feature extraction")
			sd.stats.mutex.Lock()
			sd.stats.IdentificationErrors++
			sd.stats.mutex.Unlock()
		}
	}()

	// Extract voice features
	features := sd.featureExtractor.ExtractFeatures(samples)
	if features == nil {
		return
	}

	sd.stats.mutex.Lock()
	sd.stats.FeaturesExtracted++
	sd.stats.mutex.Unlock()

	// Detect speaker changes
	speakerChanged := sd.changeDetector.ProcessFeatures(features)

	// Identify or create speaker
	speakerID := sd.identifyOrCreateSpeaker(features, speakerChanged)

	// Update transcript with speaker information
	if transcript != nil {
		transcript.SpeakerID = speakerID
		if profile := sd.getSpeakerProfile(speakerID); profile != nil {
			transcript.SpeakerLabel = profile.Label
			transcript.SpeakerCount = len(sd.speakers)
		}
	}

	// Update speaker profile
	sd.updateSpeakerProfile(speakerID, features, transcript)

	// Track speaker changes
	if speakerChanged && speakerID != sd.currentSpeaker {
		sd.currentSpeaker = speakerID
		sd.stats.mutex.Lock()
		sd.stats.SpeakerChanges++
		sd.stats.mutex.Unlock()

		sd.logger.WithFields(logrus.Fields{
			"speaker_id": speakerID,
			"confidence": features.Confidence,
		}).Debug("Speaker change detected")
	}
}

// identifyOrCreateSpeaker identifies an existing speaker or creates a new one
func (sd *SpeakerDiarizer) identifyOrCreateSpeaker(features *VoiceFeatures, forceNew bool) string {
	sd.speakersMux.RLock()
	existingSpeakers := make([]*SpeakerProfile, 0, len(sd.speakers))
	for _, profile := range sd.speakers {
		existingSpeakers = append(existingSpeakers, profile)
	}
	sd.speakersMux.RUnlock()

	if !forceNew && len(existingSpeakers) > 0 {
		// Find best matching speaker
		bestMatch := ""
		bestSimilarity := 0.0

		for _, profile := range existingSpeakers {
			similarity := sd.calculateSimilarity(features, profile.Features)
			if similarity > bestSimilarity && similarity > 0.7 { // 70% similarity threshold
				bestSimilarity = similarity
				bestMatch = profile.ID
			}
		}

		if bestMatch != "" {
			// Update confidence based on similarity
			features.Confidence = bestSimilarity
			return bestMatch
		}
	}

	// Create new speaker if we haven't reached the limit
	if len(existingSpeakers) < sd.maxSpeakers {
		return sd.createNewSpeaker(features)
	}

	// If at limit, assign to the least confident existing speaker
	leastConfident := ""
	leastConfidenceScore := 1.0

	for _, profile := range existingSpeakers {
		if profile.Confidence < leastConfidenceScore {
			leastConfidenceScore = profile.Confidence
			leastConfident = profile.ID
		}
	}

	return leastConfident
}

// createNewSpeaker creates a new speaker profile
func (sd *SpeakerDiarizer) createNewSpeaker(features *VoiceFeatures) string {
	sd.speakersMux.Lock()
	defer sd.speakersMux.Unlock()

	speakerID := fmt.Sprintf("speaker_%d", len(sd.speakers)+1)
	now := time.Now()

	profile := &SpeakerProfile{
		ID:              speakerID,
		Label:           speakerID,
		Features:        features,
		FirstSeen:       now,
		LastSeen:        now,
		TotalSpeechTime: 0,
		SegmentCount:    1,
		Confidence:      features.Confidence,
		Metadata:        make(map[string]interface{}),
	}

	sd.speakers[speakerID] = profile

	sd.logger.WithFields(logrus.Fields{
		"speaker_id":     speakerID,
		"total_speakers": len(sd.speakers),
	}).Info("New speaker created")

	return speakerID
}

// updateSpeakerProfile updates a speaker's profile with new data
func (sd *SpeakerDiarizer) updateSpeakerProfile(speakerID string, features *VoiceFeatures, transcript *TranscriptionEventData) {
	profile := sd.getSpeakerProfile(speakerID)
	if profile == nil {
		return
	}

	profile.mutex.Lock()
	defer profile.mutex.Unlock()

	now := time.Now()
	profile.LastSeen = now
	profile.SegmentCount++

	// Update features with exponential moving average
	sd.updateFeaturesEMA(profile.Features, features, 0.1) // 10% learning rate

	// Update speech time estimation
	if transcript != nil && transcript.EndTime > transcript.StartTime {
		segmentDuration := time.Duration((transcript.EndTime - transcript.StartTime) * float64(time.Second))
		profile.TotalSpeechTime += segmentDuration
	}

	// Update confidence
	profile.Confidence = (profile.Confidence*0.9 + features.Confidence*0.1)
}

// getSpeakerProfile safely retrieves a speaker profile
func (sd *SpeakerDiarizer) getSpeakerProfile(speakerID string) *SpeakerProfile {
	sd.speakersMux.RLock()
	defer sd.speakersMux.RUnlock()
	return sd.speakers[speakerID]
}

// calculateSimilarity calculates similarity between two feature sets
func (sd *SpeakerDiarizer) calculateSimilarity(features1, features2 *VoiceFeatures) float64 {
	if features1 == nil || features2 == nil {
		return 0.0
	}

	// Calculate weighted similarity across multiple features
	similarities := []float64{
		sd.calculateF0Similarity(features1, features2),       // 30%
		sd.calculateSpectralSimilarity(features1, features2), // 25%
		sd.calculateMFCCSimilarity(features1, features2),     // 25%
		sd.calculateFormantSimilarity(features1, features2),  // 20%
	}

	weights := []float64{0.3, 0.25, 0.25, 0.2}

	totalSimilarity := 0.0
	for i, sim := range similarities {
		totalSimilarity += sim * weights[i]
	}

	return totalSimilarity
}

// calculateF0Similarity calculates fundamental frequency similarity
func (sd *SpeakerDiarizer) calculateF0Similarity(f1, f2 *VoiceFeatures) float64 {
	if f1.F0Mean == 0 || f2.F0Mean == 0 {
		return 0.0
	}

	meanDiff := math.Abs(f1.F0Mean-f2.F0Mean) / math.Max(f1.F0Mean, f2.F0Mean)
	stdDiff := math.Abs(f1.F0Std-f2.F0Std) / math.Max(f1.F0Std, f2.F0Std)

	return math.Max(0, 1.0-(meanDiff+stdDiff)/2.0)
}

// calculateSpectralSimilarity calculates spectral feature similarity
func (sd *SpeakerDiarizer) calculateSpectralSimilarity(f1, f2 *VoiceFeatures) float64 {
	centroidDiff := math.Abs(f1.SpectralCentroid-f2.SpectralCentroid) /
		math.Max(f1.SpectralCentroid, f2.SpectralCentroid)
	rolloffDiff := math.Abs(f1.SpectralRolloff-f2.SpectralRolloff) /
		math.Max(f1.SpectralRolloff, f2.SpectralRolloff)

	return math.Max(0, 1.0-(centroidDiff+rolloffDiff)/2.0)
}

// calculateMFCCSimilarity calculates MFCC similarity using cosine similarity
func (sd *SpeakerDiarizer) calculateMFCCSimilarity(f1, f2 *VoiceFeatures) float64 {
	if len(f1.MFCC) != len(f2.MFCC) || len(f1.MFCC) == 0 {
		return 0.0
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
		return 0.0
	}

	return dotProduct / (math.Sqrt(norm1) * math.Sqrt(norm2))
}

// calculateFormantSimilarity calculates formant frequency similarity
func (sd *SpeakerDiarizer) calculateFormantSimilarity(f1, f2 *VoiceFeatures) float64 {
	if f1.F1 == 0 || f1.F2 == 0 || f2.F1 == 0 || f2.F2 == 0 {
		return 0.0
	}

	f1Diff := math.Abs(f1.F1-f2.F1) / math.Max(f1.F1, f2.F1)
	f2Diff := math.Abs(f1.F2-f2.F2) / math.Max(f1.F2, f2.F2)
	f3Diff := math.Abs(f1.F3-f2.F3) / math.Max(f1.F3, f2.F3)

	return math.Max(0, 1.0-(f1Diff+f2Diff+f3Diff)/3.0)
}

// updateFeaturesEMA updates features using exponential moving average
func (sd *SpeakerDiarizer) updateFeaturesEMA(target, source *VoiceFeatures, alpha float64) {
	target.F0Mean = target.F0Mean*(1-alpha) + source.F0Mean*alpha
	target.F0Std = target.F0Std*(1-alpha) + source.F0Std*alpha
	target.SpectralCentroid = target.SpectralCentroid*(1-alpha) + source.SpectralCentroid*alpha
	target.SpectralRolloff = target.SpectralRolloff*(1-alpha) + source.SpectralRolloff*alpha
	target.Energy = target.Energy*(1-alpha) + source.Energy*alpha

	// Update MFCC coefficients
	if len(target.MFCC) == len(source.MFCC) {
		for i := range target.MFCC {
			target.MFCC[i] = target.MFCC[i]*(1-alpha) + source.MFCC[i]*alpha
		}
	}

	target.UpdateCount++
	target.LastUpdate = time.Now()
}

// bytesToFloat64 converts byte audio data to float64 samples
func (sd *SpeakerDiarizer) bytesToFloat64(audioData []byte) []float64 {
	// Assuming 16-bit PCM audio
	if len(audioData)%2 != 0 {
		return nil
	}

	buffer := sd.bufferPool.Get().([]float64)
	defer sd.bufferPool.Put(buffer)

	sampleCount := len(audioData) / 2
	if cap(buffer) < sampleCount {
		buffer = make([]float64, sampleCount)
	} else {
		buffer = buffer[:sampleCount]
	}

	for i := 0; i < sampleCount; i++ {
		// Convert 16-bit little-endian to float64 [-1.0, 1.0]
		sample := int16(audioData[i*2]) | int16(audioData[i*2+1])<<8
		buffer[i] = float64(sample) / 32768.0
	}

	return buffer
}

// performPeriodicCleanup performs periodic cleanup of old speaker profiles
func (sd *SpeakerDiarizer) performPeriodicCleanup() {
	now := time.Now()
	if now.Sub(sd.lastCleanup) < sd.cleanupInterval {
		return
	}

	sd.lastCleanup = now

	// Cleanup old speaker profiles
	sd.speakersMux.Lock()
	defer sd.speakersMux.Unlock()

	toDelete := make([]string, 0)
	for id, profile := range sd.speakers {
		profile.mutex.RLock()
		age := now.Sub(profile.LastSeen)
		profile.mutex.RUnlock()

		if age > sd.maxProfileAge {
			toDelete = append(toDelete, id)
		}
	}

	for _, id := range toDelete {
		delete(sd.speakers, id)
		sd.logger.WithField("speaker_id", id).Debug("Removed inactive speaker profile")
	}

	// Update memory usage stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	sd.stats.mutex.Lock()
	sd.stats.MemoryUsage = int64(m.HeapInuse)
	sd.stats.mutex.Unlock()

	// Force GC if memory usage is high
	if m.HeapInuse > 100*1024*1024 { // 100MB threshold
		go runtime.GC()
	}
}

// Cleanup performs cleanup operations
func (sd *SpeakerDiarizer) Cleanup() {
	sd.speakersMux.Lock()
	defer sd.speakersMux.Unlock()

	// Clear all speaker profiles
	sd.speakers = make(map[string]*SpeakerProfile)
	sd.currentSpeaker = ""

	// Reset statistics
	sd.stats.mutex.Lock()
	sd.stats = &DiarizationStats{LastReset: time.Now()}
	sd.stats.mutex.Unlock()

	// Cleanup components
	if sd.processingPool != nil {
		_ = sd.processingPool.Stop()
	}

	sd.logger.Debug("Speaker diarizer cleaned up")
}

// GetSpeakers returns current speaker profiles
func (sd *SpeakerDiarizer) GetSpeakers() map[string]*SpeakerProfile {
	sd.speakersMux.RLock()
	defer sd.speakersMux.RUnlock()

	// Return a copy to prevent external modification
	speakers := make(map[string]*SpeakerProfile)
	for id, profile := range sd.speakers {
		speakers[id] = profile
	}
	return speakers
}

// GetStats returns diarization statistics
func (sd *SpeakerDiarizer) GetStats() *DiarizationStats {
	sd.stats.mutex.RLock()
	defer sd.stats.mutex.RUnlock()

	statsCopy := &DiarizationStats{
		TotalSegments:        sd.stats.TotalSegments,
		SpeakerChanges:       sd.stats.SpeakerChanges,
		ProcessingTime:       sd.stats.ProcessingTime,
		FeaturesExtracted:    sd.stats.FeaturesExtracted,
		IdentificationErrors: sd.stats.IdentificationErrors,
		MemoryUsage:          sd.stats.MemoryUsage,
		LastReset:            sd.stats.LastReset,
	}
	return statsCopy
}
