package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Mock provider for testing
type mockProvider struct {
	name           string
	initErr        error
	streamErr      error
	healthCheckErr error
	initialized    bool
}

func (m *mockProvider) Initialize() error {
	if m.initErr != nil {
		return m.initErr
	}
	m.initialized = true
	return nil
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	return m.streamErr
}

func (m *mockProvider) HealthCheck(ctx context.Context) error {
	return m.healthCheckErr
}

func TestHealthMonitor_RegisterProvider(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	monitor := NewHealthMonitor(logger, 100*time.Millisecond)

	provider := &mockProvider{name: "test-provider"}
	monitor.RegisterProvider("test-provider", provider)

	// Verify provider is registered
	assert.Contains(t, monitor.providers, "test-provider")
	assert.Contains(t, monitor.healthStatus, "test-provider")
	assert.Contains(t, monitor.circuitBreakerStates, "test-provider")

	// Verify initial health status
	health := monitor.healthStatus["test-provider"]
	assert.True(t, health.Healthy)
	assert.Equal(t, "closed", health.CircuitBreakerState)
}

func TestHealthMonitor_HealthCheck(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)

	t.Run("healthy provider", func(t *testing.T) {
		provider := &mockProvider{
			name:           "healthy-provider",
			healthCheckErr: nil,
		}
		monitor.RegisterProvider("healthy-provider", provider)
		
		// Run health check
		monitor.checkProvider("healthy-provider", provider)
		
		// Verify provider is marked healthy
		health, exists := monitor.GetHealthStatus("healthy-provider")
		assert.True(t, exists)
		assert.True(t, health.Healthy)
		assert.Equal(t, 0, health.ConsecutiveFails)
	})

	t.Run("unhealthy provider", func(t *testing.T) {
		provider := &mockProvider{
			name:           "unhealthy-provider",
			healthCheckErr: errors.New("connection failed"),
		}
		monitor.RegisterProvider("unhealthy-provider", provider)
		
		// Run health checks until unhealthy threshold
		for i := 0; i < monitor.unhealthyThreshold; i++ {
			monitor.checkProvider("unhealthy-provider", provider)
		}
		
		// Verify provider is marked unhealthy
		health, exists := monitor.GetHealthStatus("unhealthy-provider")
		assert.True(t, exists)
		assert.False(t, health.Healthy)
		assert.Equal(t, monitor.unhealthyThreshold, health.ConsecutiveFails)
		assert.NotNil(t, health.LastError)
	})

	t.Run("provider recovery", func(t *testing.T) {
		provider := &mockProvider{
			name:           "recovering-provider",
			healthCheckErr: errors.New("initial error"),
		}
		monitor.RegisterProvider("recovering-provider", provider)
		
		// Make provider unhealthy
		for i := 0; i < monitor.unhealthyThreshold; i++ {
			monitor.checkProvider("recovering-provider", provider)
		}
		
		health, _ := monitor.GetHealthStatus("recovering-provider")
		assert.False(t, health.Healthy)
		
		// Now make provider healthy
		provider.healthCheckErr = nil
		
		// Run successful health checks
		for i := 0; i <= monitor.recoveryThreshold; i++ {
			monitor.checkProvider("recovering-provider", provider)
		}
		
		// Verify provider recovered
		health, _ = monitor.GetHealthStatus("recovering-provider")
		assert.True(t, health.Healthy)
		assert.Equal(t, 0, health.ConsecutiveFails)
	})
}

func TestHealthMonitor_CircuitBreaker(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)
	monitor.circuitBreakerThreshold = 3
	monitor.circuitBreakerTimeout = 100 * time.Millisecond

	provider := &mockProvider{
		name:           "circuit-test",
		healthCheckErr: errors.New("service unavailable"),
	}
	monitor.RegisterProvider("circuit-test", provider)

	t.Run("circuit opens after threshold", func(t *testing.T) {
		// Trigger failures to open circuit
		for i := 0; i < monitor.circuitBreakerThreshold; i++ {
			monitor.checkProvider("circuit-test", provider)
		}
		
		cb := monitor.circuitBreakerStates["circuit-test"]
		cb.mu.Lock()
		state := cb.state
		cb.mu.Unlock()
		
		assert.Equal(t, "open", state)
		
		health, _ := monitor.GetHealthStatus("circuit-test")
		assert.Equal(t, "open", health.CircuitBreakerState)
	})

	t.Run("circuit enters half-open after timeout", func(t *testing.T) {
		// Wait for circuit breaker timeout
		time.Sleep(monitor.circuitBreakerTimeout + 10*time.Millisecond)
		
		// Provider is now healthy
		provider.healthCheckErr = nil
		
		// Run a check to trigger half-open state
		monitor.checkProvider("circuit-test", provider)
		
		cb := monitor.circuitBreakerStates["circuit-test"]
		cb.mu.Lock()
		state := cb.state
		cb.mu.Unlock()
		
		assert.Equal(t, "half-open", state)
	})

	t.Run("circuit closes after successful recovery", func(t *testing.T) {
		// Continue successful checks
		for i := 0; i < monitor.recoveryThreshold; i++ {
			monitor.checkProvider("circuit-test", provider)
		}
		
		cb := monitor.circuitBreakerStates["circuit-test"]
		cb.mu.Lock()
		state := cb.state
		cb.mu.Unlock()
		
		assert.Equal(t, "closed", state)
	})
}

