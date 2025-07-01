package messaging

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

// DeadLetterQueueManager manages dead letter queue operations
type DeadLetterQueueManager struct {
	logger              *logrus.Logger
	pool                *AMQPPool
	config              DeadLetterConfig
	exchangeManager     *ExchangeManager
	queueManager        *QueueManager
	retryScheduler      *RetryScheduler
	poisonMessageDetector *PoisonMessageDetector
	initialized         bool
	mutex               sync.RWMutex
}

// DeadLetterConfig holds dead letter queue configuration
type DeadLetterConfig struct {
	ExchangeName        string
	QueueName           string
	RoutingKey          string
	RetryExchangeName   string
	RetryQueueName      string
	PoisonQueueName     string
	MaxRetries          int
	RetryDelay          time.Duration
	RetryBackoffFactor  float64
	MaxRetryDelay       time.Duration
	EnableRetryScheduler bool
	EnablePoisonDetection bool
	PoisonThreshold     int
	TTL                 time.Duration
}

// RetryScheduler handles message retry scheduling
type RetryScheduler struct {
	logger      *logrus.Logger
	pool        *AMQPPool
	config      DeadLetterConfig
	retryJobs   map[string]*RetryJob
	mutex       sync.RWMutex
	stopChan    chan struct{}
	running     bool
}

// RetryJob represents a scheduled retry operation
type RetryJob struct {
	MessageID   string
	Content     []byte
	Headers     amqp.Table
	RetryCount  int
	NextRetry   time.Time
	OriginalExchange string
	OriginalRoutingKey string
	FailureReason string
}

// PoisonMessageDetector identifies and isolates poison messages
type PoisonMessageDetector struct {
	logger          *logrus.Logger
	pool            *AMQPPool
	failureTracking map[string]*MessageFailureInfo
	threshold       int
	mutex           sync.RWMutex
}

// MessageFailureInfo tracks message failure patterns
type MessageFailureInfo struct {
	MessageHash    string
	FailureCount   int
	LastFailure    time.Time
	FailureReasons []string
	FirstSeen      time.Time
}

// DeadLetterMessage represents a message in the dead letter queue
type DeadLetterMessage struct {
	OriginalMessage   interface{}               `json:"original_message"`
	FailureReason     string                    `json:"failure_reason"`
	FailureTimestamp  time.Time                 `json:"failure_timestamp"`
	RetryCount        int                       `json:"retry_count"`
	OriginalExchange  string                    `json:"original_exchange"`
	OriginalRoutingKey string                   `json:"original_routing_key"`
	OriginalHeaders   map[string]interface{}    `json:"original_headers"`
	MessageID         string                    `json:"message_id"`
	CorrelationID     string                    `json:"correlation_id"`
	DeadLetterReason  string                    `json:"dead_letter_reason"`
	ProcessingHistory []ProcessingEvent         `json:"processing_history"`
}

// ProcessingEvent represents an event in message processing history
type ProcessingEvent struct {
	Timestamp   time.Time `json:"timestamp"`
	Event       string    `json:"event"`
	Details     string    `json:"details"`
	ServiceID   string    `json:"service_id"`
}

// NewDeadLetterQueueManager creates a new dead letter queue manager
func NewDeadLetterQueueManager(logger *logrus.Logger, pool *AMQPPool, config DeadLetterConfig, exchangeManager *ExchangeManager, queueManager *QueueManager) *DeadLetterQueueManager {
	dlqm := &DeadLetterQueueManager{
		logger:          logger,
		pool:            pool,
		config:          config,
		exchangeManager: exchangeManager,
		queueManager:    queueManager,
	}
	
	if config.EnableRetryScheduler {
		dlqm.retryScheduler = NewRetryScheduler(logger, pool, config)
	}
	
	if config.EnablePoisonDetection {
		dlqm.poisonMessageDetector = NewPoisonMessageDetector(logger, pool, config.PoisonThreshold)
	}
	
	return dlqm
}

// NewRetryScheduler creates a new retry scheduler
func NewRetryScheduler(logger *logrus.Logger, pool *AMQPPool, config DeadLetterConfig) *RetryScheduler {
	return &RetryScheduler{
		logger:    logger,
		pool:      pool,
		config:    config,
		retryJobs: make(map[string]*RetryJob),
		stopChan:  make(chan struct{}),
	}
}

// NewPoisonMessageDetector creates a new poison message detector
func NewPoisonMessageDetector(logger *logrus.Logger, pool *AMQPPool, threshold int) *PoisonMessageDetector {
	return &PoisonMessageDetector{
		logger:          logger,
		pool:            pool,
		failureTracking: make(map[string]*MessageFailureInfo),
		threshold:       threshold,
	}
}

