package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// StreamingTranscriptionService enhances real-time streaming capabilities
type StreamingTranscriptionService struct {
	logger               *logrus.Logger
	transcriptionService *TranscriptionService
	buffers              map[string]*StreamBuffer
	bufferMutex          sync.RWMutex
	config               StreamingConfig
	metrics              *StreamingMetrics
}

// StreamingConfig holds configuration for streaming enhancements
type StreamingConfig struct {
	BufferSize                int           // Maximum number of transcriptions to buffer per call
	InterimResultTimeout      time.Duration // Timeout for interim results before considering them final
	ChunkProcessingDelay      time.Duration // Delay between processing audio chunks
	EnableSmoothing           bool          // Enable text smoothing for interim results
	EnableConfidenceFiltering bool          // Filter low-confidence results
	MinConfidenceThreshold    float64       // Minimum confidence threshold (0.0-1.0)
	EnableDuplicateDetection  bool          // Detect and filter duplicate transcriptions
	SimilarityThreshold       float64       // Similarity threshold for duplicate detection
	MaxRetries                int           // Maximum retries for failed streaming operations
	RetryDelay                time.Duration // Delay between retries
}

// StreamBuffer holds buffered transcriptions for a call
type StreamBuffer struct {
	CallUUID       string
	InterimResults []InterimResult
	FinalResults   []FinalResult
	LastActivity   time.Time
	Mutex          sync.RWMutex
	TotalDuration  time.Duration
	WordCount      int
	ProviderStats  map[string]*ProviderStatistics
}

// InterimResult represents an interim transcription result
type InterimResult struct {
	Text        string                 `json:"text"`
	Confidence  float64                `json:"confidence"`
	Timestamp   time.Time              `json:"timestamp"`
	Provider    string                 `json:"provider"`
	Metadata    map[string]interface{} `json:"metadata"`
	Sequence    int                    `json:"sequence"`
	ProcessedAt time.Time              `json:"processed_at"`
}

// FinalResult represents a final transcription result
type FinalResult struct {
	Text       string                 `json:"text"`
	Confidence float64                `json:"confidence"`
	StartTime  time.Time              `json:"start_time"`
	EndTime    time.Time              `json:"end_time"`
	Provider   string                 `json:"provider"`
	Metadata   map[string]interface{} `json:"metadata"`
	WordTiming []WordTiming           `json:"word_timing,omitempty"`
	SpeakerID  string                 `json:"speaker_id,omitempty"`
}

