package audio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AudioFingerprint represents a unique fingerprint of an audio segment
type AudioFingerprint struct {
	// Fingerprint data
	Hash           string    // Primary hash identifier
	SpectralHashes []uint32  // Spectral peak hashes for matching
	Timestamp      time.Time // When the fingerprint was created
	Duration       time.Duration

	// Audio characteristics
	SampleRate     int
	Channels       int
	AverageEnergy  float64
	PeakFrequency  float64
	SpectralCentroid float64
	ZeroCrossRate  float64

	// Metadata
	SessionID   string
	ChannelID   int
	StartOffset time.Duration // Offset from start of recording
	EndOffset   time.Duration

	// Similarity matching
	LandmarkPairs []LandmarkPair // For robust matching
}

// LandmarkPair represents a time-frequency landmark pair for robust matching
type LandmarkPair struct {
	Anchor      TimeFrequencyPoint
	Point       TimeFrequencyPoint
	TimeDelta   int
	HashCode    uint32
}

// TimeFrequencyPoint represents a point in time-frequency space
type TimeFrequencyPoint struct {
	Time      int // Time frame index
	Frequency int // Frequency bin
	Magnitude float64
}

// FingerprintConfig contains configuration for audio fingerprinting
type FingerprintConfig struct {
	// Enable fingerprinting
	Enabled bool

	// FFT parameters
	WindowSize    int     // FFT window size (e.g., 2048)
	HopSize       int     // Hop between windows (e.g., 512)
	SampleRate    int     // Sample rate
	
	// Peak detection
	PeakNeighborhood   int     // Neighborhood size for peak detection
	PeakThreshold      float64 // Minimum magnitude for peaks
	MaxPeaksPerFrame   int     // Maximum peaks per time frame
	
	// Landmark generation
	TargetZoneSize     int     // Size of target zone for pairing
	MaxPairsPerAnchor  int     // Maximum pairs per anchor point
	
	// Duplicate detection
	MinMatchScore      float64 // Minimum score to consider duplicate (0-1)
	MinMatchingHashes  int     // Minimum matching hashes
	TimeToleranceMs    int     // Time tolerance for matching (ms)
	
	// Storage
	MaxStoredFingerprints int           // Maximum fingerprints to store
	RetentionPeriod       time.Duration // How long to keep fingerprints
	
	// Performance
	ParallelProcessing bool // Enable parallel processing
	BatchSize          int  // Batch size for processing
}

// DefaultFingerprintConfig returns default fingerprint configuration
func DefaultFingerprintConfig() *FingerprintConfig {
	return &FingerprintConfig{
		Enabled:               true,
		WindowSize:            2048,
		HopSize:               512,
		SampleRate:            8000,
		PeakNeighborhood:      5,
		PeakThreshold:         0.1,
		MaxPeaksPerFrame:      5,
		TargetZoneSize:        5,
		MaxPairsPerAnchor:     3,
		MinMatchScore:         0.7,
		MinMatchingHashes:     10,
		TimeToleranceMs:       100,
		MaxStoredFingerprints: 10000,
		RetentionPeriod:       24 * time.Hour,
		ParallelProcessing:    true,
		BatchSize:             100,
	}
}

// AudioFingerprinter generates and matches audio fingerprints
type AudioFingerprinter struct {
	logger *logrus.Logger
	config *FingerprintConfig
	mu     sync.RWMutex

	// Fingerprint database
	fingerprints map[string]*AudioFingerprint // Hash -> Fingerprint
	hashIndex    map[uint32][]*AudioFingerprint // SpectralHash -> Fingerprints
	
	// Duplicate detection cache
	duplicateCache map[string][]string // Hash -> List of duplicate hashes
	
	// Processing components
	fftProcessor *FFTProcessor
	peakDetector *SpectralPeakDetector
	
	// Metrics
	metrics FingerprintMetrics
}

// FingerprintMetrics tracks fingerprinting metrics
type FingerprintMetrics struct {
	TotalFingerprints   uint64
	DuplicatesDetected  uint64
	MatchAttempts       uint64
	SuccessfulMatches   uint64
	ProcessingTime      time.Duration
	AverageMatchScore   float64
}

