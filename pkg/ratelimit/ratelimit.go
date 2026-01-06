package ratelimit

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Limiter implements a token bucket rate limiter with per-key tracking
type Limiter struct {
	rate       float64       // tokens per second
	burst      int           // maximum burst size
	clients    map[string]*bucket
	mu         sync.RWMutex
	logger     *logrus.Logger
	cleanupTTL time.Duration // how long to keep inactive clients
}

// bucket represents a token bucket for a single client
type bucket struct {
	tokens     float64
	lastUpdate time.Time
	blocked    bool
	blockUntil time.Time
}

// Config holds rate limiter configuration
type Config struct {
	// Enabled determines if rate limiting is active
	Enabled bool `json:"enabled" env:"RATE_LIMIT_ENABLED" default:"false"`

	// RequestsPerSecond is the sustained rate of requests allowed per second
	RequestsPerSecond float64 `json:"requests_per_second" env:"RATE_LIMIT_RPS" default:"100"`

	// BurstSize is the maximum number of requests allowed in a burst
	BurstSize int `json:"burst_size" env:"RATE_LIMIT_BURST" default:"200"`

	// BlockDuration is how long to block a client after exceeding limits
	BlockDuration time.Duration `json:"block_duration" env:"RATE_LIMIT_BLOCK_DURATION" default:"1m"`

	// CleanupInterval is how often to clean up stale client entries
	CleanupInterval time.Duration `json:"cleanup_interval" env:"RATE_LIMIT_CLEANUP_INTERVAL" default:"5m"`

	// WhitelistedIPs are IPs that bypass rate limiting
	WhitelistedIPs []string `json:"whitelisted_ips" env:"RATE_LIMIT_WHITELIST_IPS"`

	// WhitelistedPaths are URL paths that bypass rate limiting (for HTTP)
	WhitelistedPaths []string `json:"whitelisted_paths" env:"RATE_LIMIT_WHITELIST_PATHS"`

	// SIP-specific settings
	SIPEnabled           bool    `json:"sip_enabled" env:"RATE_LIMIT_SIP_ENABLED" default:"false"`
	SIPInvitesPerSecond  float64 `json:"sip_invites_per_second" env:"RATE_LIMIT_SIP_INVITE_RPS" default:"10"`
	SIPInviteBurst       int     `json:"sip_invite_burst" env:"RATE_LIMIT_SIP_INVITE_BURST" default:"50"`
	SIPRequestsPerSecond float64 `json:"sip_requests_per_second" env:"RATE_LIMIT_SIP_RPS" default:"100"`
	SIPRequestBurst      int     `json:"sip_request_burst" env:"RATE_LIMIT_SIP_REQUEST_BURST" default:"200"`
}

// DefaultConfig returns sensible defaults for rate limiting
func DefaultConfig() *Config {
	return &Config{
		Enabled:              false,
		RequestsPerSecond:    100,
		BurstSize:            200,
		BlockDuration:        time.Minute,
		CleanupInterval:      5 * time.Minute,
		WhitelistedIPs:       []string{"127.0.0.1", "::1"},
		WhitelistedPaths:     []string{"/health", "/health/live", "/health/ready"},
		SIPEnabled:           false,
		SIPInvitesPerSecond:  10,
		SIPInviteBurst:       50,
		SIPRequestsPerSecond: 100,
		SIPRequestBurst:      200,
	}
}

// NewLimiter creates a new rate limiter with the given configuration
func NewLimiter(rate float64, burst int, logger *logrus.Logger) *Limiter {
	l := &Limiter{
		rate:       rate,
		burst:      burst,
		clients:    make(map[string]*bucket),
		logger:     logger,
		cleanupTTL: 10 * time.Minute,
	}

	// Start cleanup goroutine
	go l.cleanup()

	return l
}

// Allow checks if a request from the given key should be allowed
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	b, exists := l.clients[key]
	if !exists {
		// New client, create bucket with full tokens
		l.clients[key] = &bucket{
			tokens:     float64(l.burst),
			lastUpdate: now,
		}
		b = l.clients[key]
	}

	// Check if client is blocked
	if b.blocked && now.Before(b.blockUntil) {
		return false
	}
	b.blocked = false

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastUpdate = now

	// Check if we have a token to spend
	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

// AllowN checks if n requests from the given key should be allowed
func (l *Limiter) AllowN(key string, n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	b, exists := l.clients[key]
	if !exists {
		l.clients[key] = &bucket{
			tokens:     float64(l.burst),
			lastUpdate: now,
		}
		b = l.clients[key]
	}

	// Check if client is blocked
	if b.blocked && now.Before(b.blockUntil) {
		return false
	}
	b.blocked = false

	// Calculate tokens to add
	elapsed := now.Sub(b.lastUpdate).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.lastUpdate = now

	// Check if we have enough tokens
	if b.tokens >= float64(n) {
		b.tokens -= float64(n)
		return true
	}

	return false
}

// Block temporarily blocks a client
func (l *Limiter) Block(key string, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, exists := l.clients[key]
	if !exists {
		l.clients[key] = &bucket{
			tokens:     0,
			lastUpdate: time.Now(),
		}
		b = l.clients[key]
	}

	b.blocked = true
	b.blockUntil = time.Now().Add(duration)
	b.tokens = 0

	if l.logger != nil {
		l.logger.WithFields(logrus.Fields{
			"key":         key,
			"block_until": b.blockUntil,
		}).Warn("Client blocked due to rate limit violation")
	}
}

// IsBlocked checks if a client is currently blocked
func (l *Limiter) IsBlocked(key string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	b, exists := l.clients[key]
	if !exists {
		return false
	}

	return b.blocked && time.Now().Before(b.blockUntil)
}

// GetTokens returns the current token count for a client (for monitoring)
func (l *Limiter) GetTokens(key string) float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()

	b, exists := l.clients[key]
	if !exists {
		return float64(l.burst)
	}

	// Calculate current tokens
	elapsed := time.Since(b.lastUpdate).Seconds()
	tokens := b.tokens + elapsed*l.rate
	if tokens > float64(l.burst) {
		tokens = float64(l.burst)
	}

	return tokens
}

// GetClientCount returns the number of tracked clients
func (l *Limiter) GetClientCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.clients)
}

// Reset removes all tracked clients
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clients = make(map[string]*bucket)
}

// cleanup periodically removes stale client entries
func (l *Limiter) cleanup() {
	ticker := time.NewTicker(l.cleanupTTL / 2)
	defer ticker.Stop()

	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for key, b := range l.clients {
			// Remove clients that haven't been seen and aren't blocked
			if now.Sub(b.lastUpdate) > l.cleanupTTL && (!b.blocked || now.After(b.blockUntil)) {
				delete(l.clients, key)
			}
		}
		l.mu.Unlock()
	}
}
