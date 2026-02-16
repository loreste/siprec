package messaging

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/realtime"
)

// AMQPRealtimePublisher publishes real-time transcription events to AMQP
type AMQPRealtimePublisher struct {
	logger *logrus.Entry
	client AMQPClientInterface
	config *AMQPRealtimeConfig

	// Message queuing
	messageQueue chan *RealtimeAMQPMessage
	batchQueue   []*RealtimeAMQPMessage
	batchMutex   sync.Mutex

	// Control
	stopChan   chan struct{}
	started    bool
	startMutex sync.RWMutex

	// Statistics
	stats *AMQPPublisherStats
}

// AMQPRealtimeConfig configures the real-time AMQP publisher
type AMQPRealtimeConfig struct {
	// Basic AMQP configuration
	URL          string `json:"url"`
	QueueName    string `json:"queue_name"`
	ExchangeName string `json:"exchange_name"`
	RoutingKey   string `json:"routing_key"`

	// Real-time specific configuration
	BatchSize      int           `json:"batch_size"`
	BatchTimeout   time.Duration `json:"batch_timeout"`
	QueueSize      int           `json:"queue_size"`
	EnableBatching bool          `json:"enable_batching"`
	EnableRetries  bool          `json:"enable_retries"`
	MaxRetries     int           `json:"max_retries"`
	RetryDelay     time.Duration `json:"retry_delay"`

	// Event filtering
	PublishPartial       bool `json:"publish_partial"`
	PublishFinal         bool `json:"publish_final"`
	PublishSentiment     bool `json:"publish_sentiment"`
	PublishKeywords      bool `json:"publish_keywords"`
	PublishSpeakerChange bool `json:"publish_speaker_change"`

	// Message configuration
	MessageTTL        time.Duration `json:"message_ttl"`
	EnableCompression bool          `json:"enable_compression"`
	IncludeAudioData  bool          `json:"include_audio_data"`
}

// DefaultAMQPRealtimeConfig returns default configuration
func DefaultAMQPRealtimeConfig() *AMQPRealtimeConfig {
	return &AMQPRealtimeConfig{
		BatchSize:            10,
		BatchTimeout:         1 * time.Second,
		QueueSize:            1000,
		EnableBatching:       true,
		EnableRetries:        true,
		MaxRetries:           3,
		RetryDelay:           2 * time.Second,
		PublishPartial:       true,
		PublishFinal:         true,
		PublishSentiment:     true,
		PublishKeywords:      true,
		PublishSpeakerChange: true,
		MessageTTL:           1 * time.Hour,
		EnableCompression:    false,
		IncludeAudioData:     false,
	}
}

// RealtimeAMQPMessage represents a real-time transcription message for AMQP
type RealtimeAMQPMessage struct {
	// Message metadata
	MessageID string    `json:"message_id"`
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`

	// Session information
	SessionID string `json:"session_id"`
	CallID    string `json:"call_id"`

	// Transcription data
	Text       string  `json:"text,omitempty"`
	IsFinal    bool    `json:"is_final,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	StartTime  float64 `json:"start_time,omitempty"`
	EndTime    float64 `json:"end_time,omitempty"`
	Language   string  `json:"language,omitempty"`

	// Speaker information
	SpeakerID    string `json:"speaker_id,omitempty"`
	SpeakerLabel string `json:"speaker_label,omitempty"`
	SpeakerCount int    `json:"speaker_count,omitempty"`

	// SIPREC stream identification
	StreamLabel     string `json:"stream_label,omitempty"`
	ParticipantName string `json:"participant_name,omitempty"`
	ParticipantRole string `json:"participant_role,omitempty"`

	// Sentiment data
	Sentiment *realtime.Sentiment `json:"sentiment,omitempty"`

	// Keywords
	Keywords []realtime.Keyword `json:"keywords,omitempty"`

	// Additional metadata
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Audio data (optional)
	AudioData   []byte `json:"audio_data,omitempty"`
	AudioFormat string `json:"audio_format,omitempty"`
}

// AMQPPublisherStats tracks publisher statistics
type AMQPPublisherStats struct {
	mutex             sync.RWMutex
	TotalMessages     int64     `json:"total_messages"`
	PublishedMessages int64     `json:"published_messages"`
	FailedMessages    int64     `json:"failed_messages"`
	QueuedMessages    int64     `json:"queued_messages"`
	DroppedMessages   int64     `json:"dropped_messages"`
	BatchesSent       int64     `json:"batches_sent"`
	RetryAttempts     int64     `json:"retry_attempts"`
	AverageLatency    int64     `json:"average_latency_ms"`
	LastPublishTime   time.Time `json:"last_publish_time"`
	LastError         string    `json:"last_error,omitempty"`
	LastReset         time.Time `json:"last_reset"`
}

