package messaging

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"siprec-server/pkg/config"
)

// EnhancedAMQPClient provides advanced AMQP functionality with connection pooling
type EnhancedAMQPClient struct {
	logger           *logrus.Logger
	config           *config.AMQPConfig
	pool             *AMQPPool
	// exchangeManager  *ExchangeManager
	// queueManager     *QueueManager
	deadLetterQueue  *DeadLetterQueue
	retryManager     *RetryManager
	circuitBreaker   *AMQPCircuitBreaker
	connected        bool
	mutex            sync.RWMutex
	shutdownChan     chan struct{}
	shutdownOnce     sync.Once
	metricsCollector *MetricsCollector
}

// ExchangeManager manages AMQP exchanges
type ExchangeManager struct {
	client    *EnhancedAMQPClient
	exchanges map[string]config.AMQPExchangeConfig
	mutex     sync.RWMutex
}

// QueueManager manages AMQP queues
type QueueManager struct {
	client *EnhancedAMQPClient
	queues map[string]config.AMQPQueueConfig
	mutex  sync.RWMutex
}

// DeadLetterQueue manages dead letter queue functionality
type DeadLetterQueue struct {
	client       *EnhancedAMQPClient
	exchangeName string
	queueName    string
	routingKey   string
	initialized  bool
	mutex        sync.Mutex
}

// RetryManager handles message retry logic
type RetryManager struct {
	client     *EnhancedAMQPClient
	maxRetries int
	retryDelay time.Duration
}

// AMQPCircuitBreaker wraps the existing circuit breaker for AMQP operations
type AMQPCircuitBreaker struct {
	*CircuitBreaker
}

// GetStatus returns the circuit breaker status for metrics
func (acb *AMQPCircuitBreaker) GetStatus() map[string]interface{} {
	acb.mutex.RLock()
	defer acb.mutex.RUnlock()
	
	return map[string]interface{}{
		"state":                acb.state.String(),
		"consecutive_failures": acb.consecutiveFailures,
		"last_failure_time":    acb.lastFailureTime,
		"last_success_time":    acb.lastSuccessTime,
	}
}

// MetricsCollector collects detailed AMQP metrics
type MetricsCollector struct {
	client                  *EnhancedAMQPClient
	exchangeMetrics         map[string]*ExchangeMetrics
	queueMetrics           map[string]*QueueMetrics
	mutex                  sync.RWMutex
	collectInterval        time.Duration
	stopChan               chan struct{}
}

// ExchangeMetrics holds metrics for an exchange
type ExchangeMetrics struct {
	Name              string
	PublishedMessages int64
	FailedPublishes   int64
	LastPublish       time.Time
}

// QueueMetrics holds metrics for a queue
type QueueMetrics struct {
	Name          string
	MessageCount  int
	ConsumerCount int
	LastUpdate    time.Time
}

// NewEnhancedAMQPClient creates a new enhanced AMQP client
func NewEnhancedAMQPClient(logger *logrus.Logger, config *config.AMQPConfig) *EnhancedAMQPClient {
	client := &EnhancedAMQPClient{
		logger:       logger,
		config:       config,
		shutdownChan: make(chan struct{}),
		circuitBreaker: &AMQPCircuitBreaker{
			CircuitBreaker: NewCircuitBreaker(logger, DefaultCircuitBreakerConfig()),
		},
	}
	
	// Initialize connection pool
	client.pool = NewAMQPPool(logger, config)
	
	// Initialize managers (temporarily disabled due to type conflicts)
	// client.exchangeManager = &ExchangeManager{
	//	client:    client,
	//	exchanges: make(map[string]config.AMQPExchangeConfig),
	// }
	
	// client.queueManager = &QueueManager{
	//	client: client,
	//	queues: make(map[string]config.AMQPQueueConfig),
	// }
	
	client.deadLetterQueue = &DeadLetterQueue{
		client:       client,
		exchangeName: config.DeadLetterExchange,
		queueName:    config.DeadLetterExchange + ".queue",
		routingKey:   config.DeadLetterRoutingKey,
	}
	
	client.retryManager = &RetryManager{
		client:     client,
		maxRetries: config.MaxRetries,
		retryDelay: config.RetryDelay,
	}
	
	// Initialize metrics collector if enabled
	if config.EnableMetrics {
		client.metricsCollector = &MetricsCollector{
			client:          client,
			exchangeMetrics: make(map[string]*ExchangeMetrics),
			queueMetrics:    make(map[string]*QueueMetrics),
			collectInterval: config.MetricsInterval,
			stopChan:        make(chan struct{}),
		}
	}
	
	return client
}

