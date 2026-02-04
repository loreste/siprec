package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	rateLimitKeyPrefix = "siprec:ratelimit:"
)

// DistributedRateLimiter provides cluster-wide rate limiting using Redis
type DistributedRateLimiter struct {
	redis    redis.UniversalClient
	logger   *logrus.Logger
	config   RateLimitConfig
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Local metrics
	metrics     *rateLimitMetrics
	metricsMu   sync.RWMutex
}

// RateLimitConfig holds rate limiter configuration
type RateLimitConfig struct {
	// Global limits (across all nodes)
	GlobalCallsPerSecond    int `json:"global_calls_per_second" default:"1000"`
	GlobalCallsPerMinute    int `json:"global_calls_per_minute" default:"50000"`
	GlobalBytesPerSecond    int64 `json:"global_bytes_per_second" default:"104857600"` // 100MB/s

	// Per-IP limits
	PerIPCallsPerSecond     int `json:"per_ip_calls_per_second" default:"10"`
	PerIPCallsPerMinute     int `json:"per_ip_calls_per_minute" default:"100"`

	// Per-user limits (if authentication is used)
	PerUserCallsPerSecond   int `json:"per_user_calls_per_second" default:"50"`
	PerUserCallsPerMinute   int `json:"per_user_calls_per_minute" default:"500"`

	// Burst allowance (percentage above limit for short bursts)
	BurstAllowance          float64 `json:"burst_allowance" default:"1.2"` // 20% burst

	// Window sizes
	ShortWindow             time.Duration `json:"short_window" default:"1s"`
	LongWindow              time.Duration `json:"long_window" default:"1m"`

	// Cleanup interval for expired keys
	CleanupInterval         time.Duration `json:"cleanup_interval" default:"5m"`
}

type rateLimitMetrics struct {
	TotalRequests     int64
	AllowedRequests   int64
	RejectedRequests  int64
	GlobalLimitHits   int64
	PerIPLimitHits    int64
	PerUserLimitHits  int64
}

// RateLimitResult contains the result of a rate limit check
type RateLimitResult struct {
	Allowed       bool          `json:"allowed"`
	Remaining     int64         `json:"remaining"`
	Limit         int64         `json:"limit"`
	ResetIn       time.Duration `json:"reset_in"`
	LimitType     string        `json:"limit_type,omitempty"` // "global", "ip", "user"
	RetryAfter    time.Duration `json:"retry_after,omitempty"`
}

// NewDistributedRateLimiter creates a new distributed rate limiter
func NewDistributedRateLimiter(redisClient redis.UniversalClient, config RateLimitConfig, logger *logrus.Logger) *DistributedRateLimiter {
	if config.GlobalCallsPerSecond == 0 {
		config.GlobalCallsPerSecond = 1000
	}
	if config.GlobalCallsPerMinute == 0 {
		config.GlobalCallsPerMinute = 50000
	}
	if config.PerIPCallsPerSecond == 0 {
		config.PerIPCallsPerSecond = 10
	}
	if config.PerIPCallsPerMinute == 0 {
		config.PerIPCallsPerMinute = 100
	}
	if config.BurstAllowance == 0 {
		config.BurstAllowance = 1.2
	}
	if config.ShortWindow == 0 {
		config.ShortWindow = time.Second
	}
	if config.LongWindow == 0 {
		config.LongWindow = time.Minute
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 5 * time.Minute
	}

	return &DistributedRateLimiter{
		redis:    redisClient,
		logger:   logger,
		config:   config,
		stopChan: make(chan struct{}),
		metrics:  &rateLimitMetrics{},
	}
}

// Start begins the rate limiter background tasks
func (r *DistributedRateLimiter) Start() {
	r.wg.Add(1)
	go r.cleanupLoop()
	r.logger.WithFields(logrus.Fields{
		"global_cps": r.config.GlobalCallsPerSecond,
		"global_cpm": r.config.GlobalCallsPerMinute,
		"per_ip_cps": r.config.PerIPCallsPerSecond,
	}).Info("Distributed rate limiter started")
}

