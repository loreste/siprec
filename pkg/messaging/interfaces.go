package messaging

// AMQPClientInterface defines the interface for AMQP clients
type AMQPClientInterface interface {
	PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error
	PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error
	IsConnected() bool
	Connect() error
	Disconnect()
}