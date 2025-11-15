package audio

import (
	"context"
	"math"
	"sync"

	"github.com/sirupsen/logrus"
)

// AudioEnhancementConfig contains configuration for audio enhancement
type AudioEnhancementConfig struct {
	// AGC (Automatic Gain Control) settings
	AGC AGCConfig

	// Echo cancellation settings
	EchoCancellation EchoCancellationConfig

	// Dynamic range compression
	Compression CompressionConfig

	// Equalizer settings
	Equalizer EqualizerConfig

	// De-esser settings (reduces sibilance)
	DeEsser DeEsserConfig
}

// AGCConfig contains Automatic Gain Control configuration
type AGCConfig struct {
	Enabled bool

	// Target level in dBFS (typically -20 to -10)
	TargetLevel float64

	// Maximum gain in dB (typically 20-30)
	MaxGain float64

	// Minimum gain in dB (typically -20 to 0)
	MinGain float64

	// Attack time in milliseconds (how fast to increase gain)
	AttackTime float64

	// Release time in milliseconds (how fast to decrease gain)
	ReleaseTime float64

	// Noise gate threshold in dB (silence detection)
	NoiseGateThreshold float64

	// Hold time in milliseconds (prevent rapid gain changes)
	HoldTime float64
}

// EchoCancellationConfig contains echo cancellation configuration
type EchoCancellationConfig struct {
	Enabled bool

	// Filter length in milliseconds (typical 100-500ms)
	FilterLength float64

	// Adaptation rate (0.0-1.0, higher = faster adaptation)
	AdaptationRate float64

	// Nonlinear processing strength (0.0-1.0)
	NonlinearProcessing float64

	// Double-talk detection threshold
	DoubleTalkThreshold float64

	// Comfort noise level in dB
	ComfortNoiseLevel float64

	// Residual echo suppression (0.0-1.0)
	ResidualSuppression float64
}

// CompressionConfig contains dynamic range compression settings
type CompressionConfig struct {
	Enabled bool

	// Threshold in dB (where compression starts)
	Threshold float64

	// Compression ratio (e.g., 4:1)
	Ratio float64

	// Knee width in dB (smooth transition)
	Knee float64

	// Attack time in milliseconds
	AttackTime float64

	// Release time in milliseconds
	ReleaseTime float64

	// Makeup gain in dB
	MakeupGain float64
}

// EqualizerConfig contains equalizer settings
type EqualizerConfig struct {
	Enabled bool

	// Frequency bands (Hz) and their gains (dB)
	Bands []EqualizerBand

	// Pre-amplification in dB
	PreAmp float64
}

// EqualizerBand represents a single equalizer band
type EqualizerBand struct {
	Frequency float64 // Center frequency in Hz
	Gain      float64 // Gain in dB
	Q         float64 // Q factor (bandwidth)
}

// DeEsserConfig contains de-esser configuration
type DeEsserConfig struct {
	Enabled bool

	// Frequency range for sibilance detection (Hz)
	FrequencyMin float64
	FrequencyMax float64

	// Threshold in dB
	Threshold float64

	// Reduction amount (0.0-1.0)
	Reduction float64
}

