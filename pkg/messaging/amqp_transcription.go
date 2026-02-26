package messaging

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/telemetry/tracing"
)

const (
	// Worker pool size for AMQP publishing
	amqpWorkerPoolSize = 32
	// Channel buffer size for non-blocking publishing
	amqpPublishChannelSize = 5000
	// Publish timeout
	amqpPublishTimeout = 500 * time.Millisecond
	// Retry configuration
	amqpMaxRetries       = 3
	amqpBaseRetryDelay   = 100 * time.Millisecond
	amqpMaxRetryDelay    = 2 * time.Second
	// Health check interval
	amqpHealthCheckInterval = 30 * time.Second
)

// transcriptionMessage represents a transcription to publish
type transcriptionMessage struct {
	callUUID      string
	transcription string
	isFinal       bool
	metadata      map[string]interface{}
	timestamp     time.Time
}

// AMQPTranscriptionListener implements the TranscriptionListener interface
// for sending transcriptions to an AMQP message queue
// Optimized for high concurrency with worker pools and buffered channels
type AMQPTranscriptionListener struct {
	logger logrus.FieldLogger
	client AMQPClientInterface

	// Async publishing with worker pool
	publishChan chan *transcriptionMessage
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// Object pool for messages to reduce GC pressure
	messagePool sync.Pool

	// Metrics - atomic for lock-free updates
	totalPublished    int64
	totalFailed       int64
	totalDropped      int64
	totalTimeouts     int64
	totalRetries      int64
	channelHighWater  int64
	lastConnectedTime int64 // Unix timestamp of last successful connection
	lastErrorTime     int64 // Unix timestamp of last error
	consecutiveErrors int64 // Count of consecutive connection errors

	// Health check
	healthCheckStop chan struct{}
}

// NewAMQPTranscriptionListener creates a new AMQP transcription listener optimized for high concurrency
func NewAMQPTranscriptionListener(logger logrus.FieldLogger, client AMQPClientInterface) *AMQPTranscriptionListener {
	l := &AMQPTranscriptionListener{
		logger:          logger,
		client:          client,
		publishChan:     make(chan *transcriptionMessage, amqpPublishChannelSize),
		stopChan:        make(chan struct{}),
		healthCheckStop: make(chan struct{}),
		messagePool: sync.Pool{
			New: func() interface{} {
				return &transcriptionMessage{
					metadata: make(map[string]interface{}, 8),
				}
			},
		},
	}

	// Start worker pool
	for i := 0; i < amqpWorkerPoolSize; i++ {
		l.wg.Add(1)
		go l.publishWorker(i)
	}

	// Start health check goroutine
	go l.healthCheckLoop()

	logger.WithFields(logrus.Fields{
		"workers":     amqpWorkerPoolSize,
		"buffer_size": amqpPublishChannelSize,
		"max_retries": amqpMaxRetries,
	}).Info("AMQP transcription listener initialized with worker pool")

	return l
}

// publishWorker processes messages from the channel
func (l *AMQPTranscriptionListener) publishWorker(id int) {
	defer l.wg.Done()

	for {
		select {
		case <-l.stopChan:
			return
		case msg := <-l.publishChan:
			if msg != nil {
				l.processMessage(msg)
				// Return message to pool
				msg.callUUID = ""
				msg.transcription = ""
				msg.isFinal = false
				for k := range msg.metadata {
					delete(msg.metadata, k)
				}
				l.messagePool.Put(msg)
			}
		}
	}
}

