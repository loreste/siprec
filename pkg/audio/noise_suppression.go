package audio

import (
	"context"
	"math"
	"sync"

	"github.com/sirupsen/logrus"
)

// NoiseSuppressionConfig contains configuration for noise suppression
type NoiseSuppressionConfig struct {
	// Enable noise suppression processing
	Enabled bool

	// Noise reduction level (0.0-1.0, where 1.0 is maximum suppression)
	SuppressionLevel float64

	// Voice Activity Detection threshold (0.0-1.0)
	VADThreshold float64

	// Spectral subtraction factor
	SpectralSubtractionFactor float64

	// Minimum noise floor in dB
	NoiseFloorDB float64

	// Window size for FFT processing (samples)
	WindowSize int

	// Overlap factor for windowing (0.0-1.0)
	OverlapFactor float64

	// High-pass filter cutoff frequency (Hz)
	HighPassCutoff float64

	// Enable adaptive noise learning
	AdaptiveMode bool

	// Noise profile learning duration (seconds)
	LearningDuration float64
}

// DefaultNoiseSuppressionConfig returns default noise suppression configuration
func DefaultNoiseSuppressionConfig() *NoiseSuppressionConfig {
	return &NoiseSuppressionConfig{
		Enabled:                   true,
		SuppressionLevel:          0.7,
		VADThreshold:              0.3,
		SpectralSubtractionFactor: 2.0,
		NoiseFloorDB:              -60.0,
		WindowSize:                512,
		OverlapFactor:             0.5,
		HighPassCutoff:            80.0,
		AdaptiveMode:              true,
		LearningDuration:          0.5,
	}
}

// NoiseSuppressor implements noise suppression for audio streams
type NoiseSuppressor struct {
	logger *logrus.Logger
	config *NoiseSuppressionConfig
	mu     sync.RWMutex

	// Noise profile estimation
	noiseProfile     []float64
	noiseFloor       float64
	profileSamples   int
	isLearning       bool
	learningComplete bool

	// Processing buffers
	inputBuffer    []float64
	outputBuffer   []float64
	overlapBuffer  []float64
	windowFunction []float64

	// FFT components (simplified - in production use a proper FFT library)
	fftSize     int
	fftReal     []float64
	fftImag     []float64
	magnitude   []float64
	phase       []float64
	smoothing   []float64

	// Voice Activity Detection
	vadState      bool
	vadHangover   int
	energyHistory []float64

	// Metrics
	totalFrames     uint64
	suppressedFrames uint64
	voiceFrames     uint64
	noiseLevel      float64
	signalLevel     float64
}

// NewNoiseSuppressor creates a new noise suppressor
func NewNoiseSuppressor(logger *logrus.Logger, config *NoiseSuppressionConfig) *NoiseSuppressor {
	if config == nil {
		config = DefaultNoiseSuppressionConfig()
	}

	ns := &NoiseSuppressor{
		logger:         logger,
		config:         config,
		fftSize:        config.WindowSize,
		inputBuffer:    make([]float64, config.WindowSize),
		outputBuffer:   make([]float64, config.WindowSize),
		overlapBuffer:  make([]float64, int(float64(config.WindowSize)*config.OverlapFactor)),
		windowFunction: generateHanningWindow(config.WindowSize),
		fftReal:        make([]float64, config.WindowSize),
		fftImag:        make([]float64, config.WindowSize),
		magnitude:      make([]float64, config.WindowSize/2+1),
		phase:          make([]float64, config.WindowSize/2+1),
		smoothing:      make([]float64, config.WindowSize/2+1),
		noiseProfile:   make([]float64, config.WindowSize/2+1),
		energyHistory:  make([]float64, 10),
		noiseFloor:     dbToLinear(config.NoiseFloorDB),
	}

	if config.AdaptiveMode {
		ns.isLearning = true
		ns.profileSamples = int(config.LearningDuration * 8000) // Assuming 8kHz sample rate
	}

	return ns
}

