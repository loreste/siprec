package config

import "time"

// AudioEnhancementConfig contains comprehensive audio enhancement settings
type AudioEnhancementConfig struct {
	// Enable audio enhancement processing
	Enabled bool `json:"enabled" env:"AUDIO_ENHANCEMENT_ENABLED" default:"true"`

	// Noise Suppression
	NoiseSuppression NoiseSuppressionConfig `json:"noise_suppression"`

	// Automatic Gain Control
	AGC AGCConfig `json:"agc"`

	// Echo Cancellation
	EchoCancellation EchoCancellationConfig `json:"echo_cancellation"`

	// Multi-channel Processing
	MultiChannel MultiChannelConfig `json:"multi_channel"`

	// Audio Fingerprinting
	Fingerprinting FingerprintingConfig `json:"fingerprinting"`

	// Dynamic Range Compression
	Compression CompressionConfig `json:"compression"`

	// Equalizer
	Equalizer EqualizerConfig `json:"equalizer"`

	// De-esser
	DeEsser DeEsserConfig `json:"de_esser"`
}

// NoiseSuppressionConfig contains noise suppression settings
type NoiseSuppressionConfig struct {
	Enabled                   bool    `json:"enabled" env:"NOISE_SUPPRESSION_ENABLED" default:"true"`
	SuppressionLevel          float64 `json:"suppression_level" env:"NOISE_SUPPRESSION_LEVEL" default:"0.7"`
	VADThreshold              float64 `json:"vad_threshold" env:"VAD_THRESHOLD" default:"0.3"`
	SpectralSubtractionFactor float64 `json:"spectral_subtraction_factor" env:"SPECTRAL_SUBTRACTION_FACTOR" default:"2.0"`
	NoiseFloorDB              float64 `json:"noise_floor_db" env:"NOISE_FLOOR_DB" default:"-60"`
	WindowSize                int     `json:"window_size" env:"NS_WINDOW_SIZE" default:"512"`
	OverlapFactor             float64 `json:"overlap_factor" env:"NS_OVERLAP_FACTOR" default:"0.5"`
	HighPassCutoff            float64 `json:"high_pass_cutoff" env:"HIGH_PASS_CUTOFF" default:"80"`
	AdaptiveMode              bool    `json:"adaptive_mode" env:"NS_ADAPTIVE_MODE" default:"true"`
	LearningDuration          float64 `json:"learning_duration" env:"NS_LEARNING_DURATION" default:"0.5"`
}

// AGCConfig contains Automatic Gain Control settings
type AGCConfig struct {
	Enabled            bool    `json:"enabled" env:"AGC_ENABLED" default:"true"`
	TargetLevel        float64 `json:"target_level" env:"AGC_TARGET_LEVEL" default:"-18"`
	MaxGain            float64 `json:"max_gain" env:"AGC_MAX_GAIN" default:"24"`
	MinGain            float64 `json:"min_gain" env:"AGC_MIN_GAIN" default:"-12"`
	AttackTime         float64 `json:"attack_time" env:"AGC_ATTACK_TIME" default:"10"`
	ReleaseTime        float64 `json:"release_time" env:"AGC_RELEASE_TIME" default:"100"`
	NoiseGateThreshold float64 `json:"noise_gate_threshold" env:"NOISE_GATE_THRESHOLD" default:"-50"`
	HoldTime           float64 `json:"hold_time" env:"AGC_HOLD_TIME" default:"50"`
}

// EchoCancellationConfig contains echo cancellation settings
type EchoCancellationConfig struct {
	Enabled             bool    `json:"enabled" env:"ECHO_CANCELLATION_ENABLED" default:"true"`
	FilterLength        float64 `json:"filter_length" env:"ECHO_FILTER_LENGTH" default:"200"`
	AdaptationRate      float64 `json:"adaptation_rate" env:"ECHO_ADAPTATION_RATE" default:"0.5"`
	NonlinearProcessing float64 `json:"nonlinear_processing" env:"ECHO_NONLINEAR_PROCESSING" default:"0.3"`
	DoubleTalkThreshold float64 `json:"double_talk_threshold" env:"DOUBLE_TALK_THRESHOLD" default:"0.5"`
	ComfortNoiseLevel   float64 `json:"comfort_noise_level" env:"COMFORT_NOISE_LEVEL" default:"-60"`
	ResidualSuppression float64 `json:"residual_suppression" env:"RESIDUAL_SUPPRESSION" default:"0.5"`
}

