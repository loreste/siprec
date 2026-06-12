package circuitbreaker

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager manages multiple circuit breakers
type Manager struct {
	logger        *logrus.Entry
	breakers      map[string]*CircuitBreaker
	mutex         sync.RWMutex
	defaultConfig *Config
}

// NewManager creates a new circuit breaker manager
func NewManager(logger *logrus.Logger, defaultConfig *Config) *Manager {
	if defaultConfig == nil {
		defaultConfig = DefaultConfig()
	}

	return &Manager{
		logger:        logger.WithField("component", "circuit_breaker_manager"),
		breakers:      make(map[string]*CircuitBreaker),
		defaultConfig: defaultConfig,
	}
}

// GetCircuitBreaker gets or creates a circuit breaker
func (m *Manager) GetCircuitBreaker(name string, config *Config) *CircuitBreaker {
	m.mutex.RLock()
	if breaker, exists := m.breakers[name]; exists {
		m.mutex.RUnlock()
		return breaker
	}
	m.mutex.RUnlock()

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists := m.breakers[name]; exists {
		return breaker
	}

	// Use provided config or default
	if config == nil {
		config = m.defaultConfig
	}

	breaker := NewCircuitBreaker(name, config, m.logger.Logger)

	// Set up state change callback for monitoring
	breaker.SetStateChangeCallback(m.onStateChange)

	m.breakers[name] = breaker

	m.logger.WithFields(logrus.Fields{
		"circuit_name":      name,
		"failure_threshold": config.FailureThreshold,
		"timeout":           config.Timeout,
	}).Info("Created new circuit breaker")

	return breaker
}

// onStateChange handles state change events
func (m *Manager) onStateChange(name string, from State, to State) {
	m.logger.WithFields(logrus.Fields{
		"circuit_name": name,
		"from_state":   from.String(),
		"to_state":     to.String(),
		"timestamp":    time.Now(),
	}).Warn("Circuit breaker state changed")

	// Here you could add additional logic like:
	// - Sending alerts
	// - Publishing metrics
	// - Triggering other system responses
}
