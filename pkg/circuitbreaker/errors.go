package circuitbreaker

import (
	"fmt"
	"time"
)

// CircuitBreakerError represents an error from circuit breaker
type CircuitBreakerError struct {
	CircuitName string
	State       State
	Message     string
	Timestamp   time.Time
}

func (e *CircuitBreakerError) Error() string {
	return fmt.Sprintf("circuit breaker '%s' is %s: %s", e.CircuitName, e.State.String(), e.Message)
}

// NewCircuitBreakerOpenError creates an error for when circuit is open
func NewCircuitBreakerOpenError(name string, state State) *CircuitBreakerError {
	return &CircuitBreakerError{
		CircuitName: name,
		State:       state,
		Message:     "circuit breaker is open, request rejected",
		Timestamp:   time.Now(),
	}
}

// IsCircuitBreakerError checks if an error is a circuit breaker error
func IsCircuitBreakerError(err error) bool {
	_, ok := err.(*CircuitBreakerError)
	return ok
}

// IsCircuitBreakerOpenError checks if an error is specifically a circuit breaker open error
func IsCircuitBreakerOpenError(err error) bool {
	if cbErr, ok := err.(*CircuitBreakerError); ok {
		return cbErr.State == StateOpen
	}
	return false
}