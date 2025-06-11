package realtime

import (
	"math"
	"math/cmplx"
	"time"
)

// AudioFeatureExtractor extracts voice features from audio samples
func NewAudioFeatureExtractor(sampleRate int) *AudioFeatureExtractor {
	frameSize := 1024
	hopSize := frameSize / 4
	fftSize := frameSize
	melBanks := 26
	mfccCount := 12
	
	extractor := &AudioFeatureExtractor{
		sampleRate: sampleRate,
		frameSize:  frameSize,
		hopSize:    hopSize,
		windowType: "hamming",
		fftSize:    fftSize,
		melBanks:   melBanks,
		mfccCount:  mfccCount,
		fftBuffer:  make([]complex128, fftSize),
	}
	
	// Initialize Hamming window
	extractor.window = make([]float64, frameSize)
	for i := 0; i < frameSize; i++ {
		extractor.window[i] = 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(frameSize-1))
	}
	
	// Initialize Mel filter bank
	extractor.initializeMelFilters()
	
	return extractor
}

// ExtractFeatures extracts comprehensive voice features from audio samples
func (afe *AudioFeatureExtractor) ExtractFeatures(samples []float64) *VoiceFeatures {
	afe.mutex.Lock()
	defer afe.mutex.Unlock()
	
	if len(samples) < afe.frameSize {
		return nil
	}
	
	features := &VoiceFeatures{
		UpdateCount: 1,
		LastUpdate:  time.Now(),
		Confidence:  0.8, // Base confidence
	}
	
	// Extract fundamental frequency (F0) features
	afe.extractF0Features(samples, features)
	
	// Extract spectral features
	afe.extractSpectralFeatures(samples, features)
	
	// Extract MFCC features
	afe.extractMFCCFeatures(samples, features)
	
	// Extract formant frequencies
	afe.extractFormantFeatures(samples, features)
	
	// Extract energy and zero-crossing rate
	afe.extractEnergyFeatures(samples, features)
	
	// Extract prosodic features
	afe.extractProsodicFeatures(samples, features)
	
	return features
}

// extractF0Features extracts fundamental frequency features
func (afe *AudioFeatureExtractor) extractF0Features(samples []float64, features *VoiceFeatures) {
	// Simple autocorrelation-based F0 estimation
	f0Values := make([]float64, 0)
	
	for i := 0; i <= len(samples)-afe.frameSize; i += afe.hopSize {
		frame := samples[i : i+afe.frameSize]
		f0 := afe.estimateF0Autocorrelation(frame)
		if f0 > 50 && f0 < 500 { // Valid F0 range for human speech
			f0Values = append(f0Values, f0)
		}
	}
	
	if len(f0Values) > 0 {
		features.F0Mean = afe.calculateMean(f0Values)
		features.F0Std = afe.calculateStdDev(f0Values, features.F0Mean)
		features.F0Range = afe.calculateRange(f0Values)
	}
}

// extractSpectralFeatures extracts spectral characteristics
func (afe *AudioFeatureExtractor) extractSpectralFeatures(samples []float64, features *VoiceFeatures) {
	// Calculate FFT for the entire signal (or average over frames)
	spectrum := afe.calculateFFT(samples[:afe.frameSize])
	magnitudes := afe.calculateMagnitudes(spectrum)
	
	// Spectral centroid
	features.SpectralCentroid = afe.calculateSpectralCentroid(magnitudes)
	
	// Spectral rolloff
	features.SpectralRolloff = afe.calculateSpectralRolloff(magnitudes, 0.95)
	
	// Spectral flux (simplified - would need multiple frames for real implementation)
	features.SpectralFlux = afe.calculateSpectralFlux(magnitudes)
}

// extractMFCCFeatures extracts Mel-frequency cepstral coefficients
func (afe *AudioFeatureExtractor) extractMFCCFeatures(samples []float64, features *VoiceFeatures) {
	// Calculate power spectrum
	spectrum := afe.calculateFFT(samples[:afe.frameSize])
	powerSpectrum := afe.calculatePowerSpectrum(spectrum)
	
	// Apply Mel filter bank
	melSpectrum := afe.applyMelFilters(powerSpectrum)
	
	// Calculate DCT to get MFCC
	features.MFCC = afe.calculateDCT(melSpectrum, afe.mfccCount)
}

