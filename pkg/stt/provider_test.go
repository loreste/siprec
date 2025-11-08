package stt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"siprec-server/pkg/metrics"
)

func init() {
	metrics.EnableMetrics(false)
}

// MockSttProvider implements Provider interface for testing
type MockSttProvider struct {
	mock.Mock
}

func (m *MockSttProvider) Initialize() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockSttProvider) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockSttProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	args := m.Called(ctx, audioStream, callUUID)
	return args.Error(0)
}

func TestNewProviderManager(t *testing.T) {
	logger := logrus.New()
	defaultProvider := "test"

	manager := NewProviderManager(logger, defaultProvider, nil)

	assert.NotNil(t, manager, "ProviderManager should not be nil")
	assert.Equal(t, defaultProvider, manager.defaultProvider, "Default provider should match")
	assert.Empty(t, manager.providers, "Providers map should be initialized and empty")
}

func TestRegisterProvider(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "test", nil)

	// Create a mock provider that initializes successfully
	provider := new(MockSttProvider)
	provider.On("Initialize").Return(nil)
	provider.On("Name").Return("test")

	// Register the provider
	err := manager.RegisterProvider(provider)

	assert.NoError(t, err, "RegisterProvider should not return an error")
	assert.Len(t, manager.providers, 1, "ProviderManager should have 1 provider")

	// Verify the mock was called
	provider.AssertExpectations(t)
}

func TestRegisterProviderInitError(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "test", nil)

	// Create a mock provider that fails to initialize
	provider := new(MockSttProvider)
	provider.On("Name").Return("test")
	provider.On("Initialize").Return(errors.New("initialization error"))

	// Register the provider
	err := manager.RegisterProvider(provider)

	assert.Error(t, err, "RegisterProvider should return an error")
	assert.Empty(t, manager.providers, "No provider should be registered")

	// Verify the mock was called
	provider.AssertExpectations(t)
}

func TestGetProvider(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "test", nil)

	// Create and register a mock provider
	provider := new(MockSttProvider)
	provider.On("Initialize").Return(nil)
	provider.On("Name").Return("test")

	manager.RegisterProvider(provider)

	// Test getting an existing provider
	p, exists := manager.GetProvider("test")
	assert.True(t, exists, "Provider should exist")
	assert.Equal(t, provider, p, "Provider should match the registered one")

	// Test getting a non-existent provider
	p, exists = manager.GetProvider("nonexistent")
	assert.False(t, exists, "Provider should not exist")
	assert.Nil(t, p, "Provider should be nil")
}

func TestGetDefaultProvider(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "default", nil)

	// Create and register a mock provider
	provider := new(MockSttProvider)
	provider.On("Initialize").Return(nil)
	provider.On("Name").Return("default")

	manager.RegisterProvider(provider)

	// Test getting the default provider
	p, exists := manager.GetDefaultProvider()
	assert.True(t, exists, "Default provider should exist")
	assert.Equal(t, provider, p, "Provider should match the registered default")
}

func TestStreamToProvider(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "default", []string{"default", "specific"})

	// Create and register mock providers
	defaultProvider := new(MockSttProvider)
	defaultProvider.On("Initialize").Return(nil)
	defaultProvider.On("Name").Return("default")

	specificProvider := new(MockSttProvider)
	specificProvider.On("Initialize").Return(nil)
	specificProvider.On("Name").Return("specific")

	manager.RegisterProvider(defaultProvider)
	manager.RegisterProvider(specificProvider)

	// Set expectations for StreamToText
	ctx := context.Background()
	audioData := []byte("test audio data")
	audioStream := bytes.NewReader(audioData)
	callUUID := "test-call-uuid"

	// Use mock.Anything for context to handle tracing wrappers
	specificProvider.On("StreamToText", mock.Anything, mock.Anything, callUUID).Return(nil)

	// Call StreamToProvider
	err := manager.StreamToProvider(ctx, "specific", audioStream, callUUID)

	assert.NoError(t, err, "StreamToProvider should not return an error")
	specificProvider.AssertExpectations(t)
}

