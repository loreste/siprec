package unit

import (
	"fmt"
	"testing"
	"time"

	"siprec-server/pkg/messaging"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
)

// MessagingTestSuite provides comprehensive unit tests for messaging components
type MessagingTestSuite struct {
	suite.Suite
	logger *logrus.Logger
}

func (suite *MessagingTestSuite) SetupSuite() {
	suite.logger = logrus.New()
	suite.logger.SetLevel(logrus.WarnLevel) // Reduce noise in tests
}

// TestMemoryMessageStorage tests the in-memory message storage implementation
func (suite *MessagingTestSuite) TestMemoryMessageStorage() {
	storage := messaging.NewMemoryMessageStorage()

	// Test storing a message
	msg := &messaging.PendingMessage{
		ID:           "test-msg-1",
		CallUUID:     "test-call-1",
		Content:      "test transcription",
		CreatedAt:    time.Now(),
		AttemptCount: 1,
		Priority:     messaging.PriorityHigh,
		Metadata: map[string]interface{}{
			"provider": "test",
		},
	}

	err := storage.Store(msg)
	suite.Assert().NoError(err)

	// Test retrieving the message
	retrieved, err := storage.Retrieve("test-msg-1")
	suite.Assert().NoError(err)
	suite.Assert().NotNil(retrieved)
	suite.Assert().Equal(msg.ID, retrieved.ID)
	suite.Assert().Equal(msg.CallUUID, retrieved.CallUUID)
	suite.Assert().Equal(msg.Content, retrieved.Content)

	// Test listing messages
	messages, err := storage.List(10)
	suite.Assert().NoError(err)
	suite.Assert().Len(messages, 1)

	// Test counting messages
	count, err := storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(1, count)

	// Test deleting a message
	err = storage.Delete("test-msg-1")
	suite.Assert().NoError(err)

	// Verify deletion
	retrieved, err = storage.Retrieve("test-msg-1")
	suite.Assert().NoError(err)
	suite.Assert().Nil(retrieved)

	count, err = storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(0, count)
}

// TestMemoryStorageConcurrency tests concurrent access to memory storage
func (suite *MessagingTestSuite) TestMemoryStorageConcurrency() {
	storage := messaging.NewMemoryMessageStorage()
	
	const numGoroutines = 10
	const messagesPerGoroutine = 20

	// Concurrent writes
	errChan := make(chan error, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			for j := 0; j < messagesPerGoroutine; j++ {
				msg := &messaging.PendingMessage{
					ID:           fmt.Sprintf("msg-%d-%d", index, j),
					CallUUID:     fmt.Sprintf("call-%d", index),
					Content:      fmt.Sprintf("content-%d-%d", index, j),
					CreatedAt:    time.Now(),
					AttemptCount: 1,
					Priority:     messaging.PriorityNormal,
				}
				if err := storage.Store(msg); err != nil {
					errChan <- err
					return
				}
			}
			errChan <- nil
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		err := <-errChan
		suite.Assert().NoError(err)
	}

	// Verify total count
	count, err := storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(numGoroutines*messagesPerGoroutine, count)

	// Concurrent reads and deletes
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			for j := 0; j < messagesPerGoroutine; j++ {
				msgID := fmt.Sprintf("msg-%d-%d", index, j)
				
				// Read
				retrieved, err := storage.Retrieve(msgID)
				if err != nil || retrieved == nil {
					errChan <- fmt.Errorf("failed to retrieve message %s", msgID)
					return
				}

				// Delete
				if err := storage.Delete(msgID); err != nil {
					errChan <- err
					return
				}
			}
			errChan <- nil
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		err := <-errChan
		suite.Assert().NoError(err)
	}

	// Verify all messages are deleted
	count, err = storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(0, count)
}

