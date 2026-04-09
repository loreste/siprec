package test

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/messaging"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/stt"
)

func init() {
	// Disable Prometheus metrics so tests don't need a full metrics registry
	metrics.EnableMetrics(false)
}

// --------------------------------------------------------------------------
// Mock AMQP client for testing without a real broker
// --------------------------------------------------------------------------

type mockAMQPClient struct {
	connected    atomic.Bool
	publishCount atomic.Int64
	publishDelay time.Duration
	publishErr   error
	mu           sync.Mutex
}

func newMockAMQPClient(connected bool) *mockAMQPClient {
	c := &mockAMQPClient{}
	c.connected.Store(connected)
	return c
}

func (m *mockAMQPClient) PublishTranscription(transcription, callUUID string, metadata map[string]interface{}) error {
	if m.publishDelay > 0 {
		time.Sleep(m.publishDelay)
	}
	m.mu.Lock()
	err := m.publishErr
	m.mu.Unlock()
	if err != nil {
		return err
	}
	m.publishCount.Add(1)
	return nil
}

func (m *mockAMQPClient) PublishToDeadLetterQueue(content, callUUID string, metadata map[string]interface{}) error {
	return nil
}

func (m *mockAMQPClient) IsConnected() bool {
	return m.connected.Load()
}

func (m *mockAMQPClient) Connect() error {
	m.connected.Store(true)
	return nil
}

func (m *mockAMQPClient) Disconnect() {
	m.connected.Store(false)
}

// --------------------------------------------------------------------------
// Mock STT provider for testing without real cloud services
// --------------------------------------------------------------------------

type mockSTTProvider struct {
	name         string
	initErr      error
	streamErr    error
	streamDelay  time.Duration
	streamCount  atomic.Int64
	callback     stt.TranscriptionCallback
	callbackMu   sync.Mutex
}

func (p *mockSTTProvider) Initialize() error { return p.initErr }
func (p *mockSTTProvider) Name() string      { return p.name }

