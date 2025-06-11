package messaging

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/circuitbreaker"
)

// AMQPCircuitBreakerWrapper wraps AMQP client with circuit breaker protection
type AMQPCircuitBreakerWrapper struct {
	client         *AMQPClient
	circuitBreaker *circuitbreaker.CircuitBreaker
	logger         *logrus.Entry
	name           string
	
	// Fallback options
	enableFallbackLogging   bool
	fallbackLogLevel       logrus.Level
	fallbackMetrics        *AMQPFallbackMetrics
}

// AMQPFallbackMetrics tracks fallback statistics
type AMQPFallbackMetrics struct {
	FallbackActivations int64     `json:"fallback_activations"`
	MessagesDropped     int64     `json:"messages_dropped"`
	MessagesLogged      int64     `json:"messages_logged"`
	LastFallbackTime    time.Time `json:"last_fallback_time"`
}

// NewAMQPCircuitBreakerWrapper creates a new AMQP circuit breaker wrapper
func NewAMQPCircuitBreakerWrapper(client *AMQPClient, cbManager *circuitbreaker.Manager, logger *logrus.Logger) *AMQPCircuitBreakerWrapper {
	name := "amqp_messaging"
	
	// Get circuit breaker with AMQP-optimized config
	cb := cbManager.GetCircuitBreaker(name, circuitbreaker.AMQPConfig())
	
	return &AMQPCircuitBreakerWrapper{
		client:         client,
		circuitBreaker: cb,
		logger: logger.WithFields(logrus.Fields{
			"component":    "amqp_circuit_breaker",
			"circuit_name": name,
		}),
		name:                    name,
		enableFallbackLogging:   true,
		fallbackLogLevel:       logrus.WarnLevel,
		fallbackMetrics:        &AMQPFallbackMetrics{},
	}
}

// Connect connects to AMQP with circuit breaker protection
func (w *AMQPCircuitBreakerWrapper) Connect() error {
	return w.circuitBreaker.Execute(context.Background(), func(ctx context.Context) error {
		if err := w.client.Connect(); err != nil {
			w.logger.WithError(err).Error("Failed to connect to AMQP")
			return err
		}
		
		w.logger.Info("Connected to AMQP successfully")
		return nil
	})
}

// Disconnect disconnects from AMQP
func (w *AMQPCircuitBreakerWrapper) Disconnect() {
	w.client.Disconnect()
}

// IsConnected checks if AMQP is connected with circuit breaker awareness
func (w *AMQPCircuitBreakerWrapper) IsConnected() bool {
	// If circuit is open, consider as not connected
	if w.circuitBreaker.IsOpen() {
		return false
	}
	
	return w.client.IsConnected()
}

// PublishTranscription publishes transcription with circuit breaker protection
func (w *AMQPCircuitBreakerWrapper) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	return w.circuitBreaker.ExecuteWithFallback(
		context.Background(),
		// Primary function
		func(ctx context.Context) error {
			start := time.Now()
			err := w.client.PublishTranscription(transcription, callUUID, metadata)
			duration := time.Since(start)
			
			if err != nil {
				w.logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"duration":  duration,
					"state":     w.circuitBreaker.GetState().String(),
				}).Error("AMQP publish failed")
				return err
			}
			
			w.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"duration":  duration,
				"state":     w.circuitBreaker.GetState().String(),
			}).Debug("AMQP publish succeeded")
			
			return nil
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.FallbackActivations++
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			if w.enableFallbackLogging {
				w.fallbackMetrics.MessagesLogged++
				
				// Log the message as fallback
				w.logger.WithFields(logrus.Fields{
					"call_uuid":      callUUID,
					"transcription":  transcription,
					"metadata":       metadata,
					"fallback_reason": "amqp_circuit_breaker_open",
					"timestamp":      time.Now(),
				}).Log(w.fallbackLogLevel, "AMQP unavailable, logging transcription as fallback")
				
				return nil // Don't return error for successful fallback
			} else {
				w.fallbackMetrics.MessagesDropped++
				
				w.logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"reason":    "amqp_circuit_breaker_open",
				}).Warn("Dropping transcription message due to AMQP circuit breaker")
				
				return nil // Don't return error to avoid blocking transcription flow
			}
		},
	)
}

// PublishToDeadLetterQueue publishes to dead letter queue with circuit breaker protection
func (w *AMQPCircuitBreakerWrapper) PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error {
	return w.circuitBreaker.ExecuteWithFallback(
		context.Background(),
		// Primary function
		func(ctx context.Context) error {
			return w.client.PublishToDeadLetterQueue(content, callUUID, metadata)
		},
		// Fallback function
		func(ctx context.Context) error {
			// For dead letter queue, we log but don't fail
			w.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"content":   content,
				"metadata":  metadata,
				"reason":    "amqp_circuit_breaker_open",
			}).Error("Unable to publish to dead letter queue, AMQP circuit breaker open")
			
			return nil // Don't fail the operation
		},
	)
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (w *AMQPCircuitBreakerWrapper) GetCircuitBreakerStats() *circuitbreaker.Statistics {
	return w.circuitBreaker.GetStatistics()
}

// GetCircuitBreakerState returns the current circuit breaker state
func (w *AMQPCircuitBreakerWrapper) GetCircuitBreakerState() circuitbreaker.State {
	return w.circuitBreaker.GetState()
}

// GetFallbackMetrics returns fallback metrics
func (w *AMQPCircuitBreakerWrapper) GetFallbackMetrics() *AMQPFallbackMetrics {
	return w.fallbackMetrics
}

// ResetCircuitBreaker resets the circuit breaker
func (w *AMQPCircuitBreakerWrapper) ResetCircuitBreaker() {
	w.circuitBreaker.Reset()
	w.logger.Info("AMQP circuit breaker reset")
}

// SetFallbackLogging enables or disables fallback logging
func (w *AMQPCircuitBreakerWrapper) SetFallbackLogging(enabled bool, level logrus.Level) {
	w.enableFallbackLogging = enabled
	w.fallbackLogLevel = level
	
	w.logger.WithFields(logrus.Fields{
		"enabled": enabled,
		"level":   level.String(),
	}).Info("AMQP fallback logging configured")
}