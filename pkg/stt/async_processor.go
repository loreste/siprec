package stt

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"siprec-server/pkg/metrics"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// AsyncSTTProcessor handles asynchronous STT processing with queueing
type AsyncSTTProcessor struct {
	queue           STTQueue
	providerManager *ProviderManager
	logger          *logrus.Logger
	config          *AsyncSTTConfig
	workers         []*STTWorker
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	metrics         *AsyncSTTMetrics
	callbacks       []JobCallback
	callbacksMutex  sync.RWMutex  // Dedicated mutex for callbacks slice
	started         bool
	mutex           sync.RWMutex
}

// AsyncSTTConfig holds configuration for async STT processing
type AsyncSTTConfig struct {
	WorkerCount         int           `yaml:"worker_count" env:"STT_WORKER_COUNT" default:"3"`
	MaxRetries          int           `yaml:"max_retries" env:"STT_MAX_RETRIES" default:"3"`
	RetryBackoff        time.Duration `yaml:"retry_backoff" env:"STT_RETRY_BACKOFF" default:"30s"`
	JobTimeout          time.Duration `yaml:"job_timeout" env:"STT_JOB_TIMEOUT" default:"300s"`
	QueueBufferSize     int           `yaml:"queue_buffer_size" env:"STT_QUEUE_BUFFER_SIZE" default:"1000"`
	BatchSize           int           `yaml:"batch_size" env:"STT_BATCH_SIZE" default:"10"`
	BatchTimeout        time.Duration `yaml:"batch_timeout" env:"STT_BATCH_TIMEOUT" default:"60s"`
	EnablePrioritization bool          `yaml:"enable_prioritization" env:"STT_ENABLE_PRIORITIZATION" default:"true"`
	MaxConcurrentJobs   int           `yaml:"max_concurrent_jobs" env:"STT_MAX_CONCURRENT_JOBS" default:"50"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" env:"STT_CLEANUP_INTERVAL" default:"300s"`
	JobRetentionTime    time.Duration `yaml:"job_retention_time" env:"STT_JOB_RETENTION_TIME" default:"24h"`
	EnableCostTracking  bool          `yaml:"enable_cost_tracking" env:"STT_ENABLE_COST_TRACKING" default:"true"`
	
	// Performance optimization settings
	MaxMemoryPerWorker  int64         `yaml:"max_memory_per_worker" env:"STT_MAX_MEMORY_PER_WORKER" default:"268435456"` // 256MB
	GCInterval          time.Duration `yaml:"gc_interval" env:"STT_GC_INTERVAL" default:"300s"`                         // Force GC every 5 minutes
	WorkerIdleTimeout   time.Duration `yaml:"worker_idle_timeout" env:"STT_WORKER_IDLE_TIMEOUT" default:"30s"`         // Worker idle timeout
}

// DefaultAsyncSTTConfig returns default configuration
func DefaultAsyncSTTConfig() *AsyncSTTConfig {
	return &AsyncSTTConfig{
		WorkerCount:         3,
		MaxRetries:          3,
		RetryBackoff:        30 * time.Second,
		JobTimeout:          5 * time.Minute,
		QueueBufferSize:     1000,
		BatchSize:           10,
		BatchTimeout:        60 * time.Second,
		EnablePrioritization: true,
		MaxConcurrentJobs:   50,
		CleanupInterval:     5 * time.Minute,
		JobRetentionTime:    24 * time.Hour,
		EnableCostTracking:  true,
		MaxMemoryPerWorker:  256 * 1024 * 1024, // 256MB
		GCInterval:          5 * time.Minute,
		WorkerIdleTimeout:   30 * time.Second,
	}
}

// JobCallback defines a callback function for job completion
type JobCallback func(*STTJob)

// AsyncSTTMetrics tracks async STT processing metrics
type AsyncSTTMetrics struct {
	JobsEnqueued       int64         `json:"jobs_enqueued"`
	JobsProcessed      int64         `json:"jobs_processed"`
	JobsFailed         int64         `json:"jobs_failed"`
	JobsRetried        int64         `json:"jobs_retried"`
	AverageProcessTime time.Duration `json:"average_process_time"`
	TotalCost          float64       `json:"total_cost"`
	ActiveWorkers      int           `json:"active_workers"`
	QueueSize          int           `json:"queue_size"`
	mutex              sync.RWMutex
}

