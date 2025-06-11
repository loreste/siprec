package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/circuitbreaker"
)

// HTTPCircuitBreakerWrapper wraps HTTP client with circuit breaker protection
type HTTPCircuitBreakerWrapper struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	logger         *logrus.Entry
	name           string
	
	// Fallback options
	fallbackHandler func(*http.Request) (*http.Response, error)
	
	// Metrics
	fallbackMetrics *HTTPFallbackMetrics
}

// HTTPFallbackMetrics tracks HTTP fallback statistics
type HTTPFallbackMetrics struct {
	FallbackRequests    int64     `json:"fallback_requests"`
	FallbackSuccesses   int64     `json:"fallback_successes"`
	FallbackFailures    int64     `json:"fallback_failures"`
	LastFallbackTime    time.Time `json:"last_fallback_time"`
}

// NewHTTPCircuitBreakerWrapper creates a new HTTP circuit breaker wrapper
func NewHTTPCircuitBreakerWrapper(client *http.Client, serviceName string, cbManager *circuitbreaker.Manager, logger *logrus.Logger) *HTTPCircuitBreakerWrapper {
	name := fmt.Sprintf("http_%s", serviceName)
	
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}
	
	// Get circuit breaker with HTTP-optimized config
	cb := cbManager.GetCircuitBreaker(name, circuitbreaker.HTTPClientConfig())
	
	return &HTTPCircuitBreakerWrapper{
		client:         client,
		circuitBreaker: cb,
		logger: logger.WithFields(logrus.Fields{
			"component":    "http_circuit_breaker",
			"service":      serviceName,
			"circuit_name": name,
		}),
		name:            name,
		fallbackMetrics: &HTTPFallbackMetrics{},
	}
}

// Do executes HTTP request with circuit breaker protection
func (w *HTTPCircuitBreakerWrapper) Do(req *http.Request) (*http.Response, error) {
	var response *http.Response
	var err error
	
	cbErr := w.circuitBreaker.ExecuteWithFallback(req.Context(),
		// Primary function
		func(ctx context.Context) error {
			start := time.Now()
			
			// Clone request with new context to ensure timeout
			reqWithContext := req.WithContext(ctx)
			
			response, err = w.client.Do(reqWithContext)
			duration := time.Since(start)
			
			if err != nil {
				w.logger.WithError(err).WithFields(logrus.Fields{
					"url":      req.URL.String(),
					"method":   req.Method,
					"duration": duration,
					"state":    w.circuitBreaker.GetState().String(),
				}).Error("HTTP request failed")
				return err
			}
			
			// Consider 5xx status codes as failures
			if response.StatusCode >= 500 {
				err = fmt.Errorf("server error: %d %s", response.StatusCode, response.Status)
				w.logger.WithError(err).WithFields(logrus.Fields{
					"url":         req.URL.String(),
					"method":      req.Method,
					"status_code": response.StatusCode,
					"duration":    duration,
					"state":       w.circuitBreaker.GetState().String(),
				}).Error("HTTP request returned server error")
				return err
			}
			
			w.logger.WithFields(logrus.Fields{
				"url":         req.URL.String(),
				"method":      req.Method,
				"status_code": response.StatusCode,
				"duration":    duration,
				"state":       w.circuitBreaker.GetState().String(),
			}).Debug("HTTP request succeeded")
			
			return nil
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.FallbackRequests++
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			if w.fallbackHandler != nil {
				fallbackResp, fallbackErr := w.fallbackHandler(req)
				if fallbackErr != nil {
					w.fallbackMetrics.FallbackFailures++
					w.logger.WithError(fallbackErr).WithFields(logrus.Fields{
						"url":    req.URL.String(),
						"method": req.Method,
					}).Error("HTTP fallback handler failed")
					return fallbackErr
				}
				
				w.fallbackMetrics.FallbackSuccesses++
				response = fallbackResp
				
				w.logger.WithFields(logrus.Fields{
					"url":    req.URL.String(),
					"method": req.Method,
				}).Info("HTTP fallback handler succeeded")
				
				return nil
			}
			
			// No fallback handler, return original error
			w.fallbackMetrics.FallbackFailures++
			return fmt.Errorf("HTTP service unavailable, circuit breaker open and no fallback configured")
		},
	)
	
	// If circuit breaker wrapper failed but we have a response, return it
	if cbErr != nil && response == nil {
		return nil, cbErr
	}
	
	return response, err
}

// Get performs GET request with circuit breaker protection
func (w *HTTPCircuitBreakerWrapper) Get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	return w.Do(req)
}

// Post performs POST request with circuit breaker protection
func (w *HTTPCircuitBreakerWrapper) Post(ctx context.Context, url, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}
	
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	
	return w.Do(req)
}

// SetFallbackHandler sets a fallback handler for when circuit breaker is open
func (w *HTTPCircuitBreakerWrapper) SetFallbackHandler(handler func(*http.Request) (*http.Response, error)) {
	w.fallbackHandler = handler
	w.logger.Info("HTTP fallback handler configured")
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (w *HTTPCircuitBreakerWrapper) GetCircuitBreakerStats() *circuitbreaker.Statistics {
	return w.circuitBreaker.GetStatistics()
}

// GetFallbackMetrics returns fallback metrics
func (w *HTTPCircuitBreakerWrapper) GetFallbackMetrics() *HTTPFallbackMetrics {
	return w.fallbackMetrics
}

// GetCircuitBreakerState returns the current circuit breaker state
func (w *HTTPCircuitBreakerWrapper) GetCircuitBreakerState() circuitbreaker.State {
	return w.circuitBreaker.GetState()
}

// ResetCircuitBreaker resets the circuit breaker
func (w *HTTPCircuitBreakerWrapper) ResetCircuitBreaker() {
	w.circuitBreaker.Reset()
	w.logger.Info("HTTP circuit breaker reset")
}

// CreateCachedFallback creates a simple cached response fallback
func CreateCachedFallback(cachedResponse *http.Response, logger *logrus.Logger) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		if cachedResponse == nil {
			return nil, fmt.Errorf("no cached response available")
		}
		
		logger.WithFields(logrus.Fields{
			"url":    req.URL.String(),
			"method": req.Method,
		}).Info("Returning cached response as fallback")
		
		return cachedResponse, nil
	}
}

// CreateErrorFallback creates a fallback that returns a specific error response
func CreateErrorFallback(statusCode int, message string, logger *logrus.Logger) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		logger.WithFields(logrus.Fields{
			"url":         req.URL.String(),
			"method":      req.Method,
			"status_code": statusCode,
			"message":     message,
		}).Info("Returning error response as fallback")
		
		resp := &http.Response{
			StatusCode: statusCode,
			Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
			Body:       io.NopCloser(strings.NewReader(message)),
			Header:     make(http.Header),
			Request:    req,
		}
		
		resp.Header.Set("Content-Type", "text/plain")
		resp.Header.Set("X-Fallback", "circuit-breaker")
		
		return resp, nil
	}
}