// ProcessFrame processes a single frame of audio samples
func (ns *NoiseSuppressor) ProcessFrame(ctx context.Context, samples []float64) ([]float64, error) {
	if !ns.config.Enabled {
		return samples, nil
	}

	ns.mu.Lock()
	defer ns.mu.Unlock()

	// Update metrics
	ns.totalFrames++

	// If in learning mode, update noise profile
	if ns.isLearning && !ns.learningComplete {
		ns.updateNoiseProfile(samples)
		if ns.profileSamples <= 0 {
			ns.learningComplete = true
			ns.isLearning = false
			ns.logger.Debug("Noise profile learning complete")
		}
		// During learning, pass audio through with minimal processing
		return ns.applyHighPassFilter(samples), nil
	}

	// Apply windowing
	windowed := ns.applyWindow(samples)

	// Perform FFT (simplified - in production use proper FFT)
	ns.computeFFT(windowed)

	// Compute magnitude and phase
	ns.computeMagnitudePhase()

	// Voice Activity Detection
	isVoice := ns.detectVoiceActivity(ns.magnitude)
	if isVoice {
		ns.voiceFrames++
	}

	// Apply spectral subtraction
	suppressed := ns.spectralSubtraction(ns.magnitude, isVoice)

	// Apply smoothing
	suppressed = ns.applySpectralSmoothing(suppressed)

	// Reconstruct signal from magnitude and phase
	ns.reconstructSignal(suppressed, ns.phase)

	// Perform inverse FFT
	output := ns.computeInverseFFT()

	// Apply overlap-add
	output = ns.overlapAdd(output)

	// Apply post-processing
	output = ns.postProcess(output, isVoice)

	ns.suppressedFrames++

	return output, nil
}

// updateNoiseProfile updates the noise profile during learning phase
func (ns *NoiseSuppressor) updateNoiseProfile(samples []float64) {
	windowed := ns.applyWindow(samples)
	ns.computeFFT(windowed)
	ns.computeMagnitudePhase()

	// Update noise profile using exponential averaging
	alpha := 0.9
	for i := range ns.noiseProfile {
		if ns.profileSamples == int(ns.config.LearningDuration*8000) {
			// First frame - initialize
			ns.noiseProfile[i] = ns.magnitude[i]
		} else {
			// Update with exponential averaging
			ns.noiseProfile[i] = alpha*ns.noiseProfile[i] + (1-alpha)*ns.magnitude[i]
		}
	}

	ns.profileSamples -= len(samples)
}

// detectVoiceActivity detects if the current frame contains voice
func (ns *NoiseSuppressor) detectVoiceActivity(magnitude []float64) bool {
	// Compute frame energy
	energy := 0.0
	for _, mag := range magnitude {
		energy += mag * mag
	}
	energy = math.Sqrt(energy / float64(len(magnitude)))

	// Update energy history
	ns.energyHistory = append(ns.energyHistory[1:], energy)

	// Compute adaptive threshold
	minEnergy := math.MaxFloat64
	maxEnergy := -math.MaxFloat64
	for _, e := range ns.energyHistory {
		if e < minEnergy {
			minEnergy = e
		}
		if e > maxEnergy {
			maxEnergy = e
		}
	}

	threshold := minEnergy + ns.config.VADThreshold*(maxEnergy-minEnergy)

	// Apply hysteresis
	if energy > threshold*1.1 {
		ns.vadState = true
		ns.vadHangover = 3 // Hangover frames
	} else if energy < threshold*0.9 {
		if ns.vadHangover > 0 {
			ns.vadHangover--
		} else {
			ns.vadState = false
		}
	}

	// Update signal/noise levels
	if ns.vadState {
		ns.signalLevel = 0.9*ns.signalLevel + 0.1*energy
	} else {
		ns.noiseLevel = 0.95*ns.noiseLevel + 0.05*energy
	}

	return ns.vadState
}

// spectralSubtraction applies spectral subtraction for noise suppression
func (ns *NoiseSuppressor) spectralSubtraction(magnitude []float64, isVoice bool) []float64 {
	result := make([]float64, len(magnitude))
	
	suppressionFactor := ns.config.SpectralSubtractionFactor
	if isVoice {
		// Reduce suppression for voice frames
		suppressionFactor *= 0.5
	}

	for i := range magnitude {
		// Subtract noise profile
		cleaned := magnitude[i] - suppressionFactor*ns.noiseProfile[i]
		
		// Apply noise floor
		if cleaned < ns.noiseFloor {
			cleaned = ns.noiseFloor
		}

		// Apply suppression level
		result[i] = magnitude[i]*(1-ns.config.SuppressionLevel) + cleaned*ns.config.SuppressionLevel
		
		// Ensure non-negative
		if result[i] < 0 {
			result[i] = ns.noiseFloor
		}
	}

	return result
}

