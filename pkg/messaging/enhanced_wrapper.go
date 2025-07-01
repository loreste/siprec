package messaging

import (
	"time"
)

// EnhancedAMQPClientWrapper wraps EnhancedAMQPClient to implement AMQPClient interface
type EnhancedAMQPClientWrapper struct {
	EnhancedClient *EnhancedAMQPClient
}

// Ensure EnhancedAMQPClientWrapper implements AMQPClient interface
var _ AMQPClientInterface = (*EnhancedAMQPClientWrapper)(nil)

// Use the existing AMQPClientInterface from interfaces.go

// Connect connects to AMQP
func (w *EnhancedAMQPClientWrapper) Connect() error {
	return w.EnhancedClient.Connect()
}

// Disconnect disconnects from AMQP
func (w *EnhancedAMQPClientWrapper) Disconnect() {
	w.EnhancedClient.Disconnect()
}

// PublishTranscription publishes a transcription message
func (w *EnhancedAMQPClientWrapper) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	return w.EnhancedClient.PublishTranscription(transcription, callUUID, metadata)
}

// IsConnected returns connection status
func (w *EnhancedAMQPClientWrapper) IsConnected() bool {
	return w.EnhancedClient.IsConnected()
}

// PublishToDeadLetterQueue publishes a message to the dead letter queue
func (w *EnhancedAMQPClientWrapper) PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error {
	// Convert content to message format
	message := map[string]interface{}{
		"content":   content,
		"call_uuid": callUUID,
		"metadata":  metadata,
	}
	
	headers := map[string]interface{}{
		"message_type": "dead_letter",
		"call_uuid":    callUUID,
		"timestamp":    time.Now().Unix(),
	}
	
	return w.EnhancedClient.PublishMessage(
		w.EnhancedClient.config.DeadLetterExchange,
		w.EnhancedClient.config.DeadLetterRoutingKey,
		message,
		headers,
	)
}

// GetEnhancedClient returns the underlying enhanced client for advanced operations
func (w *EnhancedAMQPClientWrapper) GetEnhancedClient() *EnhancedAMQPClient {
	return w.EnhancedClient
}