// Initialize sets up the dead letter queue infrastructure
func (dlqm *DeadLetterQueueManager) Initialize() error {
	dlqm.mutex.Lock()
	defer dlqm.mutex.Unlock()
	
	if dlqm.initialized {
		return nil
	}
	
	// Create dead letter exchange
	dlxConfig := ExchangeInfo{
		Name:       dlqm.config.ExchangeName,
		Type:       "direct",
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
		Arguments:  make(map[string]interface{}),
	}
	
	if err := dlqm.createExchange(dlxConfig); err != nil {
		return fmt.Errorf("failed to create dead letter exchange: %w", err)
	}
	
	// Create dead letter queue
	dlqConfig := QueueInfo{
		Name:       dlqm.config.QueueName,
		Durable:    true,
		AutoDelete: false,
		Exclusive:  false,
		Arguments:  make(map[string]interface{}),
		Bindings: []BindingInfo{
			{
				Exchange:   dlqm.config.ExchangeName,
				RoutingKey: dlqm.config.RoutingKey,
				Arguments:  make(map[string]interface{}),
			},
		},
	}
	
	if err := dlqm.createQueue(dlqConfig); err != nil {
		return fmt.Errorf("failed to create dead letter queue: %w", err)
	}
	
	// Create retry infrastructure if enabled
	if dlqm.config.EnableRetryScheduler {
		if err := dlqm.setupRetryInfrastructure(); err != nil {
			return fmt.Errorf("failed to setup retry infrastructure: %w", err)
		}
		
		// Start retry scheduler
		go dlqm.retryScheduler.Start()
	}
	
	// Create poison message queue if enabled
	if dlqm.config.EnablePoisonDetection {
		if err := dlqm.setupPoisonMessageQueue(); err != nil {
			return fmt.Errorf("failed to setup poison message queue: %w", err)
		}
	}
	
	dlqm.initialized = true
	dlqm.logger.Info("Dead letter queue manager initialized successfully")
	
	return nil
}

// setupRetryInfrastructure creates retry exchange and queue
func (dlqm *DeadLetterQueueManager) setupRetryInfrastructure() error {
	// Create retry exchange
	retryExchangeConfig := ExchangeInfo{
		Name:       dlqm.config.RetryExchangeName,
		Type:       "direct",
		Durable:    true,
		AutoDelete: false,
		Internal:   false,
		Arguments:  make(map[string]interface{}),
	}
	
	if err := dlqm.createExchange(retryExchangeConfig); err != nil {
		return err
	}
	
	// Create retry queue with TTL
	retryArguments := make(map[string]interface{})
	retryArguments["x-message-ttl"] = int64(dlqm.config.RetryDelay.Milliseconds())
	retryArguments["x-dead-letter-exchange"] = dlqm.config.ExchangeName
	retryArguments["x-dead-letter-routing-key"] = "retry"
	
	retryQueueConfig := QueueInfo{
		Name:       dlqm.config.RetryQueueName,
		Durable:    true,
		AutoDelete: false,
		Exclusive:  false,
		Arguments:  retryArguments,
		Bindings: []BindingInfo{
			{
				Exchange:   dlqm.config.RetryExchangeName,
				RoutingKey: "retry",
				Arguments:  make(map[string]interface{}),
			},
		},
	}
	
	return dlqm.createQueue(retryQueueConfig)
}

// setupPoisonMessageQueue creates poison message queue
func (dlqm *DeadLetterQueueManager) setupPoisonMessageQueue() error {
	poisonQueueConfig := QueueInfo{
		Name:       dlqm.config.PoisonQueueName,
		Durable:    true,
		AutoDelete: false,
		Exclusive:  false,
		Arguments:  make(map[string]interface{}),
		Bindings: []BindingInfo{
			{
				Exchange:   dlqm.config.ExchangeName,
				RoutingKey: "poison",
				Arguments:  make(map[string]interface{}),
			},
		},
	}
	
	return dlqm.createQueue(poisonQueueConfig)
}

