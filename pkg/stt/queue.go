package stt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// STTJob represents a speech-to-text processing job
type STTJob struct {
	ID              string                 `json:"id"`
	CallUUID        string                 `json:"call_uuid"`
	SessionID       string                 `json:"session_id"`
	AudioPath       string                 `json:"audio_path"`
	Provider        string                 `json:"provider"`
	Language        string                 `json:"language"`
	Priority        int                    `json:"priority"` // 1=high, 2=normal, 3=low
	CreatedAt       time.Time              `json:"created_at"`
	StartedAt       *time.Time             `json:"started_at,omitempty"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
	FailedAt        *time.Time             `json:"failed_at,omitempty"`
	RetryCount      int                    `json:"retry_count"`
	MaxRetries      int                    `json:"max_retries"`
	Status          STTJobStatus           `json:"status"`
	Result          *TranscriptionResult   `json:"result,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	ProcessingTime  time.Duration          `json:"processing_time"`
	EstimatedCost   float64                `json:"estimated_cost"`
	ActualCost      float64                `json:"actual_cost"`
}

// STTJobStatus represents the status of an STT job
type STTJobStatus string

const (
	StatusPending    STTJobStatus = "pending"
	StatusQueued     STTJobStatus = "queued"
	StatusProcessing STTJobStatus = "processing"
	StatusCompleted  STTJobStatus = "completed"
	StatusFailed     STTJobStatus = "failed"
	StatusRetrying   STTJobStatus = "retrying"
	StatusCancelled  STTJobStatus = "cancelled"
)

// TranscriptionResult represents the result of STT processing
type TranscriptionResult struct {
	Text        string                   `json:"text"`
	Confidence  float64                  `json:"confidence"`
	Language    string                   `json:"language"`
	Duration    time.Duration            `json:"duration"`
	WordCount   int                      `json:"word_count"`
	Segments    []TranscriptionSegment   `json:"segments,omitempty"`
	Alternatives []TranscriptionAlternative `json:"alternatives,omitempty"`
	Provider    string                   `json:"provider"`
	ModelUsed   string                   `json:"model_used"`
}


// TranscriptionAlternative represents alternative transcriptions
type TranscriptionAlternative struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// STTQueue interface defines the contract for STT job queueing
type STTQueue interface {
	// Queue management
	Enqueue(job *STTJob) error
	Dequeue(ctx context.Context) (*STTJob, error)
	GetQueueSize() (int, error)
	GetQueueStats() (*QueueStats, error)

	// Job management
	UpdateJob(job *STTJob) error
	GetJob(jobID string) (*STTJob, error)
	DeleteJob(jobID string) error
	GetJobsByStatus(status STTJobStatus) ([]*STTJob, error)
	GetJobsByCallUUID(callUUID string) ([]*STTJob, error)

	// Queue operations
	Purge() error
	Close() error
}

// QueueStats represents queue statistics
type QueueStats struct {
	TotalJobs       int64   `json:"total_jobs"`
	PendingJobs     int64   `json:"pending_jobs"`
	ProcessingJobs  int64   `json:"processing_jobs"`
	CompletedJobs   int64   `json:"completed_jobs"`
	FailedJobs      int64   `json:"failed_jobs"`
	AverageWaitTime float64 `json:"average_wait_time_seconds"`
	Throughput      float64 `json:"jobs_per_hour"`
	ErrorRate       float64 `json:"error_rate_percent"`
}

// MemorySTTQueue implements STTQueue using in-memory storage
type MemorySTTQueue struct {
	jobs     map[string]*STTJob
	queue    chan *STTJob
	mutex    sync.RWMutex
	logger   *logrus.Logger
	stats    *QueueStats
	statsMux sync.RWMutex
}

// NewMemorySTTQueue creates a new in-memory STT queue
func NewMemorySTTQueue(bufferSize int, logger *logrus.Logger) *MemorySTTQueue {
	return &MemorySTTQueue{
		jobs:   make(map[string]*STTJob),
		queue:  make(chan *STTJob, bufferSize),
		logger: logger,
		stats:  &QueueStats{},
	}
}

// Enqueue adds a job to the queue
func (q *MemorySTTQueue) Enqueue(job *STTJob) error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	// Store job
	q.jobs[job.ID] = job
	job.Status = StatusQueued

	// Add to queue with priority handling
	select {
	case q.queue <- job:
		q.updateStats("enqueued")
		q.logger.WithFields(logrus.Fields{
			"job_id":    job.ID,
			"call_uuid": job.CallUUID,
			"provider":  job.Provider,
			"priority":  job.Priority,
		}).Info("STT job enqueued")
		return nil
	default:
		job.Status = StatusFailed
		job.Error = "queue is full"
		return fmt.Errorf("STT queue is full, cannot enqueue job %s", job.ID)
	}
}

