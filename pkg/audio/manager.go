package audio

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ProcessingManager orchestrates audio processing for RTP streams
type ProcessingManager struct {
	// Configuration
	config ProcessingConfig

	// Audio processors
	pipeline     *AudioPipeline
	vad          *VoiceActivityDetector
	noiseReducer *NoiseReducer
	channelMixer *ChannelMixer

	// Runtime state
	enabled bool
	mu      sync.Mutex
	logger  *logrus.Logger

	// Metrics
	packetsProcessed uint64
	bytesProcessed   uint64
	processingErrors uint64
	voiceDetected    uint64
	silenceDetected  uint64
	startTime        time.Time
}

// NewProcessingManager creates a new audio processing manager
func NewProcessingManager(config ProcessingConfig, logger *logrus.Logger) *ProcessingManager {
	pipeline := NewAudioPipeline(config)

	// Create audio processors
	vad := NewVoiceActivityDetector(config)
	noiseReducer := NewNoiseReducer(config)
	channelMixer := NewChannelMixer(config)

	// Add processors to pipeline in the correct order
	if config.ChannelCount > 1 && config.MixChannels {
		pipeline.AddProcessor(channelMixer) // First mix channels if needed
	}

	if config.EnableNoiseReduction {
		pipeline.AddProcessor(noiseReducer) // Then reduce noise
	}

	if config.EnableVAD {
		pipeline.AddProcessor(vad) // Finally detect voice activity
	}

	return &ProcessingManager{
		config:       config,
		pipeline:     pipeline,
		vad:          vad,
		noiseReducer: noiseReducer,
		channelMixer: channelMixer,
		enabled:      true,
		logger:       logger,

		// Initialize metrics
		packetsProcessed: 0,
		bytesProcessed:   0,
		processingErrors: 0,
		voiceDetected:    0,
		silenceDetected:  0,
		startTime:        time.Now(),
	}
}

// ProcessAudio processes a chunk of audio data
func (pm *ProcessingManager) ProcessAudio(data []byte) ([]byte, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.enabled {
		return data, nil
	}

	// Increment metrics
	atomic.AddUint64(&pm.packetsProcessed, 1)
	atomic.AddUint64(&pm.bytesProcessed, uint64(len(data)))

	// Use the pipeline to process the audio
	processed, err := pm.pipeline.Process(data)

	// Track errors
	if err != nil {
		atomic.AddUint64(&pm.processingErrors, 1)
		return nil, err
	}

	// Track voice activity if VAD is enabled
	if pm.config.EnableVAD && pm.vad != nil {
		if pm.vad.IsVoiceActive() {
			atomic.AddUint64(&pm.voiceDetected, 1)
		} else {
			atomic.AddUint64(&pm.silenceDetected, 1)
		}
	}

	// Log periodic stats (every 100 packets)
	if pm.packetsProcessed%100 == 0 {
		pm.logStats()
	}

	return processed, nil
}

// WrapReader wraps an io.Reader with audio processing
func (pm *ProcessingManager) WrapReader(reader io.Reader) io.Reader {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.enabled {
		return reader
	}

	// Use the pipeline to process the audio stream
	return pm.pipeline.Start(reader)
}

// Enable enables or disables audio processing
func (pm *ProcessingManager) Enable(enabled bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.enabled = enabled
}

// IsEnabled returns whether audio processing is enabled
func (pm *ProcessingManager) IsEnabled() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.enabled
}

// SetVADEnabled enables or disables voice activity detection
func (pm *ProcessingManager) SetVADEnabled(enabled bool) {
	pm.vad.SetSilenceSuppression(enabled)
}

// SetNoiseReductionEnabled enables or disables noise reduction
func (pm *ProcessingManager) SetNoiseReductionEnabled(enabled bool) {
	pm.noiseReducer.SetEnabled(enabled)
}

// SetChannelMixingEnabled enables or disables channel mixing
func (pm *ProcessingManager) SetChannelMixingEnabled(enabled bool) {
	pm.channelMixer.SetMixChannels(enabled)
}

// Reset resets all audio processors
func (pm *ProcessingManager) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.vad.Reset()
	pm.noiseReducer.Reset()
	pm.channelMixer.Reset()
}

