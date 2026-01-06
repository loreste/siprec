package ratelimit

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SIPMethod represents a SIP method type
type SIPMethod string

const (
	SIPMethodINVITE  SIPMethod = "INVITE"
	SIPMethodACK     SIPMethod = "ACK"
	SIPMethodBYE     SIPMethod = "BYE"
	SIPMethodCANCEL  SIPMethod = "CANCEL"
	SIPMethodOPTIONS SIPMethod = "OPTIONS"
	SIPMethodREGISTER SIPMethod = "REGISTER"
	SIPMethodOther   SIPMethod = "OTHER"
)

// SIPLimiter provides rate limiting for SIP requests with method-specific limits
type SIPLimiter struct {
	inviteLimiter  *Limiter
	requestLimiter *Limiter
	config         *Config
	logger         *logrus.Logger
	whitelistedIPs map[string]bool
	whitelistedNets []*net.IPNet
	mu             sync.RWMutex

	// Metrics callback
	metricsCallback func(clientIP string, method SIPMethod, allowed bool)
}

// NewSIPLimiter creates a new SIP rate limiter
func NewSIPLimiter(config *Config, logger *logrus.Logger) *SIPLimiter {
	if config == nil {
		config = DefaultConfig()
	}

	s := &SIPLimiter{
		inviteLimiter:  NewLimiter(config.SIPInvitesPerSecond, config.SIPInviteBurst, logger),
		requestLimiter: NewLimiter(config.SIPRequestsPerSecond, config.SIPRequestBurst, logger),
		config:         config,
		logger:         logger,
		whitelistedIPs: make(map[string]bool),
	}

	// Parse whitelisted IPs
	for _, ip := range config.WhitelistedIPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		if strings.Contains(ip, "/") {
			_, ipNet, err := net.ParseCIDR(ip)
			if err == nil {
				s.whitelistedNets = append(s.whitelistedNets, ipNet)
			}
		} else {
			s.whitelistedIPs[ip] = true
		}
	}

	logger.WithFields(logrus.Fields{
		"invite_rps":     config.SIPInvitesPerSecond,
		"invite_burst":   config.SIPInviteBurst,
		"request_rps":    config.SIPRequestsPerSecond,
		"request_burst":  config.SIPRequestBurst,
		"whitelisted":    len(s.whitelistedIPs) + len(s.whitelistedNets),
	}).Info("SIP rate limiter initialized")

	return s
}

// SetMetricsCallback sets a callback for recording rate limit metrics
func (s *SIPLimiter) SetMetricsCallback(callback func(clientIP string, method SIPMethod, allowed bool)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsCallback = callback
}

// AllowRequest checks if a SIP request should be allowed
func (s *SIPLimiter) AllowRequest(clientIP string, method string) bool {
	if !s.config.SIPEnabled {
		return true
	}

	// Check whitelist
	if s.isWhitelisted(clientIP) {
		return true
	}

	sipMethod := s.parseMethod(method)
	var allowed bool

	switch sipMethod {
	case SIPMethodINVITE:
		// INVITE has stricter limits
		allowed = s.inviteLimiter.Allow(clientIP)
		if !allowed {
			s.logger.WithFields(logrus.Fields{
				"client_ip": clientIP,
				"method":    method,
			}).Warn("SIP INVITE rate limit exceeded")
		}
	case SIPMethodREGISTER:
		// REGISTER uses request limiter but tracked separately
		key := clientIP + ":REGISTER"
		allowed = s.requestLimiter.Allow(key)
		if !allowed {
			s.logger.WithFields(logrus.Fields{
				"client_ip": clientIP,
				"method":    method,
			}).Warn("SIP REGISTER rate limit exceeded")
		}
	case SIPMethodOPTIONS:
		// OPTIONS are usually lightweight, allow more
		allowed = s.requestLimiter.Allow(clientIP + ":OPTIONS")
	default:
		// Other methods use general request limiter
		allowed = s.requestLimiter.Allow(clientIP)
	}

	// Call metrics callback
	s.mu.RLock()
	callback := s.metricsCallback
	s.mu.RUnlock()
	if callback != nil {
		callback(clientIP, sipMethod, allowed)
	}

	return allowed
}

// AllowINVITE is a convenience method for checking INVITE rate limits
func (s *SIPLimiter) AllowINVITE(clientIP string) bool {
	return s.AllowRequest(clientIP, "INVITE")
}

// BlockClient temporarily blocks a client from all SIP requests
func (s *SIPLimiter) BlockClient(clientIP string, duration time.Duration) {
	s.inviteLimiter.Block(clientIP, duration)
	s.requestLimiter.Block(clientIP, duration)

	s.logger.WithFields(logrus.Fields{
		"client_ip": clientIP,
		"duration":  duration,
	}).Warn("SIP client blocked")
}

// IsBlocked checks if a client is currently blocked
func (s *SIPLimiter) IsBlocked(clientIP string) bool {
	return s.inviteLimiter.IsBlocked(clientIP) || s.requestLimiter.IsBlocked(clientIP)
}

// GetStats returns current rate limiter statistics
func (s *SIPLimiter) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"invite_clients":  s.inviteLimiter.GetClientCount(),
		"request_clients": s.requestLimiter.GetClientCount(),
		"config": map[string]interface{}{
			"invite_rps":    s.config.SIPInvitesPerSecond,
			"invite_burst":  s.config.SIPInviteBurst,
			"request_rps":   s.config.SIPRequestsPerSecond,
			"request_burst": s.config.SIPRequestBurst,
		},
	}
}

// isWhitelisted checks if an IP is whitelisted
func (s *SIPLimiter) isWhitelisted(ip string) bool {
	if s.whitelistedIPs[ip] {
		return true
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	for _, ipNet := range s.whitelistedNets {
		if ipNet.Contains(parsedIP) {
			return true
		}
	}

	return false
}

// parseMethod converts a string method to SIPMethod
func (s *SIPLimiter) parseMethod(method string) SIPMethod {
	switch strings.ToUpper(method) {
	case "INVITE":
		return SIPMethodINVITE
	case "ACK":
		return SIPMethodACK
	case "BYE":
		return SIPMethodBYE
	case "CANCEL":
		return SIPMethodCANCEL
	case "OPTIONS":
		return SIPMethodOPTIONS
	case "REGISTER":
		return SIPMethodREGISTER
	default:
		return SIPMethodOther
	}
}

// AddToWhitelist adds an IP or CIDR to the whitelist
func (s *SIPLimiter) AddToWhitelist(ip string) error {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.Contains(ip, "/") {
		_, ipNet, err := net.ParseCIDR(ip)
		if err != nil {
			return err
		}
		s.whitelistedNets = append(s.whitelistedNets, ipNet)
	} else {
		if net.ParseIP(ip) == nil {
			return &net.ParseError{Type: "IP address", Text: ip}
		}
		s.whitelistedIPs[ip] = true
	}

	return nil
}

// Reset clears all rate limit tracking
func (s *SIPLimiter) Reset() {
	s.inviteLimiter.Reset()
	s.requestLimiter.Reset()
}