// Connect establishes the enhanced AMQP connection
func (c *EnhancedAMQPClient) Connect() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	if c.connected {
		return nil
	}
	
	c.logger.Info("Connecting enhanced AMQP client")
	
	// Initialize connection pool
	if err := c.pool.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize connection pool: %w", err)
	}
	
	// Set up exchanges (temporarily disabled)
	// if err := c.setupExchanges(); err != nil {
	//	return fmt.Errorf("failed to setup exchanges: %w", err)
	// }
	
	// Set up queues (temporarily disabled)
	// if err := c.setupQueues(); err != nil {
	//	return fmt.Errorf("failed to setup queues: %w", err)
	// }
	
	// Initialize dead letter queue
	if err := c.initializeDeadLetterQueue(); err != nil {
		c.logger.WithError(err).Warn("Failed to initialize dead letter queue")
	}
	
	// Start metrics collection
	if c.metricsCollector != nil {
		go c.metricsCollector.start()
	}
	
	c.connected = true
	c.logger.Info("Enhanced AMQP client connected successfully")
	
	return nil
}

// setupExchanges creates and configures exchanges
func (c *EnhancedAMQPClient) setupExchanges() error {
	for _, exchangeConfig := range c.config.Exchanges {
		if err := c.DeclareExchange(exchangeConfig); err != nil {
			return fmt.Errorf("failed to declare exchange %s: %w", exchangeConfig.Name, err)
		}
	}
	return nil
}

// setupQueues creates and configures queues
func (c *EnhancedAMQPClient) setupQueues() error {
	for _, queueConfig := range c.config.Queues {
		if err := c.DeclareQueue(queueConfig); err != nil {
			return fmt.Errorf("failed to declare queue %s: %w", queueConfig.Name, err)
		}
	}
	return nil
}

// DeclareExchange declares an AMQP exchange
func (c *EnhancedAMQPClient) DeclareExchange(exchangeConfig config.AMQPExchangeConfig) error {
	ch, err := c.pool.GetChannel()
	if err != nil {
		return err
	}
	defer c.pool.ReturnChannel(ch)
	
	err = ch.channel.ExchangeDeclare(
		exchangeConfig.Name,
		exchangeConfig.Type,
		exchangeConfig.Durable,
		exchangeConfig.AutoDelete,
		exchangeConfig.Internal,
		exchangeConfig.NoWait,
		amqp.Table(exchangeConfig.Arguments),
	)
	
	if err != nil {
		return err
	}
	
	// c.exchangeManager.mutex.Lock()
	// c.exchangeManager.exchanges[exchangeConfig.Name] = exchangeConfig
	// c.exchangeManager.mutex.Unlock()
	
	// Initialize metrics for this exchange
	if c.metricsCollector != nil {
		c.metricsCollector.mutex.Lock()
		c.metricsCollector.exchangeMetrics[exchangeConfig.Name] = &ExchangeMetrics{
			Name: exchangeConfig.Name,
		}
		c.metricsCollector.mutex.Unlock()
	}
	
	c.logger.WithFields(logrus.Fields{
		"name":    exchangeConfig.Name,
		"type":    exchangeConfig.Type,
		"durable": exchangeConfig.Durable,
	}).Info("Exchange declared")
	
	return nil
}

// DeclareQueue declares an AMQP queue with bindings
func (c *EnhancedAMQPClient) DeclareQueue(queueConfig config.AMQPQueueConfig) error {
	ch, err := c.pool.GetChannel()
	if err != nil {
		return err
	}
	defer c.pool.ReturnChannel(ch)
	
	// Declare the queue
	queue, err := ch.channel.QueueDeclare(
		queueConfig.Name,
		queueConfig.Durable,
		queueConfig.AutoDelete,
		queueConfig.Exclusive,
		queueConfig.NoWait,
		amqp.Table(queueConfig.Arguments),
	)
	
	if err != nil {
		return err
	}
	
	// Create bindings
	for _, binding := range queueConfig.Bindings {
		err = ch.channel.QueueBind(
			queue.Name,
			binding.RoutingKey,
			binding.Exchange,
			binding.NoWait,
			amqp.Table(binding.Arguments),
		)
		
		if err != nil {
			return fmt.Errorf("failed to bind queue %s to exchange %s: %w", 
				queue.Name, binding.Exchange, err)
		}
		
		c.logger.WithFields(logrus.Fields{
			"queue":       queue.Name,
			"exchange":    binding.Exchange,
			"routing_key": binding.RoutingKey,
		}).Info("Queue binding created")
	}
	
	// c.queueManager.mutex.Lock()
	// c.queueManager.queues[queueConfig.Name] = queueConfig
	// c.queueManager.mutex.Unlock()
	
	// Initialize metrics for this queue
	if c.metricsCollector != nil {
		c.metricsCollector.mutex.Lock()
		c.metricsCollector.queueMetrics[queueConfig.Name] = &QueueMetrics{
			Name: queueConfig.Name,
		}
		c.metricsCollector.mutex.Unlock()
	}
	
	c.logger.WithFields(logrus.Fields{
		"name":     queueConfig.Name,
		"durable":  queueConfig.Durable,
		"bindings": len(queueConfig.Bindings),
	}).Info("Queue declared")
	
	return nil
}

