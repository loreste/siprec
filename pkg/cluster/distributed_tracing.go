package cluster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	traceKeyPrefix     = "siprec:trace:"
	traceIndexKey      = "siprec:trace:index"
	traceTTL           = 1 * time.Hour
	traceUpdateChannel = "siprec:trace:updates"
)

// TraceContext holds distributed tracing context
type TraceContext struct {
	TraceID    string `json:"trace_id"`
	SpanID     string `json:"span_id"`
	ParentID   string `json:"parent_id,omitempty"`
	NodeID     string `json:"node_id"`
	CallUUID   string `json:"call_uuid,omitempty"`
	Operation  string `json:"operation"`
	StartTime  int64  `json:"start_time_ns"`
	EndTime    int64  `json:"end_time_ns,omitempty"`
	Duration   int64  `json:"duration_ns,omitempty"`
	Status     string `json:"status"` // "active", "completed", "error"
	Error      string `json:"error,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Logs       []TraceLog        `json:"logs,omitempty"`
}

// TraceLog represents a log entry within a span
type TraceLog struct {
	Timestamp int64             `json:"timestamp_ns"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// DistributedTracer provides cross-node distributed tracing
type DistributedTracer struct {
	redis    redis.UniversalClient
	logger   *logrus.Logger
	nodeID   string
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Active traces on this node
	activeTraces map[string]*TraceContext
	traceMu      sync.RWMutex

	// Pub/sub for trace updates
	pubsub *redis.PubSub

	// Trace aggregation callbacks
	callbacksMu sync.RWMutex
	onComplete  []func(trace *TraceContext)
}

// NewDistributedTracer creates a new distributed tracer
func NewDistributedTracer(redisClient redis.UniversalClient, nodeID string, logger *logrus.Logger) *DistributedTracer {
	return &DistributedTracer{
		redis:        redisClient,
		logger:       logger,
		nodeID:       nodeID,
		stopChan:     make(chan struct{}),
		activeTraces: make(map[string]*TraceContext),
	}
}

// Start begins the distributed tracer
func (t *DistributedTracer) Start(ctx context.Context) error {
	// Subscribe to trace updates
	t.pubsub = t.redis.Subscribe(ctx, traceUpdateChannel)

	t.wg.Add(1)
	go t.subscriptionLoop(ctx)

	t.wg.Add(1)
	go t.cleanupLoop(ctx)

	t.logger.WithField("node_id", t.nodeID).Info("Distributed tracer started")
	return nil
}

// Stop stops the distributed tracer
func (t *DistributedTracer) Stop() {
	close(t.stopChan)
	if t.pubsub != nil {
		t.pubsub.Close()
	}
	t.wg.Wait()
	t.logger.Info("Distributed tracer stopped")
}

// StartTrace begins a new trace
func (t *DistributedTracer) StartTrace(ctx context.Context, operation string, callUUID string) *TraceContext {
	traceID := generateTraceID()
	spanID := generateSpanID()

	trace := &TraceContext{
		TraceID:   traceID,
		SpanID:    spanID,
		NodeID:    t.nodeID,
		CallUUID:  callUUID,
		Operation: operation,
		StartTime: time.Now().UnixNano(),
		Status:    "active",
		Tags:      make(map[string]string),
	}

	t.traceMu.Lock()
	t.activeTraces[spanID] = trace
	t.traceMu.Unlock()

	// Store in Redis for cross-node visibility
	t.saveTrace(trace)

	return trace
}

// StartSpan starts a new span within an existing trace
func (t *DistributedTracer) StartSpan(ctx context.Context, parentTrace *TraceContext, operation string) *TraceContext {
	if parentTrace == nil {
		return t.StartTrace(ctx, operation, "")
	}

	spanID := generateSpanID()

	span := &TraceContext{
		TraceID:   parentTrace.TraceID,
		SpanID:    spanID,
		ParentID:  parentTrace.SpanID,
		NodeID:    t.nodeID,
		CallUUID:  parentTrace.CallUUID,
		Operation: operation,
		StartTime: time.Now().UnixNano(),
		Status:    "active",
		Tags:      make(map[string]string),
	}

	t.traceMu.Lock()
	t.activeTraces[spanID] = span
	t.traceMu.Unlock()

	t.saveTrace(span)

	return span
}

// ContinueTrace continues a trace from another node
func (t *DistributedTracer) ContinueTrace(ctx context.Context, traceID, parentSpanID, operation string, callUUID string) *TraceContext {
	spanID := generateSpanID()

	span := &TraceContext{
		TraceID:   traceID,
		SpanID:    spanID,
		ParentID:  parentSpanID,
		NodeID:    t.nodeID,
		CallUUID:  callUUID,
		Operation: operation,
		StartTime: time.Now().UnixNano(),
		Status:    "active",
		Tags:      make(map[string]string),
	}

	t.traceMu.Lock()
	t.activeTraces[spanID] = span
	t.traceMu.Unlock()

	t.saveTrace(span)

	t.logger.WithFields(logrus.Fields{
		"trace_id":   traceID,
		"span_id":    spanID,
		"parent_id":  parentSpanID,
		"operation":  operation,
	}).Debug("Continuing trace from another node")

	return span
}

// SetTag sets a tag on a span
func (t *DistributedTracer) SetTag(span *TraceContext, key, value string) {
	if span == nil {
		return
	}
	if span.Tags == nil {
		span.Tags = make(map[string]string)
	}
	span.Tags[key] = value
}

// Log adds a log entry to a span
func (t *DistributedTracer) Log(span *TraceContext, message string, fields map[string]string) {
	if span == nil {
		return
	}

	logEntry := TraceLog{
		Timestamp: time.Now().UnixNano(),
		Message:   message,
		Fields:    fields,
	}

	span.Logs = append(span.Logs, logEntry)
}

// EndSpan completes a span
func (t *DistributedTracer) EndSpan(span *TraceContext, err error) {
	if span == nil {
		return
	}

	span.EndTime = time.Now().UnixNano()
	span.Duration = span.EndTime - span.StartTime

	if err != nil {
		span.Status = "error"
		span.Error = err.Error()
	} else {
		span.Status = "completed"
	}

	t.traceMu.Lock()
	delete(t.activeTraces, span.SpanID)
	t.traceMu.Unlock()

	// Save final state
	t.saveTrace(span)

	// Publish completion
	t.publishTraceUpdate(span, "complete")

	// Notify callbacks
	t.notifyComplete(span)
}

// GetTrace retrieves a trace by ID
func (t *DistributedTracer) GetTrace(ctx context.Context, traceID string) ([]*TraceContext, error) {
	// Find all spans for this trace
	pattern := traceKeyPrefix + traceID + ":*"
	keys, err := t.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	// Fetch all spans
	pipe := t.redis.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	var spans []*TraceContext
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}

		var span TraceContext
		if err := json.Unmarshal(data, &span); err != nil {
			continue
		}
		spans = append(spans, &span)
	}

	return spans, nil
}

