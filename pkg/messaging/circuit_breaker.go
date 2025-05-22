package messaging

import (
	"errors"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// CircuitBreakerState represents the state of a circuit breaker
type CircuitBreakerState int

const (
	// StateClosed indicates the circuit breaker is closed (normal operation)
	StateClosed CircuitBreakerState = iota
	// StateOpen indicates the circuit breaker is open (failing fast)
	StateOpen
	// StateHalfOpen indicates the circuit breaker is half-open (testing recovery)
	StateHalfOpen
)

// String returns the string representation of the circuit breaker state
func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig holds configuration for the circuit breaker
type CircuitBreakerConfig struct {
	MaxFailures         int           // Maximum number of failures before opening
	ResetTimeout        time.Duration // Time to wait before transitioning from open to half-open
	MaxRetries          int           // Maximum number of retries in half-open state
	FailureThreshold    float64       // Failure rate threshold (0.0-1.0)
	MinRequestThreshold int           // Minimum number of requests before evaluating failure rate
	SlidingWindowSize   int           // Size of the sliding window for failure tracking
	SlidingWindowTime   time.Duration // Time window for sliding window
}

// DefaultCircuitBreakerConfig returns default configuration for circuit breaker
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxFailures:         5,
		ResetTimeout:        30 * time.Second,
		MaxRetries:          3,
		FailureThreshold:    0.5, // 50% failure rate
		MinRequestThreshold: 10,
		SlidingWindowSize:   100,
		SlidingWindowTime:   60 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern for AMQP operations
type CircuitBreaker struct {
	logger              *logrus.Logger
	config              CircuitBreakerConfig
	state               CircuitBreakerState
	lastFailureTime     time.Time
	lastSuccessTime     time.Time
	consecutiveFailures int
	halfOpenSuccesses   int
	slidingWindow       *SlidingWindow
	mutex               sync.RWMutex
	onStateChange       func(CircuitBreakerState)
}

// SlidingWindow tracks requests in a time-based sliding window
type SlidingWindow struct {
	requests []RequestResult
	mutex    sync.RWMutex
}

// RequestResult represents the result of a request
type RequestResult struct {
	Timestamp time.Time
	Success   bool
}

// CircuitBreakerMetrics holds metrics for the circuit breaker
type CircuitBreakerMetrics struct {
	State               CircuitBreakerState `json:"state"`
	ConsecutiveFailures int                 `json:"consecutive_failures"`
	TotalRequests       int64               `json:"total_requests"`
	TotalFailures       int64               `json:"total_failures"`
	TotalSuccesses      int64               `json:"total_successes"`
	FailureRate         float64             `json:"failure_rate"`
	LastFailureTime     time.Time           `json:"last_failure_time"`
	LastSuccessTime     time.Time           `json:"last_success_time"`
	StateTransitions    int64               `json:"state_transitions"`
}

// Common circuit breaker errors
var (
	ErrCircuitBreakerOpen     = errors.New("circuit breaker is open")
	ErrCircuitBreakerTimeout  = errors.New("circuit breaker operation timeout")
	ErrMaxRetriesExceeded     = errors.New("circuit breaker max retries exceeded")
)

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(logger *logrus.Logger, config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		logger:        logger,
		config:        config,
		state:         StateClosed,
		slidingWindow: NewSlidingWindow(),
	}
}

// NewSlidingWindow creates a new sliding window
func NewSlidingWindow() *SlidingWindow {
	return &SlidingWindow{
		requests: make([]RequestResult, 0),
	}
}

// Execute executes a function through the circuit breaker
func (cb *CircuitBreaker) Execute(operation func() error) error {
	// Check if circuit breaker allows the request
	if !cb.canExecute() {
		cb.logger.WithField("state", cb.state).Debug("Circuit breaker preventing execution")
		return ErrCircuitBreakerOpen
	}

	// Execute the operation
	startTime := time.Now()
	err := operation()
	duration := time.Since(startTime)

	// Record the result
	cb.recordResult(err == nil, duration)

	return err
}

// canExecute determines if the circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if enough time has passed to transition to half-open
		if time.Since(cb.lastFailureTime) >= cb.config.ResetTimeout {
			cb.transitionToHalfOpen()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

// recordResult records the result of an operation
func (cb *CircuitBreaker) recordResult(success bool, duration time.Duration) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	// Add to sliding window
	cb.slidingWindow.Add(RequestResult{
		Timestamp: time.Now(),
		Success:   success,
	})

	currentTime := time.Now()

	if success {
		cb.lastSuccessTime = currentTime
		cb.onSuccess()
	} else {
		cb.lastFailureTime = currentTime
		cb.onFailure()
	}

	cb.logger.WithFields(logrus.Fields{
		"success":  success,
		"duration": duration,
		"state":    cb.state,
		"failures": cb.consecutiveFailures,
	}).Debug("Circuit breaker recorded operation result")
}

