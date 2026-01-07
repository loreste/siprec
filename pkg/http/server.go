package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"siprec-server/pkg/errors"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/version"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// Use Config from config.go instead of defining it here

// MetricsProvider is an interface that exposes metrics for the HTTP server
type MetricsProvider interface {
	GetActiveCallCount() int
	GetMetrics() map[string]interface{}
}

// RateLimitMiddleware interface for rate limiting
type RateLimitMiddleware interface {
	Middleware(next http.Handler) http.Handler
}

// CorrelationMiddleware interface for request correlation
type CorrelationMiddleware interface {
	Middleware(next http.Handler) http.Handler
}

// Server represents the HTTP server for health checks and metrics
type Server struct {
	config                *Config
	logger                *logrus.Logger
	httpServer            *http.Server
	mux                   *http.ServeMux
	metricsProvider       MetricsProvider
	startTime             time.Time
	additionalHandlers    map[string]http.HandlerFunc
	sipHandler            interface{} // Reference to SIP handler
	wsHub                 *TranscriptionHub
	amqpClient            interface{} // Reference to AMQP client
	analyticsWSHandler    *AnalyticsWebSocketHandler
	authMiddleware        *AuthMiddleware
	rateLimitMiddleware   RateLimitMiddleware
	correlationMiddleware CorrelationMiddleware
}

// NewServer creates a new HTTP server instance
func NewServer(logger *logrus.Logger, config *Config, metricsProvider MetricsProvider) *Server {
	if config == nil {
		config = DefaultConfig()
	}

	server := &Server{
		config:             config,
		logger:             logger,
		metricsProvider:    metricsProvider,
		startTime:          time.Now(),
		additionalHandlers: make(map[string]http.HandlerFunc),
	}

	mux := http.NewServeMux()
	server.mux = mux
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler := http.Handler(mux)
		// Apply auth middleware (inner layer)
		if server.authMiddleware != nil {
			handler = server.authMiddleware.Middleware(handler)
		}
		// Apply rate limiting middleware
		if server.rateLimitMiddleware != nil {
			handler = server.rateLimitMiddleware.Middleware(handler)
		}
		// Apply correlation middleware (outermost - adds correlation ID first)
		if server.correlationMiddleware != nil {
			handler = server.correlationMiddleware.Middleware(handler)
		}
		handler.ServeHTTP(w, r)
	})

	// Wrap handlers with middleware that adds Server header
	addServerHeader := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Server", version.ServerHeader())
			next(w, r)
		}
	}

	// Register standard endpoints
	mux.HandleFunc("/health", addServerHeader(server.HealthHandler))
	mux.HandleFunc("/health/live", addServerHeader(server.LivenessHandler))
	mux.HandleFunc("/health/ready", addServerHeader(server.ReadinessHandler))

	// Add metrics endpoints based on configuration
	if config.EnableMetrics {
		// Use the comprehensive Prometheus metrics registry if available
		if registry := metrics.GetRegistry(); registry != nil {
			// Wrap promhttp handler with Server header middleware
			promHandler := promhttp.HandlerFor(
				registry,
				promhttp.HandlerOpts{
					EnableOpenMetrics: true,
					Registry:          registry,
				},
			)
			mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Server", version.ServerHeader())
				promHandler.ServeHTTP(w, r)
			})
			logger.Info("Prometheus metrics endpoint enabled at /metrics")
		} else {
			// Fallback to simple metrics
			mux.HandleFunc("/metrics", addServerHeader(server.metricsHandler))
			logger.Info("Simple metrics endpoint enabled at /metrics")
		}

		// Add a simple metrics endpoint as well for basic monitoring
		mux.HandleFunc("/metrics/simple", addServerHeader(server.metricsHandler))
	} else {
		logger.Info("Metrics endpoints disabled")
	}

	mux.HandleFunc("/status", addServerHeader(server.statusHandler))

	// Create the HTTP server
	server.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      rootHandler,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	return server
}

// SetAuthMiddleware sets the authentication middleware for the server.
func (s *Server) SetAuthMiddleware(middleware *AuthMiddleware) {
	s.authMiddleware = middleware
}

// SetRateLimitMiddleware sets the rate limiting middleware for the server.
func (s *Server) SetRateLimitMiddleware(middleware RateLimitMiddleware) {
	s.rateLimitMiddleware = middleware
	s.logger.Info("Rate limiting middleware configured")
}

// SetCorrelationMiddleware sets the correlation ID middleware for request tracking.
func (s *Server) SetCorrelationMiddleware(middleware CorrelationMiddleware) {
	s.correlationMiddleware = middleware
	s.logger.Info("Correlation ID middleware configured")
}

// RegisterHandler adds a custom handler to the server
func (s *Server) RegisterHandler(path string, handler http.HandlerFunc) {
	s.additionalHandlers[path] = handler

	// Add to mux
	if s.mux != nil {
		s.mux.HandleFunc(path, handler)
	}

	s.logger.WithField("path", path).Info("Registered custom HTTP handler")
}

