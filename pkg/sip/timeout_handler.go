package sip

import (
	"time"

	"github.com/sirupsen/logrus"
)

// TimeoutConfig holds timeout configuration for SIP operations
type TimeoutConfig struct {
	// Request timeouts
	InviteTimeout  time.Duration // Timeout for INVITE transactions
	ByeTimeout     time.Duration // Timeout for BYE transactions
	OptionsTimeout time.Duration // Timeout for OPTIONS transactions
	DefaultTimeout time.Duration // Default timeout for other methods

	// Connection timeouts
	TCPConnectTimeout time.Duration // TCP connection establishment timeout
	TCPReadTimeout    time.Duration // TCP read timeout
	TCPWriteTimeout   time.Duration // TCP write timeout

	// Session timeouts
	SessionIdleTimeout time.Duration // Idle session timeout
	MaxSessionDuration time.Duration // Maximum session duration
}

// DefaultTimeoutConfig returns default timeout configuration
func DefaultTimeoutConfig() *TimeoutConfig {
	return &TimeoutConfig{
		// Request timeouts
		InviteTimeout:  32 * time.Second, // RFC 3261 Timer B
		ByeTimeout:     32 * time.Second,
		OptionsTimeout: 5 * time.Second,
		DefaultTimeout: 30 * time.Second,

		// Connection timeouts
		TCPConnectTimeout: 10 * time.Second,
		TCPReadTimeout:    30 * time.Second,
		TCPWriteTimeout:   10 * time.Second,

		// Session timeouts
		SessionIdleTimeout: 5 * time.Minute,
		MaxSessionDuration: 24 * time.Hour,
	}
}

// TimeoutHandler manages timeouts for SIP operations
type TimeoutHandler struct {
	config *TimeoutConfig
	logger *logrus.Logger
}

// NewTimeoutHandler creates a new timeout handler
func NewTimeoutHandler(config *TimeoutConfig, logger *logrus.Logger) *TimeoutHandler {
	if config == nil {
		config = DefaultTimeoutConfig()
	}

	return &TimeoutHandler{
		config: config,
		logger: logger,
	}
}

// ValidateSessionAge checks if a session has exceeded maximum duration
func (th *TimeoutHandler) ValidateSessionAge(createdAt time.Time) bool {
	return time.Since(createdAt) <= th.config.MaxSessionDuration
}

// IsSessionIdle checks if a session is idle
func (th *TimeoutHandler) IsSessionIdle(lastActivity time.Time) bool {
	return time.Since(lastActivity) > th.config.SessionIdleTimeout
}