// onSuccess handles successful operations
func (cb *CircuitBreaker) onSuccess() {
	switch cb.state {
	case StateClosed:
		cb.consecutiveFailures = 0
	case StateHalfOpen:
		cb.halfOpenSuccesses++
		if cb.halfOpenSuccesses >= cb.config.MaxRetries {
			cb.transitionToClosed()
		}
	}
}

// onFailure handles failed operations
func (cb *CircuitBreaker) onFailure() {
	cb.consecutiveFailures++

	switch cb.state {
	case StateClosed:
		if cb.shouldOpenCircuit() {
			cb.transitionToOpen()
		}
	case StateHalfOpen:
		cb.transitionToOpen()
	}
}

// shouldOpenCircuit determines if the circuit should be opened
func (cb *CircuitBreaker) shouldOpenCircuit() bool {
	// Check consecutive failures
	if cb.consecutiveFailures >= cb.config.MaxFailures {
		return true
	}

	// Check failure rate in sliding window
	windowStats := cb.slidingWindow.GetStats(cb.config.SlidingWindowTime)
	if windowStats.TotalRequests >= int64(cb.config.MinRequestThreshold) {
		failureRate := float64(windowStats.TotalFailures) / float64(windowStats.TotalRequests)
		if failureRate >= cb.config.FailureThreshold {
			return true
		}
	}

	return false
}

// transitionToClosed transitions the circuit breaker to closed state
func (cb *CircuitBreaker) transitionToClosed() {
	if cb.state != StateClosed {
		cb.logger.WithField("previous_state", cb.state).Info("Circuit breaker transitioning to CLOSED")
		cb.state = StateClosed
		cb.consecutiveFailures = 0
		cb.halfOpenSuccesses = 0
		cb.notifyStateChange()
	}
}

// transitionToOpen transitions the circuit breaker to open state
func (cb *CircuitBreaker) transitionToOpen() {
	if cb.state != StateOpen {
		cb.logger.WithFields(logrus.Fields{
			"previous_state":       cb.state,
			"consecutive_failures": cb.consecutiveFailures,
		}).Warn("Circuit breaker transitioning to OPEN")
		cb.state = StateOpen
		cb.halfOpenSuccesses = 0
		cb.notifyStateChange()
	}
}

// transitionToHalfOpen transitions the circuit breaker to half-open state
func (cb *CircuitBreaker) transitionToHalfOpen() {
	if cb.state != StateHalfOpen {
		cb.logger.WithField("previous_state", cb.state).Info("Circuit breaker transitioning to HALF_OPEN")
		cb.state = StateHalfOpen
		cb.halfOpenSuccesses = 0
		cb.notifyStateChange()
	}
}

// notifyStateChange notifies listeners of state changes
func (cb *CircuitBreaker) notifyStateChange() {
	if cb.onStateChange != nil {
		go cb.onStateChange(cb.state)
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetMetrics returns current circuit breaker metrics
func (cb *CircuitBreaker) GetMetrics() CircuitBreakerMetrics {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	windowStats := cb.slidingWindow.GetStats(cb.config.SlidingWindowTime)
	
	var failureRate float64
	if windowStats.TotalRequests > 0 {
		failureRate = float64(windowStats.TotalFailures) / float64(windowStats.TotalRequests)
	}

	return CircuitBreakerMetrics{
		State:               cb.state,
		ConsecutiveFailures: cb.consecutiveFailures,
		TotalRequests:       windowStats.TotalRequests,
		TotalFailures:       windowStats.TotalFailures,
		TotalSuccesses:      windowStats.TotalSuccesses,
		FailureRate:         failureRate,
		LastFailureTime:     cb.lastFailureTime,
		LastSuccessTime:     cb.lastSuccessTime,
	}
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.logger.Info("Manually resetting circuit breaker")
	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.halfOpenSuccesses = 0
	cb.slidingWindow.Clear()
	cb.notifyStateChange()
}

// SetStateChangeCallback sets a callback for state changes
func (cb *CircuitBreaker) SetStateChangeCallback(callback func(CircuitBreakerState)) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.onStateChange = callback
}

// ForceOpen forces the circuit breaker to open state
func (cb *CircuitBreaker) ForceOpen() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.logger.Info("Forcing circuit breaker to OPEN state")
	cb.transitionToOpen()
}

