package realtime

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// WorkerPool provides a pool of workers for concurrent task processing
type WorkerPool struct {
	logger      *logrus.Entry
	workerCount int

	// Task queue
	taskChan chan Task
	workers  []*Worker

	// Control
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	startMutex sync.RWMutex

	// Statistics
	stats *PoolStats
}

// Worker represents a single worker in the pool
type Worker struct {
	id       int
	pool     *WorkerPool
	taskChan <-chan Task
	quit     chan struct{}
	stats    *WorkerStats
}

// Task represents a task to be executed by a worker
type Task struct {
	ID       string
	Function func()
	Priority int
	Created  time.Time
}

// PoolStats tracks worker pool statistics
type PoolStats struct {
	mutex           sync.RWMutex
	TotalTasks      int64     `json:"total_tasks"`
	CompletedTasks  int64     `json:"completed_tasks"`
	FailedTasks     int64     `json:"failed_tasks"`
	ActiveWorkers   int       `json:"active_workers"`
	IdleWorkers     int       `json:"idle_workers"`
	QueueSize       int       `json:"queue_size"`
	QueueCapacity   int       `json:"queue_capacity"`
	AverageWaitTime int64     `json:"average_wait_time_ms"`
	AverageExecTime int64     `json:"average_exec_time_ms"`
	DroppedTasks    int64     `json:"dropped_tasks"`
	LastReset       time.Time `json:"last_reset"`
}

// WorkerStats tracks individual worker statistics
type WorkerStats struct {
	mutex         sync.RWMutex
	WorkerID      int       `json:"worker_id"`
	TasksExecuted int64     `json:"tasks_executed"`
	TasksFailed   int64     `json:"tasks_failed"`
	TotalExecTime int64     `json:"total_exec_time_ms"`
	LastTaskTime  time.Time `json:"last_task_time"`
	IsActive      bool      `json:"is_active"`
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workerCount int, logger *logrus.Logger) *WorkerPool {
	// Use reasonable defaults
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}

	queueSize := workerCount * 10 // 10 tasks per worker queue buffer

	ctx, cancel := context.WithCancel(context.Background())

	wp := &WorkerPool{
		logger:      logger.WithField("component", "worker_pool"),
		workerCount: workerCount,
		taskChan:    make(chan Task, queueSize),
		workers:     make([]*Worker, 0, workerCount),
		ctx:         ctx,
		cancel:      cancel,
		stats: &PoolStats{
			QueueCapacity: queueSize,
			LastReset:     time.Now(),
		},
	}

	return wp
}

