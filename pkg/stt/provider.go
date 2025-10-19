package stt

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/metrics"
	"siprec-server/pkg/security/audit"
	"siprec-server/pkg/telemetry/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TranscriptionCallback is the callback function signature for real-time transcription results
type TranscriptionCallback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})

// Provider defines the interface for speech-to-text providers
type Provider interface {
	// Initialize initializes the provider with any required configuration
	Initialize() error

	// Name returns the provider name
	Name() string

	// StreamToText streams audio data to the provider and returns text
	StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error
}

// StreamingProvider extends Provider with real-time streaming capabilities
type StreamingProvider interface {
	Provider

	// SetCallback sets the callback function for real-time transcription results
	SetCallback(callback TranscriptionCallback)
}

// EnhancedStreamingProvider extends StreamingProvider with advanced capabilities
type EnhancedStreamingProvider interface {
	StreamingProvider

	// GetActiveConnections returns the number of active streaming connections
	GetActiveConnections() int

	// Shutdown gracefully shuts down all active connections
	Shutdown(ctx context.Context) error
}

// ProviderManager manages all speech-to-text providers
type ProviderManager struct {
	logger          *logrus.Logger
	providers       map[string]Provider
	defaultProvider string
	fallbackOrder   []string
	languageRouting map[string]string
	callRouting     map[string]string
	routingMutex    sync.RWMutex
}

// NewProviderManager creates a new provider manager
func NewProviderManager(logger *logrus.Logger, defaultProvider string, fallbackOrder []string) *ProviderManager {
	orderCopy := make([]string, 0, len(fallbackOrder))
	for _, vendor := range fallbackOrder {
		trimmed := strings.TrimSpace(vendor)
		if trimmed == "" {
			continue
		}
		orderCopy = append(orderCopy, trimmed)
	}

	return &ProviderManager{
		logger:          logger,
		providers:       make(map[string]Provider),
		defaultProvider: defaultProvider,
		fallbackOrder:   orderCopy,
		languageRouting: make(map[string]string),
		callRouting:     make(map[string]string),
	}
}

// RegisterProvider registers a speech-to-text provider
func (m *ProviderManager) RegisterProvider(provider Provider) error {
	// Try to initialize the provider
	if err := provider.Initialize(); err != nil {
		m.logger.WithFields(logrus.Fields{
			"provider": provider.Name(),
			"error":    err,
		}).Error("Failed to initialize speech-to-text provider")
		return err
	}

	// Add to available providers
	m.providers[provider.Name()] = provider
	m.logger.WithField("provider", provider.Name()).Info("Registered speech-to-text provider")

	return nil
}

// SetLanguageRouting configures language -> provider mappings.
func (m *ProviderManager) SetLanguageRouting(routing map[string]string) {
	m.routingMutex.Lock()
	defer m.routingMutex.Unlock()

	m.languageRouting = make(map[string]string, len(routing))
	for lang, provider := range routing {
		normalized := strings.ToLower(strings.TrimSpace(lang))
		if normalized == "" || provider == "" {
			continue
		}
		m.languageRouting[normalized] = strings.TrimSpace(provider)
	}
	if len(m.languageRouting) > 0 {
		m.logger.WithField("language_routing", m.languageRouting).Info("STT language routing configured")
	}
}

// RouteCallByLanguage maps a call to a provider based on detected language.
// Returns the provider chosen (or empty string if no mapping).
func (m *ProviderManager) RouteCallByLanguage(callUUID string, language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if normalized == "" {
		return ""
	}

	m.routingMutex.Lock()
	defer m.routingMutex.Unlock()

	provider, ok := m.languageRouting[normalized]
	if !ok {
		return ""
	}

	if current, exists := m.callRouting[callUUID]; exists && current == provider {
		return provider
	}

	m.callRouting[callUUID] = provider
	m.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"language":  normalized,
		"provider":  provider,
	}).Info("Updated STT provider routing for call based on language detection")
	return provider
}