// Stop stops the rate limiter
func (r *DistributedRateLimiter) Stop() {
	close(r.stopChan)
	r.wg.Wait()
	r.logger.Info("Distributed rate limiter stopped")
}

// AllowCall checks if a call is allowed based on global and per-IP limits
func (r *DistributedRateLimiter) AllowCall(ctx context.Context, remoteIP string) *RateLimitResult {
	r.metricsMu.Lock()
	r.metrics.TotalRequests++
	r.metricsMu.Unlock()

	// Check global limit first
	globalResult := r.checkGlobalLimit(ctx)
	if !globalResult.Allowed {
		r.metricsMu.Lock()
		r.metrics.RejectedRequests++
		r.metrics.GlobalLimitHits++
		r.metricsMu.Unlock()
		return globalResult
	}

	// Check per-IP limit
	if remoteIP != "" {
		ipResult := r.checkIPLimit(ctx, remoteIP)
		if !ipResult.Allowed {
			r.metricsMu.Lock()
			r.metrics.RejectedRequests++
			r.metrics.PerIPLimitHits++
			r.metricsMu.Unlock()
			return ipResult
		}
	}

	r.metricsMu.Lock()
	r.metrics.AllowedRequests++
	r.metricsMu.Unlock()

	return &RateLimitResult{
		Allowed:   true,
		Remaining: globalResult.Remaining,
		Limit:     globalResult.Limit,
		ResetIn:   globalResult.ResetIn,
	}
}

// AllowCallWithUser checks limits including user-based limits
func (r *DistributedRateLimiter) AllowCallWithUser(ctx context.Context, remoteIP, userID string) *RateLimitResult {
	// First check global and IP limits
	result := r.AllowCall(ctx, remoteIP)
	if !result.Allowed {
		return result
	}

	// Check user limit if provided
	if userID != "" {
		userResult := r.checkUserLimit(ctx, userID)
		if !userResult.Allowed {
			r.metricsMu.Lock()
			r.metrics.RejectedRequests++
			r.metrics.AllowedRequests-- // Undo the increment from AllowCall
			r.metrics.PerUserLimitHits++
			r.metricsMu.Unlock()
			return userResult
		}
	}

	return result
}

// checkGlobalLimit checks the global rate limit
func (r *DistributedRateLimiter) checkGlobalLimit(ctx context.Context) *RateLimitResult {
	key := rateLimitKeyPrefix + "global"
	limit := int64(float64(r.config.GlobalCallsPerSecond) * r.config.BurstAllowance)

	return r.checkSlidingWindowLimit(ctx, key, limit, r.config.ShortWindow, "global")
}

// checkIPLimit checks the per-IP rate limit
func (r *DistributedRateLimiter) checkIPLimit(ctx context.Context, ip string) *RateLimitResult {
	key := rateLimitKeyPrefix + "ip:" + ip
	limit := int64(float64(r.config.PerIPCallsPerSecond) * r.config.BurstAllowance)

	return r.checkSlidingWindowLimit(ctx, key, limit, r.config.ShortWindow, "ip")
}

// checkUserLimit checks the per-user rate limit
func (r *DistributedRateLimiter) checkUserLimit(ctx context.Context, userID string) *RateLimitResult {
	key := rateLimitKeyPrefix + "user:" + userID
	limit := int64(float64(r.config.PerUserCallsPerSecond) * r.config.BurstAllowance)

	return r.checkSlidingWindowLimit(ctx, key, limit, r.config.ShortWindow, "user")
}

