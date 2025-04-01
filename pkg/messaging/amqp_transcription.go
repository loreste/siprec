package messaging

import (
	"github.com/sirupsen/logrus"
)

// AMQPTranscriptionListener implements the TranscriptionListener interface
// for sending transcriptions to an AMQP message queue
type AMQPTranscriptionListener struct {
	logger *logrus.Logger
	client *AMQPClient
}

// NewAMQPTranscriptionListener creates a new AMQP transcription listener
func NewAMQPTranscriptionListener(logger *logrus.Logger, client *AMQPClient) *AMQPTranscriptionListener {
	return &AMQPTranscriptionListener{
		logger: logger,
		client: client,
	}
}

// OnTranscription is called when a new transcription is available
// It publishes the transcription to the AMQP queue
func (l *AMQPTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if !l.client.IsConnected() {
		l.logger.WithField("call_uuid", callUUID).Warn("Cannot publish transcription: AMQP client not connected")
		return
	}

	// Add isFinal flag to metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["is_final"] = isFinal

	// Publish to AMQP
	err := l.client.PublishTranscription(transcription, callUUID, metadata)
	if err != nil {
		l.logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"error":     err.Error(),
		}).Error("Failed to publish transcription to AMQP")
	} else {
		l.logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"is_final":  isFinal,
		}).Info("Transcription published to AMQP queue")
	}
}