// DefaultAudioEnhancementConfig returns default audio enhancement configuration
func DefaultAudioEnhancementConfig() *AudioEnhancementConfig {
	return &AudioEnhancementConfig{
		AGC: AGCConfig{
			Enabled:            true,
			TargetLevel:        -18.0,
			MaxGain:            24.0,
			MinGain:            -12.0,
			AttackTime:         10.0,
			ReleaseTime:        100.0,
			NoiseGateThreshold: -50.0,
			HoldTime:           50.0,
		},
		EchoCancellation: EchoCancellationConfig{
			Enabled:             true,
			FilterLength:        200.0,
			AdaptationRate:      0.5,
			NonlinearProcessing: 0.3,
			DoubleTalkThreshold: 0.5,
			ComfortNoiseLevel:   -60.0,
			ResidualSuppression: 0.5,
		},
		Compression: CompressionConfig{
			Enabled:    false,
			Threshold:  -20.0,
			Ratio:      4.0,
			Knee:       2.0,
			AttackTime: 5.0,
			ReleaseTime: 50.0,
			MakeupGain: 0.0,
		},
		Equalizer: EqualizerConfig{
			Enabled: true,
			Bands: []EqualizerBand{
				{Frequency: 100, Gain: -2.0, Q: 0.7},  // Reduce low rumble
				{Frequency: 300, Gain: 1.0, Q: 0.7},   // Boost warmth
				{Frequency: 1000, Gain: 0.5, Q: 0.7},  // Slight presence boost
				{Frequency: 3000, Gain: 1.5, Q: 0.7},  // Clarity boost
				{Frequency: 8000, Gain: -1.0, Q: 0.7}, // Reduce harshness
			},
			PreAmp: 0.0,
		},
		DeEsser: DeEsserConfig{
			Enabled:      true,
			FrequencyMin: 4000.0,
			FrequencyMax: 10000.0,
			Threshold:    -30.0,
			Reduction:    0.5,
		},
	}
}

// AudioEnhancer provides comprehensive audio enhancement
type AudioEnhancer struct {
	logger *logrus.Logger
	config *AudioEnhancementConfig
	mu     sync.RWMutex

	// AGC components
	agc *AutomaticGainControl

	// Echo cancellation components
	echo *EchoCanceller

	// Compressor
	compressor *DynamicRangeCompressor

	// Equalizer
	equalizer *ParametricEqualizer

	// De-esser
	deesser *DeEsser

	// Processing metrics
	metrics AudioEnhancementMetrics
}

// AudioEnhancementMetrics tracks enhancement metrics
type AudioEnhancementMetrics struct {
	InputLevel      float64
	OutputLevel     float64
	CurrentGain     float64
	EchoReduction   float64
	CompressionGain float64
	ProcessedFrames uint64
}

// NewAudioEnhancer creates a new audio enhancer
func NewAudioEnhancer(logger *logrus.Logger, config *AudioEnhancementConfig) *AudioEnhancer {
	if config == nil {
		config = DefaultAudioEnhancementConfig()
	}

	ae := &AudioEnhancer{
		logger:     logger,
		config:     config,
		agc:        NewAutomaticGainControl(&config.AGC),
		echo:       NewEchoCanceller(&config.EchoCancellation),
		compressor: NewDynamicRangeCompressor(&config.Compression),
		equalizer:  NewParametricEqualizer(&config.Equalizer),
		deesser:    NewDeEsser(&config.DeEsser),
	}

	return ae
}

// ProcessAudio applies all enhancement stages to audio
func (ae *AudioEnhancer) ProcessAudio(ctx context.Context, samples []float64) ([]float64, error) {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	// Track input level
	ae.metrics.InputLevel = calculateRMS(samples)
	ae.metrics.ProcessedFrames++

	// Stage 1: Echo cancellation (should be first)
	output := samples
	if ae.config.EchoCancellation.Enabled {
		output = ae.echo.Process(output)
		ae.metrics.EchoReduction = ae.echo.GetReduction()
	}

	// Stage 2: Noise gate (part of AGC)
	if ae.config.AGC.Enabled {
		output = ae.agc.ApplyNoiseGate(output)
	}

	// Stage 3: Equalizer
	if ae.config.Equalizer.Enabled {
		output = ae.equalizer.Process(output)
	}

	// Stage 4: De-esser
	if ae.config.DeEsser.Enabled {
		output = ae.deesser.Process(output)
	}

	// Stage 5: AGC
	if ae.config.AGC.Enabled {
		output, ae.metrics.CurrentGain = ae.agc.Process(output)
	}

	// Stage 6: Compression
	if ae.config.Compression.Enabled {
		output, ae.metrics.CompressionGain = ae.compressor.Process(output)
	}

	// Track output level
	ae.metrics.OutputLevel = calculateRMS(output)

	return output, nil
}

// GetMetrics returns current enhancement metrics
func (ae *AudioEnhancer) GetMetrics() AudioEnhancementMetrics {
	ae.mu.RLock()
	defer ae.mu.RUnlock()
	return ae.metrics
}

