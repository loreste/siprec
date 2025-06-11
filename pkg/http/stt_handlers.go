package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
)

// STTHandlers manages HTTP endpoints for STT operations
type STTHandlers struct {
	processor *stt.AsyncSTTProcessor
	logger    *logrus.Logger
}

// NewSTTHandlers creates new STT HTTP handlers
func NewSTTHandlers(processor *stt.AsyncSTTProcessor, logger *logrus.Logger) *STTHandlers {
	return &STTHandlers{
		processor: processor,
		logger:    logger,
	}
}

// RegisterSTTEndpoints registers STT endpoints on the server
func (s *Server) RegisterSTTEndpoints(handlers *STTHandlers) {
	s.RegisterHandler("/api/stt/submit", handlers.SubmitJobHandler)
	s.RegisterHandler("/api/stt/jobs", handlers.ListJobsHandler)
	s.RegisterHandler("/api/stt/jobs/", handlers.GetJobHandler)
	s.RegisterHandler("/api/stt/stats", handlers.GetStatsHandler)
	s.RegisterHandler("/api/stt/metrics", handlers.GetMetricsHandler)
	s.RegisterHandler("/api/stt/queue/purge", handlers.PurgeQueueHandler)
}

// SubmitJobRequest represents a request to submit an STT job
type SubmitJobRequest struct {
	AudioPath string `json:"audio_path"`
	CallUUID  string `json:"call_uuid"`
	SessionID string `json:"session_id"`
	Provider  string `json:"provider,omitempty"`
	Language  string `json:"language,omitempty"`
	Priority  int    `json:"priority,omitempty"`
}

// SubmitJobResponse represents the response to a job submission
type SubmitJobResponse struct {
	JobID        string  `json:"job_id"`
	Status       string  `json:"status"`
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
	Message      string  `json:"message"`
}

// JobStatusResponse represents a job status response
type JobStatusResponse struct {
	Job     *stt.STTJob `json:"job"`
	Message string      `json:"message,omitempty"`
}

// JobListResponse represents a list of jobs
type JobListResponse struct {
	Jobs    []*stt.STTJob `json:"jobs"`
	Count   int           `json:"count"`
	Message string        `json:"message,omitempty"`
}

// StatsResponse represents queue statistics
type StatsResponse struct {
	QueueStats *stt.QueueStats       `json:"queue_stats"`
	Metrics    *stt.AsyncSTTMetrics  `json:"metrics"`
	Timestamp  time.Time             `json:"timestamp"`
}

