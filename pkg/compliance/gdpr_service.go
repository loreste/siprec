package compliance

import (
	encodingjson "encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"siprec-server/pkg/database"

	"github.com/sirupsen/logrus"
)

// GDPRService provides export and erasure capabilities for call data.
type GDPRService struct {
	repo      *database.Repository
	exportDir string
	logger    *logrus.Logger
}

// NewGDPRService creates a new GDPR service instance.
func NewGDPRService(repo *database.Repository, exportDir string, logger *logrus.Logger) *GDPRService {
	return &GDPRService{
		repo:      repo,
		exportDir: exportDir,
		logger:    logger,
	}
}

// ExportBundle represents exported call data.
type ExportBundle struct {
	CallID         string                    `json:"call_id"`
	Session        *database.Session         `json:"session,omitempty"`
	Participants   []*database.Participant   `json:"participants,omitempty"`
	Streams        []*database.Stream        `json:"streams,omitempty"`
	Transcriptions []*database.Transcription `json:"transcriptions,omitempty"`
	CDR            *database.CDR             `json:"cdr,omitempty"`
	ExportedAt     time.Time                 `json:"exported_at"`
}

// ExportCallData writes call data to a JSON bundle and returns the output path.
func (s *GDPRService) ExportCallData(callID string) (string, error) {
	if s.repo == nil {
		return "", fmt.Errorf("repository unavailable")
	}

	if err := os.MkdirAll(s.exportDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create export directory: %w", err)
	}

	session, err := s.repo.GetSessionByCallID(callID)
	if err != nil {
		return "", err
	}

	participants, err := s.repo.GetParticipantsBySession(session.ID)
	if err != nil {
		return "", err
	}

	streams, err := s.repo.GetStreamsBySession(session.ID)
	if err != nil {
		return "", err
	}

	transcriptions, err := s.repo.GetTranscriptionsBySession(session.ID)
	if err != nil {
		return "", err
	}

	cdr, err := s.repo.GetCDRByCallID(callID)
	if err != nil {
		s.logger.WithError(err).Debug("CDR not found for export")
		cdr = nil
	}

	bundle := ExportBundle{
		CallID:         callID,
		Session:        session,
		Participants:   participants,
		Streams:        streams,
		Transcriptions: transcriptions,
		CDR:            cdr,
		ExportedAt:     time.Now().UTC(),
	}

	filename := fmt.Sprintf("%s-%d.json", callID, time.Now().Unix())
	output := filepath.Join(s.exportDir, filename)

	file, err := os.Create(output)
	if err != nil {
		return "", fmt.Errorf("failed to create export file: %w", err)
	}
	defer file.Close()

	encoder := encodingjson.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&bundle); err != nil {
		return "", fmt.Errorf("failed to encode export bundle: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"call_id": callID,
		"path":    output,
	}).Info("GDPR export created")

	return output, nil
}

// EraseCallData deletes persisted call data and recording artifacts.
func (s *GDPRService) EraseCallData(callID string) error {
	if s.repo == nil {
		return fmt.Errorf("repository unavailable")
	}

	var recordingPath string
	if cdr, err := s.repo.GetCDRByCallID(callID); err == nil && cdr != nil {
		recordingPath = cdr.RecordingPath
	}

	if err := s.repo.DeleteCallData(callID); err != nil {
		return err
	}

	if recordingPath != "" {
		if err := os.Remove(recordingPath); err != nil && !os.IsNotExist(err) {
			s.logger.WithError(err).WithField("path", recordingPath).Warn("Failed to delete recording file during GDPR erase")
		}
	}

	s.logger.WithField("call_id", callID).Info("GDPR erase completed")
	return nil
}
