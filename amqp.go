package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/fatih/color"
	"github.com/streadway/amqp"
)

var (
	amqpChannel   *amqp.Channel
	amqpQueueName string
)

// initAMQP initializes the connection to the AMQP server with retry logic.
func initAMQP() {
	// Initialize AMQP settings
	amqpURL := os.Getenv("AMQP_URL")
	amqpQueueName = os.Getenv("AMQP_QUEUE_NAME")
	if amqpURL == "" || amqpQueueName == "" {
		logger.Warn("AMQP_URL or AMQP_QUEUE_NAME not set in .env file, AMQP functionality will be disabled")
		return
	}

	// Start a background goroutine to handle AMQP connection and reconnection
	go manageAMQPConnection(amqpURL)
}

// manageAMQPConnection handles connecting to AMQP and reconnecting if needed
func manageAMQPConnection(amqpURL string) {
	for {
		// Attempt to connect to the AMQP server
		conn, err := amqp.Dial(amqpURL)
		if err != nil {
			// Log unsuccessful connection in red and retry
			color.Red("Failed to connect to AMQP server at %s: %v", amqpURL, err)
			logger.WithError(err).Errorf("Retrying connection to AMQP server at %s in 5 seconds...", amqpURL)
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}

		// Log successful connection in green
		color.Green("Successfully connected to AMQP server at %s", amqpURL)
		logger.Infof("Successfully connected to AMQP server at %s", amqpURL)

		// Set up connection closed notification channel
		closeChan := make(chan *amqp.Error)
		conn.NotifyClose(closeChan)

		// Attempt to open a channel
		ch, err := conn.Channel()
		if err != nil {
			color.Red("Failed to open AMQP channel: %v", err)
			logger.WithError(err).Error("Failed to open channel, will retry connection")
			conn.Close() // Make sure to close the connection before retrying
			time.Sleep(5 * time.Second)
			continue
		}

		color.Green("Successfully opened AMQP channel")

		// Attempt to declare the queue
		queue, err := ch.QueueDeclare(
			amqpQueueName,
			true,  // Durable
			false, // Delete when unused
			false, // Exclusive
			false, // No-wait
			nil,   // Arguments
		)
		if err != nil {
			color.Red("Failed to declare AMQP queue %s: %v", amqpQueueName, err)
			logger.WithError(err).Error("Failed to declare queue, will retry connection")
			ch.Close()
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// Log success with detailed queue information
		color.Green("Successfully declared AMQP queue: %s", queue.Name)
		logger.Infof("Queue Details: Name=%s, Messages=%d, Consumers=%d", queue.Name, queue.Messages, queue.Consumers)

		// Set channel for global use
		amqpChannel = ch

		// Wait for connection to close
		closeErr := <-closeChan

		// Connection closed, clean up and retry
		amqpChannel = nil
		logger.WithError(closeErr).Warn("AMQP connection closed, will reconnect")
		time.Sleep(5 * time.Second)
	}
}

// Function to send transcription messages to the AMQP queue
func sendTranscriptionToAMQP(transcription, callUUID string) {
	if amqpChannel == nil {
		logger.WithField("call_uuid", callUUID).Warn("AMQP channel not initialized, transcription not sent to AMQP")
		return
	}

	// Get the recording session if available
	var recSession *RecordingSession
	if forwarderValue, exists := activeCalls.Load(callUUID); exists {
		forwarder := forwarderValue.(*RTPForwarder)
		recSession = forwarder.recordingSession
	}

	// Create message body with or without recording session info
	var body map[string]interface{}

	if recSession != nil {
		// Include SIPREC metadata
		body = map[string]interface{}{
			"call_uuid":     callUUID,
			"transcription": transcription,
			"timestamp":     time.Now().Format(time.RFC3339),
			"siprec": map[string]interface{}{
				"session_id":      recSession.ID,
				"recording_state": recSession.RecordingState,
				"sequence_number": recSession.SequenceNumber,
				"participants":    recSession.Participants,
			},
		}
	} else {
		// Basic message without SIPREC data
		body = map[string]interface{}{
			"call_uuid":     callUUID,
			"transcription": transcription,
			"timestamp":     time.Now().Format(time.RFC3339),
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal transcription to JSON")
		return
	}

	// Use channel mutex to avoid race conditions during reconnection
	err = amqpChannel.Publish(
		"",            // Exchange
		amqpQueueName, // Routing key (queue name)
		false,         // Mandatory
		false,         // Immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         bodyBytes,
			DeliveryMode: amqp.Persistent, // Make message persistent
			Timestamp:    time.Now(),
		},
	)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to publish transcription to AMQP")
		return
	}

	logger.WithField("call_uuid", callUUID).Debug("Successfully published transcription to AMQP")
}

// Function to send metadata updates to the AMQP queue
func sendMetadataUpdateToAMQP(callUUID string, recordingSession *RecordingSession, added, removed, modified []Participant) {
	if amqpChannel == nil {
		logger.WithField("call_uuid", callUUID).Error("AMQP channel not initialized")
		return
	}

	// Create the message body with SIPREC metadata
	body := map[string]interface{}{
		"call_uuid":  callUUID,
		"event_type": "metadata_update",
		"timestamp":  time.Now().Format(time.RFC3339),
		"siprec": map[string]interface{}{
			"session_id":            recordingSession.ID,
			"recording_state":       recordingSession.RecordingState,
			"sequence_number":       recordingSession.SequenceNumber,
			"participants":          recordingSession.Participants,
			"participants_added":    added,
			"participants_removed":  removed,
			"participants_modified": modified,
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal metadata update to JSON")
		return
	}

	err = amqpChannel.Publish(
		"",            // Exchange
		amqpQueueName, // Routing key (queue name)
		false,         // Mandatory
		false,         // Immediate
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         bodyBytes,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
		},
	)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to publish metadata update to AMQP")
		return
	}

	logger.WithField("call_uuid", callUUID).Info("Successfully published metadata update to AMQP")
}
