package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type mockMetricsRecorder struct {
	hits   int
	blocks int
	allows int
}

func (m *mockMetricsRecorder) RecordRateLimitHit(clientIP, path string) {
	m.hits++
}

func (m *mockMetricsRecorder) RecordRateLimitBlock(clientIP, path string) {
	m.blocks++
}

func (m *mockMetricsRecorder) RecordRateLimitAllow(clientIP, path string) {
	m.allows++
}

func newTestMiddlewareLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func TestHTTPMiddleware_DisabledByDefault(t *testing.T) {
	config := &Config{
		Enabled: false,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	// Create a test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrapped := middleware.Middleware(handler)

	// Should pass through when disabled
	for i := 0; i < 1000; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestHTTPMiddleware_RateLimitEnforced(t *testing.T) {
	config := &Config{
		Enabled:           true,
		RequestsPerSecond: 100,
		BurstSize:         5,
		BlockDuration:     time.Minute,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// First 5 requests should succeed (burst)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "Request %d should succeed", i+1)
	}

	// 6th request should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, "60", rr.Header().Get("Retry-After"))
}

func TestHTTPMiddleware_WhitelistedPath(t *testing.T) {
	config := &Config{
		Enabled:          true,
		BurstSize:        1,
		WhitelistedPaths: []string{"/health", "/health/*"},
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// Health endpoints should bypass rate limiting
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// Health/* should also be whitelisted
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/health/live", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
}

func TestHTTPMiddleware_WhitelistedIP(t *testing.T) {
	config := &Config{
		Enabled:        true,
		BurstSize:      1,
		WhitelistedIPs: []string{"192.168.1.1", "10.0.0.0/8"},
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// Whitelisted IP should bypass rate limiting
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "Whitelisted IP should always succeed")
	}

	// IP in whitelisted network should bypass rate limiting
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "IP in whitelisted network should always succeed")
	}
}

func TestHTTPMiddleware_XForwardedFor(t *testing.T) {
	config := &Config{
		Enabled:   true,
		BurstSize: 2,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// Request with X-Forwarded-For should use that IP
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18, 150.172.238.178")
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)

	// Different forwarded IP should have its own limit
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.25")
	rr = httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHTTPMiddleware_XRealIP(t *testing.T) {
	config := &Config{
		Enabled:   true,
		BurstSize: 2,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// Request with X-Real-IP should use that IP
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		req.Header.Set("X-Real-IP", "203.0.113.100")
		rr := httptest.NewRecorder()

		wrapped.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.100")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
}

func TestHTTPMiddleware_MetricsRecording(t *testing.T) {
	config := &Config{
		Enabled:       true,
		BurstSize:     2,
		BlockDuration: time.Minute,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())
	recorder := &mockMetricsRecorder{}
	middleware.SetMetricsRecorder(recorder)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	// Make 3 requests - 2 allowed, 1 blocked
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rr := httptest.NewRecorder()
		wrapped.ServeHTTP(rr, req)
	}

	assert.Equal(t, 2, recorder.allows)
	assert.Equal(t, 1, recorder.hits)
	assert.Equal(t, 1, recorder.blocks)
}

func TestHTTPMiddleware_RateLimitHeaders(t *testing.T) {
	config := &Config{
		Enabled:           true,
		RequestsPerSecond: 100,
		BurstSize:         10,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.Middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "100", rr.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, rr.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rr.Header().Get("X-RateLimit-Reset"))
}

func TestHTTPMiddleware_GetLimiter(t *testing.T) {
	config := &Config{
		Enabled:   true,
		BurstSize: 10,
	}

	middleware := NewHTTPMiddleware(config, newTestMiddlewareLogger())

	limiter := middleware.GetLimiter()
	assert.NotNil(t, limiter)
}
