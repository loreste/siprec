package cluster

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func goroutineBaseline() int {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

func assertNoGoroutineLeak(t *testing.T, baseline int, tolerance int, label string) {
	t.Helper()
	for i := 0; i < 30; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		if runtime.NumGoroutine() <= baseline+tolerance {
			break
		}
	}
	final := runtime.NumGoroutine()
	diff := final - baseline
	if diff > tolerance {
		buf := make([]byte, 64*1024)
		n := runtime.Stack(buf, true)
		t.Errorf("[%s] goroutine leak: baseline=%d final=%d diff=%d (tolerance=%d)\n\nGoroutine dump:\n%s",
			label, baseline, final, diff, tolerance, string(buf[:n]))
	} else {
		t.Logf("[%s] goroutines OK: baseline=%d final=%d diff=%d", label, baseline, final, diff)
	}
}

func leakTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

// ---------------------------------------------------------------------------
// Manager goroutine leak tests
// ---------------------------------------------------------------------------

func TestManagerLifecycle_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		cfg := Config{
			Enabled:               true,
			NodeID:                fmt.Sprintf("leak-node-%d", i),
			HeartbeatInterval:     50 * time.Millisecond,
			NodeTTL:               200 * time.Millisecond,
			LeaderElectionEnabled: true,
			LeaderLockTTL:         200 * time.Millisecond,
			LeaderRetryInterval:   50 * time.Millisecond,
		}
		mgr := NewManager(cfg, client, leakTestLogger(), "localhost")
		require.NoError(t, mgr.Start())
		time.Sleep(30 * time.Millisecond) // let goroutines run
		mgr.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "Manager lifecycle x10")
}

func TestManagerLeaderElection_NoGoroutineLeak(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()
	baseline := goroutineBaseline()

	const nodes = 5
	managers := make([]*Manager, nodes)
	for i := 0; i < nodes; i++ {
		c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cfg := Config{
			Enabled:               true,
			NodeID:                fmt.Sprintf("leader-leak-%d", i),
			HeartbeatInterval:     50 * time.Millisecond,
			NodeTTL:               200 * time.Millisecond,
			LeaderElectionEnabled: true,
			LeaderLockTTL:         200 * time.Millisecond,
			LeaderRetryInterval:   50 * time.Millisecond,
		}
		managers[i] = NewManager(cfg, c, leakTestLogger(), "localhost")
		require.NoError(t, managers[i].Start())
	}

	time.Sleep(100 * time.Millisecond) // let election settle

	// Stop all
	for _, m := range managers {
		m.Stop()
	}

	assertNoGoroutineLeak(t, baseline, 3, "Manager 5-node leader election")
}

// ---------------------------------------------------------------------------
// RTPStateManager goroutine leak tests
// ---------------------------------------------------------------------------

func TestRTPStateManager_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		mgr := NewRTPStateManager(client, fmt.Sprintf("rtp-node-%d", i), leakTestLogger())
		require.NoError(t, mgr.Start(context.Background()))

		// Register/unregister some streams
		for j := 0; j < 20; j++ {
			_ = mgr.RegisterStream(&RTPStreamState{
				CallUUID:  fmt.Sprintf("call-%d-%d", i, j),
				LocalPort: 10000 + j,
				CodecName: "PCMU",
			})
		}
		for j := 0; j < 20; j++ {
			_ = mgr.UnregisterStream(fmt.Sprintf("call-%d-%d", i, j))
		}

		mgr.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "RTPStateManager lifecycle x10")
}

func TestRTPStateManager_RapidRegisterUnregister(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	baseline := goroutineBaseline()
	mgr := NewRTPStateManager(client, "rapid-node", leakTestLogger())
	require.NoError(t, mgr.Start(context.Background()))

	// Rapid create/destroy
	for i := 0; i < 500; i++ {
		callID := fmt.Sprintf("rapid-%d", i)
		_ = mgr.RegisterStream(&RTPStreamState{CallUUID: callID, LocalPort: 10000 + i%1000, CodecName: "PCMU"})
		_ = mgr.UnregisterStream(callID)
	}

	mgr.Stop()
	assertNoGoroutineLeak(t, baseline, 3, "RTPState rapid register/unregister x500")
}

// ---------------------------------------------------------------------------
// StreamMigrationManager goroutine leak tests
// ---------------------------------------------------------------------------

