package stt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/metrics"
)

// HealthCheckProvider extends Provider with health check capabilities
type HealthCheckProvider interface {
	Provider
	// HealthCheck performs a health check on the provider
	HealthCheck(ctx context.Context) error
}

// ProviderHealth represents the health status of a provider
type ProviderHealth struct {
	Name              string
	Healthy           bool
	LastCheck         time.Time
	LastSuccess       time.Time
	ConsecutiveFails  int
	ResponseTime      time.Duration
	ErrorRate         float64
	SuccessCount      int64
	FailureCount      int64
	AverageLatency    time.Duration
	P95Latency        time.Duration
	P99Latency        time.Duration
	LastError         error
	CircuitBreakerState string
}

// HealthMonitor monitors provider health
type HealthMonitor struct {
	logger           *logrus.Logger
	providers        map[string]Provider
	healthStatus     map[string]*ProviderHealth
	mu               sync.RWMutex
	checkInterval    time.Duration
	unhealthyThreshold int
	recoveryThreshold int
	stopChan         chan struct{}
	wg               sync.WaitGroup
	
	// Circuit breaker settings
	circuitBreakerThreshold int
	circuitBreakerTimeout    time.Duration
	circuitBreakerStates     map[string]*ProviderCircuitBreaker
}

// ProviderCircuitBreaker implements circuit breaker pattern for providers
type ProviderCircuitBreaker struct {
	state           string // "closed", "open", "half-open"
	failureCount    int
	lastFailureTime time.Time
	openedAt        time.Time
	halfOpenTests   int
	mu              sync.Mutex
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(logger *logrus.Logger, checkInterval time.Duration) *HealthMonitor {
	if checkInterval == 0 {
		checkInterval = 30 * time.Second
	}
	
	return &HealthMonitor{
		logger:                   logger,
		providers:                make(map[string]Provider),
		healthStatus:             make(map[string]*ProviderHealth),
		checkInterval:            checkInterval,
		unhealthyThreshold:       3,
		recoveryThreshold:        2,
		circuitBreakerThreshold:  5,
		circuitBreakerTimeout:    60 * time.Second,
		circuitBreakerStates:     make(map[string]*ProviderCircuitBreaker),
		stopChan:                 make(chan struct{}),
	}
}

// RegisterProvider registers a provider for health monitoring
func (h *HealthMonitor) RegisterProvider(name string, provider Provider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	h.providers[name] = provider
	h.healthStatus[name] = &ProviderHealth{
		Name:         name,
		Healthy:      true,
		LastCheck:    time.Now(),
		LastSuccess:  time.Now(),
		CircuitBreakerState: "closed",
	}
	h.circuitBreakerStates[name] = &ProviderCircuitBreaker{
		state: "closed",
	}
	
	h.logger.WithField("provider", name).Info("Provider registered for health monitoring")
}

// Start begins health monitoring
func (h *HealthMonitor) Start() {
	h.wg.Add(1)
	go h.monitorLoop()
	h.logger.Info("Health monitoring started")
}

// Stop stops health monitoring
func (h *HealthMonitor) Stop() {
	close(h.stopChan)
	h.wg.Wait()
	h.logger.Info("Health monitoring stopped")
}

// monitorLoop performs periodic health checks
func (h *HealthMonitor) monitorLoop() {
	defer h.wg.Done()
	
	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()
	
	// Initial health check
	h.checkAllProviders()
	
	for {
		select {
		case <-ticker.C:
			h.checkAllProviders()
		case <-h.stopChan:
			return
		}
	}
}

// checkAllProviders checks health of all registered providers
func (h *HealthMonitor) checkAllProviders() {
	h.mu.RLock()
	providers := make(map[string]Provider)
	for name, provider := range h.providers {
		providers[name] = provider
	}
	h.mu.RUnlock()
	
	var wg sync.WaitGroup
	for name, provider := range providers {
		wg.Add(1)
		go func(n string, p Provider) {
			defer wg.Done()
			h.checkProvider(n, p)
		}(name, provider)
	}
	wg.Wait()
}

// checkProvider performs health check on a single provider
func (h *HealthMonitor) checkProvider(name string, provider Provider) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	start := time.Now()
	var err error
	
	// Check if provider implements HealthCheckProvider
	if healthProvider, ok := provider.(HealthCheckProvider); ok {
		err = healthProvider.HealthCheck(ctx)
	} else {
		// Basic connectivity check - try to initialize
		err = provider.Initialize()
	}
	
	responseTime := time.Since(start)
	
	h.updateHealthStatus(name, err, responseTime)
	h.updateCircuitBreaker(name, err)
	
	// Record metrics
	status := "healthy"
	if err != nil {
		status = "unhealthy"
	}
	metrics.RecordProviderHealth(name, status, responseTime.Milliseconds())
}