// MultiChannelConfig contains multi-channel audio settings
type MultiChannelConfig struct {
	Enabled                   bool          `json:"enabled" env:"MULTI_CHANNEL_ENABLED" default:"false"`
	Configuration             string        `json:"configuration" env:"CHANNEL_CONFIGURATION" default:"stereo"`
	ChannelCount              int           `json:"channel_count" env:"CHANNEL_COUNT" default:"2"`
	EnableTelephonySeparation bool          `json:"enable_telephony_separation" env:"TELEPHONY_SEPARATION" default:"true"`
	EnableStereoWidening      bool          `json:"enable_stereo_widening" env:"STEREO_WIDENING" default:"false"`
	StereoWidth               float64       `json:"stereo_width" env:"STEREO_WIDTH" default:"1.0"`
	EnableMixing              bool          `json:"enable_mixing" env:"CHANNEL_MIXING" default:"false"`
	MaxDesync                 time.Duration `json:"max_desync" env:"MAX_CHANNEL_DESYNC" default:"50ms"`
	ResyncInterval            time.Duration `json:"resync_interval" env:"CHANNEL_RESYNC_INTERVAL" default:"1s"`
	BufferSize                int           `json:"buffer_size" env:"CHANNEL_BUFFER_SIZE" default:"4096"`
	BufferLatency             time.Duration `json:"buffer_latency" env:"CHANNEL_BUFFER_LATENCY" default:"20ms"`

	// Channel roles for telephony
	CallerChannelID int `json:"caller_channel_id" env:"CALLER_CHANNEL_ID" default:"0"`
	CalleeChannelID int `json:"callee_channel_id" env:"CALLEE_CHANNEL_ID" default:"1"`
}

// FingerprintingConfig contains audio fingerprinting settings
type FingerprintingConfig struct {
	Enabled               bool          `json:"enabled" env:"FINGERPRINTING_ENABLED" default:"true"`
	WindowSize            int           `json:"window_size" env:"FP_WINDOW_SIZE" default:"2048"`
	HopSize               int           `json:"hop_size" env:"FP_HOP_SIZE" default:"512"`
	PeakNeighborhood      int           `json:"peak_neighborhood" env:"FP_PEAK_NEIGHBORHOOD" default:"5"`
	PeakThreshold         float64       `json:"peak_threshold" env:"FP_PEAK_THRESHOLD" default:"0.1"`
	MaxPeaksPerFrame      int           `json:"max_peaks_per_frame" env:"FP_MAX_PEAKS_PER_FRAME" default:"5"`
	TargetZoneSize        int           `json:"target_zone_size" env:"FP_TARGET_ZONE_SIZE" default:"5"`
	MaxPairsPerAnchor     int           `json:"max_pairs_per_anchor" env:"FP_MAX_PAIRS_PER_ANCHOR" default:"3"`
	MinMatchScore         float64       `json:"min_match_score" env:"FP_MIN_MATCH_SCORE" default:"0.7"`
	MinMatchingHashes     int           `json:"min_matching_hashes" env:"FP_MIN_MATCHING_HASHES" default:"10"`
	TimeToleranceMs       int           `json:"time_tolerance_ms" env:"FP_TIME_TOLERANCE_MS" default:"100"`
	MaxStoredFingerprints int           `json:"max_stored_fingerprints" env:"FP_MAX_STORED" default:"10000"`
	RetentionPeriod       time.Duration `json:"retention_period" env:"FP_RETENTION_PERIOD" default:"24h"`
	ParallelProcessing    bool          `json:"parallel_processing" env:"FP_PARALLEL_PROCESSING" default:"true"`
	BatchSize             int           `json:"batch_size" env:"FP_BATCH_SIZE" default:"100"`

	// Duplicate detection behavior
	BlockDuplicates    bool `json:"block_duplicates" env:"FP_BLOCK_DUPLICATES" default:"false"`
	AlertOnDuplicates  bool `json:"alert_on_duplicates" env:"FP_ALERT_ON_DUPLICATES" default:"true"`
	TagDuplicates      bool `json:"tag_duplicates" env:"FP_TAG_DUPLICATES" default:"true"`
}