// NewAudioFingerprinter creates a new audio fingerprinter
func NewAudioFingerprinter(logger *logrus.Logger, config *FingerprintConfig) *AudioFingerprinter {
	if config == nil {
		config = DefaultFingerprintConfig()
	}

	return &AudioFingerprinter{
		logger:         logger,
		config:         config,
		fingerprints:   make(map[string]*AudioFingerprint),
		hashIndex:      make(map[uint32][]*AudioFingerprint),
		duplicateCache: make(map[string][]string),
		fftProcessor:   NewFFTProcessor(config.WindowSize, config.HopSize),
		peakDetector:   NewSpectralPeakDetector(config.PeakNeighborhood, config.PeakThreshold),
	}
}

// GenerateFingerprint generates a fingerprint for audio samples
func (af *AudioFingerprinter) GenerateFingerprint(ctx context.Context, samples []float64, metadata map[string]interface{}) (*AudioFingerprint, error) {
	if !af.config.Enabled {
		return nil, nil
	}

	startTime := time.Now()
	
	// Extract audio characteristics
	characteristics := af.extractCharacteristics(samples)
	
	// Generate spectral representation
	spectrogram := af.fftProcessor.ComputeSpectrogram(samples)
	
	// Detect peaks in spectrogram
	peaks := af.peakDetector.DetectPeaks(spectrogram)
	
	// Generate landmark pairs
	landmarks := af.generateLandmarks(peaks)
	
	// Generate hashes from landmarks
	spectralHashes := af.generateHashes(landmarks)
	
	// Generate primary hash
	primaryHash := af.generatePrimaryHash(samples, characteristics)
	
	// Create fingerprint
	fingerprint := &AudioFingerprint{
		Hash:             primaryHash,
		SpectralHashes:   spectralHashes,
		Timestamp:        time.Now(),
		Duration:         time.Duration(len(samples)) * time.Second / time.Duration(af.config.SampleRate),
		SampleRate:       af.config.SampleRate,
		Channels:         1, // Assume mono for now
		AverageEnergy:    characteristics["energy"].(float64),
		PeakFrequency:    characteristics["peak_freq"].(float64),
		SpectralCentroid: characteristics["centroid"].(float64),
		ZeroCrossRate:    characteristics["zcr"].(float64),
		LandmarkPairs:    landmarks,
	}
	
	// Add metadata
	if sessionID, ok := metadata["session_id"].(string); ok {
		fingerprint.SessionID = sessionID
	}
	if channelID, ok := metadata["channel_id"].(int); ok {
		fingerprint.ChannelID = channelID
	}
	
	// Store fingerprint
	af.storeFingerprint(fingerprint)
	
	// Update metrics
	af.mu.Lock()
	af.metrics.TotalFingerprints++
	af.metrics.ProcessingTime += time.Since(startTime)
	af.mu.Unlock()
	
	return fingerprint, nil
}