// RouteCallToProvider explicitly assigns a provider to a call.
func (m *ProviderManager) RouteCallToProvider(callUUID, provider string) {
	if provider == "" {
		return
	}
	m.routingMutex.Lock()
	m.callRouting[callUUID] = provider
	m.routingMutex.Unlock()
}

// ClearCallRoute removes any call-specific routing.
func (m *ProviderManager) ClearCallRoute(callUUID string) {
	m.routingMutex.Lock()
	delete(m.callRouting, callUUID)
	m.routingMutex.Unlock()
}

// SelectProviderForCall determines the provider that should be used for the call without starting the stream yet.
func (m *ProviderManager) SelectProviderForCall(callUUID, requested string) string {
	return m.resolveProvider(callUUID, requested)
}

// resolveProvider determines which provider to use for a call.
func (m *ProviderManager) resolveProvider(callUUID, requested string) string {
	requested = strings.TrimSpace(requested)
	if requested != "" && !strings.EqualFold(requested, "auto") {
		m.RouteCallToProvider(callUUID, requested)
		return requested
	}

	m.routingMutex.RLock()
	provider, ok := m.callRouting[callUUID]
	m.routingMutex.RUnlock()
	if ok && provider != "" {
		return provider
	}

	return m.defaultProvider
}

// GetProvider returns a provider by name
func (m *ProviderManager) GetProvider(name string) (Provider, bool) {
	provider, exists := m.providers[name]
	return provider, exists
}

// GetDefaultProvider returns the default provider
func (m *ProviderManager) GetDefaultProvider() (Provider, bool) {
	return m.GetProvider(m.defaultProvider)
}

// StreamToProvider is a generic function to stream audio to the desired provider
func (m *ProviderManager) StreamToProvider(ctx context.Context, providerName string, audioStream io.Reader, callUUID string) error {
	resolvedProvider := m.resolveProvider(callUUID, providerName)
	attempts := m.buildAttemptOrder(resolvedProvider)
	if len(attempts) == 0 {
		return ErrNoProviderAvailable
	}

	ctx, sttSpan := tracing.StartSpan(ctx, "stt.stream", trace.WithAttributes(
		attribute.String("call.id", callUUID),
		attribute.String("stt.requested_provider", providerName),
		attribute.String("stt.resolved_provider", resolvedProvider),
	))
	defer sttSpan.End()

	var lastErr error
	streamSeekable, _ := audioStream.(io.Seeker)
	seekableWarningLogged := false

	for idx, vendor := range attempts {
		provider, exists := m.GetProvider(vendor)
		if !exists {
			m.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"provider":  vendor,
				"attempt":   idx + 1,
			}).Warn("STT provider not registered; skipping")
			continue
		}

		if idx > 0 {
			if streamSeekable == nil {
				if !seekableWarningLogged {
					m.logger.WithFields(logrus.Fields{
						"call_uuid": callUUID,
						"attempt":   vendor,
					}).Warn("Audio stream is not seekable; STT fallback limited")
					seekableWarningLogged = true
				}
				break
			}
			if _, err := streamSeekable.Seek(0, io.SeekStart); err != nil {
				lastErr = err
				m.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to rewind audio stream for STT fallback")
				break
			}
		}

		m.logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"provider":  vendor,
			"attempt":   idx + 1,
		}).Info("Starting transcription")

		if ctx.Err() != nil {
			return ctx.Err()
		}

		attemptCtx, attemptSpan := tracing.StartSpan(ctx, "stt.provider.attempt", trace.WithAttributes(
			attribute.String("stt.provider", vendor),
			attribute.Int("stt.attempt_number", idx+1),
		), trace.WithSpanKind(trace.SpanKindClient))

		startTime := time.Now()
		stopTimer := metrics.ObserveSTTLatency(vendor)
		err := provider.StreamToText(attemptCtx, audioStream, callUUID)
		stopTimer()

		status := "success"
		if err != nil {
			status = "error"
			lastErr = err
		}
		metrics.RecordSTTRequest(vendor, status)

		elapsed := time.Since(startTime)
		attemptSpan.SetAttributes(
			attribute.Int64("stt.duration_ms", elapsed.Milliseconds()),
		)
		if err != nil {
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, err.Error())
		} else {
			attemptSpan.SetStatus(codes.Ok, "completed")
		}
		attemptSpan.End()

		m.logger.WithFields(logrus.Fields{
			"call_uuid":   callUUID,
			"provider":    vendor,
			"duration_ms": elapsed.Milliseconds(),
			"error":       err != nil,
			"attempt":     idx + 1,
		}).Info("Transcription attempt completed")

		if err == nil {
			if idx > 0 {
				m.logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"provider":  vendor,
					"attempts":  idx + 1,
				}).Warn("STT fallback succeeded after previous provider errors")
			}
			sttSpan.SetAttributes(
				attribute.String("stt.provider", vendor),
				attribute.Int("stt.attempts", idx+1),
			)
			sttSpan.SetStatus(codes.Ok, "transcription completed")
			audit.Log(attemptCtx, m.logger, &audit.Event{
				Category: "stt",
				Action:   "transcription",
				Outcome:  audit.OutcomeSuccess,
				CallID:   callUUID,
				Details: map[string]interface{}{
					"provider":    vendor,
					"attempt":     idx + 1,
					"duration_ms": elapsed.Milliseconds(),
				},
			})
			return nil
		}

		// Provider failed; log and try next if possible
		m.logger.WithError(err).WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"provider":  vendor,
			"attempt":   idx + 1,
		}).Warn("STT provider failed, evaluating fallback options")

		audit.Log(attemptCtx, m.logger, &audit.Event{
			Category: "stt",
			Action:   "transcription",
			Outcome:  audit.OutcomeFailure,
			CallID:   callUUID,
			Details: map[string]interface{}{
				"provider": vendor,
				"attempt":  idx + 1,
				"error":    err.Error(),
			},
		})
	}

	if lastErr != nil {
		sttSpan.RecordError(lastErr)
		sttSpan.SetStatus(codes.Error, lastErr.Error())
		return lastErr
	}

	sttSpan.SetStatus(codes.Error, "no provider available")
	return ErrNoProviderAvailable
}