// CompressionConfig contains dynamic range compression settings
type CompressionConfig struct {
	Enabled    bool    `json:"enabled" env:"COMPRESSION_ENABLED" default:"false"`
	Threshold  float64 `json:"threshold" env:"COMPRESSION_THRESHOLD" default:"-20"`
	Ratio      float64 `json:"ratio" env:"COMPRESSION_RATIO" default:"4"`
	Knee       float64 `json:"knee" env:"COMPRESSION_KNEE" default:"2"`
	AttackTime float64 `json:"attack_time" env:"COMPRESSION_ATTACK" default:"5"`
	ReleaseTime float64 `json:"release_time" env:"COMPRESSION_RELEASE" default:"50"`
	MakeupGain float64 `json:"makeup_gain" env:"COMPRESSION_MAKEUP_GAIN" default:"0"`
}

// EqualizerConfig contains equalizer settings
type EqualizerConfig struct {
	Enabled bool               `json:"enabled" env:"EQ_ENABLED" default:"true"`
	PreAmp  float64            `json:"pre_amp" env:"EQ_PRE_AMP" default:"0"`
	Bands   []EqualizerBandConfig `json:"bands"`

	// Preset configurations
	UsePreset   bool   `json:"use_preset" env:"EQ_USE_PRESET" default:"true"`
	PresetName  string `json:"preset_name" env:"EQ_PRESET_NAME" default:"voice_clarity"`
}

// EqualizerBandConfig represents a single equalizer band configuration
type EqualizerBandConfig struct {
	Frequency float64 `json:"frequency"`
	Gain      float64 `json:"gain"`
	Q         float64 `json:"q"`
}

// DeEsserConfig contains de-esser settings
type DeEsserConfig struct {
	Enabled      bool    `json:"enabled" env:"DEESSER_ENABLED" default:"true"`
	FrequencyMin float64 `json:"frequency_min" env:"DEESSER_FREQ_MIN" default:"4000"`
	FrequencyMax float64 `json:"frequency_max" env:"DEESSER_FREQ_MAX" default:"10000"`
	Threshold    float64 `json:"threshold" env:"DEESSER_THRESHOLD" default:"-30"`
	Reduction    float64 `json:"reduction" env:"DEESSER_REDUCTION" default:"0.5"`
}

