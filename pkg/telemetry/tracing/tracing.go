package tracing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"siprec-server/pkg/config"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer     = otel.Tracer("siprec-server")
	callScopes sync.Map // map[string]*CallScope
)

type metadataKey struct{}

// CallMetadata carries enriched data tied to a single recording session/call.
type CallMetadata struct {
	mu        sync.RWMutex
	CallID    string
	Tenant    string
	Users     []string
	SessionID string
}

// SetTenant stores the tenant identifier for the call.
func (m *CallMetadata) SetTenant(tenant string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Tenant = tenant
}

// TenantOrUnknown returns the tenant identifier or "unknown" when unset.
func (m *CallMetadata) TenantOrUnknown() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Tenant == "" {
		return "unknown"
	}
	return m.Tenant
}

// SetUsers stores the list of participant identifiers for the call.
func (m *CallMetadata) SetUsers(users []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(users))
	copy(cp, users)
	m.Users = cp
}

// UsersOrEmpty returns a copy of the participant identifiers associated to the call.
func (m *CallMetadata) UsersOrEmpty() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]string, len(m.Users))
	copy(cp, m.Users)
	return cp
}

// SetSessionID stores the SIPREC recording session identifier.
func (m *CallMetadata) SetSessionID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SessionID = id
}

// SessionIDOrEmpty returns the recording session identifier if known.
func (m *CallMetadata) SessionIDOrEmpty() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.SessionID
}

// CallScope tracks the lifecycle of a per-call span hierarchy.
type CallScope struct {
	callID   string
	ctx      context.Context
	cancel   context.CancelFunc
	span     trace.Span
	metadata *CallMetadata
	endOnce  sync.Once
}

// Context returns the context carrying the call span.
func (c *CallScope) Context() context.Context {
	if c == nil {
		return context.Background()
	}
	return c.ctx
}

// Span returns the root span for the call.
func (c *CallScope) Span() trace.Span {
	if c == nil {
		return trace.SpanFromContext(context.Background())
	}
	return c.span
}

// Metadata exposes the mutable call metadata for enrichment.
func (c *CallScope) Metadata() *CallMetadata {
	if c == nil {
		return nil
	}
	return c.metadata
}

// SetAttributes attaches attributes to the call root span.
func (c *CallScope) SetAttributes(attrs ...attribute.KeyValue) {
	if c == nil {
		return
	}
	c.span.SetAttributes(attrs...)
}

// RecordError records an error on the call root span without ending it.
func (c *CallScope) RecordError(err error) {
	if c == nil || err == nil {
		return
	}
	c.span.RecordError(err)
}

// End marks the root span as completed and cleans up the registered scope.
func (c *CallScope) End(err error) {
	if c == nil {
		return
	}
	c.endOnce.Do(func() {
		if err != nil {
			c.span.RecordError(err)
			c.span.SetStatus(codes.Error, err.Error())
		} else {
			c.span.SetStatus(codes.Ok, "completed")
		}
		c.span.End()
		if c.cancel != nil {
			c.cancel()
		}
		callScopes.Delete(c.callID)
	})
}

// Init configures the global tracer provider based on repository configuration.
func Init(ctx context.Context, cfg config.TracingConfig, logger *logrus.Logger) (func(context.Context) error, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "siprec-server"
	}

	sampleRatio := cfg.SampleRatio
	if sampleRatio <= 0 {
		sampleRatio = 1.0
	}
	if sampleRatio > 1 {
		sampleRatio = 1
	}

	var providerOpts []sdktrace.TracerProviderOption

	if res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
		),
	); err != nil {
		logger.WithError(err).Warn("failed to build OpenTelemetry resource")
	} else {
		providerOpts = append(providerOpts, sdktrace.WithResource(res))
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio))
	providerOpts = append(providerOpts, sdktrace.WithSampler(sampler))

	var spanProcessor sdktrace.SpanProcessor
	if cfg.Enabled && cfg.Endpoint != "" {
		exporterCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		clientOpts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
		}

		otlpExporter, err := otlptracegrpc.New(exporterCtx, clientOpts...)
		if err != nil {
			logger.WithError(err).Warn("failed to initialize OTLP tracing exporter; falling back to local processing")
		} else {
			spanProcessor = sdktrace.NewBatchSpanProcessor(otlpExporter)
			providerOpts = append(providerOpts, sdktrace.WithSpanProcessor(spanProcessor))
		}
	}

	provider := sdktrace.NewTracerProvider(providerOpts...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = provider.Tracer("siprec-server/tracing")

	shutdown := func(shutdownCtx context.Context) error {
		if spanProcessor != nil {
			if err := spanProcessor.ForceFlush(shutdownCtx); err != nil {
				logger.WithError(err).Warn("failed to flush spans during shutdown")
			}
		}
		return provider.Shutdown(shutdownCtx)
	}

	return shutdown, nil
}

// StartCallScope registers and returns a new per-call tracing scope.
func StartCallScope(parent context.Context, callID string, attrs ...attribute.KeyValue) *CallScope {
	if parent == nil {
		parent = context.Background()
	}

	metadata := &CallMetadata{CallID: callID}
	ctxWithMeta := context.WithValue(parent, metadataKey{}, metadata)
	ctx, cancel := context.WithCancel(ctxWithMeta)

	callAttrs := []attribute.KeyValue{attribute.String("call.id", callID)}
	callAttrs = append(callAttrs, attrs...)

	spanName := fmt.Sprintf("call.%s", callID)
	ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(callAttrs...), trace.WithSpanKind(trace.SpanKindServer))

	scope := &CallScope{
		callID:   callID,
		ctx:      ctx,
		cancel:   cancel,
		span:     span,
		metadata: metadata,
	}

	callScopes.Store(callID, scope)
	return scope
}

// StartSpan creates a child span beneath the current context using the shared tracer.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, name, opts...)
}

// GetCallScope retrieves the registered scope for the given call identifier.
func GetCallScope(callID string) (*CallScope, bool) {
	value, ok := callScopes.Load(callID)
	if !ok {
		return nil, false
	}
	scope, ok := value.(*CallScope)
	return scope, ok
}

// ContextForCall returns the tracing context for a call, or background if none exists.
func ContextForCall(callID string) context.Context {
	if scope, ok := GetCallScope(callID); ok {
		return scope.Context()
	}
	return context.Background()
}

// MetadataFromContext extracts call metadata for the current tracing context.
func MetadataFromContext(ctx context.Context) *CallMetadata {
	if ctx == nil {
		return nil
	}
	if value := ctx.Value(metadataKey{}); value != nil {
		if md, ok := value.(*CallMetadata); ok {
			return md
		}
	}
	return nil
}

// SpanFromContext safely resolves a span from context, falling back to a no-op span.
func SpanFromContext(ctx context.Context) trace.Span {
	if ctx == nil {
		return trace.SpanFromContext(context.Background())
	}
	return trace.SpanFromContext(ctx)
}
