package http

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

// HealthStatus represents the health status of the service
type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp string                 `json:"timestamp"`
	Uptime    string                 `json:"uptime"`
	Version   string                 `json:"version"`
	Checks    map[string]CheckResult `json:"checks"`
	System    SystemInfo             `json:"system"`
}

// CheckResult represents an individual health check result
type CheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// SystemInfo contains system resource information
type SystemInfo struct {
	GoRoutines   int    `json:"goroutines"`
	MemoryMB     uint64 `json:"memory_mb"`
	CPUCount     int    `json:"cpu_count"`
	ActiveCalls  int    `json:"active_calls"`
	PortsInUse   int    `json:"ports_in_use"`
}

// HealthHandler handles health check requests
func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	health := HealthStatus{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Uptime:    time.Since(s.startTime).Round(time.Second).String(),
		Version:   "1.0.0",
		Checks:    make(map[string]CheckResult),
	}

	// Check SIP service
	if s.sipHandler != nil {
		health.Checks["sip"] = CheckResult{
			Status: "healthy",
			Message: "SIP service is running",
		}
	} else {
		health.Checks["sip"] = CheckResult{
			Status: "unhealthy",
			Message: "SIP handler not initialized",
		}
		health.Status = "unhealthy"
	}

	// Check WebSocket service
	if s.wsHub != nil && s.wsHub.IsRunning() {
		health.Checks["websocket"] = CheckResult{
			Status: "healthy",
			Message: "WebSocket hub is running",
		}
	} else {
		health.Checks["websocket"] = CheckResult{
			Status: "degraded",
			Message: "WebSocket hub not running",
		}
	}

	// Check session storage
	sessionMgr := siprec.GetGlobalSessionManager()
	if sessionMgr != nil {
		activeSessions := sessionMgr.GetActiveSessionCount()
		health.Checks["sessions"] = CheckResult{
			Status: "healthy",
			Message: "Session storage operational",
		}
		health.System.ActiveCalls = activeSessions
	} else {
		health.Checks["sessions"] = CheckResult{
			Status: "unhealthy",
			Message: "Session manager not available",
		}
		health.Status = "unhealthy"
	}

	// Check RTP port availability
	availablePorts, totalPorts := media.GetPortManagerStats()
	if availablePorts > 0 {
		health.Checks["rtp_ports"] = CheckResult{
			Status: "healthy",
			Message: "RTP ports available",
		}
		health.System.PortsInUse = totalPorts - availablePorts
	} else {
		health.Checks["rtp_ports"] = CheckResult{
			Status: "unhealthy",
			Message: "No RTP ports available",
		}
		health.Status = "degraded"
	}

	// Check AMQP if configured
	if s.amqpClient != nil {
		// Type assert to access IsConnected method
		if amqpClient, ok := s.amqpClient.(interface{ IsConnected() bool }); ok {
			if amqpClient.IsConnected() {
				health.Checks["amqp"] = CheckResult{
					Status: "healthy",
					Message: "AMQP connected",
				}
			} else {
				health.Checks["amqp"] = CheckResult{
					Status: "degraded",
					Message: "AMQP disconnected",
				}
			}
		}
	}

	// System information
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	health.System.GoRoutines = runtime.NumGoroutine()
	health.System.MemoryMB = m.Alloc / 1024 / 1024
	health.System.CPUCount = runtime.NumCPU()

	// Log health check if it's detailed
	if r.URL.Query().Get("detailed") == "true" {
		s.logger.WithFields(logrus.Fields{
			"status":     health.Status,
			"checks":     health.Checks,
			"system":     health.System,
			"duration":   time.Since(startTime),
		}).Debug("Health check performed")
	}

	// Set appropriate status code
	statusCode := http.StatusOK
	if health.Status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(health)
}

// LivenessHandler handles kubernetes liveness probe
func (s *Server) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// ReadinessHandler handles kubernetes readiness probe
func (s *Server) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	// Check if essential services are ready
	ready := true
	
	if s.sipHandler == nil {
		ready = false
	}
	
	sessionMgr := siprec.GetGlobalSessionManager()
	if sessionMgr == nil {
		ready = false
	}
	
	if ready {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready"))
	}
}