// TestCircuitBreaker tests the circuit breaker implementation
func (suite *MessagingTestSuite) TestCircuitBreaker() {
	config := messaging.DefaultCircuitBreakerConfig()
	config.MaxFailures = 3
	config.ResetTimeout = 100 * time.Millisecond

	cb := messaging.NewCircuitBreaker(suite.logger, config)

	// Initially should be closed
	suite.Assert().Equal(messaging.StateClosed, cb.GetState())

	// Successful operations should keep it closed
	for i := 0; i < 5; i++ {
		err := cb.Execute(func() error {
			return nil // Success
		})
		suite.Assert().NoError(err)
		suite.Assert().Equal(messaging.StateClosed, cb.GetState())
	}

	// Failures should eventually open the circuit
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error {
			return fmt.Errorf("test failure")
		})
		suite.Assert().Error(err)
	}

	// Circuit should now be open
	suite.Assert().Equal(messaging.StateOpen, cb.GetState())

	// Subsequent requests should fail fast
	err := cb.Execute(func() error {
		return nil // This should not be executed
	})
	suite.Assert().ErrorIs(err, messaging.ErrCircuitBreakerOpen)

	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)

	// Should transition to half-open on next request
	err = cb.Execute(func() error {
		return nil // Success
	})
	suite.Assert().NoError(err)

	// Should be in half-open or closed state after successful retry
	state := cb.GetState()
	suite.Assert().True(state == messaging.StateClosed || state == messaging.StateHalfOpen)
}

// TestCircuitBreakerMetrics tests circuit breaker metrics collection
func (suite *MessagingTestSuite) TestCircuitBreakerMetrics() {
	config := messaging.DefaultCircuitBreakerConfig()
	config.MaxFailures = 2

	cb := messaging.NewCircuitBreaker(suite.logger, config)

	// Execute some operations
	cb.Execute(func() error { return nil })                        // Success
	cb.Execute(func() error { return fmt.Errorf("failure 1") })    // Failure
	cb.Execute(func() error { return nil })                        // Success
	cb.Execute(func() error { return fmt.Errorf("failure 2") })    // Failure

	metrics := cb.GetMetrics()
	
	suite.Assert().Equal(messaging.StateClosed, metrics.State)
	suite.Assert().Equal(int64(4), metrics.TotalRequests)
	suite.Assert().Equal(int64(2), metrics.TotalFailures)
	suite.Assert().Equal(int64(2), metrics.TotalSuccesses)
	suite.Assert().Equal(0.5, metrics.FailureRate)
}

// TestGuaranteedDeliveryService tests the guaranteed delivery service
func (suite *MessagingTestSuite) TestGuaranteedDeliveryService() {
	// Create mock AMQP client
	mockClient := &MockAMQPClient{
		connected: true,
		failures:  make(map[string]bool),
	}

	storage := messaging.NewMemoryMessageStorage()
	
	config := messaging.DefaultDeliveryConfig()
	config.WorkerCount = 1
	config.MaxRetries = 2
	config.InitialRetryDelay = 10 * time.Millisecond

	service := messaging.NewGuaranteedDeliveryService(suite.logger, mockClient, storage, &config)

	// Start the service
	err := service.Start()
	suite.Assert().NoError(err)

	// Send a message
	err = service.SendMessage("test-call", "test content", map[string]interface{}{"test": "metadata"}, messaging.PriorityNormal)
	suite.Assert().NoError(err)

	// Wait a bit for processing
	time.Sleep(50 * time.Millisecond)

	// Check metrics
	metrics := service.GetMetrics()
	suite.Assert().True(metrics.TotalMessages >= 1, "Should have at least 1 message")
	
	// Stop the service
	err = service.Stop()
	suite.Assert().NoError(err)
}

// TestGuaranteedDeliveryRetries tests retry mechanisms
func (suite *MessagingTestSuite) TestGuaranteedDeliveryRetries() {
	// Create mock AMQP client that fails initially
	mockClient := &MockAMQPClient{
		connected:    true,
		failures:     make(map[string]bool),
		failureCount: 2, // Fail first 2 attempts
	}

	storage := messaging.NewMemoryMessageStorage()
	
	config := messaging.DefaultDeliveryConfig()
	config.WorkerCount = 1
	config.MaxRetries = 3
	config.InitialRetryDelay = 10 * time.Millisecond
	config.MessageTimeout = 1 * time.Second

	service := messaging.NewGuaranteedDeliveryService(suite.logger, mockClient, storage, &config)

	err := service.Start()
	suite.Assert().NoError(err)

	// Send a message that will initially fail
	err = service.SendMessage("test-call", "test content", nil, messaging.PriorityNormal)
	suite.Assert().NoError(err)

	// Wait for retries to complete
	time.Sleep(200 * time.Millisecond)

	// Should eventually succeed
	metrics := service.GetMetrics()
	suite.Assert().True(metrics.SuccessfulDeliveries > 0 || metrics.FailedDeliveries > 0)

	err = service.Stop()
	suite.Assert().NoError(err)
}