// NewAMQPRealtimePublisher creates a new real-time AMQP publisher
func NewAMQPRealtimePublisher(logger *logrus.Logger, client AMQPClientInterface, config *AMQPRealtimeConfig) *AMQPRealtimePublisher {
	if config == nil {
		config = DefaultAMQPRealtimeConfig()
	}

	publisher := &AMQPRealtimePublisher{
		logger:       logger.WithField("component", "amqp_realtime_publisher"),
		client:       client,
		config:       config,
		messageQueue: make(chan *RealtimeAMQPMessage, config.QueueSize),
		batchQueue:   make([]*RealtimeAMQPMessage, 0, config.BatchSize),
		stopChan:     make(chan struct{}),
		stats:        &AMQPPublisherStats{LastReset: time.Now()},
	}

	return publisher
}

// Start starts the AMQP publisher
func (p *AMQPRealtimePublisher) Start() error {
	p.startMutex.Lock()
	defer p.startMutex.Unlock()

	if p.started {
		return nil
	}

	p.started = true

	// Start message processing goroutines
	if p.config.EnableBatching {
		go p.batchProcessor()
	} else {
		go p.messageProcessor()
	}

	p.logger.WithFields(logrus.Fields{
		"queue_size":       p.config.QueueSize,
		"batch_size":       p.config.BatchSize,
		"batching_enabled": p.config.EnableBatching,
	}).Info("AMQP real-time publisher started")

	return nil
}

// Stop stops the AMQP publisher
func (p *AMQPRealtimePublisher) Stop() error {
	p.startMutex.Lock()
	defer p.startMutex.Unlock()

	if !p.started {
		return nil
	}

	close(p.stopChan)
	p.started = false

	p.logger.Info("AMQP real-time publisher stopped")
	return nil
}

// PublishTranscriptionEvent publishes a transcription event to AMQP
func (p *AMQPRealtimePublisher) PublishTranscriptionEvent(event realtime.TranscriptionEvent) error {
	// Check if this event type should be published
	if !p.shouldPublishEvent(event.Type) {
		return nil
	}

	// Convert to AMQP message
	amqpMessage := p.convertToAMQPMessage(event)

	// Try to enqueue message
	select {
	case p.messageQueue <- amqpMessage:
		p.stats.mutex.Lock()
		p.stats.TotalMessages++
		p.stats.QueuedMessages++
		p.stats.mutex.Unlock()
		return nil

	default:
		// Queue is full, drop message
		p.stats.mutex.Lock()
		p.stats.DroppedMessages++
		p.stats.mutex.Unlock()

		p.logger.WithFields(logrus.Fields{
			"event_type": event.Type,
			"session_id": event.SessionID,
			"call_id":    event.CallID,
		}).Warning("AMQP message queue full, dropping event")

		return nil // Don't return error to avoid blocking transcription
	}
}

// shouldPublishEvent checks if an event type should be published
func (p *AMQPRealtimePublisher) shouldPublishEvent(eventType realtime.EventType) bool {
	switch eventType {
	case realtime.EventTypePartialTranscript:
		return p.config.PublishPartial
	case realtime.EventTypeFinalTranscript:
		return p.config.PublishFinal
	case realtime.EventTypeSentimentUpdate:
		return p.config.PublishSentiment
	case realtime.EventTypeKeywordDetected:
		return p.config.PublishKeywords
	case realtime.EventTypeSpeakerChange:
		return p.config.PublishSpeakerChange
	default:
		return true // Publish unknown event types by default
	}
}

// convertToAMQPMessage converts a transcription event to AMQP message
func (p *AMQPRealtimePublisher) convertToAMQPMessage(event realtime.TranscriptionEvent) *RealtimeAMQPMessage {
	message := &RealtimeAMQPMessage{
		MessageID:    generateRealtimeMessageID(),
		Timestamp:    event.Timestamp,
		EventType:    string(event.Type),
		SessionID:    event.SessionID,
		CallID:       event.CallID,
		Text:         event.Data.Text,
		IsFinal:      event.Data.IsFinal,
		Confidence:   event.Data.Confidence,
		StartTime:    event.Data.StartTime,
		EndTime:      event.Data.EndTime,
		Language:     event.Data.Language,
		SpeakerID:    event.Data.SpeakerID,
		SpeakerLabel: event.Data.SpeakerLabel,
		SpeakerCount: event.Data.SpeakerCount,
		Metadata:     make(map[string]interface{}),
	}

	if event.Data.Metadata != nil {
		for key, value := range event.Data.Metadata {
			message.Metadata[key] = value
		}
		// Extract SIPREC stream labels from metadata into top-level fields
		if sl, ok := event.Data.Metadata["stream_label"].(string); ok {
			message.StreamLabel = sl
		}
		if pn, ok := event.Data.Metadata["participant_name"].(string); ok {
			message.ParticipantName = pn
		}
		if pr, ok := event.Data.Metadata["participant_role"].(string); ok {
			message.ParticipantRole = pr
		}
	}

	// Add sentiment if present
	if event.Data.Sentiment.Label != "" {
		message.Sentiment = &event.Data.Sentiment
	}

	// Add keywords if present
	if len(event.Data.Keywords) > 0 {
		message.Keywords = event.Data.Keywords
	}

	// Add additional metadata
	message.Metadata["event_source"] = "siprec-realtime"
	message.Metadata["server_timestamp"] = time.Now()

	return message
}