// CheckDuplicate checks if audio is a duplicate of existing recordings
func (af *AudioFingerprinter) CheckDuplicate(ctx context.Context, samples []float64) (*DuplicateResult, error) {
	if !af.config.Enabled {
		return &DuplicateResult{IsDuplicate: false}, nil
	}

	af.mu.Lock()
	af.metrics.MatchAttempts++
	af.mu.Unlock()
	
	// Generate fingerprint for input audio
	fingerprint, err := af.GenerateFingerprint(ctx, samples, nil)
	if err != nil {
		return nil, err
	}
	
	// Check cache first
	if duplicates, exists := af.duplicateCache[fingerprint.Hash]; exists && len(duplicates) > 0 {
		af.mu.Lock()
		af.metrics.DuplicatesDetected++
		af.metrics.SuccessfulMatches++
		af.mu.Unlock()
		
		return &DuplicateResult{
			IsDuplicate:     true,
			MatchingHashes:  duplicates,
			ConfidenceScore: 1.0,
		}, nil
	}
	
	// Search for matches using spectral hashes
	matches := af.findMatches(fingerprint)
	
	if len(matches) > 0 {
		bestMatch := af.selectBestMatch(fingerprint, matches)
		
		if bestMatch != nil && bestMatch.Score >= af.config.MinMatchScore {
			af.mu.Lock()
			af.metrics.DuplicatesDetected++
			af.metrics.SuccessfulMatches++
			af.metrics.AverageMatchScore = (af.metrics.AverageMatchScore + bestMatch.Score) / 2
			af.mu.Unlock()
			
			// Cache the result
			af.duplicateCache[fingerprint.Hash] = []string{bestMatch.Hash}
			
			return &DuplicateResult{
				IsDuplicate:     true,
				MatchingHashes:  []string{bestMatch.Hash},
				ConfidenceScore: bestMatch.Score,
				MatchedSession:  bestMatch.SessionID,
				TimeOffset:      bestMatch.TimeOffset,
			}, nil
		}
	}
	
	return &DuplicateResult{IsDuplicate: false}, nil
}

// extractCharacteristics extracts audio characteristics
func (af *AudioFingerprinter) extractCharacteristics(samples []float64) map[string]interface{} {
	characteristics := make(map[string]interface{})
	
	// Calculate energy
	energy := 0.0
	for _, sample := range samples {
		energy += sample * sample
	}
	characteristics["energy"] = math.Sqrt(energy / float64(len(samples)))
	
	// Calculate zero crossing rate
	zcr := 0.0
	for i := 1; i < len(samples); i++ {
		if (samples[i] > 0) != (samples[i-1] > 0) {
			zcr++
		}
	}
	characteristics["zcr"] = zcr / float64(len(samples))
	
	// Calculate spectral features (simplified)
	spectrum := af.fftProcessor.ComputeFFT(samples)
	
	// Find peak frequency
	maxMag := 0.0
	peakBin := 0
	for i, mag := range spectrum {
		if mag > maxMag {
			maxMag = mag
			peakBin = i
		}
	}
	peakFreq := float64(peakBin) * float64(af.config.SampleRate) / float64(af.config.WindowSize)
	characteristics["peak_freq"] = peakFreq
	
	// Calculate spectral centroid
	weightedSum := 0.0
	magnitudeSum := 0.0
	for i, mag := range spectrum {
		freq := float64(i) * float64(af.config.SampleRate) / float64(af.config.WindowSize)
		weightedSum += freq * mag
		magnitudeSum += mag
	}
	if magnitudeSum > 0 {
		characteristics["centroid"] = weightedSum / magnitudeSum
	} else {
		characteristics["centroid"] = 0.0
	}
	
	return characteristics
}

// generateLandmarks generates landmark pairs from peaks
func (af *AudioFingerprinter) generateLandmarks(peaks []TimeFrequencyPoint) []LandmarkPair {
	landmarks := []LandmarkPair{}
	
	for i, anchor := range peaks {
		// Find points in target zone
		targetStart := i + 1
		targetEnd := minInt(i+1+af.config.TargetZoneSize, len(peaks))
		
		pairsForAnchor := 0
		for j := targetStart; j < targetEnd && pairsForAnchor < af.config.MaxPairsPerAnchor; j++ {
			point := peaks[j]
			
			// Create landmark pair
			timeDelta := point.Time - anchor.Time
			if timeDelta > 0 && timeDelta < af.config.TargetZoneSize {
				pair := LandmarkPair{
					Anchor:    anchor,
					Point:     point,
					TimeDelta: timeDelta,
					HashCode:  af.hashLandmark(anchor, point, timeDelta),
				}
				landmarks = append(landmarks, pair)
				pairsForAnchor++
			}
		}
	}
	
	return landmarks
}

// generateHashes generates hashes from landmark pairs
func (af *AudioFingerprinter) generateHashes(landmarks []LandmarkPair) []uint32 {
	hashes := make([]uint32, 0, len(landmarks))
	hashSet := make(map[uint32]bool)
	
	for _, landmark := range landmarks {
		if !hashSet[landmark.HashCode] {
			hashes = append(hashes, landmark.HashCode)
			hashSet[landmark.HashCode] = true
		}
	}
	
	return hashes
}

