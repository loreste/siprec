package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// Standard header names for correlation IDs
const (
	// HTTPHeader is the standard HTTP header for correlation IDs
	HTTPHeader = "X-Correlation-ID"

	// HTTPRequestIDHeader is an alternative header name
	HTTPRequestIDHeader = "X-Request-ID"

	// HTTPTraceIDHeader is used by some tracing systems
	HTTPTraceIDHeader = "X-Trace-ID"

	// SIPHeader is the custom SIP header for correlation IDs
	SIPHeader = "X-Correlation-ID"
)

// contextKey is the type for context keys to avoid collisions
type contextKey int

const (
	correlationIDKey contextKey = iota
	requestStartTimeKey
	clientIPKey
	methodKey
)

var (
	// counter for generating unique IDs within the same millisecond
	counter uint64
)

// ID represents a correlation ID
type ID string

// String returns the string representation of the correlation ID
func (id ID) String() string {
	return string(id)
}

// IsEmpty returns true if the correlation ID is empty
func (id ID) IsEmpty() bool {
	return id == ""
}

// New generates a new unique correlation ID
// Format: timestamp-random-counter (e.g., "1704531234567-a1b2c3d4-0001")
func New() ID {
	timestamp := time.Now().UnixMilli()
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to counter-based if random fails
		randomBytes = []byte{0, 0, 0, 0}
	}
	randomHex := hex.EncodeToString(randomBytes)
	count := atomic.AddUint64(&counter, 1)

	return ID(fmt.Sprintf("%d-%s-%04x", timestamp, randomHex, count&0xFFFF))
}

// NewShort generates a shorter correlation ID for compact logging
// Format: random hex string (16 characters)
func NewShort() ID {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-based
		return ID(fmt.Sprintf("%x", time.Now().UnixNano()))
	}
	return ID(hex.EncodeToString(randomBytes))
}

// FromString creates a correlation ID from an existing string
// Returns the provided ID or generates a new one if empty
func FromString(s string) ID {
	if s == "" {
		return New()
	}
	return ID(s)
}

// WithCorrelationID returns a new context with the correlation ID attached
func WithCorrelationID(ctx context.Context, id ID) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// FromContext extracts the correlation ID from a context
// Returns an empty ID if not present
func FromContext(ctx context.Context) ID {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(correlationIDKey).(ID); ok {
		return id
	}
	return ""
}

// FromContextOrNew extracts the correlation ID from context or generates a new one
func FromContextOrNew(ctx context.Context) ID {
	id := FromContext(ctx)
	if id.IsEmpty() {
		return New()
	}
	return id
}

// WithRequestStartTime returns a new context with the request start time attached
func WithRequestStartTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, requestStartTimeKey, t)
}

// RequestStartTimeFromContext extracts the request start time from a context
func RequestStartTimeFromContext(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	if t, ok := ctx.Value(requestStartTimeKey).(time.Time); ok {
		return t, true
	}
	return time.Time{}, false
}

// WithClientIP returns a new context with the client IP attached
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey, ip)
}

// ClientIPFromContext extracts the client IP from a context
func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if ip, ok := ctx.Value(clientIPKey).(string); ok {
		return ip
	}
	return ""
}

// WithMethod returns a new context with the request method attached
func WithMethod(ctx context.Context, method string) context.Context {
	return context.WithValue(ctx, methodKey, method)
}

// MethodFromContext extracts the request method from a context
func MethodFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if method, ok := ctx.Value(methodKey).(string); ok {
		return method
	}
	return ""
}

// RequestInfo contains all correlation-related information for a request
type RequestInfo struct {
	CorrelationID ID
	StartTime     time.Time
	ClientIP      string
	Method        string
	Path          string
}

// NewRequestInfo creates a new RequestInfo with a generated correlation ID
func NewRequestInfo(clientIP, method, path string) *RequestInfo {
	return &RequestInfo{
		CorrelationID: New(),
		StartTime:     time.Now(),
		ClientIP:      clientIP,
		Method:        method,
		Path:          path,
	}
}

// ToContext attaches all request info to a context
func (r *RequestInfo) ToContext(ctx context.Context) context.Context {
	ctx = WithCorrelationID(ctx, r.CorrelationID)
	ctx = WithRequestStartTime(ctx, r.StartTime)
	ctx = WithClientIP(ctx, r.ClientIP)
	ctx = WithMethod(ctx, r.Method)
	return ctx
}

// Duration returns the time elapsed since the request started
func (r *RequestInfo) Duration() time.Duration {
	return time.Since(r.StartTime)
}

// LogFields returns a map of fields suitable for structured logging
func (r *RequestInfo) LogFields() map[string]interface{} {
	return map[string]interface{}{
		"correlation_id": r.CorrelationID.String(),
		"client_ip":      r.ClientIP,
		"method":         r.Method,
		"path":           r.Path,
		"duration_ms":    r.Duration().Milliseconds(),
	}
}