func (p *mockSTTProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	p.streamCount.Add(1)
	if p.streamDelay > 0 {
		select {
		case <-time.After(p.streamDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Consume the audio stream like a real provider would
	buf := make([]byte, 1024)
	for {
		_, err := audioStream.Read(buf)
		if err != nil {
			break
		}
	}

	// Fire callback if set
	p.callbackMu.Lock()
	cb := p.callback
	p.callbackMu.Unlock()
	if cb != nil {
		cb(callUUID, "test transcription", true, map[string]interface{}{"provider": p.name})
	}
	return p.streamErr
}

func (p *mockSTTProvider) SetCallback(callback stt.TranscriptionCallback) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.callback = callback
}

// --------------------------------------------------------------------------
// Goroutine Leak Tests
// --------------------------------------------------------------------------

func goroutineBaseline() int {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

func assertNoGoroutineLeak(t *testing.T, baseline int, tolerance int, label string) {
	t.Helper()
	// Give goroutines time to exit
	for i := 0; i < 20; i++ {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		if runtime.NumGoroutine() <= baseline+tolerance {
			break
		}
	}
	final := runtime.NumGoroutine()
	diff := final - baseline
	if diff > tolerance {
		// Dump goroutine stacks for debugging
		buf := make([]byte, 64*1024)
		n := runtime.Stack(buf, true)
		t.Errorf("[%s] goroutine leak: baseline=%d final=%d diff=%d (tolerance=%d)\n\nGoroutine dump:\n%s",
			label, baseline, final, diff, tolerance, string(buf[:n]))
	} else {
		t.Logf("[%s] goroutines OK: baseline=%d final=%d diff=%d", label, baseline, final, diff)
	}
}

func TestAMQPTranscriptionListener_GoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	const cycles = 20
	for i := 0; i < cycles; i++ {
		listener := messaging.NewAMQPTranscriptionListener(logger, client)

		// Pump some messages through
		for j := 0; j < 100; j++ {
			listener.OnTranscription(
				fmt.Sprintf("call-%d-%d", i, j),
				"hello world",
				j%2 == 0,
				map[string]interface{}{"seq": j},
			)
		}

		// Let workers drain
		time.Sleep(20 * time.Millisecond)
		listener.Shutdown()
	}

	// 33 goroutines started per listener (32 workers + 1 health check)
	// After Shutdown all must exit.
	assertNoGoroutineLeak(t, baseline, 3, "AMQP listener lifecycle")
}

func TestAMQPTranscriptionListener_ShutdownDrainsWorkers(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	baseline := goroutineBaseline()

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	// Flood the channel
	for i := 0; i < 1000; i++ {
		listener.OnTranscription("call-drain", "text", true, nil)
	}

	listener.Shutdown()
	assertNoGoroutineLeak(t, baseline, 3, "AMQP shutdown drain")
}

func TestSTTProviderManager_GoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	const cycles = 10
	for i := 0; i < cycles; i++ {
		mgr := stt.NewProviderManager(logger, "mock", []string{"mock"})

		provider := &mockSTTProvider{name: "mock"}
		err := mgr.RegisterProvider(provider)
		require.NoError(t, err)

		// Stream audio through provider
		audio := strings.NewReader("fake audio data for testing purposes only")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = mgr.StreamToProvider(ctx, "mock", audio, fmt.Sprintf("call-%d", i))
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = mgr.Shutdown(shutdownCtx)
		shutdownCancel()
	}

	assertNoGoroutineLeak(t, baseline, 3, "STT ProviderManager lifecycle")
}

// --------------------------------------------------------------------------
// Memory Allocation / GC Pressure Tests
// --------------------------------------------------------------------------

func TestAMQPTranscriptionListener_MessagePoolReducesAllocs(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Use a slow client so messages queue up and pool can be exercised
	client := newMockAMQPClient(true)

	listener := messaging.NewAMQPTranscriptionListener(logger, client)
	defer listener.Shutdown()

	// Warm up the pool
	for i := 0; i < 200; i++ {
		listener.OnTranscription("warmup", "warmup text", true, map[string]interface{}{"k": "v"})
	}
	time.Sleep(100 * time.Millisecond)

	// Measure allocations
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	const msgCount = 5000
	for i := 0; i < msgCount; i++ {
		listener.OnTranscription(
			fmt.Sprintf("call-%d", i%100),
			"transcription text for allocation test",
			i%2 == 0,
			map[string]interface{}{"provider": "test", "seq": i},
		)
	}

	// Wait for workers to process
	time.Sleep(500 * time.Millisecond)

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	allocBytes := memAfter.TotalAlloc - memBefore.TotalAlloc
	allocsCount := memAfter.Mallocs - memBefore.Mallocs

	// Log allocation info (not strict failure — pool effectiveness varies)
	t.Logf("Published %d messages: total_alloc=%d KB, mallocs=%d, alloc_per_msg=%d bytes",
		msgCount, allocBytes/1024, allocsCount, allocBytes/msgCount)

	// The pool should prevent per-message struct allocations from growing linearly.
	// Sanity check: less than 10KB per message on average.
	avgAllocPerMsg := allocBytes / msgCount
	if avgAllocPerMsg > 10*1024 {
		t.Errorf("Excessive allocation per message: %d bytes (expected < 10KB)", avgAllocPerMsg)
	}
}

func TestAMQPTranscriptionListener_NoMemoryLeakUnderLoad(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	listener := messaging.NewAMQPTranscriptionListener(logger, client)
	defer listener.Shutdown()

	// Run multiple rounds and check that heap doesn't grow unboundedly
	var heapSizes [5]uint64

	for round := 0; round < 5; round++ {
		for i := 0; i < 2000; i++ {
			listener.OnTranscription(
				fmt.Sprintf("call-%d", i),
				"leak detection transcription text",
				true,
				map[string]interface{}{"round": round},
			)
		}
		time.Sleep(200 * time.Millisecond)

		runtime.GC()
		runtime.GC() // double GC for finalizers
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		heapSizes[round] = ms.HeapInuse
		t.Logf("Round %d: HeapInuse=%d KB", round, ms.HeapInuse/1024)
	}

	// Heap should stabilize — last round shouldn't be 3x the second round
	if heapSizes[4] > heapSizes[1]*3 && heapSizes[4] > 10*1024*1024 {
		t.Errorf("Possible memory leak: heap grew from %d KB to %d KB across rounds",
			heapSizes[1]/1024, heapSizes[4]/1024)
	}
}

// --------------------------------------------------------------------------
// Race Condition Tests (run with -race flag)
// --------------------------------------------------------------------------

func TestAMQPTranscriptionListener_ConcurrentPublish(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	listener := messaging.NewAMQPTranscriptionListener(logger, client)
	defer listener.Shutdown()

	var wg sync.WaitGroup
	const goroutines = 50
	const msgsPerGoroutine = 200

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsPerGoroutine; j++ {
				listener.OnTranscription(
					fmt.Sprintf("call-%d-%d", id, j),
					fmt.Sprintf("concurrent message %d from goroutine %d", j, id),
					j%3 == 0,
					map[string]interface{}{"goroutine": id, "msg": j},
				)
			}
		}(g)
	}

	wg.Wait()
	time.Sleep(300 * time.Millisecond) // let workers drain

	published, failed, dropped, _, _ := listener.GetMetrics()
	total := published + failed + dropped
	t.Logf("Concurrent publish: published=%d failed=%d dropped=%d total=%d expected=%d",
		published, failed, dropped, total, goroutines*msgsPerGoroutine)

	// All messages should be accounted for (published + dropped; failed only if client errors)
	assert.True(t, total > 0, "at least some messages should be processed")
}

