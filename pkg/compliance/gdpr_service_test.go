package compliance

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/database"
	"siprec-server/pkg/siprec"
)

// Mock recording storage for testing
type mockRecordingStorage struct {
	deletedCalls []deleteCall
	deleteError  error
}

type deleteCall struct {
	callUUID  string
	session   *siprec.RecordingSession
	localPath string
}

func (m *mockRecordingStorage) Upload(callUUID string, session *siprec.RecordingSession, localPath string) error {
	return nil
}

func (m *mockRecordingStorage) Delete(callUUID string, session *siprec.RecordingSession, localPath string) error {
	m.deletedCalls = append(m.deletedCalls, deleteCall{
		callUUID:  callUUID,
		session:   session,
		localPath: localPath,
	})

	// Simulate manifest deletion like the real implementation
	if localPath != "" {
		manifestPath := localPath + ".locations"
		_ = os.Remove(manifestPath) // Ignore error like the real implementation does
	}

	return m.deleteError
}

func (m *mockRecordingStorage) KeepLocalCopy() bool {
	return true
}

// Mock database repository for testing
type mockRepository struct {
	cdr       *database.CDR
	deleteErr error
}

func (m *mockRepository) GetSessionByCallID(callID string) (*database.Session, error) {
	return nil, nil
}

func (m *mockRepository) GetParticipantsBySession(sessionID string) ([]*database.Participant, error) {
	return nil, nil
}

func (m *mockRepository) GetStreamsBySession(sessionID string) ([]*database.Stream, error) {
	return nil, nil
}

func (m *mockRepository) GetTranscriptionsBySession(sessionID string) ([]*database.Transcription, error) {
	return nil, nil
}

func (m *mockRepository) GetCDRByCallID(callID string) (*database.CDR, error) {
	if m.cdr == nil {
		return nil, fmt.Errorf("cdr not found for call %s", callID)
	}
	return m.cdr, nil
}

func (m *mockRepository) DeleteCallData(callID string) error {
	return m.deleteErr
}

func TestGDPRService_EraseCallData_DeletesLocalRecordings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test recording file
	recording := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-123",
			RecordingPath: recording,
		},
	}

	service := NewGDPRService(mockRepo, tmpDir, nil, logrus.New())

	err := service.EraseCallData("call-123")
	require.NoError(t, err)

	// Verify file was deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err), "recording should be deleted")
}

func TestGDPRService_EraseCallData_DeletesRemoteCopies(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-456",
			RecordingPath: recording,
		},
	}

	mockStorage := &mockRecordingStorage{}

	service := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := service.EraseCallData("call-456")
	require.NoError(t, err)

	// Verify storage.Delete was called
	require.Len(t, mockStorage.deletedCalls, 1)
	assert.Equal(t, "call-456", mockStorage.deletedCalls[0].callUUID)
	assert.Equal(t, recording, mockStorage.deletedCalls[0].localPath)
}

func TestGDPRService_EraseCallData_WithoutStorage(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-789",
			RecordingPath: recording,
		},
	}

	// No storage configured (nil)
	service := NewGDPRService(mockRepo, tmpDir, nil, logrus.New())

	err := service.EraseCallData("call-789")
	require.NoError(t, err)

	// Local file should still be deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err))
}

func TestGDPRService_EraseCallData_ContinuesOnStorageError(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-error",
			RecordingPath: recording,
		},
	}

	mockStorage := &mockRecordingStorage{
		deleteError: assert.AnError, // Simulate storage error
	}

	service := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	// Should not error even if storage deletion fails
	err := service.EraseCallData("call-error")
	require.NoError(t, err)

	// Local file should still be deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err))

	// Storage deletion should have been attempted
	assert.Len(t, mockStorage.deletedCalls, 1)
}

func TestGDPRService_EraseCallData_HandlesNonexistentFiles(t *testing.T) {
	tmpDir := t.TempDir()

	nonexistent := filepath.Join(tmpDir, "nonexistent.wav")

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-missing",
			RecordingPath: nonexistent,
		},
	}

	mockStorage := &mockRecordingStorage{}

	service := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	// Should not error on nonexistent files
	err := service.EraseCallData("call-missing")
	require.NoError(t, err)

	// Storage delete should still be called
	assert.Len(t, mockStorage.deletedCalls, 1)
	assert.Equal(t, nonexistent, mockStorage.deletedCalls[0].localPath)
}

func TestGDPRService_EraseCallData_NoCDR(t *testing.T) {
	tmpDir := t.TempDir()

	mockRepo := &mockRepository{
		cdr: nil, // No CDR found
	}

	mockStorage := &mockRecordingStorage{}

	service := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := service.EraseCallData("call-no-cdr")
	require.NoError(t, err)

	// No storage deletions should occur since no recording path
	assert.Empty(t, mockStorage.deletedCalls)
}

func TestGDPRService_EraseCallData_EmptyRecordingPath(t *testing.T) {
	tmpDir := t.TempDir()

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-empty",
			RecordingPath: "", // Empty recording path
		},
	}

	mockStorage := &mockRecordingStorage{}

	service := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := service.EraseCallData("call-empty")
	require.NoError(t, err)

	// No storage deletions should occur
	assert.Empty(t, mockStorage.deletedCalls)
}
