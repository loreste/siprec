package audio

import (
	"math"
	"sync"
)

// NoiseReducer implements a simple spectral subtraction noise reduction
type NoiseReducer struct {
	// Configuration
	noiseFloor        float64 // Estimated noise floor level
	attenuationFactor float64 // Noise attenuation factor
	sampleRate        int     // Audio sample rate
	frameSize         int     // Size of audio frames in samples
	bytesPerSample    int     // Bytes per sample (typically 2 for PCM16)
	
	// State
	enabled           bool    // Whether noise reduction is enabled
	noiseProfile      []float64 // Noise spectral profile
	profileInitialized bool    // Whether noise profile has been initialized
	noiseEstimationFrames int  // Number of frames to use for initial noise estimation
	framesProcessed   int     // Count of frames processed for noise estimation
	
	// For frequency domain processing
	fftSize           int     // FFT size (power of 2, typically 2x frameSize)
	
	// Lock for thread safety
	mu                sync.Mutex
	
	// Working buffers
	buffer            []byte
	spectrumBuffer    []float64
}

// NewNoiseReducer creates a new noise reduction processor
func NewNoiseReducer(config ProcessingConfig) *NoiseReducer {
	fftSize := 512 // Power of 2, larger than typical frame size
	
	// Convert dB attenuation to linear factor
	attenuationFactor := math.Pow(10, -config.NoiseAttenuationDB/20.0)
	
	return &NoiseReducer{
		noiseFloor:        config.NoiseFloor,
		attenuationFactor: attenuationFactor,
		sampleRate:        config.SampleRate,
		frameSize:         config.FrameSize,
		bytesPerSample:    2, // Assume 16-bit PCM
		
		enabled:           config.EnableNoiseReduction,
		noiseProfile:      make([]float64, fftSize/2+1), // Half FFT size + 1 for real signal
		profileInitialized: false,
		noiseEstimationFrames: 30, // Use 30 frames (600ms at 20ms frames) for initial noise profile
		framesProcessed:   0,
		
		fftSize:           fftSize,
		
		buffer:            make([]byte, config.BufferSize),
		spectrumBuffer:    make([]float64, fftSize),
	}
}

// Process implements AudioProcessor interface
// This is a simplified spectral subtraction implementation
func (nr *NoiseReducer) Process(data []byte) ([]byte, error) {
	if !nr.enabled {
		return data, nil
	}
	
	nr.mu.Lock()
	defer nr.mu.Unlock()
	
	// Convert PCM bytes to float samples
	samples := make([]float64, len(data)/nr.bytesPerSample)
	bytesToFloat64Samples(data, samples, nr.bytesPerSample)
	
	// For simplicity in this implementation, we'll use a time-domain approach
	// rather than a full FFT-based spectral subtraction
	
	// If still building noise profile
	if !nr.profileInitialized && nr.framesProcessed < nr.noiseEstimationFrames {
		nr.updateNoiseProfile(samples)
		nr.framesProcessed++
		
		if nr.framesProcessed >= nr.noiseEstimationFrames {
			nr.profileInitialized = true
		}
		
		// During noise profile building, return original data
		return data, nil
	}
	
	// Process each sample with noise reduction
	processedSamples := make([]float64, len(samples))
	for i, sample := range samples {
		// Simple noise gate with smoothing
		if math.Abs(sample) < nr.noiseFloor {
			// Attenuate noise
			processedSamples[i] = sample * nr.attenuationFactor
		} else {
			// Keep signal above noise floor
			// Apply soft transition at the threshold for smoother results
			ratio := math.Min(1.0, (math.Abs(sample)-nr.noiseFloor)/(nr.noiseFloor*2))
			attenuation := nr.attenuationFactor + (1.0-nr.attenuationFactor)*ratio
			processedSamples[i] = sample * attenuation
		}
	}
	
	// Convert back to bytes
	result := make([]byte, len(data))
	float64SamplesToBytes(processedSamples, result, nr.bytesPerSample)
	
	return result, nil
}

// updateNoiseProfile analyzes the audio to estimate the noise profile
func (nr *NoiseReducer) updateNoiseProfile(samples []float64) {
	// Calculate energy
	totalEnergy := 0.0
	for _, sample := range samples {
		totalEnergy += sample * sample
	}
	avgEnergy := totalEnergy / float64(len(samples))
	
	// Update noise floor estimate with exponential moving average
	// Use slower adaptation for noise floor to avoid adapting to speech
	nr.noiseFloor = 0.9*nr.noiseFloor + 0.1*math.Sqrt(avgEnergy)
}

// bytesToFloat64Samples converts PCM byte data to float64 samples
func bytesToFloat64Samples(data []byte, samples []float64, bytesPerSample int) {
	for i := 0; i < len(data)/bytesPerSample && i < len(samples); i++ {
		sampleIndex := i * bytesPerSample
		
		// 16-bit PCM little endian to float conversion
		if bytesPerSample == 2 && sampleIndex+1 < len(data) {
			sampleVal := int16(data[sampleIndex]) | (int16(data[sampleIndex+1]) << 8)
			samples[i] = float64(sampleVal) / 32768.0 // Normalize to -1.0 to 1.0
		}
	}
}

// float64SamplesToBytes converts float64 samples to PCM byte data
func float64SamplesToBytes(samples []float64, data []byte, bytesPerSample int) {
	for i := 0; i < len(samples) && i*bytesPerSample+bytesPerSample <= len(data); i++ {
		// Clamp sample to -1.0...1.0 range
		sample := samples[i]
		if sample > 1.0 {
			sample = 1.0
		} else if sample < -1.0 {
			sample = -1.0
		}
		
		// Convert to 16-bit PCM value
		sampleVal := int16(sample * 32767.0)
		sampleIndex := i * bytesPerSample
		
		// Store as little endian
		data[sampleIndex] = byte(sampleVal & 0xFF)
		data[sampleIndex+1] = byte(sampleVal >> 8)
	}
}

// Reset implements AudioProcessor interface
func (nr *NoiseReducer) Reset() {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	
	nr.profileInitialized = false
	nr.framesProcessed = 0
	
	// Reset noise profile
	for i := range nr.noiseProfile {
		nr.noiseProfile[i] = 0.0
	}
}

// Close implements AudioProcessor interface
func (nr *NoiseReducer) Close() error {
	return nil
}

// SetEnabled enables or disables noise reduction
func (nr *NoiseReducer) SetEnabled(enabled bool) {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	nr.enabled = enabled
}

// GetNoiseFloor returns the current estimated noise floor
func (nr *NoiseReducer) GetNoiseFloor() float64 {
	nr.mu.Lock()
	defer nr.mu.Unlock()
	return nr.noiseFloor
}