// AutomaticGainControl implements AGC
type AutomaticGainControl struct {
	config *AGCConfig
	mu     sync.Mutex

	currentGain   float64
	targetLevel   float64
	envelope      float64
	gateThreshold float64
	holdCounter   int

	// Time constants
	attackCoeff  float64
	releaseCoeff float64
}

// NewAutomaticGainControl creates a new AGC processor
func NewAutomaticGainControl(config *AGCConfig) *AutomaticGainControl {
	sampleRate := 8000.0 // Assuming 8kHz

	agc := &AutomaticGainControl{
		config:        config,
		currentGain:   1.0,
		targetLevel:   dbToLinear(config.TargetLevel),
		gateThreshold: dbToLinear(config.NoiseGateThreshold),
	}

	// Calculate time constants
	agc.attackCoeff = 1.0 - math.Exp(-1.0/(config.AttackTime*sampleRate/1000.0))
	agc.releaseCoeff = 1.0 - math.Exp(-1.0/(config.ReleaseTime*sampleRate/1000.0))

	return agc
}

// Process applies AGC to audio samples
func (agc *AutomaticGainControl) Process(samples []float64) ([]float64, float64) {
	agc.mu.Lock()
	defer agc.mu.Unlock()

	if !agc.config.Enabled {
		return samples, 1.0
	}

	output := make([]float64, len(samples))
	
	for i, sample := range samples {
		// Update envelope follower
		absSample := math.Abs(sample)
		if absSample > agc.envelope {
			agc.envelope += agc.attackCoeff * (absSample - agc.envelope)
		} else {
			agc.envelope += agc.releaseCoeff * (absSample - agc.envelope)
		}

		// Calculate desired gain
		desiredGain := 1.0
		if agc.envelope > 0.001 {
			desiredGain = agc.targetLevel / agc.envelope
		}

		// Limit gain
		if desiredGain > dbToLinear(agc.config.MaxGain) {
			desiredGain = dbToLinear(agc.config.MaxGain)
		} else if desiredGain < dbToLinear(agc.config.MinGain) {
			desiredGain = dbToLinear(agc.config.MinGain)
		}

		// Smooth gain changes
		if desiredGain > agc.currentGain {
			agc.currentGain += agc.attackCoeff * (desiredGain - agc.currentGain)
		} else {
			agc.currentGain += agc.releaseCoeff * (desiredGain - agc.currentGain)
		}

		// Apply gain
		output[i] = sample * agc.currentGain

		// Prevent clipping
		if output[i] > 0.95 {
			output[i] = 0.95
		} else if output[i] < -0.95 {
			output[i] = -0.95
		}
	}

	return output, agc.currentGain
}

// ApplyNoiseGate applies noise gate to silence low-level noise
func (agc *AutomaticGainControl) ApplyNoiseGate(samples []float64) []float64 {
	agc.mu.Lock()
	defer agc.mu.Unlock()

	output := make([]float64, len(samples))
	
	for i, sample := range samples {
		level := math.Abs(sample)
		
		if level < agc.gateThreshold {
			// Below threshold - apply gate
			if agc.holdCounter > 0 {
				agc.holdCounter--
				output[i] = sample // Hold period
			} else {
				output[i] = sample * 0.1 // Attenuate
			}
		} else {
			// Above threshold - pass through
			agc.holdCounter = int(agc.config.HoldTime * 8) // Reset hold counter
			output[i] = sample
		}
	}

	return output
}

// EchoCanceller implements acoustic echo cancellation
type EchoCanceller struct {
	config *EchoCancellationConfig
	mu     sync.Mutex

	// Adaptive filter coefficients
	filterCoeffs []float64
	filterBuffer []float64
	
	// Reference signal buffer (far-end)
	referenceBuffer []float64
	
	// Error signal for adaptation
	errorSignal []float64
	
	// Double-talk detector
	nearEndPower float64
	farEndPower  float64
	doubleTalk   bool
	
	// Metrics
	echoReduction float64
}

