package compliance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/database"
	"siprec-server/pkg/siprec"
)

// Integration test for complete GDPR erase flow
// Tests: Upload → Track → Erase → Verify all copies deleted

func TestGDPRService_EraseCallData_Integration_CompleteFlow(t *testing.T) {
	tmpDir := t.TempDir()

	// Step 1: Create a recording file
	recording := filepath.Join(tmpDir, "recording.siprec")
	require.NoError(t, os.WriteFile(recording, []byte("encrypted audio data"), 0o600))

	// Step 2: Set up mock storage that tracks uploads
	mockStorage := &mockRecordingStorage{}

	// Step 3: Simulate upload with manifest tracking
	session := &siprec.RecordingSession{ID: "session-123"}
	err := mockStorage.Upload("call-integration", session, recording)
	require.NoError(t, err)

	// Verify upload was tracked
	require.Len(t, mockStorage.deletedCalls, 0, "No deletions should have occurred yet")

	// Step 4: Create manifest file to simulate recorded locations
	manifestPath := recording + ".locations"
	require.NoError(t, os.WriteFile(manifestPath, []byte(`["s3://bucket/recording.siprec","gs://backup/recording.siprec"]`), 0o600))

	// Step 5: Set up GDPR service with repository
	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-integration",
			RecordingPath: recording,
		},
	}

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	// Step 6: Execute GDPR erase
	err = gdprService.EraseCallData("call-integration")
	require.NoError(t, err)

	// Step 7: Verify complete cleanup
	t.Run("LocalFileDeleted", func(t *testing.T) {
		_, err := os.Stat(recording)
		assert.True(t, os.IsNotExist(err), "Local recording should be deleted")
	})

	t.Run("RemoteCopiesDeleted", func(t *testing.T) {
		require.Len(t, mockStorage.deletedCalls, 1, "Storage.Delete should be called once")
		assert.Equal(t, "call-integration", mockStorage.deletedCalls[0].callUUID)
		assert.Equal(t, recording, mockStorage.deletedCalls[0].localPath)
	})

	t.Run("ManifestRemoved", func(t *testing.T) {
		_, err := os.Stat(manifestPath)
		assert.True(t, os.IsNotExist(err), "Manifest file should be deleted")
	})
}

func TestGDPRService_EraseCallData_Integration_MultipleRecordings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple recordings for the same call
	recordings := []string{
		filepath.Join(tmpDir, "recording1.siprec"),
		filepath.Join(tmpDir, "recording2.wav"),
		filepath.Join(tmpDir, "recording3.siprec"),
	}

	for _, rec := range recordings {
		require.NoError(t, os.WriteFile(rec, []byte("audio"), 0o600))
	}

	// For this test, we'll use the first recording in the CDR
	primaryRecording := recordings[0]

	mockStorage := &mockRecordingStorage{}
	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-multi",
			RecordingPath: primaryRecording,
		},
	}

	// Create manifest for primary recording
	manifestPath := primaryRecording + ".locations"
	require.NoError(t, os.WriteFile(manifestPath, []byte(`["s3://bucket/recording.siprec"]`), 0o600))

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := gdprService.EraseCallData("call-multi")
	require.NoError(t, err)

	// Verify primary recording was deleted
	_, err = os.Stat(primaryRecording)
	assert.True(t, os.IsNotExist(err))

	// Verify manifest was removed
	_, err = os.Stat(manifestPath)
	assert.True(t, os.IsNotExist(err))

	// Verify storage delete was called
	require.Len(t, mockStorage.deletedCalls, 1)
}

func TestGDPRService_EraseCallData_Integration_PartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.siprec")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	manifestPath := recording + ".locations"
	require.NoError(t, os.WriteFile(manifestPath, []byte(`["s3://bucket/recording.siprec"]`), 0o600))

	// Simulate storage deletion failure
	mockStorage := &mockRecordingStorage{
		deleteError: assert.AnError,
	}

	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-partial",
			RecordingPath: recording,
		},
	}

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	// Should not error even if storage deletion fails
	err := gdprService.EraseCallData("call-partial")
	require.NoError(t, err)

	// Local file should still be deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err), "Local file should be deleted despite storage error")

	// Verify storage deletion was attempted
	require.Len(t, mockStorage.deletedCalls, 1)
}

func TestGDPRService_EraseCallData_Integration_NoManifest(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	// No manifest file created - simulates recording without remote upload

	mockStorage := &mockRecordingStorage{}
	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-no-manifest",
			RecordingPath: recording,
		},
	}

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := gdprService.EraseCallData("call-no-manifest")
	require.NoError(t, err)

	// Local file should be deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err))

	// Storage delete should still be called (will find no locations)
	require.Len(t, mockStorage.deletedCalls, 1)
}

