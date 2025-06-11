package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager manages multiple circuit breakers
type Manager struct {
	logger          *logrus.Entry
	breakers        map[string]*CircuitBreaker
	mutex           sync.RWMutex
	defaultConfig   *Config
	
	// Monitoring
	monitoringEnabled bool
	monitorInterval   time.Duration
	stopMonitoring    chan struct{}
}

// NewManager creates a new circuit breaker manager
func NewManager(logger *logrus.Logger, defaultConfig *Config) *Manager {
	if defaultConfig == nil {
		defaultConfig = DefaultConfig()
	}
	
	return &Manager{
		logger:            logger.WithField("component", "circuit_breaker_manager"),
		breakers:          make(map[string]*CircuitBreaker),
		defaultConfig:     defaultConfig,
		monitoringEnabled: false,
		monitorInterval:   30 * time.Second,
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

// Execute runs a function with circuit breaker protection
func (m *Manager) Execute(name string, ctx context.Context, fn func(ctx context.Context) error) error {
	breaker := m.GetCircuitBreaker(name, nil)
	return breaker.Execute(ctx, fn)
}

// ExecuteWithFallback runs a function with circuit breaker protection and fallback
func (m *Manager) ExecuteWithFallback(name string, ctx context.Context, fn func(ctx context.Context) error, fallback func(ctx context.Context) error) error {
	breaker := m.GetCircuitBreaker(name, nil)
	return breaker.ExecuteWithFallback(ctx, fn, fallback)
}

// ExecuteWithConfig runs a function with circuit breaker protection using specific config
func (m *Manager) ExecuteWithConfig(name string, config *Config, ctx context.Context, fn func(ctx context.Context) error) error {
	breaker := m.GetCircuitBreaker(name, config)
	return breaker.Execute(ctx, fn)
}

// GetAllStatistics returns statistics for all circuit breakers
func (m *Manager) GetAllStatistics() map[string]*Statistics {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	stats := make(map[string]*Statistics)
	for name, breaker := range m.breakers {
		stats[name] = breaker.GetStatistics()
	}
	
	return stats
}

// GetBreakerNames returns all circuit breaker names
func (m *Manager) GetBreakerNames() []string {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	names := make([]string, 0, len(m.breakers))
	for name := range m.breakers {
		names = append(names, name)
	}
	
	return names
}

// ResetCircuitBreaker resets a specific circuit breaker
func (m *Manager) ResetCircuitBreaker(name string) error {
	m.mutex.RLock()
	breaker, exists := m.breakers[name]
	m.mutex.RUnlock()
	
	if !exists {
		return fmt.Errorf("circuit breaker '%s' not found", name)
	}
	
	breaker.Reset()
	return nil
}

// ResetAllCircuitBreakers resets all circuit breakers
func (m *Manager) ResetAllCircuitBreakers() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	for name, breaker := range m.breakers {
		breaker.Reset()
		m.logger.WithField("circuit_name", name).Info("Reset circuit breaker")
	}
}

// StartMonitoring starts monitoring circuit breakers
func (m *Manager) StartMonitoring() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if m.monitoringEnabled {
		return
	}
	
	m.monitoringEnabled = true
	m.stopMonitoring = make(chan struct{})
	
	go m.monitorLoop()
	
	m.logger.WithField("interval", m.monitorInterval).Info("Started circuit breaker monitoring")
}

// StopMonitoring stops monitoring circuit breakers
func (m *Manager) StopMonitoring() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	if !m.monitoringEnabled {
		return
	}
	
	m.monitoringEnabled = false
	close(m.stopMonitoring)
	
	m.logger.Info("Stopped circuit breaker monitoring")
}

// monitorLoop runs the monitoring loop
func (m *Manager) monitorLoop() {
	ticker := time.NewTicker(m.monitorInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.stopMonitoring:
			return
		case <-ticker.C:
			m.logStatistics()
		}
	}
}

// logStatistics logs statistics for all circuit breakers
func (m *Manager) logStatistics() {
	stats := m.GetAllStatistics()
	
	for name, stat := range stats {
		if stat.TotalRequests > 0 {
			successRate := float64(stat.SuccessfulRequests) / float64(stat.TotalRequests) * 100
			
			m.logger.WithFields(logrus.Fields{
				"circuit_name":         name,
				"state":               m.breakers[name].GetState().String(),
				"total_requests":      stat.TotalRequests,
				"successful_requests": stat.SuccessfulRequests,
				"failed_requests":     stat.FailedRequests,
				"rejected_requests":   stat.RejectedRequests,
				"success_rate":        successRate,
				"consecutive_failures": stat.ConsecutiveFailures,
				"state_transitions":   stat.StateTransitions,
			}).Debug("Circuit breaker statistics")
		}
	}
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

// Shutdown gracefully shuts down the manager
func (m *Manager) Shutdown() {
	m.StopMonitoring()
	
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	// Clear all circuit breakers
	for name := range m.breakers {
		delete(m.breakers, name)
	}
	
	m.logger.Info("Circuit breaker manager shut down")
}