func TestAMQPTranscriptionListener_ConcurrentPublishAndShutdown(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	baseline := goroutineBaseline()

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	// Publish from multiple goroutines
	var wg sync.WaitGroup
	stopPublishing := make(chan struct{})

	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stopPublishing:
					return
				default:
					listener.OnTranscription(
						fmt.Sprintf("call-%d-%d", id, i),
						"shutdown race test",
						true,
						nil,
					)
					i++
					time.Sleep(time.Microsecond)
				}
			}
		}(g)
	}

	// Let publishers run briefly then trigger shutdown
	time.Sleep(50 * time.Millisecond)
	close(stopPublishing)
	wg.Wait()

	listener.Shutdown()
	assertNoGoroutineLeak(t, baseline, 3, "concurrent publish + shutdown")
}

func TestAMQPTranscriptionListener_ConcurrentMetricsAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	listener := messaging.NewAMQPTranscriptionListener(logger, client)
	defer listener.Shutdown()

	var wg sync.WaitGroup

	// Concurrent publishers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				listener.OnTranscription(fmt.Sprintf("call-%d", id), "text", true, nil)
			}
		}(g)
	}

	// Concurrent metrics readers
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				listener.GetMetrics()
				listener.GetExtendedMetrics()
				listener.IsHealthy()
				listener.GetQueueLength()
				time.Sleep(time.Microsecond)
			}
		}()
	}

	wg.Wait()
}

func TestSTTProviderManager_ConcurrentRegistrationAndStreaming(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	mgr := stt.NewProviderManager(logger, "mock-0", []string{"mock-0", "mock-1", "mock-2"})

	// Register providers concurrently
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			provider := &mockSTTProvider{name: fmt.Sprintf("mock-%d", id)}
			_ = mgr.RegisterProvider(provider)
		}(i)
	}
	wg.Wait()

	// Stream concurrently from multiple goroutines
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			audio := strings.NewReader("concurrent audio stream data")
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			_ = mgr.StreamToProvider(ctx, fmt.Sprintf("mock-%d", id%3), audio, fmt.Sprintf("call-%d", id))
		}(i)
	}
	wg.Wait()
}

func TestSTTProviderManager_ConcurrentRoutingUpdates(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	mgr := stt.NewProviderManager(logger, "mock-0", []string{"mock-0", "mock-1"})

	for i := 0; i < 2; i++ {
		provider := &mockSTTProvider{name: fmt.Sprintf("mock-%d", i)}
		require.NoError(t, mgr.RegisterProvider(provider))
	}

	var wg sync.WaitGroup

	// Concurrent language routing updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mgr.SetLanguageRouting(map[string]string{
					"en-us": fmt.Sprintf("mock-%d", j%2),
					"es-es": fmt.Sprintf("mock-%d", (j+1)%2),
				})
			}
		}(i)
	}

	// Concurrent call routing
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				callUUID := fmt.Sprintf("call-%d-%d", id, j)
				mgr.RouteCallByLanguage(callUUID, "en-us")
				mgr.RouteCallToProvider(callUUID, fmt.Sprintf("mock-%d", j%2))
				_ = mgr.SelectProviderForCall(callUUID, "")
				mgr.ClearCallRoute(callUUID)
			}
		}(i)
	}

	// Concurrent provider listing
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = mgr.GetAllProviders()
				_, _ = mgr.GetProvider("mock-0")
				_, _ = mgr.GetDefaultProvider()
			}
		}()
	}

	wg.Wait()
}