func TestStreamMigration_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		rtpMgr := NewRTPStateManager(client, fmt.Sprintf("mig-node-%d", i), leakTestLogger())
		require.NoError(t, rtpMgr.Start(context.Background()))

		migMgr := NewStreamMigrationManager(client, fmt.Sprintf("mig-node-%d", i), rtpMgr, leakTestLogger())
		require.NoError(t, migMgr.Start(context.Background()))

		time.Sleep(20 * time.Millisecond)

		migMgr.Stop()
		rtpMgr.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "StreamMigration lifecycle x10")
}

// ---------------------------------------------------------------------------
// DistributedTracer goroutine leak tests
// ---------------------------------------------------------------------------

func TestDistributedTracer_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		tracer := NewDistributedTracer(client, fmt.Sprintf("trace-node-%d", i), leakTestLogger())
		require.NoError(t, tracer.Start(context.Background()))

		// Create and end traces
		for j := 0; j < 20; j++ {
			span := tracer.StartTrace(context.Background(), "test.op", fmt.Sprintf("call-%d", j))
			tracer.SetTag(span, "key", "val")
			tracer.EndSpan(span, nil)
		}

		tracer.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "DistributedTracer lifecycle x10")
}

func TestDistributedTracer_SpanHierarchy_NoLeak(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	baseline := goroutineBaseline()
	tracer := NewDistributedTracer(client, "hierarchy-node", leakTestLogger())
	require.NoError(t, tracer.Start(context.Background()))

	// Deep span hierarchies
	for i := 0; i < 50; i++ {
		root := tracer.StartTrace(context.Background(), "root", fmt.Sprintf("call-%d", i))
		child1 := tracer.StartSpan(context.Background(), root, "child1")
		child2 := tracer.StartSpan(context.Background(), child1, "child2")
		child3 := tracer.StartSpan(context.Background(), child2, "child3")
		tracer.EndSpan(child3, nil)
		tracer.EndSpan(child2, nil)
		tracer.EndSpan(child1, nil)
		tracer.EndSpan(root, nil)
	}

	tracer.Stop()
	assertNoGoroutineLeak(t, baseline, 3, "Tracer span hierarchy x50")
}

// ---------------------------------------------------------------------------
// SplitBrainDetector goroutine leak tests
// ---------------------------------------------------------------------------

func TestSplitBrainDetector_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		detector := NewSplitBrainDetector(client, fmt.Sprintf("sb-node-%d", i), SplitBrainConfig{
			MinQuorum:       1,
			CheckInterval:   50 * time.Millisecond,
			NodeTimeout:     100 * time.Millisecond,
			GracePeriod:     200 * time.Millisecond,
			PartitionAction: "readonly",
			EnableFencing:   true,
		}, leakTestLogger())

		require.NoError(t, detector.Start(context.Background()))
		time.Sleep(30 * time.Millisecond) // let check loop run
		detector.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "SplitBrainDetector lifecycle x10")
}

// ---------------------------------------------------------------------------
// DistributedRateLimiter goroutine leak tests
// ---------------------------------------------------------------------------

func TestDistributedRateLimiter_NoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	for i := 0; i < 10; i++ {
		mr, client := setupTestRedis(t)
		limiter := NewDistributedRateLimiter(client, RateLimitConfig{
			GlobalCallsPerSecond:     100,
			GlobalCallsPerMinute:     5000,
			PerIPCallsPerSecond:      10,
			PerIPCallsPerMinute:      100,
			BurstAllowance: 1.2,
		}, leakTestLogger())

		limiter.Start()

		// Make some rate limit checks
		for j := 0; j < 50; j++ {
			limiter.AllowCall(context.Background(), fmt.Sprintf("10.0.0.%d", j%256))
		}

		limiter.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 3, "DistributedRateLimiter lifecycle x10")
}

// ---------------------------------------------------------------------------
// Memory leak tests
// ---------------------------------------------------------------------------

