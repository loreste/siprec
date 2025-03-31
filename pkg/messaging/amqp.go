package messaging

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/streadway/amqp"
)

// AMQPClient handles AMQP connections and message publishing
type AMQPClient struct {
	logger    *logrus.Logger
	url       string
	queueName string
	conn      *amqp.Connection
	channel   *amqp.Channel
	connected bool
	connMutex sync.RWMutex
	stopChan  chan struct{}
}

// NewAMQPClient creates a new AMQP client
func NewAMQPClient(logger *logrus.Logger, url, queueName string) *AMQPClient {
	return &AMQPClient{
		logger:    logger,
		url:       url,
		queueName: queueName,
		stopChan:  make(chan struct{}),
	}
}

// Connect establishes a connection to the AMQP server
func (c *AMQPClient) Connect() error {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	// Check if already connected
	if c.connected {
		return nil
	}

	// Initialize AMQP connection
	if c.url == "" || c.queueName == "" {
		c.logger.Warn("AMQP_URL or AMQP_QUEUE_NAME not set, AMQP functionality will be disabled")
		return fmt.Errorf("AMQP URL or queue name not configured")
	}

	// Establish connection
	var err error
	c.conn, err = amqp.Dial(c.url)
	if err != nil {
		return fmt.Errorf("failed to connect to AMQP server: %w", err)
	}

	// Create channel
	c.channel, err = c.conn.Channel()
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("failed to open AMQP channel: %w", err)
	}

	// Declare queue
	_, err = c.channel.QueueDeclare(
		c.queueName,
		true,  // Durable
		false, // Delete when unused
		false, // Exclusive
		false, // No-wait
		nil,   // Arguments
	)
	if err != nil {
		c.channel.Close()
		c.conn.Close()
		return fmt.Errorf("failed to declare AMQP queue: %w", err)
	}

	// Set connection status
	c.connected = true
	c.logger.WithFields(logrus.Fields{
		"url":   c.url,
		"queue": c.queueName,
	}).Info("Connected to AMQP server")

	// Start monitoring for connection closing
	go c.monitorConnection()

	return nil
}

// Disconnect closes the AMQP connection
func (c *AMQPClient) Disconnect() {
	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if !c.connected {
		return
	}

	// Signal connection monitor to stop
	close(c.stopChan)

	// Close channel and connection
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}

	c.connected = false
	c.logger.Info("Disconnected from AMQP server")
}

// IsConnected returns the connection status
func (c *AMQPClient) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.connected
}

// PublishTranscription publishes a transcription message to the AMQP queue
func (c *AMQPClient) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected to AMQP server")
	}

	// Create message body
	body := map[string]interface{}{
		"call_uuid":     callUUID,
		"transcription": transcription,
		"timestamp":     time.Now().Format(time.RFC3339),
	}

	// Add metadata if provided
	if metadata != nil {
		for k, v := range metadata {
			body[k] = v
		}
	}

	// Marshal to JSON
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal transcription to JSON: %w", err)
	}

	// Publish message
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	err = c.channel.Publish(
		"",          // Exchange
		c.queueName, // Routing key (queue name)
		false,       // Mandatory
		false,       // Immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         bodyBytes,
			DeliveryMode: amqp.Persistent, // Make message persistent
			Timestamp:    time.Now(),
		},
	)
	if err != nil {
		return fmt.Errorf("failed to publish transcription to AMQP: %w", err)
	}

	c.logger.WithField("call_uuid", callUUID).Debug("Successfully published transcription to AMQP")
	return nil
}

// monitorConnection monitors the AMQP connection and attempts to reconnect if it closes
func (c *AMQPClient) monitorConnection() {
	// Set up connection closed notification channel
	closeChan := make(chan *amqp.Error)

	c.connMutex.RLock()
	if c.conn != nil {
		c.conn.NotifyClose(closeChan)
	}
	c.connMutex.RUnlock()

	for {
		select {
		case <-c.stopChan:
			// Shutting down
			return
		case closeErr := <-closeChan:
			c.connMutex.Lock()
			c.connected = false
			c.connMutex.Unlock()

			c.logger.WithError(closeErr).Warn("AMQP connection closed, attempting to reconnect")

			// Attempt to reconnect with backoff
			for attempt := 1; attempt <= 10; attempt++ {
				c.logger.WithField("attempt", attempt).Info("Reconnecting to AMQP server")

				err := c.Connect()
				if err == nil {
					c.logger.Info("Successfully reconnected to AMQP server")
					break
				}

				c.logger.WithError(err).WithField("attempt", attempt).Error("Failed to reconnect to AMQP server")

				// Exponential backoff with max delay of 30 seconds
				backoff := time.Duration(1<<uint(attempt-1)) * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}

				time.Sleep(backoff)
			}
		}
	}
}