// PublishMessage publishes a message with retry and circuit breaker logic
func (c *EnhancedAMQPClient) PublishMessage(exchange, routingKey string, message interface{}, headers map[string]interface{}) error {
	// Check circuit breaker
	if !c.circuitBreaker.canExecute() {
		return fmt.Errorf("circuit breaker is open")
	}
	
	messageBytes, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	
	amqpHeaders := make(amqp.Table)
	for k, v := range headers {
		amqpHeaders[k] = v
	}
	
	// Use default exchange if not specified
	if exchange == "" {
		exchange = c.config.DefaultExchange
	}
	
	// Use default routing key if not specified
	if routingKey == "" {
		routingKey = c.config.DefaultRoutingKey
	}
	
	// Publish with retry logic
	err = c.retryManager.executeWithRetry(func() error {
		return c.pool.PublishWithConfirm(exchange, routingKey, messageBytes, amqpHeaders)
	})
	
	if err != nil {
		c.circuitBreaker.recordResult(false, 0)
		
		// Send to dead letter queue on max retries exceeded
		if c.deadLetterQueue.initialized {
			dlqErr := c.sendToDeadLetterQueue(message, headers, err.Error())
			if dlqErr != nil {
				c.logger.WithError(dlqErr).Error("Failed to send message to dead letter queue")
			}
		}
		
		return err
	}
	
	c.circuitBreaker.recordResult(true, 0)
	
	// Update exchange metrics
	if c.metricsCollector != nil {
		c.metricsCollector.updateExchangeMetrics(exchange, true)
	}
	
	return nil
}

// PublishTranscription publishes a transcription message (backward compatibility)
func (c *EnhancedAMQPClient) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	message := AMQPMessage{
		CallUUID:      callUUID,
		Transcription: transcription,
		Timestamp:     time.Now(),
		Metadata:      metadata,
		DeadLetter:    false,
	}
	
	headers := map[string]interface{}{
		"message_type": "transcription",
		"call_uuid":    callUUID,
		"timestamp":    time.Now().Unix(),
	}
	
	return c.PublishMessage("", "", message, headers)
}

// initializeDeadLetterQueue sets up the dead letter queue
func (c *EnhancedAMQPClient) initializeDeadLetterQueue() error {
	c.deadLetterQueue.mutex.Lock()
	defer c.deadLetterQueue.mutex.Unlock()
	
	if c.deadLetterQueue.initialized {
		return nil
	}
	
	ch, err := c.pool.GetChannel()
	if err != nil {
		return err
	}
	defer c.pool.ReturnChannel(ch)
	
	// Declare dead letter exchange
	err = ch.channel.ExchangeDeclare(
		c.deadLetterQueue.exchangeName,
		"direct",
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare dead letter exchange: %w", err)
	}
	
	// Declare dead letter queue
	_, err = ch.channel.QueueDeclare(
		c.deadLetterQueue.queueName,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare dead letter queue: %w", err)
	}
	
	// Bind dead letter queue to exchange
	err = ch.channel.QueueBind(
		c.deadLetterQueue.queueName,
		c.deadLetterQueue.routingKey,
		c.deadLetterQueue.exchangeName,
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind dead letter queue: %w", err)
	}
	
	c.deadLetterQueue.initialized = true
	
	c.logger.WithFields(logrus.Fields{
		"exchange": c.deadLetterQueue.exchangeName,
		"queue":    c.deadLetterQueue.queueName,
	}).Info("Dead letter queue initialized")
	
	return nil
}

// sendToDeadLetterQueue sends a failed message to the dead letter queue
func (c *EnhancedAMQPClient) sendToDeadLetterQueue(originalMessage interface{}, headers map[string]interface{}, reason string) error {
	if !c.deadLetterQueue.initialized {
		return fmt.Errorf("dead letter queue not initialized")
	}
	
	deadLetterMessage := map[string]interface{}{
		"original_message": originalMessage,
		"failure_reason":   reason,
		"failed_at":        time.Now(),
		"original_headers": headers,
	}
	
	dlqHeaders := map[string]interface{}{
		"x-death-reason": reason,
		"x-failed-at":    time.Now().Unix(),
	}
	
	messageBytes, err := json.Marshal(deadLetterMessage)
	if err != nil {
		return fmt.Errorf("failed to marshal dead letter message: %w", err)
	}
	
	amqpHeaders := make(amqp.Table)
	for k, v := range dlqHeaders {
		amqpHeaders[k] = v
	}
	
	return c.pool.PublishWithConfirm(
		c.deadLetterQueue.exchangeName,
		c.deadLetterQueue.routingKey,
		messageBytes,
		amqpHeaders,
	)
}

