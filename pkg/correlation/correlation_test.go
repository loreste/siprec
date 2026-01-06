package correlation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_GeneratesUniqueIDs(t *testing.T) {
	ids := make(map[string]bool)
	count := 1000

	for i := 0; i < count; i++ {
		id := New()
		assert.False(t, id.IsEmpty(), "Generated ID should not be empty")
		assert.False(t, ids[id.String()], "Generated ID should be unique")
		ids[id.String()] = true
	}
}

func TestNew_ConcurrentGeneration(t *testing.T) {
	ids := sync.Map{}
	var wg sync.WaitGroup
	count := 100
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < count; i++ {
				id := New()
				_, loaded := ids.LoadOrStore(id.String(), true)
				assert.False(t, loaded, "ID collision detected in concurrent generation")
			}
		}()
	}

	wg.Wait()
}

func TestNewShort_GeneratesShortIDs(t *testing.T) {
	id := NewShort()
	assert.False(t, id.IsEmpty())
	assert.Len(t, id.String(), 16) // 8 bytes = 16 hex chars
}

func TestFromString_WithEmptyString(t *testing.T) {
	id := FromString("")
	assert.False(t, id.IsEmpty(), "FromString with empty string should generate new ID")
}

func TestFromString_WithExistingID(t *testing.T) {
	existing := "my-correlation-id"
	id := FromString(existing)
	assert.Equal(t, existing, id.String())
}

func TestID_IsEmpty(t *testing.T) {
	assert.True(t, ID("").IsEmpty())
	assert.False(t, ID("test").IsEmpty())
}

func TestWithCorrelationID_AndFromContext(t *testing.T) {
	ctx := context.Background()
	id := New()

	// Add to context
	ctx = WithCorrelationID(ctx, id)

	// Retrieve from context
	retrieved := FromContext(ctx)
	assert.Equal(t, id, retrieved)
}

func TestFromContext_WithNilContext(t *testing.T) {
	id := FromContext(nil)
	assert.True(t, id.IsEmpty())
}

func TestFromContext_WithoutCorrelationID(t *testing.T) {
	ctx := context.Background()
	id := FromContext(ctx)
	assert.True(t, id.IsEmpty())
}

func TestFromContextOrNew_WithExisting(t *testing.T) {
	ctx := context.Background()
	existing := New()
	ctx = WithCorrelationID(ctx, existing)

	id := FromContextOrNew(ctx)
	assert.Equal(t, existing, id)
}

func TestFromContextOrNew_WithoutExisting(t *testing.T) {
	ctx := context.Background()
	id := FromContextOrNew(ctx)
	assert.False(t, id.IsEmpty())
}

func TestWithRequestStartTime_AndRetrieve(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	ctx = WithRequestStartTime(ctx, now)

	retrieved, ok := RequestStartTimeFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, now, retrieved)
}

func TestRequestStartTimeFromContext_NotSet(t *testing.T) {
	ctx := context.Background()
	_, ok := RequestStartTimeFromContext(ctx)
	assert.False(t, ok)
}

func TestWithClientIP_AndRetrieve(t *testing.T) {
	ctx := context.Background()
	ip := "192.168.1.100"

	ctx = WithClientIP(ctx, ip)

	retrieved := ClientIPFromContext(ctx)
	assert.Equal(t, ip, retrieved)
}

func TestWithMethod_AndRetrieve(t *testing.T) {
	ctx := context.Background()
	method := "POST"

	ctx = WithMethod(ctx, method)

	retrieved := MethodFromContext(ctx)
	assert.Equal(t, method, retrieved)
}

func TestRequestInfo_ToContext(t *testing.T) {
	info := NewRequestInfo("192.168.1.1", "GET", "/api/test")

	ctx := info.ToContext(context.Background())

	assert.Equal(t, info.CorrelationID, FromContext(ctx))
	assert.Equal(t, info.ClientIP, ClientIPFromContext(ctx))
	assert.Equal(t, info.Method, MethodFromContext(ctx))
}

func TestRequestInfo_Duration(t *testing.T) {
	info := NewRequestInfo("192.168.1.1", "GET", "/api/test")
	time.Sleep(10 * time.Millisecond)

	duration := info.Duration()
	assert.True(t, duration >= 10*time.Millisecond)
}

func TestRequestInfo_LogFields(t *testing.T) {
	info := NewRequestInfo("192.168.1.1", "GET", "/api/test")

	fields := info.LogFields()
	assert.Equal(t, info.CorrelationID.String(), fields["correlation_id"])
	assert.Equal(t, "192.168.1.1", fields["client_ip"])
	assert.Equal(t, "GET", fields["method"])
	assert.Equal(t, "/api/test", fields["path"])
	assert.Contains(t, fields, "duration_ms")
}

