package sip

import (
	"fmt"
	"time"

	"siprec-server/pkg/http"

	"github.com/sirupsen/logrus"
)

// PauseResumeService implements the http.PauseResumeService interface
type PauseResumeService struct {
	handler *Handler
	logger  *logrus.Logger
}

// NewPauseResumeService creates a new pause/resume service
func NewPauseResumeService(handler *Handler, logger *logrus.Logger) *PauseResumeService {
	return &PauseResumeService{
		handler: handler,
		logger:  logger,
	}
}

// PauseSession pauses recording and/or transcription for a session
func (s *PauseResumeService) PauseSession(sessionID string, pauseRecording, pauseTranscription bool) error {
	// Get the call data from the active calls map
	value, ok := s.handler.ActiveCalls.Load(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	callData, ok := value.(*CallData)
	if !ok {
		return fmt.Errorf("invalid call data for session: %s", sessionID)
	}

	// Pause the RTP forwarder
	if callData.Forwarder != nil {
		callData.Forwarder.Pause(pauseRecording, pauseTranscription)
		s.logger.WithFields(logrus.Fields{
			"session_id":          sessionID,
			"pause_recording":     pauseRecording,
			"pause_transcription": pauseTranscription,
		}).Info("Session paused via API")
	}

	return nil
}

// ResumeSession resumes recording and/or transcription for a session
func (s *PauseResumeService) ResumeSession(sessionID string) error {
	// Get the call data from the active calls map
	value, ok := s.handler.ActiveCalls.Load(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	callData, ok := value.(*CallData)
	if !ok {
		return fmt.Errorf("invalid call data for session: %s", sessionID)
	}

	// Resume the RTP forwarder
	if callData.Forwarder != nil {
		callData.Forwarder.Resume()
		s.logger.WithField("session_id", sessionID).Info("Session resumed via API")
	}

	return nil
}

// PauseAll pauses all active sessions
func (s *PauseResumeService) PauseAll(pauseRecording, pauseTranscription bool) error {
	// Get all active sessions
	sessions := s.handler.ActiveCalls.Keys()
	
	for _, sessionID := range sessions {
		if err := s.PauseSession(sessionID, pauseRecording, pauseTranscription); err != nil {
			s.logger.WithError(err).WithField("session_id", sessionID).Warn("Failed to pause session")
		}
	}

	s.logger.WithFields(logrus.Fields{
		"session_count":       len(sessions),
		"pause_recording":     pauseRecording,
		"pause_transcription": pauseTranscription,
	}).Info("All sessions paused")

	return nil
}

// ResumeAll resumes all paused sessions
func (s *PauseResumeService) ResumeAll() error {
	// Get all active sessions
	sessions := s.handler.ActiveCalls.Keys()
	
	for _, sessionID := range sessions {
		if err := s.ResumeSession(sessionID); err != nil {
			s.logger.WithError(err).WithField("session_id", sessionID).Warn("Failed to resume session")
		}
	}

	s.logger.WithField("session_count", len(sessions)).Info("All sessions resumed")

	return nil
}

// GetPauseStatus returns the pause status for a session
func (s *PauseResumeService) GetPauseStatus(sessionID string) (*http.PauseStatus, error) {
	// Get the call data from the active calls map
	value, ok := s.handler.ActiveCalls.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	callData, ok := value.(*CallData)
	if !ok {
		return nil, fmt.Errorf("invalid call data for session: %s", sessionID)
	}

	status := &http.PauseStatus{
		SessionID: sessionID,
	}

	if callData.Forwarder != nil {
		recordingPaused, transcriptionPaused, pausedAt := callData.Forwarder.GetPauseStatus()
		status.RecordingPaused = recordingPaused
		status.TranscriptionPaused = transcriptionPaused
		status.PausedAt = pausedAt
		status.IsPaused = recordingPaused || transcriptionPaused
		
		// Calculate pause duration if currently paused
		if status.IsPaused && pausedAt != nil {
			status.PauseDuration = time.Since(*pausedAt)
		}
	}

	return status, nil
}

// GetAllPauseStatuses returns pause status for all sessions
func (s *PauseResumeService) GetAllPauseStatuses() (map[string]*http.PauseStatus, error) {
	statuses := make(map[string]*http.PauseStatus)
	
	// Get all active sessions
	sessions := s.handler.ActiveCalls.Keys()
	
	for _, sessionID := range sessions {
		status, err := s.GetPauseStatus(sessionID)
		if err != nil {
			s.logger.WithError(err).WithField("session_id", sessionID).Warn("Failed to get pause status")
			continue
		}
		statuses[sessionID] = status
	}

	return statuses, nil
}