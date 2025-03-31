package messaging

import (
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewAMQPClient(t *testing.T) {
	logger := logrus.New()
	url := "amqp://guest:guest@localhost:5672/"
	queueName := "test_queue"
	
	client := NewAMQPClient(logger, url, queueName)
	
	assert.NotNil(t, client, "AMQPClient should not be nil")
	assert.Equal(t, url, client.url, "URL should be set correctly")
	assert.Equal(t, queueName, client.queueName, "Queue name should be set correctly")
	assert.NotNil(t, client.stopChan, "Stop channel should be initialized")
	assert.False(t, client.connected, "Client should not be connected initially")
}

func TestAMQPClientWithEmptyConfig(t *testing.T) {
	logger := logrus.New()
	
	// Create client with empty configuration
	client := NewAMQPClient(logger, "", "")
	
	// Try to connect
	err := client.Connect()
	
	// Should fail with configuration error
	assert.Error(t, err, "Connect should return an error with empty configuration")
	assert.Contains(t, err.Error(), "AMQP URL or queue name not configured", "Error message should indicate configuration issue")
	assert.False(t, client.connected, "Client should not be connected")
}

func TestPublishTranscription(t *testing.T) {
	logger := logrus.New()
	url := "amqp://guest:guest@localhost:5672/"
	queueName := "test_queue"
	
	client := NewAMQPClient(logger, url, queueName)
	
	// Create metadata for the test
	metadata := map[string]interface{}{
		"language_code": "en-US",
		"confidence": 0.95,
		"speaker_channel": 0,
	}
	
	// Try to publish when not connected
	err := client.PublishTranscription("This is a test", "test-uuid", metadata)
	
	// Should fail because we're not connected
	assert.Error(t, err, "Publishing should fail when not connected")
	assert.Contains(t, err.Error(), "not connected", "Error should indicate connection issue")
}

func TestDisconnect(t *testing.T) {
	logger := logrus.New()
	url := "amqp://guest:guest@localhost:5672/"
	queueName := "test_queue"
	
	client := NewAMQPClient(logger, url, queueName)
	
	// Disconnect should not crash even if not connected
	client.Disconnect()
	assert.False(t, client.connected, "Client should not be connected after disconnect")
}

func TestJSONMarshal(t *testing.T) {
	// Create a test payload as map
	payload := map[string]interface{}{
		"call_uuid":       "test-uuid",
		"language_code":   "en-US",
		"transcription":   "This is a test",
		"confidence":      0.95,
		"timestamp":       12345,
		"speaker_channel": 0,
		"is_final":        true,
		"provider":        "mock",
	}
	
	// Convert to JSON using standard library
	jsonData, err := json.Marshal(payload)
	
	// Verify results
	assert.NoError(t, err, "json.Marshal should not return an error")
	assert.NotEmpty(t, jsonData, "JSON data should not be empty")
	assert.Contains(t, string(jsonData), "test-uuid", "JSON should contain call UUID")
	assert.Contains(t, string(jsonData), "en-US", "JSON should contain language code")
	assert.Contains(t, string(jsonData), "This is a test", "JSON should contain transcription text")
}