// applySpectralSmoothing applies smoothing to reduce musical noise
func (ns *NoiseSuppressor) applySpectralSmoothing(magnitude []float64) []float64 {
	result := make([]float64, len(magnitude))
	
	// Temporal smoothing
	alpha := 0.7
	for i := range magnitude {
		ns.smoothing[i] = alpha*ns.smoothing[i] + (1-alpha)*magnitude[i]
		result[i] = ns.smoothing[i]
	}

	// Frequency smoothing (3-point median filter)
	for i := 1; i < len(result)-1; i++ {
		values := []float64{result[i-1], result[i], result[i+1]}
		result[i] = median(values)
	}

	return result
}

// applyWindow applies a Hanning window to the input samples
func (ns *NoiseSuppressor) applyWindow(samples []float64) []float64 {
	result := make([]float64, len(samples))
	minLen := len(samples)
	if len(ns.windowFunction) < minLen {
		minLen = len(ns.windowFunction)
	}

	for i := 0; i < minLen; i++ {
		result[i] = samples[i] * ns.windowFunction[i]
	}
	return result
}

// applyHighPassFilter applies a simple high-pass filter
func (ns *NoiseSuppressor) applyHighPassFilter(samples []float64) []float64 {
	if ns.config.HighPassCutoff <= 0 {
		return samples
	}

	// Simple first-order high-pass filter
	result := make([]float64, len(samples))
	alpha := ns.config.HighPassCutoff / (ns.config.HighPassCutoff + 8000.0) // Assuming 8kHz sample rate
	
	for i := range samples {
		if i == 0 {
			result[i] = samples[i]
		} else {
			result[i] = alpha * (result[i-1] + samples[i] - samples[i-1])
		}
	}

	return result
}

// computeFFT performs FFT (simplified - use a proper FFT library in production)
func (ns *NoiseSuppressor) computeFFT(samples []float64) {
	// Copy input to real part
	for i := range ns.fftReal {
		if i < len(samples) {
			ns.fftReal[i] = samples[i]
			ns.fftImag[i] = 0
		} else {
			ns.fftReal[i] = 0
			ns.fftImag[i] = 0
		}
	}

	// Simplified DFT (in production, use FFT library like github.com/mjibson/go-dsp/fft)
	// This is just for demonstration
	N := len(ns.fftReal)
	for k := 0; k < N/2+1; k++ {
		sumReal := 0.0
		sumImag := 0.0
		for n := 0; n < N; n++ {
			angle := -2.0 * math.Pi * float64(k*n) / float64(N)
			sumReal += ns.fftReal[n] * math.Cos(angle)
			sumImag += ns.fftReal[n] * math.Sin(angle)
		}
		ns.fftReal[k] = sumReal
		ns.fftImag[k] = sumImag
	}
}

// computeMagnitudePhase computes magnitude and phase from FFT
func (ns *NoiseSuppressor) computeMagnitudePhase() {
	for i := range ns.magnitude {
		real := ns.fftReal[i]
		imag := ns.fftImag[i]
		ns.magnitude[i] = math.Sqrt(real*real + imag*imag)
		ns.phase[i] = math.Atan2(imag, real)
	}
}

// reconstructSignal reconstructs FFT from magnitude and phase
func (ns *NoiseSuppressor) reconstructSignal(magnitude, phase []float64) {
	for i := range magnitude {
		ns.fftReal[i] = magnitude[i] * math.Cos(phase[i])
		ns.fftImag[i] = magnitude[i] * math.Sin(phase[i])
	}
}

// computeInverseFFT performs inverse FFT
func (ns *NoiseSuppressor) computeInverseFFT() []float64 {
	N := len(ns.fftReal)
	result := make([]float64, N)

	// Simplified IDFT (in production, use FFT library)
	for n := 0; n < N; n++ {
		sum := 0.0
		for k := 0; k < N/2+1; k++ {
			angle := 2.0 * math.Pi * float64(k*n) / float64(N)
			sum += ns.fftReal[k]*math.Cos(angle) - ns.fftImag[k]*math.Sin(angle)
		}
		result[n] = sum / float64(N)
	}

	return result
}

