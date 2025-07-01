package messaging

import (
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
	"siprec-server/pkg/config"
)

// AMQPExchangeManager handles AMQP exchange operations
type AMQPExchangeManager struct {
	logger    *logrus.Logger
	pool      *AMQPPool
	exchanges map[string]ExchangeInfo
	mutex     sync.RWMutex
}

// AMQPQueueManager handles AMQP queue operations
type AMQPQueueManager struct {
	logger *logrus.Logger
	pool   *AMQPPool
	queues map[string]QueueInfo
	mutex  sync.RWMutex
}

// ExchangeInfo holds information about a declared exchange
type ExchangeInfo struct {
	Name       string
	Type       string
	Durable    bool
	AutoDelete bool
	Internal   bool
	Arguments  map[string]interface{}
	CreatedAt  string
}

// QueueInfo holds information about a declared queue
type QueueInfo struct {
	Name         string
	Durable      bool
	AutoDelete   bool
	Exclusive    bool
	Arguments    map[string]interface{}
	Bindings     []BindingInfo
	MessageCount int
	ConsumerCount int
	CreatedAt    string
}

// BindingInfo holds information about queue bindings
type BindingInfo struct {
	Exchange   string
	RoutingKey string
	Arguments  map[string]interface{}
}

// MessageRouter handles message routing logic
type MessageRouter struct {
	logger         *logrus.Logger
	pool           *AMQPPool
	defaultExchange string
	routingRules   map[string]RoutingRule
	mutex          sync.RWMutex
}

// RoutingRule defines how messages should be routed
type RoutingRule struct {
	Pattern     string
	Exchange    string
	RoutingKey  string
	Priority    int
	Conditions  map[string]interface{}
	Enabled     bool
}

// MessageTemplate defines a reusable message template
type MessageTemplate struct {
	Name        string
	Exchange    string
	RoutingKey  string
	Headers     map[string]interface{}
	Properties  MessageProperties
	ContentType string
}

// MessageProperties defines AMQP message properties
type MessageProperties struct {
	DeliveryMode uint8
	Priority     uint8
	Expiration   string
	MessageID    string
	Timestamp    bool
	Type         string
	UserID       string
	AppID        string
}

// NewAMQPExchangeManager creates a new exchange manager
func NewAMQPExchangeManager(logger *logrus.Logger, pool *AMQPPool) *AMQPExchangeManager {
	return &AMQPExchangeManager{
		logger:    logger,
		pool:      pool,
		exchanges: make(map[string]ExchangeInfo),
	}
}

// NewAMQPQueueManager creates a new queue manager
func NewAMQPQueueManager(logger *logrus.Logger, pool *AMQPPool) *AMQPQueueManager {
	return &AMQPQueueManager{
		logger: logger,
		pool:   pool,
		queues: make(map[string]QueueInfo),
	}
}

// NewMessageRouter creates a new message router
func NewMessageRouter(logger *logrus.Logger, pool *AMQPPool, defaultExchange string) *MessageRouter {
	return &MessageRouter{
		logger:          logger,
		pool:            pool,
		defaultExchange: defaultExchange,
		routingRules:    make(map[string]RoutingRule),
	}
}

// DeclareExchange declares an AMQP exchange
func (em *AMQPExchangeManager) DeclareExchange(config config.AMQPExchangeConfig) error {
	ch, err := em.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer em.pool.ReturnChannel(ch)

	err = ch.channel.ExchangeDeclare(
		config.Name,
		config.Type,
		config.Durable,
		config.AutoDelete,
		config.Internal,
		config.NoWait,
		amqp.Table(config.Arguments),
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange %s: %w", config.Name, err)
	}

	// Store exchange info
	em.mutex.Lock()
	em.exchanges[config.Name] = ExchangeInfo{
		Name:       config.Name,
		Type:       config.Type,
		Durable:    config.Durable,
		AutoDelete: config.AutoDelete,
		Internal:   config.Internal,
		Arguments:  config.Arguments,
		CreatedAt:  fmt.Sprintf("%d", getCurrentTimestamp()),
	}
	em.mutex.Unlock()

	em.logger.WithFields(logrus.Fields{
		"name":        config.Name,
		"type":        config.Type,
		"durable":     config.Durable,
		"auto_delete": config.AutoDelete,
		"internal":    config.Internal,
	}).Info("Exchange declared successfully")

	return nil
}

