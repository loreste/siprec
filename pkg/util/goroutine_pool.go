package util

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Task represents a unit of work to be executed
type Task func()

// GoroutinePool manages a pool of goroutines for concurrent task execution
type GoroutinePool struct {
	workers    int32
	maxWorkers int32
	taskQueue  chan Task
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	stats      *PoolStats
}

// PoolStats tracks pool performance metrics
type PoolStats struct {
	TasksSubmitted int64
	TasksCompleted int64
	ActiveWorkers  int32
	QueueLength    int32
	TotalWorkers   int32
}

// NewGoroutinePool creates a new goroutine pool
func NewGoroutinePool(maxWorkers int, queueSize int) *GoroutinePool {
	if maxWorkers <= 0 {
		maxWorkers = runtime.NumCPU()
	}
	if queueSize <= 0 {
		queueSize = maxWorkers * 10
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &GoroutinePool{
		maxWorkers: int32(maxWorkers),
		taskQueue:  make(chan Task, queueSize),
		ctx:        ctx,
		cancel:     cancel,
		stats:      &PoolStats{},
	}
}

// Start initializes the goroutine pool with initial workers
func (gp *GoroutinePool) Start(initialWorkers int) {
	if initialWorkers <= 0 {
		initialWorkers = int(gp.maxWorkers) / 2
	}
	if initialWorkers > int(gp.maxWorkers) {
		initialWorkers = int(gp.maxWorkers)
	}

	// Start initial workers
	for i := 0; i < initialWorkers; i++ {
		gp.addWorker()
	}
}

// Submit adds a task to the pool
func (gp *GoroutinePool) Submit(task Task) bool {
	if task == nil {
		return false
	}

	select {
	case gp.taskQueue <- task:
		atomic.AddInt64(&gp.stats.TasksSubmitted, 1)
		atomic.StoreInt32(&gp.stats.QueueLength, int32(len(gp.taskQueue)))

		// Scale up workers if needed
		gp.scaleUp()
		return true
	case <-gp.ctx.Done():
		return false
	default:
		// Queue is full, try to add more workers if possible
		if gp.scaleUp() {
			// Retry submission after scaling up
			select {
			case gp.taskQueue <- task:
				atomic.AddInt64(&gp.stats.TasksSubmitted, 1)
				atomic.StoreInt32(&gp.stats.QueueLength, int32(len(gp.taskQueue)))
				return true
			default:
				return false
			}
		}
		return false
	}
}

// addWorker creates a new worker goroutine
func (gp *GoroutinePool) addWorker() {
	atomic.AddInt32(&gp.workers, 1)
	atomic.AddInt32(&gp.stats.TotalWorkers, 1)

	gp.wg.Add(1)
	go gp.worker()
}

// worker is the main worker function
func (gp *GoroutinePool) worker() {
	defer gp.wg.Done()
	defer atomic.AddInt32(&gp.workers, -1)

	idleTimer := time.NewTimer(30 * time.Second) // Worker idle timeout
	defer idleTimer.Stop()

	for {
		select {
		case task := <-gp.taskQueue:
			if task != nil {
				atomic.AddInt32(&gp.stats.ActiveWorkers, 1)
				atomic.StoreInt32(&gp.stats.QueueLength, int32(len(gp.taskQueue)))

				// Execute the task with panic recovery
				func() {
					defer func() {
						if r := recover(); r != nil {
							// Log panic but don't crash the worker
							// In production, this should be logged properly
						}
						atomic.AddInt32(&gp.stats.ActiveWorkers, -1)
						atomic.AddInt64(&gp.stats.TasksCompleted, 1)
					}()
					task()
				}()

				// Reset idle timer after completing work
				idleTimer.Reset(30 * time.Second)
			}

		case <-idleTimer.C:
			// Worker has been idle, check if we should terminate
			if gp.shouldTerminateWorker() {
				return
			}
			idleTimer.Reset(30 * time.Second)

		case <-gp.ctx.Done():
			return
		}
	}
}

// scaleUp adds more workers if needed and possible
func (gp *GoroutinePool) scaleUp() bool {
	currentWorkers := atomic.LoadInt32(&gp.workers)
	queueLen := int32(len(gp.taskQueue))

	// Add worker if queue is getting full and we haven't reached max
	if queueLen > currentWorkers*2 && currentWorkers < gp.maxWorkers {
		gp.addWorker()
		return true
	}

	return false
}

// shouldTerminateWorker determines if a worker should terminate due to low load
func (gp *GoroutinePool) shouldTerminateWorker() bool {
	currentWorkers := atomic.LoadInt32(&gp.workers)
	queueLen := int32(len(gp.taskQueue))

	// Keep at least 1 worker, and don't terminate if queue has work
	if currentWorkers <= 1 || queueLen > 0 {
		return false
	}

	// Terminate worker if we have too many workers for current load
	return queueLen < currentWorkers/4
}

// GetStats returns current pool statistics
func (gp *GoroutinePool) GetStats() PoolStats {
	return PoolStats{
		TasksSubmitted: atomic.LoadInt64(&gp.stats.TasksSubmitted),
		TasksCompleted: atomic.LoadInt64(&gp.stats.TasksCompleted),
		ActiveWorkers:  atomic.LoadInt32(&gp.stats.ActiveWorkers),
		QueueLength:    int32(len(gp.taskQueue)),
		TotalWorkers:   atomic.LoadInt32(&gp.workers),
	}
}

// Shutdown gracefully shuts down the pool
func (gp *GoroutinePool) Shutdown(timeout time.Duration) {
	gp.cancel()

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		gp.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All workers finished
	case <-time.After(timeout):
		// Timeout exceeded, workers may still be running
	}
}

