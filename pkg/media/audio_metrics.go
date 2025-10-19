package media

import "time"

// AudioMetrics describes quality measurements for an RTP stream.
type AudioMetrics struct {
	MOS         float64        `json:"mos"`
	VoiceRatio  float64        `json:"voice_ratio"`
	NoiseFloor  float64        `json:"noise_floor"`
	PacketLoss  float64        `json:"packet_loss"`
	JitterMs    float64        `json:"jitter_ms"`
	Timestamp   time.Time      `json:"timestamp"`
	Details     map[string]any `json:"details,omitempty"`
	WindowStart time.Time      `json:"window_start"`
	WindowEnd   time.Time      `json:"window_end"`
}

// AcousticEvent represents a detected acoustic phenomenon (silence, hold music, DTMF, etc.).
type AcousticEvent struct {
	Type       string                 `json:"type"`
	Confidence float64                `json:"confidence"`
	Timestamp  time.Time              `json:"timestamp"`
	Details    map[string]interface{} `json:"details,omitempty"`
}

// AudioMetricsListener receives audio quality updates and acoustic events.
type AudioMetricsListener interface {
	OnAudioMetrics(callUUID string, metrics AudioMetrics)
	OnAcousticEvent(callUUID string, event AcousticEvent)
}