func TestRTPStateManager_NoMemoryLeak(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	mgr := NewRTPStateManager(client, "mem-node", leakTestLogger())
	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop()

	var heapSizes [5]uint64

	for round := 0; round < 5; round++ {
		// Register and unregister a batch of streams
		for i := 0; i < 1000; i++ {
			callID := fmt.Sprintf("mem-call-%d-%d", round, i)
			_ = mgr.RegisterStream(&RTPStreamState{
				CallUUID:  callID,
				LocalPort: 10000 + i%10000,
				CodecName: "PCMU",
				Metadata:  map[string]string{"round": fmt.Sprintf("%d", round)},
			})
		}
		for i := 0; i < 1000; i++ {
			_ = mgr.UnregisterStream(fmt.Sprintf("mem-call-%d-%d", round, i))
		}

		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapSizes[round] = ms.HeapInuse
		t.Logf("Round %d: HeapInuse=%d KB, localStates=%d", round, ms.HeapInuse/1024, len(mgr.ListLocalStreams()))
	}

	// Local cache should be empty after all unregisters
	assert.Equal(t, 0, len(mgr.ListLocalStreams()), "local cache should be empty after cleanup")

	// Heap shouldn't grow unboundedly — last round shouldn't be 3x the second
	if heapSizes[4] > heapSizes[1]*3 && heapSizes[4] > 10*1024*1024 {
		t.Errorf("Possible memory leak: heap grew from %d KB to %d KB",
			heapSizes[1]/1024, heapSizes[4]/1024)
	}
}

func TestDistributedTracer_NoMemoryLeak(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	tracer := NewDistributedTracer(client, "mem-trace-node", leakTestLogger())
	require.NoError(t, tracer.Start(context.Background()))
	defer tracer.Stop()

	var heapSizes [5]uint64

	for round := 0; round < 5; round++ {
		for i := 0; i < 500; i++ {
			span := tracer.StartTrace(context.Background(), "mem-test", fmt.Sprintf("call-%d-%d", round, i))
			tracer.SetTag(span, "round", fmt.Sprintf("%d", round))
			tracer.Log(span, "processing", map[string]string{"i": fmt.Sprintf("%d", i)})
			tracer.EndSpan(span, nil)
		}

		time.Sleep(50 * time.Millisecond)
		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapSizes[round] = ms.HeapInuse

		stats := tracer.GetStats()
		t.Logf("Round %d: HeapInuse=%d KB, active_spans=%v", round, ms.HeapInuse/1024, stats["active_spans"])
	}

	// Active spans should be 0 after all EndSpan calls
	stats := tracer.GetStats()
	assert.Equal(t, 0, stats["active_spans"], "no active spans should remain")

	if heapSizes[4] > heapSizes[1]*3 && heapSizes[4] > 10*1024*1024 {
		t.Errorf("Possible memory leak: heap grew from %d KB to %d KB",
			heapSizes[1]/1024, heapSizes[4]/1024)
	}
}

func TestManager_NoMemoryLeak(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	cfg := Config{
		Enabled:               true,
		NodeID:                "mem-mgr-node",
		HeartbeatInterval:     100 * time.Millisecond,
		NodeTTL:               500 * time.Millisecond,
		LeaderElectionEnabled: false,
	}
	mgr := NewManager(cfg, client, leakTestLogger(), "localhost")
	require.NoError(t, mgr.Start())
	defer mgr.Stop()

	var heapSizes [3]uint64
	for round := 0; round < 3; round++ {
		// Simulate lots of ListNodes / GetClusterStatus calls
		for i := 0; i < 500; i++ {
			_, _ = mgr.ListNodes(context.Background())
			_, _ = mgr.GetClusterStatus(context.Background())
		}

		runtime.GC()
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapSizes[round] = ms.HeapInuse
		t.Logf("Round %d: HeapInuse=%d KB", round, ms.HeapInuse/1024)
	}

	if heapSizes[2] > heapSizes[0]*3 && heapSizes[2] > 10*1024*1024 {
		t.Errorf("Possible memory leak: heap grew from %d KB to %d KB",
			heapSizes[0]/1024, heapSizes[2]/1024)
	}
}

// ---------------------------------------------------------------------------
// Race condition tests (all run with -race flag)
// ---------------------------------------------------------------------------

func TestManager_ConcurrentLeadershipCallbacks(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	cfg := Config{
		Enabled:               true,
		NodeID:                "race-cb-node",
		HeartbeatInterval:     50 * time.Millisecond,
		NodeTTL:               200 * time.Millisecond,
		LeaderElectionEnabled: true,
		LeaderLockTTL:         200 * time.Millisecond,
		LeaderRetryInterval:   50 * time.Millisecond,
	}
	mgr := NewManager(cfg, client, leakTestLogger(), "localhost")

	var wg sync.WaitGroup
	// Concurrent callback registration
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.OnLeadershipChange(func(isLeader bool) {
				// just access the flag
				_ = isLeader
			})
		}()
	}

	require.NoError(t, mgr.Start())

	// Concurrent reads while manager is running
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = mgr.IsLeader()
				_, _ = mgr.GetLeader(context.Background())
				_, _ = mgr.ListNodes(context.Background())
				_, _ = mgr.GetClusterStatus(context.Background())
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
	mgr.Stop()
}