// checkSlidingWindowLimit implements sliding window rate limiting using Redis
func (r *DistributedRateLimiter) checkSlidingWindowLimit(ctx context.Context, key string, limit int64, window time.Duration, limitType string) *RateLimitResult {
	now := time.Now()
	windowStart := now.Add(-window).UnixNano()
	nowNano := now.UnixNano()

	// Use Redis sorted set with timestamps as scores
	// Lua script for atomic sliding window rate limiting
	script := redis.NewScript(`
		local key = KEYS[1]
		local now = tonumber(ARGV[1])
		local window_start = tonumber(ARGV[2])
		local limit = tonumber(ARGV[3])
		local ttl = tonumber(ARGV[4])

		-- Remove old entries outside the window
		redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

		-- Count current entries
		local count = redis.call('ZCARD', key)

		if count < limit then
			-- Add new entry with current timestamp as score
			redis.call('ZADD', key, now, now .. ':' .. math.random(1000000))
			-- Set TTL on the key
			redis.call('PEXPIRE', key, ttl)
			return {1, limit - count - 1, 0}
		else
			-- Get oldest entry to calculate retry time
			local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
			local retry_after = 0
			if #oldest >= 2 then
				retry_after = oldest[2] - window_start
			end
			return {0, 0, retry_after}
		end
	`)

	result, err := script.Run(ctx, r.redis, []string{key}, nowNano, windowStart, limit, window.Milliseconds()).Result()
	if err != nil {
		r.logger.WithError(err).Error("Rate limit check failed, allowing request")
		return &RateLimitResult{
			Allowed:   true,
			Remaining: limit,
			Limit:     limit,
			ResetIn:   window,
		}
	}

	values := result.([]interface{})
	allowed := values[0].(int64) == 1
	remaining := values[1].(int64)
	retryAfterNano := values[2].(int64)

	retryAfter := time.Duration(0)
	if retryAfterNano > 0 {
		retryAfter = time.Duration(retryAfterNano)
	}

	return &RateLimitResult{
		Allowed:    allowed,
		Remaining:  remaining,
		Limit:      limit,
		ResetIn:    window,
		LimitType:  limitType,
		RetryAfter: retryAfter,
	}
}

// GetBandwidthLimit checks bandwidth rate limit
func (r *DistributedRateLimiter) GetBandwidthLimit(ctx context.Context, bytes int64) *RateLimitResult {
	key := rateLimitKeyPrefix + "bandwidth"

	script := redis.NewScript(`
		local key = KEYS[1]
		local bytes = tonumber(ARGV[1])
		local limit = tonumber(ARGV[2])
		local window = tonumber(ARGV[3])

		local current = redis.call('INCRBY', key, bytes)
		if current == bytes then
			redis.call('PEXPIRE', key, window)
		end

		if current <= limit then
			return {1, limit - current}
		else
			return {0, 0}
		end
	`)

	result, err := script.Run(ctx, r.redis, []string{key}, bytes, r.config.GlobalBytesPerSecond, r.config.ShortWindow.Milliseconds()).Result()
	if err != nil {
		return &RateLimitResult{Allowed: true}
	}

	values := result.([]interface{})
	allowed := values[0].(int64) == 1
	remaining := values[1].(int64)

	return &RateLimitResult{
		Allowed:   allowed,
		Remaining: remaining,
		Limit:     r.config.GlobalBytesPerSecond,
		ResetIn:   r.config.ShortWindow,
		LimitType: "bandwidth",
	}
}

// ResetIPLimit resets the rate limit for a specific IP
func (r *DistributedRateLimiter) ResetIPLimit(ctx context.Context, ip string) error {
	key := rateLimitKeyPrefix + "ip:" + ip
	return r.redis.Del(ctx, key).Err()
}

// ResetUserLimit resets the rate limit for a specific user
func (r *DistributedRateLimiter) ResetUserLimit(ctx context.Context, userID string) error {
	key := rateLimitKeyPrefix + "user:" + userID
	return r.redis.Del(ctx, key).Err()
}