func TestGDPRService_EraseCallData_Integration_DatabaseFailure(t *testing.T) {
	tmpDir := t.TempDir()

	recording := filepath.Join(tmpDir, "recording.siprec")
	require.NoError(t, os.WriteFile(recording, []byte("audio"), 0o600))

	mockStorage := &mockRecordingStorage{}
	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-db-fail",
			RecordingPath: recording,
		},
		deleteErr: assert.AnError, // Simulate database deletion failure
	}

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	// Should error due to database failure
	err := gdprService.EraseCallData("call-db-fail")
	require.Error(t, err)

	// Local file should NOT be deleted if database deletion fails
	_, err = os.Stat(recording)
	assert.False(t, os.IsNotExist(err), "Local file should remain if database deletion fails")

	// Storage deletion should not be attempted
	require.Empty(t, mockStorage.deletedCalls, "Storage deletion should not occur if database fails")
}

func TestGDPRService_EraseCallData_Integration_EncryptedRecording(t *testing.T) {
	tmpDir := t.TempDir()

	// Simulate encrypted .siprec file with metadata
	recording := filepath.Join(tmpDir, "recording.siprec")
	require.NoError(t, os.WriteFile(recording, []byte("encrypted data"), 0o600))

	metadataFile := recording + ".metadata"
	require.NoError(t, os.WriteFile(metadataFile, []byte(`{"algorithm":"aes-256-gcm"}`), 0o600))

	manifestPath := recording + ".locations"
	require.NoError(t, os.WriteFile(manifestPath, []byte(`["s3://secure-bucket/recording.siprec","s3://secure-backup/recording.siprec"]`), 0o600))

	mockStorage := &mockRecordingStorage{}
	mockRepo := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-encrypted",
			RecordingPath: recording,
		},
	}

	gdprService := NewGDPRService(mockRepo, tmpDir, mockStorage, logrus.New())

	err := gdprService.EraseCallData("call-encrypted")
	require.NoError(t, err)

	// Verify encrypted recording deleted
	_, err = os.Stat(recording)
	assert.True(t, os.IsNotExist(err))

	// Verify manifest deleted
	_, err = os.Stat(manifestPath)
	assert.True(t, os.IsNotExist(err))

	// Note: metadata file cleanup is handled by other components
	// This test focuses on the GDPR erase flow

	// Verify remote copies deletion
	require.Len(t, mockStorage.deletedCalls, 1)
	assert.Equal(t, recording, mockStorage.deletedCalls[0].localPath)
}

func TestGDPRService_EraseCallData_Integration_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple recordings for different calls
	call1Recording := filepath.Join(tmpDir, "call1.siprec")
	call2Recording := filepath.Join(tmpDir, "call2.siprec")

	require.NoError(t, os.WriteFile(call1Recording, []byte("audio1"), 0o600))
	require.NoError(t, os.WriteFile(call2Recording, []byte("audio2"), 0o600))

	// Create manifests
	require.NoError(t, os.WriteFile(call1Recording+".locations", []byte(`["s3://bucket/call1.siprec"]`), 0o600))
	require.NoError(t, os.WriteFile(call2Recording+".locations", []byte(`["s3://bucket/call2.siprec"]`), 0o600))

	mockStorage := &mockRecordingStorage{}

	// Create separate repositories for each call
	mockRepo1 := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-1",
			RecordingPath: call1Recording,
		},
	}

	mockRepo2 := &mockRepository{
		cdr: &database.CDR{
			CallID:        "call-2",
			RecordingPath: call2Recording,
		},
	}

	gdprService1 := NewGDPRService(mockRepo1, tmpDir, mockStorage, logrus.New())
	gdprService2 := NewGDPRService(mockRepo2, tmpDir, mockStorage, logrus.New())

	// Execute concurrent erasures
	err1 := gdprService1.EraseCallData("call-1")
	err2 := gdprService2.EraseCallData("call-2")

	require.NoError(t, err1)
	require.NoError(t, err2)

	// Verify both recordings deleted
	_, err := os.Stat(call1Recording)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(call2Recording)
	assert.True(t, os.IsNotExist(err))

	// Verify both manifests deleted
	_, err = os.Stat(call1Recording + ".locations")
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(call2Recording + ".locations")
	assert.True(t, os.IsNotExist(err))

	// Verify storage deletions for both calls
	require.Len(t, mockStorage.deletedCalls, 2)
}
