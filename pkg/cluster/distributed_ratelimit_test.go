package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func newTestRateLimiter(t *testing.T, client redis.UniversalClient, globalCPS int) *DistributedRateLimiter {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	cfg := RateLimitConfig{
		GlobalCallsPerSecond: globalCPS,
		GlobalCallsPerMinute: 50000,
		PerIPCallsPerSecond:  10,
		PerIPCallsPerMinute:  100,
		BurstAllowance:       1.0, // no burst so limits are exact
		ShortWindow:          time.Second,
		LongWindow:           time.Minute,
		CleanupInterval:      5 * time.Minute,
	}
	return NewDistributedRateLimiter(client, cfg, logger)
}

func TestRateLimitAllowCall(t *testing.T) {
	_, client := setupTestRedis(t)
	limiter := newTestRateLimiter(t, client, 100)

	ctx := context.Background()
	result := limiter.AllowCall(ctx, "192.168.1.1")

	require.NotNil(t, result)
	assert.True(t, result.Allowed, "first call should be allowed")
	assert.True(t, result.Remaining >= 0, "remaining should be non-negative")
}

func TestRateLimitExceedGlobal(t *testing.T) {
	_, client := setupTestRedis(t)

	// Set a very low global limit so we can exceed it quickly.
	limiter := newTestRateLimiter(t, client, 3)

	ctx := context.Background()

	var lastResult *RateLimitResult
	rejected := false
	for i := 0; i < 10; i++ {
		lastResult = limiter.AllowCall(ctx, "10.0.0.1")
		if !lastResult.Allowed {
			rejected = true
			break
		}
	}

	assert.True(t, rejected, "expected at least one call to be rejected after exceeding global CPS")
	assert.False(t, lastResult.Allowed)
	assert.Equal(t, "global", lastResult.LimitType)
}

func TestRateLimitGetMetrics(t *testing.T) {
	_, client := setupTestRedis(t)
	limiter := newTestRateLimiter(t, client, 100)

	ctx := context.Background()

	// Make a few calls to generate metrics.
	for i := 0; i < 5; i++ {
		limiter.AllowCall(ctx, "10.0.0.2")
	}

	metrics := limiter.GetMetrics()
	require.NotNil(t, metrics)

	assert.Equal(t, int64(5), metrics["total_requests"])
	assert.Equal(t, int64(5), metrics["allowed_requests"])
	assert.Equal(t, int64(0), metrics["rejected_requests"])
	assert.Equal(t, int64(0), metrics["global_limit_hits"])
	assert.Equal(t, int64(0), metrics["per_ip_limit_hits"])
	assert.Equal(t, int64(0), metrics["per_user_limit_hits"])
}

func TestRateLimitConcurrentAccess(t *testing.T) {
	_, client := setupTestRedis(t)
	limiter := newTestRateLimiter(t, client, 1000)

	ctx := context.Background()
	const goroutines = 20
	const callsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	var mu sync.Mutex
	var allowed, rejected int

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				result := limiter.AllowCall(ctx, "10.0.0.3")
				mu.Lock()
				if result.Allowed {
					allowed++
				} else {
					rejected++
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	totalCalls := goroutines * callsPerGoroutine
	assert.Equal(t, totalCalls, allowed+rejected, "all calls should be accounted for")

	metrics := limiter.GetMetrics()
	assert.Equal(t, int64(totalCalls), metrics["total_requests"])
	assert.Equal(t, int64(allowed), metrics["allowed_requests"])
	assert.Equal(t, int64(rejected), metrics["rejected_requests"])
}
