package stt

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestMockProviderInterface(t *testing.T) {
	// This test ensures MockProvider implements the Provider interface
	var _ Provider = (*MockProvider)(nil)
}

func TestNewMockProvider(t *testing.T) {
	logger := logrus.New()
	provider := NewMockProvider(logger)

	assert.NotNil(t, provider, "MockProvider should not be nil")
	assert.Equal(t, logger, provider.logger, "Logger should be set correctly")
}

func TestMockProviderName(t *testing.T) {
	logger := logrus.New()
	provider := NewMockProvider(logger)

	assert.Equal(t, "mock", provider.Name(), "Name should return 'mock'")
}

func TestMockProviderInitialize(t *testing.T) {
	logger := logrus.New()
	provider := NewMockProvider(logger)

	err := provider.Initialize()
	assert.NoError(t, err, "Initialize should not return an error")
}

func TestMockProviderStreamToText(t *testing.T) {
	logger := logrus.New()
	provider := NewMockProvider(logger)

	// Create audio data and context with cancellation
	audioData := []byte("test audio data")
	audioStream := bytes.NewReader(audioData)
	callUUID := "test-call-uuid"

	ctx, cancel := context.WithCancel(context.Background())

	// Create a channel to signal when the function returns
	done := make(chan struct{})

	// Start streaming in a goroutine
	go func() {
		err := provider.StreamToText(ctx, audioStream, callUUID)
		assert.NoError(t, err, "StreamToText should not return an error when cancelled")
		close(done)
	}()

	// Give it a moment to start processing
	time.Sleep(100 * time.Millisecond)

	// Then cancel the context to stop processing
	cancel()

	// Wait for the function to complete
	select {
	case <-done:
		// Function completed successfully
	case <-time.After(2 * time.Second):
		t.Fatal("StreamToText did not exit after context cancellation")
	}
}