// processMessage handles a single message publish with retry logic
func (l *AMQPTranscriptionListener) processMessage(msg *transcriptionMessage) {
	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"panic":     r,
			}).Error("Recovered from panic in AMQP publish worker")
		}
	}()

	// Check connection status with detailed logging
	if l.client == nil {
		atomic.AddInt64(&l.totalFailed, 1)
		atomic.AddInt64(&l.consecutiveErrors, 1)
		atomic.StoreInt64(&l.lastErrorTime, time.Now().Unix())
		l.logger.WithFields(logrus.Fields{
			"call_uuid":          msg.callUUID,
			"consecutive_errors": atomic.LoadInt64(&l.consecutiveErrors),
		}).Error("AMQP client is nil - transcription cannot be published")
		return
	}

	if !l.client.IsConnected() {
		// Attempt retry with backoff for disconnected state
		if l.retryWithBackoff(msg) {
			return // Success after retry
		}
		atomic.AddInt64(&l.totalFailed, 1)
		atomic.AddInt64(&l.consecutiveErrors, 1)
		atomic.StoreInt64(&l.lastErrorTime, time.Now().Unix())
		l.logger.WithFields(logrus.Fields{
			"call_uuid":          msg.callUUID,
			"is_final":           msg.isFinal,
			"consecutive_errors": atomic.LoadInt64(&l.consecutiveErrors),
			"message_age_ms":     time.Since(msg.timestamp).Milliseconds(),
		}).Error("AMQP client disconnected - transcription will be lost after retries exhausted")
		return
	}

	callCtx := tracing.ContextForCall(msg.callUUID)
	_, publishSpan := tracing.StartSpan(callCtx, "amqp.publish.transcription", trace.WithAttributes(
		attribute.String("call.id", msg.callUUID),
		attribute.Bool("transcription.final", msg.isFinal),
	), trace.WithSpanKind(trace.SpanKindProducer))
	defer publishSpan.End()

	// Attempt publish with retries
	var lastErr error
	for attempt := 0; attempt <= amqpMaxRetries; attempt++ {
		if attempt > 0 {
			atomic.AddInt64(&l.totalRetries, 1)
			delay := l.calculateBackoff(attempt)
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"attempt":   attempt,
				"delay_ms":  delay.Milliseconds(),
			}).Debug("Retrying AMQP publish after delay")
			time.Sleep(delay)
		}

		// Use a timeout context for publishing
		ctx, cancel := context.WithTimeout(context.Background(), amqpPublishTimeout)

		// Publish with timeout
		publishDone := make(chan error, 1)
		go func() {
			publishDone <- l.client.PublishTranscription(msg.transcription, msg.callUUID, msg.metadata)
		}()

		start := time.Now()
		select {
		case err := <-publishDone:
			cancel()
			publishSpan.SetAttributes(attribute.Int64("amqp.publish.duration_ms", time.Since(start).Milliseconds()))
			if err != nil {
				if attempt < amqpMaxRetries {
					continue // Try again
				}
				atomic.AddInt64(&l.totalFailed, 1)
				atomic.AddInt64(&l.consecutiveErrors, 1)
				atomic.StoreInt64(&l.lastErrorTime, time.Now().Unix())
				l.logger.WithFields(logrus.Fields{
					"call_uuid":    msg.callUUID,
					"error":        err.Error(),
					"attempts":     attempt + 1,
					"is_final":     msg.isFinal,
					"message_age":  time.Since(msg.timestamp).String(),
				}).Error("Failed to publish transcription to AMQP after all retries")
				publishSpan.RecordError(err)
				publishSpan.SetStatus(codes.Error, err.Error())
				return
			}
			// Success
			atomic.AddInt64(&l.totalPublished, 1)
			atomic.StoreInt64(&l.consecutiveErrors, 0) // Reset consecutive errors
			atomic.StoreInt64(&l.lastConnectedTime, time.Now().Unix())
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"is_final":  msg.isFinal,
				"attempts":  attempt + 1,
			}).Debug("Transcription published to AMQP queue")
			publishSpan.SetStatus(codes.Ok, "published")
			return

		case <-ctx.Done():
			cancel()
			lastErr = context.DeadlineExceeded
			if attempt < amqpMaxRetries {
				continue // Try again
			}
			atomic.AddInt64(&l.totalTimeouts, 1)
			atomic.AddInt64(&l.consecutiveErrors, 1)
			atomic.StoreInt64(&l.lastErrorTime, time.Now().Unix())
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"attempts":  attempt + 1,
			}).Error("AMQP publish timed out after all retries")
			publishSpan.RecordError(lastErr)
			publishSpan.SetStatus(codes.Error, "publish timeout")
			return
		}
	}
}

