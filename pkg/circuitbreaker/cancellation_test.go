package circuitbreaker

import (
	"context"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
	"time"
)

func TestCallerCancellationDoesNotOpenCircuit(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := DefaultConfig()
	cfg.FailureThreshold = 1
	cb := NewCircuitBreaker("test", cfg, logger)
	ctx, cancel := context.WithCancel(context.Background())
	err := cb.Execute(ctx, func(context.Context) error { cancel(); return ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, StateClosed, cb.GetState())
}
func TestOwnTimeoutStillOpensCircuit(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := DefaultConfig()
	cfg.FailureThreshold = 1
	cfg.RequestTimeout = time.Millisecond
	cb := NewCircuitBreaker("test", cfg, logger)
	err := cb.Execute(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, StateOpen, cb.GetState())
}

func TestPreCanceledCallDoesNotExecute(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cb := NewCircuitBreaker("test", DefaultConfig(), logger)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, cb.Execute(ctx, func(context.Context) error { t.Fatal("canceled request executed"); return nil }), context.Canceled)
}
