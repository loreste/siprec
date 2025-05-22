package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/errors"
)

// Use Config from config.go instead of defining it here

// MetricsProvider is an interface that exposes metrics for the HTTP server
type MetricsProvider interface {
	GetActiveCallCount() int
	GetMetrics() map[string]interface{}
}

// Server represents the HTTP server for health checks and metrics
type Server struct {
	config             *Config
	logger             *logrus.Logger
	httpServer         *http.Server
	metricsProvider    MetricsProvider
	startTime          time.Time
	additionalHandlers map[string]http.HandlerFunc
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

	// Register standard endpoints
	mux.HandleFunc("/health", server.healthHandler)
	mux.HandleFunc("/metrics", server.metricsHandler)
	mux.HandleFunc("/status", server.statusHandler)

	// Create the HTTP server
	server.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Port),
		Handler:      mux,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	return server
}

// RegisterHandler adds a custom handler to the server
func (s *Server) RegisterHandler(path string, handler http.HandlerFunc) {
	s.additionalHandlers[path] = handler

	// Add to mux
	mux := s.httpServer.Handler.(*http.ServeMux)
	mux.HandleFunc(path, handler)

	s.logger.WithField("path", path).Info("Registered custom HTTP handler")
}

// Start starts the HTTP server in a goroutine
func (s *Server) Start() {
	s.logger.WithField("port", s.config.Port).Info("Starting HTTP server")

	// Start serving in a goroutine
	go func() {
		s.logger.Infof("HTTP server listening on port %d", s.config.Port)
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

// healthHandler handles the /health endpoint
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.WithField("endpoint", "/health").Debug("Health endpoint accessed")

	response := map[string]interface{}{
		"status": "ok",
		"uptime": time.Since(s.startTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// metricsHandler handles the /metrics endpoint
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	s.logger.WithField("endpoint", "/metrics").Debug("Metrics endpoint accessed")

	// Simple metrics in Prometheus format
	activeCalls := 0
	if s.metricsProvider != nil {
		activeCalls = s.metricsProvider.GetActiveCallCount()
	}

	metrics := fmt.Sprintf(`# HELP siprec_active_calls Number of active calls
# TYPE siprec_active_calls gauge
siprec_active_calls %d

# HELP siprec_uptime_seconds Uptime in seconds
# TYPE siprec_uptime_seconds counter
siprec_uptime_seconds %d
`, activeCalls, int(time.Since(s.startTime).Seconds()))

	w.Header().Set("Content-Type", "text/plain")
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
		"version":      "1.0.0", // Replace with actual version from build
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