// NewEchoCanceller creates a new echo canceller
func NewEchoCanceller(config *EchoCancellationConfig) *EchoCanceller {
	filterLen := int(config.FilterLength * 8) // 8 samples per ms at 8kHz

	return &EchoCanceller{
		config:          config,
		filterCoeffs:    make([]float64, filterLen),
		filterBuffer:    make([]float64, filterLen),
		referenceBuffer: make([]float64, filterLen),
		errorSignal:     make([]float64, filterLen),
	}
}

// Process removes echo from audio signal
func (ec *EchoCanceller) Process(samples []float64) []float64 {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if !ec.config.Enabled {
		return samples
	}

	output := make([]float64, len(samples))
	
	for i, sample := range samples {
		// Update reference buffer (simulated far-end signal)
		ec.updateReferenceBuffer(sample)
		
		// Estimate echo using adaptive filter
		echoEstimate := ec.estimateEcho()
		
		// Subtract estimated echo
		errorSignal := sample - echoEstimate
		
		// Detect double-talk
		ec.detectDoubleTalk(sample, echoEstimate)
		
		// Update filter coefficients (if not double-talk)
		if !ec.doubleTalk {
			ec.updateFilterCoefficients(errorSignal)
		}
		
		// Apply nonlinear processing
		processed := ec.applyNonlinearProcessing(errorSignal)
		
		// Add comfort noise
		processed = ec.addComfortNoise(processed)
		
		// Apply residual echo suppression
		output[i] = ec.suppressResidualEcho(processed, echoEstimate)
		
		// Update metrics
		ec.updateMetrics(sample, output[i])
	}

	return output
}

// updateReferenceBuffer updates the reference signal buffer
func (ec *EchoCanceller) updateReferenceBuffer(sample float64) {
	if len(ec.referenceBuffer) == 0 {
		return
	}
	// Shift buffer and add new sample
	copy(ec.referenceBuffer[1:], ec.referenceBuffer[:len(ec.referenceBuffer)-1])
	ec.referenceBuffer[0] = sample
}

// estimateEcho estimates the echo signal
func (ec *EchoCanceller) estimateEcho() float64 {
	estimate := 0.0
	for j := 0; j < len(ec.filterCoeffs); j++ {
		if j < len(ec.referenceBuffer) {
			estimate += ec.filterCoeffs[j] * ec.referenceBuffer[j]
		}
	}
	return estimate
}

// detectDoubleTalk detects simultaneous near-end and far-end speech
func (ec *EchoCanceller) detectDoubleTalk(nearEnd, farEnd float64) {
	// Update power estimates
	alpha := 0.99
	ec.nearEndPower = alpha*ec.nearEndPower + (1-alpha)*nearEnd*nearEnd
	ec.farEndPower = alpha*ec.farEndPower + (1-alpha)*farEnd*farEnd
	
	// Detect double-talk
	if ec.farEndPower > 0 {
		ratio := ec.nearEndPower / ec.farEndPower
		ec.doubleTalk = ratio > ec.config.DoubleTalkThreshold
	} else {
		ec.doubleTalk = false
	}
}

// updateFilterCoefficients updates adaptive filter using NLMS algorithm
func (ec *EchoCanceller) updateFilterCoefficients(error float64) {
	// Calculate step size
	power := 0.0
	for _, ref := range ec.referenceBuffer {
		power += ref * ref
	}
	
	if power > 0.001 {
		stepSize := ec.config.AdaptationRate / (power + 0.001)
		
		// Update coefficients
		for j := 0; j < len(ec.filterCoeffs); j++ {
			if j < len(ec.referenceBuffer) {
				ec.filterCoeffs[j] += stepSize * error * ec.referenceBuffer[j]
			}
		}
	}
}

// applyNonlinearProcessing applies NLP to remove residual echo
func (ec *EchoCanceller) applyNonlinearProcessing(signal float64) float64 {
	threshold := 0.01 * ec.config.NonlinearProcessing
	
	if math.Abs(signal) < threshold {
		// Suppress small signals (likely residual echo)
		return signal * 0.1
	}
	
	return signal
}

