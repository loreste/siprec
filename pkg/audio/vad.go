package audio

import (
	"math"
	"math/rand"
	"sync"
)

// VoiceActivityDetector implements voice activity detection
type VoiceActivityDetector struct {
	// Configuration
	threshold      float64 // Energy threshold for VAD
	holdTime       int     // Frames to hold voice detection after energy drops
	sampleRate     int     // Audio sample rate
	frameSize      int     // Size of audio frames in samples
	bytesPerSample int     // Bytes per sample (typically 2 for PCM16)

	// State
	holdCounter   int     // Counter for holding voice detection
	isVoiceActive bool    // Current voice state
	noiseFloor    float64 // Estimated noise floor
	avgEnergy     float64 // Average signal energy

	// Energy history for adaptive thresholding
	energyHistory []float64
	historySize   int
	historyIndex  int

	// Lock for thread safety
	mu sync.Mutex

	// Silence suppression
	silenceSuppress bool

	// For audio format conversion
	buffer []byte
}

// NewVoiceActivityDetector creates a new VAD processor
func NewVoiceActivityDetector(config ProcessingConfig) *VoiceActivityDetector {
	historySize := 100 // Keep track of 100 frames for energy history

	return &VoiceActivityDetector{
		threshold:      config.VADThreshold,
		holdTime:       config.VADHoldTime,
		sampleRate:     config.SampleRate,
		frameSize:      config.FrameSize,
		bytesPerSample: 2, // Assume 16-bit PCM

		holdCounter:   0,
		isVoiceActive: false,
		noiseFloor:    0.01, // Initial estimate
		avgEnergy:     0.0,

		energyHistory: make([]float64, historySize),
		historySize:   historySize,
		historyIndex:  0,

		silenceSuppress: true, // Default to true

		buffer: make([]byte, config.BufferSize),
	}
}

// Process implements AudioProcessor interface
func (v *VoiceActivityDetector) Process(data []byte) ([]byte, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Calculate energy for this frame
	energy := v.calculateEnergy(data)

	// Update energy history for adaptive thresholding
	v.energyHistory[v.historyIndex] = energy
	v.historyIndex = (v.historyIndex + 1) % v.historySize

	// Update average energy with exponential moving average
	v.avgEnergy = 0.95*v.avgEnergy + 0.05*energy

	// Determine if this frame has voice activity
	v.detectVoice(energy)

	// If silence suppression is enabled, return empty data when no voice is detected
	if v.silenceSuppress && !v.isVoiceActive {
		// Return a short comfort noise frame instead of nothing
		// This helps prevent audio devices from thinking the connection is dead
		comfortNoise := generateComfortNoise(16, v.noiseFloor/10) // Very low level comfort noise
		return comfortNoise, nil
	}

	// If voice activity state changed, mark the transition in the data
	// This could be used for downstream processing or debugging

	// Return the original data
	return data, nil
}

// calculateEnergy computes the energy level of an audio frame
func (v *VoiceActivityDetector) calculateEnergy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}

	// For PCM16 audio (common in telephony)
	totalEnergy := 0.0
	samples := len(data) / v.bytesPerSample

	for i := 0; i < samples; i++ {
		// Convert bytes to 16-bit sample
		sampleIndex := i * v.bytesPerSample
		if sampleIndex+1 >= len(data) {
			break
		}

		// Convert two bytes to a 16-bit sample (little endian)
		sample := int16(data[sampleIndex]) | (int16(data[sampleIndex+1]) << 8)

		// Square the sample value and add to total energy
		sampleFloat := float64(sample) / 32768.0 // Normalize to -1.0 to 1.0
		totalEnergy += sampleFloat * sampleFloat
	}

	// Calculate average energy per sample
	if samples > 0 {
		return totalEnergy / float64(samples)
	}
	return 0.0
}

// detectVoice determines if the current frame contains voice
func (v *VoiceActivityDetector) detectVoice(energy float64) {
	// Dynamic threshold based on noise floor
	effectiveThreshold := math.Max(v.threshold, v.noiseFloor*2.0)

	// Check if energy exceeds threshold
	if energy > effectiveThreshold {
		v.isVoiceActive = true
		v.holdCounter = v.holdTime
	} else {
		// Decrement hold counter
		if v.holdCounter > 0 {
			v.holdCounter--
			// Voice is still active during hold time
			v.isVoiceActive = true
		} else {
			v.isVoiceActive = false

			// Update noise floor when no voice is detected
			// Use a slow adaptation rate to avoid suppressing low-level speech
			v.noiseFloor = 0.99*v.noiseFloor + 0.01*energy
		}
	}
}

// Reset implements AudioProcessor interface
func (v *VoiceActivityDetector) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.holdCounter = 0
	v.isVoiceActive = false
	v.avgEnergy = 0.0

	// Reset energy history
	for i := range v.energyHistory {
		v.energyHistory[i] = 0.0
	}
	v.historyIndex = 0
}

// Close implements AudioProcessor interface
func (v *VoiceActivityDetector) Close() error {
	return nil
}

// SetSilenceSuppression enables or disables silence suppression
func (v *VoiceActivityDetector) SetSilenceSuppression(enable bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.silenceSuppress = enable
}

// IsVoiceActive returns the current voice activity state
func (v *VoiceActivityDetector) IsVoiceActive() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.isVoiceActive
}

// GetNoiseFloor returns the current estimated noise floor
func (v *VoiceActivityDetector) GetNoiseFloor() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.noiseFloor
}

// generateComfortNoise creates comfort noise at a specified level
func generateComfortNoise(samples int, level float64) []byte {
	noise := make([]byte, samples*2) // 2 bytes per sample for 16-bit PCM

	// Simple white noise generator
	for i := 0; i < samples; i++ {
		// Generate random value between -level and level
		value := (rand.Float64()*2.0 - 1.0) * level

		// Convert to 16-bit PCM
		sample := int16(value * 32767.0)

		// Store as little endian
		noise[i*2] = byte(sample & 0xFF)
		noise[i*2+1] = byte(sample >> 8)
	}

	return noise
}
