package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// HTTPMiddleware provides rate limiting for HTTP requests
type HTTPMiddleware struct {
	limiter          *Limiter
	config           *Config
	logger           *logrus.Logger
	whitelistedIPs   map[string]bool
	whitelistedNets  []*net.IPNet
	whitelistedPaths map[string]bool
	metricsRecorder  MetricsRecorder
}

// MetricsRecorder interface for recording rate limit metrics
type MetricsRecorder interface {
	RecordRateLimitHit(clientIP, path string)
	RecordRateLimitBlock(clientIP, path string)
	RecordRateLimitAllow(clientIP, path string)
}

// NewHTTPMiddleware creates a new HTTP rate limiting middleware
func NewHTTPMiddleware(config *Config, logger *logrus.Logger) *HTTPMiddleware {
	if config == nil {
		config = DefaultConfig()
	}

	m := &HTTPMiddleware{
		limiter:          NewLimiter(config.RequestsPerSecond, config.BurstSize, logger),
		config:           config,
		logger:           logger,
		whitelistedIPs:   make(map[string]bool),
		whitelistedPaths: make(map[string]bool),
	}

	// Parse whitelisted IPs
	for _, ip := range config.WhitelistedIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		// Check if it's a CIDR
		if strings.Contains(ip, "/") {
			_, ipNet, err := net.ParseCIDR(ip)
			if err == nil {
				m.whitelistedNets = append(m.whitelistedNets, ipNet)
			} else {
				logger.WithError(err).Warnf("Invalid CIDR in whitelist: %s", ip)
			}
		} else {
			m.whitelistedIPs[ip] = true
		}
	}

	// Parse whitelisted paths
	for _, path := range config.WhitelistedPaths {
		path = strings.TrimSpace(path)
		if path != "" {
			m.whitelistedPaths[path] = true
		}
	}

	logger.WithFields(logrus.Fields{
		"rps":              config.RequestsPerSecond,
		"burst":            config.BurstSize,
		"whitelisted_ips":  len(m.whitelistedIPs) + len(m.whitelistedNets),
		"whitelisted_paths": len(m.whitelistedPaths),
	}).Info("HTTP rate limiting middleware initialized")

	return m
}

// SetMetricsRecorder sets the metrics recorder for the middleware
func (m *HTTPMiddleware) SetMetricsRecorder(recorder MetricsRecorder) {
	m.metricsRecorder = recorder
}

// Middleware returns an HTTP middleware function that applies rate limiting
func (m *HTTPMiddleware) Middleware(next http.Handler) http.Handler {
	if !m.config.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := m.getClientIP(r)
		path := r.URL.Path

		// Check if path is whitelisted
		if m.isPathWhitelisted(path) {
			next.ServeHTTP(w, r)
			return
		}

		// Check if IP is whitelisted
		if m.isIPWhitelisted(clientIP) {
			next.ServeHTTP(w, r)
			return
		}

		// Check rate limit
		if !m.limiter.Allow(clientIP) {
			m.logger.WithFields(logrus.Fields{
				"client_ip": clientIP,
				"path":      path,
				"method":    r.Method,
			}).Warn("Rate limit exceeded")

			if m.metricsRecorder != nil {
				m.metricsRecorder.RecordRateLimitHit(clientIP, path)
			}

			// Block client for configured duration
			m.limiter.Block(clientIP, m.config.BlockDuration)

			if m.metricsRecorder != nil {
				m.metricsRecorder.RecordRateLimitBlock(clientIP, path)
			}

			w.Header().Set("Retry-After", "60")
			w.Header().Set("X-RateLimit-Limit", formatFloat(m.config.RequestsPerSecond))
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "Rate limit exceeded. Please retry later.", http.StatusTooManyRequests)
			return
		}

		// Add rate limit headers
		tokens := m.limiter.GetTokens(clientIP)
		w.Header().Set("X-RateLimit-Limit", formatFloat(m.config.RequestsPerSecond))
		w.Header().Set("X-RateLimit-Remaining", formatFloat(tokens))
		w.Header().Set("X-RateLimit-Reset", formatInt64(time.Now().Add(time.Second).Unix()))

		if m.metricsRecorder != nil {
			m.metricsRecorder.RecordRateLimitAllow(clientIP, path)
		}

		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts the client IP from the request
func (m *HTTPMiddleware) getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (from reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isIPWhitelisted checks if an IP is in the whitelist
func (m *HTTPMiddleware) isIPWhitelisted(ip string) bool {
	// Check exact match
	if m.whitelistedIPs[ip] {
		return true
	}

	// Check CIDR ranges
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	for _, ipNet := range m.whitelistedNets {
		if ipNet.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// isPathWhitelisted checks if a path is in the whitelist
func (m *HTTPMiddleware) isPathWhitelisted(path string) bool {
	// Exact match
	if m.whitelistedPaths[path] {
		return true
	}

	// Check prefix matches (paths ending with *)
	for p := range m.whitelistedPaths {
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}
	}

	return false
}

// GetLimiter returns the underlying limiter for advanced use
func (m *HTTPMiddleware) GetLimiter() *Limiter {
	return m.limiter
}

// formatFloat formats a float64 for header values
func formatFloat(f float64) string {
	return fmt.Sprintf("%.0f", f)
}

// formatInt64 formats an int64 for header values
func formatInt64(i int64) string {
	return fmt.Sprintf("%d", i)
}