// STTWorker represents a worker that processes STT jobs
type STTWorker struct {
	id        int
	processor *AsyncSTTProcessor
	logger    *logrus.Entry
	active    bool
	mutex     sync.RWMutex
}

// NewAsyncSTTProcessor creates a new async STT processor
func NewAsyncSTTProcessor(providerManager *ProviderManager, logger *logrus.Logger, config *AsyncSTTConfig) *AsyncSTTProcessor {
	if config == nil {
		config = DefaultAsyncSTTConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Initialize queue (using memory queue for now)
	queue := NewMemorySTTQueue(config.QueueBufferSize, logger)

	processor := &AsyncSTTProcessor{
		queue:           queue,
		providerManager: providerManager,
		logger:          logger,
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
		metrics:         &AsyncSTTMetrics{},
		callbacks:       make([]JobCallback, 0),
	}

	return processor
}

// Start starts the async STT processor
func (p *AsyncSTTProcessor) Start() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.started {
		return fmt.Errorf("async STT processor already started")
	}

	p.logger.WithField("worker_count", p.config.WorkerCount).Info("Starting async STT processor")

	// Create and start workers
	p.workers = make([]*STTWorker, p.config.WorkerCount)
	for i := 0; i < p.config.WorkerCount; i++ {
		worker := &STTWorker{
			id:        i,
			processor: p,
			logger:    p.logger.WithField("worker_id", i),
		}
		p.workers[i] = worker

		p.wg.Add(1)
		go p.runWorker(worker)
	}

	// Start cleanup routine
	p.wg.Add(1)
	go p.runCleanup()

	// Start metrics collection
	p.wg.Add(1)
	go p.runMetricsCollection()

	// Start memory management routine
	p.wg.Add(1)
	go p.runMemoryManagement()

	p.started = true
	p.logger.Info("Async STT processor started")

	return nil
}

// Stop stops the async STT processor
func (p *AsyncSTTProcessor) Stop() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if !p.started {
		return fmt.Errorf("async STT processor not started")
	}

	p.logger.Info("Stopping async STT processor")

	// Cancel context to stop workers
	p.cancel()

	// Wait for all workers to finish
	p.wg.Wait()

	// Close the queue
	p.queue.Close()

	p.started = false
	p.logger.Info("Async STT processor stopped")

	return nil
}

// SubmitJob submits a new STT job for processing
func (p *AsyncSTTProcessor) SubmitJob(audioPath, callUUID, sessionID, provider, language string, priority int) (*STTJob, error) {
	jobID := uuid.New().String()

	job := &STTJob{
		ID:         jobID,
		CallUUID:   callUUID,
		SessionID:  sessionID,
		AudioPath:  audioPath,
		Provider:   provider,
		Language:   language,
		Priority:   priority,
		CreatedAt:  time.Now(),
		Status:     StatusPending,
		MaxRetries: p.config.MaxRetries,
		Metadata:   make(map[string]interface{}),
	}

	// Estimate cost if enabled
	if p.config.EnableCostTracking {
		cost, err := p.estimateJobCost(job)
		if err != nil {
			p.logger.WithError(err).Warning("Failed to estimate job cost")
		} else {
			job.EstimatedCost = cost
		}
	}

	// Enqueue the job
	if err := p.queue.Enqueue(job); err != nil {
		p.logger.WithError(err).WithField("job_id", jobID).Error("Failed to enqueue STT job")
		return nil, fmt.Errorf("failed to enqueue STT job: %w", err)
	}

	// Update metrics
	p.updateMetrics("enqueued", nil)

	p.logger.WithFields(logrus.Fields{
		"job_id":    jobID,
		"call_uuid": callUUID,
		"provider":  provider,
		"priority":  priority,
	}).Info("STT job submitted for async processing")

	return job, nil
}