// GetTraceByCallUUID retrieves traces for a call
func (t *DistributedTracer) GetTraceByCallUUID(ctx context.Context, callUUID string) ([]*TraceContext, error) {
	// Look up trace ID from index
	traceIDs, err := t.redis.SMembers(ctx, traceIndexKey+":call:"+callUUID).Result()
	if err != nil {
		return nil, err
	}

	var allSpans []*TraceContext
	for _, traceID := range traceIDs {
		spans, err := t.GetTrace(ctx, traceID)
		if err != nil {
			continue
		}
		allSpans = append(allSpans, spans...)
	}

	return allSpans, nil
}

// OnTraceComplete registers a callback for trace completion
func (t *DistributedTracer) OnTraceComplete(callback func(trace *TraceContext)) {
	t.callbacksMu.Lock()
	defer t.callbacksMu.Unlock()
	t.onComplete = append(t.onComplete, callback)
}

// ExtractTraceContext extracts trace context from headers (e.g., HTTP headers)
func (t *DistributedTracer) ExtractTraceContext(headers map[string]string) (traceID, spanID string) {
	traceID = headers["x-trace-id"]
	spanID = headers["x-span-id"]
	return
}

// InjectTraceContext injects trace context into headers
func (t *DistributedTracer) InjectTraceContext(span *TraceContext, headers map[string]string) {
	if span == nil || headers == nil {
		return
	}
	headers["x-trace-id"] = span.TraceID
	headers["x-span-id"] = span.SpanID
	headers["x-parent-id"] = span.ParentID
}

// saveTrace saves a trace span to Redis
func (t *DistributedTracer) saveTrace(span *TraceContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := json.Marshal(span)
	if err != nil {
		return
	}

	key := fmt.Sprintf("%s%s:%s", traceKeyPrefix, span.TraceID, span.SpanID)
	t.redis.Set(ctx, key, data, traceTTL)

	// Index by call UUID if present
	if span.CallUUID != "" {
		t.redis.SAdd(ctx, traceIndexKey+":call:"+span.CallUUID, span.TraceID)
		t.redis.Expire(ctx, traceIndexKey+":call:"+span.CallUUID, traceTTL)
	}
}

