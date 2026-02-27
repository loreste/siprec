package performance

import (
	"context"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PerformanceMonitor tracks system performance metrics
type PerformanceMonitor struct {
	logger              *logrus.Logger
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	mutex               sync.RWMutex
	
	// Configuration
	monitorInterval     time.Duration
	gcThresholdMB       int64
	memoryLimitMB       int64
	cpuLimit            float64
	
	// Current metrics
	lastGCTime       time.Time
	lastMemCheckTime time.Time
	
	// Statistics
	gcCount             int
	totalMemoryFreed    int64
	avgGCTime           time.Duration
}

// Config holds performance monitor configuration
type Config struct {
	MonitorInterval   time.Duration `yaml:"monitor_interval" default:"30s"`
	GCThresholdMB     int64         `yaml:"gc_threshold_mb" default:"100"`    // Trigger GC if heap grows by this amount
	MemoryLimitMB     int64         `yaml:"memory_limit_mb" default:"512"`    // Log warnings above this limit
	CPULimit          float64       `yaml:"cpu_limit" default:"80.0"`         // CPU usage percentage limit
}

// DefaultConfig returns default performance monitor configuration
func DefaultConfig() *Config {
	return &Config{
		MonitorInterval: 30 * time.Second,
		GCThresholdMB:   100,
		MemoryLimitMB:   512,
		CPULimit:        80.0,
	}
}

// NewPerformanceMonitor creates a new performance monitor
func NewPerformanceMonitor(logger *logrus.Logger, config *Config) *PerformanceMonitor {
	if config == nil {
		config = DefaultConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &PerformanceMonitor{
		logger:          logger,
		ctx:             ctx,
		cancel:          cancel,
		monitorInterval: config.MonitorInterval,
		gcThresholdMB:   config.GCThresholdMB,
		memoryLimitMB:   config.MemoryLimitMB,
		cpuLimit:        config.CPULimit,
		lastGCTime:      time.Now(),
		lastMemCheckTime: time.Now(),
	}
}

// Start begins performance monitoring
func (pm *PerformanceMonitor) Start() {
	pm.wg.Add(1)
	go pm.runMonitor()
	pm.logger.Info("Performance monitor started")
}

// Stop stops performance monitoring
func (pm *PerformanceMonitor) Stop() {
	pm.cancel()
	pm.wg.Wait()
	pm.logger.Info("Performance monitor stopped")
}

// runMonitor runs the main monitoring loop
func (pm *PerformanceMonitor) runMonitor() {
	defer pm.wg.Done()
	
	ticker := time.NewTicker(pm.monitorInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			pm.performanceCheck()
		}
	}
}

// performanceCheck performs a comprehensive performance check
func (pm *PerformanceMonitor) performanceCheck() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	
	// Check memory usage
	heapInUseMB := float64(mem.HeapInuse) / 1024 / 1024
	heapAllocMB := float64(mem.HeapAlloc) / 1024 / 1024
	
	// Check if we should trigger GC
	if pm.shouldTriggerGC(&mem) {
		pm.triggerOptimizedGC(&mem)
	}
	
	// Log performance metrics at debug level
	pm.logger.WithFields(logrus.Fields{
		"heap_inuse_mb":    heapInUseMB,
		"heap_alloc_mb":    heapAllocMB,
		"goroutines":       runtime.NumGoroutine(),
		"gc_cycles":        mem.NumGC,
		"gc_pause_total":   mem.PauseTotalNs / 1000000, // Convert to milliseconds
		"mallocs":          mem.Mallocs,
		"frees":            mem.Frees,
	}).Debug("Performance metrics")
	
	// Check for memory warnings
	if heapInUseMB > float64(pm.memoryLimitMB) {
		pm.logger.WithFields(logrus.Fields{
			"current_mb": heapInUseMB,
			"limit_mb":   pm.memoryLimitMB,
		}).Warning("Memory usage above configured limit")
	}
	
	// Check for goroutine leaks (simplified check)
	goroutineCount := runtime.NumGoroutine()
	if goroutineCount > 1000 { // Arbitrary threshold
		pm.logger.WithField("goroutine_count", goroutineCount).Warning("High goroutine count detected - possible leak")
	}
}

// shouldTriggerGC determines if garbage collection should be triggered
func (pm *PerformanceMonitor) shouldTriggerGC(mem *runtime.MemStats) bool {
	// Only trigger if it's been a while since last GC and memory usage is high
	timeSinceLastGC := time.Since(pm.lastGCTime)
	heapGrowthMB := float64(mem.HeapInuse) / 1024 / 1024
	
	return timeSinceLastGC > 2*time.Minute && heapGrowthMB > float64(pm.gcThresholdMB)
}

// triggerOptimizedGC performs optimized garbage collection
func (pm *PerformanceMonitor) triggerOptimizedGC(memBefore *runtime.MemStats) {
	gcStart := time.Now()
	
	// Force garbage collection
	runtime.GC()
	
	// Return memory to OS
	debug.FreeOSMemory()
	
	gcDuration := time.Since(gcStart)
	
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	
	// Update statistics
	pm.gcCount++
	freedMB := float64(memBefore.HeapInuse-memAfter.HeapInuse) / 1024 / 1024
	pm.totalMemoryFreed += int64(freedMB)
	
	// Update average GC time
	if pm.avgGCTime == 0 {
		pm.avgGCTime = gcDuration
	} else {
		pm.avgGCTime = (pm.avgGCTime + gcDuration) / 2
	}
	
	pm.lastGCTime = time.Now()
	
	if freedMB > 1 { // Only log if significant memory was freed
		pm.logger.WithFields(logrus.Fields{
			"memory_freed_mb": freedMB,
			"gc_duration_ms":  gcDuration.Milliseconds(),
			"heap_after_mb":   float64(memAfter.HeapInuse) / 1024 / 1024,
			"gc_count":        pm.gcCount,
		}).Info("Optimized garbage collection completed")
	}
}

// GetStats returns current performance statistics
func (pm *PerformanceMonitor) GetStats() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	
	return map[string]interface{}{
		"heap_inuse_mb":       float64(mem.HeapInuse) / 1024 / 1024,
		"heap_alloc_mb":       float64(mem.HeapAlloc) / 1024 / 1024,
		"goroutines":          runtime.NumGoroutine(),
		"gc_cycles":           mem.NumGC,
		"gc_pause_total_ms":   mem.PauseTotalNs / 1000000,
		"manual_gc_count":     pm.gcCount,
		"total_memory_freed":  pm.totalMemoryFreed,
		"avg_gc_time_ms":      pm.avgGCTime.Milliseconds(),
		"time_since_last_gc":  time.Since(pm.lastGCTime).Seconds(),
	}
}

// EnableMemoryProfiling sets up memory profiling for development
func (pm *PerformanceMonitor) EnableMemoryProfiling() {
	// Set GC target percentage (lower = more frequent GC, less memory usage)
	debug.SetGCPercent(50) // Default is 100
	
	// Set memory limit (requires Go 1.19+)
	// This is commented out as it might not be available in all Go versions
	// debug.SetMemoryLimit(pm.memoryLimitMB * 1024 * 1024)
	
	pm.logger.Info("Memory profiling enabled with optimized GC settings")
}