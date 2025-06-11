package realtime

import (
	"sync"
	"time"
)

// StreamingMetrics tracks real-time streaming transcription metrics
type StreamingMetrics struct {
	mutex sync.RWMutex
	
	// Session metrics
	SessionStartTime   time.Time `json:"session_start_time"`
	SessionDuration    int64     `json:"session_duration_ms"`
	IsActive          bool      `json:"is_active"`
	
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
	ProcessingTime       int64   `json:"processing_time_ms"`
	AverageLatency       int64   `json:"average_latency_ms"`
	MaxLatency           int64   `json:"max_latency_ms"`
	MinLatency           int64   `json:"min_latency_ms"`
	
	// Event metrics
	EventsSent           int64 `json:"events_sent"`
	EventsDropped        int64 `json:"events_dropped"`
	
	// Error metrics
	Errors               int64 `json:"errors"`
	ErrorRate            float64 `json:"error_rate"`
	
	// Feature metrics
	SpeakerChanges       int64 `json:"speaker_changes"`
	KeywordsDetected     int64 `json:"keywords_detected"`
	SentimentUpdates     int64 `json:"sentiment_updates"`
	
	// Resource metrics
	MemoryUsage          int64 `json:"memory_usage_bytes"`
	CPUUsage             float64 `json:"cpu_usage_percent"`
	
	// Quality metrics
	QualityScore         float64 `json:"quality_score"`
	
	// Reset tracking
	LastReset            time.Time `json:"last_reset"`
}

// NewStreamingMetrics creates a new streaming metrics instance
func NewStreamingMetrics() *StreamingMetrics {
	return &StreamingMetrics{
		SessionStartTime: time.Now(),
		MinLatency:       999999, // Initialize to high value
		LastReset:        time.Now(),
	}
}

// StartSession starts tracking a new session
func (sm *StreamingMetrics) StartSession() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.SessionStartTime = time.Now()
	sm.IsActive = true
}

// EndSession ends the current session
func (sm *StreamingMetrics) EndSession() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	if sm.IsActive {
		sm.SessionDuration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
		sm.IsActive = false
	}
}

// IncrementAudioFrames increments audio frame counter
func (sm *StreamingMetrics) IncrementAudioFrames() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.AudioFramesProcessed++
}

// AddAudioBytes adds processed audio bytes
func (sm *StreamingMetrics) AddAudioBytes(bytes int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.AudioBytesProcessed += bytes
}

// IncrementAudioDropped increments dropped audio counter
func (sm *StreamingMetrics) IncrementAudioDropped() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.AudioDropped++
}

// IncrementTranscripts increments transcript counter
func (sm *StreamingMetrics) IncrementTranscripts() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.TranscriptsGenerated++
}

// IncrementPartialTranscripts increments partial transcript counter
func (sm *StreamingMetrics) IncrementPartialTranscripts() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.PartialTranscripts++
}

// IncrementFinalTranscripts increments final transcript counter
func (sm *StreamingMetrics) IncrementFinalTranscripts() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.FinalTranscripts++
}

// UpdateConfidence updates average confidence score
func (sm *StreamingMetrics) UpdateConfidence(confidence float64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	if sm.TranscriptsGenerated > 0 {
		sm.AverageConfidence = (sm.AverageConfidence*float64(sm.TranscriptsGenerated-1) + confidence) / float64(sm.TranscriptsGenerated)
	} else {
		sm.AverageConfidence = confidence
	}
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

// IncrementEventsSent increments events sent counter
func (sm *StreamingMetrics) IncrementEventsSent() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.EventsSent++
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

// IncrementSpeakerChanges increments speaker change counter
func (sm *StreamingMetrics) IncrementSpeakerChanges() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.SpeakerChanges++
}

// IncrementKeywordsDetected increments keyword detection counter
func (sm *StreamingMetrics) IncrementKeywordsDetected() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.KeywordsDetected++
}

// IncrementSentimentUpdates increments sentiment update counter
func (sm *StreamingMetrics) IncrementSentimentUpdates() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.SentimentUpdates++
}

// UpdateMemoryUsage updates memory usage metric
func (sm *StreamingMetrics) UpdateMemoryUsage(bytes int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.MemoryUsage = bytes
}

// UpdateCPUUsage updates CPU usage metric
func (sm *StreamingMetrics) UpdateCPUUsage(percent float64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.CPUUsage = percent
}

// UpdateQualityScore updates overall quality score
func (sm *StreamingMetrics) UpdateQualityScore(score float64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.QualityScore = score
}