// updateHealthStatus updates the health status of a provider
func (h *HealthMonitor) updateHealthStatus(name string, err error, responseTime time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	health, exists := h.healthStatus[name]
	if !exists {
		return
	}
	
	health.LastCheck = time.Now()
	health.ResponseTime = responseTime
	
	// Update latency metrics
	if health.AverageLatency == 0 {
		health.AverageLatency = responseTime
	} else {
		health.AverageLatency = (health.AverageLatency + responseTime) / 2
	}
	
	if responseTime > health.P95Latency {
		health.P95Latency = responseTime
	}
	if responseTime > health.P99Latency {
		health.P99Latency = responseTime
	}
	
	if err != nil {
		health.ConsecutiveFails++
		health.FailureCount++
		health.LastError = err
		
		if health.ConsecutiveFails >= h.unhealthyThreshold {
			if health.Healthy {
				h.logger.WithFields(logrus.Fields{
					"provider": name,
					"consecutive_fails": health.ConsecutiveFails,
					"error": err.Error(),
				}).Error("Provider marked as unhealthy")
			}
			health.Healthy = false
		}
	} else {
		health.LastSuccess = time.Now()
		health.SuccessCount++
		health.LastError = nil
		
		if !health.Healthy && health.ConsecutiveFails > 0 {
			health.ConsecutiveFails--
			if health.ConsecutiveFails <= h.recoveryThreshold {
				h.logger.WithField("provider", name).Info("Provider recovered and marked as healthy")
				health.Healthy = true
				health.ConsecutiveFails = 0
			}
		} else {
			health.ConsecutiveFails = 0
			health.Healthy = true
		}
	}
	
	// Calculate error rate
	total := health.SuccessCount + health.FailureCount
	if total > 0 {
		health.ErrorRate = float64(health.FailureCount) / float64(total)
	}
}

// updateCircuitBreaker updates circuit breaker state for a provider
func (h *HealthMonitor) updateCircuitBreaker(name string, err error) {
	h.mu.Lock()
	cb, exists := h.circuitBreakerStates[name]
	h.mu.Unlock()

	if !exists {
		return
	}

	// Track state changes to apply later (avoids holding both cb.mu and h.mu)
	var newCBState string

	cb.mu.Lock()
	switch cb.state {
	case "closed":
		if err != nil {
			cb.failureCount++
			cb.lastFailureTime = time.Now()

			if cb.failureCount >= h.circuitBreakerThreshold {
				cb.state = "open"
				cb.openedAt = time.Now()
				newCBState = "open"
				h.logger.WithFields(logrus.Fields{
					"provider": name,
					"failure_count": cb.failureCount,
				}).Warn("Circuit breaker opened for provider")
			}
		} else {
			cb.failureCount = 0
		}

	case "open":
		if time.Since(cb.openedAt) > h.circuitBreakerTimeout {
			cb.state = "half-open"
			cb.halfOpenTests = 0
			newCBState = "half-open"
			h.logger.WithField("provider", name).Info("Circuit breaker entering half-open state")
		}

	case "half-open":
		cb.halfOpenTests++
		if err != nil {
			cb.state = "open"
			cb.openedAt = time.Now()
			cb.failureCount++
			newCBState = "open"
			h.logger.WithField("provider", name).Warn("Circuit breaker re-opened after half-open test failed")
		} else if cb.halfOpenTests >= h.recoveryThreshold {
			cb.state = "closed"
			cb.failureCount = 0
			newCBState = "closed"
			h.logger.WithField("provider", name).Info("Circuit breaker closed after successful recovery")
		}
	}
	cb.mu.Unlock()

	// Update health status after releasing cb.mu to avoid deadlock
	if newCBState != "" {
		h.mu.Lock()
		if health, exists := h.healthStatus[name]; exists {
			health.CircuitBreakerState = newCBState
		}
		h.mu.Unlock()
	}
}

