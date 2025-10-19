package sip

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
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

func TestCleanupActiveCalls(t *testing.T) {
	// Create a handler
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		ShardCount: 16,
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Add some calls
	for i := 0; i < 5; i++ {
		callID := "call" + string(rune('1'+i))
		callData := &CallData{
			LastActivity: time.Now(),
			DialogInfo:   &DialogInfo{CallID: callID},
		}
		handler.ActiveCalls.Store(callID, callData)
	}

	// Verify calls exist
	assert.Equal(t, 5, handler.GetActiveCallCount())

	// Cleanup all active calls
	handler.CleanupActiveCalls()

	// Verify all calls were removed
	assert.Equal(t, 0, handler.GetActiveCallCount())
}

func TestGetSession(t *testing.T) {
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		ShardCount: 16,
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Test with non-existent session
	session, err := handler.GetSession("non-existent")
	assert.Error(t, err)
	assert.Nil(t, session)

	// Add a session
	callData := &CallData{
		LastActivity: time.Now(),
		DialogInfo:   &DialogInfo{CallID: "call1"},
		RecordingSession: &siprec.RecordingSession{
			ID:         "session1",
			SIPID:      "call1",
			StartTime:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}

	handler.ActiveCalls.Store("call1", callData)

	// Test getting the session
	session, err = handler.GetSession("call1")
	assert.NoError(t, err)
	assert.NotNil(t, session)
}

func TestGetAllSessions(t *testing.T) {
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		ShardCount: 16,
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Initially should be empty
	sessions, err := handler.GetAllSessions()
	assert.NoError(t, err)
	assert.Empty(t, sessions)

	// Add some sessions
	for i := 0; i < 3; i++ {
		callID := "call" + string(rune('1'+i))
		callData := &CallData{
			LastActivity: time.Now(),
			DialogInfo:   &DialogInfo{CallID: callID},
			RecordingSession: &siprec.RecordingSession{
				ID:        "session" + string(rune('1'+i)),
				SIPID:     callID,
				StartTime: time.Now(),
				UpdatedAt: time.Now(),
			},
		}
		handler.ActiveCalls.Store(callID, callData)
	}

	// Get all sessions
	sessions, err = handler.GetAllSessions()
	assert.NoError(t, err)
	assert.Len(t, sessions, 3)
}

func TestGetSessionStatistics(t *testing.T) {
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls: 100,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		ShardCount: 16,
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Add sessions with different states
	now := time.Now()
	
	// Active session
	activeSession := &CallData{
		LastActivity: now,
		DialogInfo:   &DialogInfo{CallID: "call1"},
		RecordingSession: &siprec.RecordingSession{
			ID:             "active1",
			SIPID:          "call1",
			RecordingState: "active",
			StartTime:      now.Add(-5 * time.Minute),
			UpdatedAt:      now,
		},
	}

	// Paused session
	pausedSession := &CallData{
		LastActivity: now.Add(-2 * time.Minute),
		DialogInfo:   &DialogInfo{CallID: "call2"},
		RecordingSession: &siprec.RecordingSession{
			ID:             "paused1",
			SIPID:          "call2",
			RecordingState: "paused",
			StartTime:      now.Add(-10 * time.Minute),
			UpdatedAt:      now.Add(-2 * time.Minute),
		},
	}

	handler.ActiveCalls.Store("call1", activeSession)
	handler.ActiveCalls.Store("call2", pausedSession)

	// Get statistics
	stats := handler.GetSessionStatistics()
	
	assert.Equal(t, 2, stats["active_calls"])
	assert.Equal(t, 2, stats["recording_sessions"])
	assert.Equal(t, 1, stats["connected_sessions"]) // Only the active one
	assert.NotNil(t, stats["timestamp"])
	assert.True(t, stats["metrics_available"].(bool))
}

func TestShutdown(t *testing.T) {
	logger := logrus.New()
	config := &Config{
		MaxConcurrentCalls:   100,
		SessionTimeout:       30 * time.Second,
		SessionCheckInterval: 5 * time.Second,
		MediaConfig: &media.Config{
			RTPPortMin: 10000,
			RTPPortMax: 20000,
		},
		ShardCount: 16,
	}
	handler, _ := NewHandler(logger, config, mockSttProvider)

	// Setup handlers first
	handler.SetupHandlers()

	// Add an active call
	callData := &CallData{
		LastActivity: time.Now(),
		DialogInfo:   &DialogInfo{CallID: "call1"},
		RecordingSession: &siprec.RecordingSession{
			ID:        "session1",
			SIPID:     "call1",
			StartTime: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	handler.ActiveCalls.Store("call1", callData)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	err := handler.Shutdown(ctx)
	assert.NoError(t, err)

	// Verify cleanup was performed
	assert.Equal(t, 0, handler.GetActiveCallCount())
}
