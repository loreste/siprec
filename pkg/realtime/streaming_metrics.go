package realtime

import (
	"sync"
	"time"
)

// StreamingMetrics tracks real-time streaming transcription metrics
type StreamingMetrics struct {
	mutex sync.RWMutex

	// Session metrics
	SessionStartTime time.Time `json:"session_start_time"`
	SessionDuration  int64     `json:"session_duration_ms"`
	IsActive         bool      `json:"is_active"`

	// Audio processing metrics
	AudioFramesProcessed int64 `json:"audio_frames_processed"`
	AudioBytesProcessed  int64 `json:"audio_bytes_processed"`
	AudioDropped         int64 `json:"audio_dropped"`

	// Transcription metrics
	TranscriptsGenerated int64   `json:"transcripts_generated"`
	PartialTranscripts   int64   `json:"partial_transcripts"`
	FinalTranscripts     int64   `json:"final_transcripts"`
	AverageConfidence    float64 `json:"average_confidence"`

	// Performance metrics
	ProcessingTime int64 `json:"processing_time_ms"`
	AverageLatency int64 `json:"average_latency_ms"`
	MaxLatency     int64 `json:"max_latency_ms"`
	MinLatency     int64 `json:"min_latency_ms"`

	// Event metrics
	EventsSent    int64 `json:"events_sent"`
	EventsDropped int64 `json:"events_dropped"`

	// Error metrics
	Errors    int64   `json:"errors"`
	ErrorRate float64 `json:"error_rate"`

	// Feature metrics
	SpeakerChanges   int64 `json:"speaker_changes"`
	KeywordsDetected int64 `json:"keywords_detected"`
	SentimentUpdates int64 `json:"sentiment_updates"`

	// Resource metrics
	MemoryUsage int64   `json:"memory_usage_bytes"`
	CPUUsage    float64 `json:"cpu_usage_percent"`

	// Quality metrics
	QualityScore float64 `json:"quality_score"`

	// Reset tracking
	LastReset time.Time `json:"last_reset"`
}

// NewStreamingMetrics creates a new streaming metrics instance
func NewStreamingMetrics() *StreamingMetrics {
	return &StreamingMetrics{
		SessionStartTime: time.Now(),
		MinLatency:       999999, // Initialize to high value
		LastReset:        time.Now(),
	}
}

// IncrementAudioFrames increments audio frame counter
func (sm *StreamingMetrics) IncrementAudioFrames() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.AudioFramesProcessed++
}

// IncrementTranscripts increments transcript counter
func (sm *StreamingMetrics) IncrementTranscripts() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.TranscriptsGenerated++
}

// AddProcessingTime adds processing time
func (sm *StreamingMetrics) AddProcessingTime(duration time.Duration) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	ms := duration.Nanoseconds() / 1e6
	sm.ProcessingTime += ms

	// Update latency metrics
	sm.updateLatency(ms)
}

// updateLatency updates latency statistics
func (sm *StreamingMetrics) updateLatency(latencyMs int64) {
	// Update average latency
	if sm.TranscriptsGenerated > 0 {
		sm.AverageLatency = (sm.AverageLatency*(sm.TranscriptsGenerated-1) + latencyMs) / sm.TranscriptsGenerated
	} else {
		sm.AverageLatency = latencyMs
	}

	// Update max latency
	if latencyMs > sm.MaxLatency {
		sm.MaxLatency = latencyMs
	}

	// Update min latency
	if latencyMs < sm.MinLatency {
		sm.MinLatency = latencyMs
	}
}

// IncrementDroppedEvents increments dropped events counter
func (sm *StreamingMetrics) IncrementDroppedEvents() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.EventsDropped++
}

// IncrementErrors increments error counter
func (sm *StreamingMetrics) IncrementErrors() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	sm.Errors++

	// Update error rate
	totalOperations := sm.TranscriptsGenerated + sm.Errors
	if totalOperations > 0 {
		sm.ErrorRate = float64(sm.Errors) / float64(totalOperations) * 100
	}
}
