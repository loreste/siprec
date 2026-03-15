package resources

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager handles resource limits and monitoring for the SIPREC server
type Manager struct {
	config  Config
	logger  *logrus.Entry

	// Resource tracking
	activeCalls    int64
	activeStreams  int64
	memoryUsedMB   int64

	// Components
	workerPool    *WorkerPool
	memoryMonitor *MemoryMonitor
	rtpLimiter    *RTPLimiter

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Callbacks
	onResourceExhausted func(resourceType string)
	onResourceRecovered func(resourceType string)
}

// Config holds resource management configuration
type Config struct {
	MaxConcurrentCalls int
	MaxRTPStreams      int
	WorkerPoolSize     int
	MaxMemoryMB        int
	HorizontalScaling  bool
	NodeID             string
	MonitorInterval    time.Duration
}

// Stats represents current resource statistics
type Stats struct {
	ActiveCalls      int64   `json:"active_calls"`
	ActiveStreams    int64   `json:"active_streams"`
	MemoryUsedMB     int64   `json:"memory_used_mb"`
	MemoryLimitMB    int     `json:"memory_limit_mb"`
	WorkerPoolActive int     `json:"worker_pool_active"`
	WorkerPoolSize   int     `json:"worker_pool_size"`
	CallCapacity     float64 `json:"call_capacity"`     // 0.0-1.0
	StreamCapacity   float64 `json:"stream_capacity"`   // 0.0-1.0
	MemoryCapacity   float64 `json:"memory_capacity"`   // 0.0-1.0
	NodeID           string  `json:"node_id,omitempty"`
}

// NewManager creates a new resource manager
func NewManager(cfg Config, logger *logrus.Logger) (*Manager, error) {
	if cfg.MonitorInterval == 0 {
		cfg.MonitorInterval = 10 * time.Second
	}
	if cfg.MaxConcurrentCalls == 0 {
		cfg.MaxConcurrentCalls = 500
	}
	if cfg.MaxRTPStreams == 0 {
		cfg.MaxRTPStreams = 1500
	}
	if cfg.WorkerPoolSize == 0 {
		cfg.WorkerPoolSize = runtime.NumCPU() * 4
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config: cfg,
		logger: logger.WithField("component", "resource_manager"),
		ctx:    ctx,
		cancel: cancel,
	}

	// Initialize worker pool
	m.workerPool = NewWorkerPool(cfg.WorkerPoolSize, logger)

	// Initialize memory monitor if limit is set
	if cfg.MaxMemoryMB > 0 {
		m.memoryMonitor = NewMemoryMonitor(cfg.MaxMemoryMB, cfg.MonitorInterval, logger)
		m.memoryMonitor.SetCallback(func(used, limit int64) {
			atomic.StoreInt64(&m.memoryUsedMB, used/(1024*1024))
			if m.onResourceExhausted != nil && used > limit*90/100 {
				m.onResourceExhausted("memory")
			}
		})
	}

	// Initialize RTP limiter
	m.rtpLimiter = NewRTPLimiter(cfg.MaxRTPStreams, logger)

	m.logger.WithFields(logrus.Fields{
		"max_calls":        cfg.MaxConcurrentCalls,
		"max_rtp_streams":  cfg.MaxRTPStreams,
		"worker_pool_size": cfg.WorkerPoolSize,
		"max_memory_mb":    cfg.MaxMemoryMB,
		"node_id":          cfg.NodeID,
	}).Info("Resource manager initialized")

	return m, nil
}

// Start begins resource monitoring
func (m *Manager) Start() {
	m.workerPool.Start()

	if m.memoryMonitor != nil {
		m.memoryMonitor.Start()
	}

	m.wg.Add(1)
	go m.monitorLoop()

	m.logger.Info("Resource manager started")
}

func (m *Manager) monitorLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.logStats()
		}
	}
}

func (m *Manager) logStats() {
	stats := m.GetStats()

	m.logger.WithFields(logrus.Fields{
		"active_calls":    stats.ActiveCalls,
		"active_streams":  stats.ActiveStreams,
		"memory_used_mb":  stats.MemoryUsedMB,
		"call_capacity":   fmt.Sprintf("%.1f%%", stats.CallCapacity*100),
		"stream_capacity": fmt.Sprintf("%.1f%%", stats.StreamCapacity*100),
		"memory_capacity": fmt.Sprintf("%.1f%%", stats.MemoryCapacity*100),
	}).Debug("Resource stats")
}

