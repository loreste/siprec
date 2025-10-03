package media

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/backup"
	"siprec-server/pkg/security/audit"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/telemetry/tracing"
)

// RecordingStorage defines how completed recordings are persisted
// after local capture.
type RecordingStorage interface {
	Upload(callUUID string, session *siprec.RecordingSession, localPath string) error
	KeepLocalCopy() bool
}

// noopRecordingStorage is used when remote storage is disabled.
type noopRecordingStorage struct{}

func (noopRecordingStorage) Upload(string, *siprec.RecordingSession, string) error { return nil }
func (noopRecordingStorage) KeepLocalCopy() bool                                   { return true }

// backupRecordingStorage uploads recordings using the backup storage utilities.
type backupRecordingStorage struct {
	logger    *logrus.Logger
	storage   backup.BackupStorage
	keepLocal bool
}

// NewRecordingStorage wraps a backup storage backend for recordings.
func NewRecordingStorage(logger *logrus.Logger, store backup.BackupStorage, keepLocal bool) RecordingStorage {
	if store == nil {
		return noopRecordingStorage{}
	}
	return &backupRecordingStorage{
		logger:    logger,
		storage:   store,
		keepLocal: keepLocal,
	}
}

func (b *backupRecordingStorage) Upload(callUUID string, session *siprec.RecordingSession, localPath string) error {
	if session == nil {
		return nil
	}

	backupID := session.ID
	if backupID == "" {
		backupID = callUUID
	}
	backupID = backupID + "-" + time.Now().Format("20060102-150405")

	locations, err := b.storage.Upload(localPath, backupID)
	if err != nil {
		audit.Log(tracing.ContextForCall(callUUID), b.logger, &audit.Event{
			Category:  "storage",
			Action:    "upload",
			Outcome:   audit.OutcomeFailure,
			CallID:    callUUID,
			SessionID: session.ID,
			Details: map[string]interface{}{
				"error": err.Error(),
			},
		})
		return err
	}

	b.logger.WithFields(logrus.Fields{
		"session_id": session.ID,
		"call_uuid":  callUUID,
		"stored_at":  locations,
	}).Info("Recording persisted to external storage")

	audit.Log(tracing.ContextForCall(callUUID), b.logger, &audit.Event{
		Category:  "storage",
		Action:    "upload",
		Outcome:   audit.OutcomeSuccess,
		CallID:    callUUID,
		SessionID: session.ID,
		Details: map[string]interface{}{
			"locations": locations,
		},
	})

	return nil
}

func (b *backupRecordingStorage) KeepLocalCopy() bool {
	return b.keepLocal
}

// RemoveLocalRecording removes the local recording file when retention is disabled.
func RemoveLocalRecording(logger *logrus.Logger, path string) {
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.WithError(err).WithField("path", path).Warn("Failed to remove local recording after upload")
	}
}
