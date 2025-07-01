package sip

import (
	"context"
	"time"
	
	"github.com/sirupsen/logrus"
)

// TimeoutConfig holds timeout configuration for SIP operations
type TimeoutConfig struct {
	// Request timeouts
	InviteTimeout    time.Duration // Timeout for INVITE transactions
	ByeTimeout       time.Duration // Timeout for BYE transactions
	OptionsTimeout   time.Duration // Timeout for OPTIONS transactions
	DefaultTimeout   time.Duration // Default timeout for other methods
	
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
		InviteTimeout:    32 * time.Second, // RFC 3261 Timer B
		ByeTimeout:       32 * time.Second,
		OptionsTimeout:   5 * time.Second,
		DefaultTimeout:   30 * time.Second,
		
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

// GetMethodTimeout returns the appropriate timeout for a SIP method
func (th *TimeoutHandler) GetMethodTimeout(method string) time.Duration {
	switch method {
	case "INVITE":
		return th.config.InviteTimeout
	case "BYE":
		return th.config.ByeTimeout
	case "OPTIONS":
		return th.config.OptionsTimeout
	default:
		return th.config.DefaultTimeout
	}
}

// WithTimeout executes a function with a timeout based on the SIP method
func (th *TimeoutHandler) WithTimeout(ctx context.Context, method string, fn func(context.Context) error) error {
	timeout := th.GetMethodTimeout(method)
	
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// Channel for result
	done := make(chan error, 1)
	
	// Execute function in goroutine
	go func() {
		// Panic recovery
		defer func() {
			if r := recover(); r != nil {
				th.logger.WithFields(logrus.Fields{
					"panic": r,
					"method": method,
				}).Error("Panic in timeout handler")
				done <- &TimeoutPanicError{Method: method, Panic: r}
			}
		}()
		
		done <- fn(timeoutCtx)
	}()
	
	// Wait for completion or timeout
	select {
	case err := <-done:
		return err
	case <-timeoutCtx.Done():
		th.logger.WithFields(logrus.Fields{
			"method": method,
			"timeout": timeout,
		}).Warn("Operation timed out")
		return &TimeoutError{Method: method, Timeout: timeout}
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

// TimeoutError represents a timeout error
type TimeoutError struct {
	Method  string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return "timeout for " + e.Method + " after " + e.Timeout.String()
}

// TimeoutPanicError represents a panic during timeout handling
type TimeoutPanicError struct {
	Method string
	Panic  interface{}
}

func (e *TimeoutPanicError) Error() string {
	return "panic during " + e.Method + " operation"
}