package realtime

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// BatchProcessor processes audio data in batches for optimal performance
type BatchProcessor struct {
	logger    *logrus.Entry
	batchSize int
	timeout   time.Duration

	// Processing queue
	queue      chan BatchItem
	workers    []*Worker
	workerPool *WorkerPool

	// Batch accumulation
	currentBatch []BatchItem
	batchMutex   sync.Mutex
	lastFlush    time.Time

	// Control
	ctx        context.Context
	cancel     context.CancelFunc
	started    bool
	startMutex sync.RWMutex

	// Statistics
	stats *BatchStats
}

// BatchItem represents an item to be processed in batch
type BatchItem struct {
	Data      []byte
	Processor func([]byte)
	Timestamp time.Time
	ID        string
}

// BatchStats tracks batch processing statistics
type BatchStats struct {
	mutex            sync.RWMutex
	TotalBatches     int64     `json:"total_batches"`
	TotalItems       int64     `json:"total_items"`
	AverageBatchSize float64   `json:"average_batch_size"`
	ProcessingTime   int64     `json:"processing_time_ms"`
	QueueSize        int       `json:"queue_size"`
	DroppedItems     int64     `json:"dropped_items"`
	LastReset        time.Time `json:"last_reset"`
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(batchSize int, timeout time.Duration, logger *logrus.Logger) *BatchProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	bp := &BatchProcessor{
		logger:       logger.WithField("component", "batch_processor"),
		batchSize:    batchSize,
		timeout:      timeout,
		queue:        make(chan BatchItem, batchSize*2), // 2x buffer
		currentBatch: make([]BatchItem, 0, batchSize),
		lastFlush:    time.Now(),
		ctx:          ctx,
		cancel:       cancel,
		stats:        &BatchStats{LastReset: time.Now()},
	}

	// Initialize worker pool
	bp.workerPool = NewWorkerPool(2, logger) // 2 workers for batch processing

	return bp
}

// Start starts the batch processor
func (bp *BatchProcessor) Start() error {
	bp.startMutex.Lock()
	defer bp.startMutex.Unlock()

	if bp.started {
		return nil
	}

	bp.started = true

	// Start batch accumulator
	go bp.batchAccumulator()

	// Start periodic flusher
	go bp.periodicFlusher()

	bp.logger.Debug("Batch processor started")
	return nil
}

// Stop stops the batch processor
func (bp *BatchProcessor) Stop() error {
	bp.startMutex.Lock()
	defer bp.startMutex.Unlock()

	if !bp.started {
		return nil
	}

	// Cancel context
	bp.cancel()

	// Flush remaining items
	bp.flushBatch()

	// Stop worker pool
	if bp.workerPool != nil {
		bp.workerPool.Stop()
	}

	bp.started = false
	bp.logger.Debug("Batch processor stopped")
	return nil
}

// Process adds an item to the batch for processing
func (bp *BatchProcessor) Process(data []byte, processor func([]byte)) {
	bp.startMutex.RLock()
	if !bp.started {
		bp.startMutex.RUnlock()
		// Auto-start if not started
		bp.Start()
		bp.startMutex.RLock()
	}
	bp.startMutex.RUnlock()

	item := BatchItem{
		Data:      data,
		Processor: processor,
		Timestamp: time.Now(),
		ID:        generateID(),
	}

	select {
	case bp.queue <- item:
		// Successfully queued
	case <-bp.ctx.Done():
		// Processor is shutting down
		return
	default:
		// Queue is full, drop item
		bp.stats.mutex.Lock()
		bp.stats.DroppedItems++
		bp.stats.mutex.Unlock()
		bp.logger.Warning("Batch queue full, dropping item")
	}
}

// batchAccumulator accumulates items into batches
func (bp *BatchProcessor) batchAccumulator() {
	defer func() {
		if r := recover(); r != nil {
			bp.logger.WithField("panic", r).Error("Panic in batch accumulator")
		}
	}()

	for {
		select {
		case <-bp.ctx.Done():
			return

		case item := <-bp.queue:
			bp.addToBatch(item)

			// Check if batch is full
			if bp.shouldFlushBatch() {
				bp.flushBatch()
			}
		}
	}
}