func TestRTPStateManager_ConcurrentRegisterUpdateUnregister(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	mgr := NewRTPStateManager(client, "race-rtp-node", leakTestLogger())
	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop()

	var wg sync.WaitGroup
	const goroutines = 20
	const opsPerGoroutine = 50

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				callID := fmt.Sprintf("race-%d-%d", id, j)
				_ = mgr.RegisterStream(&RTPStreamState{
					CallUUID:  callID,
					LocalPort: 10000 + j,
					CodecName: "PCMU",
				})
				_ = mgr.UpdateStream(callID, map[string]interface{}{
					"packets_received": int64(j * 100),
					"jitter":           float64(j) * 0.5,
				})
				_ = mgr.ListLocalStreams()
				_ = mgr.UnregisterStream(callID)
			}
		}(g)
	}
	wg.Wait()
}

func TestDistributedTracer_ConcurrentStartEndSpans(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	tracer := NewDistributedTracer(client, "race-trace-node", leakTestLogger())
	require.NoError(t, tracer.Start(context.Background()))
	defer tracer.Stop()

	var wg sync.WaitGroup

	// Concurrent trace creation/completion
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				span := tracer.StartTrace(context.Background(), "race.op", fmt.Sprintf("call-%d-%d", id, j))
				tracer.SetTag(span, "goroutine", fmt.Sprintf("%d", id))
				tracer.Log(span, "message", map[string]string{"seq": fmt.Sprintf("%d", j)})
				child := tracer.StartSpan(context.Background(), span, "child.op")
				tracer.EndSpan(child, nil)
				tracer.EndSpan(span, nil)
			}
		}(g)
	}

	// Concurrent stats reading
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = tracer.GetStats()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Concurrent callback registration
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracer.OnTraceComplete(func(tc *TraceContext) { _ = tc.Status })
		}()
	}

	wg.Wait()
}

func TestSplitBrainDetector_ConcurrentReads(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	detector := NewSplitBrainDetector(client, "race-sb-node", SplitBrainConfig{
		MinQuorum:       1,
		CheckInterval:   50 * time.Millisecond,
		NodeTimeout:     100 * time.Millisecond,
		GracePeriod:     200 * time.Millisecond,
		PartitionAction: "readonly",
		EnableFencing:   true,
	}, leakTestLogger())

	require.NoError(t, detector.Start(context.Background()))
	defer detector.Stop()

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = detector.HasQuorum()
				_ = detector.IsPartitioned()
				_ = detector.IsFenced()
				_ = detector.GetStatus()
				_ = detector.GetReachableNodes()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Concurrent callback registration
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detector.OnPartitionChange(func(detected bool, nodes []string) {})
		}()
	}

	// Concurrent forced checks
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = detector.ForceQuorumCheck(context.Background())
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

func TestDistributedRateLimiter_ConcurrentAllowCall(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	limiter := NewDistributedRateLimiter(client, RateLimitConfig{
		GlobalCallsPerSecond:      1000,
		GlobalCallsPerMinute:      50000,
		PerIPCallsPerSecond:       100,
		PerIPCallsPerMinute:       1000,
		BurstAllowance: 1.2,
	}, leakTestLogger())
	limiter.Start()
	defer limiter.Stop()

	var wg sync.WaitGroup
	var allowed, denied int64

	for g := 0; g < 30; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var localAllowed, localDenied int64
			for j := 0; j < 100; j++ {
				result := limiter.AllowCall(context.Background(), fmt.Sprintf("10.0.%d.%d", id%256, j%256))
				if result.Allowed {
					localAllowed++
				} else {
					localDenied++
				}
			}
			// Use atomic-free approach: sync at end
			mu.Lock()
			allowed += localAllowed
			denied += localDenied
			mu.Unlock()
		}(g)
	}

	wg.Wait()

	total := allowed + denied
	assert.Equal(t, int64(3000), total, "all calls should be accounted for")
	t.Logf("Rate limiter concurrent: allowed=%d denied=%d total=%d", allowed, denied, total)

	metrics := limiter.GetMetrics()
	t.Logf("Metrics: %v", metrics)
}

var mu sync.Mutex

// ---------------------------------------------------------------------------
// Combined stress: all components lifecycle
// ---------------------------------------------------------------------------