// SetSIPHandler sets the SIP handler reference for health checks
func (s *Server) SetSIPHandler(handler interface{}) {
	s.sipHandler = handler
}

// SetWebSocketHub sets the WebSocket hub reference for health checks
func (s *Server) SetWebSocketHub(hub *TranscriptionHub) {
	s.wsHub = hub
}

// SetAnalyticsWebSocketHandler sets the analytics WebSocket handler
func (s *Server) SetAnalyticsWebSocketHandler(handler *AnalyticsWebSocketHandler) {
	s.analyticsWSHandler = handler

	// Register the WebSocket endpoint
	if s.mux != nil {
		s.mux.HandleFunc("/ws/analytics", handler.ServeHTTP)
		s.logger.Info("Analytics WebSocket endpoint registered at /ws/analytics")
	}
}

// GetAnalyticsWebSocketHandler returns the analytics WebSocket handler
func (s *Server) GetAnalyticsWebSocketHandler() *AnalyticsWebSocketHandler {
	return s.analyticsWSHandler
}

// SetAMQPClient sets the AMQP client reference for health checks
func (s *Server) SetAMQPClient(client interface{}) {
	s.amqpClient = client
}

// Start starts the HTTP server in a goroutine
func (s *Server) Start() {
	s.logger.WithField("port", s.config.Port).Info("Starting HTTP server")

	// Start serving in a goroutine
	go func() {
		s.logger.Infof("HTTP server listening on port %d", s.config.Port)
		if s.config.TLSEnabled {
			if s.config.TLSCertFile == "" || s.config.TLSKeyFile == "" {
				s.logger.Error("TLS is enabled but certificate or key path is missing; refusing to start HTTP server")
				return
			}

			// Enforce modern TLS settings
			s.httpServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}

			if err := s.httpServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				s.logger.WithError(err).Error("HTTP TLS server failed")
			}
			return
		}

		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.WithError(err).Error("HTTP server failed")
		}
	}()

	// Verify that we can actually bind to the port
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.logger.Info("Verifying HTTP server is running...")

		// Try to open a connection to the server port
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.config.Port), 2*time.Second)
		if err != nil {
			s.logger.WithError(err).Error("Could not connect to HTTP server")
		} else {
			s.logger.Info("HTTP server is running correctly")
			conn.Close()
		}
	}()
}

// Shutdown gracefully shuts down the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down HTTP server...")
	return s.httpServer.Shutdown(ctx)
}

// Removed simple healthHandler - using comprehensive HealthHandler from health.go instead

// metricsHandler handles the /metrics endpoint using Prometheus registry
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.WithField("endpoint", "/metrics").Debug("Metrics endpoint accessed")

	// Enhanced metrics with proper Prometheus format and additional information
	activeCalls := 0
	if s.metricsProvider != nil {
		activeCalls = s.metricsProvider.GetActiveCallCount()
	}

	metrics := fmt.Sprintf(`# HELP siprec_active_calls Number of active calls
# TYPE siprec_active_calls gauge
siprec_active_calls %d

# HELP siprec_uptime_seconds Uptime of the service in seconds
# TYPE siprec_uptime_seconds counter
siprec_uptime_seconds %.2f

# HELP siprec_http_requests_total Total number of HTTP requests
# TYPE siprec_http_requests_total counter
siprec_http_requests_total{endpoint="/metrics",method="GET"} 1

# HELP siprec_build_info Build information
# TYPE siprec_build_info gauge
siprec_build_info{version="%s",component="siprec-server",go_version="go1.23"} 1

# HELP siprec_health_status Health status of components (1 = healthy, 0 = unhealthy)
# TYPE siprec_health_status gauge
siprec_health_status{component="server"} 1
`,
		activeCalls,
		time.Since(s.startTime).Seconds(),
		version.Version,
	)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metrics))
}

// statusHandler handles the /status endpoint
func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.WithField("endpoint", "/status").Debug("Status endpoint accessed")

	status := map[string]interface{}{
		"status":       "ok",
		"uptime":       time.Since(s.startTime).String(),
		"active_calls": 0,
		"version":      version.Version,
		"started_at":   s.startTime.Format(time.RFC3339),
	}

	// Add metrics if available
	if s.metricsProvider != nil {
		status["active_calls"] = s.metricsProvider.GetActiveCallCount()

		// Add other metrics if available
		if metrics := s.metricsProvider.GetMetrics(); metrics != nil {
			for k, v := range metrics {
				status[k] = v
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// ErrorResponse sends a standardized error response
func (s *Server) ErrorResponse(w http.ResponseWriter, err error) {
	errors.WriteError(w, err)
	s.logger.WithError(err).Warn("HTTP error response sent")
}