// GetMetrics returns rate limiter metrics
func (r *DistributedRateLimiter) GetMetrics() map[string]interface{} {
	r.metricsMu.RLock()
	defer r.metricsMu.RUnlock()

	return map[string]interface{}{
		"total_requests":     r.metrics.TotalRequests,
		"allowed_requests":   r.metrics.AllowedRequests,
		"rejected_requests":  r.metrics.RejectedRequests,
		"global_limit_hits":  r.metrics.GlobalLimitHits,
		"per_ip_limit_hits":  r.metrics.PerIPLimitHits,
		"per_user_limit_hits": r.metrics.PerUserLimitHits,
	}
}

// GetCurrentUsage returns current usage statistics
func (r *DistributedRateLimiter) GetCurrentUsage(ctx context.Context) (*RateLimitUsage, error) {
	usage := &RateLimitUsage{}

	// Get global usage
	globalKey := rateLimitKeyPrefix + "global"
	globalCount, err := r.redis.ZCard(ctx, globalKey).Result()
	if err == nil {
		usage.GlobalCallsPerSecond = globalCount
	}

	// Count IPs with active limits
	ipPattern := rateLimitKeyPrefix + "ip:*"
	ipKeys, err := r.redis.Keys(ctx, ipPattern).Result()
	if err == nil {
		usage.ActiveIPs = int64(len(ipKeys))
	}

	// Count users with active limits
	userPattern := rateLimitKeyPrefix + "user:*"
	userKeys, err := r.redis.Keys(ctx, userPattern).Result()
	if err == nil {
		usage.ActiveUsers = int64(len(userKeys))
	}

	usage.Limits = RateLimitLimits{
		GlobalCallsPerSecond: int64(r.config.GlobalCallsPerSecond),
		PerIPCallsPerSecond:  int64(r.config.PerIPCallsPerSecond),
		PerUserCallsPerSecond: int64(r.config.PerUserCallsPerSecond),
	}

	return usage, nil
}

// RateLimitUsage contains current rate limit usage
type RateLimitUsage struct {
	GlobalCallsPerSecond int64           `json:"global_calls_per_second"`
	ActiveIPs            int64           `json:"active_ips"`
	ActiveUsers          int64           `json:"active_users"`
	Limits               RateLimitLimits `json:"limits"`
}

// RateLimitLimits contains configured limits
type RateLimitLimits struct {
	GlobalCallsPerSecond  int64 `json:"global_calls_per_second"`
	PerIPCallsPerSecond   int64 `json:"per_ip_calls_per_second"`
	PerUserCallsPerSecond int64 `json:"per_user_calls_per_second"`
}

// cleanupLoop periodically cleans up expired rate limit keys
func (r *DistributedRateLimiter) cleanupLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

// cleanup removes expired rate limit entries
func (r *DistributedRateLimiter) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find all rate limit keys
	pattern := rateLimitKeyPrefix + "*"
	keys, err := r.redis.Keys(ctx, pattern).Result()
	if err != nil {
		r.logger.WithError(err).Error("Failed to list rate limit keys for cleanup")
		return
	}

	// Clean up each key's expired entries
	windowStart := time.Now().Add(-r.config.LongWindow).UnixNano()

	pipe := r.redis.Pipeline()
	for _, key := range keys {
		pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", windowStart))
	}

	if _, err := pipe.Exec(ctx); err != nil {
		r.logger.WithError(err).Error("Failed to cleanup expired rate limit entries")
	}
}

// BlockIP adds an IP to the blocklist
func (r *DistributedRateLimiter) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	key := rateLimitKeyPrefix + "blocked:" + ip
	return r.redis.Set(ctx, key, "1", duration).Err()
}

// UnblockIP removes an IP from the blocklist
func (r *DistributedRateLimiter) UnblockIP(ctx context.Context, ip string) error {
	key := rateLimitKeyPrefix + "blocked:" + ip
	return r.redis.Del(ctx, key).Err()
}

// IsIPBlocked checks if an IP is blocked
func (r *DistributedRateLimiter) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	key := rateLimitKeyPrefix + "blocked:" + ip
	exists, err := r.redis.Exists(ctx, key).Result()
	return exists > 0, err
}
