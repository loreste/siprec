package realtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AMQPPublisher interface for publishing real-time transcription events
type AMQPPublisher interface {
	PublishTranscriptionEvent(event TranscriptionEvent) error
	IsStarted() bool
}

// TranscriptionEvent represents a real-time transcription event
type TranscriptionEvent struct {
	Type      EventType              `json:"type"`
	SessionID string                 `json:"session_id"`
	CallID    string                 `json:"call_id"`
	Timestamp time.Time              `json:"timestamp"`
	Data      TranscriptionEventData `json:"data"`
}

// EventType defines the type of transcription event
type EventType string

const (
	EventTypePartialTranscript EventType = "partial_transcript"
	EventTypeFinalTranscript   EventType = "final_transcript"
	EventTypeSpeakerChange     EventType = "speaker_change"
	EventTypeSentimentUpdate   EventType = "sentiment_update"
	EventTypeKeywordDetected   EventType = "keyword_detected"
	EventTypeError             EventType = "error"
	EventTypeSessionStart      EventType = "session_start"
	EventTypeSessionEnd        EventType = "session_end"
)

// TranscriptionEventData holds the event-specific data
type TranscriptionEventData struct {
	// Transcription data
	Text       string                 `json:"text,omitempty"`
	IsFinal    bool                   `json:"is_final,omitempty"`
	Confidence float64                `json:"confidence,omitempty"`
	StartTime  float64                `json:"start_time,omitempty"` // Seconds from start
	EndTime    float64                `json:"end_time,omitempty"`   // Seconds from start
	Language   string                 `json:"language,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`

	// Speaker information
	SpeakerID    string `json:"speaker_id,omitempty"`
	SpeakerLabel string `json:"speaker_label,omitempty"`
	SpeakerCount int    `json:"speaker_count,omitempty"`

	// Sentiment analysis
	Sentiment Sentiment `json:"sentiment,omitempty"`

	// Keyword detection
	Keywords []Keyword `json:"keywords,omitempty"`

	// Error information
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Sentiment represents sentiment analysis results
type Sentiment struct {
	Label        string  `json:"label"`                  // positive, negative, neutral
	Score        float64 `json:"score"`                  // confidence score 0-1
	Magnitude    float64 `json:"magnitude"`              // intensity 0-1
	Subjectivity float64 `json:"subjectivity,omitempty"` // 0=objective, 1=subjective
}

// Keyword represents a detected keyword
type Keyword struct {
	Text       string  `json:"text"`
	Category   string  `json:"category"` // compliance, security, etc.
	Confidence float64 `json:"confidence"`
	StartTime  float64 `json:"start_time"`
	EndTime    float64 `json:"end_time"`
	Severity   string  `json:"severity"` // low, medium, high, critical
}

// StreamingTranscriber handles real-time transcription with advanced features
type StreamingTranscriber struct {
	sessionID string
	callID    string
	logger    *logrus.Entry
	ctx       context.Context
	cancel    context.CancelFunc

	// Event handling
	eventChan      chan TranscriptionEvent
	subscribers    map[string]chan<- TranscriptionEvent
	subscribersMux sync.RWMutex

	// Audio processing
	audioBuffer *AudioBuffer
	sampleRate  int
	channels    int

	// Feature processors
	diarizer        *SpeakerDiarizer
	sentimentEngine *SentimentAnalyzer
	keywordDetector *KeywordDetector

	// AMQP publisher for real-time events
	amqpPublisher AMQPPublisher

	// Performance optimization
	batchProcessor  *BatchProcessor
	resourceMonitor *ResourceMonitor

	// Configuration
	config *StreamingConfig

	// State management
	isActive     bool
	activeMux    sync.RWMutex
	startTime    time.Time
	lastActivity time.Time

	// Metrics
	metrics *StreamingMetrics
}

// StreamingConfig holds configuration for real-time transcription
type StreamingConfig struct {
	// Audio settings
	SampleRate   int `json:"sample_rate" default:"16000"`
	Channels     int `json:"channels" default:"1"`
	BufferSizeMS int `json:"buffer_size_ms" default:"100"`

	// Transcription settings
	Language        string `json:"language" default:"en-US"`
	InterimResults  bool   `json:"interim_results" default:"true"`
	MaxAlternatives int    `json:"max_alternatives" default:"1"`
	ProfanityFilter bool   `json:"profanity_filter" default:"false"`

	// Feature settings
	EnableDiarization bool `json:"enable_diarization" default:"true"`
	EnableSentiment   bool `json:"enable_sentiment" default:"true"`
	EnableKeywords    bool `json:"enable_keywords" default:"true"`
	MaxSpeakers       int  `json:"max_speakers" default:"8"`

	// Performance settings
	BatchSize         int           `json:"batch_size" default:"10"`
	ProcessingTimeout time.Duration `json:"processing_timeout" default:"5s"`
	MaxBufferSize     int           `json:"max_buffer_size" default:"1048576"` // 1MB

	// Memory optimization
	GCInterval     time.Duration `json:"gc_interval" default:"30s"`
	MaxMemoryUsage int64         `json:"max_memory_usage" default:"134217728"` // 128MB
}

// DefaultStreamingConfig returns default configuration
func DefaultStreamingConfig() *StreamingConfig {
	return &StreamingConfig{
		SampleRate:        16000,
		Channels:          1,
		BufferSizeMS:      100,
		Language:          "en-US",
		InterimResults:    true,
		MaxAlternatives:   1,
		ProfanityFilter:   false,
		EnableDiarization: true,
		EnableSentiment:   true,
		EnableKeywords:    true,
		MaxSpeakers:       8,
		BatchSize:         10,
		ProcessingTimeout: 5 * time.Second,
		MaxBufferSize:     1024 * 1024, // 1MB
		GCInterval:        30 * time.Second,
		MaxMemoryUsage:    128 * 1024 * 1024, // 128MB
	}
}

// NewStreamingTranscriber creates a new streaming transcriber
func NewStreamingTranscriber(sessionID, callID string, config *StreamingConfig, logger *logrus.Logger) *StreamingTranscriber {
	return NewStreamingTranscriberWithAMQP(sessionID, callID, config, logger, nil)
}

// NewStreamingTranscriberWithAMQP creates a new streaming transcriber with AMQP publisher
func NewStreamingTranscriberWithAMQP(sessionID, callID string, config *StreamingConfig, logger *logrus.Logger, amqpPublisher AMQPPublisher) *StreamingTranscriber {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	transcriber := &StreamingTranscriber{
		sessionID:     sessionID,
		callID:        callID,
		logger:        logger.WithFields(logrus.Fields{"session_id": sessionID, "call_id": callID}),
		ctx:           ctx,
		cancel:        cancel,
		eventChan:     make(chan TranscriptionEvent, config.BatchSize*2),
		subscribers:   make(map[string]chan<- TranscriptionEvent),
		sampleRate:    config.SampleRate,
		channels:      config.Channels,
		config:        config,
		startTime:     time.Now(),
		lastActivity:  time.Now(),
		metrics:       NewStreamingMetrics(),
		amqpPublisher: amqpPublisher,
	}

	// Initialize components
	transcriber.audioBuffer = NewAudioBuffer(config.BufferSizeMS, config.SampleRate, config.Channels)

	if config.EnableDiarization {
		transcriber.diarizer = NewSpeakerDiarizer(config.MaxSpeakers, logger)
	}

	if config.EnableSentiment {
		transcriber.sentimentEngine = NewSentimentAnalyzer(logger)
	}

	if config.EnableKeywords {
		transcriber.keywordDetector = NewKeywordDetector(logger)
	}

	transcriber.batchProcessor = NewBatchProcessor(config.BatchSize, config.ProcessingTimeout, logger)
	transcriber.resourceMonitor = NewResourceMonitor(config.MaxMemoryUsage, logger)

	return transcriber
}

// Start begins the streaming transcription session
func (st *StreamingTranscriber) Start() error {
	st.activeMux.Lock()
	defer st.activeMux.Unlock()

	if st.isActive {
		return fmt.Errorf("transcription session already active")
	}

	st.isActive = true
	st.startTime = time.Now()
	st.lastActivity = time.Now()

	// Start processing goroutines
	go st.processEvents()
	go st.monitorResources()
	go st.performPeriodicCleanup()

	// Send session start event
	st.sendEvent(TranscriptionEvent{
		Type:      EventTypeSessionStart,
		SessionID: st.sessionID,
		CallID:    st.callID,
		Timestamp: time.Now(),
		Data: TranscriptionEventData{
			Text: "Transcription session started",
		},
	})

	st.logger.Info("Streaming transcription session started")
	return nil
}

// Stop ends the streaming transcription session
func (st *StreamingTranscriber) Stop() error {
	st.activeMux.Lock()
	defer st.activeMux.Unlock()

	if !st.isActive {
		return fmt.Errorf("transcription session not active")
	}

	// Send session end event
	st.sendEvent(TranscriptionEvent{
		Type:      EventTypeSessionEnd,
		SessionID: st.sessionID,
		CallID:    st.callID,
		Timestamp: time.Now(),
		Data: TranscriptionEventData{
			Text: "Transcription session ended",
		},
	})

	// Cancel context and cleanup
	st.cancel()
	st.isActive = false

	// Close all subscriber channels
	st.subscribersMux.Lock()
	for id, ch := range st.subscribers {
		close(ch)
		delete(st.subscribers, id)
	}
	st.subscribersMux.Unlock()

	// Close event channel
	close(st.eventChan)

	duration := time.Since(st.startTime)
	st.logger.WithField("duration", duration).Info("Streaming transcription session ended")

	return nil
}

// Subscribe adds a subscriber to receive transcription events
func (st *StreamingTranscriber) Subscribe(subscriberID string, bufferSize int) (<-chan TranscriptionEvent, error) {
	st.subscribersMux.Lock()
	defer st.subscribersMux.Unlock()

	if _, exists := st.subscribers[subscriberID]; exists {
		return nil, fmt.Errorf("subscriber %s already exists", subscriberID)
	}

	// Create buffered channel for subscriber
	eventChan := make(chan TranscriptionEvent, bufferSize)
	st.subscribers[subscriberID] = eventChan

	st.logger.WithField("subscriber_id", subscriberID).Debug("New subscriber added")
	return eventChan, nil
}

// Unsubscribe removes a subscriber
func (st *StreamingTranscriber) Unsubscribe(subscriberID string) error {
	st.subscribersMux.Lock()
	defer st.subscribersMux.Unlock()

	if ch, exists := st.subscribers[subscriberID]; exists {
		close(ch)
		delete(st.subscribers, subscriberID)
		st.logger.WithField("subscriber_id", subscriberID).Debug("Subscriber removed")
		return nil
	}

	return fmt.Errorf("subscriber %s not found", subscriberID)
}

// ProcessAudio processes incoming audio data for real-time transcription
func (st *StreamingTranscriber) ProcessAudio(audioData []byte) error {
	st.activeMux.RLock()
	if !st.isActive {
		st.activeMux.RUnlock()
		return fmt.Errorf("transcription session not active")
	}
	st.activeMux.RUnlock()

	st.lastActivity = time.Now()

	// Add audio to buffer
	if err := st.audioBuffer.Write(audioData); err != nil {
		st.metrics.IncrementErrors()
		return fmt.Errorf("failed to write audio data: %w", err)
	}

	// Check if we have enough data to process
	if st.audioBuffer.CanRead() {
		go st.processAudioBuffer()
	}

	st.metrics.IncrementAudioFrames()
	return nil
}

// processAudioBuffer processes buffered audio data
func (st *StreamingTranscriber) processAudioBuffer() {
	defer func() {
		if r := recover(); r != nil {
			st.logger.WithField("panic", r).Error("Panic in audio buffer processing")
			st.metrics.IncrementErrors()
		}
	}()

	// Read audio data from buffer
	audioData, err := st.audioBuffer.Read()
	if err != nil {
		st.logger.WithError(err).Error("Failed to read from audio buffer")
		st.metrics.IncrementErrors()
		return
	}

	// Process with transcription, diarization, sentiment, and keywords
	st.batchProcessor.Process(audioData, st.processAudioChunk)
}

// processAudioChunk processes a chunk of audio data
func (st *StreamingTranscriber) processAudioChunk(audioData []byte) {
	startTime := time.Now()

	// Mock transcription result (in real implementation, this would call STT provider)
	transcript := st.performTranscription(audioData)
	if transcript == nil {
		return
	}

	// Process speaker diarization
	if st.config.EnableDiarization && st.diarizer != nil {
		st.diarizer.ProcessAudio(audioData, transcript)
	}

	// Process sentiment analysis
	if st.config.EnableSentiment && st.sentimentEngine != nil && transcript.Text != "" {
		sentiment := st.sentimentEngine.AnalyzeText(transcript.Text)
		transcript.Sentiment = sentiment
	}

	// Process keyword detection
	if st.config.EnableKeywords && st.keywordDetector != nil && transcript.Text != "" {
		keywords := st.keywordDetector.DetectKeywords(transcript.Text)
		transcript.Keywords = keywords
	}

	// Send transcription event
	event := TranscriptionEvent{
		Type:      EventTypeFinalTranscript,
		SessionID: st.sessionID,
		CallID:    st.callID,
		Timestamp: time.Now(),
		Data:      *transcript,
	}

	if !transcript.IsFinal {
		event.Type = EventTypePartialTranscript
	}

	st.sendEvent(event)

	// Send additional events for features
	if transcript.Sentiment.Label != "" {
		st.sendSentimentEvent(transcript.Sentiment)
	}

	if len(transcript.Keywords) > 0 {
		st.sendKeywordEvents(transcript.Keywords)
	}

	// Update metrics
	processingTime := time.Since(startTime)
	st.metrics.AddProcessingTime(processingTime)
	st.metrics.IncrementTranscripts()
}

// performTranscription performs the actual transcription (mock implementation)
func (st *StreamingTranscriber) performTranscription(audioData []byte) *TranscriptionEventData {
	// This is a mock implementation - in reality, this would integrate with
	// actual STT providers (Google, Azure, AWS, etc.)

	if len(audioData) < 1000 { // Not enough data
		return nil
	}

	// Simulate processing time
	time.Sleep(10 * time.Millisecond)

	// Mock result
	return &TranscriptionEventData{
		Text:       "Mock transcription result",
		IsFinal:    len(audioData) > 5000, // Simulate partial vs final
		Confidence: 0.85,
		StartTime:  time.Since(st.startTime).Seconds(),
		EndTime:    time.Since(st.startTime).Seconds() + 2.0,
		Language:   st.config.Language,
		SpeakerID:  "speaker_1",
	}
}

// sendEvent sends an event to all subscribers
func (st *StreamingTranscriber) sendEvent(event TranscriptionEvent) {
	// Send to event channel for processing
	select {
	case st.eventChan <- event:
	default:
		st.logger.Warning("Event channel full, dropping event")
		st.metrics.IncrementDroppedEvents()
	}
}

// processEvents processes and distributes events to subscribers
func (st *StreamingTranscriber) processEvents() {
	defer func() {
		if r := recover(); r != nil {
			st.logger.WithField("panic", r).Error("Panic in event processing")
		}
	}()

	for {
		select {
		case <-st.ctx.Done():
			return
		case event, ok := <-st.eventChan:
			if !ok {
				return
			}

			st.distributeEvent(event)
		}
	}
}

// distributeEvent distributes an event to all subscribers and AMQP
func (st *StreamingTranscriber) distributeEvent(event TranscriptionEvent) {
	// Publish to AMQP if publisher is available
	if st.amqpPublisher != nil && st.amqpPublisher.IsStarted() {
		go func() {
			if err := st.amqpPublisher.PublishTranscriptionEvent(event); err != nil {
				st.logger.WithError(err).WithFields(logrus.Fields{
					"event_type": event.Type,
					"session_id": event.SessionID,
				}).Warning("Failed to publish event to AMQP")
			}
		}()
	}

	// Distribute to WebSocket subscribers
	st.subscribersMux.RLock()
	defer st.subscribersMux.RUnlock()

	for subscriberID, ch := range st.subscribers {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel is full, log warning
			st.logger.WithField("subscriber_id", subscriberID).Warning("Subscriber channel full, dropping event")
			st.metrics.IncrementDroppedEvents()
		}
	}
}