// WordTiming represents word-level timing information
type WordTiming struct {
	Word       string    `json:"word"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Confidence float64   `json:"confidence"`
}

// ProviderStatistics tracks statistics for each provider
type ProviderStatistics struct {
	TotalResults      int           `json:"total_results"`
	InterimResults    int           `json:"interim_results"`
	FinalResults      int           `json:"final_results"`
	AverageConfidence float64       `json:"average_confidence"`
	TotalLatency      time.Duration `json:"total_latency"`
	AverageLatency    time.Duration `json:"average_latency"`
	ErrorCount        int           `json:"error_count"`
	LastActivity      time.Time     `json:"last_activity"`
}

// StreamingMetrics tracks overall streaming metrics
type StreamingMetrics struct {
	TotalCalls          int64         `json:"total_calls"`
	ActiveCalls         int64         `json:"active_calls"`
	TotalTranscriptions int64         `json:"total_transcriptions"`
	InterimCount        int64         `json:"interim_count"`
	FinalCount          int64         `json:"final_count"`
	AverageLatency      time.Duration `json:"average_latency"`
	ErrorRate           float64       `json:"error_rate"`
	ThroughputPerSecond float64       `json:"throughput_per_second"`
	mutex               sync.RWMutex
}

// DefaultStreamingConfig returns default configuration for streaming enhancements
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		BufferSize:                1000,
		InterimResultTimeout:      2 * time.Second,
		ChunkProcessingDelay:      50 * time.Millisecond,
		EnableSmoothing:           true,
		EnableConfidenceFiltering: true,
		MinConfidenceThreshold:    0.3,
		EnableDuplicateDetection:  true,
		SimilarityThreshold:       0.85,
		MaxRetries:                3,
		RetryDelay:                100 * time.Millisecond,
	}
}

// NewStreamingTranscriptionService creates a new streaming transcription service
func NewStreamingTranscriptionService(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *StreamingConfig) *StreamingTranscriptionService {
	if cfg == nil {
		defaultCfg := DefaultStreamingConfig()
		cfg = &defaultCfg
	}

	return &StreamingTranscriptionService{
		logger:               logger,
		transcriptionService: transcriptionSvc,
		buffers:              make(map[string]*StreamBuffer),
		config:               *cfg,
		metrics:              &StreamingMetrics{},
	}
}

// Start initializes the streaming service
func (s *StreamingTranscriptionService) Start(ctx context.Context) {
	s.logger.Info("Starting enhanced streaming transcription service")

	// Start cleanup routine
	go s.runCleanupRoutine(ctx)

	// Start metrics collection
	go s.runMetricsCollection(ctx)

	// Register as transcription listener
	if s.transcriptionService != nil {
		s.transcriptionService.AddListener(s)
	}
}

// OnTranscription implements TranscriptionListener interface with enhanced processing
func (s *StreamingTranscriptionService) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Update metrics
	s.updateMetrics(isFinal)

	// Get or create buffer for this call
	buffer := s.getOrCreateBuffer(callUUID)

	// Process the transcription with enhancements
	s.processTranscription(buffer, transcription, isFinal, metadata)
}

// processTranscription processes a transcription with streaming enhancements
func (s *StreamingTranscriptionService) processTranscription(buffer *StreamBuffer, text string, isFinal bool, metadata map[string]interface{}) {
	buffer.Mutex.Lock()
	defer buffer.Mutex.Unlock()

	buffer.LastActivity = time.Now()

	// Extract provider information
	provider := "unknown"
	confidence := 0.0
	if metadata != nil {
		if p, ok := metadata["provider"].(string); ok {
			provider = p
		}
		if c, ok := metadata["confidence"].(float64); ok {
			confidence = c
		}
	}

	// Apply confidence filtering
	if s.config.EnableConfidenceFiltering && confidence < s.config.MinConfidenceThreshold {
		s.logger.WithFields(logrus.Fields{
			"call_uuid":  buffer.CallUUID,
			"confidence": confidence,
			"threshold":  s.config.MinConfidenceThreshold,
		}).Debug("Filtered low-confidence transcription")
		return
	}

	// Apply duplicate detection
	if s.config.EnableDuplicateDetection && s.isDuplicate(buffer, text, isFinal) {
		s.logger.WithFields(logrus.Fields{
			"call_uuid": buffer.CallUUID,
			"text":      text,
		}).Debug("Filtered duplicate transcription")
		return
	}

	// Update provider statistics
	s.updateProviderStats(buffer, provider, confidence, isFinal)

	if isFinal {
		s.processFinalResult(buffer, text, confidence, provider, metadata)
	} else {
		s.processInterimResult(buffer, text, confidence, provider, metadata)
	}

	// Apply text smoothing if enabled
	if s.config.EnableSmoothing && !isFinal {
		text = s.applyTextSmoothing(buffer, text)
	}

	// Forward to original transcription service listeners
	if s.transcriptionService != nil {
		enhancedMetadata := s.enhanceMetadata(metadata, buffer, isFinal)
		s.transcriptionService.PublishTranscription(buffer.CallUUID, text, isFinal, enhancedMetadata)
	}
}

// processFinalResult handles final transcription results
func (s *StreamingTranscriptionService) processFinalResult(buffer *StreamBuffer, text string, confidence float64, provider string, metadata map[string]interface{}) {
	result := FinalResult{
		Text:       text,
		Confidence: confidence,
		StartTime:  time.Now(),
		EndTime:    time.Now(),
		Provider:   provider,
		Metadata:   metadata,
	}

	// Extract timing information if available
	if metadata != nil {
		if startTime, ok := metadata["start_time"].(float64); ok {
			result.StartTime = time.Unix(0, int64(startTime*1e6))
		}
		if endTime, ok := metadata["end_time"].(float64); ok {
			result.EndTime = time.Unix(0, int64(endTime*1e6))
		}
		if speakerID, ok := metadata["speaker_id"].(string); ok {
			result.SpeakerID = speakerID
		}

		// Extract word timing if available
		if words, ok := metadata["words"].([]interface{}); ok {
			result.WordTiming = s.extractWordTiming(words)
		}
	}

	// Add to buffer
	buffer.FinalResults = append(buffer.FinalResults, result)
	buffer.WordCount += len(result.Text)

	// Maintain buffer size
	if len(buffer.FinalResults) > s.config.BufferSize {
		buffer.FinalResults = buffer.FinalResults[1:]
	}

	// Clear interim results that are now superseded
	buffer.InterimResults = buffer.InterimResults[:0]

	s.logger.WithFields(logrus.Fields{
		"call_uuid":  buffer.CallUUID,
		"provider":   provider,
		"confidence": confidence,
		"text_len":   len(text),
	}).Debug("Processed final transcription result")
}

// processInterimResult handles interim transcription results
func (s *StreamingTranscriptionService) processInterimResult(buffer *StreamBuffer, text string, confidence float64, provider string, metadata map[string]interface{}) {
	result := InterimResult{
		Text:        text,
		Confidence:  confidence,
		Timestamp:   time.Now(),
		Provider:    provider,
		Metadata:    metadata,
		Sequence:    len(buffer.InterimResults),
		ProcessedAt: time.Now(),
	}

	// Add to buffer
	buffer.InterimResults = append(buffer.InterimResults, result)

	// Maintain buffer size
	if len(buffer.InterimResults) > s.config.BufferSize/2 {
		buffer.InterimResults = buffer.InterimResults[1:]
	}

	s.logger.WithFields(logrus.Fields{
		"call_uuid":  buffer.CallUUID,
		"provider":   provider,
		"confidence": confidence,
		"sequence":   result.Sequence,
	}).Debug("Processed interim transcription result")
}

// isDuplicate checks if a transcription is a duplicate
func (s *StreamingTranscriptionService) isDuplicate(buffer *StreamBuffer, text string, isFinal bool) bool {
	if !s.config.EnableDuplicateDetection {
		return false
	}

	if isFinal {
		// Check against recent final results
		for i := len(buffer.FinalResults) - 1; i >= 0 && i >= len(buffer.FinalResults)-5; i-- {
			if s.calculateSimilarity(text, buffer.FinalResults[i].Text) > s.config.SimilarityThreshold {
				return true
			}
		}
	} else {
		// Check against recent interim results
		for i := len(buffer.InterimResults) - 1; i >= 0 && i >= len(buffer.InterimResults)-3; i-- {
			if s.calculateSimilarity(text, buffer.InterimResults[i].Text) > s.config.SimilarityThreshold {
				return true
			}
		}
	}

	return false
}

// calculateSimilarity calculates text similarity using a simple algorithm
func (s *StreamingTranscriptionService) calculateSimilarity(text1, text2 string) float64 {
	if text1 == text2 {
		return 1.0
	}
	if len(text1) == 0 || len(text2) == 0 {
		return 0.0
	}

	// Simple Levenshtein-based similarity
	// This could be enhanced with more sophisticated algorithms
	shorter, longer := text1, text2
	if len(shorter) > len(longer) {
		shorter, longer = longer, shorter
	}

	if len(longer) == 0 {
		return 1.0
	}

	// Simple character-level comparison
	matches := 0
	for i, char := range shorter {
		if i < len(longer) && byte(char) == longer[i] {
			matches++
		}
	}

	return float64(matches) / float64(len(longer))
}

// applyTextSmoothing applies smoothing to interim transcription text
func (s *StreamingTranscriptionService) applyTextSmoothing(buffer *StreamBuffer, text string) string {
	if !s.config.EnableSmoothing || len(buffer.InterimResults) < 2 {
		return text
	}

	// Simple smoothing: if current text is very similar to previous interim result,
	// and confidence is lower, keep the previous text
	prev := buffer.InterimResults[len(buffer.InterimResults)-2]
	if s.calculateSimilarity(text, prev.Text) > 0.9 {
		// Keep the text with higher confidence
		if len(buffer.InterimResults) > 0 {
			current := buffer.InterimResults[len(buffer.InterimResults)-1]
			if prev.Confidence > current.Confidence {
				return prev.Text
			}
		}
	}

	return text
}

// extractWordTiming extracts word timing information from metadata
func (s *StreamingTranscriptionService) extractWordTiming(words []interface{}) []WordTiming {
	var timing []WordTiming

	for _, wordData := range words {
		if wordMap, ok := wordData.(map[string]interface{}); ok {
			wordTiming := WordTiming{}

			if word, ok := wordMap["word"].(string); ok {
				wordTiming.Word = word
			}
			if startTime, ok := wordMap["start_time"].(float64); ok {
				wordTiming.StartTime = time.Unix(0, int64(startTime*1e6))
			}
			if endTime, ok := wordMap["end_time"].(float64); ok {
				wordTiming.EndTime = time.Unix(0, int64(endTime*1e6))
			}
			if confidence, ok := wordMap["confidence"].(float64); ok {
				wordTiming.Confidence = confidence
			}

			timing = append(timing, wordTiming)
		}
	}

	return timing
}

// enhanceMetadata adds streaming-specific metadata
func (s *StreamingTranscriptionService) enhanceMetadata(original map[string]interface{}, buffer *StreamBuffer, isFinal bool) map[string]interface{} {
	enhanced := make(map[string]interface{})

	// Copy original metadata
	if original != nil {
		for k, v := range original {
			enhanced[k] = v
		}
	}

	// Add streaming enhancements
	enhanced["streaming_enhanced"] = true
	enhanced["buffer_stats"] = map[string]interface{}{
		"interim_count": len(buffer.InterimResults),
		"final_count":   len(buffer.FinalResults),
		"word_count":    buffer.WordCount,
		"last_activity": buffer.LastActivity,
	}

	// Add provider statistics
	if len(buffer.ProviderStats) > 0 {
		enhanced["provider_stats"] = buffer.ProviderStats
	}

	return enhanced
}

// updateProviderStats updates statistics for a provider
func (s *StreamingTranscriptionService) updateProviderStats(buffer *StreamBuffer, provider string, confidence float64, isFinal bool) {
	if buffer.ProviderStats == nil {
		buffer.ProviderStats = make(map[string]*ProviderStatistics)
	}

	if _, exists := buffer.ProviderStats[provider]; !exists {
		buffer.ProviderStats[provider] = &ProviderStatistics{}
	}

	stats := buffer.ProviderStats[provider]
	stats.TotalResults++
	stats.LastActivity = time.Now()

	if isFinal {
		stats.FinalResults++
	} else {
		stats.InterimResults++
	}

	// Update average confidence
	if stats.TotalResults == 1 {
		stats.AverageConfidence = confidence
	} else {
		stats.AverageConfidence = (stats.AverageConfidence*float64(stats.TotalResults-1) + confidence) / float64(stats.TotalResults)
	}
}

// getOrCreateBuffer gets or creates a stream buffer for a call
func (s *StreamingTranscriptionService) getOrCreateBuffer(callUUID string) *StreamBuffer {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	if buffer, exists := s.buffers[callUUID]; exists {
		return buffer
	}

	buffer := &StreamBuffer{
		CallUUID:       callUUID,
		InterimResults: make([]InterimResult, 0),
		FinalResults:   make([]FinalResult, 0),
		LastActivity:   time.Now(),
		ProviderStats:  make(map[string]*ProviderStatistics),
	}

	s.buffers[callUUID] = buffer
	s.metrics.mutex.Lock()
	s.metrics.ActiveCalls++
	s.metrics.TotalCalls++
	s.metrics.mutex.Unlock()

	s.logger.WithField("call_uuid", callUUID).Info("Created new stream buffer")
	return buffer
}

// updateMetrics updates streaming metrics
func (s *StreamingTranscriptionService) updateMetrics(isFinal bool) {
	s.metrics.mutex.Lock()
	defer s.metrics.mutex.Unlock()

	s.metrics.TotalTranscriptions++
	if isFinal {
		s.metrics.FinalCount++
	} else {
		s.metrics.InterimCount++
	}
}

// runCleanupRoutine periodically cleans up old buffers
func (s *StreamingTranscriptionService) runCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOldBuffers()
		}
	}
}

// cleanupOldBuffers removes buffers that haven't been active for a while
func (s *StreamingTranscriptionService) cleanupOldBuffers() {
	cutoff := time.Now().Add(-30 * time.Minute)

	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	var removed []string
	for callUUID, buffer := range s.buffers {
		buffer.Mutex.RLock()
		if buffer.LastActivity.Before(cutoff) {
			removed = append(removed, callUUID)
		}
		buffer.Mutex.RUnlock()
	}

	for _, callUUID := range removed {
		delete(s.buffers, callUUID)
		s.metrics.mutex.Lock()
		s.metrics.ActiveCalls--
		s.metrics.mutex.Unlock()
	}

	if len(removed) > 0 {
		s.logger.WithField("cleaned_count", len(removed)).Info("Cleaned up old stream buffers")
	}
}

// runMetricsCollection collects and logs metrics periodically
func (s *StreamingTranscriptionService) runMetricsCollection(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.logMetrics()
		}
	}
}

// logMetrics logs current streaming metrics
func (s *StreamingTranscriptionService) logMetrics() {
	s.metrics.mutex.RLock()
	defer s.metrics.mutex.RUnlock()

	s.logger.WithFields(logrus.Fields{
		"active_calls":         s.metrics.ActiveCalls,
		"total_calls":          s.metrics.TotalCalls,
		"total_transcriptions": s.metrics.TotalTranscriptions,
		"interim_count":        s.metrics.InterimCount,
		"final_count":          s.metrics.FinalCount,
	}).Info("Streaming transcription metrics")
}

// GetStreamBuffer returns the current stream buffer for a call (for monitoring/debugging)
func (s *StreamingTranscriptionService) GetStreamBuffer(callUUID string) (*StreamBuffer, bool) {
	s.bufferMutex.RLock()
	defer s.bufferMutex.RUnlock()

	buffer, exists := s.buffers[callUUID]
	return buffer, exists
}

// GetMetrics returns current streaming metrics
func (s *StreamingTranscriptionService) GetMetrics() StreamingMetrics {
	s.metrics.mutex.RLock()
	defer s.metrics.mutex.RUnlock()
	
	// Return a copy without the mutex to avoid copying the lock
	return StreamingMetrics{
		TotalCalls:          s.metrics.TotalCalls,
		ActiveCalls:         s.metrics.ActiveCalls,
		TotalTranscriptions: s.metrics.TotalTranscriptions,
		InterimCount:        s.metrics.InterimCount,
		FinalCount:          s.metrics.FinalCount,
		AverageLatency:      s.metrics.AverageLatency,
		ErrorRate:           s.metrics.ErrorRate,
		ThroughputPerSecond: s.metrics.ThroughputPerSecond,
	}
}

// ExportBuffer exports a stream buffer as JSON for analysis
func (s *StreamingTranscriptionService) ExportBuffer(callUUID string) ([]byte, error) {
	buffer, exists := s.GetStreamBuffer(callUUID)
	if !exists {
		return nil, fmt.Errorf("buffer not found for call UUID: %s", callUUID)
	}

	buffer.Mutex.RLock()
	defer buffer.Mutex.RUnlock()

	return json.MarshalIndent(buffer, "", "  ")
}

// StreamingWriter provides a streaming writer interface for audio data
type StreamingWriter struct {
	service    *StreamingTranscriptionService
	callUUID   string
	provider   Provider
	ctx        context.Context
	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	logger     *logrus.Entry
}

// NewStreamingWriter creates a new streaming writer for real-time audio processing
func (s *StreamingTranscriptionService) NewStreamingWriter(callUUID string, provider Provider) *StreamingWriter {
	pr, pw := io.Pipe()

	writer := &StreamingWriter{
		service:    s,
		callUUID:   callUUID,
		provider:   provider,
		ctx:        context.Background(),
		pipeReader: pr,
		pipeWriter: pw,
		logger:     s.logger.WithField("call_uuid", callUUID),
	}

	// Start processing in background
	go writer.process()

	return writer
}

// Write implements io.Writer interface for streaming audio data
func (w *StreamingWriter) Write(data []byte) (int, error) {
	return w.pipeWriter.Write(data)
}

// Close closes the streaming writer
func (w *StreamingWriter) Close() error {
	return w.pipeWriter.Close()
}

// process handles the streaming transcription processing
func (w *StreamingWriter) process() {
	defer w.pipeReader.Close()

	// Process audio stream with the provider
	if err := w.provider.StreamToText(w.ctx, w.pipeReader, w.callUUID); err != nil {
		w.logger.WithError(err).Error("Streaming transcription failed")
	}
}
