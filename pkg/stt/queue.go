package stt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// STTJob represents a speech-to-text processing job
type STTJob struct {
	ID             string                 `json:"id"`
	CallUUID       string                 `json:"call_uuid"`
	SessionID      string                 `json:"session_id"`
	AudioPath      string                 `json:"audio_path"`
	Provider       string                 `json:"provider"`
	Language       string                 `json:"language"`
	Priority       int                    `json:"priority"` // 1=high, 2=normal, 3=low
	CreatedAt      time.Time              `json:"created_at"`
	StartedAt      *time.Time             `json:"started_at,omitempty"`
	CompletedAt    *time.Time             `json:"completed_at,omitempty"`
	FailedAt       *time.Time             `json:"failed_at,omitempty"`
	RetryCount     int                    `json:"retry_count"`
	MaxRetries     int                    `json:"max_retries"`
	Status         STTJobStatus           `json:"status"`
	Result         *TranscriptionResult   `json:"result,omitempty"`
	Error          string                 `json:"error,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	ProcessingTime time.Duration          `json:"processing_time"`
	EstimatedCost  float64                `json:"estimated_cost"`
	ActualCost     float64                `json:"actual_cost"`
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

var redisTrackedStatuses = []STTJobStatus{
	StatusPending,
	StatusQueued,
	StatusProcessing,
	StatusCompleted,
	StatusFailed,
	StatusRetrying,
	StatusCancelled,
}

var errRedisJobNotFound = errors.New("stt job not found")

// TranscriptionResult represents the result of STT processing
type TranscriptionResult struct {
	Text         string                     `json:"text"`
	Confidence   float64                    `json:"confidence"`
	Language     string                     `json:"language"`
	Duration     time.Duration              `json:"duration"`
	WordCount    int                        `json:"word_count"`
	Segments     []TranscriptionSegment     `json:"segments,omitempty"`
	Alternatives []TranscriptionAlternative `json:"alternatives,omitempty"`
	Provider     string                     `json:"provider"`
	ModelUsed    string                     `json:"model_used"`
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
	client       redis.UniversalClient
	keyPrefix    string
	logger       *logrus.Logger
	jobTTL       time.Duration
	opTimeout    time.Duration
	blockTimeout time.Duration
	historyLimit int
}

// NewRedisSTTQueue creates a new Redis-backed STT queue
func NewRedisSTTQueue(client redis.UniversalClient, keyPrefix string, logger *logrus.Logger) *RedisSTTQueue {
	if logger == nil {
		logger = logrus.New()
	}

	if keyPrefix == "" {
		keyPrefix = "siprec:stt_queue"
	}

	return &RedisSTTQueue{
		client:       client,
		keyPrefix:    strings.TrimSuffix(keyPrefix, ":"),
		logger:       logger,
		jobTTL:       24 * time.Hour,
		opTimeout:    5 * time.Second,
		blockTimeout: time.Second,
		historyLimit: 200,
	}
}

// SetJobTTL overrides the default TTL applied to job records.
func (q *RedisSTTQueue) SetJobTTL(ttl time.Duration) {
	if ttl > 0 {
		q.jobTTL = ttl
	}
}

// SetOperationTimeout overrides the timeout used for Redis operations.
func (q *RedisSTTQueue) SetOperationTimeout(timeout time.Duration) {
	if timeout > 0 {
		q.opTimeout = timeout
	}
}

// SetBlockTimeout overrides how long a worker will block when dequeueing before re-checking context cancellation.
func (q *RedisSTTQueue) SetBlockTimeout(timeout time.Duration) {
	if timeout > 0 {
		q.blockTimeout = timeout
	}
}

// SetHistoryLimit bounds how many job IDs are retained per call UUID in the Redis index.
func (q *RedisSTTQueue) SetHistoryLimit(limit int) {
	if limit > 0 {
		q.historyLimit = limit
	}
}

// Enqueue adds a job to the Redis-backed queue
func (q *RedisSTTQueue) Enqueue(job *STTJob) error {
	if job == nil {
		return errors.New("cannot enqueue nil job")
	}

	if q.client == nil {
		return errors.New("redis client is not configured")
	}

	if job.ID == "" {
		return errors.New("job ID is required for Redis queue")
	}

	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}

	now := time.Now()
	job.Status = StatusQueued
	job.Error = ""
	job.StartedAt = nil
	job.CompletedAt = nil
	job.FailedAt = nil

	if err := q.saveJob(job); err != nil {
		return err
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	pipe := q.client.TxPipeline()
	pipe.LPush(ctx, q.queueKey(), job.ID)
	q.addStatusUpdates(pipe, ctx, job.ID, StatusQueued)

	if job.CallUUID != "" {
		callKey := q.callIndexKey(job.CallUUID)
		pipe.ZAdd(ctx, callKey, redis.Z{Score: float64(now.Unix()), Member: job.ID})
		pipe.Expire(ctx, callKey, q.jobTTL)

		if q.historyLimit > 0 {
			pipe.ZRemRangeByRank(ctx, callKey, 0, int64(-q.historyLimit-1))
		}
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to enqueue job %s in Redis: %w", job.ID, err)
	}

	q.logger.WithFields(logrus.Fields{
		"job_id":    job.ID,
		"call_uuid": job.CallUUID,
		"provider":  job.Provider,
	}).Info("STT job enqueued in Redis")

	return nil
}

// Dequeue retrieves the next job, blocking until available or context cancellation.
func (q *RedisSTTQueue) Dequeue(ctx context.Context) (*STTJob, error) {
	if q.client == nil {
		return nil, errors.New("redis client is not configured")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		blockCtx, cancel := context.WithTimeout(ctx, q.blockTimeout)
		result, err := q.client.BRPop(blockCtx, q.blockTimeout, q.queueKey()).Result()
		cancel()

		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}

			if blockCtx.Err() != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}

			return nil, fmt.Errorf("failed to dequeue job from Redis: %w", err)
		}

		if len(result) < 2 {
			continue
		}

		jobID := result[1]
		job, err := q.loadJob(jobID)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				q.logger.WithField("job_id", jobID).Warning("Job payload missing after dequeue; skipping")
				continue
			}
			return nil, err
		}

		now := time.Now()
		job.Status = StatusProcessing
		job.StartedAt = &now

		if err := q.saveJob(job); err != nil {
			return nil, err
		}

		opCtx, cancelStatus := q.newOpContext()
		pipe := q.client.TxPipeline()
		q.addStatusUpdates(pipe, opCtx, jobID, StatusProcessing)
		pipe.Expire(opCtx, q.jobKey(jobID), q.jobTTL)
		if _, err := pipe.Exec(opCtx); err != nil {
			cancelStatus()
			q.logger.WithError(err).WithField("job_id", jobID).
				Warning("Failed to update Redis status sets for dequeued job")
		} else {
			cancelStatus()
		}

		q.logger.WithFields(logrus.Fields{
			"job_id":    job.ID,
			"call_uuid": job.CallUUID,
		}).Info("STT job dequeued from Redis")

		return job, nil
	}
}

// GetQueueSize returns the number of queued jobs in Redis
func (q *RedisSTTQueue) GetQueueSize() (int, error) {
	if q.client == nil {
		return 0, errors.New("redis client is not configured")
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	len, err := q.client.LLen(ctx, q.queueKey()).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to read Redis queue length: %w", err)
	}

	return int(len), nil
}

// GetQueueStats returns aggregated queue statistics from Redis indexes
func (q *RedisSTTQueue) GetQueueStats() (*QueueStats, error) {
	if q.client == nil {
		return nil, errors.New("redis client is not configured")
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	pipe := q.client.Pipeline()
	pendingCmd := pipe.SCard(ctx, q.statusKey(StatusQueued))
	processingCmd := pipe.SCard(ctx, q.statusKey(StatusProcessing))
	completedCmd := pipe.SCard(ctx, q.statusKey(StatusCompleted))
	failedCmd := pipe.SCard(ctx, q.statusKey(StatusFailed))
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to gather Redis queue statistics: %w", err)
	}

	stats := &QueueStats{
		PendingJobs:    pendingCmd.Val(),
		ProcessingJobs: processingCmd.Val(),
		CompletedJobs:  completedCmd.Val(),
		FailedJobs:     failedCmd.Val(),
	}
	stats.TotalJobs = stats.PendingJobs + stats.ProcessingJobs + stats.CompletedJobs + stats.FailedJobs

	if stats.TotalJobs > 0 {
		stats.ErrorRate = float64(stats.FailedJobs) / float64(stats.TotalJobs) * 100
	}

	return stats, nil
}

// UpdateJob updates job metadata and moves status indexes accordingly
func (q *RedisSTTQueue) UpdateJob(job *STTJob) error {
	if q.client == nil {
		return errors.New("redis client is not configured")
	}

	if job == nil {
		return errors.New("cannot update nil job")
	}

	if _, err := q.GetJob(job.ID); err != nil {
		return err
	}

	if err := q.saveJob(job); err != nil {
		return err
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	pipe := q.client.TxPipeline()
	for _, status := range redisTrackedStatuses {
		pipe.SRem(ctx, q.statusKey(status), job.ID)
	}
	pipe.SAdd(ctx, q.statusKey(job.Status), job.ID)
	pipe.Expire(ctx, q.jobKey(job.ID), q.jobTTL)

	switch job.Status {
	case StatusCompleted:
		pipe.Incr(ctx, q.metricsKey("jobs_completed"))
	case StatusFailed:
		pipe.Incr(ctx, q.metricsKey("jobs_failed"))
	case StatusRetrying:
		pipe.Incr(ctx, q.metricsKey("jobs_retried"))
	}

	if job.ActualCost > 0 {
		pipe.IncrByFloat(ctx, q.metricsKey("total_cost"), job.ActualCost)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to update job %s in Redis: %w", job.ID, err)
	}

	return nil
}

// GetJob retrieves a job payload by ID
func (q *RedisSTTQueue) GetJob(jobID string) (*STTJob, error) {
	if q.client == nil {
		return nil, errors.New("redis client is not configured")
	}

	job, err := q.loadJob(jobID)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("job %s not found: %w", jobID, errRedisJobNotFound)
		}
		return nil, err
	}

	return job, nil
}

// DeleteJob removes a job from Redis storage and indexes
func (q *RedisSTTQueue) DeleteJob(jobID string) error {
	if q.client == nil {
		return errors.New("redis client is not configured")
	}

	var callUUID string
	if job, err := q.loadJob(jobID); err == nil {
		callUUID = job.CallUUID
	} else if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to load job %s for deletion: %w", jobID, err)
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	pipe := q.client.TxPipeline()
	pipe.Del(ctx, q.jobKey(jobID))
	pipe.LRem(ctx, q.queueKey(), 0, jobID)
	for _, status := range redisTrackedStatuses {
		pipe.SRem(ctx, q.statusKey(status), jobID)
	}

	if callUUID != "" {
		pipe.ZRem(ctx, q.callIndexKey(callUUID), jobID)
	}

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to delete job %s from Redis: %w", jobID, err)
	}

	return nil
}

// GetJobsByStatus returns jobs by status using Redis sets
func (q *RedisSTTQueue) GetJobsByStatus(status STTJobStatus) ([]*STTJob, error) {
	if q.client == nil {
		return nil, errors.New("redis client is not configured")
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	ids, err := q.client.SMembers(ctx, q.statusKey(status)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to load jobs for status %s: %w", status, err)
	}

	jobs := make([]*STTJob, 0, len(ids))
	for _, id := range ids {
		job, err := q.GetJob(id)
		if err != nil {
			if errors.Is(err, errRedisJobNotFound) {
				continue
			}
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// GetJobsByCallUUID returns all jobs associated with a call UUID
func (q *RedisSTTQueue) GetJobsByCallUUID(callUUID string) ([]*STTJob, error) {
	if q.client == nil {
		return nil, errors.New("redis client is not configured")
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	ids, err := q.client.ZRevRange(ctx, q.callIndexKey(callUUID), 0, -1).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return []*STTJob{}, nil
		}
		return nil, fmt.Errorf("failed to load jobs for call %s: %w", callUUID, err)
	}

	jobs := make([]*STTJob, 0, len(ids))
	for _, id := range ids {
		job, err := q.GetJob(id)
		if err != nil {
			if errors.Is(err, errRedisJobNotFound) {
				continue
			}
			return nil, err
		}
		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Purge removes all queue-related keys from Redis
func (q *RedisSTTQueue) Purge() error {
	if q.client == nil {
		return errors.New("redis client is not configured")
	}

	pattern := fmt.Sprintf("%s:*", q.keyPrefix)
	ctx := context.Background()

	iter := q.client.Scan(ctx, 0, pattern, 500).Iterator()
	var batch []string

	for iter.Next(ctx) {
		batch = append(batch, iter.Val())
		if len(batch) >= 200 {
			if err := q.deleteKeys(batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan Redis keys for purge: %w", err)
	}

	if len(batch) > 0 {
		if err := q.deleteKeys(batch); err != nil {
			return err
		}
	}

	q.logger.Warn("Redis STT queue purged")
	return nil
}

// Close releases references; the client lifecycle is managed externally.
func (q *RedisSTTQueue) Close() error {
	q.client = nil
	return nil
}

func (q *RedisSTTQueue) newOpContext() (context.Context, context.CancelFunc) {
	timeout := q.opTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return context.WithTimeout(context.Background(), timeout)
}

func (q *RedisSTTQueue) queueKey() string {
	return fmt.Sprintf("%s:queue", q.keyPrefix)
}

func (q *RedisSTTQueue) jobKey(jobID string) string {
	return fmt.Sprintf("%s:job:%s", q.keyPrefix, jobID)
}

func (q *RedisSTTQueue) statusKey(status STTJobStatus) string {
	return fmt.Sprintf("%s:status:%s", q.keyPrefix, status)
}

func (q *RedisSTTQueue) callIndexKey(callUUID string) string {
	return fmt.Sprintf("%s:call:%s", q.keyPrefix, callUUID)
}

func (q *RedisSTTQueue) metricsKey(name string) string {
	return fmt.Sprintf("%s:metrics:%s", q.keyPrefix, name)
}

func (q *RedisSTTQueue) saveJob(job *STTJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to serialize job %s: %w", job.ID, err)
	}

	ctx, cancel := q.newOpContext()
	defer cancel()

	if err := q.client.Set(ctx, q.jobKey(job.ID), data, q.jobTTL).Err(); err != nil {
		return fmt.Errorf("failed to persist job %s to Redis: %w", job.ID, err)
	}

	return nil
}

func (q *RedisSTTQueue) loadJob(jobID string) (*STTJob, error) {
	ctx, cancel := q.newOpContext()
	defer cancel()

	data, err := q.client.Get(ctx, q.jobKey(jobID)).Bytes()
	if err != nil {
		return nil, err
	}

	var job STTJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("failed to decode job %s from Redis: %w", jobID, err)
	}

	return &job, nil
}

func (q *RedisSTTQueue) addStatusUpdates(pipe redis.Pipeliner, ctx context.Context, jobID string, status STTJobStatus) {
	for _, tracked := range redisTrackedStatuses {
		pipe.SRem(ctx, q.statusKey(tracked), jobID)
	}
	pipe.SAdd(ctx, q.statusKey(status), jobID)
}

func (q *RedisSTTQueue) deleteKeys(keys []string) error {
	ctx, cancel := q.newOpContext()
	defer cancel()

	if err := q.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to delete Redis keys: %w", err)
	}

	return nil
}