// GetHealthStatus returns current health status of a provider
func (h *HealthMonitor) GetHealthStatus(name string) (*ProviderHealth, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	health, exists := h.healthStatus[name]
	if !exists {
		return nil, false
	}
	
	// Return a copy to prevent external modifications
	healthCopy := *health
	return &healthCopy, true
}

// GetAllHealthStatus returns health status of all providers
func (h *HealthMonitor) GetAllHealthStatus() map[string]*ProviderHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	result := make(map[string]*ProviderHealth)
	for name, health := range h.healthStatus {
		healthCopy := *health
		result[name] = &healthCopy
	}
	
	return result
}

// IsProviderHealthy checks if a provider is healthy
func (h *HealthMonitor) IsProviderHealthy(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	health, exists := h.healthStatus[name]
	if !exists {
		return false
	}
	
	cb, cbExists := h.circuitBreakerStates[name]
	if !cbExists {
		return health.Healthy
	}
	
	cb.mu.Lock()
	circuitOpen := cb.state == "open"
	cb.mu.Unlock()
	
	return health.Healthy && !circuitOpen
}

// GetHealthyProviders returns list of healthy provider names
func (h *HealthMonitor) GetHealthyProviders() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var healthy []string
	for name := range h.healthStatus {
		if h.isProviderHealthyLocked(name) {
			healthy = append(healthy, name)
		}
	}

	return healthy
}

// isProviderHealthyLocked checks if a provider is healthy (caller must hold at least RLock)
func (h *HealthMonitor) isProviderHealthyLocked(name string) bool {
	health, exists := h.healthStatus[name]
	if !exists {
		return false
	}

	cb, cbExists := h.circuitBreakerStates[name]
	if !cbExists {
		return health.Healthy
	}

	cb.mu.Lock()
	circuitOpen := cb.state == "open"
	cb.mu.Unlock()

	return health.Healthy && !circuitOpen
}

// GetProviderScore calculates a score for provider selection
func (h *HealthMonitor) GetProviderScore(name string) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return h.getProviderScoreLocked(name)
}

// getProviderScoreLocked calculates a score for provider selection (caller must hold at least RLock)
func (h *HealthMonitor) getProviderScoreLocked(name string) float64 {
	health, exists := h.healthStatus[name]
	if !exists || !health.Healthy {
		return 0
	}

	// Calculate score based on various factors
	score := 100.0

	// Deduct for error rate
	score -= health.ErrorRate * 50

	// Deduct for high latency
	if health.AverageLatency > 500*time.Millisecond {
		score -= 20
	} else if health.AverageLatency > 200*time.Millisecond {
		score -= 10
	}

	// Deduct for recent failures
	if time.Since(health.LastSuccess) > 5*time.Minute {
		score -= 15
	}

	// Bonus for long uptime
	if health.ConsecutiveFails == 0 && health.SuccessCount > 100 {
		score += 10
	}

	// Check circuit breaker state
	if cb, exists := h.circuitBreakerStates[name]; exists {
		cb.mu.Lock()
		if cb.state == "half-open" {
			score -= 30
		} else if cb.state == "open" {
			score = 0
		}
		cb.mu.Unlock()
	}

	if score < 0 {
		score = 0
	}

	return score
}

// GetBestProvider returns the best available provider based on health scores
func (h *HealthMonitor) GetBestProvider(excludeList []string) (string, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	excludeMap := make(map[string]bool)
	for _, name := range excludeList {
		excludeMap[name] = true
	}

	var bestProvider string
	var bestScore float64

	for name := range h.providers {
		if excludeMap[name] {
			continue
		}

		score := h.getProviderScoreLocked(name)
		if score > bestScore {
			bestScore = score
			bestProvider = name
		}
	}

	if bestProvider == "" {
		return "", fmt.Errorf("no healthy providers available")
	}

	return bestProvider, nil
}
