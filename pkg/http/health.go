package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"siprec-server/pkg/core"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"

	"github.com/sirupsen/logrus"
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
	GoRoutines  int    `json:"goroutines"`
	MemoryMB    uint64 `json:"memory_mb"`
	CPUCount    int    `json:"cpu_count"`
	ActiveCalls int    `json:"active_calls"`
	PortsInUse  int    `json:"ports_in_use"`
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
			Status:  "healthy",
			Message: "SIP service is running",
		}
	} else {
		health.Checks["sip"] = CheckResult{
			Status:  "unhealthy",
			Message: "SIP handler not initialized",
		}
		health.Status = "unhealthy"
	}

	// Check WebSocket service
	if s.wsHub != nil && s.wsHub.IsRunning() {
		health.Checks["websocket"] = CheckResult{
			Status:  "healthy",
			Message: "WebSocket hub is running",
		}
	} else {
		health.Checks["websocket"] = CheckResult{
			Status:  "degraded",
			Message: "WebSocket hub not running",
		}
	}

	// Check session storage
	sessionMgr := siprec.GetGlobalSessionManager()
	if sessionMgr != nil {
		activeSessions := sessionMgr.GetActiveSessionCount()
		health.Checks["sessions"] = CheckResult{
			Status:  "healthy",
			Message: "Session storage operational",
		}
		health.System.ActiveCalls = activeSessions
	} else {
		health.Checks["sessions"] = CheckResult{
			Status:  "unhealthy",
			Message: "Session manager not available",
		}
		health.Status = "unhealthy"
	}

	// Check RTP port availability
	availablePorts, totalPorts := media.GetPortManagerStats()
	if availablePorts > 0 {
		health.Checks["rtp_ports"] = CheckResult{
			Status:  "healthy",
			Message: "RTP ports available",
		}
		health.System.PortsInUse = totalPorts - availablePorts
	} else {
		health.Checks["rtp_ports"] = CheckResult{
			Status:  "unhealthy",
			Message: "No RTP ports available",
		}
		health.Status = "degraded"
	}

	// Check AMQP if configured
	if s.amqpClient != nil {
		// Safely call IsConnected with panic recovery
		func() {
			defer func() {
				if r := recover(); r != nil {
					health.Checks["amqp"] = CheckResult{
						Status:  "degraded",
						Message: "AMQP client error",
					}
				}
			}()
			
			// Type assert to messaging.AMQPClientInterface
			if amqpClient, ok := s.amqpClient.(interface{ IsConnected() bool }); ok {
				if amqpClient.IsConnected() {
					health.Checks["amqp"] = CheckResult{
						Status:  "healthy",
						Message: "AMQP connected",
					}
				} else {
					health.Checks["amqp"] = CheckResult{
						Status:  "degraded",
						Message: "AMQP disconnected",
					}
				}
			} else {
				health.Checks["amqp"] = CheckResult{
					Status:  "degraded",
					Message: "AMQP client not properly initialized",
				}
			}
		}()
	}

	// Check Redis session store if available
	if redisSessionMgr := getRedisSessionManager(); redisSessionMgr != nil {
		if err := redisSessionMgr.Health(); err != nil {
			health.Checks["redis"] = CheckResult{
				Status:  "degraded",
				Message: fmt.Sprintf("Redis session store unhealthy: %v", err),
			}
		} else {
			health.Checks["redis"] = CheckResult{
				Status:  "healthy",
				Message: "Redis session store operational",
			}
		}
	}

	// Check database connectivity if available
	if dbHealth := getDatabaseHealth(); dbHealth != nil {
		if err := dbHealth.Health(); err != nil {
			health.Checks["database"] = CheckResult{
				Status:  "degraded",
				Message: fmt.Sprintf("Database unhealthy: %v", err),
			}
		} else {
			health.Checks["database"] = CheckResult{
				Status:  "healthy",
				Message: "Database operational",
			}
		}
	}

	// Check STT providers health
	if sttProviderHealth := getSTTProviderHealth(); sttProviderHealth != nil {
		for provider, status := range sttProviderHealth {
			if status.Healthy {
				health.Checks[fmt.Sprintf("stt_%s", provider)] = CheckResult{
					Status:  "healthy",
					Message: fmt.Sprintf("STT provider %s operational", provider),
				}
			} else {
				health.Checks[fmt.Sprintf("stt_%s", provider)] = CheckResult{
					Status:  "degraded",
					Message: fmt.Sprintf("STT provider %s unhealthy: %s", provider, status.Error),
				}
			}
		}
	}

	// Check encryption services
	if encryptionHealth := getEncryptionHealth(); encryptionHealth != nil {
		if encryptionHealth.Healthy {
			health.Checks["encryption"] = CheckResult{
				Status:  "healthy",
				Message: "Encryption services operational",
			}
		} else {
			health.Checks["encryption"] = CheckResult{
				Status:  "degraded",
				Message: fmt.Sprintf("Encryption services unhealthy: %s", encryptionHealth.Error),
			}
		}
	}

	// Check async STT system if available
	if sttHealth := getAsyncSTTHealth(); sttHealth != nil {
		for component, status := range sttHealth {
			if status["status"] == "healthy" {
				health.Checks[fmt.Sprintf("stt_%s", component)] = CheckResult{
					Status:  "healthy",
					Message: fmt.Sprintf("STT %s operational", component),
				}
			} else {
				health.Checks[fmt.Sprintf("stt_%s", component)] = CheckResult{
					Status:  "degraded",
					Message: fmt.Sprintf("STT %s: %s", component, status["message"]),
				}
			}
		}
	}

	// Check configuration system if available
	if configHealth := getConfigSystemHealth(); configHealth != nil {
		if configHealth["status"] == "healthy" {
			health.Checks["config_system"] = CheckResult{
				Status:  "healthy",
				Message: configHealth["message"].(string),
			}
		} else {
			health.Checks["config_system"] = CheckResult{
				Status:  "degraded",
				Message: configHealth["message"].(string),
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
			"status":   health.Status,
			"checks":   health.Checks,
			"system":   health.System,
			"duration": time.Since(startTime),
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

// Helper functions for health checks

// HealthStatus represents component health status
type ComponentHealth struct {
	Healthy bool
	Error   string
}

// STTProviderStatus represents STT provider health
type STTProviderStatus map[string]ComponentHealth

// getRedisSessionManager returns the Redis session manager if available
func getRedisSessionManager() interface{ Health() error } {
	// For now, we don't have a global Redis client getter
	// This would be implemented when Redis integration is added
	return nil
}

// getDatabaseHealth returns the database health checker if available
func getDatabaseHealth() interface{ Health() error } {
	// For now, we don't have a global database repository getter
	// This would be implemented when database integration is added
	return nil
}

// getSTTProviderHealth returns health status for all STT providers
func getSTTProviderHealth() STTProviderStatus {
	// For now, we don't have a global STT provider manager getter
	// This would be implemented when STT integration is added to health checks
	return nil
}

// getEncryptionHealth returns encryption service health status
func getEncryptionHealth() *ComponentHealth {
	// For now, we don't have a global encryption manager getter
	// This would be implemented when encryption integration is added
	return nil
}

// getAsyncSTTHealth returns async STT system health status
func getAsyncSTTHealth() map[string]map[string]interface{} {
	registry := core.GetServiceRegistry()
	processor := registry.GetAsyncSTTProcessor()
	
	if processor == nil {
		return nil
	}

	health := make(map[string]map[string]interface{})
	
	// Get metrics from the processor
	metrics := processor.GetMetrics()
	
	// Overall async STT health
	health["async_processor"] = map[string]interface{}{
		"status": "healthy",
		"message": fmt.Sprintf("Processing %d jobs with %d workers", metrics.QueueSize, metrics.ActiveWorkers),
		"active_workers": metrics.ActiveWorkers,
		"queue_size": metrics.QueueSize,
		"jobs_processed": metrics.JobsProcessed,
		"jobs_failed": metrics.JobsFailed,
	}
	
	// Check queue health
	queueStats, err := processor.GetQueueStats()
	if err != nil {
		health["queue"] = map[string]interface{}{
			"status": "degraded",
			"message": fmt.Sprintf("Failed to get queue stats: %v", err),
		}
	} else {
		queueStatus := "healthy"
		message := "Queue operating normally"
		
		// Check for high error rate
		if queueStats.ErrorRate > 50 {
			queueStatus = "unhealthy"
			message = fmt.Sprintf("High error rate: %.2f%%", queueStats.ErrorRate)
		} else if queueStats.PendingJobs > 1000 {
			queueStatus = "degraded"
			message = fmt.Sprintf("Queue backlog: %d pending jobs", queueStats.PendingJobs)
		}
		
		health["queue"] = map[string]interface{}{
			"status": queueStatus,
			"message": message,
			"pending_jobs": queueStats.PendingJobs,
			"processing_jobs": queueStats.ProcessingJobs,
			"error_rate": queueStats.ErrorRate,
			"throughput": queueStats.Throughput,
		}
	}
	
	return health
}

// getConfigSystemHealth returns configuration system health status
func getConfigSystemHealth() map[string]interface{} {
	registry := core.GetServiceRegistry()
	hotReloadManager := registry.GetHotReloadManager()
	
	if hotReloadManager == nil {
		return nil
	}
	
	health := map[string]interface{}{
		"hot_reload_enabled": hotReloadManager.IsEnabled(),
	}
	
	if hotReloadManager.IsEnabled() {
		health["status"] = "healthy"
		health["message"] = "Configuration hot-reload is active and monitoring changes"
	} else {
		health["status"] = "degraded"
		health["message"] = "Configuration hot-reload is disabled"
	}
	
	return health
}
