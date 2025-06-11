package circuitbreaker

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// State represents the circuit breaker state
type State int

const (
	StateClosed State = iota
	StateHalfOpen
	StateOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name           string
	logger         *logrus.Entry
	config         *Config
	state          State
	failures       int64
	lastFailTime   time.Time
	lastSuccessTime time.Time
	nextAttempt    time.Time
	mutex          sync.RWMutex
	
	// Statistics
	stats          *Statistics
	
	// Callbacks
	onStateChange  func(name string, from State, to State)
}

// Config holds circuit breaker configuration
type Config struct {
	// Failure threshold before opening circuit
	FailureThreshold int64 `json:"failure_threshold" default:"5"`
	
	// Success threshold for closing circuit from half-open
	SuccessThreshold int64 `json:"success_threshold" default:"2"`
	
	// Timeout before attempting to close circuit
	Timeout time.Duration `json:"timeout" default:"60s"`
	
	// Maximum timeout (for exponential backoff)
	MaxTimeout time.Duration `json:"max_timeout" default:"300s"`
	
	// Request timeout for operations
	RequestTimeout time.Duration `json:"request_timeout" default:"30s"`
	
	// Whether to use exponential backoff
	ExponentialBackoff bool `json:"exponential_backoff" default:"true"`
	
	// Failure rate threshold (0.0-1.0)
	FailureRateThreshold float64 `json:"failure_rate_threshold" default:"0.5"`
	
	// Minimum number of requests before evaluating failure rate
	MinRequestThreshold int64 `json:"min_request_threshold" default:"10"`
	
	// Time window for failure rate calculation
	TimeWindow time.Duration `json:"time_window" default:"60s"`
}

// DefaultConfig returns default circuit breaker configuration
func DefaultConfig() *Config {
	return &Config{
		FailureThreshold:     5,
		SuccessThreshold:     2,
		Timeout:              60 * time.Second,
		MaxTimeout:           300 * time.Second,
		RequestTimeout:       30 * time.Second,
		ExponentialBackoff:   true,
		FailureRateThreshold: 0.5,
		MinRequestThreshold:  10,
		TimeWindow:           60 * time.Second,
	}
}

// Statistics tracks circuit breaker performance
type Statistics struct {
	mutex             sync.RWMutex
	TotalRequests     int64     `json:"total_requests"`
	SuccessfulRequests int64    `json:"successful_requests"`
	FailedRequests    int64     `json:"failed_requests"`
	RejectedRequests  int64     `json:"rejected_requests"`
	ConsecutiveFailures int64   `json:"consecutive_failures"`
	ConsecutiveSuccesses int64  `json:"consecutive_successes"`
	LastFailureTime   time.Time `json:"last_failure_time"`
	LastSuccessTime   time.Time `json:"last_success_time"`
	StateTransitions  int64     `json:"state_transitions"`
	
	// Time-windowed statistics
	WindowRequests    []RequestRecord `json:"-"`
	WindowStart       time.Time       `json:"window_start"`
}

// RequestRecord represents a request within the time window
type RequestRecord struct {
	Timestamp time.Time
	Success   bool
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config *Config, logger *logrus.Logger) *CircuitBreaker {
	if config == nil {
		config = DefaultConfig()
	}
	
	return &CircuitBreaker{
		name:   name,
		logger: logger.WithField("circuit_breaker", name),
		config: config,
		state:  StateClosed,
		stats:  &Statistics{
			WindowStart: time.Now(),
		},
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	// Check if circuit allows execution
	if !cb.allowRequest() {
		cb.recordRejection()
		return NewCircuitBreakerOpenError(cb.name, cb.state)
	}
	
	// Create timeout context if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cb.config.RequestTimeout)
		defer cancel()
	}
	
	// Execute the function
	err := fn(ctx)
	
	// Record the result
	if err != nil {
		cb.recordFailure(err)
		return err
	}
	
	cb.recordSuccess()
	return nil
}

