package messaging

import (
	"time"

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

	// Wait with timeout
	select {
	case err := <-publishDone:
		if err != nil {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"error":     err.Error(),
			}).Warn("Failed to publish transcription to AMQP")
		} else {
			l.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"is_final":  isFinal,
			}).Debug("Transcription published to AMQP queue")
		}
	case <-time.After(500 * time.Millisecond):
		l.logger.WithField("call_uuid", callUUID).Warn("AMQP publish timed out, continuing processing")
	}
}