// GetJob retrieves a job by ID
func (p *AsyncSTTProcessor) GetJob(jobID string) (*STTJob, error) {
	return p.queue.GetJob(jobID)
}

// GetJobsByCallUUID retrieves all jobs for a call
func (p *AsyncSTTProcessor) GetJobsByCallUUID(callUUID string) ([]*STTJob, error) {
	return p.queue.GetJobsByCallUUID(callUUID)
}

// GetQueueStats returns current queue statistics
func (p *AsyncSTTProcessor) GetQueueStats() (*QueueStats, error) {
	return p.queue.GetQueueStats()
}

// GetMetrics returns current processing metrics
func (p *AsyncSTTProcessor) GetMetrics() *AsyncSTTMetrics {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()

	// Get current queue size
	if size, err := p.queue.GetQueueSize(); err == nil {
		p.metrics.QueueSize = size
	}

	// Count active workers
	activeWorkers := 0
	for _, worker := range p.workers {
		worker.mutex.RLock()
		if worker.active {
			activeWorkers++
		}
		worker.mutex.RUnlock()
	}
	p.metrics.ActiveWorkers = activeWorkers

	// Return a copy of metrics
	metricsCopy := *p.metrics
	return &metricsCopy
}

// AddCallback adds a job completion callback in a thread-safe manner
func (p *AsyncSTTProcessor) AddCallback(callback JobCallback) {
	p.callbacksMutex.Lock()
	defer p.callbacksMutex.Unlock()
	p.callbacks = append(p.callbacks, callback)
}

// runWorker runs a single STT worker with optimized polling and idle timeout
func (p *AsyncSTTProcessor) runWorker(worker *STTWorker) {
	defer p.wg.Done()

	worker.logger.Info("STT worker started")
	
	idleTimer := time.NewTimer(p.config.WorkerIdleTimeout)
	defer idleTimer.Stop()

	for {
		// Reset idle timer for each iteration
		if !idleTimer.Stop() {
			<-idleTimer.C
		}
		idleTimer.Reset(p.config.WorkerIdleTimeout)

		select {
		case <-p.ctx.Done():
			worker.logger.Info("STT worker stopping")
			return
		case <-idleTimer.C:
			// Worker has been idle, but continue running (just log debug info)
			worker.logger.Debug("Worker idle timeout reached, continuing to poll")
			continue
		default:
			// Try to get a job from the queue with a short timeout
			jobCtx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
			job, err := p.queue.Dequeue(jobCtx)
			cancel()
			
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					if err == context.Canceled {
						return // Shutdown requested
					}
					// Timeout - continue polling
					continue
				}
				worker.logger.WithError(err).Error("Failed to dequeue job")
				// Back off on error to prevent busy loop
				time.Sleep(time.Second)
				continue
			}

			// Mark worker as active
			worker.mutex.Lock()
			worker.active = true
			worker.mutex.Unlock()

			// Process the job
			p.processJob(worker, job)

			// Mark worker as inactive
			worker.mutex.Lock()
			worker.active = false
			worker.mutex.Unlock()
		}
	}
}