// AcquireCall attempts to acquire a call slot
func (m *Manager) AcquireCall() bool {
	current := atomic.LoadInt64(&m.activeCalls)
	if int(current) >= m.config.MaxConcurrentCalls {
		m.logger.Warn("Call limit reached")
		if m.onResourceExhausted != nil {
			m.onResourceExhausted("calls")
		}
		return false
	}

	atomic.AddInt64(&m.activeCalls, 1)
	return true
}

// ReleaseCall releases a call slot
func (m *Manager) ReleaseCall() {
	current := atomic.AddInt64(&m.activeCalls, -1)
	if current < 0 {
		atomic.StoreInt64(&m.activeCalls, 0)
	}
}

// AcquireRTPStream attempts to acquire an RTP stream slot
func (m *Manager) AcquireRTPStream() bool {
	return m.rtpLimiter.Acquire()
}

// ReleaseRTPStream releases an RTP stream slot
func (m *Manager) ReleaseRTPStream() {
	m.rtpLimiter.Release()
}

// GetActiveStreams returns the current number of active RTP streams
func (m *Manager) GetActiveStreams() int64 {
	return m.rtpLimiter.ActiveCount()
}

// SubmitWork submits work to the worker pool
func (m *Manager) SubmitWork(work func()) bool {
	return m.workerPool.Submit(work)
}

// SubmitWorkBlocking submits work and blocks until accepted
func (m *Manager) SubmitWorkBlocking(work func()) bool {
	return m.workerPool.SubmitBlocking(work)
}

// SubmitWorkWithTimeout submits work with a timeout
func (m *Manager) SubmitWorkWithTimeout(work func(), timeout time.Duration) bool {
	return m.workerPool.SubmitWithTimeout(work, timeout)
}

// CheckMemory returns true if memory is within limits
func (m *Manager) CheckMemory() bool {
	if m.memoryMonitor == nil {
		return true
	}
	return m.memoryMonitor.CheckWithinLimit()
}

// GetMemoryUsage returns current memory usage in bytes
func (m *Manager) GetMemoryUsage() int64 {
	if m.memoryMonitor == nil {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)
		return int64(memStats.Alloc)
	}
	return m.memoryMonitor.CurrentUsage()
}

// GetStats returns current resource statistics
func (m *Manager) GetStats() Stats {
	activeCalls := atomic.LoadInt64(&m.activeCalls)
	activeStreams := m.rtpLimiter.ActiveCount()
	memoryUsedMB := atomic.LoadInt64(&m.memoryUsedMB)

	// Calculate capacities
	callCapacity := float64(activeCalls) / float64(m.config.MaxConcurrentCalls)
	streamCapacity := float64(activeStreams) / float64(m.config.MaxRTPStreams)
	memoryCapacity := float64(0)
	if m.config.MaxMemoryMB > 0 {
		memoryCapacity = float64(memoryUsedMB) / float64(m.config.MaxMemoryMB)
	}

	return Stats{
		ActiveCalls:      activeCalls,
		ActiveStreams:    activeStreams,
		MemoryUsedMB:     memoryUsedMB,
		MemoryLimitMB:    m.config.MaxMemoryMB,
		WorkerPoolActive: m.workerPool.ActiveWorkers(),
		WorkerPoolSize:   m.config.WorkerPoolSize,
		CallCapacity:     callCapacity,
		StreamCapacity:   streamCapacity,
		MemoryCapacity:   memoryCapacity,
		NodeID:           m.config.NodeID,
	}
}

// SetCallbacks sets resource exhaustion callbacks
func (m *Manager) SetCallbacks(onExhausted, onRecovered func(resourceType string)) {
	m.onResourceExhausted = onExhausted
	m.onResourceRecovered = onRecovered
}

// IsOverloaded returns true if any resource is near capacity
func (m *Manager) IsOverloaded() bool {
	stats := m.GetStats()
	return stats.CallCapacity > 0.9 || stats.StreamCapacity > 0.9 || stats.MemoryCapacity > 0.9
}

// GetCapacity returns the minimum available capacity across all resources
func (m *Manager) GetCapacity() float64 {
	stats := m.GetStats()
	minAvailable := 1.0 - stats.CallCapacity
	if streamAvail := 1.0 - stats.StreamCapacity; streamAvail < minAvailable {
		minAvailable = streamAvail
	}
	if m.config.MaxMemoryMB > 0 {
		if memAvail := 1.0 - stats.MemoryCapacity; memAvail < minAvailable {
			minAvailable = memAvail
		}
	}
	return minAvailable
}

// Stop shuts down the resource manager
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()

	m.workerPool.Stop()
	if m.memoryMonitor != nil {
		m.memoryMonitor.Stop()
	}

	m.logger.Info("Resource manager stopped")
}
