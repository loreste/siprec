package resources

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerPool(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	pool := NewWorkerPool(4, logger)
	pool.Start()

	var completed int64
	var wg sync.WaitGroup

	// Submit work using blocking submit to ensure all work gets accepted
	for i := 0; i < 100; i++ {
		wg.Add(1)
		ok := pool.SubmitBlocking(func() {
			atomic.AddInt64(&completed, 1)
			wg.Done()
		})
		require.True(t, ok)
	}

	// Wait for completion
	wg.Wait()
	assert.Equal(t, int64(100), atomic.LoadInt64(&completed))

	pool.Stop()

	submitted, completedStat, rejected := pool.Stats()
	assert.Equal(t, int64(100), submitted)
	assert.Equal(t, int64(100), completedStat)
	assert.Equal(t, int64(0), rejected)
}

func TestWorkerPoolRejection(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Small pool that will fill up
	pool := NewWorkerPool(1, logger)
	pool.Start()

	// Fill the queue with blocking work
	blocker := make(chan struct{})
	pool.Submit(func() {
		<-blocker // Block forever until released
	})

	// Fill the buffer (size is 2 for 1 worker)
	time.Sleep(10 * time.Millisecond)
	pool.Submit(func() {})
	pool.Submit(func() {})

	// This should be rejected
	rejected := !pool.Submit(func() {})
	assert.True(t, rejected)

	close(blocker)
	pool.Stop()
}

func TestWorkerPoolWithTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	pool := NewWorkerPool(2, logger)
	pool.Start()

	var executed int64
	ok := pool.SubmitWithTimeout(func() {
		atomic.StoreInt64(&executed, 1)
	}, 100*time.Millisecond)

	require.True(t, ok)
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), atomic.LoadInt64(&executed))

	pool.Stop()
}

func TestWorkerPoolPanicRecovery(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	pool := NewWorkerPool(2, logger)
	pool.Start()

	// Submit work that panics
	var afterPanic int64
	pool.Submit(func() {
		panic("test panic")
	})

	time.Sleep(50 * time.Millisecond)

	// Pool should still work after panic
	ok := pool.Submit(func() {
		atomic.StoreInt64(&afterPanic, 1)
	})
	require.True(t, ok)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), atomic.LoadInt64(&afterPanic))

	pool.Stop()
}

func TestRTPLimiter(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limiter := NewRTPLimiter(10, logger)

	// Acquire all slots
	for i := 0; i < 10; i++ {
		ok := limiter.Acquire()
		require.True(t, ok, "Should acquire slot %d", i)
	}

	assert.Equal(t, int64(10), limiter.ActiveCount())
	assert.Equal(t, int64(0), limiter.AvailableSlots())

	// Next acquire should fail
	ok := limiter.Acquire()
	assert.False(t, ok)

	// Release one
	limiter.Release()
	assert.Equal(t, int64(9), limiter.ActiveCount())
	assert.Equal(t, int64(1), limiter.AvailableSlots())

	// Should be able to acquire again
	ok = limiter.Acquire()
	assert.True(t, ok)

	// Release all
	for i := 0; i < 10; i++ {
		limiter.Release()
	}
	assert.Equal(t, int64(0), limiter.ActiveCount())
}

func TestRTPLimiterConcurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limiter := NewRTPLimiter(100, logger)

	var wg sync.WaitGroup
	var acquired int64
	var released int64

	// Concurrent acquires
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.Acquire() {
				atomic.AddInt64(&acquired, 1)
				time.Sleep(time.Millisecond)
				limiter.Release()
				atomic.AddInt64(&released, 1)
			}
		}()
	}

	wg.Wait()

	// Should have limited to 100 concurrent
	assert.LessOrEqual(t, atomic.LoadInt64(&acquired), int64(200))
	assert.Equal(t, atomic.LoadInt64(&acquired), atomic.LoadInt64(&released))
	assert.Equal(t, int64(0), limiter.ActiveCount())
}

func TestRTPLimiterWaitForSlot(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	limiter := NewRTPLimiter(1, logger)

	// Take the only slot
	ok := limiter.Acquire()
	require.True(t, ok)

	// Try to wait for slot (should timeout)
	start := time.Now()
	ok = limiter.WaitForSlot(100 * time.Millisecond)
	elapsed := time.Since(start)

	assert.False(t, ok)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond)

	// Release and try again
	go func() {
		time.Sleep(50 * time.Millisecond)
		limiter.Release()
	}()

	ok = limiter.WaitForSlot(200 * time.Millisecond)
	assert.True(t, ok)
}

func TestMemoryMonitor(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Set a very high limit so we're within it
	monitor := NewMemoryMonitor(10000, 50*time.Millisecond, logger)
	monitor.Start()

	time.Sleep(100 * time.Millisecond)

	// Should be within limits
	assert.True(t, monitor.CheckWithinLimit())
	assert.Greater(t, monitor.CurrentUsage(), int64(0))

	// Get detailed stats
	stats := monitor.GetDetailedStats()
	assert.Contains(t, stats, "alloc_mb")
	assert.Contains(t, stats, "heap_objects")

	monitor.Stop()
}