// extractFormantFeatures extracts formant frequencies
func (afe *AudioFeatureExtractor) extractFormantFeatures(samples []float64, features *VoiceFeatures) {
	// Simplified formant estimation using LPC (Linear Predictive Coding)
	lpcCoeffs := afe.calculateLPC(samples[:afe.frameSize], 12)
	formants := afe.findFormants(lpcCoeffs)
	
	if len(formants) >= 3 {
		features.F1 = formants[0]
		features.F2 = formants[1] 
		features.F3 = formants[2]
	}
}

// extractEnergyFeatures extracts energy-related features
func (afe *AudioFeatureExtractor) extractEnergyFeatures(samples []float64, features *VoiceFeatures) {
	// RMS energy
	sumSquares := 0.0
	for _, sample := range samples {
		sumSquares += sample * sample
	}
	features.Energy = math.Sqrt(sumSquares / float64(len(samples)))
	
	// Zero crossing rate
	zeroCrossings := 0
	for i := 1; i < len(samples); i++ {
		if (samples[i-1] >= 0) != (samples[i] >= 0) {
			zeroCrossings++
		}
	}
	features.ZeroCrossingRate = float64(zeroCrossings) / float64(len(samples))
}

// extractProsodicFeatures extracts prosodic characteristics
func (afe *AudioFeatureExtractor) extractProsodicFeatures(samples []float64, features *VoiceFeatures) {
	// Simplified prosodic features
	duration := float64(len(samples)) / float64(afe.sampleRate)
	
	// Estimate speech rate (very simplified)
	// In a real implementation, this would analyze syllable detection
	features.SpeechRate = 1.0 / duration // Simplified rate
	
	// Voiced ratio estimation
	voicedFrames := 0
	totalFrames := 0
	
	for i := 0; i <= len(samples)-afe.frameSize; i += afe.hopSize {
		frame := samples[i : i+afe.frameSize]
		if afe.isVoiced(frame) {
			voicedFrames++
		}
		totalFrames++
	}
	
	if totalFrames > 0 {
		features.VoicedRatio = float64(voicedFrames) / float64(totalFrames)
	}
}

// estimateF0Autocorrelation estimates F0 using autocorrelation
func (afe *AudioFeatureExtractor) estimateF0Autocorrelation(frame []float64) float64 {
	n := len(frame)
	autocorr := make([]float64, n/2)
	
	// Calculate autocorrelation
	for lag := 0; lag < len(autocorr); lag++ {
		sum := 0.0
		for i := 0; i < n-lag; i++ {
			sum += frame[i] * frame[i+lag]
		}
		autocorr[lag] = sum
	}
	
	// Find peak in autocorrelation (excluding lag 0)
	minLag := afe.sampleRate / 500 // 500 Hz max
	maxLag := afe.sampleRate / 50  // 50 Hz min
	
	if maxLag >= len(autocorr) {
		maxLag = len(autocorr) - 1
	}
	
	maxVal := 0.0
	maxLag_idx := 0
	
	for lag := minLag; lag < maxLag; lag++ {
		if autocorr[lag] > maxVal {
			maxVal = autocorr[lag]
			maxLag_idx = lag
		}
	}
	
	if maxLag_idx > 0 {
		return float64(afe.sampleRate) / float64(maxLag_idx)
	}
	
	return 0.0
}

// calculateFFT calculates FFT of the input signal
func (afe *AudioFeatureExtractor) calculateFFT(samples []float64) []complex128 {
	// Copy samples to complex buffer
	for i := 0; i < len(samples) && i < len(afe.fftBuffer); i++ {
		afe.fftBuffer[i] = complex(samples[i]*afe.window[i], 0)
	}
	
	// Pad with zeros if necessary
	for i := len(samples); i < len(afe.fftBuffer); i++ {
		afe.fftBuffer[i] = 0
	}
	
	// Simple DFT implementation (in production, use FFT library)
	result := make([]complex128, len(afe.fftBuffer))
	n := len(afe.fftBuffer)
	
	for k := 0; k < n; k++ {
		sum := complex(0, 0)
		for j := 0; j < n; j++ {
			angle := -2 * math.Pi * float64(k) * float64(j) / float64(n)
			w := cmplx.Exp(complex(0, angle))
			sum += afe.fftBuffer[j] * w
		}
		result[k] = sum
	}
	
	return result
}