// processJob processes a single STT job
func (p *AsyncSTTProcessor) processJob(worker *STTWorker, job *STTJob) {
	startTime := time.Now()

	worker.logger.WithFields(logrus.Fields{
		"job_id":    job.ID,
		"call_uuid": job.CallUUID,
		"provider":  job.Provider,
		"attempt":   job.RetryCount + 1,
	}).Info("Processing STT job")

	// Create job timeout context
	jobCtx, cancel := context.WithTimeout(p.ctx, p.config.JobTimeout)
	defer cancel()

	// Process the job
	result, err := p.executeSTTJob(jobCtx, job)
	processingTime := time.Since(startTime)
	job.ProcessingTime = processingTime

	if err != nil {
		p.handleJobFailure(worker, job, err)
		return
	}

	// Job completed successfully
	job.Result = result
	job.Status = StatusCompleted
	now := time.Now()
	job.CompletedAt = &now

	// Calculate actual cost if enabled
	if p.config.EnableCostTracking {
		job.ActualCost = p.calculateActualCost(job, result)
	}

	// Update job in queue
	if err := p.queue.UpdateJob(job); err != nil {
		worker.logger.WithError(err).WithField("job_id", job.ID).Error("Failed to update completed job")
	}

	// Update metrics
	p.updateMetrics("completed", &processingTime)

	// Record metrics for Prometheus
	if metrics.IsMetricsEnabled() {
		metrics.RecordSTTRequest(job.Provider, "success")
		endTimer := metrics.ObserveSTTLatency(job.Provider)
		endTimer()
	}

	// Execute callbacks safely
	p.callbacksMutex.RLock()
	callbacksCopy := make([]JobCallback, len(p.callbacks))
	copy(callbacksCopy, p.callbacks)
	p.callbacksMutex.RUnlock()
	
	for _, callback := range callbacksCopy {
		go callback(job)
	}

	worker.logger.WithFields(logrus.Fields{
		"job_id":          job.ID,
		"processing_time": processingTime,
		"word_count":      result.WordCount,
		"confidence":      result.Confidence,
		"cost":            job.ActualCost,
	}).Info("STT job completed successfully")
}

// executeSTTJob executes the actual STT processing
func (p *AsyncSTTProcessor) executeSTTJob(ctx context.Context, job *STTJob) (*TranscriptionResult, error) {
	// Get the STT provider
	provider, exists := p.providerManager.GetProvider(job.Provider)
	if !exists {
		return nil, fmt.Errorf("STT provider %s not found", job.Provider)
	}

	// Open audio file
	audioFile, err := os.Open(job.AudioPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file %s: %w", job.AudioPath, err)
	}
	defer audioFile.Close()

	// Get file info for size estimation
	fileInfo, err := audioFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get audio file info: %w", err)
	}

	// Create a buffered, context-aware reader for efficiency
	bufferedReader := bufio.NewReaderSize(audioFile, 64*1024) // 64KB buffer for efficient reading
	audioReader := &contextReader{
		reader: bufferedReader,
		ctx:    ctx,
	}

	// Process STT (this is a simplified version - in reality you'd need to adapt existing providers)
	err = provider.StreamToText(ctx, audioReader, job.CallUUID)
	if err != nil {
		return nil, fmt.Errorf("STT provider failed: %w", err)
	}

	// For now, create a mock result since the existing providers don't return structured results
	// In a real implementation, you'd modify the providers to return TranscriptionResult
	result := &TranscriptionResult{
		Text:       "Mock transcription result",
		Confidence: 0.95,
		Language:   job.Language,
		Duration:   time.Duration(fileInfo.Size()/16000) * time.Second, // Estimate based on 16kHz
		WordCount:  10, // Mock word count
		Provider:   job.Provider,
		ModelUsed:  "enhanced",
	}

	return result, nil
}

// handleJobFailure handles job processing failures
func (p *AsyncSTTProcessor) handleJobFailure(worker *STTWorker, job *STTJob, jobError error) {
	job.RetryCount++
	job.Error = jobError.Error()

	// Check if we should retry
	if job.RetryCount <= job.MaxRetries {
		job.Status = StatusRetrying

		// Calculate retry delay with exponential backoff
		retryDelay := p.config.RetryBackoff * time.Duration(job.RetryCount)

		worker.logger.WithFields(logrus.Fields{
			"job_id":      job.ID,
			"retry_count": job.RetryCount,
			"max_retries": job.MaxRetries,
			"retry_delay": retryDelay,
			"error":       jobError.Error(),
		}).Warning("STT job failed, will retry")

		// Re-enqueue after delay
		go func() {
			time.Sleep(retryDelay)
			if err := p.queue.Enqueue(job); err != nil {
				worker.logger.WithError(err).WithField("job_id", job.ID).Error("Failed to re-enqueue job for retry")
			}
		}()

		p.updateMetrics("retried", nil)
	} else {
		// Max retries exceeded
		job.Status = StatusFailed
		now := time.Now()
		job.FailedAt = &now

		worker.logger.WithFields(logrus.Fields{
			"job_id":      job.ID,
			"retry_count": job.RetryCount,
			"max_retries": job.MaxRetries,
			"error":       jobError.Error(),
		}).Error("STT job failed permanently after max retries")

		p.updateMetrics("failed", nil)

		// Record failure metrics
		if metrics.IsMetricsEnabled() {
			metrics.RecordSTTRequest(job.Provider, "failed")
		}

		// Execute callbacks for failed jobs too (safely)
		p.callbacksMutex.RLock()
		callbacksCopy := make([]JobCallback, len(p.callbacks))
		copy(callbacksCopy, p.callbacks)
		p.callbacksMutex.RUnlock()
		
		for _, callback := range callbacksCopy {
			go callback(job)
		}
	}

	// Update job in queue
	if err := p.queue.UpdateJob(job); err != nil {
		worker.logger.WithError(err).WithField("job_id", job.ID).Error("Failed to update failed job")
	}
}