// hashLandmark generates a hash for a landmark pair
func (af *AudioFingerprinter) hashLandmark(anchor, point TimeFrequencyPoint, timeDelta int) uint32 {
	// Simple hash combining frequency and time information
	hash := uint32(anchor.Frequency) << 20
	hash |= uint32(point.Frequency) << 10
	hash |= uint32(timeDelta) & 0x3FF
	return hash
}

// generatePrimaryHash generates the primary hash for audio
func (af *AudioFingerprinter) generatePrimaryHash(samples []float64, characteristics map[string]interface{}) string {
	hasher := sha256.New()
	
	// Hash audio characteristics
	hasher.Write([]byte(fmt.Sprintf("%.4f", characteristics["energy"])))
	hasher.Write([]byte(fmt.Sprintf("%.4f", characteristics["peak_freq"])))
	hasher.Write([]byte(fmt.Sprintf("%.4f", characteristics["centroid"])))
	hasher.Write([]byte(fmt.Sprintf("%.4f", characteristics["zcr"])))
	
	// Hash a subset of samples (for efficiency)
	stride := len(samples) / 100
	if stride < 1 {
		stride = 1
	}
	for i := 0; i < len(samples); i += stride {
		hasher.Write([]byte(fmt.Sprintf("%.6f", samples[i])))
	}
	
	return hex.EncodeToString(hasher.Sum(nil))
}

// storeFingerprint stores a fingerprint in the database
func (af *AudioFingerprinter) storeFingerprint(fingerprint *AudioFingerprint) {
	af.mu.Lock()
	defer af.mu.Unlock()
	
	// Store in main index
	af.fingerprints[fingerprint.Hash] = fingerprint
	
	// Update hash index
	for _, hash := range fingerprint.SpectralHashes {
		af.hashIndex[hash] = append(af.hashIndex[hash], fingerprint)
	}
	
	// Clean old fingerprints if needed
	if len(af.fingerprints) > af.config.MaxStoredFingerprints {
		af.cleanOldFingerprints()
	}
}

// findMatches finds potential matches for a fingerprint
func (af *AudioFingerprinter) findMatches(fingerprint *AudioFingerprint) []*FingerprintMatch {
	af.mu.RLock()
	defer af.mu.RUnlock()
	
	matchCounts := make(map[string]int)
	timeOffsets := make(map[string][]int)
	
	// Count matching hashes
	for _, hash := range fingerprint.SpectralHashes {
		if candidates, exists := af.hashIndex[hash]; exists {
			for _, candidate := range candidates {
				if candidate.Hash != fingerprint.Hash {
					matchCounts[candidate.Hash]++
					// Track time offsets for alignment
					for _, landmarkNew := range fingerprint.LandmarkPairs {
						if landmarkNew.HashCode == hash {
							for _, landmarkOld := range candidate.LandmarkPairs {
								if landmarkOld.HashCode == hash {
									offset := landmarkOld.Anchor.Time - landmarkNew.Anchor.Time
									timeOffsets[candidate.Hash] = append(timeOffsets[candidate.Hash], offset)
									break
								}
							}
							break
						}
					}
				}
			}
		}
	}
	
	// Create matches
	matches := []*FingerprintMatch{}
	for hash, count := range matchCounts {
		if count >= af.config.MinMatchingHashes {
			candidate := af.fingerprints[hash]
			
			// Check time alignment
			offsets := timeOffsets[hash]
			alignedOffsets := af.findAlignedOffsets(offsets)
			
			if len(alignedOffsets) >= af.config.MinMatchingHashes/2 {
				score := af.calculateMatchScore(fingerprint, candidate, count, alignedOffsets)
				
				match := &FingerprintMatch{
					Hash:           candidate.Hash,
					Score:          score,
					MatchingHashes: count,
					SessionID:      candidate.SessionID,
					TimeOffset:     time.Duration(alignedOffsets[0]) * time.Millisecond,
				}
				matches = append(matches, match)
			}
		}
	}
	
	return matches
}

