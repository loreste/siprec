package sip

import (
	"fmt"
	"testing"
	"time"

	"siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"

	"github.com/sirupsen/logrus"
)

func TestPauseResumeService(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	t.Run("pause and resume single session", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create a mock forwarder
		forwarder := &media.RTPForwarder{
			RecordingSession: &siprec.RecordingSession{
				ID: "test-session-1",
			},
			Logger: logger,
		}

		// Add call data to active calls
		callData := &CallData{
			Forwarder: forwarder,
		}
		handler.ActiveCalls.Store("test-session-1", callData)

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Test pause
		err := service.PauseSession("test-session-1", true, true)
		if err != nil {
			t.Fatalf("unexpected error pausing session: %v", err)
		}

		// Verify pause status
		status, err := service.GetPauseStatus("test-session-1")
		if err != nil {
			t.Fatalf("unexpected error getting pause status: %v", err)
		}
		if !status.IsPaused {
			t.Fatal("expected session to be paused")
		}
		if !status.RecordingPaused {
			t.Fatal("expected recording to be paused")
		}
		if !status.TranscriptionPaused {
			t.Fatal("expected transcription to be paused")
		}

		// Test resume
		err = service.ResumeSession("test-session-1")
		if err != nil {
			t.Fatalf("unexpected error resuming session: %v", err)
		}

		// Verify resume status
		status, err = service.GetPauseStatus("test-session-1")
		if err != nil {
			t.Fatalf("unexpected error getting pause status: %v", err)
		}
		if status.IsPaused {
			t.Fatal("expected session to be resumed")
		}
		if status.RecordingPaused {
			t.Fatal("expected recording to be resumed")
		}
		if status.TranscriptionPaused {
			t.Fatal("expected transcription to be resumed")
		}
	})

	t.Run("pause all sessions", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create multiple mock forwarders
		for i := 1; i <= 3; i++ {
			forwarder := &media.RTPForwarder{
				RecordingSession: &siprec.RecordingSession{
					ID: fmt.Sprintf("test-session-%d", i),
				},
				Logger: logger,
			}
			callData := &CallData{
				Forwarder: forwarder,
			}
			handler.ActiveCalls.Store(fmt.Sprintf("test-session-%d", i), callData)
		}

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Pause all sessions
		err := service.PauseAll(true, false)
		if err != nil {
			t.Fatalf("unexpected error pausing all sessions: %v", err)
		}

		// Verify all sessions are paused
		statuses, err := service.GetAllPauseStatuses()
		if err != nil {
			t.Fatalf("unexpected error getting all pause statuses: %v", err)
		}
		if len(statuses) != 3 {
			t.Fatalf("expected 3 sessions, got %d", len(statuses))
		}

		for _, status := range statuses {
			if !status.IsPaused {
				t.Fatalf("expected session %s to be paused", status.SessionID)
			}
			if !status.RecordingPaused {
				t.Fatalf("expected recording for session %s to be paused", status.SessionID)
			}
			if status.TranscriptionPaused {
				t.Fatalf("expected transcription for session %s to NOT be paused", status.SessionID)
			}
		}

		// Resume all sessions
		err = service.ResumeAll()
		if err != nil {
			t.Fatalf("unexpected error resuming all sessions: %v", err)
		}

		// Verify all sessions are resumed
		statuses, err = service.GetAllPauseStatuses()
		if err != nil {
			t.Fatalf("unexpected error getting all pause statuses: %v", err)
		}

		for _, status := range statuses {
			if status.IsPaused {
				t.Fatalf("expected session %s to be resumed", status.SessionID)
			}
		}
	})

	t.Run("session not found", func(t *testing.T) {
		// Create a handler with an empty sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Try to pause non-existent session
		err := service.PauseSession("non-existent", true, true)
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
		if err.Error() != "session not found: non-existent" {
			t.Fatalf("unexpected error message: %v", err)
		}

		// Try to get status of non-existent session
		_, err = service.GetPauseStatus("non-existent")
		if err == nil {
			t.Fatal("expected error for non-existent session")
		}
	})

	t.Run("pause duration calculation", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create a mock forwarder
		forwarder := &media.RTPForwarder{
			RecordingSession: &siprec.RecordingSession{
				ID: "test-session-duration",
			},
			Logger: logger,
		}

		// Add call data to active calls
		callData := &CallData{
			Forwarder: forwarder,
		}
		handler.ActiveCalls.Store("test-session-duration", callData)

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Pause the session
		err := service.PauseSession("test-session-duration", true, true)
		if err != nil {
			t.Fatalf("unexpected error pausing session: %v", err)
		}

		// Wait a bit
		time.Sleep(100 * time.Millisecond)

		// Get status and check duration
		status, err := service.GetPauseStatus("test-session-duration")
		if err != nil {
			t.Fatalf("unexpected error getting pause status: %v", err)
		}
		if status.PauseDuration < 100*time.Millisecond {
			t.Fatalf("expected pause duration to be at least 100ms, got %v", status.PauseDuration)
		}
		if status.PausedAt == nil {
			t.Fatal("expected PausedAt to be set")
		}
	})

	t.Run("mute and unmute single session", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create a mock forwarder
		forwarder := &media.RTPForwarder{
			RecordingSession: &siprec.RecordingSession{
				ID: "test-mute-session",
			},
			Logger: logger,
		}

		// Add call data to active calls
		callData := &CallData{
			Forwarder: forwarder,
		}
		handler.ActiveCalls.Store("test-mute-session", callData)

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Mute inbound only
		err := service.MuteSession("test-mute-session", true, false)
		if err != nil {
			t.Fatalf("unexpected error muting session: %v", err)
		}

		// Check mute status
		status, err := service.GetMuteStatus("test-mute-session")
		if err != nil {
			t.Fatalf("unexpected error getting mute status: %v", err)
		}
		if !status.InboundMuted {
			t.Fatal("expected inbound to be muted")
		}
		if status.OutboundMuted {
			t.Fatal("expected outbound to NOT be muted")
		}
		if !status.IsMuted {
			t.Fatal("expected session to be muted")
		}

		// Mute outbound as well
		err = service.MuteSession("test-mute-session", false, true)
		if err != nil {
			t.Fatalf("unexpected error muting outbound: %v", err)
		}

		status, err = service.GetMuteStatus("test-mute-session")
		if err != nil {
			t.Fatalf("unexpected error getting mute status: %v", err)
		}
		if !status.InboundMuted || !status.OutboundMuted {
			t.Fatal("expected both inbound and outbound to be muted")
		}

		// Unmute inbound only
		err = service.UnmuteSession("test-mute-session", true, false)
		if err != nil {
			t.Fatalf("unexpected error unmuting session: %v", err)
		}

		status, err = service.GetMuteStatus("test-mute-session")
		if err != nil {
			t.Fatalf("unexpected error getting mute status: %v", err)
		}
		if status.InboundMuted {
			t.Fatal("expected inbound to be unmuted")
		}
		if !status.OutboundMuted {
			t.Fatal("expected outbound to still be muted")
		}

		// Unmute all
		err = service.UnmuteSession("test-mute-session", true, true)
		if err != nil {
			t.Fatalf("unexpected error unmuting all: %v", err)
		}

		status, err = service.GetMuteStatus("test-mute-session")
		if err != nil {
			t.Fatalf("unexpected error getting mute status: %v", err)
		}
		if status.IsMuted {
			t.Fatal("expected session to be fully unmuted")
		}
	})

	t.Run("get all mute statuses", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Add multiple sessions
		for i := 0; i < 3; i++ {
			sessionID := fmt.Sprintf("mute-test-%d", i)
			forwarder := &media.RTPForwarder{
				RecordingSession: &siprec.RecordingSession{
					ID: sessionID,
				},
				Logger: logger,
			}
			callData := &CallData{
				Forwarder: forwarder,
			}
			handler.ActiveCalls.Store(sessionID, callData)
		}

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Mute first session
		err := service.MuteSession("mute-test-0", true, true)
		if err != nil {
			t.Fatalf("unexpected error muting session: %v", err)
		}

		// Get all mute statuses
		statuses, err := service.GetAllMuteStatuses()
		if err != nil {
			t.Fatalf("unexpected error getting all mute statuses: %v", err)
		}

		if len(statuses) != 3 {
			t.Fatalf("expected 3 statuses, got %d", len(statuses))
		}

		// Check that only first session is muted
		if !statuses["mute-test-0"].IsMuted {
			t.Fatal("expected mute-test-0 to be muted")
		}
		if statuses["mute-test-1"].IsMuted {
			t.Fatal("expected mute-test-1 to NOT be muted")
		}
		if statuses["mute-test-2"].IsMuted {
			t.Fatal("expected mute-test-2 to NOT be muted")
		}
	})

	t.Run("mute non-existent session", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Try to mute non-existent session
		err := service.MuteSession("non-existent", true, true)
		if err == nil {
			t.Fatal("expected error muting non-existent session")
		}
	})

	t.Run("mute duration calculation", func(t *testing.T) {
		// Create a handler with a sharded map
		handler := &Handler{
			ActiveCalls: NewShardedMap(16),
			Logger:      logger,
		}

		// Create a mock forwarder
		forwarder := &media.RTPForwarder{
			RecordingSession: &siprec.RecordingSession{
				ID: "test-mute-duration",
			},
			Logger: logger,
		}

		// Add call data to active calls
		callData := &CallData{
			Forwarder: forwarder,
		}
		handler.ActiveCalls.Store("test-mute-duration", callData)

		// Create pause/resume service
		service := NewPauseResumeService(handler, logger)

		// Mute the session
		err := service.MuteSession("test-mute-duration", true, true)
		if err != nil {
			t.Fatalf("unexpected error muting session: %v", err)
		}

		// Wait a bit
		time.Sleep(100 * time.Millisecond)

		// Get status and check duration
		status, err := service.GetMuteStatus("test-mute-duration")
		if err != nil {
			t.Fatalf("unexpected error getting mute status: %v", err)
		}
		if status.MuteDuration < 100*time.Millisecond {
			t.Fatalf("expected mute duration to be at least 100ms, got %v", status.MuteDuration)
		}
		if status.MutedAt == nil {
			t.Fatal("expected MutedAt to be set")
		}
	})
}

// Ensure PauseResumeService implements the interface
var _ http.PauseResumeService = (*PauseResumeService)(nil)