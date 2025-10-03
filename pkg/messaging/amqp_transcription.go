package messaging

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/telemetry/tracing"
)

// AMQPTranscriptionListener implements the TranscriptionListener interface
// for sending transcriptions to an AMQP message queue
type AMQPTranscriptionListener struct {
	logger *logrus.Logger
	client AMQPClientInterface
}

// NewAMQPTranscriptionListener creates a new AMQP transcription listener
func NewAMQPTranscriptionListener(logger *logrus.Logger, client AMQPClientInterface) *AMQPTranscriptionListener {
	return &AMQPTranscriptionListener{
		logger: logger,
		client: client,
	}
}

// OnTranscription is called when a new transcription is available
// It publishes the transcription to the AMQP queue with robust error handling
func (l *AMQPTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Use recover to prevent any panics from affecting the main server
	defer func() {
		if r := recover(); r != nil {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"recover":   r,
			}).Error("Recovered from panic in AMQP transcription listener")
		}
	}()

	// Skip if transcription is empty
	if transcription == "" {
		return
	}

	// Check connection status
	if l.client == nil {
		l.logger.WithField("call_uuid", callUUID).Debug("AMQP client is nil, skipping transcription publishing")
		return
	}

	if !l.client.IsConnected() {
		l.logger.WithField("call_uuid", callUUID).Debug("AMQP client not connected, skipping transcription publishing")
		return
	}

	callCtx := tracing.ContextForCall(callUUID)
	_, publishSpan := tracing.StartSpan(callCtx, "amqp.publish.transcription", trace.WithAttributes(
		attribute.String("call.id", callUUID),
		attribute.Bool("transcription.final", isFinal),
	), trace.WithSpanKind(trace.SpanKindProducer))
	defer publishSpan.End()

	// Add isFinal flag to metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["is_final"] = isFinal

	// Publish to AMQP with timeout
	// We don't want publishing to block the main processing flow
	publishDone := make(chan error, 1)
	go func() {
		publishDone <- l.client.PublishTranscription(transcription, callUUID, metadata)
	}()

	start := time.Now()
	// Wait with timeout
	select {
	case err := <-publishDone:
		publishSpan.SetAttributes(attribute.Int64("amqp.publish.duration_ms", time.Since(start).Milliseconds()))
		if err != nil {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"error":     err.Error(),
			}).Warn("Failed to publish transcription to AMQP")
			publishSpan.RecordError(err)
			publishSpan.SetStatus(codes.Error, err.Error())
		} else {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"is_final":  isFinal,
			}).Debug("Transcription published to AMQP queue")
			publishSpan.SetStatus(codes.Ok, "published")
		}
	case <-time.After(500 * time.Millisecond):
		l.logger.WithField("call_uuid", callUUID).Warn("AMQP publish timed out, continuing processing")
		publishSpan.RecordError(context.DeadlineExceeded)
		publishSpan.SetStatus(codes.Error, "publish timeout")
	}
}
