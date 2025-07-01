package messaging

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

func TestEnhancedAMQPClient_Creation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 2,
		Username:       "guest",
		Password:       "guest",
	}

	client := NewEnhancedAMQPClient(logger, config)
	if client == nil {
		t.Fatal("EnhancedAMQPClient should not be nil")
	}

	if !client.IsConnected() {
		t.Log("Client not connected (expected in test environment)")
	}

	client.Disconnect()
}

func TestAMQPMetricsCollector_Creation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	poolConfig := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 1,
		Username:       "guest",
		Password:       "guest",
	}
	pool := NewAMQPPool(logger, poolConfig)

	collector := NewAMQPMetricsCollector(logger, pool, nil, nil, nil)
	if collector == nil {
		t.Fatal("Metrics collector should not be nil")
	}

	// Test recording an error
	collector.RecordError("test", "Test error", "test", "error")

	// Test getting snapshot
	snapshot := collector.GetSnapshot()
	if snapshot == nil {
		t.Error("Snapshot should not be nil")
	}

	if snapshot.Timestamp.IsZero() {
		t.Error("Snapshot timestamp should be set")
	}
}

func TestDurationMetrics_Basic(t *testing.T) {
	dm := NewDurationMetrics()

	// Test recording a duration
	testDuration := 100 * time.Millisecond
	dm.Record(testDuration)

	if dm.Count != 1 {
		t.Errorf("Expected count 1, got %d", dm.Count)
	}

	if dm.Min != testDuration {
		t.Errorf("Expected min %v, got %v", testDuration, dm.Min)
	}

	if dm.Max != testDuration {
		t.Errorf("Expected max %v, got %v", testDuration, dm.Max)
	}
}

func TestEnhancedWrapper_Interface(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 1,
		Username:       "guest",
		Password:       "guest",
	}

	enhanced := NewEnhancedAMQPClient(logger, config)
	wrapper := &EnhancedAMQPClientWrapper{EnhancedClient: enhanced}

	// Test interface compliance
	var _ AMQPClientInterface = wrapper

	// Test basic operations (should not panic)
	connected := wrapper.IsConnected()
	t.Logf("Wrapper connected: %v", connected)

	// Test publishing (will fail but should not crash)
	err := wrapper.PublishTranscription("test", "uuid", map[string]interface{}{})
	if err == nil {
		t.Log("Unexpected success (no connection expected)")
	}

	wrapper.Disconnect()
}