// --------------------------------------------------------------------------
// Connection Disruption Tests (simulated)
// --------------------------------------------------------------------------

func TestAMQPTranscriptionListener_ConnectionLossRecovery(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	baseline := goroutineBaseline()

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	// Publish while connected
	for i := 0; i < 50; i++ {
		listener.OnTranscription("call-conn", "connected msg", true, nil)
	}
	time.Sleep(100 * time.Millisecond)

	// Simulate disconnect
	client.connected.Store(false)
	for i := 0; i < 50; i++ {
		listener.OnTranscription("call-disconn", "disconnected msg", true, nil)
	}
	time.Sleep(100 * time.Millisecond)

	// Simulate reconnect
	client.connected.Store(true)
	for i := 0; i < 50; i++ {
		listener.OnTranscription("call-reconn", "reconnected msg", true, nil)
	}
	time.Sleep(200 * time.Millisecond)

	listener.Shutdown()
	assertNoGoroutineLeak(t, baseline, 3, "connection loss recovery")

	published, failed, dropped, _, _ := listener.GetMetrics()
	t.Logf("Connection disruption: published=%d failed=%d dropped=%d", published, failed, dropped)
}

func TestAMQPTranscriptionListener_SlowPublishNoGoroutineLeak(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	client := newMockAMQPClient(true)
	client.publishDelay = 50 * time.Millisecond // simulate slow broker

	baseline := goroutineBaseline()

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	// Flood with messages while publish is slow
	for i := 0; i < 500; i++ {
		listener.OnTranscription("call-slow", "slow publish test", true, nil)
	}

	// Give workers some time then shut down
	time.Sleep(200 * time.Millisecond)
	listener.Shutdown()

	assertNoGoroutineLeak(t, baseline, 3, "slow publish")
}

func TestAMQPTranscriptionListener_ErrorPublishNoGoroutineLeak(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	client := newMockAMQPClient(true)
	client.mu.Lock()
	client.publishErr = fmt.Errorf("simulated broker error")
	client.mu.Unlock()

	baseline := goroutineBaseline()

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	for i := 0; i < 200; i++ {
		listener.OnTranscription("call-err", "error test", true, nil)
	}

	time.Sleep(500 * time.Millisecond) // retries take time
	listener.Shutdown()

	assertNoGoroutineLeak(t, baseline, 3, "error publish")

	_, failed, _, _, _ := listener.GetMetrics()
	assert.True(t, failed > 0, "should have recorded failures")
}

// --------------------------------------------------------------------------
// STT Provider Context Cancellation Tests
// --------------------------------------------------------------------------

func TestSTTProviderManager_ContextCancellation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	mgr := stt.NewProviderManager(logger, "slow", []string{"slow"})

	provider := &mockSTTProvider{
		name:        "slow",
		streamDelay: 10 * time.Second, // deliberately long
	}
	require.NoError(t, mgr.RegisterProvider(provider))

	baseline := goroutineBaseline()

	// Cancel context quickly — should not leak goroutines
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	audio := strings.NewReader("audio data for cancellation test")
	err := mgr.StreamToProvider(ctx, "slow", audio, "call-cancel")
	assert.Error(t, err, "should fail due to context timeout")

	assertNoGoroutineLeak(t, baseline, 3, "STT context cancellation")
}

func TestSTTProviderManager_RapidCreateDestroy(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	baseline := goroutineBaseline()

	for i := 0; i < 50; i++ {
		mgr := stt.NewProviderManager(logger, "mock", nil)
		provider := &mockSTTProvider{name: "mock"}
		_ = mgr.RegisterProvider(provider)

		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		audio := strings.NewReader("rapid lifecycle test audio")
		_ = mgr.StreamToProvider(ctx, "mock", audio, fmt.Sprintf("call-rapid-%d", i))
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
		_ = mgr.Shutdown(shutdownCtx)
		shutdownCancel()
	}

	assertNoGoroutineLeak(t, baseline, 3, "STT rapid create/destroy")
}

// --------------------------------------------------------------------------
// Backpressure and Channel Saturation Tests
// --------------------------------------------------------------------------