// retryWithBackoff attempts to wait for connection to restore
func (l *AMQPTranscriptionListener) retryWithBackoff(msg *transcriptionMessage) bool {
	for attempt := 1; attempt <= amqpMaxRetries; attempt++ {
		delay := l.calculateBackoff(attempt)
		l.logger.WithFields(logrus.Fields{
			"call_uuid": msg.callUUID,
			"attempt":   attempt,
			"delay_ms":  delay.Milliseconds(),
		}).Debug("Waiting for AMQP connection to restore")

		select {
		case <-l.stopChan:
			return false
		case <-time.After(delay):
			if l.client != nil && l.client.IsConnected() {
				atomic.AddInt64(&l.totalRetries, 1)
				l.logger.WithFields(logrus.Fields{
					"call_uuid": msg.callUUID,
					"attempt":   attempt,
				}).Info("AMQP connection restored, proceeding with publish")
				return false // Connection restored, proceed with normal publish
			}
		}
	}
	return false // Retries exhausted, connection still down
}

// calculateBackoff returns delay for the given attempt using exponential backoff
func (l *AMQPTranscriptionListener) calculateBackoff(attempt int) time.Duration {
	delay := amqpBaseRetryDelay * time.Duration(1<<uint(attempt-1))
	if delay > amqpMaxRetryDelay {
		delay = amqpMaxRetryDelay
	}
	return delay
}

// healthCheckLoop periodically logs health status
func (l *AMQPTranscriptionListener) healthCheckLoop() {
	ticker := time.NewTicker(amqpHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-l.healthCheckStop:
			return
		case <-ticker.C:
			l.logHealthStatus()
		}
	}
}

// logHealthStatus logs the current health of the AMQP listener
func (l *AMQPTranscriptionListener) logHealthStatus() {
	published := atomic.LoadInt64(&l.totalPublished)
	failed := atomic.LoadInt64(&l.totalFailed)
	dropped := atomic.LoadInt64(&l.totalDropped)
	timeouts := atomic.LoadInt64(&l.totalTimeouts)
	retries := atomic.LoadInt64(&l.totalRetries)
	consecutiveErrors := atomic.LoadInt64(&l.consecutiveErrors)
	queueLen := len(l.publishChan)

	connected := false
	if l.client != nil {
		connected = l.client.IsConnected()
	}

	logLevel := logrus.InfoLevel
	if consecutiveErrors > 10 || !connected {
		logLevel = logrus.ErrorLevel
	} else if consecutiveErrors > 0 || dropped > 0 {
		logLevel = logrus.WarnLevel
	}

	l.logger.WithFields(logrus.Fields{
		"published":          published,
		"failed":             failed,
		"dropped":            dropped,
		"timeouts":           timeouts,
		"retries":            retries,
		"consecutive_errors": consecutiveErrors,
		"queue_length":       queueLen,
		"connected":          connected,
	}).Log(logLevel, "AMQP transcription listener health status")

	// Alert if connection has been down for extended period
	if !connected && consecutiveErrors > 50 {
		l.logger.WithFields(logrus.Fields{
			"consecutive_errors": consecutiveErrors,
		}).Error("AMQP connection has been down for an extended period - transcriptions are being lost")
	}
}