// overlapAdd performs overlap-add synthesis
func (ns *NoiseSuppressor) overlapAdd(samples []float64) []float64 {
	overlapSize := len(ns.overlapBuffer)
	result := make([]float64, len(samples))

	// Add overlap from previous frame
	for i := 0; i < overlapSize && i < len(result); i++ {
		result[i] = samples[i] + ns.overlapBuffer[i]
	}

	// Copy non-overlapping part
	for i := overlapSize; i < len(result); i++ {
		result[i] = samples[i]
	}

	// Save overlap for next frame
	if len(samples) > len(result)-overlapSize {
		start := len(result) - overlapSize
		for i := 0; i < overlapSize && start+i < len(samples); i++ {
			ns.overlapBuffer[i] = samples[start+i]
		}
	}

	return result
}

// postProcess applies post-processing to the output signal
func (ns *NoiseSuppressor) postProcess(samples []float64, isVoice bool) []float64 {
	result := make([]float64, len(samples))

	// Apply comfort noise during silence
	if !isVoice {
		for i := range samples {
			// Add low-level comfort noise
			comfortNoise := (math.Sin(float64(i)*0.1) * 0.001)
			result[i] = samples[i]*0.5 + comfortNoise
		}
	} else {
		copy(result, samples)
	}

	// Apply limiter to prevent clipping
	for i := range result {
		if result[i] > 1.0 {
			result[i] = 1.0
		} else if result[i] < -1.0 {
			result[i] = -1.0
		}
	}

	return result
}

// GetMetrics returns noise suppression metrics
func (ns *NoiseSuppressor) GetMetrics() map[string]interface{} {
	ns.mu.RLock()
	defer ns.mu.RUnlock()

	return map[string]interface{}{
		"total_frames":      ns.totalFrames,
		"suppressed_frames": ns.suppressedFrames,
		"voice_frames":      ns.voiceFrames,
		"noise_level_db":    linearToDb(ns.noiseLevel),
		"signal_level_db":   linearToDb(ns.signalLevel),
		"snr_db":            linearToDb(ns.signalLevel / (ns.noiseLevel + 1e-10)),
		"learning_complete": ns.learningComplete,
	}
}

// Reset resets the noise suppressor state
func (ns *NoiseSuppressor) Reset() {
	ns.mu.Lock()
	defer ns.mu.Unlock()

	// Reset buffers
	for i := range ns.overlapBuffer {
		ns.overlapBuffer[i] = 0
	}
	for i := range ns.smoothing {
		ns.smoothing[i] = 0
	}

	// Reset noise profile for re-learning
	if ns.config.AdaptiveMode {
		ns.isLearning = true
		ns.learningComplete = false
		ns.profileSamples = int(ns.config.LearningDuration * 8000)
		for i := range ns.noiseProfile {
			ns.noiseProfile[i] = 0
		}
	}

	// Reset VAD state
	ns.vadState = false
	ns.vadHangover = 0

	// Reset metrics
	ns.totalFrames = 0
	ns.suppressedFrames = 0
	ns.voiceFrames = 0
	ns.noiseLevel = 0
	ns.signalLevel = 0
}

// Helper functions

func generateHanningWindow(size int) []float64 {
	window := make([]float64, size)
	for i := 0; i < size; i++ {
		window[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(size-1)))
	}
	return window
}

func dbToLinear(db float64) float64 {
	return math.Pow(10, db/20.0)
}

func linearToDb(linear float64) float64 {
	if linear <= 0 {
		return -100.0
	}
	return 20 * math.Log10(linear)
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	// Simple median for 3 values
	if len(values) == 3 {
		if values[0] > values[1] {
			if values[1] > values[2] {
				return values[1]
			} else if values[0] > values[2] {
				return values[2]
			}
			return values[0]
		} else {
			if values[0] > values[2] {
				return values[0]
			} else if values[1] > values[2] {
				return values[2]
			}
			return values[1]
		}
	}
	// For other sizes, return average (simplified)
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