// calculateMagnitudes calculates magnitude spectrum
func (afe *AudioFeatureExtractor) calculateMagnitudes(spectrum []complex128) []float64 {
	magnitudes := make([]float64, len(spectrum)/2)
	for i := 0; i < len(magnitudes); i++ {
		magnitudes[i] = cmplx.Abs(spectrum[i])
	}
	return magnitudes
}

// calculatePowerSpectrum calculates power spectrum
func (afe *AudioFeatureExtractor) calculatePowerSpectrum(spectrum []complex128) []float64 {
	power := make([]float64, len(spectrum)/2)
	for i := 0; i < len(power); i++ {
		magnitude := cmplx.Abs(spectrum[i])
		power[i] = magnitude * magnitude
	}
	return power
}

// calculateSpectralCentroid calculates spectral centroid
func (afe *AudioFeatureExtractor) calculateSpectralCentroid(magnitudes []float64) float64 {
	weightedSum := 0.0
	totalMagnitude := 0.0
	
	for i, mag := range magnitudes {
		freq := float64(i) * float64(afe.sampleRate) / float64(2*len(magnitudes))
		weightedSum += freq * mag
		totalMagnitude += mag
	}
	
	if totalMagnitude > 0 {
		return weightedSum / totalMagnitude
	}
	return 0.0
}

// calculateSpectralRolloff calculates spectral rolloff
func (afe *AudioFeatureExtractor) calculateSpectralRolloff(magnitudes []float64, threshold float64) float64 {
	totalEnergy := 0.0
	for _, mag := range magnitudes {
		totalEnergy += mag * mag
	}
	
	targetEnergy := threshold * totalEnergy
	cumulativeEnergy := 0.0
	
	for i, mag := range magnitudes {
		cumulativeEnergy += mag * mag
		if cumulativeEnergy >= targetEnergy {
			return float64(i) * float64(afe.sampleRate) / float64(2*len(magnitudes))
		}
	}
	
	return float64(len(magnitudes)-1) * float64(afe.sampleRate) / float64(2*len(magnitudes))
}

// calculateSpectralFlux calculates spectral flux
func (afe *AudioFeatureExtractor) calculateSpectralFlux(magnitudes []float64) float64 {
	// Simplified: return variance of magnitudes as flux indicator
	mean := afe.calculateMean(magnitudes)
	return afe.calculateStdDev(magnitudes, mean)
}

// initializeMelFilters initializes Mel filter bank
func (afe *AudioFeatureExtractor) initializeMelFilters() {
	afe.melFilters = make([][]float64, afe.melBanks)
	
	// Create triangular filters in Mel scale
	melMin := afe.hzToMel(0)
	melMax := afe.hzToMel(float64(afe.sampleRate) / 2)
	melPoints := make([]float64, afe.melBanks+2)
	
	for i := range melPoints {
		melPoints[i] = melMin + float64(i)*(melMax-melMin)/float64(len(melPoints)-1)
	}
	
	hzPoints := make([]float64, len(melPoints))
	for i, mel := range melPoints {
		hzPoints[i] = afe.melToHz(mel)
	}
	
	fftBins := afe.fftSize / 2
	for i := 0; i < afe.melBanks; i++ {
		afe.melFilters[i] = make([]float64, fftBins)
		
		leftHz := hzPoints[i]
		centerHz := hzPoints[i+1]
		rightHz := hzPoints[i+2]
		
		for j := 0; j < fftBins; j++ {
			freq := float64(j) * float64(afe.sampleRate) / float64(2*fftBins)
			
			if freq >= leftHz && freq <= centerHz {
				afe.melFilters[i][j] = (freq - leftHz) / (centerHz - leftHz)
			} else if freq > centerHz && freq <= rightHz {
				afe.melFilters[i][j] = (rightHz - freq) / (rightHz - centerHz)
			}
		}
	}
}