// sendSentimentEvent sends a sentiment analysis event
func (st *StreamingTranscriber) sendSentimentEvent(sentiment Sentiment) {
	event := TranscriptionEvent{
		Type:      EventTypeSentimentUpdate,
		SessionID: st.sessionID,
		CallID:    st.callID,
		Timestamp: time.Now(),
		Data: TranscriptionEventData{
			Sentiment: sentiment,
		},
	}
	st.sendEvent(event)
}

// sendKeywordEvents sends keyword detection events
func (st *StreamingTranscriber) sendKeywordEvents(keywords []Keyword) {
	for _, keyword := range keywords {
		event := TranscriptionEvent{
			Type:      EventTypeKeywordDetected,
			SessionID: st.sessionID,
			CallID:    st.callID,
			Timestamp: time.Now(),
			Data: TranscriptionEventData{
				Keywords: []Keyword{keyword},
			},
		}
		st.sendEvent(event)
	}
}

// monitorResources monitors memory and CPU usage
func (st *StreamingTranscriber) monitorResources() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			st.resourceMonitor.CheckResources()
		}
	}
}

// performPeriodicCleanup performs periodic memory cleanup
func (st *StreamingTranscriber) performPeriodicCleanup() {
	ticker := time.NewTicker(st.config.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			st.cleanup()
		}
	}
}