// SendToDeadLetter sends a failed message to the dead letter queue
func (dlqm *DeadLetterQueueManager) SendToDeadLetter(originalMessage interface{}, failureReason string, originalExchange, originalRoutingKey string, originalHeaders map[string]interface{}, retryCount int) error {
	if !dlqm.initialized {
		return fmt.Errorf("dead letter queue manager not initialized")
	}
	
	// Check for poison message
	if dlqm.config.EnablePoisonDetection && dlqm.poisonMessageDetector != nil {
		if isPoisonMessage := dlqm.poisonMessageDetector.CheckMessage(originalMessage, failureReason); isPoisonMessage {
			return dlqm.sendToPoisonQueue(originalMessage, failureReason, originalHeaders)
		}
	}
	
	// Check if message should be retried
	if dlqm.config.EnableRetryScheduler && retryCount < dlqm.config.MaxRetries {
		return dlqm.scheduleRetry(originalMessage, failureReason, originalExchange, originalRoutingKey, originalHeaders, retryCount)
	}
	
	// Send to dead letter queue
	deadLetterMessage := DeadLetterMessage{
		OriginalMessage:    originalMessage,
		FailureReason:      failureReason,
		FailureTimestamp:   time.Now(),
		RetryCount:         retryCount,
		OriginalExchange:   originalExchange,
		OriginalRoutingKey: originalRoutingKey,
		OriginalHeaders:    originalHeaders,
		MessageID:          generateMessageID(),
		DeadLetterReason:   "max_retries_exceeded",
		ProcessingHistory:  []ProcessingEvent{
			{
				Timestamp: time.Now(),
				Event:     "dead_letter",
				Details:   failureReason,
				ServiceID: "siprec-server",
			},
		},
	}
	
	messageBytes, err := json.Marshal(deadLetterMessage)
	if err != nil {
		return fmt.Errorf("failed to marshal dead letter message: %w", err)
	}
	
	headers := make(amqp.Table)
	headers["x-death-reason"] = failureReason
	headers["x-retry-count"] = retryCount
	headers["x-original-exchange"] = originalExchange
	headers["x-original-routing-key"] = originalRoutingKey
	headers["x-death-timestamp"] = time.Now().Unix()
	
	return dlqm.pool.PublishWithConfirm(
		dlqm.config.ExchangeName,
		dlqm.config.RoutingKey,
		messageBytes,
		headers,
	)
}

// scheduleRetry schedules a message for retry
func (dlqm *DeadLetterQueueManager) scheduleRetry(originalMessage interface{}, failureReason, originalExchange, originalRoutingKey string, originalHeaders map[string]interface{}, retryCount int) error {
	if dlqm.retryScheduler == nil {
		return fmt.Errorf("retry scheduler not enabled")
	}
	
	// Calculate retry delay with exponential backoff
	delay := dlqm.calculateRetryDelay(retryCount)
	
	retryJob := &RetryJob{
		MessageID:          generateMessageID(),
		RetryCount:         retryCount + 1,
		NextRetry:          time.Now().Add(delay),
		OriginalExchange:   originalExchange,
		OriginalRoutingKey: originalRoutingKey,
		FailureReason:      failureReason,
	}
	
	// Marshal original message
	messageBytes, err := json.Marshal(originalMessage)
	if err != nil {
		return fmt.Errorf("failed to marshal message for retry: %w", err)
	}
	retryJob.Content = messageBytes
	
	// Convert headers
	retryJob.Headers = make(amqp.Table)
	for k, v := range originalHeaders {
		retryJob.Headers[k] = v
	}
	retryJob.Headers["x-retry-count"] = retryJob.RetryCount
	retryJob.Headers["x-retry-scheduled"] = time.Now().Unix()
	
	return dlqm.retryScheduler.ScheduleRetry(retryJob)
}

// sendToPoisonQueue sends a poison message to the poison queue
func (dlqm *DeadLetterQueueManager) sendToPoisonQueue(originalMessage interface{}, failureReason string, originalHeaders map[string]interface{}) error {
	poisonMessage := map[string]interface{}{
		"original_message": originalMessage,
		"failure_reason":   failureReason,
		"poison_detected":  time.Now(),
		"original_headers": originalHeaders,
		"poison_reason":    "excessive_failures",
	}
	
	messageBytes, err := json.Marshal(poisonMessage)
	if err != nil {
		return fmt.Errorf("failed to marshal poison message: %w", err)
	}
	
	headers := make(amqp.Table)
	headers["x-poison-message"] = true
	headers["x-poison-detected"] = time.Now().Unix()
	headers["x-poison-reason"] = "excessive_failures"
	
	return dlqm.pool.PublishWithConfirm(
		dlqm.config.ExchangeName,
		"poison",
		messageBytes,
		headers,
	)
}

// calculateRetryDelay calculates retry delay with exponential backoff
func (dlqm *DeadLetterQueueManager) calculateRetryDelay(retryCount int) time.Duration {
	delay := dlqm.config.RetryDelay
	
	// Apply exponential backoff
	for i := 0; i < retryCount; i++ {
		delay = time.Duration(float64(delay) * dlqm.config.RetryBackoffFactor)
		if delay > dlqm.config.MaxRetryDelay {
			delay = dlqm.config.MaxRetryDelay
			break
		}
	}
	
	return delay
}