// messageProcessor processes messages individually (no batching)
func (p *AMQPRealtimePublisher) messageProcessor() {
	for {
		select {
		case <-p.stopChan:
			return

		case message := <-p.messageQueue:
			p.publishSingleMessage(message)
		}
	}
}

// batchProcessor processes messages in batches
func (p *AMQPRealtimePublisher) batchProcessor() {
	ticker := time.NewTicker(p.config.BatchTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Flush remaining messages before stopping
			p.flushBatch()
			return

		case message := <-p.messageQueue:
			p.batchMutex.Lock()
			p.batchQueue = append(p.batchQueue, message)
			shouldFlush := len(p.batchQueue) >= p.config.BatchSize
			p.batchMutex.Unlock()

			if shouldFlush {
				p.flushBatch()
			}

		case <-ticker.C:
			p.flushBatch()
		}
	}
}

// flushBatch sends the current batch of messages
func (p *AMQPRealtimePublisher) flushBatch() {
	p.batchMutex.Lock()
	if len(p.batchQueue) == 0 {
		p.batchMutex.Unlock()
		return
	}

	batch := make([]*RealtimeAMQPMessage, len(p.batchQueue))
	copy(batch, p.batchQueue)
	p.batchQueue = p.batchQueue[:0] // Clear batch queue
	p.batchMutex.Unlock()

	// Publish batch
	p.publishBatch(batch)
}

// publishBatch publishes a batch of messages
func (p *AMQPRealtimePublisher) publishBatch(messages []*RealtimeAMQPMessage) {
	startTime := time.Now()

	// Create batch message
	batchMessage := map[string]interface{}{
		"batch_id":      generateRealtimeMessageID(),
		"timestamp":     time.Now(),
		"message_count": len(messages),
		"messages":      messages,
	}

	// Marshal to JSON
	data, err := json.Marshal(batchMessage)
	if err != nil {
		p.logger.WithError(err).Error("Failed to marshal batch message")
		p.updateFailedStats(len(messages))
		return
	}

	// Publish batch
	err = p.publishToAMQP(data, "batch")

	// Update statistics
	processingTime := time.Since(startTime)
	p.stats.mutex.Lock()
	if err != nil {
		p.stats.FailedMessages += int64(len(messages))
		p.stats.LastError = err.Error()
	} else {
		p.stats.PublishedMessages += int64(len(messages))
		p.stats.BatchesSent++
		p.stats.LastPublishTime = time.Now()
	}
	p.stats.QueuedMessages -= int64(len(messages))

	// Update average latency
	if p.stats.PublishedMessages > 0 {
		p.stats.AverageLatency = (p.stats.AverageLatency*(p.stats.PublishedMessages-int64(len(messages))) +
			processingTime.Nanoseconds()/1e6) / p.stats.PublishedMessages
	}
	p.stats.mutex.Unlock()

	if err != nil {
		p.logger.WithError(err).WithField("batch_size", len(messages)).Error("Failed to publish batch to AMQP")
	} else {
		p.logger.WithFields(logrus.Fields{
			"batch_size":      len(messages),
			"processing_time": processingTime,
		}).Debug("Successfully published batch to AMQP")
	}
}