// ExecuteWithFallback runs the function with circuit breaker protection and fallback
func (cb *CircuitBreaker) ExecuteWithFallback(ctx context.Context, fn func(ctx context.Context) error, fallback func(ctx context.Context) error) error {
	err := cb.Execute(ctx, fn)
	if err != nil {
		if IsCircuitBreakerError(err) && fallback != nil {
			cb.logger.WithError(err).Debug("Circuit breaker open, executing fallback")
			return fallback(ctx)
		}
		return err
	}
	return nil
}

// allowRequest checks if a request should be allowed
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	now := time.Now()
	
	switch cb.state {
	case StateClosed:
		return true
		
	case StateOpen:
		if now.After(cb.nextAttempt) {
			cb.setState(StateHalfOpen)
			return true
		}
		return false
		
	case StateHalfOpen:
		return true
		
	default:
		return false
	}
}

// recordSuccess records a successful execution
func (cb *CircuitBreaker) recordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.failures = 0
	cb.lastSuccessTime = time.Now()
	
	cb.stats.mutex.Lock()
	cb.stats.TotalRequests++
	cb.stats.SuccessfulRequests++
	cb.stats.ConsecutiveFailures = 0
	cb.stats.ConsecutiveSuccesses++
	cb.stats.LastSuccessTime = cb.lastSuccessTime
	cb.addWindowRecord(cb.lastSuccessTime, true)
	cb.stats.mutex.Unlock()
	
	if cb.state == StateHalfOpen {
		if cb.stats.ConsecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.setState(StateClosed)
		}
	}
}

// recordFailure records a failed execution
func (cb *CircuitBreaker) recordFailure(err error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.failures++
	cb.lastFailTime = time.Now()
	
	cb.stats.mutex.Lock()
	cb.stats.TotalRequests++
	cb.stats.FailedRequests++
	cb.stats.ConsecutiveFailures++
	cb.stats.ConsecutiveSuccesses = 0
	cb.stats.LastFailureTime = cb.lastFailTime
	cb.addWindowRecord(cb.lastFailTime, false)
	cb.stats.mutex.Unlock()
	
	if cb.shouldTrip() {
		cb.setState(StateOpen)
	}
	
	cb.logger.WithError(err).WithFields(logrus.Fields{
		"failures": cb.failures,
		"state":    cb.state.String(),
	}).Debug("Circuit breaker recorded failure")
}

// recordRejection records a rejected request
func (cb *CircuitBreaker) recordRejection() {
	cb.stats.mutex.Lock()
	defer cb.stats.mutex.Unlock()
	
	cb.stats.RejectedRequests++
}

// shouldTrip determines if the circuit should trip to open state
func (cb *CircuitBreaker) shouldTrip() bool {
	// Check consecutive failures threshold
	if cb.failures >= cb.config.FailureThreshold {
		return true
	}
	
	// Check failure rate threshold
	if cb.stats.TotalRequests >= cb.config.MinRequestThreshold {
		failureRate := cb.getFailureRate()
		if failureRate >= cb.config.FailureRateThreshold {
			return true
		}
	}
	
	return false
}

// getFailureRate calculates the current failure rate within the time window
func (cb *CircuitBreaker) getFailureRate() float64 {
	now := time.Now()
	windowStart := now.Add(-cb.config.TimeWindow)
	
	var totalRequests, failedRequests int64
	
	for _, record := range cb.stats.WindowRequests {
		if record.Timestamp.After(windowStart) {
			totalRequests++
			if !record.Success {
				failedRequests++
			}
		}
	}
	
	if totalRequests == 0 {
		return 0.0
	}
	
	return float64(failedRequests) / float64(totalRequests)
}