// addComfortNoise adds comfort noise during suppression
func (ec *EchoCanceller) addComfortNoise(signal float64) float64 {
	noiseLevel := dbToLinear(ec.config.ComfortNoiseLevel)
	noise := (math.Sin(float64(ec.farEndPower*1000)) * noiseLevel)
	return signal + noise
}

// suppressResidualEcho applies residual echo suppression
func (ec *EchoCanceller) suppressResidualEcho(signal, echoEstimate float64) float64 {
	if math.Abs(echoEstimate) > 0.001 {
		suppression := 1.0 - ec.config.ResidualSuppression*math.Min(1.0, math.Abs(echoEstimate)/0.1)
		return signal * suppression
	}
	return signal
}

// updateMetrics updates echo cancellation metrics
func (ec *EchoCanceller) updateMetrics(input, output float64) {
	if math.Abs(input) > 0.001 {
		ec.echoReduction = 1.0 - math.Abs(output)/math.Abs(input)
	}
}

// GetReduction returns current echo reduction amount
func (ec *EchoCanceller) GetReduction() float64 {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.echoReduction
}

// DynamicRangeCompressor implements audio compression
type DynamicRangeCompressor struct {
	config      *CompressionConfig
	envelope    float64
	attackCoeff float64
	releaseCoeff float64
}

// NewDynamicRangeCompressor creates a new compressor
func NewDynamicRangeCompressor(config *CompressionConfig) *DynamicRangeCompressor {
	sampleRate := 8000.0
	
	return &DynamicRangeCompressor{
		config:       config,
		attackCoeff:  1.0 - math.Exp(-1.0/(config.AttackTime*sampleRate/1000.0)),
		releaseCoeff: 1.0 - math.Exp(-1.0/(config.ReleaseTime*sampleRate/1000.0)),
	}
}

// Process applies dynamic range compression
func (drc *DynamicRangeCompressor) Process(samples []float64) ([]float64, float64) {
	if !drc.config.Enabled {
		return samples, 1.0
	}

	output := make([]float64, len(samples))
	avgGain := 0.0
	
	for i, sample := range samples {
		// Update envelope
		level := math.Abs(sample)
		if level > drc.envelope {
			drc.envelope += drc.attackCoeff * (level - drc.envelope)
		} else {
			drc.envelope += drc.releaseCoeff * (level - drc.envelope)
		}
		
		// Calculate gain reduction
		gainReduction := 1.0
		levelDb := linearToDb(drc.envelope)
		
		if levelDb > drc.config.Threshold {
			// Apply compression
			excess := levelDb - drc.config.Threshold
			
			// Apply soft knee
			if excess < drc.config.Knee {
				ratio := 1.0 + (drc.config.Ratio-1.0) * (excess/drc.config.Knee) * (excess/drc.config.Knee)
				excess = excess / ratio
			} else {
				excess = excess / drc.config.Ratio
			}
			
			gainReduction = dbToLinear(-excess)
		}
		
		// Apply gain reduction and makeup gain
		gain := gainReduction * dbToLinear(drc.config.MakeupGain)
		output[i] = sample * gain
		avgGain += gain
	}
	
	return output, avgGain / float64(len(samples))
}

// ParametricEqualizer implements multi-band parametric EQ
type ParametricEqualizer struct {
	config *EqualizerConfig
	bands  []*BiquadFilter
}

// NewParametricEqualizer creates a new equalizer
func NewParametricEqualizer(config *EqualizerConfig) *ParametricEqualizer {
	eq := &ParametricEqualizer{
		config: config,
		bands:  make([]*BiquadFilter, len(config.Bands)),
	}
	
	// Create biquad filters for each band
	for i, band := range config.Bands {
		eq.bands[i] = NewBiquadFilter(band.Frequency, band.Gain, band.Q, 8000.0)
	}
	
	return eq
}