// periodicFlusher flushes batches periodically based on timeout
func (bp *BatchProcessor) periodicFlusher() {
	ticker := time.NewTicker(bp.timeout / 4) // Check 4 times per timeout period
	defer ticker.Stop()

	for {
		select {
		case <-bp.ctx.Done():
			return

		case <-ticker.C:
			if bp.shouldFlushByTimeout() {
				bp.flushBatch()
			}
		}
	}
}

// addToBatch adds an item to the current batch
func (bp *BatchProcessor) addToBatch(item BatchItem) {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	bp.currentBatch = append(bp.currentBatch, item)

	// Update queue size stat
	bp.stats.mutex.Lock()
	bp.stats.QueueSize = len(bp.queue)
	bp.stats.mutex.Unlock()
}

// shouldFlushBatch checks if the batch should be flushed due to size
func (bp *BatchProcessor) shouldFlushBatch() bool {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	return len(bp.currentBatch) >= bp.batchSize
}

// shouldFlushByTimeout checks if the batch should be flushed due to timeout
func (bp *BatchProcessor) shouldFlushByTimeout() bool {
	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	return len(bp.currentBatch) > 0 && time.Since(bp.lastFlush) >= bp.timeout
}

// flushBatch processes the current batch
func (bp *BatchProcessor) flushBatch() {
	bp.batchMutex.Lock()

	if len(bp.currentBatch) == 0 {
		bp.batchMutex.Unlock()
		return
	}

	// Take ownership of current batch
	batch := bp.currentBatch
	bp.currentBatch = make([]BatchItem, 0, bp.batchSize)
	bp.lastFlush = time.Now()

	bp.batchMutex.Unlock()

	// Process batch asynchronously
	bp.workerPool.Submit(func() {
		bp.processBatch(batch)
	})
}

// processBatch processes a batch of items
func (bp *BatchProcessor) processBatch(batch []BatchItem) {
	startTime := time.Now()
	defer func() {
		processingTime := time.Since(startTime)

		bp.stats.mutex.Lock()
		bp.stats.TotalBatches++
		bp.stats.TotalItems += int64(len(batch))
		bp.stats.ProcessingTime += processingTime.Nanoseconds() / 1e6

		if bp.stats.TotalBatches > 0 {
			bp.stats.AverageBatchSize = float64(bp.stats.TotalItems) / float64(bp.stats.TotalBatches)
		}
		bp.stats.mutex.Unlock()

		if r := recover(); r != nil {
			bp.logger.WithField("panic", r).Error("Panic in batch processing")
		}
	}()

	bp.logger.WithFields(logrus.Fields{
		"batch_size":      len(batch),
		"oldest_item_age": time.Since(batch[0].Timestamp),
	}).Debug("Processing batch")

	// Process each item in the batch
	for _, item := range batch {
		if item.Processor != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						bp.logger.WithFields(logrus.Fields{
							"item_id": item.ID,
							"panic":   r,
						}).Error("Panic processing batch item")
					}
				}()

				item.Processor(item.Data)
			}()
		}
	}
}

// GetStats returns batch processing statistics
func (bp *BatchProcessor) GetStats() *BatchStats {
	bp.stats.mutex.RLock()
	defer bp.stats.mutex.RUnlock()

	statsCopy := BatchStats{}
	statsCopy.TotalBatches = bp.stats.TotalBatches
	statsCopy.TotalItems = bp.stats.TotalItems
	statsCopy.AverageBatchSize = bp.stats.AverageBatchSize
	statsCopy.ProcessingTime = bp.stats.ProcessingTime
	statsCopy.QueueSize = bp.stats.QueueSize
	statsCopy.DroppedItems = bp.stats.DroppedItems
	statsCopy.LastReset = bp.stats.LastReset
	return &statsCopy
}

// IsStarted returns whether the processor is started
func (bp *BatchProcessor) IsStarted() bool {
	bp.startMutex.RLock()
	defer bp.startMutex.RUnlock()
	return bp.started
}

// QueueSize returns the current queue size
func (bp *BatchProcessor) QueueSize() int {
	return len(bp.queue)
}

// SetBatchSize updates the batch size
func (bp *BatchProcessor) SetBatchSize(size int) {
	if size <= 0 {
		return
	}

	bp.batchMutex.Lock()
	defer bp.batchMutex.Unlock()

	bp.batchSize = size
}

// SetTimeout updates the batch timeout
func (bp *BatchProcessor) SetTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}

	bp.timeout = timeout
}

// generateID generates a simple ID for batch items
func generateID() string {
	return time.Now().Format("20060102150405.000000")
}
