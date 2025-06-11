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
	maxMemoryUsage  int64
	gcThreshold     int64
	checkInterval   time.Duration
	
	// Resource tracking
	lastMemStats    runtime.MemStats
	lastCheck       time.Time
	lastGC          time.Time
	gcCount         int64
	
	// Thresholds
	memoryWarningThreshold  float64
	memoryCriticalThreshold float64
	
	// Control
	running     bool
	runningMux  sync.RWMutex
	stopChan    chan struct{}
	
	// Statistics
	stats       *ResourceStats
}

// ResourceStats tracks resource monitoring statistics
type ResourceStats struct {
	mutex               sync.RWMutex
	CurrentMemoryUsage  int64     `json:"current_memory_usage_bytes"`
	MaxMemoryUsage      int64     `json:"max_memory_usage_bytes"`
	MemoryLimit         int64     `json:"memory_limit_bytes"`
	GCCount             int64     `json:"gc_count"`
	LastGCTime          time.Time `json:"last_gc_time"`
	HeapObjects         uint64    `json:"heap_objects"`
	NumGoroutines       int       `json:"num_goroutines"`
	CPUPercent          float64   `json:"cpu_percent"`
	MemoryPercent       float64   `json:"memory_percent"`
	LastCheck           time.Time `json:"last_check"`
	WarningCount        int64     `json:"warning_count"`
	CriticalCount       int64     `json:"critical_count"`
	OptimizationCount   int64     `json:"optimization_count"`
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(maxMemoryUsage int64, logger *logrus.Logger) *ResourceMonitor {
	rm := &ResourceMonitor{
		logger:                  logger.WithField("component", "resource_monitor"),
		maxMemoryUsage:         maxMemoryUsage,
		gcThreshold:            maxMemoryUsage / 4, // GC when 25% of limit is used
		checkInterval:          10 * time.Second,
		memoryWarningThreshold: 0.7,  // 70% of limit
		memoryCriticalThreshold: 0.9, // 90% of limit
		lastCheck:              time.Now(),
		lastGC:                 time.Now(),
		stopChan:               make(chan struct{}),
		stats:                  &ResourceStats{
			MemoryLimit: maxMemoryUsage,
			LastCheck:   time.Now(),
		},
	}
	
	// Start monitoring
	go rm.monitorLoop()
	
	return rm
}

// Start starts the resource monitor
func (rm *ResourceMonitor) Start() {
	rm.runningMux.Lock()
	defer rm.runningMux.Unlock()
	
	if !rm.running {
		rm.running = true
		rm.logger.Debug("Resource monitor started")
	}
}

// Stop stops the resource monitor
func (rm *ResourceMonitor) Stop() {
	rm.runningMux.Lock()
	defer rm.runningMux.Unlock()
	
	if rm.running {
		rm.running = false
		close(rm.stopChan)
		rm.logger.Debug("Resource monitor stopped")
	}
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
		"memory_freed_mb":    float64(memoryFreed) / 1024 / 1024,
		"optimization_time":  optimizationTime,
		"memory_before_mb":   float64(initialMemory) / 1024 / 1024,
		"memory_after_mb":    float64(finalMemory) / 1024 / 1024,
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
	
	rm.lastMemStats = memStats
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
			"memory_usage_mb":  float64(currentUsage) / 1024 / 1024,
			"memory_percent":   memoryPercent,
			"memory_limit_mb":  float64(rm.maxMemoryUsage) / 1024 / 1024,
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

// GetStats returns current resource statistics
func (rm *ResourceMonitor) GetStats() *ResourceStats {
	rm.stats.mutex.RLock()
	defer rm.stats.mutex.RUnlock()
	
	statsCopy := *rm.stats
	return &statsCopy
}

// GetMemoryUsage returns current memory usage information
func (rm *ResourceMonitor) GetMemoryUsage() map[string]interface{} {
	rm.stats.mutex.RLock()
	defer rm.stats.mutex.RUnlock()
	
	return map[string]interface{}{
		"current_usage_bytes": rm.stats.CurrentMemoryUsage,
		"current_usage_mb":    float64(rm.stats.CurrentMemoryUsage) / 1024 / 1024,
		"max_usage_bytes":     rm.stats.MaxMemoryUsage,
		"max_usage_mb":        float64(rm.stats.MaxMemoryUsage) / 1024 / 1024,
		"limit_bytes":         rm.stats.MemoryLimit,
		"limit_mb":            float64(rm.stats.MemoryLimit) / 1024 / 1024,
		"usage_percent":       rm.stats.MemoryPercent,
		"heap_objects":        rm.stats.HeapObjects,
		"num_goroutines":      rm.stats.NumGoroutines,
		"gc_count":            rm.stats.GCCount,
		"last_gc":             rm.stats.LastGCTime,
	}
}

// IsMemoryThresholdExceeded returns true if memory usage exceeds warning threshold
func (rm *ResourceMonitor) IsMemoryThresholdExceeded() bool {
	rm.stats.mutex.RLock()
	defer rm.stats.mutex.RUnlock()
	
	return rm.stats.MemoryPercent >= rm.memoryWarningThreshold*100
}

// IsCriticalMemoryUsage returns true if memory usage exceeds critical threshold
func (rm *ResourceMonitor) IsCriticalMemoryUsage() bool {
	rm.stats.mutex.RLock()
	defer rm.stats.mutex.RUnlock()
	
	return rm.stats.MemoryPercent >= rm.memoryCriticalThreshold*100
}

// SetMemoryLimit updates the memory limit
func (rm *ResourceMonitor) SetMemoryLimit(limit int64) {
	if limit <= 0 {
		return
	}
	
	rm.maxMemoryUsage = limit
	rm.gcThreshold = limit / 4
	
	rm.stats.mutex.Lock()
	rm.stats.MemoryLimit = limit
	// Recalculate percentage with new limit
	if limit > 0 {
		rm.stats.MemoryPercent = float64(rm.stats.CurrentMemoryUsage) / float64(limit) * 100
	}
	rm.stats.mutex.Unlock()
}

// SetCheckInterval updates the monitoring interval
func (rm *ResourceMonitor) SetCheckInterval(interval time.Duration) {
	if interval <= 0 {
		return
	}
	rm.checkInterval = interval
}

// SetThresholds updates the warning and critical thresholds
func (rm *ResourceMonitor) SetThresholds(warning, critical float64) {
	if warning <= 0 || warning >= 1 || critical <= 0 || critical >= 1 || warning >= critical {
		return
	}
	
	rm.memoryWarningThreshold = warning
	rm.memoryCriticalThreshold = critical
}

// ForceGC forces a garbage collection cycle
func (rm *ResourceMonitor) ForceGC() {
	go rm.OptimizeMemory()
}

// Reset resets the resource monitor statistics
func (rm *ResourceMonitor) Reset() {
	rm.stats.mutex.Lock()
	defer rm.stats.mutex.Unlock()
	
	rm.stats.MaxMemoryUsage = 0
	rm.stats.GCCount = 0
	rm.stats.WarningCount = 0
	rm.stats.CriticalCount = 0
	rm.stats.OptimizationCount = 0
	rm.gcCount = 0
	
	rm.logger.Debug("Resource monitor statistics reset")
}

// GetDetailedMemoryInfo returns detailed memory information
func (rm *ResourceMonitor) GetDetailedMemoryInfo() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	
	return map[string]interface{}{
		"heap_alloc_bytes":       memStats.HeapAlloc,
		"heap_alloc_mb":          float64(memStats.HeapAlloc) / 1024 / 1024,
		"heap_sys_bytes":         memStats.HeapSys,
		"heap_sys_mb":            float64(memStats.HeapSys) / 1024 / 1024,
		"heap_idle_bytes":        memStats.HeapIdle,
		"heap_idle_mb":           float64(memStats.HeapIdle) / 1024 / 1024,
		"heap_inuse_bytes":       memStats.HeapInuse,
		"heap_inuse_mb":          float64(memStats.HeapInuse) / 1024 / 1024,
		"heap_released_bytes":    memStats.HeapReleased,
		"heap_released_mb":       float64(memStats.HeapReleased) / 1024 / 1024,
		"heap_objects":           memStats.HeapObjects,
		"stack_inuse_bytes":      memStats.StackInuse,
		"stack_inuse_mb":         float64(memStats.StackInuse) / 1024 / 1024,
		"stack_sys_bytes":        memStats.StackSys,
		"stack_sys_mb":           float64(memStats.StackSys) / 1024 / 1024,
		"m_span_inuse_bytes":     memStats.MSpanInuse,
		"m_span_inuse_mb":        float64(memStats.MSpanInuse) / 1024 / 1024,
		"m_cache_inuse_bytes":    memStats.MCacheInuse,
		"m_cache_inuse_mb":       float64(memStats.MCacheInuse) / 1024 / 1024,
		"buck_hash_sys_bytes":    memStats.BuckHashSys,
		"buck_hash_sys_mb":       float64(memStats.BuckHashSys) / 1024 / 1024,
		"gc_sys_bytes":           memStats.GCSys,
		"gc_sys_mb":              float64(memStats.GCSys) / 1024 / 1024,
		"other_sys_bytes":        memStats.OtherSys,
		"other_sys_mb":           float64(memStats.OtherSys) / 1024 / 1024,
		"sys_bytes":              memStats.Sys,
		"sys_mb":                 float64(memStats.Sys) / 1024 / 1024,
		"num_gc":                 memStats.NumGC,
		"num_forced_gc":          memStats.NumForcedGC,
		"gc_cpu_fraction":        memStats.GCCPUFraction,
		"last_gc_time":           time.Unix(0, int64(memStats.LastGC)),
		"num_goroutines":         runtime.NumGoroutine(),
		"num_cpu":                runtime.NumCPU(),
	}
}