// GetStats returns a copy of current metrics
func (sm *StreamingMetrics) GetStats() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	// Update session duration if active
	sessionDuration := sm.SessionDuration
	if sm.IsActive {
		sessionDuration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
	}
	
	return map[string]interface{}{
		"session_start_time":       sm.SessionStartTime,
		"session_duration_ms":      sessionDuration,
		"session_duration_s":       float64(sessionDuration) / 1000,
		"is_active":                sm.IsActive,
		"audio_frames_processed":   sm.AudioFramesProcessed,
		"audio_bytes_processed":    sm.AudioBytesProcessed,
		"audio_dropped":            sm.AudioDropped,
		"transcripts_generated":    sm.TranscriptsGenerated,
		"partial_transcripts":      sm.PartialTranscripts,
		"final_transcripts":        sm.FinalTranscripts,
		"average_confidence":       sm.AverageConfidence,
		"processing_time_ms":       sm.ProcessingTime,
		"processing_time_s":        float64(sm.ProcessingTime) / 1000,
		"average_latency_ms":       sm.AverageLatency,
		"max_latency_ms":           sm.MaxLatency,
		"min_latency_ms":           sm.MinLatency,
		"events_sent":              sm.EventsSent,
		"events_dropped":           sm.EventsDropped,
		"errors":                   sm.Errors,
		"error_rate_percent":       sm.ErrorRate,
		"speaker_changes":          sm.SpeakerChanges,
		"keywords_detected":        sm.KeywordsDetected,
		"sentiment_updates":        sm.SentimentUpdates,
		"memory_usage_bytes":       sm.MemoryUsage,
		"memory_usage_mb":          float64(sm.MemoryUsage) / 1024 / 1024,
		"cpu_usage_percent":        sm.CPUUsage,
		"quality_score":            sm.QualityScore,
		"last_reset":               sm.LastReset,
		
		// Calculated metrics
		"audio_processing_rate":    sm.calculateAudioProcessingRate(),
		"transcript_rate":          sm.calculateTranscriptRate(),
		"average_processing_time":  sm.calculateAverageProcessingTime(),
		"throughput_fps":           sm.calculateThroughput(),
	}
}

// calculateAudioProcessingRate calculates audio processing rate (frames per second)
func (sm *StreamingMetrics) calculateAudioProcessingRate() float64 {
	if sm.SessionDuration == 0 && !sm.IsActive {
		return 0
	}
	
	duration := sm.SessionDuration
	if sm.IsActive {
		duration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
	}
	
	if duration > 0 {
		return float64(sm.AudioFramesProcessed) / (float64(duration) / 1000)
	}
	return 0
}

// calculateTranscriptRate calculates transcript generation rate (transcripts per second)
func (sm *StreamingMetrics) calculateTranscriptRate() float64 {
	if sm.SessionDuration == 0 && !sm.IsActive {
		return 0
	}
	
	duration := sm.SessionDuration
	if sm.IsActive {
		duration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
	}
	
	if duration > 0 {
		return float64(sm.TranscriptsGenerated) / (float64(duration) / 1000)
	}
	return 0
}

// calculateAverageProcessingTime calculates average processing time per transcript
func (sm *StreamingMetrics) calculateAverageProcessingTime() float64 {
	if sm.TranscriptsGenerated > 0 {
		return float64(sm.ProcessingTime) / float64(sm.TranscriptsGenerated)
	}
	return 0
}

// calculateThroughput calculates overall throughput (operations per second)
func (sm *StreamingMetrics) calculateThroughput() float64 {
	if sm.SessionDuration == 0 && !sm.IsActive {
		return 0
	}
	
	duration := sm.SessionDuration
	if sm.IsActive {
		duration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
	}
	
	totalOperations := sm.AudioFramesProcessed + sm.TranscriptsGenerated + sm.EventsSent
	
	if duration > 0 {
		return float64(totalOperations) / (float64(duration) / 1000)
	}
	return 0
}

// Reset resets all metrics to zero
func (sm *StreamingMetrics) Reset() {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.SessionStartTime = time.Now()
	sm.SessionDuration = 0
	sm.IsActive = false
	sm.AudioFramesProcessed = 0
	sm.AudioBytesProcessed = 0
	sm.AudioDropped = 0
	sm.TranscriptsGenerated = 0
	sm.PartialTranscripts = 0
	sm.FinalTranscripts = 0
	sm.AverageConfidence = 0
	sm.ProcessingTime = 0
	sm.AverageLatency = 0
	sm.MaxLatency = 0
	sm.MinLatency = 999999
	sm.EventsSent = 0
	sm.EventsDropped = 0
	sm.Errors = 0
	sm.ErrorRate = 0
	sm.SpeakerChanges = 0
	sm.KeywordsDetected = 0
	sm.SentimentUpdates = 0
	sm.MemoryUsage = 0
	sm.CPUUsage = 0
	sm.QualityScore = 0
	sm.LastReset = time.Now()
}

// GetSummary returns a summary of key metrics
func (sm *StreamingMetrics) GetSummary() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	sessionDuration := sm.SessionDuration
	if sm.IsActive {
		sessionDuration = time.Since(sm.SessionStartTime).Nanoseconds() / 1e6
	}
	
	return map[string]interface{}{
		"is_active":             sm.IsActive,
		"session_duration_s":    float64(sessionDuration) / 1000,
		"transcripts_generated": sm.TranscriptsGenerated,
		"average_confidence":    sm.AverageConfidence,
		"average_latency_ms":    sm.AverageLatency,
		"error_rate_percent":    sm.ErrorRate,
		"speaker_changes":       sm.SpeakerChanges,
		"keywords_detected":     sm.KeywordsDetected,
		"quality_score":         sm.QualityScore,
		"throughput_fps":        sm.calculateThroughput(),
	}
}

// GetHealthStatus returns health status based on metrics
func (sm *StreamingMetrics) GetHealthStatus() map[string]interface{} {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	// Determine health status based on error rate and latency
	status := "healthy"
	if sm.ErrorRate > 10 { // >10% error rate
		status = "unhealthy"
	} else if sm.ErrorRate > 5 || sm.AverageLatency > 5000 { // >5% error rate or >5s latency
		status = "degraded"
	}
	
	return map[string]interface{}{
		"status":             status,
		"error_rate":         sm.ErrorRate,
		"average_latency_ms": sm.AverageLatency,
		"memory_usage_mb":    float64(sm.MemoryUsage) / 1024 / 1024,
		"cpu_usage_percent":  sm.CPUUsage,
		"last_check":         time.Now(),
	}
}