func (m *ProviderManager) buildAttemptOrder(requested string) []string {
	seen := make(map[string]bool)
	order := make([]string, 0, len(m.fallbackOrder)+2)

	add := func(v string, requireRegistered bool) {
		candidate := strings.TrimSpace(v)
		if candidate == "" {
			return
		}
		if requireRegistered {
			if _, ok := m.providers[candidate]; !ok {
				return
			}
		}
		if !seen[candidate] {
			order = append(order, candidate)
			seen[candidate] = true
		}
	}

	add(requested, false)
	add(m.defaultProvider, true)

	for _, vendor := range m.fallbackOrder {
		add(vendor, true)
	}

	return order
}

// Shutdown gracefully shuts down all registered providers
func (m *ProviderManager) Shutdown(ctx context.Context) error {
	m.logger.Info("Starting shutdown of all STT providers...")

	shutdownErrors := []error{}

	for name, provider := range m.providers {
		// Check if provider implements EnhancedStreamingProvider with Shutdown method
		if enhancedProvider, ok := provider.(EnhancedStreamingProvider); ok {
			m.logger.WithField("provider", name).Debug("Shutting down enhanced provider")

			// Create a timeout context for each provider shutdown
			providerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			if err := enhancedProvider.Shutdown(providerCtx); err != nil {
				m.logger.WithError(err).WithField("provider", name).Error("Failed to shutdown provider")
				shutdownErrors = append(shutdownErrors, err)
			} else {
				m.logger.WithField("provider", name).Info("Provider shut down successfully")
			}
		}
	}

	if len(shutdownErrors) > 0 {
		m.logger.WithField("error_count", len(shutdownErrors)).Warn("Some providers failed to shutdown cleanly")
		// Return the first error, but log all of them
		return shutdownErrors[0]
	}

	m.logger.Info("All STT providers shut down successfully")
	return nil
}
