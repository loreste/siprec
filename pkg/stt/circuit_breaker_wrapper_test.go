package stt

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/circuitbreaker"
)

// fakeProvider is a minimal STT provider for testing the circuit breaker wrapper.
type fakeProvider struct {
	name        string
	streamFunc  func(ctx context.Context, audio io.Reader, callUUID string) error
	initialized bool
}

func (f *fakeProvider) Initialize() error { f.initialized = true; return nil }
func (f *fakeProvider) Name() string      { return f.name }
func (f *fakeProvider) StreamToText(ctx context.Context, audio io.Reader, callUUID string) error {
	if f.streamFunc != nil {
		return f.streamFunc(ctx, audio, callUUID)
	}
	return nil
}

// TestCircuitBreakerWrapper_StreamSurvivesRequestTimeout reproduces issue #47:
// a live-call stream that outlasts the circuit breaker's RequestTimeout must
// NOT be killed by context cancellation.
func TestCircuitBreakerWrapper_StreamSurvivesRequestTimeout(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Circuit breaker with a very short RequestTimeout to simulate the bug.
	cbCfg := &circuitbreaker.Config{
		FailureThreshold:     5,
		SuccessThreshold:     2,
		Timeout:              time.Second,
		MaxTimeout:           5 * time.Second,
		RequestTimeout:       200 * time.Millisecond, // would kill the stream before the fix
		ExponentialBackoff:   false,
		FailureRateThreshold: 0.5,
		MinRequestThreshold:  10,
		TimeWindow:           time.Minute,
	}

	cbManager := circuitbreaker.NewManager(logger, cbCfg)

	var ctxErr atomic.Value // captures the context error seen by the provider

	provider := &fakeProvider{
		name: "slow-stream",
		streamFunc: func(ctx context.Context, _ io.Reader, _ string) error {
			// Simulate a stream that runs longer than RequestTimeout.
			select {
			case <-time.After(600 * time.Millisecond):
				ctxErr.Store("") // no error — stream completed
				return nil
			case <-ctx.Done():
				ctxErr.Store(ctx.Err().Error())
				return ctx.Err()
			}
		},
	}

	wrapper := NewCircuitBreakerWrapper(provider, cbManager, logger, nil)

	// Use a background context with no deadline, just like a live SIP call.
	err := wrapper.StreamToText(context.Background(), nil, "call-issue-47")

	require.NoError(t, err, "stream should complete without being killed by RequestTimeout")

	stored := ctxErr.Load()
	if stored != nil {
		assert.Empty(t, stored.(string), "provider should not have observed context cancellation")
	}

	// Circuit breaker should have recorded a success.
	stats := wrapper.GetCircuitBreakerStats()
	assert.Equal(t, int64(1), stats.SuccessfulRequests)
	assert.Equal(t, int64(0), stats.FailedRequests)
}

// TestCircuitBreakerWrapper_CallContextCancellationStillWorks ensures that
// cancelling the *call* context (e.g. SIP BYE) still propagates correctly.
func TestCircuitBreakerWrapper_CallContextCancellationStillWorks(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cbManager := circuitbreaker.NewManager(logger, nil)

	provider := &fakeProvider{
		name: "waiting-stream",
		streamFunc: func(ctx context.Context, _ io.Reader, _ string) error {
			// Block until context is cancelled (simulates reading from live pipe).
			<-ctx.Done()
			return ctx.Err()
		},
	}

	wrapper := NewCircuitBreakerWrapper(provider, cbManager, logger, nil)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- wrapper.StreamToText(ctx, nil, "call-bye")
	}()

	// Simulate a BYE arriving after a short delay.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("StreamToText did not return after call context was cancelled")
	}
}

// TestCircuitBreakerWrapper_FallbackOnOpen verifies the fallback provider is
// used when the circuit breaker is open. NewCircuitBreakerWrapper hardcodes
// STTConfig (FailureThreshold=3), so we fail 3 times to trip the breaker.
func TestCircuitBreakerWrapper_FallbackOnOpen(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	cbManager := circuitbreaker.NewManager(logger, nil)

	var fallbackCalled atomic.Bool

	primary := &fakeProvider{
		name: "broken",
		streamFunc: func(_ context.Context, _ io.Reader, _ string) error {
			return io.ErrUnexpectedEOF
		},
	}
	fallback := &fakeProvider{
		name: "backup",
		streamFunc: func(_ context.Context, _ io.Reader, _ string) error {
			fallbackCalled.Store(true)
			return nil
		},
	}

	wrapper := NewCircuitBreakerWrapper(primary, cbManager, logger, fallback)

	// STTConfig has FailureThreshold=3; fail enough times to trip the breaker.
	for i := 0; i < 3; i++ {
		err := wrapper.StreamToText(context.Background(), nil, "call-fail")
		require.Error(t, err)
	}

	assert.True(t, wrapper.IsCircuitBreakerOpen(), "circuit breaker should be open after 3 failures")

	// Next call should go to fallback because breaker is open.
	err := wrapper.StreamToText(context.Background(), nil, "call-fallback")
	require.NoError(t, err)
	assert.True(t, fallbackCalled.Load(), "fallback provider should have been called")
}