func TestHealthMonitor_GetHealthyProviders(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)

	// Register multiple providers
	healthy1 := &mockProvider{name: "healthy1"}
	healthy2 := &mockProvider{name: "healthy2"}
	unhealthy := &mockProvider{name: "unhealthy", healthCheckErr: errors.New("error")}
	
	monitor.RegisterProvider("healthy1", healthy1)
	monitor.RegisterProvider("healthy2", healthy2)
	monitor.RegisterProvider("unhealthy", unhealthy)
	
	// Make one unhealthy
	for i := 0; i < monitor.unhealthyThreshold; i++ {
		monitor.checkProvider("unhealthy", unhealthy)
	}
	
	healthyProviders := monitor.GetHealthyProviders()
	assert.Contains(t, healthyProviders, "healthy1")
	assert.Contains(t, healthyProviders, "healthy2")
	assert.NotContains(t, healthyProviders, "unhealthy")
}

func TestHealthMonitor_GetProviderScore(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)

	provider := &mockProvider{name: "scored-provider"}
	monitor.RegisterProvider("scored-provider", provider)
	
	// Perfect health should give high score
	score := monitor.GetProviderScore("scored-provider")
	assert.Greater(t, score, 90.0)
	
	// Add some failures to reduce score
	health := monitor.healthStatus["scored-provider"]
	health.ErrorRate = 0.3
	health.AverageLatency = 600 * time.Millisecond
	
	score = monitor.GetProviderScore("scored-provider")
	assert.Less(t, score, 70.0)
	
	// Open circuit breaker should give 0 score
	cb := monitor.circuitBreakerStates["scored-provider"]
	cb.mu.Lock()
	cb.state = "open"
	cb.mu.Unlock()
	
	score = monitor.GetProviderScore("scored-provider")
	assert.Equal(t, 0.0, score)
}

func TestHealthMonitor_GetBestProvider(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)

	// Register providers with different health states
	excellent := &mockProvider{name: "excellent"}
	good := &mockProvider{name: "good"}
	poor := &mockProvider{name: "poor", healthCheckErr: errors.New("occasional error")}
	
	monitor.RegisterProvider("excellent", excellent)
	monitor.RegisterProvider("good", good)
	monitor.RegisterProvider("poor", poor)
	
	// Set different health metrics
	monitor.healthStatus["excellent"].ErrorRate = 0.01
	monitor.healthStatus["excellent"].AverageLatency = 100 * time.Millisecond
	
	monitor.healthStatus["good"].ErrorRate = 0.1
	monitor.healthStatus["good"].AverageLatency = 300 * time.Millisecond
	
	monitor.healthStatus["poor"].ErrorRate = 0.4
	monitor.healthStatus["poor"].AverageLatency = 800 * time.Millisecond
	
	// Get best provider
	best, err := monitor.GetBestProvider([]string{})
	assert.NoError(t, err)
	assert.Equal(t, "excellent", best)
	
	// Exclude excellent, should get good
	best, err = monitor.GetBestProvider([]string{"excellent"})
	assert.NoError(t, err)
	assert.Equal(t, "good", best)
	
	// Exclude all healthy ones
	monitor.healthStatus["excellent"].Healthy = false
	monitor.healthStatus["good"].Healthy = false
	monitor.healthStatus["poor"].Healthy = false
	
	_, err = monitor.GetBestProvider([]string{})
	assert.Error(t, err)
}

func TestHealthMonitor_StartStop(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 50*time.Millisecond)
	
	provider := &mockProvider{name: "test"}
	monitor.RegisterProvider("test", provider)
	
	// Start monitoring
	monitor.Start()
	
	// Let it run for a bit
	time.Sleep(150 * time.Millisecond)
	
	// Stop monitoring
	monitor.Stop()
	
	// Verify health checks ran
	health, _ := monitor.GetHealthStatus("test")
	assert.NotNil(t, health)
	assert.True(t, health.LastCheck.After(health.LastSuccess.Add(-time.Second)))
}

func TestHealthMonitor_ConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	monitor := NewHealthMonitor(logger, 10*time.Millisecond)
	
	// Register multiple providers
	for i := 0; i < 10; i++ {
		provider := &mockProvider{name: fmt.Sprintf("provider-%d", i)}
		monitor.RegisterProvider(provider.name, provider)
	}
	
	// Start concurrent operations
	done := make(chan bool)
	
	// Goroutine 1: Continuous health checks
	go func() {
		for i := 0; i < 100; i++ {
			monitor.checkAllProviders()
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()
	
	// Goroutine 2: Read health status
	go func() {
		for i := 0; i < 100; i++ {
			monitor.GetAllHealthStatus()
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()
	
	// Goroutine 3: Get best provider
	go func() {
		for i := 0; i < 100; i++ {
			monitor.GetBestProvider([]string{})
			time.Sleep(5 * time.Millisecond)
		}
		done <- true
	}()
	
	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}
	
	// No panic means concurrent access is safe
	assert.True(t, true)
}