// HTTP Middleware Tests

func newTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return logger
}

func TestHTTPMiddleware_GeneratesCorrelationID(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		assert.False(t, id.IsEmpty(), "Correlation ID should be set in context")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get(HTTPHeader))
	assert.NotEmpty(t, rr.Header().Get(HTTPRequestIDHeader))
}

func TestHTTPMiddleware_UsesExistingCorrelationID(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)
	existingID := "my-existing-correlation-id"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		assert.Equal(t, existingID, id.String())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HTTPHeader, existingID)
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, existingID, rr.Header().Get(HTTPHeader))
}

func TestHTTPMiddleware_UsesXRequestID(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)
	existingID := "x-request-id-value"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		assert.Equal(t, existingID, id.String())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(HTTPRequestIDHeader, existingID)
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHTTPMiddleware_ExtractsClientIP(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ClientIPFromContext(r.Context())
		assert.NotEmpty(t, ip)
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestHTTPMiddleware_ExtractsXForwardedFor(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)

	var capturedIP string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIP = ClientIPFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 70.41.3.18")
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "203.0.113.50", capturedIP)
}

func TestHTTPMiddleware_ExtractsXRealIP(t *testing.T) {
	middleware := NewHTTPMiddleware(newTestLogger(), nil)

	var capturedIP string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedIP = ClientIPFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Real-IP", "198.51.100.25")
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "198.51.100.25", capturedIP)
}

func TestHTTPMiddleware_DisableGenerateIfMissing(t *testing.T) {
	config := &HTTPMiddlewareConfig{
		GenerateIfMissing: false,
		LogRequests:       false,
	}
	middleware := NewHTTPMiddleware(newTestLogger(), config)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := FromContext(r.Context())
		assert.True(t, id.IsEmpty(), "Correlation ID should be empty when not provided and generation disabled")
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	middleware.Middleware(handler).ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

// Logger Tests

func TestLoggerFromContext(t *testing.T) {
	logger := logrus.New()
	ctx := context.Background()

	id := New()
	ctx = WithCorrelationID(ctx, id)
	ctx = WithClientIP(ctx, "192.168.1.1")
	ctx = WithMethod(ctx, "POST")

	entry := LoggerFromContext(ctx, logger)
	require.NotNil(t, entry)

	// The entry should have the correlation fields
	assert.Equal(t, id.String(), entry.Data["correlation_id"])
	assert.Equal(t, "192.168.1.1", entry.Data["client_ip"])
	assert.Equal(t, "POST", entry.Data["method"])
}

func TestContextFields(t *testing.T) {
	ctx := context.Background()
	id := New()
	ctx = WithCorrelationID(ctx, id)
	ctx = WithClientIP(ctx, "10.0.0.1")

	fields := ContextFields(ctx)
	assert.Equal(t, id.String(), fields["correlation_id"])
	assert.Equal(t, "10.0.0.1", fields["client_ip"])
}

func TestFieldsWithCorrelation(t *testing.T) {
	id := New()
	additional := logrus.Fields{
		"custom_field": "value",
		"another":      123,
	}

	fields := FieldsWithCorrelation(id, additional)
	assert.Equal(t, id.String(), fields["correlation_id"])
	assert.Equal(t, "value", fields["custom_field"])
	assert.Equal(t, 123, fields["another"])
}

func TestWithCorrelation(t *testing.T) {
	logger := logrus.New()
	entry := logger.WithField("existing", "value")

	id := New()
	entry = WithCorrelation(entry, id)

	assert.Equal(t, id.String(), entry.Data["correlation_id"])
	assert.Equal(t, "value", entry.Data["existing"])
}

// ID Format Tests

func TestNew_IDFormat(t *testing.T) {
	id := New()
	parts := strings.Split(id.String(), "-")
	assert.Len(t, parts, 3, "ID should have 3 parts separated by dashes")

	// First part should be a timestamp (numeric)
	assert.Regexp(t, `^\d+$`, parts[0], "First part should be numeric timestamp")

	// Second part should be hex (8 chars)
	assert.Regexp(t, `^[0-9a-f]{8}$`, parts[1], "Second part should be 8 hex characters")

	// Third part should be hex counter (4 chars)
	assert.Regexp(t, `^[0-9a-f]{4}$`, parts[2], "Third part should be 4 hex characters")
}