// Start starts the retry scheduler
func (rs *RetryScheduler) Start() {
	rs.mutex.Lock()
	if rs.running {
		rs.mutex.Unlock()
		return
	}
	rs.running = true
	rs.mutex.Unlock()
	
	rs.logger.Info("Retry scheduler started")
	
	ticker := time.NewTicker(time.Second * 10) // Check every 10 seconds
	defer ticker.Stop()
	
	for {
		select {
		case <-rs.stopChan:
			rs.logger.Info("Retry scheduler stopped")
			return
		case <-ticker.C:
			rs.processRetries()
		}
	}
}

// Stop stops the retry scheduler
func (rs *RetryScheduler) Stop() {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	
	if !rs.running {
		return
	}
	
	close(rs.stopChan)
	rs.running = false
}

// ScheduleRetry schedules a retry job
func (rs *RetryScheduler) ScheduleRetry(job *RetryJob) error {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	
	rs.retryJobs[job.MessageID] = job
	
	rs.logger.WithFields(logrus.Fields{
		"message_id":    job.MessageID,
		"retry_count":   job.RetryCount,
		"next_retry":    job.NextRetry,
		"original_exchange": job.OriginalExchange,
	}).Info("Retry job scheduled")
	
	return nil
}

// processRetries processes pending retry jobs
func (rs *RetryScheduler) processRetries() {
	rs.mutex.Lock()
	defer rs.mutex.Unlock()
	
	now := time.Now()
	var toRetry []*RetryJob
	
	// Find jobs ready for retry
	for id, job := range rs.retryJobs {
		if now.After(job.NextRetry) {
			toRetry = append(toRetry, job)
			delete(rs.retryJobs, id)
		}
	}
	
	// Process retry jobs
	for _, job := range toRetry {
		if err := rs.retryMessage(job); err != nil {
			rs.logger.WithError(err).WithField("message_id", job.MessageID).Error("Failed to retry message")
		}
	}
}

// retryMessage retries a failed message
func (rs *RetryScheduler) retryMessage(job *RetryJob) error {
	return rs.pool.PublishWithConfirm(
		job.OriginalExchange,
		job.OriginalRoutingKey,
		job.Content,
		job.Headers,
	)
}

// CheckMessage checks if a message is a poison message
func (pmd *PoisonMessageDetector) CheckMessage(message interface{}, failureReason string) bool {
	messageHash := generateMessageHash(message)
	
	pmd.mutex.Lock()
	defer pmd.mutex.Unlock()
	
	info, exists := pmd.failureTracking[messageHash]
	if !exists {
		info = &MessageFailureInfo{
			MessageHash:    messageHash,
			FailureCount:   0,
			FirstSeen:      time.Now(),
			FailureReasons: make([]string, 0),
		}
		pmd.failureTracking[messageHash] = info
	}
	
	info.FailureCount++
	info.LastFailure = time.Now()
	info.FailureReasons = append(info.FailureReasons, failureReason)
	
	// Check if threshold exceeded
	if info.FailureCount >= pmd.threshold {
		pmd.logger.WithFields(logrus.Fields{
			"message_hash":   messageHash,
			"failure_count":  info.FailureCount,
			"threshold":      pmd.threshold,
			"failure_reasons": info.FailureReasons,
		}).Warn("Poison message detected")
		
		return true
	}
	
	return false
}

// createExchange helper method to create exchanges
func (dlqm *DeadLetterQueueManager) createExchange(config ExchangeInfo) error {
	ch, err := dlqm.pool.GetChannel()
	if err != nil {
		return err
	}
	defer dlqm.pool.ReturnChannel(ch)
	
	return ch.channel.ExchangeDeclare(
		config.Name,
		config.Type,
		config.Durable,
		config.AutoDelete,
		config.Internal,
		false, // no-wait
		amqp.Table(config.Arguments),
	)
}

// createQueue helper method to create queues
func (dlqm *DeadLetterQueueManager) createQueue(config QueueInfo) error {
	ch, err := dlqm.pool.GetChannel()
	if err != nil {
		return err
	}
	defer dlqm.pool.ReturnChannel(ch)
	
	// Declare queue
	_, err = ch.channel.QueueDeclare(
		config.Name,
		config.Durable,
		config.AutoDelete,
		config.Exclusive,
		false, // no-wait
		amqp.Table(config.Arguments),
	)
	if err != nil {
		return err
	}
	
	// Create bindings
	for _, binding := range config.Bindings {
		err = ch.channel.QueueBind(
			config.Name,
			binding.RoutingKey,
			binding.Exchange,
			false, // no-wait
			amqp.Table(binding.Arguments),
		)
		if err != nil {
			return err
		}
	}
	
	return nil
}

// Helper functions
func generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

func generateMessageHash(message interface{}) string {
	// Simple hash based on message content
	messageBytes, _ := json.Marshal(message)
	return fmt.Sprintf("hash-%x", len(messageBytes))
}