// findAlignedOffsets finds time-aligned offsets
func (af *AudioFingerprinter) findAlignedOffsets(offsets []int) []int {
	if len(offsets) == 0 {
		return []int{}
	}
	
	// Group offsets by similarity
	tolerance := af.config.TimeToleranceMs
	groups := make(map[int][]int)
	
	for _, offset := range offsets {
		found := false
		for key := range groups {
			if absInt(offset-key) <= tolerance {
				groups[key] = append(groups[key], offset)
				found = true
				break
			}
		}
		if !found {
			groups[offset] = []int{offset}
		}
	}
	
	// Find largest group
	var largestGroup []int
	for _, group := range groups {
		if len(group) > len(largestGroup) {
			largestGroup = group
		}
	}
	
	return largestGroup
}

// calculateMatchScore calculates the match score between fingerprints
func (af *AudioFingerprinter) calculateMatchScore(fp1, fp2 *AudioFingerprint, matchingHashes int, alignedOffsets []int) float64 {
	score := 0.0
	weights := 0.0
	
	// Hash match score
	hashScore := float64(matchingHashes) / float64(len(fp1.SpectralHashes))
	score += hashScore * 0.5
	weights += 0.5
	
	// Time alignment score
	if len(alignedOffsets) > 0 {
		alignmentScore := float64(len(alignedOffsets)) / float64(matchingHashes)
		score += alignmentScore * 0.3
		weights += 0.3
	}
	
	// Characteristic similarity
	energyDiff := math.Abs(fp1.AverageEnergy - fp2.AverageEnergy)
	energyScore := 1.0 - math.Min(energyDiff/fp1.AverageEnergy, 1.0)
	score += energyScore * 0.1
	weights += 0.1
	
	freqDiff := math.Abs(fp1.PeakFrequency - fp2.PeakFrequency)
	freqScore := 1.0 - math.Min(freqDiff/1000.0, 1.0) // Normalize to 1kHz
	score += freqScore * 0.1
	weights += 0.1
	
	return score / weights
}

// selectBestMatch selects the best match from candidates
func (af *AudioFingerprinter) selectBestMatch(fingerprint *AudioFingerprint, matches []*FingerprintMatch) *FingerprintMatch {
	if len(matches) == 0 {
		return nil
	}
	
	bestMatch := matches[0]
	for _, match := range matches[1:] {
		if match.Score > bestMatch.Score {
			bestMatch = match
		}
	}
	
	return bestMatch
}

// cleanOldFingerprints removes old fingerprints
func (af *AudioFingerprinter) cleanOldFingerprints() {
	cutoff := time.Now().Add(-af.config.RetentionPeriod)
	toRemove := []string{}
	
	for hash, fp := range af.fingerprints {
		if fp.Timestamp.Before(cutoff) {
			toRemove = append(toRemove, hash)
		}
	}
	
	for _, hash := range toRemove {
		fp := af.fingerprints[hash]
		delete(af.fingerprints, hash)
		
		// Remove from hash index
		for _, spectralHash := range fp.SpectralHashes {
			if candidates, exists := af.hashIndex[spectralHash]; exists {
				filtered := []*AudioFingerprint{}
				for _, candidate := range candidates {
					if candidate.Hash != hash {
						filtered = append(filtered, candidate)
					}
				}
				if len(filtered) > 0 {
					af.hashIndex[spectralHash] = filtered
				} else {
					delete(af.hashIndex, spectralHash)
				}
			}
		}
		
		// Remove from duplicate cache
		delete(af.duplicateCache, hash)
	}
	
	af.logger.WithField("removed", len(toRemove)).Debug("Cleaned old fingerprints")
}

// GetMetrics returns fingerprinting metrics
func (af *AudioFingerprinter) GetMetrics() FingerprintMetrics {
	af.mu.RLock()
	defer af.mu.RUnlock()
	return af.metrics
}

