package sip

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"siprec-server/pkg/media"
)

// Mock function for STT provider
func mockSttProvider(_ context.Context, _ string, _ io.Reader, _ string) error {
	return nil
}

func TestNewHandler(t *testing.T) {
	// Create logger and config
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
	}

	// Create new handler
	handler, err := NewHandler(logger, config, mockSttProvider)

	assert.NoError(t, err, "NewHandler should not return an error")
	assert.NotNil(t, handler, "Handler should not be nil")
	assert.Equal(t, config, handler.Config, "Config should be set")
	assert.NotNil(t, handler.ActiveCalls, "ActiveCalls map should be initialized")
}

// TestCallData tests the CallData struct
func TestCallData(t *testing.T) {
	// Create a call data directly
	callUUID := "test-call-uuid"
	callData := &CallData{
		LastActivity: time.Now(),
		DialogInfo: &DialogInfo{
			CallID: callUUID,
		},
	}

	assert.NotNil(t, callData, "CallData should not be nil")
	assert.Equal(t, callUUID, callData.DialogInfo.CallID, "CallID should be set")
	assert.NotNil(t, callData.LastActivity, "LastActivity should be set")
	assert.Nil(t, callData.Forwarder, "Forwarder should be nil initially")
	assert.Nil(t, callData.RecordingSession, "RecordingSession should be nil initially")
}

func TestGetActiveCallCount(t *testing.T) {
	// Create a handler
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Initial count should be zero
	count := handler.GetActiveCallCount()
	assert.Equal(t, 0, count, "Initial active call count should be zero")

	// Add some calls
	callData1 := &CallData{LastActivity: time.Now(), DialogInfo: &DialogInfo{CallID: "call1"}}
	callData2 := &CallData{LastActivity: time.Now(), DialogInfo: &DialogInfo{CallID: "call2"}}
	callData3 := &CallData{LastActivity: time.Now(), DialogInfo: &DialogInfo{CallID: "call3"}}

	handler.ActiveCalls.Store("call1", callData1)
	handler.ActiveCalls.Store("call2", callData2)
	handler.ActiveCalls.Store("call3", callData3)

	// Count should now be 3
	count = handler.GetActiveCallCount()
	assert.Equal(t, 3, count, "Active call count should be 3")

	// Remove a call
	handler.ActiveCalls.Delete("call2")

	// Count should now be 2
	count = handler.GetActiveCallCount()
	assert.Equal(t, 2, count, "Active call count should be 2")
}
