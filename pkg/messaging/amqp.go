package messaging

import (
	"context"
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

	// Create a connection timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use a separate goroutine with the timeout context
	connChan := make(chan struct {
		conn *amqp.Connection
		err  error
	}, 1)

	go func() {
		conn, err := amqp.Dial(c.url)
		connChan <- struct {
			conn *amqp.Connection
			err  error
		}{conn, err}
	}()

	// Wait for connection with timeout
	var conn *amqp.Connection
	var err error
	select {
	case result := <-connChan:
		conn = result.conn
		err = result.err
	case <-ctx.Done():
		return fmt.Errorf("connection to AMQP server timed out after 5 seconds")
	}

	if err != nil {
		return fmt.Errorf("failed to connect to AMQP server: %w", err)
	}
	
	// Store the connection
	c.conn = conn

	// Create channel with timeout
	channelChan := make(chan struct {
		channel *amqp.Channel
		err     error
	}, 1)

	go func() {
		channel, err := conn.Channel()
		channelChan <- struct {
			channel *amqp.Channel
			err     error
		}{channel, err}
	}()

	// Wait for channel creation with timeout
	var channel *amqp.Channel
	select {
	case result := <-channelChan:
		channel = result.channel
		err = result.err
	case <-time.After(3 * time.Second):
		conn.Close()
		return fmt.Errorf("channel creation timed out after 3 seconds")
	}

	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to open AMQP channel: %w", err)
	}
	
	// Store the channel
	c.channel = channel

	// Declare queue with timeout
	queueChan := make(chan struct {
		queue amqp.Queue
		err   error
	}, 1)

	go func() {
		queue, err := channel.QueueDeclare(
			c.queueName,
			true,  // Durable
			false, // Delete when unused
			false, // Exclusive
			false, // No-wait
			nil,   // Arguments
		)
		queueChan <- struct {
			queue amqp.Queue
			err   error
		}{queue, err}
	}()

	// Wait for queue declaration with timeout
	select {
	case result := <-queueChan:
		err = result.err
	case <-time.After(3 * time.Second):
		channel.Close()
		conn.Close()
		return fmt.Errorf("queue declaration timed out after 3 seconds")
	}

	if err != nil {
		channel.Close()
		conn.Close()
		return fmt.Errorf("failed to declare AMQP queue: %w", err)
	}

	// Set up channel Qos to prevent overloading the server
	err = channel.Qos(
		10,    // prefetch count (only handle 10 messages at a time)
		0,     // prefetch size (no specific size limit)
		false, // global (false means apply to just this channel)
	)
	if err != nil {
		c.logger.WithError(err).Warn("Failed to set QoS on AMQP channel, continuing anyway")
	}

	// Set connection status
	c.connected = true
	c.logger.WithFields(logrus.Fields{
		"url":   c.url,
		"queue": c.queueName,
	}).Info("Connected to AMQP server")

	// Create a new stop channel (in case this is a reconnect)
	c.stopChan = make(chan struct{})
	
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
	// Recover from any panics to prevent AMQP issues from crashing the server
	defer func() {
		if r := recover(); r != nil {
			c.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"recover":   r,
			}).Error("Recovered from panic in AMQP PublishTranscription")
		}
	}()

	// Check connection status with timeout for lock acquisition
	connCheckChan := make(chan bool, 1)
	go func() {
		connCheckChan <- c.IsConnected()
	}()

	// Wait up to 100ms for the connection check
	var isConnected bool
	select {
	case isConnected = <-connCheckChan:
	case <-time.After(100 * time.Millisecond):
		return fmt.Errorf("timed out while checking AMQP connection status")
	}

	if !isConnected {
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

	// Create a timeout context for publishing
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Publish message with timeout
	publishChan := make(chan error, 1)
	go func() {
		// Acquire the lock
		c.connMutex.RLock()
		defer c.connMutex.RUnlock()

		// Check if still connected after acquiring the lock
		if !c.connected || c.channel == nil {
			publishChan <- fmt.Errorf("lost AMQP connection before publishing")
			return
		}

		// Try publishing
		err := c.channel.Publish(
			"",          // Exchange
			c.queueName, // Routing key (queue name)
			false,       // Mandatory
			false,       // Immediate
			amqp.Publishing{
				ContentType:  "application/json",
				Body:         bodyBytes,
				DeliveryMode: amqp.Persistent, // Make message persistent
				Timestamp:    time.Now(),
				// Add message expiration to prevent queue buildup in case of consumer issues
				Expiration: "43200000", // 12 hours in milliseconds
			},
		)
		publishChan <- err
	}()

	// Wait for publish with timeout
	select {
	case err := <-publishChan:
		if err != nil {
			return fmt.Errorf("failed to publish transcription to AMQP: %w", err)
		}
	case <-ctx.Done():
		return fmt.Errorf("publishing to AMQP timed out after 200ms")
	}

	// Return success
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
