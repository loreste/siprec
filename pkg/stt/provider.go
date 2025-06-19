package stt

import (
	"context"
	"io"
	"time"

	"github.com/sirupsen/logrus"
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
}

// NewProviderManager creates a new provider manager
func NewProviderManager(logger *logrus.Logger, defaultProvider string) *ProviderManager {
	return &ProviderManager{
		logger:          logger,
		providers:       make(map[string]Provider),
		defaultProvider: defaultProvider,
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
	// Get start time for latency tracking
	startTime := time.Now()

	// Log start of transcription
	m.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"provider":  providerName,
	}).Info("Starting transcription")

	// Get the provider
	provider, exists := m.GetProvider(providerName)
	if !exists {
		// Try default provider
		m.logger.WithFields(logrus.Fields{
			"call_uuid":        callUUID,
			"provider":         providerName,
			"default_provider": m.defaultProvider,
		}).Warn("Provider not found, falling back to default")

		provider, exists = m.GetDefaultProvider()
		if !exists {
			return ErrNoProviderAvailable
		}
	}

	// Stream to the provider
	err := provider.StreamToText(ctx, audioStream, callUUID)

	// Log transcription completion and latency
	elapsed := time.Since(startTime)
	m.logger.WithFields(logrus.Fields{
		"call_uuid":   callUUID,
		"provider":    provider.Name(),
		"duration_ms": elapsed.Milliseconds(),
		"error":       err != nil,
	}).Info("Transcription completed")

	return err
}