// IsConnected returns the connection status
func (c *EnhancedAMQPClient) IsConnected() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.connected
}

// GetExchangeManager returns the exchange manager (temporarily disabled)
// func (c *EnhancedAMQPClient) GetExchangeManager() *ExchangeManager {
//	return c.exchangeManager
// }

// GetQueueManager returns the queue manager (temporarily disabled)
// func (c *EnhancedAMQPClient) GetQueueManager() *QueueManager {
//	return c.queueManager
// }

// GetMetrics returns comprehensive metrics
func (c *EnhancedAMQPClient) GetMetrics() map[string]interface{} {
	poolMetrics := c.pool.GetMetrics()
	
	metrics := map[string]interface{}{
		"pool": map[string]interface{}{
			"total_connections":  poolMetrics.TotalConnections,
			"active_connections": poolMetrics.ActiveConnections,
			"total_channels":     poolMetrics.TotalChannels,
			"active_channels":    poolMetrics.ActiveChannels,
			"published_messages": poolMetrics.PublishedMessages,
			"failed_publishes":   poolMetrics.FailedPublishes,
			"reconnect_attempts": poolMetrics.ReconnectAttempts,
		},
		"circuit_breaker": c.circuitBreaker.GetStatus(),
	}
	
	if c.metricsCollector != nil {
		metrics["exchanges"] = c.metricsCollector.getExchangeMetrics()
		metrics["queues"] = c.metricsCollector.getQueueMetrics()
	}
	
	return metrics
}

// Disconnect gracefully disconnects the client
func (c *EnhancedAMQPClient) Disconnect() {
	c.shutdownOnce.Do(func() {
		c.mutex.Lock()
		c.connected = false
		c.mutex.Unlock()
		
		close(c.shutdownChan)
		
		if c.metricsCollector != nil {
			close(c.metricsCollector.stopChan)
		}
		
		if c.pool != nil {
			c.pool.Shutdown()
		}
		
		c.logger.Info("Enhanced AMQP client disconnected")
	})
}

// executeWithRetry executes a function with retry logic
func (rm *RetryManager) executeWithRetry(fn func() error) error {
	var lastErr error
	
	for attempt := 0; attempt <= rm.maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		if attempt < rm.maxRetries {
			rm.client.logger.WithFields(logrus.Fields{
				"attempt": attempt + 1,
				"max":     rm.maxRetries,
				"error":   err,
			}).Warn("Operation failed, retrying")
			
			time.Sleep(rm.retryDelay)
		}
	}
	
	return fmt.Errorf("operation failed after %d retries: %w", rm.maxRetries, lastErr)
}


// Metrics collector methods
func (mc *MetricsCollector) start() {
	ticker := time.NewTicker(mc.collectInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-mc.stopChan:
			return
		case <-ticker.C:
			mc.collectMetrics()
		}
	}
}

func (mc *MetricsCollector) collectMetrics() {
	// Update queue metrics
	mc.updateQueueMetrics()
}

func (mc *MetricsCollector) updateExchangeMetrics(exchangeName string, success bool) {
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	metrics, exists := mc.exchangeMetrics[exchangeName]
	if !exists {
		metrics = &ExchangeMetrics{Name: exchangeName}
		mc.exchangeMetrics[exchangeName] = metrics
	}
	
	if success {
		metrics.PublishedMessages++
	} else {
		metrics.FailedPublishes++
	}
	metrics.LastPublish = time.Now()
}

func (mc *MetricsCollector) updateQueueMetrics() {
	// This would require additional AMQP management API calls
	// For now, we'll just update the timestamp
	mc.mutex.Lock()
	defer mc.mutex.Unlock()
	
	for _, metrics := range mc.queueMetrics {
		metrics.LastUpdate = time.Now()
	}
}

func (mc *MetricsCollector) getExchangeMetrics() map[string]interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	
	result := make(map[string]interface{})
	for name, metrics := range mc.exchangeMetrics {
		result[name] = map[string]interface{}{
			"published_messages": metrics.PublishedMessages,
			"failed_publishes":   metrics.FailedPublishes,
			"last_publish":       metrics.LastPublish,
		}
	}
	return result
}

func (mc *MetricsCollector) getQueueMetrics() map[string]interface{} {
	mc.mutex.RLock()
	defer mc.mutex.RUnlock()
	
	result := make(map[string]interface{})
	for name, metrics := range mc.queueMetrics {
		result[name] = map[string]interface{}{
			"message_count":  metrics.MessageCount,
			"consumer_count": metrics.ConsumerCount,
			"last_update":    metrics.LastUpdate,
		}
	}
	return result
}