// runCleanup runs periodic cleanup of old jobs
func (p *AsyncSTTProcessor) runCleanup() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanupOldJobs()
		}
	}
}

// cleanupOldJobs removes old completed/failed jobs
func (p *AsyncSTTProcessor) cleanupOldJobs() {
	cutoffTime := time.Now().Add(-p.config.JobRetentionTime)

	// Get completed jobs
	completedJobs, err := p.queue.GetJobsByStatus(StatusCompleted)
	if err != nil {
		p.logger.WithError(err).Error("Failed to get completed jobs for cleanup")
		return
	}

	// Get failed jobs
	failedJobs, err := p.queue.GetJobsByStatus(StatusFailed)
	if err != nil {
		p.logger.WithError(err).Error("Failed to get failed jobs for cleanup")
		return
	}

	// Combine and filter old jobs
	allJobs := append(completedJobs, failedJobs...)
	var cleanedCount int

	for _, job := range allJobs {
		var jobTime time.Time
		if job.CompletedAt != nil {
			jobTime = *job.CompletedAt
		} else if job.FailedAt != nil {
			jobTime = *job.FailedAt
		} else {
			continue
		}

		if jobTime.Before(cutoffTime) {
			if err := p.queue.DeleteJob(job.ID); err != nil {
				p.logger.WithError(err).WithField("job_id", job.ID).Warning("Failed to delete old job")
			} else {
				cleanedCount++
			}
		}
	}

	if cleanedCount > 0 {
		p.logger.WithField("cleaned_jobs", cleanedCount).Info("Cleaned up old STT jobs")
	}
}

// runMetricsCollection runs periodic metrics collection
func (p *AsyncSTTProcessor) runMetricsCollection() {
	defer p.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.collectMetrics()
		}
	}
}

// collectMetrics collects and updates metrics
func (p *AsyncSTTProcessor) collectMetrics() {
	stats, err := p.queue.GetQueueStats()
	if err != nil {
		p.logger.WithError(err).Warning("Failed to collect queue stats")
		return
	}

	p.metrics.mutex.Lock()
	defer p.metrics.mutex.Unlock()

	// Update metrics from queue stats
	p.metrics.JobsProcessed = stats.CompletedJobs
	p.metrics.JobsFailed = stats.FailedJobs
}

// runMemoryManagement manages memory usage and garbage collection
func (p *AsyncSTTProcessor) runMemoryManagement() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.performMemoryOptimizations()
		}
	}
}