func TestAllComponents_CombinedLifecycle_NoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping combined stress test in short mode")
	}

	baseline := goroutineBaseline()

	for cycle := 0; cycle < 5; cycle++ {
		mr, client := setupTestRedis(t)
		logger := leakTestLogger()
		nodeID := fmt.Sprintf("combined-%d", cycle)

		// Start all components
		mgr := NewManager(Config{
			Enabled:               true,
			NodeID:                nodeID,
			HeartbeatInterval:     50 * time.Millisecond,
			NodeTTL:               200 * time.Millisecond,
			LeaderElectionEnabled: true,
			LeaderLockTTL:         200 * time.Millisecond,
			LeaderRetryInterval:   50 * time.Millisecond,
		}, client, logger, "localhost")
		require.NoError(t, mgr.Start())

		rtpState := NewRTPStateManager(client, nodeID, logger)
		require.NoError(t, rtpState.Start(context.Background()))

		tracer := NewDistributedTracer(client, nodeID, logger)
		require.NoError(t, tracer.Start(context.Background()))

		migration := NewStreamMigrationManager(client, nodeID, rtpState, logger)
		require.NoError(t, migration.Start(context.Background()))

		detector := NewSplitBrainDetector(client, nodeID, SplitBrainConfig{
			MinQuorum:       1,
			CheckInterval:   100 * time.Millisecond,
			NodeTimeout:     200 * time.Millisecond,
			GracePeriod:     500 * time.Millisecond,
			PartitionAction: "readonly",
		}, logger)
		require.NoError(t, detector.Start(context.Background()))

		limiter := NewDistributedRateLimiter(client, RateLimitConfig{
			GlobalCallsPerSecond: 100, GlobalCallsPerMinute: 5000, PerIPCallsPerSecond: 10, PerIPCallsPerMinute: 100,
			BurstAllowance: 1.2,
		}, logger)
		limiter.Start()

		// Exercise all components
		for i := 0; i < 20; i++ {
			callID := fmt.Sprintf("combined-%d-%d", cycle, i)
			_ = rtpState.RegisterStream(&RTPStreamState{CallUUID: callID, LocalPort: 10000 + i, CodecName: "PCMU"})
			span := tracer.StartTrace(context.Background(), "test", callID)
			limiter.AllowCall(context.Background(), "10.0.0.1")
			_ = detector.HasQuorum()
			tracer.EndSpan(span, nil)
			_ = rtpState.UnregisterStream(callID)
		}

		// Shutdown in reverse order
		limiter.Stop()
		detector.Stop()
		migration.Stop()
		tracer.Stop()
		rtpState.Stop()
		mgr.Stop()
		client.Close()
		mr.Close()
	}

	assertNoGoroutineLeak(t, baseline, 5, "All components combined lifecycle x5")
}

// ---------------------------------------------------------------------------
// GC pressure test
// ---------------------------------------------------------------------------

func TestRTPStateManager_GCPressure(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	mgr := NewRTPStateManager(client, "gc-node", leakTestLogger())
	require.NoError(t, mgr.Start(context.Background()))
	defer mgr.Stop()

	// Warm up
	for i := 0; i < 100; i++ {
		callID := fmt.Sprintf("gc-warm-%d", i)
		_ = mgr.RegisterStream(&RTPStreamState{CallUUID: callID, LocalPort: 10000 + i, CodecName: "PCMU"})
		_ = mgr.UnregisterStream(callID)
	}

	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	const ops = 5000
	for i := 0; i < ops; i++ {
		callID := fmt.Sprintf("gc-test-%d", i)
		_ = mgr.RegisterStream(&RTPStreamState{
			CallUUID:  callID,
			LocalPort: 10000 + i%10000,
			CodecName: "PCMU",
			Metadata:  map[string]string{"seq": fmt.Sprintf("%d", i)},
		})
		_ = mgr.UpdateStream(callID, map[string]interface{}{
			"packets_received": int64(i * 100),
		})
		_ = mgr.UnregisterStream(callID)
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
	allocsCount := memAfter.Mallocs - memBefore.Mallocs

	t.Logf("GC pressure: %d ops, total_alloc=%d KB, mallocs=%d, alloc_per_op=%d bytes",
		ops, allocBytes/1024, allocsCount, allocBytes/ops)

	// Sanity: less than 30KB per register/update/unregister cycle
	// (each op involves JSON marshal + Redis round-trip allocations)
	avgPerOp := allocBytes / ops
	if avgPerOp > 30*1024 {
		t.Errorf("Excessive allocation per op: %d bytes (expected <30KB)", avgPerOp)
	}
}
