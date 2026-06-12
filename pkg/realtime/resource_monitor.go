package realtime

import (
	"runtime"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ResourceMonitor monitors system resources and optimizes memory usage
type ResourceMonitor struct {
	logger *logrus.Entry

	// Configuration
	maxMemoryUsage int64
	gcThreshold    int64
	checkInterval  time.Duration

	// Resource tracking
	lastCheck time.Time
	lastGC    time.Time
	gcCount   int64

	// Thresholds
	memoryWarningThreshold  float64
	memoryCriticalThreshold float64

	// Control
	running    bool
	runningMux sync.RWMutex
	stopChan   chan struct{}

	// Statistics
	stats *ResourceStats
}

// ResourceStats tracks resource monitoring statistics
type ResourceStats struct {
	mutex              sync.RWMutex
	CurrentMemoryUsage int64     `json:"current_memory_usage_bytes"`
	MaxMemoryUsage     int64     `json:"max_memory_usage_bytes"`
	MemoryLimit        int64     `json:"memory_limit_bytes"`
	GCCount            int64     `json:"gc_count"`
	LastGCTime         time.Time `json:"last_gc_time"`
	HeapObjects        uint64    `json:"heap_objects"`
	NumGoroutines      int       `json:"num_goroutines"`
	CPUPercent         float64   `json:"cpu_percent"`
	MemoryPercent      float64   `json:"memory_percent"`
	LastCheck          time.Time `json:"last_check"`
	WarningCount       int64     `json:"warning_count"`
	CriticalCount      int64     `json:"critical_count"`
	OptimizationCount  int64     `json:"optimization_count"`
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(maxMemoryUsage int64, logger *logrus.Logger) *ResourceMonitor {
	rm := &ResourceMonitor{
		logger:                  logger.WithField("component", "resource_monitor"),
		maxMemoryUsage:          maxMemoryUsage,
		gcThreshold:             maxMemoryUsage / 4, // GC when 25% of limit is used
		checkInterval:           10 * time.Second,
		memoryWarningThreshold:  0.7, // 70% of limit
		memoryCriticalThreshold: 0.9, // 90% of limit
		lastCheck:               time.Now(),
		lastGC:                  time.Now(),
		stopChan:                make(chan struct{}),
		stats: &ResourceStats{
			MemoryLimit: maxMemoryUsage,
			LastCheck:   time.Now(),
		},
	}

	// Start monitoring
	go rm.monitorLoop()

	return rm
}

// CheckResources performs an immediate resource check
func (rm *ResourceMonitor) CheckResources() {
	rm.updateMemoryStats()
	rm.checkMemoryThresholds()
	rm.updateSystemStats()
}

// OptimizeMemory performs memory optimization
func (rm *ResourceMonitor) OptimizeMemory() {
	startTime := time.Now()

	rm.stats.mutex.Lock()
	initialMemory := rm.stats.CurrentMemoryUsage
	rm.stats.mutex.Unlock()

	// Force garbage collection
	runtime.GC()

	// Update stats after GC
	rm.updateMemoryStats()

	rm.stats.mutex.Lock()
	finalMemory := rm.stats.CurrentMemoryUsage
	rm.stats.OptimizationCount++
	rm.lastGC = time.Now()
	rm.gcCount++
	rm.stats.GCCount = rm.gcCount
	rm.stats.LastGCTime = rm.lastGC
	rm.stats.mutex.Unlock()

	memoryFreed := initialMemory - finalMemory
	optimizationTime := time.Since(startTime)

	rm.logger.WithFields(logrus.Fields{
		"memory_freed_mb":   float64(memoryFreed) / 1024 / 1024,
		"optimization_time": optimizationTime,
		"memory_before_mb":  float64(initialMemory) / 1024 / 1024,
		"memory_after_mb":   float64(finalMemory) / 1024 / 1024,
	}).Debug("Memory optimization completed")
}

// monitorLoop runs the main monitoring loop
func (rm *ResourceMonitor) monitorLoop() {
	ticker := time.NewTicker(rm.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopChan:
			return
		case <-ticker.C:
			rm.runningMux.RLock()
			if rm.running {
				rm.CheckResources()
			}
			rm.runningMux.RUnlock()
		}
	}
}

// updateMemoryStats updates memory statistics
func (rm *ResourceMonitor) updateMemoryStats() {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	rm.stats.mutex.Lock()
	defer rm.stats.mutex.Unlock()

	rm.lastCheck = time.Now()

	rm.stats.CurrentMemoryUsage = int64(memStats.HeapInuse)
	rm.stats.HeapObjects = memStats.HeapObjects
	rm.stats.LastCheck = rm.lastCheck

	// Update max memory usage
	if rm.stats.CurrentMemoryUsage > rm.stats.MaxMemoryUsage {
		rm.stats.MaxMemoryUsage = rm.stats.CurrentMemoryUsage
	}

	// Calculate memory percentage
	if rm.maxMemoryUsage > 0 {
		rm.stats.MemoryPercent = float64(rm.stats.CurrentMemoryUsage) / float64(rm.maxMemoryUsage) * 100
	}
}

// updateSystemStats updates system-level statistics
func (rm *ResourceMonitor) updateSystemStats() {
	rm.stats.mutex.Lock()
	defer rm.stats.mutex.Unlock()

	// Update goroutine count
	rm.stats.NumGoroutines = runtime.NumGoroutine()
}

// checkMemoryThresholds checks if memory usage exceeds thresholds
func (rm *ResourceMonitor) checkMemoryThresholds() {
	rm.stats.mutex.RLock()
	currentUsage := rm.stats.CurrentMemoryUsage
	memoryPercent := rm.stats.MemoryPercent
	rm.stats.mutex.RUnlock()

	if memoryPercent >= rm.memoryCriticalThreshold*100 {
		// Critical threshold exceeded
		rm.stats.mutex.Lock()
		rm.stats.CriticalCount++
		rm.stats.mutex.Unlock()

		rm.logger.WithFields(logrus.Fields{
			"memory_usage_mb": float64(currentUsage) / 1024 / 1024,
			"memory_percent":  memoryPercent,
			"memory_limit_mb": float64(rm.maxMemoryUsage) / 1024 / 1024,
		}).Warning("Critical memory usage detected")

		// Force immediate optimization
		go rm.OptimizeMemory()

	} else if memoryPercent >= rm.memoryWarningThreshold*100 {
		// Warning threshold exceeded
		rm.stats.mutex.Lock()
		rm.stats.WarningCount++
		rm.stats.mutex.Unlock()

		rm.logger.WithFields(logrus.Fields{
			"memory_usage_mb": float64(currentUsage) / 1024 / 1024,
			"memory_percent":  memoryPercent,
		}).Debug("High memory usage detected")

		// Schedule optimization if it's been a while since last GC
		if time.Since(rm.lastGC) > 30*time.Second {
			go rm.OptimizeMemory()
		}
	}

	// Check for automatic GC trigger
	if currentUsage > rm.gcThreshold && time.Since(rm.lastGC) > 15*time.Second {
		go rm.OptimizeMemory()
	}
}