// Start starts the worker pool
func (wp *WorkerPool) Start() error {
	wp.startMutex.Lock()
	defer wp.startMutex.Unlock()

	if wp.started {
		return nil
	}

	// Create and start workers
	for i := 0; i < wp.workerCount; i++ {
		worker := &Worker{
			id:       i + 1,
			pool:     wp,
			taskChan: wp.taskChan,
			quit:     make(chan struct{}),
			stats: &WorkerStats{
				WorkerID: i + 1,
			},
		}

		wp.workers = append(wp.workers, worker)
		go worker.start()
	}

	wp.started = true
	wp.logger.WithField("worker_count", wp.workerCount).Info("Worker pool started")

	return nil
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop() error {
	wp.startMutex.Lock()
	defer wp.startMutex.Unlock()

	if !wp.started {
		return nil
	}

	// Cancel context
	wp.cancel()

	// Stop all workers
	for _, worker := range wp.workers {
		close(worker.quit)
	}

	// Close task channel
	close(wp.taskChan)

	wp.started = false
	wp.logger.Info("Worker pool stopped")

	return nil
}

// Submit submits a task to the worker pool
func (wp *WorkerPool) Submit(fn func()) {
	wp.SubmitWithPriority(fn, 0)
}

// SubmitWithPriority submits a task with a specific priority
func (wp *WorkerPool) SubmitWithPriority(fn func(), priority int) {
	if fn == nil {
		return
	}

	wp.startMutex.RLock()
	if !wp.started {
		wp.startMutex.RUnlock()
		// Auto-start if not started
		_ = wp.Start()
		wp.startMutex.RLock()
	}
	wp.startMutex.RUnlock()

	task := Task{
		ID:       generateTaskID(),
		Function: fn,
		Priority: priority,
		Created:  time.Now(),
	}

	select {
	case wp.taskChan <- task:
		wp.stats.mutex.Lock()
		wp.stats.TotalTasks++
		wp.stats.QueueSize = len(wp.taskChan)
		wp.stats.mutex.Unlock()

	case <-wp.ctx.Done():
		// Pool is shutting down
		return

	default:
		// Queue is full, drop task
		wp.stats.mutex.Lock()
		wp.stats.DroppedTasks++
		wp.stats.mutex.Unlock()
		wp.logger.Warning("Worker pool queue full, dropping task")
	}
}

// start starts a worker
func (w *Worker) start() {
	defer func() {
		if r := recover(); r != nil {
			w.pool.logger.WithFields(logrus.Fields{
				"worker_id": w.id,
				"panic":     r,
			}).Error("Worker panic recovered")
		}
	}()

	w.pool.logger.WithField("worker_id", w.id).Debug("Worker started")

	for {
		select {
		case task, ok := <-w.taskChan:
			if !ok {
				// Channel closed, exit
				return
			}

			w.executeTask(task)

		case <-w.quit:
			return

		case <-w.pool.ctx.Done():
			return
		}
	}
}

// executeTask executes a single task
func (w *Worker) executeTask(task Task) {
	startTime := time.Now()
	waitTime := startTime.Sub(task.Created)

	// Update worker stats
	w.stats.mutex.Lock()
	w.stats.IsActive = true
	w.stats.LastTaskTime = startTime
	w.stats.mutex.Unlock()

	// Update pool stats
	w.pool.stats.mutex.Lock()
	w.pool.stats.ActiveWorkers++
	w.pool.stats.IdleWorkers--
	if w.pool.stats.IdleWorkers < 0 {
		w.pool.stats.IdleWorkers = 0
	}
	w.pool.stats.mutex.Unlock()

	defer func() {
		execTime := time.Since(startTime)

		// Update worker stats
		w.stats.mutex.Lock()
		w.stats.IsActive = false
		w.stats.TasksExecuted++
		w.stats.TotalExecTime += execTime.Nanoseconds() / 1e6
		w.stats.mutex.Unlock()

		// Update pool stats
		w.pool.stats.mutex.Lock()
		w.pool.stats.ActiveWorkers--
		w.pool.stats.IdleWorkers++
		w.pool.stats.CompletedTasks++
		w.pool.stats.QueueSize = len(w.pool.taskChan)

		// Update average times
		if w.pool.stats.CompletedTasks > 0 {
			totalWaitTime := w.pool.stats.AverageWaitTime * (w.pool.stats.CompletedTasks - 1)
			w.pool.stats.AverageWaitTime = (totalWaitTime + waitTime.Nanoseconds()/1e6) / w.pool.stats.CompletedTasks

			totalExecTime := w.pool.stats.AverageExecTime * (w.pool.stats.CompletedTasks - 1)
			w.pool.stats.AverageExecTime = (totalExecTime + execTime.Nanoseconds()/1e6) / w.pool.stats.CompletedTasks
		}
		w.pool.stats.mutex.Unlock()

		if r := recover(); r != nil {
			w.stats.mutex.Lock()
			w.stats.TasksFailed++
			w.stats.mutex.Unlock()

			w.pool.stats.mutex.Lock()
			w.pool.stats.FailedTasks++
			w.pool.stats.mutex.Unlock()

			w.pool.logger.WithFields(logrus.Fields{
				"worker_id": w.id,
				"task_id":   task.ID,
				"panic":     r,
			}).Error("Task execution panic")
		}
	}()

	// Execute the task
	task.Function()
}

// generateTaskID generates a unique task ID
func generateTaskID() string {
	return time.Now().Format("20060102150405.000000")
}