// Process applies equalization
func (eq *ParametricEqualizer) Process(samples []float64) []float64 {
	if !eq.config.Enabled {
		return samples
	}

	output := make([]float64, len(samples))
	copy(output, samples)
	
	// Apply pre-amplification
	if eq.config.PreAmp != 0 {
		preAmpGain := dbToLinear(eq.config.PreAmp)
		for i := range output {
			output[i] *= preAmpGain
		}
	}
	
	// Apply each band
	for _, band := range eq.bands {
		output = band.Process(output)
	}
	
	return output
}

// BiquadFilter implements a second-order IIR filter
type BiquadFilter struct {
	a0, a1, a2 float64 // Feedforward coefficients
	b1, b2     float64 // Feedback coefficients
	x1, x2     float64 // Input delay line
	y1, y2     float64 // Output delay line
}

// NewBiquadFilter creates a peaking EQ biquad filter
func NewBiquadFilter(frequency, gain, q, sampleRate float64) *BiquadFilter {
	omega := 2.0 * math.Pi * frequency / sampleRate
	alpha := math.Sin(omega) / (2.0 * q)
	A := math.Sqrt(dbToLinear(gain))
	
	// Peaking EQ coefficients
	b0 := 1.0 + alpha*A
	b1 := -2.0 * math.Cos(omega)
	b2 := 1.0 - alpha*A
	a0 := 1.0 + alpha/A
	a1 := -2.0 * math.Cos(omega)
	a2 := 1.0 - alpha/A
	
	// Normalize
	return &BiquadFilter{
		a0: b0 / a0,
		a1: b1 / a0,
		a2: b2 / a0,
		b1: a1 / a0,
		b2: a2 / a0,
	}
}

// Process applies the biquad filter
func (bf *BiquadFilter) Process(samples []float64) []float64 {
	output := make([]float64, len(samples))
	
	for i, x0 := range samples {
		// Direct Form II
		y0 := bf.a0*x0 + bf.a1*bf.x1 + bf.a2*bf.x2 - bf.b1*bf.y1 - bf.b2*bf.y2
		
		// Update delay lines
		bf.x2 = bf.x1
		bf.x1 = x0
		bf.y2 = bf.y1
		bf.y1 = y0
		
		output[i] = y0
	}
	
	return output
}

// DeEsser reduces sibilance in audio
type DeEsser struct {
	config       *DeEsserConfig
	detector     *BiquadFilter
	envelope     float64
	attackCoeff  float64
	releaseCoeff float64
}

// NewDeEsser creates a new de-esser
func NewDeEsser(config *DeEsserConfig) *DeEsser {
	centerFreq := (config.FrequencyMin + config.FrequencyMax) / 2
	bandwidth := config.FrequencyMax - config.FrequencyMin
	q := centerFreq / bandwidth
	
	return &DeEsser{
		config:       config,
		detector:     NewBiquadFilter(centerFreq, 0, q, 8000.0),
		attackCoeff:  0.99,
		releaseCoeff: 0.999,
	}
}

// Process applies de-essing
func (de *DeEsser) Process(samples []float64) []float64 {
	if !de.config.Enabled {
		return samples
	}

	// Detect sibilance
	detected := de.detector.Process(samples)
	output := make([]float64, len(samples))
	
	for i := range samples {
		// Update envelope of detected signal
		level := math.Abs(detected[i])
		if level > de.envelope {
			de.envelope = de.attackCoeff*de.envelope + (1-de.attackCoeff)*level
		} else {
			de.envelope = de.releaseCoeff*de.envelope + (1-de.releaseCoeff)*level
		}
		
		// Calculate reduction
		reduction := 1.0
		if linearToDb(de.envelope) > de.config.Threshold {
			reduction = 1.0 - de.config.Reduction
		}
		
		// Apply reduction only to high frequencies
		highFreq := detected[i]
		lowFreq := samples[i] - highFreq
		output[i] = lowFreq + highFreq*reduction
	}
	
	return output
}

// Helper function to calculate RMS
func calculateRMS(samples []float64) float64 {
	sum := 0.0
	for _, s := range samples {
		sum += s * s
	}
	return math.Sqrt(sum / float64(len(samples)))
}