// Close releases resources
func (pm *ProcessingManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Stop the pipeline
	pm.pipeline.Stop()

	// Close all processors
	pm.vad.Close()
	pm.noiseReducer.Close()
	pm.channelMixer.Close()

	return nil
}

// IsVoiceActive returns whether voice activity is currently detected
func (pm *ProcessingManager) IsVoiceActive() bool {
	return pm.vad.IsVoiceActive()
}

// GetNoiseFloor returns the current estimated noise floor
func (pm *ProcessingManager) GetNoiseFloor() float64 {
	return pm.noiseReducer.GetNoiseFloor()
}

// AudioProcessingStats contains metrics about audio processing
type AudioProcessingStats struct {
	PacketsProcessed      uint64  `json:"packets_processed"`
	BytesProcessed        uint64  `json:"bytes_processed"`
	ProcessingErrors      uint64  `json:"processing_errors"`
	VoiceDetected         uint64  `json:"voice_detected"`
	SilenceDetected       uint64  `json:"silence_detected"`
	VoiceRatio            float64 `json:"voice_ratio"`
	PacketsPerSecond      float64 `json:"packets_per_second"`
	UptimeSeconds         float64 `json:"uptime_seconds"`
	NoiseFloor            float64 `json:"noise_floor"`
	VADEnabled            bool    `json:"vad_enabled"`
	NoiseReductionEnabled bool    `json:"noise_reduction_enabled"`
}

// GetStats returns the current audio processing statistics
func (pm *ProcessingManager) GetStats() AudioProcessingStats {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Calculate uptime
	uptime := time.Since(pm.startTime).Seconds()

	// Calculate voice ratio
	var voiceRatio float64 = 0
	if pm.config.EnableVAD && (pm.voiceDetected+pm.silenceDetected > 0) {
		voiceRatio = float64(pm.voiceDetected) / float64(pm.voiceDetected+pm.silenceDetected)
	}

	// Calculate packet rate
	packetsPerSec := float64(pm.packetsProcessed) / uptime

	// Get noise floor
	noiseFloor := 0.0
	if pm.config.EnableNoiseReduction && pm.noiseReducer != nil {
		noiseFloor = pm.noiseReducer.GetNoiseFloor()
	}

	return AudioProcessingStats{
		PacketsProcessed:      pm.packetsProcessed,
		BytesProcessed:        pm.bytesProcessed,
		ProcessingErrors:      pm.processingErrors,
		VoiceDetected:         pm.voiceDetected,
		SilenceDetected:       pm.silenceDetected,
		VoiceRatio:            voiceRatio,
		PacketsPerSecond:      packetsPerSec,
		UptimeSeconds:         uptime,
		NoiseFloor:            noiseFloor,
		VADEnabled:            pm.config.EnableVAD,
		NoiseReductionEnabled: pm.config.EnableNoiseReduction,
	}
}

// logStats logs audio processing metrics and statistics
func (pm *ProcessingManager) logStats() {
	// Get the stats
	stats := pm.GetStats()

	// Log all metrics
	pm.logger.WithFields(logrus.Fields{
		"packets_processed": stats.PacketsProcessed,
		"bytes_processed":   stats.BytesProcessed,
		"errors":            stats.ProcessingErrors,
		"voice_detected":    stats.VoiceDetected,
		"silence_detected":  stats.SilenceDetected,
		"voice_ratio":       stats.VoiceRatio,
		"packets_per_sec":   stats.PacketsPerSecond,
		"uptime_sec":        stats.UptimeSeconds,
		"noise_floor":       stats.NoiseFloor,
		"vad_enabled":       stats.VADEnabled,
		"nr_enabled":        stats.NoiseReductionEnabled,
	}).Info("Audio processing statistics")
}

// UpdateConfig updates the audio processing configuration
func (pm *ProcessingManager) UpdateConfig(config ProcessingConfig) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Store the new configuration
	pm.config = config

	// Reset and update processors
	pm.Reset()

	// Update state based on new config
	pm.SetVADEnabled(config.EnableVAD)
	pm.SetNoiseReductionEnabled(config.EnableNoiseReduction)
	pm.SetChannelMixingEnabled(config.MixChannels)
}
