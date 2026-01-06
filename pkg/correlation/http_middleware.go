package correlation

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// HTTPMiddleware adds correlation ID tracking to HTTP requests
type HTTPMiddleware struct {
	logger           *logrus.Logger
	generateIfMissing bool
	logRequests      bool
}

// HTTPMiddlewareConfig configures the HTTP correlation middleware
type HTTPMiddlewareConfig struct {
	// GenerateIfMissing generates a new correlation ID if not provided in headers
	GenerateIfMissing bool

	// LogRequests enables request/response logging with correlation IDs
	LogRequests bool
}

// DefaultHTTPMiddlewareConfig returns sensible defaults
func DefaultHTTPMiddlewareConfig() *HTTPMiddlewareConfig {
	return &HTTPMiddlewareConfig{
		GenerateIfMissing: true,
		LogRequests:       true,
	}
}

// NewHTTPMiddleware creates a new HTTP correlation middleware
func NewHTTPMiddleware(logger *logrus.Logger, config *HTTPMiddlewareConfig) *HTTPMiddleware {
	if config == nil {
		config = DefaultHTTPMiddlewareConfig()
	}

	return &HTTPMiddleware{
		logger:            logger,
		generateIfMissing: config.GenerateIfMissing,
		logRequests:       config.LogRequests,
	}
}

// Middleware returns an HTTP middleware function that adds correlation ID tracking
func (m *HTTPMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Try to extract correlation ID from various headers
		correlationID := m.extractCorrelationID(r)

		// Generate new ID if missing and configured to do so
		if correlationID.IsEmpty() && m.generateIfMissing {
			correlationID = New()
		}

		// Get client IP
		clientIP := m.getClientIP(r)

		// Create request info and attach to context
		reqInfo := &RequestInfo{
			CorrelationID: correlationID,
			StartTime:     startTime,
			ClientIP:      clientIP,
			Method:        r.Method,
			Path:          r.URL.Path,
		}

		// Add correlation ID to request context
		ctx := reqInfo.ToContext(r.Context())
		r = r.WithContext(ctx)

		// Set correlation ID in response headers
		if !correlationID.IsEmpty() {
			w.Header().Set(HTTPHeader, correlationID.String())
			w.Header().Set(HTTPRequestIDHeader, correlationID.String())
		}

		// Create response wrapper to capture status code
		wrapper := &responseWrapper{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Log request start if enabled
		if m.logRequests && m.logger != nil {
			m.logger.WithFields(logrus.Fields{
				"correlation_id": correlationID.String(),
				"method":         r.Method,
				"path":           r.URL.Path,
				"client_ip":      clientIP,
				"user_agent":     r.UserAgent(),
			}).Debug("HTTP request started")
		}

		// Process request
		next.ServeHTTP(wrapper, r)

		// Log request completion if enabled
		if m.logRequests && m.logger != nil {
			duration := time.Since(startTime)
			fields := logrus.Fields{
				"correlation_id": correlationID.String(),
				"method":         r.Method,
				"path":           r.URL.Path,
				"status":         wrapper.statusCode,
				"duration_ms":    duration.Milliseconds(),
				"client_ip":      clientIP,
			}

			// Log at appropriate level based on status code
			if wrapper.statusCode >= 500 {
				m.logger.WithFields(fields).Error("HTTP request completed with server error")
			} else if wrapper.statusCode >= 400 {
				m.logger.WithFields(fields).Warn("HTTP request completed with client error")
			} else {
				m.logger.WithFields(fields).Debug("HTTP request completed")
			}
		}
	})
}

// extractCorrelationID tries to extract a correlation ID from request headers
func (m *HTTPMiddleware) extractCorrelationID(r *http.Request) ID {
	// Try standard correlation ID header first
	if id := r.Header.Get(HTTPHeader); id != "" {
		return ID(id)
	}

	// Try X-Request-ID
	if id := r.Header.Get(HTTPRequestIDHeader); id != "" {
		return ID(id)
	}

	// Try X-Trace-ID
	if id := r.Header.Get(HTTPTraceIDHeader); id != "" {
		return ID(id)
	}

	return ""
}

// getClientIP extracts the client IP from the request
func (m *HTTPMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// responseWrapper wraps http.ResponseWriter to capture status code
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader captures the status code
func (w *responseWrapper) WriteHeader(statusCode int) {
	if !w.written {
		w.statusCode = statusCode
		w.written = true
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write captures that a write occurred
func (w *responseWrapper) Write(b []byte) (int, error) {
	if !w.written {
		w.written = true
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter (for http.Flusher, etc.)
func (w *responseWrapper) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