// applyMelFilters applies Mel filter bank to power spectrum
func (afe *AudioFeatureExtractor) applyMelFilters(powerSpectrum []float64) []float64 {
	melSpectrum := make([]float64, afe.melBanks)
	
	for i := 0; i < afe.melBanks; i++ {
		for j := 0; j < len(powerSpectrum) && j < len(afe.melFilters[i]); j++ {
			melSpectrum[i] += powerSpectrum[j] * afe.melFilters[i][j]
		}
		if melSpectrum[i] > 0 {
			melSpectrum[i] = math.Log(melSpectrum[i])
		}
	}
	
	return melSpectrum
}

// calculateDCT calculates Discrete Cosine Transform
func (afe *AudioFeatureExtractor) calculateDCT(input []float64, numCoeffs int) []float64 {
	if numCoeffs > len(input) {
		numCoeffs = len(input)
	}
	
	dct := make([]float64, numCoeffs)
	n := len(input)
	
	for k := 0; k < numCoeffs; k++ {
		sum := 0.0
		for j := 0; j < n; j++ {
			sum += input[j] * math.Cos(math.Pi*float64(k)*float64(2*j+1)/float64(2*n))
		}
		dct[k] = sum
	}
	
	return dct
}

// calculateLPC calculates Linear Predictive Coding coefficients
func (afe *AudioFeatureExtractor) calculateLPC(samples []float64, order int) []float64 {
	// Simplified LPC using autocorrelation method
	autocorr := make([]float64, order+1)
	
	// Calculate autocorrelation
	for lag := 0; lag <= order; lag++ {
		sum := 0.0
		for i := 0; i < len(samples)-lag; i++ {
			sum += samples[i] * samples[i+lag]
		}
		autocorr[lag] = sum
	}
	
	// Solve using Levinson-Durbin algorithm (simplified)
	lpc := make([]float64, order)
	if autocorr[0] != 0 {
		lpc[0] = -autocorr[1] / autocorr[0]
		
		for i := 1; i < order; i++ {
			sum := 0.0
			for j := 0; j < i; j++ {
				sum += lpc[j] * autocorr[i-j]
			}
			if autocorr[0] != 0 {
				lpc[i] = -(autocorr[i+1] + sum) / autocorr[0]
			}
		}
	}
	
	return lpc
}

// findFormants finds formant frequencies from LPC coefficients
func (afe *AudioFeatureExtractor) findFormants(lpcCoeffs []float64) []float64 {
	// Simplified formant detection
	// In practice, this would find roots of LPC polynomial
	formants := make([]float64, 3)
	
	// Mock formant values based on typical ranges
	formants[0] = 700  // F1: 300-1000 Hz
	formants[1] = 1200 // F2: 900-2500 Hz  
	formants[2] = 2500 // F3: 1500-3500 Hz
	
	return formants
}

// isVoiced determines if a frame contains voiced speech
func (afe *AudioFeatureExtractor) isVoiced(frame []float64) bool {
	// Simple energy and zero-crossing rate based voicing detection
	energy := 0.0
	for _, sample := range frame {
		energy += sample * sample
	}
	energy = math.Sqrt(energy / float64(len(frame)))
	
	zeroCrossings := 0
	for i := 1; i < len(frame); i++ {
		if (frame[i-1] >= 0) != (frame[i] >= 0) {
			zeroCrossings++
		}
	}
	zcr := float64(zeroCrossings) / float64(len(frame))
	
	// Voiced speech typically has higher energy and lower ZCR
	return energy > 0.01 && zcr < 0.1
}

// Helper functions for statistical calculations
func (afe *AudioFeatureExtractor) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (afe *AudioFeatureExtractor) calculateStdDev(values []float64, mean float64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	sumSquaredDiff := 0.0
	for _, v := range values {
		diff := v - mean
		sumSquaredDiff += diff * diff
	}
	return math.Sqrt(sumSquaredDiff / float64(len(values)))
}

func (afe *AudioFeatureExtractor) calculateRange(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	
	min := values[0]
	max := values[0]
	
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	
	return max - min
}

// Mel scale conversion functions
func (afe *AudioFeatureExtractor) hzToMel(hz float64) float64 {
	return 2595.0 * math.Log10(1.0+hz/700.0)
}

func (afe *AudioFeatureExtractor) melToHz(mel float64) float64 {
	return 700.0 * (math.Pow(10.0, mel/2595.0) - 1.0)
}