// addWindowRecord adds a request record to the time window
func (cb *CircuitBreaker) addWindowRecord(timestamp time.Time, success bool) {
	record := RequestRecord{
		Timestamp: timestamp,
		Success:   success,
	}
	
	cb.stats.WindowRequests = append(cb.stats.WindowRequests, record)
	
	// Clean old records outside the time window
	windowStart := timestamp.Add(-cb.config.TimeWindow)
	validRecords := make([]RequestRecord, 0, len(cb.stats.WindowRequests))
	
	for _, r := range cb.stats.WindowRequests {
		if r.Timestamp.After(windowStart) {
			validRecords = append(validRecords, r)
		}
	}
	
	cb.stats.WindowRequests = validRecords
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(newState State) {
	if cb.state == newState {
		return
	}
	
	oldState := cb.state
	cb.state = newState
	
	now := time.Now()
	
	switch newState {
	case StateOpen:
		timeout := cb.config.Timeout
		if cb.config.ExponentialBackoff {
			// Exponential backoff based on consecutive failures
			backoffMultiplier := time.Duration(1 << uint(min(cb.failures-1, 10))) // Cap at 2^10
			timeout = cb.config.Timeout * backoffMultiplier
			if timeout > cb.config.MaxTimeout {
				timeout = cb.config.MaxTimeout
			}
		}
		cb.nextAttempt = now.Add(timeout)
		
	case StateClosed:
		cb.failures = 0
		cb.nextAttempt = time.Time{}
		
	case StateHalfOpen:
		// Reset success counter for half-open state
		cb.stats.mutex.Lock()
		cb.stats.ConsecutiveSuccesses = 0
		cb.stats.mutex.Unlock()
	}
	
	cb.stats.mutex.Lock()
	cb.stats.StateTransitions++
	cb.stats.mutex.Unlock()
	
	cb.logger.WithFields(logrus.Fields{
		"from_state": oldState.String(),
		"to_state":   newState.String(),
		"failures":   cb.failures,
	}).Info("Circuit breaker state changed")
	
	// Call state change callback if set
	if cb.onStateChange != nil {
		go cb.onStateChange(cb.name, oldState, newState)
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() State {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetStatistics returns circuit breaker statistics
func (cb *CircuitBreaker) GetStatistics() *Statistics {
	cb.stats.mutex.RLock()
	defer cb.stats.mutex.RUnlock()
	
	// Create a copy to avoid race conditions
	statsCopy := *cb.stats
	
	// Copy the window records slice
	statsCopy.WindowRequests = make([]RequestRecord, len(cb.stats.WindowRequests))
	copy(statsCopy.WindowRequests, cb.stats.WindowRequests)
	
	return &statsCopy
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.setState(StateClosed)
	cb.failures = 0
	cb.lastFailTime = time.Time{}
	cb.lastSuccessTime = time.Time{}
	cb.nextAttempt = time.Time{}
	
	cb.stats.mutex.Lock()
	cb.stats.TotalRequests = 0
	cb.stats.SuccessfulRequests = 0
	cb.stats.FailedRequests = 0
	cb.stats.RejectedRequests = 0
	cb.stats.ConsecutiveFailures = 0
	cb.stats.ConsecutiveSuccesses = 0
	cb.stats.LastFailureTime = time.Time{}
	cb.stats.LastSuccessTime = time.Time{}
	cb.stats.WindowRequests = nil
	cb.stats.WindowStart = time.Now()
	cb.stats.mutex.Unlock()
	
	cb.logger.Info("Circuit breaker reset")
}

// SetStateChangeCallback sets a callback for state changes
func (cb *CircuitBreaker) SetStateChangeCallback(callback func(name string, from State, to State)) {
	cb.onStateChange = callback
}

// GetName returns the circuit breaker name
func (cb *CircuitBreaker) GetName() string {
	return cb.name
}

// IsOpen returns true if the circuit is open
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.GetState() == StateOpen
}

// IsClosed returns true if the circuit is closed
func (cb *CircuitBreaker) IsClosed() bool {
	return cb.GetState() == StateClosed
}

// IsHalfOpen returns true if the circuit is half-open
func (cb *CircuitBreaker) IsHalfOpen() bool {
	return cb.GetState() == StateHalfOpen
}

// min returns the minimum of two integers
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}