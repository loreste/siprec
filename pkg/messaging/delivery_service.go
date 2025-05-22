package messaging

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// GuaranteedDeliveryService provides guaranteed message delivery with retry mechanisms
type GuaranteedDeliveryService struct {
	logger       *logrus.Logger
	amqpClient   AMQPClientInterface
	config       DeliveryConfig
	pendingQueue chan *PendingMessage
	storage      MessageStorage
	metrics      *DeliveryMetrics
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// DeliveryConfig holds configuration for guaranteed delivery
type DeliveryConfig struct {
	MaxRetries              int           // Maximum number of retry attempts
	InitialRetryDelay       time.Duration // Initial delay between retries
	MaxRetryDelay           time.Duration // Maximum delay between retries
	BackoffMultiplier       float64       // Exponential backoff multiplier
	MessageTimeout          time.Duration // Time after which messages are considered failed
	BatchSize               int           // Number of messages to process in a batch
	WorkerCount             int           // Number of concurrent workers
	PersistenceEnabled      bool          // Enable message persistence
	DeduplicationEnabled    bool          // Enable message deduplication
	DeduplicationWindow     time.Duration // Time window for deduplication
	CompressionEnabled      bool          // Enable message compression
	PriorityQueueEnabled    bool          // Enable priority-based message handling
	DeadLetterQueueEnabled  bool          // Enable dead letter queue for failed messages
	AckTimeoutDuration      time.Duration // Timeout for message acknowledgments
}

// PendingMessage represents a message pending delivery
type PendingMessage struct {
	ID            string                 `json:"id"`
	CallUUID      string                 `json:"call_uuid"`
	Content       string                 `json:"content"`
	Metadata      map[string]interface{} `json:"metadata"`
	CreatedAt     time.Time              `json:"created_at"`
	LastAttempt   time.Time              `json:"last_attempt"`
	AttemptCount  int                    `json:"attempt_count"`
	NextRetryAt   time.Time              `json:"next_retry_at"`
	Priority      MessagePriority        `json:"priority"`
	IsFinal       bool                   `json:"is_final"`
	Checksum      string                 `json:"checksum"`
	CompressedSize int                   `json:"compressed_size"`
}

// MessagePriority defines priority levels for messages
type MessagePriority int

const (
	PriorityLow MessagePriority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// MessageStorage interface for message persistence
type MessageStorage interface {
	Store(msg *PendingMessage) error
	Retrieve(id string) (*PendingMessage, error)
	List(limit int) ([]*PendingMessage, error)
	Delete(id string) error
	DeleteBatch(ids []string) error
	Count() (int, error)
	CleanupExpired(cutoff time.Time) (int, error)
}

// DeliveryMetrics tracks delivery performance
type DeliveryMetrics struct {
	TotalMessages       int64         `json:"total_messages"`
	SuccessfulDeliveries int64        `json:"successful_deliveries"`
	FailedDeliveries    int64         `json:"failed_deliveries"`
	RetriedMessages     int64         `json:"retried_messages"`
	AverageRetryCount   float64       `json:"average_retry_count"`
	AverageDeliveryTime time.Duration `json:"average_delivery_time"`
	DeadLetterCount     int64         `json:"dead_letter_count"`
	PendingCount        int64         `json:"pending_count"`
	LastDeliveryTime    time.Time     `json:"last_delivery_time"`
	ThroughputPerSecond float64       `json:"throughput_per_second"`
	mutex               sync.RWMutex
}

// DefaultDeliveryConfig returns default configuration for guaranteed delivery
func DefaultDeliveryConfig() DeliveryConfig {
	return DeliveryConfig{
		MaxRetries:              5,
		InitialRetryDelay:       1 * time.Second,
		MaxRetryDelay:           30 * time.Second,
		BackoffMultiplier:       2.0,
		MessageTimeout:          5 * time.Minute,
		BatchSize:               100,
		WorkerCount:             3,
		PersistenceEnabled:      true,
		DeduplicationEnabled:    true,
		DeduplicationWindow:     1 * time.Minute,
		CompressionEnabled:      false,
		PriorityQueueEnabled:    true,
		DeadLetterQueueEnabled:  true,
		AckTimeoutDuration:      30 * time.Second,
	}
}

// NewGuaranteedDeliveryService creates a new guaranteed delivery service
func NewGuaranteedDeliveryService(logger *logrus.Logger, amqpClient AMQPClientInterface, storage MessageStorage, cfg *DeliveryConfig) *GuaranteedDeliveryService {
	if cfg == nil {
		defaultCfg := DefaultDeliveryConfig()
		cfg = &defaultCfg
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &GuaranteedDeliveryService{
		logger:       logger,
		amqpClient:   amqpClient,
		config:       *cfg,
		pendingQueue: make(chan *PendingMessage, cfg.BatchSize*2),
		storage:      storage,
		metrics:      &DeliveryMetrics{},
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start initializes the guaranteed delivery service
func (d *GuaranteedDeliveryService) Start() error {
	d.logger.Info("Starting guaranteed delivery service")

	// Start worker goroutines
	for i := 0; i < d.config.WorkerCount; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}

	// Start retry processor
	d.wg.Add(1)
	go d.retryProcessor()

	// Start metrics collector
	d.wg.Add(1)
	go d.metricsCollector()

	// Start cleanup routine
	d.wg.Add(1)
	go d.cleanupRoutine()

	// Recover pending messages from storage
	if d.config.PersistenceEnabled && d.storage != nil {
		d.recoverPendingMessages()
	}

	d.logger.WithFields(logrus.Fields{
		"worker_count":     d.config.WorkerCount,
		"max_retries":      d.config.MaxRetries,
		"persistence":      d.config.PersistenceEnabled,
		"deduplication":    d.config.DeduplicationEnabled,
		"priority_queue":   d.config.PriorityQueueEnabled,
		"dead_letter_queue": d.config.DeadLetterQueueEnabled,
	}).Info("Guaranteed delivery service started")

	return nil
}

// Stop shuts down the guaranteed delivery service
func (d *GuaranteedDeliveryService) Stop() error {
	d.logger.Info("Stopping guaranteed delivery service")

	d.cancel()
	close(d.pendingQueue)
	d.wg.Wait()

	d.logger.Info("Guaranteed delivery service stopped")
	return nil
}

// SendMessage sends a message with guaranteed delivery
func (d *GuaranteedDeliveryService) SendMessage(callUUID, content string, metadata map[string]interface{}, priority MessagePriority) error {
	// Generate unique message ID
	messageID := fmt.Sprintf("%s-%d", callUUID, time.Now().UnixNano())

	// Create pending message
	msg := &PendingMessage{
		ID:           messageID,
		CallUUID:     callUUID,
		Content:      content,
		Metadata:     metadata,
		CreatedAt:    time.Now(),
		Priority:     priority,
		AttemptCount: 0,
		NextRetryAt:  time.Now(),
	}

	// Check if message is final transcription
	if metadata != nil {
		if isFinal, ok := metadata["is_final"].(bool); ok {
			msg.IsFinal = isFinal
		}
	}

	// Apply deduplication if enabled
	if d.config.DeduplicationEnabled {
		msg.Checksum = d.calculateChecksum(content, callUUID)
		if d.isDuplicate(msg) {
			d.logger.WithFields(logrus.Fields{
				"message_id": messageID,
				"call_uuid":  callUUID,
				"checksum":   msg.Checksum,
			}).Debug("Duplicate message detected, skipping")
			return nil
		}
	}

	// Apply compression if enabled
	if d.config.CompressionEnabled {
		compressedContent, err := d.compressContent(content)
		if err != nil {
			d.logger.WithError(err).Warn("Failed to compress message content")
		} else {
			msg.Content = compressedContent
			msg.CompressedSize = len(compressedContent)
		}
	}

	// Persist message if enabled
	if d.config.PersistenceEnabled && d.storage != nil {
		if err := d.storage.Store(msg); err != nil {
			d.logger.WithError(err).WithField("message_id", messageID).Error("Failed to persist message")
			return fmt.Errorf("failed to persist message: %w", err)
		}
	}

	// Add to pending queue
	select {
	case d.pendingQueue <- msg:
		d.updateMetrics(msg, false, false)
		d.logger.WithFields(logrus.Fields{
			"message_id": messageID,
			"call_uuid":  callUUID,
			"priority":   priority,
		}).Debug("Message queued for delivery")
		return nil
	case <-d.ctx.Done():
		return fmt.Errorf("delivery service is shutting down")
	default:
		return fmt.Errorf("delivery queue is full")
	}
}

// worker processes messages from the pending queue
func (d *GuaranteedDeliveryService) worker(workerID int) {
	defer d.wg.Done()

	logger := d.logger.WithField("worker_id", workerID)
	logger.Debug("Worker started")

	for {
		select {
		case <-d.ctx.Done():
			logger.Debug("Worker shutting down")
			return
		case msg, ok := <-d.pendingQueue:
			if !ok {
				logger.Debug("Pending queue closed, worker shutting down")
				return
			}

			d.processMessage(msg, logger)
		}
	}
}

// processMessage attempts to deliver a message
func (d *GuaranteedDeliveryService) processMessage(msg *PendingMessage, logger *logrus.Entry) {
	startTime := time.Now()
	msg.LastAttempt = startTime
	msg.AttemptCount++

	logger = logger.WithFields(logrus.Fields{
		"message_id":    msg.ID,
		"call_uuid":     msg.CallUUID,
		"attempt_count": msg.AttemptCount,
		"priority":      msg.Priority,
	})

	// Check if message has exceeded maximum retries
	if msg.AttemptCount > d.config.MaxRetries {
		d.handleFailedMessage(msg, fmt.Errorf("exceeded maximum retries (%d)", d.config.MaxRetries), logger)
		return
	}

	// Check if message has timed out
	if time.Since(msg.CreatedAt) > d.config.MessageTimeout {
		d.handleFailedMessage(msg, fmt.Errorf("message timeout exceeded"), logger)
		return
	}

	// Decompress content if needed
	content := msg.Content
	if d.config.CompressionEnabled && msg.CompressedSize > 0 {
		decompressed, err := d.decompressContent(msg.Content)
		if err != nil {
			logger.WithError(err).Error("Failed to decompress message content")
			d.scheduleRetry(msg, err, logger)
			return
		}
		content = decompressed
	}

	// Attempt delivery
	err := d.attemptDelivery(msg.CallUUID, content, msg.Metadata)
	deliveryTime := time.Since(startTime)

	if err != nil {
		logger.WithError(err).WithField("delivery_time", deliveryTime).Warn("Message delivery failed")
		d.scheduleRetry(msg, err, logger)
		return
	}

	// Delivery successful
	logger.WithField("delivery_time", deliveryTime).Info("Message delivered successfully")
	d.handleSuccessfulDelivery(msg, deliveryTime, logger)
}

// attemptDelivery attempts to deliver a message via AMQP
func (d *GuaranteedDeliveryService) attemptDelivery(callUUID, content string, metadata map[string]interface{}) error {
	if d.amqpClient == nil {
		return fmt.Errorf("AMQP client is not available")
	}

	if !d.amqpClient.IsConnected() {
		return fmt.Errorf("AMQP client is not connected")
	}

	// Create delivery context with timeout
	ctx, cancel := context.WithTimeout(d.ctx, d.config.AckTimeoutDuration)
	defer cancel()

	// Attempt to publish message
	return d.publishWithTimeout(ctx, callUUID, content, metadata)
}

// publishWithTimeout publishes a message with timeout
func (d *GuaranteedDeliveryService) publishWithTimeout(ctx context.Context, callUUID, content string, metadata map[string]interface{}) error {
	done := make(chan error, 1)

	go func() {
		done <- d.amqpClient.PublishTranscription(content, callUUID, metadata)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("delivery timeout: %w", ctx.Err())
	}
}

// scheduleRetry schedules a message for retry
func (d *GuaranteedDeliveryService) scheduleRetry(msg *PendingMessage, err error, logger *logrus.Entry) {
	// Calculate next retry delay using exponential backoff
	delay := d.calculateRetryDelay(msg.AttemptCount)
	msg.NextRetryAt = time.Now().Add(delay)

	logger.WithFields(logrus.Fields{
		"next_retry_at": msg.NextRetryAt,
		"delay":         delay,
		"error":         err.Error(),
	}).Debug("Scheduling message retry")

	// Update persistence if enabled
	if d.config.PersistenceEnabled && d.storage != nil {
		if storeErr := d.storage.Store(msg); storeErr != nil {
			logger.WithError(storeErr).Error("Failed to update message in storage")
		}
	}

	// Schedule for retry processing
	go func() {
		select {
		case <-time.After(delay):
			select {
			case d.pendingQueue <- msg:
				d.updateMetrics(msg, false, true)
			case <-d.ctx.Done():
				return
			}
		case <-d.ctx.Done():
			return
		}
	}()
}

// calculateRetryDelay calculates the retry delay using exponential backoff
func (d *GuaranteedDeliveryService) calculateRetryDelay(attemptCount int) time.Duration {
	delay := float64(d.config.InitialRetryDelay) * (d.config.BackoffMultiplier * float64(attemptCount))
	
	if delay > float64(d.config.MaxRetryDelay) {
		delay = float64(d.config.MaxRetryDelay)
	}

	return time.Duration(delay)
}

// handleSuccessfulDelivery handles a successful message delivery
func (d *GuaranteedDeliveryService) handleSuccessfulDelivery(msg *PendingMessage, deliveryTime time.Duration, logger *logrus.Entry) {
	// Update metrics
	d.updateMetrics(msg, true, false)

	// Remove from persistent storage if enabled
	if d.config.PersistenceEnabled && d.storage != nil {
		if err := d.storage.Delete(msg.ID); err != nil {
			logger.WithError(err).Error("Failed to delete message from storage")
		}
	}

	logger.WithFields(logrus.Fields{
		"delivery_time":  deliveryTime,
		"attempt_count":  msg.AttemptCount,
		"total_duration": time.Since(msg.CreatedAt),
	}).Debug("Message delivery completed successfully")
}

// handleFailedMessage handles a permanently failed message
func (d *GuaranteedDeliveryService) handleFailedMessage(msg *PendingMessage, err error, logger *logrus.Entry) {
	logger.WithError(err).WithFields(logrus.Fields{
		"attempt_count":  msg.AttemptCount,
		"total_duration": time.Since(msg.CreatedAt),
	}).Error("Message delivery permanently failed")

	// Update metrics
	d.updateMetrics(msg, false, false)
	d.metrics.mutex.Lock()
	d.metrics.FailedDeliveries++
	if d.config.DeadLetterQueueEnabled {
		d.metrics.DeadLetterCount++
	}
	d.metrics.mutex.Unlock()

	// Handle dead letter queue if enabled
	if d.config.DeadLetterQueueEnabled {
		d.sendToDeadLetterQueue(msg, err, logger)
	}

	// Remove from persistent storage if enabled
	if d.config.PersistenceEnabled && d.storage != nil {
		if deleteErr := d.storage.Delete(msg.ID); deleteErr != nil {
			logger.WithError(deleteErr).Error("Failed to delete failed message from storage")
		}
	}
}

// sendToDeadLetterQueue sends a failed message to the dead letter queue
func (d *GuaranteedDeliveryService) sendToDeadLetterQueue(msg *PendingMessage, originalErr error, logger *logrus.Entry) {
	deadLetterMetadata := map[string]interface{}{
		"original_error":   originalErr.Error(),
		"attempt_count":    msg.AttemptCount,
		"created_at":       msg.CreatedAt,
		"failed_at":        time.Now(),
		"original_metadata": msg.Metadata,
	}

	// Attempt to send to dead letter queue (with simplified retry)
	for attempts := 0; attempts < 3; attempts++ {
		if d.amqpClient != nil && d.amqpClient.IsConnected() {
			if err := d.amqpClient.PublishToDeadLetterQueue(msg.Content, msg.CallUUID, deadLetterMetadata); err != nil {
				logger.WithError(err).WithField("attempt", attempts+1).Warn("Failed to send message to dead letter queue")
				time.Sleep(time.Second * time.Duration(attempts+1))
				continue
			}
			logger.Info("Message sent to dead letter queue")
			return
		}
		time.Sleep(time.Second * time.Duration(attempts+1))
	}

	logger.Error("Failed to send message to dead letter queue after all attempts")
}

// retryProcessor handles retry scheduling and processing
func (d *GuaranteedDeliveryService) retryProcessor() {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.processRetries()
		}
	}
}

// processRetries processes messages that are ready for retry
func (d *GuaranteedDeliveryService) processRetries() {
	if !d.config.PersistenceEnabled || d.storage == nil {
		return
	}

	// Get pending messages from storage
	messages, err := d.storage.List(d.config.BatchSize)
	if err != nil {
		d.logger.WithError(err).Error("Failed to retrieve pending messages from storage")
		return
	}

	now := time.Now()
	for _, msg := range messages {
		if msg.NextRetryAt.Before(now) || msg.NextRetryAt.Equal(now) {
			select {
			case d.pendingQueue <- msg:
				d.logger.WithField("message_id", msg.ID).Debug("Requeued message for retry")
			case <-d.ctx.Done():
				return
			default:
				d.logger.WithField("message_id", msg.ID).Warn("Failed to requeue message - queue full")
			}
		}
	}
}

// recoverPendingMessages recovers pending messages from storage on startup
func (d *GuaranteedDeliveryService) recoverPendingMessages() {
	if d.storage == nil {
		return
	}

	messages, err := d.storage.List(1000) // Recovery limit
	if err != nil {
		d.logger.WithError(err).Error("Failed to recover pending messages from storage")
		return
	}

	recovered := 0
	for _, msg := range messages {
		// Check if message hasn't timed out
		if time.Since(msg.CreatedAt) <= d.config.MessageTimeout {
			select {
			case d.pendingQueue <- msg:
				recovered++
			case <-d.ctx.Done():
				return
			default:
				break
			}
		} else {
			// Clean up expired message
			d.storage.Delete(msg.ID)
		}
	}

	d.logger.WithField("recovered_count", recovered).Info("Recovered pending messages from storage")
}

// calculateChecksum calculates a checksum for deduplication
func (d *GuaranteedDeliveryService) calculateChecksum(content, callUUID string) string {
	return fmt.Sprintf("%x", content+callUUID) // Simple hash - could use more sophisticated hashing
}

// isDuplicate checks if a message is a duplicate within the deduplication window
func (d *GuaranteedDeliveryService) isDuplicate(msg *PendingMessage) bool {
	if !d.config.DeduplicationEnabled || d.storage == nil {
		return false
	}

	// Simple deduplication check - could be enhanced with more sophisticated logic
	// This is a placeholder for demonstration
	return false
}

// compressContent compresses message content (placeholder implementation)
func (d *GuaranteedDeliveryService) compressContent(content string) (string, error) {
	// Placeholder for compression implementation
	// Could use gzip, lz4, or other compression algorithms
	return content, nil
}

// decompressContent decompresses message content (placeholder implementation)
func (d *GuaranteedDeliveryService) decompressContent(content string) (string, error) {
	// Placeholder for decompression implementation
	return content, nil
}

// updateMetrics updates delivery metrics
func (d *GuaranteedDeliveryService) updateMetrics(msg *PendingMessage, success, retry bool) {
	d.metrics.mutex.Lock()
	defer d.metrics.mutex.Unlock()

	if success {
		d.metrics.SuccessfulDeliveries++
		d.metrics.LastDeliveryTime = time.Now()
	}

	if retry {
		d.metrics.RetriedMessages++
	}

	d.metrics.TotalMessages++

	// Update average retry count
	if d.metrics.TotalMessages == 1 {
		d.metrics.AverageRetryCount = float64(msg.AttemptCount)
	} else {
		d.metrics.AverageRetryCount = (d.metrics.AverageRetryCount*float64(d.metrics.TotalMessages-1) + float64(msg.AttemptCount)) / float64(d.metrics.TotalMessages)
	}
}

// metricsCollector collects and logs metrics periodically
func (d *GuaranteedDeliveryService) metricsCollector() {
	defer d.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.logMetrics()
		}
	}
}