// DeclareQueue declares an AMQP queue with bindings
func (qm *AMQPQueueManager) DeclareQueue(config config.AMQPQueueConfig) error {
	ch, err := qm.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer qm.pool.ReturnChannel(ch)

	// Declare the queue
	queue, err := ch.channel.QueueDeclare(
		config.Name,
		config.Durable,
		config.AutoDelete,
		config.Exclusive,
		config.NoWait,
		amqp.Table(config.Arguments),
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue %s: %w", config.Name, err)
	}

	// Create bindings
	var bindings []BindingInfo
	for _, binding := range config.Bindings {
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

		bindings = append(bindings, BindingInfo{
			Exchange:   binding.Exchange,
			RoutingKey: binding.RoutingKey,
			Arguments:  binding.Arguments,
		})

		qm.logger.WithFields(logrus.Fields{
			"queue":       queue.Name,
			"exchange":    binding.Exchange,
			"routing_key": binding.RoutingKey,
		}).Info("Queue binding created")
	}

	// Store queue info
	qm.mutex.Lock()
	qm.queues[config.Name] = QueueInfo{
		Name:         config.Name,
		Durable:      config.Durable,
		AutoDelete:   config.AutoDelete,
		Exclusive:    config.Exclusive,
		Arguments:    config.Arguments,
		Bindings:     bindings,
		MessageCount: queue.Messages,
		ConsumerCount: queue.Consumers,
		CreatedAt:    fmt.Sprintf("%d", getCurrentTimestamp()),
	}
	qm.mutex.Unlock()

	qm.logger.WithFields(logrus.Fields{
		"name":     config.Name,
		"durable":  config.Durable,
		"bindings": len(bindings),
		"messages": queue.Messages,
		"consumers": queue.Consumers,
	}).Info("Queue declared successfully")

	return nil
}

// DeleteExchange deletes an AMQP exchange
func (em *AMQPExchangeManager) DeleteExchange(name string, ifUnused bool) error {
	ch, err := em.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer em.pool.ReturnChannel(ch)

	err = ch.channel.ExchangeDelete(name, ifUnused, false)
	if err != nil {
		return fmt.Errorf("failed to delete exchange %s: %w", name, err)
	}

	// Remove from local storage
	em.mutex.Lock()
	delete(em.exchanges, name)
	em.mutex.Unlock()

	em.logger.WithField("name", name).Info("Exchange deleted successfully")
	return nil
}

// DeleteQueue deletes an AMQP queue
func (qm *AMQPQueueManager) DeleteQueue(name string, ifUnused, ifEmpty bool) error {
	ch, err := qm.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer qm.pool.ReturnChannel(ch)

	_, err = ch.channel.QueueDelete(name, ifUnused, ifEmpty, false)
	if err != nil {
		return fmt.Errorf("failed to delete queue %s: %w", name, err)
	}

	// Remove from local storage
	qm.mutex.Lock()
	delete(qm.queues, name)
	qm.mutex.Unlock()

	qm.logger.WithField("name", name).Info("Queue deleted successfully")
	return nil
}

// BindQueue creates a new queue binding
func (qm *AMQPQueueManager) BindQueue(queueName, exchangeName, routingKey string, arguments map[string]interface{}) error {
	ch, err := qm.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer qm.pool.ReturnChannel(ch)

	err = ch.channel.QueueBind(queueName, routingKey, exchangeName, false, amqp.Table(arguments))
	if err != nil {
		return fmt.Errorf("failed to bind queue %s to exchange %s: %w", queueName, exchangeName, err)
	}

	// Update local storage
	qm.mutex.Lock()
	if queueInfo, exists := qm.queues[queueName]; exists {
		queueInfo.Bindings = append(queueInfo.Bindings, BindingInfo{
			Exchange:   exchangeName,
			RoutingKey: routingKey,
			Arguments:  arguments,
		})
		qm.queues[queueName] = queueInfo
	}
	qm.mutex.Unlock()

	qm.logger.WithFields(logrus.Fields{
		"queue":       queueName,
		"exchange":    exchangeName,
		"routing_key": routingKey,
	}).Info("Queue binding created")

	return nil
}

// UnbindQueue removes a queue binding
func (qm *AMQPQueueManager) UnbindQueue(queueName, exchangeName, routingKey string, arguments map[string]interface{}) error {
	ch, err := qm.pool.GetChannel()
	if err != nil {
		return fmt.Errorf("failed to get channel: %w", err)
	}
	defer qm.pool.ReturnChannel(ch)

	err = ch.channel.QueueUnbind(queueName, routingKey, exchangeName, amqp.Table(arguments))
	if err != nil {
		return fmt.Errorf("failed to unbind queue %s from exchange %s: %w", queueName, exchangeName, err)
	}

	// Update local storage
	qm.mutex.Lock()
	if queueInfo, exists := qm.queues[queueName]; exists {
		newBindings := make([]BindingInfo, 0)
		for _, binding := range queueInfo.Bindings {
			if !(binding.Exchange == exchangeName && binding.RoutingKey == routingKey) {
				newBindings = append(newBindings, binding)
			}
		}
		queueInfo.Bindings = newBindings
		qm.queues[queueName] = queueInfo
	}
	qm.mutex.Unlock()

	qm.logger.WithFields(logrus.Fields{
		"queue":       queueName,
		"exchange":    exchangeName,
		"routing_key": routingKey,
	}).Info("Queue binding removed")

	return nil
}