// publishSingleMessage publishes a single message
func (p *AMQPRealtimePublisher) publishSingleMessage(message *RealtimeAMQPMessage) {
	startTime := time.Now()

	// Marshal to JSON
	data, err := json.Marshal(message)
	if err != nil {
		p.logger.WithError(err).Error("Failed to marshal message")
		p.updateFailedStats(1)
		return
	}

	// Publish message
	err = p.publishToAMQP(data, message.EventType)

	// Update statistics
	processingTime := time.Since(startTime)
	p.stats.mutex.Lock()
	if err != nil {
		p.stats.FailedMessages++
		p.stats.LastError = err.Error()
	} else {
		p.stats.PublishedMessages++
		p.stats.LastPublishTime = time.Now()
	}
	p.stats.QueuedMessages--

	// Update average latency
	if p.stats.PublishedMessages > 0 {
		p.stats.AverageLatency = (p.stats.AverageLatency*(p.stats.PublishedMessages-1) +
			processingTime.Nanoseconds()/1e6) / p.stats.PublishedMessages
	}
	p.stats.mutex.Unlock()

	if err != nil {
		p.logger.WithError(err).WithFields(logrus.Fields{
			"event_type": message.EventType,
			"session_id": message.SessionID,
		}).Error("Failed to publish message to AMQP")
	}
}

// publishToAMQP publishes data to AMQP with retry logic
func (p *AMQPRealtimePublisher) publishToAMQP(data []byte, eventType string) error {
	if p.client == nil || !p.client.IsConnected() {
		return fmt.Errorf("AMQP client not connected")
	}

	var lastErr error
	maxRetries := 1
	if p.config.EnableRetries {
		maxRetries = p.config.MaxRetries
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			p.stats.mutex.Lock()
			p.stats.RetryAttempts++
			p.stats.mutex.Unlock()

			time.Sleep(p.config.RetryDelay)
		}

		// Create metadata for the message
		metadata := map[string]interface{}{
			"event_type":   eventType,
			"attempt":      attempt + 1,
			"max_attempts": maxRetries,
		}

		// Use legacy publish method
		err := p.client.PublishTranscription(string(data), generateRealtimeMessageID(), metadata)
		if err == nil {
			return nil
		}

		lastErr = err
		p.logger.WithError(err).WithFields(logrus.Fields{
			"attempt":      attempt + 1,
			"max_attempts": maxRetries,
			"event_type":   eventType,
		}).Warning("Failed to publish to AMQP, retrying")
	}

	return lastErr
}

// updateFailedStats updates failure statistics
func (p *AMQPRealtimePublisher) updateFailedStats(count int) {
	p.stats.mutex.Lock()
	p.stats.FailedMessages += int64(count)
	p.stats.QueuedMessages -= int64(count)
	p.stats.mutex.Unlock()
}

// GetStats returns publisher statistics
func (p *AMQPRealtimePublisher) GetStats() *AMQPPublisherStats {
	p.stats.mutex.RLock()
	defer p.stats.mutex.RUnlock()

	statsCopy := &AMQPPublisherStats{
		TotalMessages:     p.stats.TotalMessages,
		PublishedMessages: p.stats.PublishedMessages,
		FailedMessages:    p.stats.FailedMessages,
		QueuedMessages:    p.stats.QueuedMessages,
		DroppedMessages:   p.stats.DroppedMessages,
		BatchesSent:       p.stats.BatchesSent,
		RetryAttempts:     p.stats.RetryAttempts,
		AverageLatency:    p.stats.AverageLatency,
		LastPublishTime:   p.stats.LastPublishTime,
		LastError:         p.stats.LastError,
		LastReset:         p.stats.LastReset,
	}
	return statsCopy
}

// IsStarted returns whether the publisher is started
func (p *AMQPRealtimePublisher) IsStarted() bool {
	p.startMutex.RLock()
	defer p.startMutex.RUnlock()
	return p.started
}

// generateRealtimeMessageID generates a unique message ID for realtime messages
func generateRealtimeMessageID() string {
	return fmt.Sprintf("realtime_msg_%d_%d", time.Now().UnixNano(), rand.Int63())
}

// AMQPRealtimeConfigFromMessaging creates an AMQPRealtimeConfig from MessagingConfig
func AMQPRealtimeConfigFromMessaging(msgConfig interface{}) *AMQPRealtimeConfig {
	// Use type assertion to extract fields (avoids import cycle)
	config := &AMQPRealtimeConfig{
		URL:                  "",
		QueueName:            "siprec_realtime",
		ExchangeName:         "",
		RoutingKey:           "siprec.realtime",
		BatchSize:            10,
		BatchTimeout:         1 * time.Second,
		QueueSize:            1000,
		EnableBatching:       true,
		EnableRetries:        true,
		MaxRetries:           3,
		RetryDelay:           2 * time.Second,
		PublishPartial:       true,
		PublishFinal:         true,
		PublishSentiment:     true,
		PublishKeywords:      true,
		PublishSpeakerChange: true,
		MessageTTL:           1 * time.Hour,
		EnableCompression:    false,
		IncludeAudioData:     false,
	}

	// Extract configuration from msgConfig using reflection-like approach
	if msgConfig != nil {
		// This would be populated by the caller with the actual values
		// Implementation depends on how the config is passed from main app
	}

	return config
}
