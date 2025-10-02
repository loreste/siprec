package stt

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/metrics"
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
	attempts := m.buildAttemptOrder(providerName)
	if len(attempts) == 0 {
		return ErrNoProviderAvailable
	}

	var lastErr error
	streamSeekable, _ := audioStream.(io.Seeker)
	seekableWarningLogged := false

	for idx, vendor := range attempts {
		provider, exists := m.GetProvider(vendor)
		if !exists {
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

		startTime := time.Now()
		stopTimer := metrics.ObserveSTTLatency(vendor)
		err := provider.StreamToText(ctx, audioStream, callUUID)
		stopTimer()

		status := "success"
		if err != nil {
			status = "error"
			lastErr = err
		}
		metrics.RecordSTTRequest(vendor, status)

		elapsed := time.Since(startTime)
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
			return nil
		}

		// Provider failed; log and try next if possible
		m.logger.WithError(err).WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"provider":  vendor,
			"attempt":   idx + 1,
		}).Warn("STT provider failed, evaluating fallback options")
	}

	if lastErr != nil {
		return lastErr
	}

	return ErrNoProviderAvailable
}

func (m *ProviderManager) buildAttemptOrder(requested string) []string {
	seen := make(map[string]bool)
	order := make([]string, 0, len(m.fallbackOrder)+2)

	add := func(v string) {
		candidate := strings.TrimSpace(v)
		if candidate == "" {
			return
		}
		if !seen[candidate] {
			order = append(order, candidate)
			seen[candidate] = true
		}
	}

	add(requested)
	add(m.defaultProvider)

	for _, vendor := range m.fallbackOrder {
		add(vendor)
	}

	return order
}