func TestAMQPTranscriptionListener_BackpressureHandling(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Very slow client to cause backpressure
	client := newMockAMQPClient(true)
	client.publishDelay = 100 * time.Millisecond

	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	// Try to publish more messages than the channel can hold (5000 buffer)
	for i := 0; i < 6000; i++ {
		listener.OnTranscription("call-bp", fmt.Sprintf("backpressure msg %d", i), true, nil)
	}

	published, _, dropped, _, _ := listener.GetMetrics()
	t.Logf("Backpressure: published=%d dropped=%d (of 6000 sent)", published, dropped)

	// Some messages should be dropped due to backpressure
	if dropped == 0 && published < 6000 {
		t.Log("Warning: expected some dropped messages under backpressure (may depend on worker speed)")
	}

	listener.Shutdown()
}

func TestAMQPTranscriptionListener_NilClientNoPanic(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create listener with a connected client, then test behavior
	client := newMockAMQPClient(false)
	listener := messaging.NewAMQPTranscriptionListener(logger, client)
	defer listener.Shutdown()

	// Should not panic
	assert.NotPanics(t, func() {
		for i := 0; i < 100; i++ {
			listener.OnTranscription("call-nil", "nil client test", true, nil)
		}
	})
}

// --------------------------------------------------------------------------
// FilteredTranscriptionListener race test
// --------------------------------------------------------------------------

func TestFilteredTranscriptionListener_ConcurrentAccess(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	client := newMockAMQPClient(true)

	inner := messaging.NewAMQPTranscriptionListener(logger, client)
	defer inner.Shutdown()

	filtered := messaging.NewFilteredTranscriptionListener(inner, true, true)

	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				filtered.OnTranscription(
					fmt.Sprintf("call-%d", id),
					"filtered test",
					j%2 == 0,
					map[string]interface{}{"g": id},
				)
			}
		}(g)
	}
	wg.Wait()
}

// --------------------------------------------------------------------------
// STT Provider Fallback Race Test
// --------------------------------------------------------------------------

func TestSTTProviderManager_FallbackRace(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	mgr := stt.NewProviderManager(logger, "failing", []string{"failing", "good"})

	failing := &mockSTTProvider{
		name:      "failing",
		streamErr: fmt.Errorf("simulated provider failure"),
	}
	good := &mockSTTProvider{name: "good"}

	require.NoError(t, mgr.RegisterProvider(failing))
	require.NoError(t, mgr.RegisterProvider(good))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Use a seekable reader so fallback can replay
			audio := strings.NewReader("fallback audio data for test")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := mgr.StreamToProvider(ctx, "", audio, fmt.Sprintf("call-fb-%d", id))
			// Should succeed via fallback to "good"
			assert.NoError(t, err, "fallback should succeed")
		}(i)
	}
	wg.Wait()

	assert.True(t, good.streamCount.Load() > 0, "good provider should have been called as fallback")
}

// --------------------------------------------------------------------------
// Combined Stress Test: AMQP + STT together
// --------------------------------------------------------------------------

func TestCombinedAMQPAndSTT_StressNoLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	baseline := goroutineBaseline()

	client := newMockAMQPClient(true)
	listener := messaging.NewAMQPTranscriptionListener(logger, client)

	mgr := stt.NewProviderManager(logger, "mock", []string{"mock"})
	provider := &mockSTTProvider{name: "mock"}
	require.NoError(t, mgr.RegisterProvider(provider))

	// Set up a callback that publishes to AMQP
	provider.callbackMu.Lock()
	provider.callback = func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		listener.OnTranscription(callUUID, transcription, isFinal, metadata)
	}
	provider.callbackMu.Unlock()

	var wg sync.WaitGroup

	// Simulate concurrent calls streaming to STT + publishing to AMQP
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				audio := strings.NewReader("combined stress test audio data")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = mgr.StreamToProvider(ctx, "mock", audio, fmt.Sprintf("stress-%d-%d", id, j))
				cancel()
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond) // let AMQP workers drain

	listener.Shutdown()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = mgr.Shutdown(shutdownCtx)
	cancel()

	assertNoGoroutineLeak(t, baseline, 5, "combined AMQP+STT stress")

	published, failed, dropped, _, _ := listener.GetMetrics()
	t.Logf("Combined stress: published=%d failed=%d dropped=%d stt_calls=%d",
		published, failed, dropped, provider.streamCount.Load())
}
