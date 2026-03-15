package resources

import (
	"context"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// MemoryMonitor tracks memory usage and enforces limits
type MemoryMonitor struct {
	limitBytes   int64
	interval     time.Duration
	logger       *logrus.Entry

	// Current state
	currentBytes int64
	isOverLimit  int32

	// Callbacks
	callback func(used, limit int64)

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewMemoryMonitor creates a new memory monitor
func NewMemoryMonitor(limitMB int, interval time.Duration, logger *logrus.Logger) *MemoryMonitor {
	if interval == 0 {
		interval = 10 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &MemoryMonitor{
		limitBytes: int64(limitMB) * 1024 * 1024,
		interval:   interval,
		logger:     logger.WithField("component", "memory_monitor"),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start begins memory monitoring
func (mm *MemoryMonitor) Start() {
	mm.wg.Add(1)
	go mm.monitorLoop()

	mm.logger.WithFields(logrus.Fields{
		"limit_mb": mm.limitBytes / (1024 * 1024),
		"interval": mm.interval,
	}).Info("Memory monitor started")
}

func (mm *MemoryMonitor) monitorLoop() {
	defer mm.wg.Done()

	ticker := time.NewTicker(mm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-mm.ctx.Done():
			return
		case <-ticker.C:
			mm.check()
		}
	}
}

func (mm *MemoryMonitor) check() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Use Alloc for currently allocated memory
	current := int64(memStats.Alloc)
	atomic.StoreInt64(&mm.currentBytes, current)

	wasOverLimit := atomic.LoadInt32(&mm.isOverLimit) == 1
	isOverLimit := current > mm.limitBytes

	// State changed
	if isOverLimit != wasOverLimit {
		if isOverLimit {
			atomic.StoreInt32(&mm.isOverLimit, 1)
			mm.handleOverLimit(current, memStats)
		} else {
			atomic.StoreInt32(&mm.isOverLimit, 0)
			mm.logger.WithFields(logrus.Fields{
				"current_mb": current / (1024 * 1024),
				"limit_mb":   mm.limitBytes / (1024 * 1024),
			}).Info("Memory usage back within limits")
		}
	}

	// Check warning threshold (80%)
	warningThreshold := mm.limitBytes * 80 / 100
	if current > warningThreshold && !isOverLimit {
		mm.logger.WithFields(logrus.Fields{
			"current_mb":     current / (1024 * 1024),
			"limit_mb":       mm.limitBytes / (1024 * 1024),
			"usage_percent":  float64(current) / float64(mm.limitBytes) * 100,
			"heap_objects":   memStats.HeapObjects,
			"gc_cycles":      memStats.NumGC,
		}).Warn("Memory usage approaching limit")
	}

	// Invoke callback
	if mm.callback != nil {
		mm.callback(current, mm.limitBytes)
	}
}

func (mm *MemoryMonitor) handleOverLimit(current int64, memStats runtime.MemStats) {
	mm.logger.WithFields(logrus.Fields{
		"current_mb":    current / (1024 * 1024),
		"limit_mb":      mm.limitBytes / (1024 * 1024),
		"heap_alloc":    memStats.HeapAlloc / (1024 * 1024),
		"heap_sys":      memStats.HeapSys / (1024 * 1024),
		"heap_objects":  memStats.HeapObjects,
		"stack_sys":     memStats.StackSys / (1024 * 1024),
		"gc_cycles":     memStats.NumGC,
	}).Error("Memory limit exceeded")

	// Force garbage collection
	runtime.GC()
	debug.FreeOSMemory()

	// Re-check after GC
	runtime.ReadMemStats(&memStats)
	afterGC := int64(memStats.Alloc)
	atomic.StoreInt64(&mm.currentBytes, afterGC)

	if afterGC <= mm.limitBytes {
		atomic.StoreInt32(&mm.isOverLimit, 0)
		mm.logger.WithFields(logrus.Fields{
			"before_gc_mb": current / (1024 * 1024),
			"after_gc_mb":  afterGC / (1024 * 1024),
			"freed_mb":     (current - afterGC) / (1024 * 1024),
		}).Info("Memory reclaimed by GC")
	} else {
		mm.logger.WithFields(logrus.Fields{
			"current_mb": afterGC / (1024 * 1024),
			"limit_mb":   mm.limitBytes / (1024 * 1024),
		}).Error("Memory still over limit after GC")
	}
}

// CheckWithinLimit returns true if memory is within limits
func (mm *MemoryMonitor) CheckWithinLimit() bool {
	return atomic.LoadInt32(&mm.isOverLimit) == 0
}

// CurrentUsage returns current memory usage in bytes
func (mm *MemoryMonitor) CurrentUsage() int64 {
	return atomic.LoadInt64(&mm.currentBytes)
}

// CurrentUsageMB returns current memory usage in megabytes
func (mm *MemoryMonitor) CurrentUsageMB() int64 {
	return atomic.LoadInt64(&mm.currentBytes) / (1024 * 1024)
}

// LimitMB returns the memory limit in megabytes
func (mm *MemoryMonitor) LimitMB() int64 {
	return mm.limitBytes / (1024 * 1024)
}

// UsagePercent returns current usage as a percentage
func (mm *MemoryMonitor) UsagePercent() float64 {
	current := atomic.LoadInt64(&mm.currentBytes)
	return float64(current) / float64(mm.limitBytes) * 100
}

// SetCallback sets the callback function for memory updates
func (mm *MemoryMonitor) SetCallback(cb func(used, limit int64)) {
	mm.callback = cb
}

// ForceGC triggers garbage collection
func (mm *MemoryMonitor) ForceGC() {
	runtime.GC()
	debug.FreeOSMemory()
	mm.check()
}

// Stop stops the memory monitor
func (mm *MemoryMonitor) Stop() {
	mm.cancel()
	mm.wg.Wait()
	mm.logger.Info("Memory monitor stopped")
}

// GetDetailedStats returns detailed memory statistics
func (mm *MemoryMonitor) GetDetailedStats() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return map[string]interface{}{
		"alloc_mb":         memStats.Alloc / (1024 * 1024),
		"total_alloc_mb":   memStats.TotalAlloc / (1024 * 1024),
		"sys_mb":           memStats.Sys / (1024 * 1024),
		"heap_alloc_mb":    memStats.HeapAlloc / (1024 * 1024),
		"heap_sys_mb":      memStats.HeapSys / (1024 * 1024),
		"heap_idle_mb":     memStats.HeapIdle / (1024 * 1024),
		"heap_inuse_mb":    memStats.HeapInuse / (1024 * 1024),
		"heap_released_mb": memStats.HeapReleased / (1024 * 1024),
		"heap_objects":     memStats.HeapObjects,
		"stack_inuse_mb":   memStats.StackInuse / (1024 * 1024),
		"stack_sys_mb":     memStats.StackSys / (1024 * 1024),
		"gc_cycles":        memStats.NumGC,
		"gc_pause_total_ms": memStats.PauseTotalNs / 1000000,
		"limit_mb":         mm.limitBytes / (1024 * 1024),
		"over_limit":       atomic.LoadInt32(&mm.isOverLimit) == 1,
	}
}