func TestStreamToProviderFallback(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "default", []string{"default"})

	// Create and register only the default provider
	defaultProvider := new(MockSttProvider)
	defaultProvider.On("Initialize").Return(nil)
	defaultProvider.On("Name").Return("default")

	manager.RegisterProvider(defaultProvider)

	// Set expectations for StreamToText
	ctx := context.Background()
	audioData := []byte("test audio data")
	audioStream := bytes.NewReader(audioData)
	callUUID := "test-call-uuid"

	// Use mock.Anything for context to handle tracing wrappers
	defaultProvider.On("StreamToText", mock.Anything, mock.Anything, callUUID).Return(nil)

	// Call StreamToProvider with a non-existent provider, should fall back to default
	err := manager.StreamToProvider(ctx, "nonexistent", audioStream, callUUID)

	assert.NoError(t, err, "StreamToProvider should not return an error")
	defaultProvider.AssertExpectations(t)
}

func TestStreamToProviderNoProviders(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "default", nil)

	// Don't register any providers

	// Call StreamToProvider
	ctx := context.Background()
	audioData := []byte("test audio data")
	audioStream := bytes.NewReader(audioData)
	callUUID := "test-call-uuid"

	err := manager.StreamToProvider(ctx, "nonexistent", audioStream, callUUID)

	assert.Error(t, err, "StreamToProvider should return an error")
	assert.Equal(t, ErrNoProviderAvailable, err, "Error should be ErrNoProviderAvailable")
}

func TestStreamToProviderFallbackOrderSeekable(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "primary", []string{"primary", "secondary"})

	primary := new(MockSttProvider)
	primary.On("Initialize").Return(nil)
	primary.On("Name").Return("primary")
	primary.On("StreamToText", mock.Anything, mock.Anything, "call-seekable").Return(errors.New("primary failure"))

	secondary := new(MockSttProvider)
	secondary.On("Initialize").Return(nil)
	secondary.On("Name").Return("secondary")
	secondary.On("StreamToText", mock.Anything, mock.Anything, "call-seekable").Return(nil)

	manager.RegisterProvider(primary)
	manager.RegisterProvider(secondary)

	ctx := context.Background()
	reader := bytes.NewReader([]byte("seekable audio"))

	err := manager.StreamToProvider(ctx, "primary", reader, "call-seekable")

	assert.NoError(t, err)
	// Use mock.Anything for context to handle tracing wrappers
	primary.AssertCalled(t, "StreamToText", mock.Anything, mock.Anything, "call-seekable")
	secondary.AssertCalled(t, "StreamToText", mock.Anything, mock.Anything, "call-seekable")
}

func TestStreamToProviderFallbackNonSeekable(t *testing.T) {
	logger := logrus.New()
	manager := NewProviderManager(logger, "primary", []string{"primary", "secondary"})

	primary := new(MockSttProvider)
	primary.On("Initialize").Return(nil)
	primary.On("Name").Return("primary")
	primaryErr := errors.New("primary failure")
	primary.On("StreamToText", mock.Anything, mock.Anything, "call-nonseek").Return(primaryErr)

	secondary := new(MockSttProvider)
	secondary.On("Initialize").Return(nil)
	secondary.On("Name").Return("secondary")

	manager.RegisterProvider(primary)
	manager.RegisterProvider(secondary)

	ctx := context.Background()
	nonSeekable := io.NopCloser(bytes.NewBufferString("stream"))

	err := manager.StreamToProvider(ctx, "primary", nonSeekable, "call-nonseek")

	assert.ErrorIs(t, err, primaryErr)
	// Use mock.Anything for context to handle tracing wrappers
	primary.AssertCalled(t, "StreamToText", mock.Anything, mock.Anything, "call-nonseek")
	secondary.AssertNotCalled(t, "StreamToText", mock.Anything, mock.Anything, "call-nonseek")
}