// GetDefaultAudioEnhancementConfig returns default audio enhancement configuration
func GetDefaultAudioEnhancementConfig() *AudioEnhancementConfig {
	return &AudioEnhancementConfig{
		Enabled: true,
		NoiseSuppression: NoiseSuppressionConfig{
			Enabled:                   true,
			SuppressionLevel:          0.7,
			VADThreshold:              0.3,
			SpectralSubtractionFactor: 2.0,
			NoiseFloorDB:              -60,
			WindowSize:                512,
			OverlapFactor:             0.5,
			HighPassCutoff:            80,
			AdaptiveMode:              true,
			LearningDuration:          0.5,
		},
		AGC: AGCConfig{
			Enabled:            true,
			TargetLevel:        -18,
			MaxGain:            24,
			MinGain:            -12,
			AttackTime:         10,
			ReleaseTime:        100,
			NoiseGateThreshold: -50,
			HoldTime:           50,
		},
		EchoCancellation: EchoCancellationConfig{
			Enabled:             true,
			FilterLength:        200,
			AdaptationRate:      0.5,
			NonlinearProcessing: 0.3,
			DoubleTalkThreshold: 0.5,
			ComfortNoiseLevel:   -60,
			ResidualSuppression: 0.5,
		},
		MultiChannel: MultiChannelConfig{
			Enabled:                   false,
			Configuration:             "stereo",
			ChannelCount:              2,
			EnableTelephonySeparation: true,
			EnableStereoWidening:      false,
			StereoWidth:               1.0,
			EnableMixing:              false,
			MaxDesync:                 50 * time.Millisecond,
			ResyncInterval:            1 * time.Second,
			BufferSize:                4096,
			BufferLatency:             20 * time.Millisecond,
			CallerChannelID:           0,
			CalleeChannelID:           1,
		},
		Fingerprinting: FingerprintingConfig{
			Enabled:               true,
			WindowSize:            2048,
			HopSize:               512,
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
			BlockDuplicates:       false,
			AlertOnDuplicates:     true,
			TagDuplicates:         true,
		},
		Compression: CompressionConfig{
			Enabled:    false,
			Threshold:  -20,
			Ratio:      4,
			Knee:       2,
			AttackTime: 5,
			ReleaseTime: 50,
			MakeupGain: 0,
		},
		Equalizer: EqualizerConfig{
			Enabled:    true,
			PreAmp:     0,
			UsePreset:  true,
			PresetName: "voice_clarity",
			Bands: []EqualizerBandConfig{
				{Frequency: 100, Gain: -2.0, Q: 0.7},  // Reduce low rumble
				{Frequency: 300, Gain: 1.0, Q: 0.7},   // Boost warmth
				{Frequency: 1000, Gain: 0.5, Q: 0.7},  // Slight presence boost
				{Frequency: 3000, Gain: 1.5, Q: 0.7},  // Clarity boost
				{Frequency: 8000, Gain: -1.0, Q: 0.7}, // Reduce harshness
			},
		},
		DeEsser: DeEsserConfig{
			Enabled:      true,
			FrequencyMin: 4000,
			FrequencyMax: 10000,
			Threshold:    -30,
			Reduction:    0.5,
		},
	}
}

// GetEqualizerPreset returns a preset equalizer configuration
func GetEqualizerPreset(name string) []EqualizerBandConfig {
	presets := map[string][]EqualizerBandConfig{
		"voice_clarity": {
			{Frequency: 100, Gain: -2.0, Q: 0.7},
			{Frequency: 300, Gain: 1.0, Q: 0.7},
			{Frequency: 1000, Gain: 0.5, Q: 0.7},
			{Frequency: 3000, Gain: 1.5, Q: 0.7},
			{Frequency: 8000, Gain: -1.0, Q: 0.7},
		},
		"telephone": {
			{Frequency: 80, Gain: -6.0, Q: 0.7},
			{Frequency: 200, Gain: 0.0, Q: 0.7},
			{Frequency: 1000, Gain: 1.0, Q: 0.7},
			{Frequency: 3000, Gain: 2.0, Q: 0.7},
			{Frequency: 8000, Gain: -3.0, Q: 0.7},
		},
		"conference": {
			{Frequency: 100, Gain: -3.0, Q: 0.7},
			{Frequency: 250, Gain: -1.0, Q: 0.7},
			{Frequency: 1000, Gain: 0.0, Q: 0.7},
			{Frequency: 2500, Gain: 1.0, Q: 0.7},
			{Frequency: 6000, Gain: 0.5, Q: 0.7},
		},
		"podcast": {
			{Frequency: 80, Gain: -1.0, Q: 0.7},
			{Frequency: 200, Gain: 2.0, Q: 0.7},
			{Frequency: 800, Gain: 0.5, Q: 0.7},
			{Frequency: 2500, Gain: 1.5, Q: 0.7},
			{Frequency: 10000, Gain: 0.5, Q: 0.7},
		},
		"flat": {
			{Frequency: 100, Gain: 0.0, Q: 0.7},
			{Frequency: 300, Gain: 0.0, Q: 0.7},
			{Frequency: 1000, Gain: 0.0, Q: 0.7},
			{Frequency: 3000, Gain: 0.0, Q: 0.7},
			{Frequency: 10000, Gain: 0.0, Q: 0.7},
		},
	}

	if preset, exists := presets[name]; exists {
		return preset
	}
	return presets["flat"]
}