func TestMemoryMonitorCallback(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	var callbackCalled int64
	var lastUsed int64

	monitor := NewMemoryMonitor(10000, 50*time.Millisecond, logger)
	monitor.SetCallback(func(used, limit int64) {
		atomic.AddInt64(&callbackCalled, 1)
		atomic.StoreInt64(&lastUsed, used)
	})
	monitor.Start()

	time.Sleep(150 * time.Millisecond)

	assert.Greater(t, atomic.LoadInt64(&callbackCalled), int64(0))
	assert.Greater(t, atomic.LoadInt64(&lastUsed), int64(0))

	monitor.Stop()
}

func TestResourceManager(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		MaxConcurrentCalls: 10,
		MaxRTPStreams:      20,
		WorkerPoolSize:     4,
		MaxMemoryMB:        1024,
		MonitorInterval:    50 * time.Millisecond,
		NodeID:             "test-node",
	}

	manager, err := NewManager(cfg, logger)
	require.NoError(t, err)

	manager.Start()

	// Test call acquisition
	for i := 0; i < 10; i++ {
		ok := manager.AcquireCall()
		require.True(t, ok, "Should acquire call %d", i)
	}

	// Should fail at limit
	ok := manager.AcquireCall()
	assert.False(t, ok)

	// Release and try again
	manager.ReleaseCall()
	ok = manager.AcquireCall()
	assert.True(t, ok)

	// Test RTP streams
	for i := 0; i < 20; i++ {
		ok := manager.AcquireRTPStream()
		require.True(t, ok, "Should acquire RTP stream %d", i)
	}

	ok = manager.AcquireRTPStream()
	assert.False(t, ok)

	// Check stats
	stats := manager.GetStats()
	assert.Equal(t, int64(10), stats.ActiveCalls)
	assert.Equal(t, int64(20), stats.ActiveStreams)
	assert.Equal(t, "test-node", stats.NodeID)
	assert.Equal(t, 1.0, stats.CallCapacity)
	assert.Equal(t, 1.0, stats.StreamCapacity)

	// Test overloaded
	assert.True(t, manager.IsOverloaded())

	// Release all
	for i := 0; i < 10; i++ {
		manager.ReleaseCall()
	}
	for i := 0; i < 20; i++ {
		manager.ReleaseRTPStream()
	}

	stats = manager.GetStats()
	assert.Equal(t, int64(0), stats.ActiveCalls)
	assert.Equal(t, int64(0), stats.ActiveStreams)
	assert.False(t, manager.IsOverloaded())

	manager.Stop()
}

func TestResourceManagerWorkerPool(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		MaxConcurrentCalls: 100,
		MaxRTPStreams:      200,
		WorkerPoolSize:     4,
	}

	manager, err := NewManager(cfg, logger)
	require.NoError(t, err)

	manager.Start()

	var completed int64
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		ok := manager.SubmitWorkBlocking(func() {
			atomic.AddInt64(&completed, 1)
			wg.Done()
		})
		require.True(t, ok)
	}

	wg.Wait()
	assert.Equal(t, int64(50), atomic.LoadInt64(&completed))

	manager.Stop()
}

func TestResourceManagerCapacity(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		MaxConcurrentCalls: 100,
		MaxRTPStreams:      100,
		WorkerPoolSize:     4,
	}

	manager, err := NewManager(cfg, logger)
	require.NoError(t, err)

	manager.Start()

	// Initially should have full capacity
	capacity := manager.GetCapacity()
	assert.Equal(t, 1.0, capacity)

	// Use 50% of calls
	for i := 0; i < 50; i++ {
		manager.AcquireCall()
	}

	capacity = manager.GetCapacity()
	assert.Equal(t, 0.5, capacity)

	// Use 70% of streams
	for i := 0; i < 70; i++ {
		manager.AcquireRTPStream()
	}

	// Capacity should be min(0.5, 0.3) = 0.3
	capacity = manager.GetCapacity()
	assert.InDelta(t, 0.3, capacity, 0.01)

	manager.Stop()
}

func TestResourceManagerCallbacks(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cfg := Config{
		MaxConcurrentCalls: 10,
		MaxRTPStreams:      10,
		WorkerPoolSize:     2,
	}

	manager, err := NewManager(cfg, logger)
	require.NoError(t, err)

	var exhaustedResource string
	manager.SetCallbacks(
		func(resourceType string) {
			exhaustedResource = resourceType
		},
		nil,
	)

	manager.Start()

	// Exhaust calls
	for i := 0; i < 10; i++ {
		manager.AcquireCall()
	}

	// Next call should trigger callback
	manager.AcquireCall()
	assert.Equal(t, "calls", exhaustedResource)

	manager.Stop()
}