// MockAMQPClient for testing
type MockAMQPClient struct {
	connected    bool
	failures     map[string]bool
	failureCount int
	attempts     int
}

func (m *MockAMQPClient) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	m.attempts++
	
	if m.failureCount > 0 && m.attempts <= m.failureCount {
		return fmt.Errorf("mock failure %d", m.attempts)
	}
	
	if m.failures[callUUID] {
		return fmt.Errorf("mock failure for call %s", callUUID)
	}
	
	return nil
}

func (m *MockAMQPClient) PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error {
	return nil
}

func (m *MockAMQPClient) IsConnected() bool {
	return m.connected
}

func (m *MockAMQPClient) Connect() error {
	m.connected = true
	return nil
}

func (m *MockAMQPClient) Disconnect() {
	m.connected = false
}

// TestDeliveryServiceIntegrationWithCircuitBreaker tests integration between delivery service and circuit breaker
func (suite *MessagingTestSuite) TestDeliveryServiceIntegrationWithCircuitBreaker() {
	// Create mock AMQP client that fails frequently
	mockClient := &MockAMQPClient{
		connected: true,
		failures:  map[string]bool{"fail-call": true},
	}

	// Wrap with circuit breaker
	cbConfig := messaging.DefaultCircuitBreakerConfig()
	cbConfig.MaxFailures = 2
	cbConfig.ResetTimeout = 50 * time.Millisecond

	cbClient := messaging.NewCircuitBreakerAMQPClient(mockClient, suite.logger, cbConfig)

	storage := messaging.NewMemoryMessageStorage()
	
	config := messaging.DefaultDeliveryConfig()
	config.WorkerCount = 1
	config.MaxRetries = 1
	config.InitialRetryDelay = 10 * time.Millisecond

	service := messaging.NewGuaranteedDeliveryService(suite.logger, cbClient, storage, &config)

	err := service.Start()
	suite.Assert().NoError(err)

	// Send messages that will fail
	for i := 0; i < 3; i++ {
		err = service.SendMessage("fail-call", fmt.Sprintf("content %d", i), nil, messaging.PriorityNormal)
		suite.Assert().NoError(err)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Circuit breaker should be open
	suite.Assert().Equal(messaging.StateOpen, cbClient.GetCircuitBreakerState())

	// Send a message that would succeed, but circuit breaker should prevent it
	err = service.SendMessage("success-call", "content", nil, messaging.PriorityNormal)
	suite.Assert().NoError(err)

	time.Sleep(50 * time.Millisecond)

	// Wait for circuit breaker to reset
	time.Sleep(100 * time.Millisecond)

	// Send another successful message
	err = service.SendMessage("success-call-2", "content", nil, messaging.PriorityNormal)
	suite.Assert().NoError(err)

	time.Sleep(50 * time.Millisecond)

	err = service.Stop()
	suite.Assert().NoError(err)
}

// TestMessageStorageCleanup tests the cleanup functionality
func (suite *MessagingTestSuite) TestMessageStorageCleanup() {
	storage := messaging.NewMemoryMessageStorage()

	// Add some old messages
	oldTime := time.Now().Add(-2 * time.Hour)
	recentTime := time.Now().Add(-5 * time.Minute)

	oldMsg := &messaging.PendingMessage{
		ID:        "old-msg",
		CallUUID:  "old-call",
		Content:   "old content",
		CreatedAt: oldTime,
	}

	recentMsg := &messaging.PendingMessage{
		ID:        "recent-msg",
		CallUUID:  "recent-call",
		Content:   "recent content",
		CreatedAt: recentTime,
	}

	err := storage.Store(oldMsg)
	suite.Assert().NoError(err)

	err = storage.Store(recentMsg)
	suite.Assert().NoError(err)

	// Cleanup messages older than 1 hour
	cutoff := time.Now().Add(-1 * time.Hour)
	cleaned, err := storage.CleanupExpired(cutoff)
	suite.Assert().NoError(err)
	suite.Assert().Equal(1, cleaned) // Should clean up 1 old message

	// Verify the old message is gone but recent one remains
	count, err := storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(1, count)

	remaining, err := storage.Retrieve("recent-msg")
	suite.Assert().NoError(err)
	suite.Assert().NotNil(remaining)

	gone, err := storage.Retrieve("old-msg")
	suite.Assert().NoError(err)
	suite.Assert().Nil(gone)
}

// TestBatchOperations tests batch operations on message storage
func (suite *MessagingTestSuite) TestBatchOperations() {
	storage := messaging.NewMemoryMessageStorage()

	// Create multiple messages
	messageIDs := []string{"msg1", "msg2", "msg3", "msg4", "msg5"}
	for _, id := range messageIDs {
		msg := &messaging.PendingMessage{
			ID:        id,
			CallUUID:  "batch-call",
			Content:   "batch content",
			CreatedAt: time.Now(),
		}
		err := storage.Store(msg)
		suite.Assert().NoError(err)
	}

	// Test batch delete
	deleteIDs := []string{"msg1", "msg3", "msg5"}
	err := storage.DeleteBatch(deleteIDs)
	suite.Assert().NoError(err)

	// Verify correct messages were deleted
	count, err := storage.Count()
	suite.Assert().NoError(err)
	suite.Assert().Equal(2, count) // Should have msg2 and msg4 remaining

	// Verify specific messages
	for _, id := range deleteIDs {
		msg, err := storage.Retrieve(id)
		suite.Assert().NoError(err)
		suite.Assert().Nil(msg, "Message %s should be deleted", id)
	}

	remainingIDs := []string{"msg2", "msg4"}
	for _, id := range remainingIDs {
		msg, err := storage.Retrieve(id)
		suite.Assert().NoError(err)
		suite.Assert().NotNil(msg, "Message %s should remain", id)
	}
}

func (suite *MessagingTestSuite) TearDownSuite() {
	suite.logger.Info("Messaging unit tests completed")
}

// TestMessagingComponents runs the complete messaging test suite
func TestMessagingComponents(t *testing.T) {
	suite.Run(t, new(MessagingTestSuite))
}

// Benchmark tests for messaging components
func BenchmarkMemoryStorage(b *testing.B) {
	storage := messaging.NewMemoryMessageStorage()
	
	// Prepare test messages
	messages := make([]*messaging.PendingMessage, 1000)
	for i := 0; i < 1000; i++ {
		messages[i] = &messaging.PendingMessage{
			ID:        fmt.Sprintf("msg-%d", i),
			CallUUID:  fmt.Sprintf("call-%d", i%100),
			Content:   fmt.Sprintf("content-%d", i),
			CreatedAt: time.Now(),
		}
	}

	b.ResetTimer()

	b.Run("Store", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			msg := messages[i%len(messages)]
			storage.Store(msg)
		}
	})

	// Store all messages for retrieval benchmark
	for _, msg := range messages {
		storage.Store(msg)
	}

	b.Run("Retrieve", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			id := fmt.Sprintf("msg-%d", i%len(messages))
			storage.Retrieve(id)
		}
	})

	b.Run("List", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			storage.List(100)
		}
	})

	b.Run("Count", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			storage.Count()
		}
	})
}

func BenchmarkCircuitBreaker(b *testing.B) {
	config := messaging.DefaultCircuitBreakerConfig()
	cb := messaging.NewCircuitBreaker(logrus.New(), config)

	b.ResetTimer()

	b.Run("Execute-Success", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cb.Execute(func() error {
				return nil
			})
		}
	})

	b.Run("Execute-Failure", func(b *testing.B) {
		cb.Reset() // Reset to closed state
		for i := 0; i < b.N; i++ {
			cb.Execute(func() error {
				return fmt.Errorf("benchmark failure")
			})
		}
	})
}