// DuplicateResult contains the result of duplicate detection
type DuplicateResult struct {
	IsDuplicate     bool
	MatchingHashes  []string
	ConfidenceScore float64
	MatchedSession  string
	TimeOffset      time.Duration
}

// FingerprintMatch represents a potential fingerprint match
type FingerprintMatch struct {
	Hash           string
	Score          float64
	MatchingHashes int
	SessionID      string
	TimeOffset     time.Duration
}

// FFTProcessor handles FFT computations
type FFTProcessor struct {
	windowSize int
	hopSize    int
	window     []float64
}

// NewFFTProcessor creates a new FFT processor
func NewFFTProcessor(windowSize, hopSize int) *FFTProcessor {
	return &FFTProcessor{
		windowSize: windowSize,
		hopSize:    hopSize,
		window:     generateHanningWindow(windowSize),
	}
}

// ComputeFFT computes FFT of samples
func (fp *FFTProcessor) ComputeFFT(samples []float64) []float64 {
	// Apply window
	windowed := make([]float64, fp.windowSize)
	for i := 0; i < fp.windowSize && i < len(samples); i++ {
		windowed[i] = samples[i] * fp.window[i]
	}
	
	// Simplified DFT (use proper FFT library in production)
	N := fp.windowSize
	spectrum := make([]float64, N/2+1)
	
	for k := 0; k < N/2+1; k++ {
		real := 0.0
		imag := 0.0
		for n := 0; n < N; n++ {
			angle := -2.0 * math.Pi * float64(k*n) / float64(N)
			real += windowed[n] * math.Cos(angle)
			imag += windowed[n] * math.Sin(angle)
		}
		spectrum[k] = math.Sqrt(real*real + imag*imag)
	}
	
	return spectrum
}

// ComputeSpectrogram computes spectrogram of samples
func (fp *FFTProcessor) ComputeSpectrogram(samples []float64) [][]float64 {
	numFrames := (len(samples) - fp.windowSize) / fp.hopSize + 1
	if numFrames < 1 {
		numFrames = 1
	}
	
	spectrogram := make([][]float64, numFrames)
	
	for i := 0; i < numFrames; i++ {
		start := i * fp.hopSize
		end := start + fp.windowSize
		if end > len(samples) {
			end = len(samples)
		}
		
		frame := samples[start:end]
		spectrogram[i] = fp.ComputeFFT(frame)
	}
	
	return spectrogram
}

// SpectralPeakDetector detects peaks in spectrograms
type SpectralPeakDetector struct {
	neighborhood int
	threshold    float64
}

// NewSpectralPeakDetector creates a new peak detector
func NewSpectralPeakDetector(neighborhood int, threshold float64) *SpectralPeakDetector {
	return &SpectralPeakDetector{
		neighborhood: neighborhood,
		threshold:    threshold,
	}
}

// DetectPeaks detects peaks in a spectrogram
func (spd *SpectralPeakDetector) DetectPeaks(spectrogram [][]float64) []TimeFrequencyPoint {
	peaks := []TimeFrequencyPoint{}
	
	for t, frame := range spectrogram {
		framePeaks := spd.detectFramePeaks(frame, t)
		peaks = append(peaks, framePeaks...)
	}
	
	return peaks
}

// detectFramePeaks detects peaks in a single frame
func (spd *SpectralPeakDetector) detectFramePeaks(frame []float64, timeIndex int) []TimeFrequencyPoint {
	peaks := []TimeFrequencyPoint{}
	
	for i := spd.neighborhood; i < len(frame)-spd.neighborhood; i++ {
		if frame[i] < spd.threshold {
			continue
		}
		
		// Check if local maximum
		isMax := true
		for j := i - spd.neighborhood; j <= i+spd.neighborhood; j++ {
			if j != i && frame[j] >= frame[i] {
				isMax = false
				break
			}
		}
		
		if isMax {
			peaks = append(peaks, TimeFrequencyPoint{
				Time:      timeIndex,
				Frequency: i,
				Magnitude: frame[i],
			})
		}
	}
	
	return peaks
}

// Helper functions
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}