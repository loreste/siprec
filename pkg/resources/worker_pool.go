package resources

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// WorkerPool manages a bounded pool of goroutines for processing work
type WorkerPool struct {
	size      int
	workChan  chan func()
	logger    *logrus.Entry

	// Tracking
	activeWorkers int64
	submitted     int64
	completed     int64
	rejected      int64

	// Lifecycle
	wg       sync.WaitGroup
	stopChan chan struct{}
	stopped  int32
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(size int, logger *logrus.Logger) *WorkerPool {
	if size <= 0 {
		size = 4
	}

	// Buffer size is 2x workers to absorb bursts
	bufferSize := size * 2
	if bufferSize > 10000 {
		bufferSize = 10000
	}

	return &WorkerPool{
		size:     size,
		workChan: make(chan func(), bufferSize),
		logger:   logger.WithField("component", "worker_pool"),
		stopChan: make(chan struct{}),
	}
}

// Start launches the worker goroutines
func (wp *WorkerPool) Start() {
	wp.logger.WithField("size", wp.size).Info("Starting worker pool")

	for i := 0; i < wp.size; i++ {
		wp.wg.Add(1)
		go wp.worker(i)
	}
}

func (wp *WorkerPool) worker(id int) {
	defer wp.wg.Done()

	for {
		select {
		case <-wp.stopChan:
			return
		case work, ok := <-wp.workChan:
			if !ok {
				return
			}
			wp.executeWork(work)
		}
	}
}

func (wp *WorkerPool) executeWork(work func()) {
	atomic.AddInt64(&wp.activeWorkers, 1)
	defer atomic.AddInt64(&wp.activeWorkers, -1)

	// Recover from panics in work functions
	defer func() {
		if r := recover(); r != nil {
			wp.logger.WithField("panic", r).Error("Worker recovered from panic")
		}
	}()

	work()
	atomic.AddInt64(&wp.completed, 1)
}

// Submit adds work to the pool (non-blocking)
func (wp *WorkerPool) Submit(work func()) bool {
	if atomic.LoadInt32(&wp.stopped) == 1 {
		return false
	}

	select {
	case wp.workChan <- work:
		atomic.AddInt64(&wp.submitted, 1)
		return true
	default:
		atomic.AddInt64(&wp.rejected, 1)
		return false
	}
}

// SubmitWithTimeout adds work with a timeout
func (wp *WorkerPool) SubmitWithTimeout(work func(), timeout time.Duration) bool {
	if atomic.LoadInt32(&wp.stopped) == 1 {
		return false
	}

	select {
	case wp.workChan <- work:
		atomic.AddInt64(&wp.submitted, 1)
		return true
	case <-time.After(timeout):
		atomic.AddInt64(&wp.rejected, 1)
		return false
	}
}

// SubmitBlocking adds work and blocks until accepted
func (wp *WorkerPool) SubmitBlocking(work func()) bool {
	if atomic.LoadInt32(&wp.stopped) == 1 {
		return false
	}

	select {
	case <-wp.stopChan:
		return false
	case wp.workChan <- work:
		atomic.AddInt64(&wp.submitted, 1)
		return true
	}
}

// ActiveWorkers returns the number of workers currently executing work
func (wp *WorkerPool) ActiveWorkers() int {
	return int(atomic.LoadInt64(&wp.activeWorkers))
}

// PendingWork returns the number of work items waiting in the queue
func (wp *WorkerPool) PendingWork() int {
	return len(wp.workChan)
}

// Stats returns pool statistics
func (wp *WorkerPool) Stats() (submitted, completed, rejected int64) {
	return atomic.LoadInt64(&wp.submitted),
		atomic.LoadInt64(&wp.completed),
		atomic.LoadInt64(&wp.rejected)
}

// Stop shuts down the worker pool gracefully
func (wp *WorkerPool) Stop() {
	if !atomic.CompareAndSwapInt32(&wp.stopped, 0, 1) {
		return // Already stopped
	}

	close(wp.stopChan)
	close(wp.workChan)
	wp.wg.Wait()

	submitted, completed, rejected := wp.Stats()
	wp.logger.WithFields(logrus.Fields{
		"submitted": submitted,
		"completed": completed,
		"rejected":  rejected,
	}).Info("Worker pool stopped")
}

// Resize adjusts the pool size (adds workers, cannot remove)
func (wp *WorkerPool) Resize(newSize int) {
	if newSize <= wp.size {
		return
	}

	additionalWorkers := newSize - wp.size
	for i := 0; i < additionalWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(wp.size + i)
	}
	wp.size = newSize

	wp.logger.WithField("new_size", newSize).Info("Worker pool resized")
}