// logMetrics logs current delivery metrics
func (d *GuaranteedDeliveryService) logMetrics() {
	d.metrics.mutex.RLock()
	defer d.metrics.mutex.RUnlock()

	d.logger.WithFields(logrus.Fields{
		"total_messages":        d.metrics.TotalMessages,
		"successful_deliveries": d.metrics.SuccessfulDeliveries,
		"failed_deliveries":     d.metrics.FailedDeliveries,
		"retried_messages":      d.metrics.RetriedMessages,
		"average_retry_count":   d.metrics.AverageRetryCount,
		"dead_letter_count":     d.metrics.DeadLetterCount,
	}).Info("Guaranteed delivery metrics")
}

// cleanupRoutine periodically cleans up expired messages
func (d *GuaranteedDeliveryService) cleanupRoutine() {
	defer d.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.cleanupExpiredMessages()
		}
	}
}

// cleanupExpiredMessages removes expired messages from storage
func (d *GuaranteedDeliveryService) cleanupExpiredMessages() {
	if !d.config.PersistenceEnabled || d.storage == nil {
		return
	}

	cutoff := time.Now().Add(-d.config.MessageTimeout)
	cleaned, err := d.storage.CleanupExpired(cutoff)
	if err != nil {
		d.logger.WithError(err).Error("Failed to cleanup expired messages")
		return
	}

	if cleaned > 0 {
		d.logger.WithField("cleaned_count", cleaned).Info("Cleaned up expired messages")
	}
}

// GetMetrics returns current delivery metrics
func (d *GuaranteedDeliveryService) GetMetrics() DeliveryMetrics {
	d.metrics.mutex.RLock()
	defer d.metrics.mutex.RUnlock()
	return *d.metrics
}

// GetPendingCount returns the current number of pending messages
func (d *GuaranteedDeliveryService) GetPendingCount() int64 {
	if d.storage == nil {
		return 0
	}

	count, err := d.storage.Count()
	if err != nil {
		d.logger.WithError(err).Error("Failed to get pending message count")
		return 0
	}

	return int64(count)
}