// cleanup performs memory and resource cleanup
func (st *StreamingTranscriber) cleanup() {
	// Clean up audio buffer
	if st.audioBuffer != nil {
		st.audioBuffer.Cleanup()
	}

	// Clean up feature processors
	if st.diarizer != nil {
		st.diarizer.Cleanup()
	}

	if st.sentimentEngine != nil {
		st.sentimentEngine.Cleanup()
	}

	if st.keywordDetector != nil {
		st.keywordDetector.Cleanup()
	}

	// Force garbage collection if memory usage is high
	st.resourceMonitor.OptimizeMemory()
}

// GetMetrics returns current streaming metrics
func (st *StreamingTranscriber) GetMetrics() *StreamingMetrics {
	return st.metrics
}

// IsActive returns whether the transcription session is active
func (st *StreamingTranscriber) IsActive() bool {
	st.activeMux.RLock()
	defer st.activeMux.RUnlock()
	return st.isActive
}

// GetSessionInfo returns session information
func (st *StreamingTranscriber) GetSessionInfo() map[string]interface{} {
	st.activeMux.RLock()
	defer st.activeMux.RUnlock()

	return map[string]interface{}{
		"session_id":    st.sessionID,
		"call_id":       st.callID,
		"is_active":     st.isActive,
		"start_time":    st.startTime,
		"duration":      time.Since(st.startTime).Seconds(),
		"last_activity": st.lastActivity,
		"subscribers":   len(st.subscribers),
		"config":        st.config,
		"metrics":       st.metrics.GetStats(),
	}
}