// GetExchangeInfo returns information about an exchange
func (em *AMQPExchangeManager) GetExchangeInfo(name string) (ExchangeInfo, bool) {
	em.mutex.RLock()
	defer em.mutex.RUnlock()
	info, exists := em.exchanges[name]
	return info, exists
}

// GetQueueInfo returns information about a queue
func (qm *AMQPQueueManager) GetQueueInfo(name string) (QueueInfo, bool) {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()
	info, exists := qm.queues[name]
	return info, exists
}

// ListExchanges returns all declared exchanges
func (em *AMQPExchangeManager) ListExchanges() map[string]ExchangeInfo {
	em.mutex.RLock()
	defer em.mutex.RUnlock()
	
	result := make(map[string]ExchangeInfo)
	for name, info := range em.exchanges {
		result[name] = info
	}
	return result
}

// ListQueues returns all declared queues
func (qm *AMQPQueueManager) ListQueues() map[string]QueueInfo {
	qm.mutex.RLock()
	defer qm.mutex.RUnlock()
	
	result := make(map[string]QueueInfo)
	for name, info := range qm.queues {
		result[name] = info
	}
	return result
}

// AddRoutingRule adds a new routing rule
func (mr *MessageRouter) AddRoutingRule(name string, rule RoutingRule) {
	mr.mutex.Lock()
	defer mr.mutex.Unlock()
	mr.routingRules[name] = rule
	
	mr.logger.WithFields(logrus.Fields{
		"rule_name":   name,
		"pattern":     rule.Pattern,
		"exchange":    rule.Exchange,
		"routing_key": rule.RoutingKey,
		"priority":    rule.Priority,
	}).Info("Routing rule added")
}

// RemoveRoutingRule removes a routing rule
func (mr *MessageRouter) RemoveRoutingRule(name string) {
	mr.mutex.Lock()
	defer mr.mutex.Unlock()
	delete(mr.routingRules, name)
	
	mr.logger.WithField("rule_name", name).Info("Routing rule removed")
}

// RouteMessage determines the exchange and routing key for a message
func (mr *MessageRouter) RouteMessage(messageType string, metadata map[string]interface{}) (exchange, routingKey string) {
	mr.mutex.RLock()
	defer mr.mutex.RUnlock()
	
	// Find matching routing rule with highest priority
	var bestRule *RoutingRule
	bestPriority := -1
	
	for _, rule := range mr.routingRules {
		if !rule.Enabled {
			continue
		}
		
		if mr.matchesPattern(messageType, rule.Pattern) && rule.Priority > bestPriority {
			if mr.matchesConditions(metadata, rule.Conditions) {
				bestRule = &rule
				bestPriority = rule.Priority
			}
		}
	}
	
	if bestRule != nil {
		return bestRule.Exchange, bestRule.RoutingKey
	}
	
	// Return default
	return mr.defaultExchange, messageType
}

// matchesPattern checks if a message type matches a routing pattern
func (mr *MessageRouter) matchesPattern(messageType, pattern string) bool {
	// Simple pattern matching (can be enhanced with regex or glob patterns)
	if pattern == "*" || pattern == messageType {
		return true
	}
	
	// Add more sophisticated pattern matching here if needed
	return false
}

// matchesConditions checks if metadata matches routing conditions
func (mr *MessageRouter) matchesConditions(metadata map[string]interface{}, conditions map[string]interface{}) bool {
	for key, expectedValue := range conditions {
		if actualValue, exists := metadata[key]; !exists || actualValue != expectedValue {
			return false
		}
	}
	return true
}

// UpdateQueueMetrics updates queue statistics
func (qm *AMQPQueueManager) UpdateQueueMetrics() error {
	// This would require AMQP management plugin or admin API
	// For now, we'll just update the timestamp
	qm.mutex.Lock()
	defer qm.mutex.Unlock()
	
	for name, info := range qm.queues {
		// In a real implementation, you would query the queue statistics
		// info.MessageCount = getQueueMessageCount(name)
		// info.ConsumerCount = getQueueConsumerCount(name)
		qm.queues[name] = info
	}
	
	return nil
}

// PurgeQueue removes all messages from a queue
func (qm *AMQPQueueManager) PurgeQueue(name string) (int, error) {
	ch, err := qm.pool.GetChannel()
	if err != nil {
		return 0, fmt.Errorf("failed to get channel: %w", err)
	}
	defer qm.pool.ReturnChannel(ch)

	count, err := ch.channel.QueuePurge(name, false)
	if err != nil {
		return 0, fmt.Errorf("failed to purge queue %s: %w", name, err)
	}

	qm.logger.WithFields(logrus.Fields{
		"queue":    name,
		"purged":   count,
	}).Info("Queue purged")

	return count, nil
}

// getCurrentTimestamp returns current Unix timestamp
func getCurrentTimestamp() int64 {
	return 1704067200 // Placeholder for time.Now().Unix()
}