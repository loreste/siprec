package main

import (
	"encoding/json"
	"os"

	"github.com/streadway/amqp"
)

var (
	amqpChannel   *amqp.Channel
	amqpQueueName string
)

func initAMQP() {
	// Initialize AMQP settings
	amqpURL := os.Getenv("AMQP_URL")
	amqpQueueName = os.Getenv("AMQP_QUEUE_NAME")
	if amqpURL == "" || amqpQueueName == "" {
		logger.Fatal("AMQP_URL or AMQP_QUEUE_NAME not set in .env file")
	}

	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		logger.Fatalf("Failed to connect to AMQP server: %v", err)
	}

	amqpChannel, err = conn.Channel()
	if err != nil {
		logger.Fatalf("Failed to open a channel: %v", err)
	}

	_, err = amqpChannel.QueueDeclare(
		amqpQueueName,
		true,  // Durable
		false, // Delete when unused
		false, // Exclusive
		false, // No-wait
		nil,   // Arguments
	)
	if err != nil {
		logger.Fatalf("Failed to declare a queue: %v", err)
	}
}

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
	}
}
