package ratelimit

import (
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func TestLimiter_Allow_WithinBurst(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	// Should allow up to burst size initially
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow("client1"), "Request %d should be allowed", i+1)
	}

	// 6th request should be denied (no tokens left)
	assert.False(t, limiter.Allow("client1"), "6th request should be denied")
}

func TestLimiter_Allow_TokenRefill(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	// Consume all tokens
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow("client1"))
	}
	assert.False(t, limiter.Allow("client1"))

	// Wait for tokens to refill (100ms = 1 token at 10/sec)
	time.Sleep(150 * time.Millisecond)

	// Should have 1 token now
	assert.True(t, limiter.Allow("client1"))
	assert.False(t, limiter.Allow("client1"))
}

func TestLimiter_Allow_DifferentClients(t *testing.T) {
	limiter := NewLimiter(10, 3, newTestLogger())

	// Client 1 consumes all tokens
	for i := 0; i < 3; i++ {
		assert.True(t, limiter.Allow("client1"))
	}
	assert.False(t, limiter.Allow("client1"))

	// Client 2 should have its own bucket
	for i := 0; i < 3; i++ {
		assert.True(t, limiter.Allow("client2"))
	}
	assert.False(t, limiter.Allow("client2"))
}

func TestLimiter_Block(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	// Block client
	limiter.Block("client1", 100*time.Millisecond)

	// Should be blocked
	assert.True(t, limiter.IsBlocked("client1"))
	assert.False(t, limiter.Allow("client1"))

	// Wait for block to expire
	time.Sleep(150 * time.Millisecond)

	// Should be unblocked
	assert.False(t, limiter.IsBlocked("client1"))
	assert.True(t, limiter.Allow("client1"))
}

func TestLimiter_AllowN(t *testing.T) {
	limiter := NewLimiter(10, 10, newTestLogger())

	// Should allow batch of 5
	assert.True(t, limiter.AllowN("client1", 5))

	// Should allow another batch of 5
	assert.True(t, limiter.AllowN("client1", 5))

	// Should deny batch of 1 (no tokens left)
	assert.False(t, limiter.AllowN("client1", 1))
}

func TestLimiter_GetTokens(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	// New client should have full tokens
	tokens := limiter.GetTokens("newclient")
	assert.Equal(t, float64(5), tokens)

	// After one request, should have 4 tokens
	limiter.Allow("newclient")
	tokens = limiter.GetTokens("newclient")
	assert.True(t, tokens >= 4 && tokens <= 5, "Expected tokens between 4 and 5, got %f", tokens)
}

func TestLimiter_GetClientCount(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	assert.Equal(t, 0, limiter.GetClientCount())

	limiter.Allow("client1")
	assert.Equal(t, 1, limiter.GetClientCount())

	limiter.Allow("client2")
	assert.Equal(t, 2, limiter.GetClientCount())

	limiter.Allow("client1") // Same client
	assert.Equal(t, 2, limiter.GetClientCount())
}

func TestLimiter_Reset(t *testing.T) {
	limiter := NewLimiter(10, 5, newTestLogger())

	limiter.Allow("client1")
	limiter.Allow("client2")
	assert.Equal(t, 2, limiter.GetClientCount())

	limiter.Reset()
	assert.Equal(t, 0, limiter.GetClientCount())
}

func TestLimiter_Concurrent(t *testing.T) {
	limiter := NewLimiter(1000, 100, newTestLogger())

	var wg sync.WaitGroup
	allowed := make(chan int, 200)

	// Launch 200 concurrent requests
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if limiter.Allow("client1") {
				allowed <- id
			}
		}(i)
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests
	count := 0
	for range allowed {
		count++
	}

	// Should allow exactly burst size initially
	assert.Equal(t, 100, count, "Expected exactly 100 requests allowed (burst size)")
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.False(t, config.Enabled)
	assert.Equal(t, float64(100), config.RequestsPerSecond)
	assert.Equal(t, 200, config.BurstSize)
	assert.Equal(t, time.Minute, config.BlockDuration)
	assert.Equal(t, 5*time.Minute, config.CleanupInterval)
	assert.Contains(t, config.WhitelistedIPs, "127.0.0.1")
	assert.Contains(t, config.WhitelistedPaths, "/health")
}

