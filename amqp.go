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
		logger.Fatal("AMQP_URL or AMQP_QUEUE_NAME not set in .env file")
	}

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

		// Attempt to open a channel
		amqpChannel, err = conn.Channel()
		if err != nil {
			color.Red("Failed to open AMQP channel: %v", err)
			logger.WithError(err).Errorf("Retrying opening channel to AMQP server at %s in 5 seconds...", amqpURL)
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}
		color.Green("Successfully opened AMQP channel")

		// Attempt to declare the queue
		queue, err := amqpChannel.QueueDeclare(
			amqpQueueName,
			true,  // Durable
			false, // Delete when unused
			false, // Exclusive
			false, // No-wait
			nil,   // Arguments
		)
		if err != nil {
			color.Red("Failed to declare AMQP queue %s: %v", amqpQueueName, err)
			logger.WithError(err).Errorf("Retrying declaring queue in 5 seconds...")
			time.Sleep(5 * time.Second) // Wait before retrying
			continue
		}

		// Log success with detailed queue information
		color.Green("Successfully declared AMQP queue: %s", queue.Name)
		logger.Infof("Queue Details: Name=%s, Messages=%d, Consumers=%d", queue.Name, queue.Messages, queue.Consumers)
		break // Exit the retry loop after a successful connection
	}
}

// Function to send transcription messages to the AMQP queue
func sendTranscriptionToAMQP(transcription, callUUID string) {
	body := map[string]string{
		"call_uuid":     callUUID,
		"transcription": transcription,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal transcription to JSON")
		return
	}

	err = amqpChannel.Publish(
		"",            // Exchange
		amqpQueueName, // Routing key (queue name)
		false,         // Mandatory
		false,         // Immediate
		amqp.Publishing{
			ContentType: "application/json",
			Body:        bodyBytes,
		},
	)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to publish transcription to AMQP")
		return
	}

	logger.WithField("call_uuid", callUUID).Info("Successfully published transcription to AMQP")
}
