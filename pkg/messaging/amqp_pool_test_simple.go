package messaging

import (
	"testing"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

func TestAMQPPool_Creation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 2,
		Username:       "guest",
		Password:       "guest",
	}

	pool := NewAMQPPool(logger, config)
	if pool == nil {
		t.Fatal("AMQPPool should not be nil")
	}

	if pool.logger != logger {
		t.Error("Logger should be set correctly")
	}

	if pool.config != config {
		t.Error("Config should be set correctly")
	}
}

func TestAMQPPool_BasicOperations(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 2,
		Username:       "guest",
		Password:       "guest",
	}

	pool := NewAMQPPool(logger, config)

	// Test getting metrics (should work even without connection)
	metrics := pool.GetMetrics()
	if metrics.TotalConnections < 0 {
		t.Error("Total connections should not be negative")
	}

	// Test shutdown (should not panic)
	pool.Shutdown()
}

func TestAMQPPool_PublishWithoutConnection(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &config.AMQPConfig{
		Hosts:          []string{"amqp://localhost:5672"},
		MaxConnections: 2,
		Username:       "guest",
		Password:       "guest",
	}

	pool := NewAMQPPool(logger, config)

	// Test publishing without connection (should fail gracefully)
	err := pool.PublishWithConfirm("test_exchange", "test_key", []byte("test message"), nil)
	if err == nil {
		t.Error("Expected error when publishing without connection")
	}

	pool.Shutdown()
}