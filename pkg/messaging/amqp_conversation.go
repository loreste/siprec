package messaging

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/telemetry/tracing"
)

// ConversationRecord represents the complete conversation for a call
// This mirrors the structure from the STT package to avoid import cycles
type ConversationRecord struct {
	CallUUID     string                   `json:"call_uuid"`
	StartTime    time.Time                `json:"start_time"`
	EndTime      time.Time                `json:"end_time,omitempty"`
	Duration     time.Duration            `json:"duration,omitempty"`
	Segments     []ConversationSegment    `json:"segments"`
	FinalText    string                   `json:"final_text"`
	WordCount    int                      `json:"word_count"`
	SegmentCount int                      `json:"segment_count"`
	Providers    []string                 `json:"providers,omitempty"`
	Metadata     map[string]interface{}   `json:"metadata,omitempty"`
}

// ConversationSegment represents a single transcription segment
type ConversationSegment struct {
	Timestamp     time.Time              `json:"timestamp"`
	Transcription string                 `json:"transcription"`
	IsFinal       bool                   `json:"is_final"`
	Speaker       string                 `json:"speaker,omitempty"`
	Confidence    float64                `json:"confidence,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// AMQPConversationPublisher publishes complete conversations to AMQP
type AMQPConversationPublisher struct {
	logger       logrus.FieldLogger
	client       AMQPClientInterface
	exchangeName string
	routingKey   string
}

// NewAMQPConversationPublisher creates a new AMQP conversation publisher
func NewAMQPConversationPublisher(logger logrus.FieldLogger, client AMQPClientInterface) *AMQPConversationPublisher {
	return &AMQPConversationPublisher{
		logger:       logger,
		client:       client,
		exchangeName: "siprec.conversations",
		routingKey:   "conversation.complete",
	}
}

// SetExchange sets the exchange name for conversation publishing
func (p *AMQPConversationPublisher) SetExchange(exchangeName string) {
	p.exchangeName = exchangeName
}

// SetRoutingKey sets the routing key for conversation publishing
func (p *AMQPConversationPublisher) SetRoutingKey(routingKey string) {
	p.routingKey = routingKey
}

// PublishConversation publishes a complete conversation record to AMQP
func (p *AMQPConversationPublisher) PublishConversation(record interface{}) error {
	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			p.logger.WithField("panic", r).Error("Recovered from panic in PublishConversation")
		}
	}()

	if p.client == nil {
		p.logger.Debug("AMQP client is nil, skipping conversation publishing")
		return nil
	}

	if !p.client.IsConnected() {
		p.logger.Debug("AMQP client not connected, skipping conversation publishing")
		return nil
	}

	// Convert record to our type
	recordJSON, err := json.Marshal(record)
	if err != nil {
		p.logger.WithError(err).Error("Failed to marshal conversation record")
		return err
	}

	// Extract call UUID for tracing
	var callUUID string
	if rec, ok := record.(*ConversationRecord); ok {
		callUUID = rec.CallUUID
	} else {
		// Try to extract from generic record
		var genericRec map[string]interface{}
		if err := json.Unmarshal(recordJSON, &genericRec); err == nil {
			if uuid, ok := genericRec["call_uuid"].(string); ok {
				callUUID = uuid
			}
		}
	}

	// Start tracing span
	callCtx := tracing.ContextForCall(callUUID)
	_, publishSpan := tracing.StartSpan(callCtx, "amqp.publish.conversation", trace.WithAttributes(
		attribute.String("call.id", callUUID),
		attribute.String("exchange", p.exchangeName),
		attribute.String("routing_key", p.routingKey),
	), trace.WithSpanKind(trace.SpanKindProducer))
	defer publishSpan.End()

	// Create metadata for the message
	metadata := map[string]interface{}{
		"message_type": "conversation_complete",
		"exchange":     p.exchangeName,
		"routing_key":  p.routingKey,
		"published_at": time.Now().UTC().Format(time.RFC3339),
	}

	// Publish with timeout
	publishDone := make(chan error, 1)
	go func() {
		publishDone <- p.client.PublishTranscription(string(recordJSON), callUUID, metadata)
	}()

	start := time.Now()
	select {
	case err := <-publishDone:
		publishSpan.SetAttributes(attribute.Int64("amqp.publish.duration_ms", time.Since(start).Milliseconds()))
		if err != nil {
			p.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"error":     err.Error(),
			}).Error("Failed to publish conversation to AMQP")
			publishSpan.RecordError(err)
			publishSpan.SetStatus(codes.Error, err.Error())
			return err
		}
		p.logger.WithFields(logrus.Fields{
			"call_uuid":     callUUID,
			"duration_ms":   time.Since(start).Milliseconds(),
		}).Info("Complete conversation published to AMQP")
		publishSpan.SetStatus(codes.Ok, "published")
		return nil
	case <-time.After(2 * time.Second):
		p.logger.WithField("call_uuid", callUUID).Warn("AMQP conversation publish timed out")
		publishSpan.RecordError(context.DeadlineExceeded)
		publishSpan.SetStatus(codes.Error, "publish timeout")
		return context.DeadlineExceeded
	}
}

// OnConversationEnd is a callback adapter for ConversationAccumulator
// This allows the publisher to be used as a ConversationEndCallback
func (p *AMQPConversationPublisher) OnConversationEnd(record interface{}) {
	if err := p.PublishConversation(record); err != nil {
		p.logger.WithError(err).Error("Failed to publish conversation on end")
	}
}