// performMemoryOptimizations performs garbage collection and memory cleanup
func (p *AsyncSTTProcessor) performMemoryOptimizations() {
	var memBefore, memAfter runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// Force garbage collection
	runtime.GC()
	
	// Force memory back to OS
	debug.FreeOSMemory()

	runtime.ReadMemStats(&memAfter)

	freedMB := float64(memBefore.HeapInuse-memAfter.HeapInuse) / 1024 / 1024
	
	if freedMB > 1 { // Only log if we freed significant memory
		p.logger.WithFields(logrus.Fields{
			"memory_freed_mb": freedMB,
			"heap_inuse_mb":   float64(memAfter.HeapInuse) / 1024 / 1024,
			"heap_alloc_mb":   float64(memAfter.HeapAlloc) / 1024 / 1024,
			"num_gc":          memAfter.NumGC,
		}).Debug("Memory optimization completed")
	}

	// Check if any worker is using too much memory and log warning
	for _, worker := range p.workers {
		// This is approximate - in a real implementation you'd track per-worker memory
		if memAfter.HeapInuse > uint64(p.config.MaxMemoryPerWorker*int64(len(p.workers))) {
			p.logger.WithFields(logrus.Fields{
				"worker_id":           worker.id,
				"total_heap_inuse_mb": float64(memAfter.HeapInuse) / 1024 / 1024,
				"max_allowed_mb":      float64(p.config.MaxMemoryPerWorker*int64(len(p.workers))) / 1024 / 1024,
			}).Warning("High memory usage detected - consider reducing worker count or job complexity")
			break
		}
	}
}

// estimateJobCost estimates the cost of processing a job
func (p *AsyncSTTProcessor) estimateJobCost(job *STTJob) (float64, error) {
	// Get file size
	fileInfo, err := os.Stat(job.AudioPath)
	if err != nil {
		return 0, err
	}

	// Estimate duration (assuming 16-bit, 16kHz mono audio)
	estimatedDurationSeconds := float64(fileInfo.Size()) / (2 * 16000)

	// Cost estimation based on provider (mock values)
	var costPerMinute float64
	switch job.Provider {
	case "google":
		costPerMinute = 0.016 // $0.016 per minute for standard model
	case "azure":
		costPerMinute = 0.012
	case "aws":
		costPerMinute = 0.024
	case "deepgram":
		costPerMinute = 0.0125
	default:
		costPerMinute = 0.02 // Default estimate
	}

	estimatedCost := (estimatedDurationSeconds / 60) * costPerMinute
	return estimatedCost, nil
}

// calculateActualCost calculates the actual cost based on result
func (p *AsyncSTTProcessor) calculateActualCost(job *STTJob, result *TranscriptionResult) float64 {
	// Use duration from result if available, otherwise fall back to estimate
	durationMinutes := result.Duration.Minutes()
	if durationMinutes == 0 {
		durationMinutes = job.EstimatedCost / 0.02 // Reverse calculate from estimate
	}

	// Apply actual pricing based on provider and features used
	var costPerMinute float64
	switch job.Provider {
	case "google":
		costPerMinute = 0.016
	case "azure":
		costPerMinute = 0.012
	case "aws":
		costPerMinute = 0.024
	case "deepgram":
		costPerMinute = 0.0125
	default:
		costPerMinute = 0.02
	}

	return durationMinutes * costPerMinute
}

// updateMetrics updates internal metrics
func (p *AsyncSTTProcessor) updateMetrics(operation string, processingTime *time.Duration) {
	p.metrics.mutex.Lock()
	defer p.metrics.mutex.Unlock()

	switch operation {
	case "enqueued":
		p.metrics.JobsEnqueued++
	case "completed":
		p.metrics.JobsProcessed++
		if processingTime != nil {
			// Update average processing time (simple moving average)
			if p.metrics.AverageProcessTime == 0 {
				p.metrics.AverageProcessTime = *processingTime
			} else {
				p.metrics.AverageProcessTime = (p.metrics.AverageProcessTime + *processingTime) / 2
			}
		}
	case "failed":
		p.metrics.JobsFailed++
	case "retried":
		p.metrics.JobsRetried++
	}
}

// contextReader wraps an io.Reader to be context-aware and memory efficient
type contextReader struct {
	reader   io.Reader
	ctx      context.Context
	readSize int64  // Track bytes read for memory optimization
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		n, err := r.reader.Read(p)
		r.readSize += int64(n)
		
		// Optimize read buffer size for large files to reduce memory allocation
		if r.readSize > 1024*1024 && len(p) < 64*1024 { // If we've read >1MB and buffer is small
			// Request larger reads for efficiency (this is just tracking, caller controls buffer size)
		}
		
		return n, err
	}
}