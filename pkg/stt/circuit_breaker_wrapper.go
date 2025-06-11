package stt

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/circuitbreaker"
)

// CircuitBreakerWrapper wraps an STT provider with circuit breaker protection
type CircuitBreakerWrapper struct {
	provider        Provider
	circuitBreaker  *circuitbreaker.CircuitBreaker
	fallbackProvider Provider
	logger          *logrus.Entry
	name            string
}

// NewCircuitBreakerWrapper creates a new circuit breaker wrapper for STT providers
func NewCircuitBreakerWrapper(provider Provider, cbManager *circuitbreaker.Manager, logger *logrus.Logger, fallbackProvider Provider) *CircuitBreakerWrapper {
	name := fmt.Sprintf("stt_%s", provider.Name())
	
	// Get circuit breaker with STT-optimized config
	cb := cbManager.GetCircuitBreaker(name, circuitbreaker.STTConfig())
	
	return &CircuitBreakerWrapper{
		provider:         provider,
		circuitBreaker:   cb,
		fallbackProvider: fallbackProvider,
		logger:          logger.WithFields(logrus.Fields{
			"component":      "stt_circuit_breaker",
			"provider":       provider.Name(),
			"circuit_name":   name,
		}),
		name: name,
	}
}

// Initialize initializes the wrapped provider
func (w *CircuitBreakerWrapper) Initialize() error {
	return w.circuitBreaker.Execute(context.Background(), func(ctx context.Context) error {
		if err := w.provider.Initialize(); err != nil {
			w.logger.WithError(err).Error("Failed to initialize STT provider")
			return err
		}
		
		w.logger.Info("STT provider initialized successfully")
		return nil
	})
}

// Name returns the provider name
func (w *CircuitBreakerWrapper) Name() string {
	return w.provider.Name()
}

// StreamToText streams audio data with circuit breaker protection
func (w *CircuitBreakerWrapper) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	return w.circuitBreaker.ExecuteWithFallback(ctx, 
		// Primary function
		func(ctx context.Context) error {
			start := time.Now()
			err := w.provider.StreamToText(ctx, audioStream, callUUID)
			duration := time.Since(start)
			
			if err != nil {
				w.logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"duration":  duration,
					"state":     w.circuitBreaker.GetState().String(),
				}).Error("STT provider failed")
				return err
			}
			
			w.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"duration":  duration,
				"state":     w.circuitBreaker.GetState().String(),
			}).Debug("STT provider succeeded")
			
			return nil
		},
		// Fallback function
		func(ctx context.Context) error {
			if w.fallbackProvider == nil {
				return fmt.Errorf("no fallback provider available for %s", w.provider.Name())
			}
			
			w.logger.WithFields(logrus.Fields{
				"call_uuid":         callUUID,
				"primary_provider":  w.provider.Name(),
				"fallback_provider": w.fallbackProvider.Name(),
			}).Warn("Using fallback STT provider due to circuit breaker")
			
			return w.fallbackProvider.StreamToText(ctx, audioStream, callUUID)
		},
	)
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (w *CircuitBreakerWrapper) GetCircuitBreakerStats() *circuitbreaker.Statistics {
	return w.circuitBreaker.GetStatistics()
}

// GetCircuitBreakerState returns the current circuit breaker state
func (w *CircuitBreakerWrapper) GetCircuitBreakerState() circuitbreaker.State {
	return w.circuitBreaker.GetState()
}

// IsCircuitBreakerOpen returns true if the circuit breaker is open
func (w *CircuitBreakerWrapper) IsCircuitBreakerOpen() bool {
	return w.circuitBreaker.IsOpen()
}

// ResetCircuitBreaker resets the circuit breaker
func (w *CircuitBreakerWrapper) ResetCircuitBreaker() {
	w.circuitBreaker.Reset()
	w.logger.Info("Circuit breaker reset")
}