// WorkerPoolManager manages multiple worker pools for different types of work
type WorkerPoolManager struct {
	pools map[string]*GoroutinePool
	mu    sync.RWMutex
}

// NewWorkerPoolManager creates a new worker pool manager
func NewWorkerPoolManager() *WorkerPoolManager {
	return &WorkerPoolManager{
		pools: make(map[string]*GoroutinePool),
	}
}

// GetPool gets or creates a worker pool for the specified category
func (wpm *WorkerPoolManager) GetPool(category string, maxWorkers, queueSize int) *GoroutinePool {
	wpm.mu.RLock()
	pool, exists := wpm.pools[category]
	wpm.mu.RUnlock()

	if exists {
		return pool
	}

	wpm.mu.Lock()
	defer wpm.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := wpm.pools[category]; exists {
		return pool
	}

	// Create new pool
	pool = NewGoroutinePool(maxWorkers, queueSize)
	pool.Start(maxWorkers / 2)
	wpm.pools[category] = pool
	return pool
}

// SubmitTask submits a task to the specified pool category
func (wpm *WorkerPoolManager) SubmitTask(category string, task Task) bool {
	// Use default pool configuration for unknown categories
	pool := wpm.GetPool(category, runtime.NumCPU(), runtime.NumCPU()*10)
	return pool.Submit(task)
}

// GetAllStats returns statistics for all pools
func (wpm *WorkerPoolManager) GetAllStats() map[string]PoolStats {
	wpm.mu.RLock()
	defer wpm.mu.RUnlock()

	stats := make(map[string]PoolStats)
	for category, pool := range wpm.pools {
		stats[category] = pool.GetStats()
	}
	return stats
}

// Shutdown gracefully shuts down all pools
func (wpm *WorkerPoolManager) Shutdown(timeout time.Duration) {
	wpm.mu.RLock()
	pools := make([]*GoroutinePool, 0, len(wpm.pools))
	for _, pool := range wpm.pools {
		pools = append(pools, pool)
	}
	wpm.mu.RUnlock()

	// Shutdown all pools concurrently
	var wg sync.WaitGroup
	for _, pool := range pools {
		wg.Add(1)
		go func(p *GoroutinePool) {
			defer wg.Done()
			p.Shutdown(timeout)
		}(pool)
	}

	wg.Wait()
}

// Global worker pool manager
var globalWorkerPoolManager = NewWorkerPoolManager()

// GetGlobalWorkerPoolManager returns the global worker pool manager
func GetGlobalWorkerPoolManager() *WorkerPoolManager {
	return globalWorkerPoolManager
}