// SubmitJobHandler handles STT job submission
func (h *STTHandlers) SubmitJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SubmitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.WithError(err).Error("Failed to decode STT job submission request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.AudioPath == "" || req.CallUUID == "" {
		http.Error(w, "Missing required fields: audio_path, call_uuid", http.StatusBadRequest)
		return
	}

	// Set defaults
	if req.Provider == "" {
		req.Provider = "google" // Default provider
	}
	if req.Language == "" {
		req.Language = "en-US" // Default language
	}
	if req.Priority == 0 {
		req.Priority = 2 // Normal priority
	}

	// Submit the job
	job, err := h.processor.SubmitJob(
		req.AudioPath,
		req.CallUUID,
		req.SessionID,
		req.Provider,
		req.Language,
		req.Priority,
	)

	if err != nil {
		h.logger.WithError(err).WithFields(logrus.Fields{
			"call_uuid":  req.CallUUID,
			"audio_path": req.AudioPath,
			"provider":   req.Provider,
		}).Error("Failed to submit STT job")

		response := SubmitJobResponse{
			Status:  "error",
			Message: "Failed to submit job: " + err.Error(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Success response
	response := SubmitJobResponse{
		JobID:         job.ID,
		Status:        string(job.Status),
		EstimatedCost: job.EstimatedCost,
		Message:       "Job submitted successfully",
	}

	h.logger.WithFields(logrus.Fields{
		"job_id":    job.ID,
		"call_uuid": req.CallUUID,
		"provider":  req.Provider,
		"priority":  req.Priority,
	}).Info("STT job submitted via API")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// GetJobHandler handles job status requests
func (h *STTHandlers) GetJobHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract job ID from URL path
	jobID := r.URL.Path[len("/api/stt/jobs/"):]
	if jobID == "" {
		http.Error(w, "Missing job ID", http.StatusBadRequest)
		return
	}

	// Get the job
	job, err := h.processor.GetJob(jobID)
	if err != nil {
		h.logger.WithError(err).WithField("job_id", jobID).Warning("Job not found")

		response := JobStatusResponse{
			Message: "Job not found",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Success response
	response := JobStatusResponse{
		Job:     job,
		Message: "Job retrieved successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// ListJobsHandler handles job listing requests
func (h *STTHandlers) ListJobsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	callUUID := query.Get("call_uuid")
	status := query.Get("status")

	var jobs []*stt.STTJob
	var err error

	if callUUID != "" {
		// Get jobs by call UUID
		jobs, err = h.processor.GetJobsByCallUUID(callUUID)
		if err != nil {
			h.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to get jobs by call UUID")
			http.Error(w, "Failed to retrieve jobs", http.StatusInternalServerError)
			return
		}
	} else if status != "" {
		// Get jobs by status (this would require adding to the async processor)
		// For now, return an error
		http.Error(w, "Filtering by status not yet implemented", http.StatusNotImplemented)
		return
	} else {
		// Return error - we need some filter criteria
		http.Error(w, "Please provide call_uuid or status parameter", http.StatusBadRequest)
		return
	}

	response := JobListResponse{
		Jobs:    jobs,
		Count:   len(jobs),
		Message: "Jobs retrieved successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetStatsHandler handles queue statistics requests
func (h *STTHandlers) GetStatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get queue statistics
	queueStats, err := h.processor.GetQueueStats()
	if err != nil {
		h.logger.WithError(err).Error("Failed to get queue statistics")
		http.Error(w, "Failed to retrieve statistics", http.StatusInternalServerError)
		return
	}

	// Get processing metrics
	metrics := h.processor.GetMetrics()

	response := StatsResponse{
		QueueStats: queueStats,
		Metrics:    metrics,
		Timestamp:  time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetMetricsHandler handles metrics requests (for Prometheus)
func (h *STTHandlers) GetMetricsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics := h.processor.GetMetrics()

	// Format metrics for Prometheus
	metricsText := fmt.Sprintf(`# HELP stt_jobs_enqueued_total Total number of STT jobs enqueued
# TYPE stt_jobs_enqueued_total counter
stt_jobs_enqueued_total %d

# HELP stt_jobs_processed_total Total number of STT jobs processed
# TYPE stt_jobs_processed_total counter
stt_jobs_processed_total %d

# HELP stt_jobs_failed_total Total number of STT jobs failed
# TYPE stt_jobs_failed_total counter
stt_jobs_failed_total %d

# HELP stt_jobs_retried_total Total number of STT jobs retried
# TYPE stt_jobs_retried_total counter
stt_jobs_retried_total %d

# HELP stt_queue_size Current number of jobs in queue
# TYPE stt_queue_size gauge
stt_queue_size %d

# HELP stt_active_workers Current number of active workers
# TYPE stt_active_workers gauge
stt_active_workers %d

# HELP stt_average_process_time_seconds Average processing time in seconds
# TYPE stt_average_process_time_seconds gauge
stt_average_process_time_seconds %.6f

# HELP stt_total_cost_usd Total cost of STT processing in USD
# TYPE stt_total_cost_usd counter
stt_total_cost_usd %.6f
`,
		metrics.JobsEnqueued,
		metrics.JobsProcessed,
		metrics.JobsFailed,
		metrics.JobsRetried,
		metrics.QueueSize,
		metrics.ActiveWorkers,
		metrics.AverageProcessTime.Seconds(),
		metrics.TotalCost,
	)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metricsText))
}

// PurgeQueueHandler handles queue purge requests
func (h *STTHandlers) PurgeQueueHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// This is a dangerous operation - in production you'd want authentication
	h.logger.Warning("STT queue purge requested via API")

	// For now, just return not implemented
	// You could implement this by adding a Purge method to the async processor
	response := map[string]string{
		"status":  "error",
		"message": "Queue purge not implemented - use with caution",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	json.NewEncoder(w).Encode(response)
}

// HealthCheckSTT returns STT system health information
func (h *STTHandlers) HealthCheckSTT() map[string]interface{} {
	metrics := h.processor.GetMetrics()
	queueStats, err := h.processor.GetQueueStats()

	health := map[string]interface{}{
		"active_workers": metrics.ActiveWorkers,
		"queue_size":     metrics.QueueSize,
		"jobs_processed": metrics.JobsProcessed,
		"jobs_failed":    metrics.JobsFailed,
		"error_rate":     0.0,
	}

	if err == nil && queueStats != nil {
		health["error_rate"] = queueStats.ErrorRate
		health["throughput"] = queueStats.Throughput
	}

	// Determine health status
	errorRate := health["error_rate"].(float64)
	queueSize := metrics.QueueSize

	if errorRate > 50 {
		health["status"] = "unhealthy"
		health["message"] = "High error rate"
	} else if queueSize > 1000 {
		health["status"] = "degraded"
		health["message"] = "Queue backlog"
	} else {
		health["status"] = "healthy"
		health["message"] = "Operating normally"
	}

	return health
}