func TestSIPLimiter_AllowRequest(t *testing.T) {
	config := &Config{
		SIPEnabled:           true,
		SIPInvitesPerSecond:  5,
		SIPInviteBurst:       3,
		SIPRequestsPerSecond: 100,
		SIPRequestBurst:      50,
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	// INVITE requests should use stricter limits
	for i := 0; i < 3; i++ {
		assert.True(t, limiter.AllowRequest("192.168.1.1", "INVITE"), "INVITE %d should be allowed", i+1)
	}
	assert.False(t, limiter.AllowRequest("192.168.1.1", "INVITE"), "4th INVITE should be denied")

	// Other requests should use general limits
	for i := 0; i < 50; i++ {
		assert.True(t, limiter.AllowRequest("192.168.1.2", "BYE"), "BYE %d should be allowed", i+1)
	}
}

func TestSIPLimiter_Whitelist(t *testing.T) {
	config := &Config{
		SIPEnabled:          true,
		SIPInvitesPerSecond: 1,
		SIPInviteBurst:      1,
		WhitelistedIPs:      []string{"10.0.0.1", "192.168.0.0/16"},
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	// Whitelisted IP should bypass rate limiting
	for i := 0; i < 10; i++ {
		assert.True(t, limiter.AllowRequest("10.0.0.1", "INVITE"), "Whitelisted IP should always be allowed")
	}

	// Whitelisted network should bypass rate limiting
	for i := 0; i < 10; i++ {
		assert.True(t, limiter.AllowRequest("192.168.1.100", "INVITE"), "IP in whitelisted network should always be allowed")
	}
}

func TestSIPLimiter_BlockClient(t *testing.T) {
	config := &Config{
		SIPEnabled:           true,
		SIPInvitesPerSecond:  10,
		SIPInviteBurst:       10,
		SIPRequestsPerSecond: 100,
		SIPRequestBurst:      100,
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	// Block client
	limiter.BlockClient("192.168.1.1", 100*time.Millisecond)

	// Should be blocked
	assert.True(t, limiter.IsBlocked("192.168.1.1"))
	assert.False(t, limiter.AllowRequest("192.168.1.1", "INVITE"))
	assert.False(t, limiter.AllowRequest("192.168.1.1", "BYE"))

	// Other clients should not be blocked
	assert.True(t, limiter.AllowRequest("192.168.1.2", "INVITE"))

	// Wait for block to expire
	time.Sleep(150 * time.Millisecond)
	assert.False(t, limiter.IsBlocked("192.168.1.1"))
}

func TestSIPLimiter_MetricsCallback(t *testing.T) {
	config := &Config{
		SIPEnabled:          true,
		SIPInvitesPerSecond: 10,
		SIPInviteBurst:      2,
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	var mu sync.Mutex
	calls := make([]struct {
		clientIP string
		method   SIPMethod
		allowed  bool
	}, 0)

	limiter.SetMetricsCallback(func(clientIP string, method SIPMethod, allowed bool) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, struct {
			clientIP string
			method   SIPMethod
			allowed  bool
		}{clientIP, method, allowed})
	})

	// Make some requests
	limiter.AllowRequest("192.168.1.1", "INVITE")
	limiter.AllowRequest("192.168.1.1", "INVITE")
	limiter.AllowRequest("192.168.1.1", "INVITE") // Should be denied

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 3)
	assert.True(t, calls[0].allowed)
	assert.True(t, calls[1].allowed)
	assert.False(t, calls[2].allowed)
}

func TestSIPLimiter_GetStats(t *testing.T) {
	config := &Config{
		SIPEnabled:           true,
		SIPInvitesPerSecond:  10,
		SIPInviteBurst:       5,
		SIPRequestsPerSecond: 100,
		SIPRequestBurst:      50,
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	// Make some requests
	limiter.AllowRequest("192.168.1.1", "INVITE")
	limiter.AllowRequest("192.168.1.2", "BYE")

	stats := limiter.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, 1, stats["invite_clients"])
	assert.Equal(t, 1, stats["request_clients"])
}

func TestSIPLimiter_Disabled(t *testing.T) {
	config := &Config{
		SIPEnabled:          false,
		SIPInvitesPerSecond: 1,
		SIPInviteBurst:      1,
	}

	limiter := NewSIPLimiter(config, newTestLogger())

	// Should allow all requests when disabled
	for i := 0; i < 100; i++ {
		assert.True(t, limiter.AllowRequest("192.168.1.1", "INVITE"), "All requests should be allowed when rate limiting is disabled")
	}
}
