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
	totalPublished   int64
	totalFailed      int64
	totalDropped     int64
	totalTimeouts    int64
	channelHighWater int64
}

// NewAMQPTranscriptionListener creates a new AMQP transcription listener optimized for high concurrency
func NewAMQPTranscriptionListener(logger logrus.FieldLogger, client AMQPClientInterface) *AMQPTranscriptionListener {
	l := &AMQPTranscriptionListener{
		logger:      logger,
		client:      client,
		publishChan: make(chan *transcriptionMessage, amqpPublishChannelSize),
		stopChan:    make(chan struct{}),
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

	logger.WithFields(logrus.Fields{
		"workers":     amqpWorkerPoolSize,
		"buffer_size": amqpPublishChannelSize,
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

// processMessage handles a single message publish
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

	// Check connection status
	if l.client == nil || !l.client.IsConnected() {
		atomic.AddInt64(&l.totalFailed, 1)
		return
	}

	callCtx := tracing.ContextForCall(msg.callUUID)
	_, publishSpan := tracing.StartSpan(callCtx, "amqp.publish.transcription", trace.WithAttributes(
		attribute.String("call.id", msg.callUUID),
		attribute.Bool("transcription.final", msg.isFinal),
	), trace.WithSpanKind(trace.SpanKindProducer))
	defer publishSpan.End()

	// Use a timeout context for publishing
	ctx, cancel := context.WithTimeout(context.Background(), amqpPublishTimeout)
	defer cancel()

	// Publish with timeout
	publishDone := make(chan error, 1)
	go func() {
		publishDone <- l.client.PublishTranscription(msg.transcription, msg.callUUID, msg.metadata)
	}()

	start := time.Now()
	select {
	case err := <-publishDone:
		publishSpan.SetAttributes(attribute.Int64("amqp.publish.duration_ms", time.Since(start).Milliseconds()))
		if err != nil {
			atomic.AddInt64(&l.totalFailed, 1)
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"error":     err.Error(),
			}).Warn("Failed to publish transcription to AMQP")
			publishSpan.RecordError(err)
			publishSpan.SetStatus(codes.Error, err.Error())
		} else {
			atomic.AddInt64(&l.totalPublished, 1)
			l.logger.WithFields(logrus.Fields{
				"call_uuid": msg.callUUID,
				"is_final":  msg.isFinal,
			}).Debug("Transcription published to AMQP queue")
			publishSpan.SetStatus(codes.Ok, "published")
		}
	case <-ctx.Done():
		atomic.AddInt64(&l.totalTimeouts, 1)
		l.logger.WithField("call_uuid", msg.callUUID).Warn("AMQP publish timed out")
		publishSpan.RecordError(context.DeadlineExceeded)
		publishSpan.SetStatus(codes.Error, "publish timeout")
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
	if metadata != nil {
		for k, v := range metadata {
			msg.metadata[k] = v
		}
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

// GetQueueLength returns current queue length
func (l *AMQPTranscriptionListener) GetQueueLength() int {
	return len(l.publishChan)
}

// Shutdown gracefully shuts down the listener
func (l *AMQPTranscriptionListener) Shutdown() {
	close(l.stopChan)
	l.wg.Wait()
	l.logger.Info("AMQP transcription listener shutdown complete")
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
