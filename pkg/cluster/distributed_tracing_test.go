package cluster

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Uses setupTestRedis from manager_test.go

func TestDistributedTracerStartStop(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	require.NotNil(t, tracer)

	err := tracer.Start(context.Background())
	require.NoError(t, err)

	tracer.Stop()
}

func TestStartTrace(t *testing.T) {
	mr, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	trace := tracer.StartTrace(context.Background(), "sip.invite", "call-123")
	require.NotNil(t, trace)

	assert.NotEmpty(t, trace.TraceID)
	assert.NotEmpty(t, trace.SpanID)
	assert.Equal(t, "node-1", trace.NodeID)
	assert.Equal(t, "call-123", trace.CallUUID)
	assert.Equal(t, "sip.invite", trace.Operation)
	assert.Equal(t, "active", trace.Status)
	assert.True(t, trace.StartTime > 0)

	// Verify stored in Redis
	time.Sleep(50 * time.Millisecond)
	keys := mr.Keys()
	found := false
	for _, k := range keys {
		if len(k) > len(traceKeyPrefix) {
			found = true
			break
		}
	}
	assert.True(t, found, "trace should be stored in Redis")
}

func TestStartSpan(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	parent := tracer.StartTrace(context.Background(), "sip.invite", "call-456")
	child := tracer.StartSpan(context.Background(), parent, "rtp.forward")

	require.NotNil(t, child)
	assert.Equal(t, parent.TraceID, child.TraceID, "child should share parent trace ID")
	assert.Equal(t, parent.SpanID, child.ParentID, "child parent should be parent span ID")
	assert.NotEqual(t, parent.SpanID, child.SpanID, "child should have unique span ID")
	assert.Equal(t, "rtp.forward", child.Operation)
	assert.Equal(t, "call-456", child.CallUUID)
}

func TestStartSpanNilParent(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	// Nil parent should create a new root trace
	span := tracer.StartSpan(context.Background(), nil, "orphan.op")
	require.NotNil(t, span)
	assert.NotEmpty(t, span.TraceID)
	assert.Empty(t, span.ParentID)
}

func TestEndSpanSuccess(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	span := tracer.StartTrace(context.Background(), "sip.invite", "call-789")
	time.Sleep(10 * time.Millisecond)

	tracer.EndSpan(span, nil)

	assert.Equal(t, "completed", span.Status)
	assert.True(t, span.EndTime > span.StartTime)
	assert.True(t, span.Duration > 0)
	assert.Empty(t, span.Error)
}

func TestEndSpanError(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	span := tracer.StartTrace(context.Background(), "sip.invite", "call-err")
	tracer.EndSpan(span, fmt.Errorf("connection refused"))

	assert.Equal(t, "error", span.Status)
	assert.Equal(t, "connection refused", span.Error)
}

func TestEndSpanNil(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	// Should not panic
	tracer.EndSpan(nil, nil)
}

func TestGetTrace(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	parent := tracer.StartTrace(context.Background(), "sip.invite", "call-get")
	child := tracer.StartSpan(context.Background(), parent, "rtp.forward")
	tracer.EndSpan(child, nil)
	tracer.EndSpan(parent, nil)

	time.Sleep(50 * time.Millisecond) // let saves complete

	spans, err := tracer.GetTrace(context.Background(), parent.TraceID)
	require.NoError(t, err)
	assert.Len(t, spans, 2, "should find both parent and child spans")
}

func TestGetTraceByCallUUID(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	tracer.StartTrace(context.Background(), "sip.invite", "call-uuid-lookup")
	time.Sleep(50 * time.Millisecond)

	spans, err := tracer.GetTraceByCallUUID(context.Background(), "call-uuid-lookup")
	require.NoError(t, err)
	assert.True(t, len(spans) >= 1)
}

func TestSetTagAndLog(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)

	span := tracer.StartTrace(context.Background(), "test", "")
	tracer.SetTag(span, "key", "value")
	tracer.Log(span, "something happened", map[string]string{"detail": "info"})

	assert.Equal(t, "value", span.Tags["key"])
	assert.Len(t, span.Logs, 1)
	assert.Equal(t, "something happened", span.Logs[0].Message)

	// Nil safety
	tracer.SetTag(nil, "k", "v")
	tracer.Log(nil, "msg", nil)
}

func TestTraceContextInjectionExtraction(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)

	span := tracer.StartTrace(context.Background(), "test", "")

	headers := make(map[string]string)
	tracer.InjectTraceContext(span, headers)

	assert.Equal(t, span.TraceID, headers["x-trace-id"])
	assert.Equal(t, span.SpanID, headers["x-span-id"])

	extractedTraceID, extractedSpanID := tracer.ExtractTraceContext(headers)
	assert.Equal(t, span.TraceID, extractedTraceID)
	assert.Equal(t, span.SpanID, extractedSpanID)
}

func TestInjectNilSafety(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)

	// Should not panic
	tracer.InjectTraceContext(nil, map[string]string{})
	tracer.InjectTraceContext(&TraceContext{}, nil)
}

func TestContinueTrace(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	original := tracer.StartTrace(context.Background(), "sip.invite", "call-cont")
	continued := tracer.ContinueTrace(context.Background(), original.TraceID, original.SpanID, "rtp.receive", "call-cont")

	assert.Equal(t, original.TraceID, continued.TraceID)
	assert.Equal(t, original.SpanID, continued.ParentID)
	assert.NotEqual(t, original.SpanID, continued.SpanID)
}

func TestOnTraceComplete(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	var completed sync.WaitGroup
	completed.Add(1)
	tracer.OnTraceComplete(func(trace *TraceContext) {
		completed.Done()
	})

	span := tracer.StartTrace(context.Background(), "test", "call-cb")
	tracer.EndSpan(span, nil)

	done := make(chan struct{})
	go func() {
		completed.Wait()
		close(done)
	}()

	select {
	case <-done:
		// callback fired
	case <-time.After(2 * time.Second):
		t.Fatal("trace completion callback not fired within timeout")
	}
}

func TestGetStats(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	tracer.StartTrace(context.Background(), "t1", "c1")
	tracer.StartTrace(context.Background(), "t2", "c2")
	time.Sleep(50 * time.Millisecond)

	stats := tracer.GetStats()
	assert.Equal(t, 2, stats["active_spans"])
	assert.Equal(t, "node-1", stats["node_id"])
}

func TestTracerConcurrentAccess(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				span := tracer.StartTrace(context.Background(), "concurrent", fmt.Sprintf("call-%d-%d", id, j))
				tracer.SetTag(span, "key", "value")
				tracer.Log(span, "msg", nil)
				tracer.EndSpan(span, nil)
			}
		}(i)
	}
	wg.Wait()
}

func TestTraceSIPMiddleware(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tracer := NewDistributedTracer(client, "node-1", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	// No existing trace headers — should create new trace
	span := tracer.TraceSIPMiddleware("sip.invite", "call-sip", map[string]string{})
	require.NotNil(t, span)
	assert.NotEmpty(t, span.TraceID)
	assert.Equal(t, "call-sip", span.CallUUID)

	// With existing trace headers — should continue
	continued := tracer.TraceSIPMiddleware("sip.bye", "call-sip", map[string]string{
		"X-Trace-ID": span.TraceID,
		"X-Span-ID":  span.SpanID,
	})
	assert.Equal(t, span.TraceID, continued.TraceID)
	assert.Equal(t, span.SpanID, continued.ParentID)
}