// SlidingWindowStats represents statistics from the sliding window
type SlidingWindowStats struct {
	TotalRequests  int64
	TotalFailures  int64
	TotalSuccesses int64
}

// Add adds a request result to the sliding window
func (sw *SlidingWindow) Add(result RequestResult) {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	sw.requests = append(sw.requests, result)

	// Keep only recent requests (simple approach - could be optimized)
	if len(sw.requests) > 1000 { // Limit to prevent unbounded growth
		sw.requests = sw.requests[500:] // Keep last 500
	}
}

// GetStats returns statistics for the given time window
func (sw *SlidingWindow) GetStats(windowTime time.Duration) SlidingWindowStats {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	cutoff := time.Now().Add(-windowTime)
	stats := SlidingWindowStats{}

	for _, req := range sw.requests {
		if req.Timestamp.After(cutoff) {
			stats.TotalRequests++
			if req.Success {
				stats.TotalSuccesses++
			} else {
				stats.TotalFailures++
			}
		}
	}

	return stats
}

// Clear clears all requests from the sliding window
func (sw *SlidingWindow) Clear() {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()
	sw.requests = sw.requests[:0]
}

// CircuitBreakerAMQPClient wraps an AMQP client with circuit breaker functionality
type CircuitBreakerAMQPClient struct {
	client         AMQPClientInterface
	circuitBreaker *CircuitBreaker
	logger         *logrus.Logger
}

// NewCircuitBreakerAMQPClient creates a new AMQP client with circuit breaker
func NewCircuitBreakerAMQPClient(client AMQPClientInterface, logger *logrus.Logger, config CircuitBreakerConfig) *CircuitBreakerAMQPClient {
	cb := NewCircuitBreaker(logger, config)
	
	// Set up state change callback
	cb.SetStateChangeCallback(func(state CircuitBreakerState) {
		logger.WithField("state", state).Info("AMQP circuit breaker state changed")
	})

	return &CircuitBreakerAMQPClient{
		client:         client,
		circuitBreaker: cb,
		logger:         logger,
	}
}

// PublishTranscription publishes a transcription with circuit breaker protection
func (cbc *CircuitBreakerAMQPClient) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	return cbc.circuitBreaker.Execute(func() error {
		return cbc.client.PublishTranscription(transcription, callUUID, metadata)
	})
}

// PublishToDeadLetterQueue publishes to dead letter queue with circuit breaker protection
func (cbc *CircuitBreakerAMQPClient) PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error {
	return cbc.circuitBreaker.Execute(func() error {
		return cbc.client.PublishToDeadLetterQueue(content, callUUID, metadata)
	})
}

// IsConnected checks if the underlying AMQP client is connected
func (cbc *CircuitBreakerAMQPClient) IsConnected() bool {
	// If circuit breaker is open, consider it as not connected
	if cbc.circuitBreaker.GetState() == StateOpen {
		return false
	}
	return cbc.client.IsConnected()
}

// Connect connects the underlying AMQP client with circuit breaker protection
func (cbc *CircuitBreakerAMQPClient) Connect() error {
	return cbc.circuitBreaker.Execute(func() error {
		return cbc.client.Connect()
	})
}

// Disconnect disconnects the underlying AMQP client
func (cbc *CircuitBreakerAMQPClient) Disconnect() {
	cbc.client.Disconnect()
}

// GetCircuitBreakerState returns the current circuit breaker state
func (cbc *CircuitBreakerAMQPClient) GetCircuitBreakerState() CircuitBreakerState {
	return cbc.circuitBreaker.GetState()
}

// GetCircuitBreakerMetrics returns circuit breaker metrics
func (cbc *CircuitBreakerAMQPClient) GetCircuitBreakerMetrics() CircuitBreakerMetrics {
	return cbc.circuitBreaker.GetMetrics()
}

// ResetCircuitBreaker manually resets the circuit breaker
func (cbc *CircuitBreakerAMQPClient) ResetCircuitBreaker() {
	cbc.circuitBreaker.Reset()
}

// ForceCircuitBreakerOpen forces the circuit breaker to open state
func (cbc *CircuitBreakerAMQPClient) ForceCircuitBreakerOpen() {
	cbc.circuitBreaker.ForceOpen()
}