// OnTranscription is called when a new transcription is available
// This method is non-blocking and queues the message for async publishing
func (l *AMQPTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Skip if transcription is empty
	if transcription == "" {
		return
	}

	// Skip if client is not available
	if l.client == nil {
		return
	}

	// Get message from pool
	msg := l.messagePool.Get().(*transcriptionMessage)
	msg.callUUID = callUUID
	msg.transcription = transcription
	msg.isFinal = isFinal
	msg.timestamp = time.Now()

	// Copy metadata into pooled message's map
	for k, v := range metadata {
		msg.metadata[k] = v
	}
	msg.metadata["is_final"] = isFinal

	// Non-blocking send with backpressure handling
	select {
	case l.publishChan <- msg:
		// Track high water mark
		currentLen := int64(len(l.publishChan))
		for {
			highWater := atomic.LoadInt64(&l.channelHighWater)
			if currentLen <= highWater {
				break
			}
			if atomic.CompareAndSwapInt64(&l.channelHighWater, highWater, currentLen) {
				break
			}
		}
	default:
		// Channel full - drop message (backpressure)
		atomic.AddInt64(&l.totalDropped, 1)
		// Return message to pool
		for k := range msg.metadata {
			delete(msg.metadata, k)
		}
		l.messagePool.Put(msg)
		l.logger.WithFields(logrus.Fields{
			"call_uuid":     callUUID,
			"queue_size":    len(l.publishChan),
			"total_dropped": atomic.LoadInt64(&l.totalDropped),
		}).Warn("AMQP transcription message dropped due to backpressure")
	}
}

// GetMetrics returns listener metrics
func (l *AMQPTranscriptionListener) GetMetrics() (published, failed, dropped, timeouts, highWater int64) {
	return atomic.LoadInt64(&l.totalPublished),
		atomic.LoadInt64(&l.totalFailed),
		atomic.LoadInt64(&l.totalDropped),
		atomic.LoadInt64(&l.totalTimeouts),
		atomic.LoadInt64(&l.channelHighWater)
}

// GetExtendedMetrics returns detailed listener metrics
func (l *AMQPTranscriptionListener) GetExtendedMetrics() map[string]int64 {
	return map[string]int64{
		"published":          atomic.LoadInt64(&l.totalPublished),
		"failed":             atomic.LoadInt64(&l.totalFailed),
		"dropped":            atomic.LoadInt64(&l.totalDropped),
		"timeouts":           atomic.LoadInt64(&l.totalTimeouts),
		"retries":            atomic.LoadInt64(&l.totalRetries),
		"consecutive_errors": atomic.LoadInt64(&l.consecutiveErrors),
		"high_water":         atomic.LoadInt64(&l.channelHighWater),
		"queue_length":       int64(len(l.publishChan)),
	}
}

// IsHealthy returns true if the AMQP listener is healthy
func (l *AMQPTranscriptionListener) IsHealthy() bool {
	if l.client == nil {
		return false
	}
	if !l.client.IsConnected() {
		return false
	}
	// Consider unhealthy if many consecutive errors
	if atomic.LoadInt64(&l.consecutiveErrors) > 10 {
		return false
	}
	return true
}

// GetQueueLength returns current queue length
func (l *AMQPTranscriptionListener) GetQueueLength() int {
	return len(l.publishChan)
}

// Shutdown gracefully shuts down the listener
func (l *AMQPTranscriptionListener) Shutdown() {
	// Stop health check first
	close(l.healthCheckStop)

	// Stop workers
	close(l.stopChan)
	l.wg.Wait()

	// Log final metrics
	metrics := l.GetExtendedMetrics()
	l.logger.WithFields(logrus.Fields{
		"published": metrics["published"],
		"failed":    metrics["failed"],
		"dropped":   metrics["dropped"],
		"timeouts":  metrics["timeouts"],
		"retries":   metrics["retries"],
	}).Info("AMQP transcription listener shutdown complete")
}

// FilteredTranscriptionListener wraps a listener and filters on final/partial events.
type FilteredTranscriptionListener struct {
	delegate       transcriptionListener
	publishPartial bool
	publishFinal   bool
}

type transcriptionListener interface {
	OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{})
}

// NewFilteredTranscriptionListener creates a filtered listener.
func NewFilteredTranscriptionListener(delegate transcriptionListener, publishPartial, publishFinal bool) *FilteredTranscriptionListener {
	return &FilteredTranscriptionListener{
		delegate:       delegate,
		publishPartial: publishPartial,
		publishFinal:   publishFinal,
	}
}

// OnTranscription applies filtering before delegating.
func (l *FilteredTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if isFinal {
		if !l.publishFinal {
			return
		}
	} else {
		if !l.publishPartial {
			return
		}
	}
	l.delegate.OnTranscription(callUUID, transcription, isFinal, metadata)
}