// publishTraceUpdate publishes trace updates to other nodes
func (t *DistributedTracer) publishTraceUpdate(span *TraceContext, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	update := map[string]interface{}{
		"action":    action,
		"trace_id":  span.TraceID,
		"span_id":   span.SpanID,
		"node_id":   span.NodeID,
		"operation": span.Operation,
		"status":    span.Status,
		"timestamp": time.Now().UnixNano(),
	}

	data, _ := json.Marshal(update)
	t.redis.Publish(ctx, traceUpdateChannel, data)
}

// subscriptionLoop handles incoming trace updates
func (t *DistributedTracer) subscriptionLoop(ctx context.Context) {
	defer t.wg.Done()

	ch := t.pubsub.Channel()
	for {
		select {
		case <-t.stopChan:
			return
		case <-ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				continue
			}
			t.handleTraceUpdate(msg.Payload)
		}
	}
}

// handleTraceUpdate processes incoming trace updates
func (t *DistributedTracer) handleTraceUpdate(payload string) {
	var update map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &update); err != nil {
		return
	}

	nodeID, _ := update["node_id"].(string)
	if nodeID == t.nodeID {
		return // Ignore our own updates
	}

	traceID, _ := update["trace_id"].(string)
	action, _ := update["action"].(string)
	operation, _ := update["operation"].(string)

	t.logger.WithFields(logrus.Fields{
		"trace_id":  traceID,
		"action":    action,
		"operation": operation,
		"from_node": nodeID,
	}).Debug("Received trace update from cluster")
}

// cleanupLoop removes old traces
func (t *DistributedTracer) cleanupLoop(ctx context.Context) {
	defer t.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.cleanup(ctx)
		}
	}
}

// cleanup removes old trace entries
func (t *DistributedTracer) cleanup(ctx context.Context) {
	// Redis TTL handles most cleanup, but clean up stale active traces
	t.traceMu.Lock()
	staleTime := time.Now().Add(-time.Hour).UnixNano()
	var staleSpans []string
	for spanID, span := range t.activeTraces {
		if span.StartTime < staleTime {
			staleSpans = append(staleSpans, spanID)
		}
	}
	for _, spanID := range staleSpans {
		delete(t.activeTraces, spanID)
	}
	t.traceMu.Unlock()

	if len(staleSpans) > 0 {
		t.logger.WithField("count", len(staleSpans)).Info("Cleaned up stale trace spans")
	}
}

// notifyComplete notifies callbacks of trace completion
func (t *DistributedTracer) notifyComplete(trace *TraceContext) {
	t.callbacksMu.RLock()
	callbacks := make([]func(*TraceContext), len(t.onComplete))
	copy(callbacks, t.onComplete)
	t.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		go func(callback func(*TraceContext)) {
			defer func() {
				if r := recover(); r != nil {
					t.logger.WithField("panic", r).Error("Trace callback panicked")
				}
			}()
			callback(trace)
		}(cb)
	}
}

// GetStats returns tracer statistics
func (t *DistributedTracer) GetStats() map[string]interface{} {
	t.traceMu.RLock()
	activeCount := len(t.activeTraces)
	t.traceMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Count total traces in Redis
	pattern := traceKeyPrefix + "*"
	keys, _ := t.redis.Keys(ctx, pattern).Result()

	return map[string]interface{}{
		"active_spans":  activeCount,
		"total_traces":  len(keys),
		"node_id":       t.nodeID,
	}
}

// Helper functions

func generateTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// TraceHTTPMiddleware creates trace context for HTTP requests
func (t *DistributedTracer) TraceHTTPMiddleware(operation string) func(ctx context.Context, headers map[string]string) *TraceContext {
	return func(ctx context.Context, headers map[string]string) *TraceContext {
		traceID, parentSpanID := t.ExtractTraceContext(headers)

		if traceID != "" {
			// Continue existing trace
			return t.ContinueTrace(ctx, traceID, parentSpanID, operation, "")
		}

		// Start new trace
		return t.StartTrace(ctx, operation, "")
	}
}

// TraceSIPMiddleware creates trace context for SIP requests
func (t *DistributedTracer) TraceSIPMiddleware(operation, callUUID string, sipHeaders map[string]string) *TraceContext {
	ctx := context.Background()

	// Check for trace headers in SIP message
	traceID := sipHeaders["X-Trace-ID"]
	parentSpanID := sipHeaders["X-Span-ID"]

	if traceID != "" {
		return t.ContinueTrace(ctx, traceID, parentSpanID, operation, callUUID)
	}

	return t.StartTrace(ctx, operation, callUUID)
}