// Dequeue retrieves a job from the queue
func (q *MemorySTTQueue) Dequeue(ctx context.Context) (*STTJob, error) {
	select {
	case job := <-q.queue:
		q.mutex.Lock()
		job.Status = StatusProcessing
		now := time.Now()
		job.StartedAt = &now
		q.mutex.Unlock()

		q.updateStats("dequeued")
		q.logger.WithFields(logrus.Fields{
			"job_id":    job.ID,
			"call_uuid": job.CallUUID,
			"provider":  job.Provider,
		}).Info("STT job dequeued for processing")
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// UpdateJob updates a job's status and result
func (q *MemorySTTQueue) UpdateJob(job *STTJob) error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if _, exists := q.jobs[job.ID]; !exists {
		return fmt.Errorf("job %s not found", job.ID)
	}

	q.jobs[job.ID] = job
	q.updateStats("updated")

	q.logger.WithFields(logrus.Fields{
		"job_id": job.ID,
		"status": job.Status,
		"error":  job.Error,
	}).Debug("STT job updated")

	return nil
}

// GetJob retrieves a job by ID
func (q *MemorySTTQueue) GetJob(jobID string) (*STTJob, error) {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	job, exists := q.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	return job, nil
}

// DeleteJob removes a job from storage
func (q *MemorySTTQueue) DeleteJob(jobID string) error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if _, exists := q.jobs[jobID]; !exists {
		return fmt.Errorf("job %s not found", jobID)
	}

	delete(q.jobs, jobID)
	q.updateStats("deleted")

	q.logger.WithField("job_id", jobID).Debug("STT job deleted")
	return nil
}

// GetJobsByStatus retrieves jobs by status
func (q *MemorySTTQueue) GetJobsByStatus(status STTJobStatus) ([]*STTJob, error) {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	var jobs []*STTJob
	for _, job := range q.jobs {
		if job.Status == status {
			jobs = append(jobs, job)
		}
	}

	return jobs, nil
}

// GetJobsByCallUUID retrieves jobs by call UUID
func (q *MemorySTTQueue) GetJobsByCallUUID(callUUID string) ([]*STTJob, error) {
	q.mutex.RLock()
	defer q.mutex.RUnlock()

	var jobs []*STTJob
	for _, job := range q.jobs {
		if job.CallUUID == callUUID {
			jobs = append(jobs, job)
		}
	}

	return jobs, nil
}

// GetQueueSize returns the current queue size
func (q *MemorySTTQueue) GetQueueSize() (int, error) {
	return len(q.queue), nil
}

// GetQueueStats returns queue statistics
func (q *MemorySTTQueue) GetQueueStats() (*QueueStats, error) {
	q.statsMux.RLock()
	defer q.statsMux.RUnlock()

	// Calculate current stats
	q.mutex.RLock()
	pending := int64(0)
	processing := int64(0)
	completed := int64(0)
	failed := int64(0)

	for _, job := range q.jobs {
		switch job.Status {
		case StatusPending, StatusQueued:
			pending++
		case StatusProcessing:
			processing++
		case StatusCompleted:
			completed++
		case StatusFailed:
			failed++
		}
	}
	q.mutex.RUnlock()

	stats := &QueueStats{
		TotalJobs:      q.stats.TotalJobs,
		PendingJobs:    pending,
		ProcessingJobs: processing,
		CompletedJobs:  completed,
		FailedJobs:     failed,
		Throughput:     q.stats.Throughput,
	}

	// Calculate error rate
	if stats.TotalJobs > 0 {
		stats.ErrorRate = float64(failed) / float64(stats.TotalJobs) * 100
	}

	return stats, nil
}

// Purge removes all jobs from the queue
func (q *MemorySTTQueue) Purge() error {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	// Drain the queue
	for len(q.queue) > 0 {
		<-q.queue
	}

	// Clear job storage
	q.jobs = make(map[string]*STTJob)

	q.logger.Info("STT queue purged")
	return nil
}

// Close closes the queue
func (q *MemorySTTQueue) Close() error {
	close(q.queue)
	q.logger.Info("STT queue closed")
	return nil
}

// updateStats updates internal statistics
func (q *MemorySTTQueue) updateStats(operation string) {
	q.statsMux.Lock()
	defer q.statsMux.Unlock()

	switch operation {
	case "enqueued":
		q.stats.TotalJobs++
	}
}

// RedisSTTQueue implements STTQueue using Redis
type RedisSTTQueue struct {
	client   interface{} // Redis client interface
	keyPrefix string
	logger   *logrus.Logger
}

// NewRedisSTTQueue creates a new Redis-backed STT queue
func NewRedisSTTQueue(client interface{}, keyPrefix string, logger *logrus.Logger) *RedisSTTQueue {
	return &RedisSTTQueue{
		client:    client,
		keyPrefix: keyPrefix,
		logger:    logger,
	}
}

// Implementation methods for RedisSTTQueue would go here
// For now, implementing the interface to satisfy the contract

func (q *RedisSTTQueue) Enqueue(job *STTJob) error {
	// TODO: Implement Redis-based enqueue
	q.logger.Warning("Redis STT queue not fully implemented")
	return fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) Dequeue(ctx context.Context) (*STTJob, error) {
	return nil, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) GetQueueSize() (int, error) {
	return 0, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) GetQueueStats() (*QueueStats, error) {
	return nil, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) UpdateJob(job *STTJob) error {
	return fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) GetJob(jobID string) (*STTJob, error) {
	return nil, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) DeleteJob(jobID string) error {
	return fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) GetJobsByStatus(status STTJobStatus) ([]*STTJob, error) {
	return nil, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) GetJobsByCallUUID(callUUID string) ([]*STTJob, error) {
	return nil, fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) Purge() error {
	return fmt.Errorf("Redis STT queue not implemented")
}

func (q *RedisSTTQueue) Close() error {
	return fmt.Errorf("Redis STT queue not implemented")
}