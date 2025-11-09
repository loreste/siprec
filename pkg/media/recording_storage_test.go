package media

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/backup"
	"siprec-server/pkg/siprec"
)

type mockBackupStorage struct {
	uploadLocations []string
	deletedPaths    []string
	location        string
}

func (m *mockBackupStorage) Upload(localPath, backupID string) ([]string, error) {
	return append([]string(nil), m.uploadLocations...), nil
}

func (m *mockBackupStorage) Download(remotePath, localPath string) error { return nil }
func (m *mockBackupStorage) List() ([]backup.StoredBackup, error)       { return nil, nil }

func (m *mockBackupStorage) Delete(remotePath string) error {
	m.deletedPaths = append(m.deletedPaths, remotePath)
	return nil
}

func (m *mockBackupStorage) GetLocation() string {
	if m.location != "" {
		return m.location
	}
	return "mock"
}

func TestBackupRecordingStoragePersistsLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "call.wav")
	require.NoError(t, os.WriteFile(localPath, []byte("test"), 0o600))

	mockStore := &mockBackupStorage{
		uploadLocations: []string{"s3://bucket/call.wav", "local://" + localPath},
		location:        "s3://bucket",
	}
	storage := &backupRecordingStorage{
		logger:  logrus.New(),
		storage: mockStore,
	}

	err := storage.Upload("call-1", &siprec.RecordingSession{ID: "session-1"}, localPath)
	require.NoError(t, err)

	data, err := os.ReadFile(locationMetadataPath(localPath))
	require.NoError(t, err)

	var locations []string
	require.NoError(t, json.Unmarshal(data, &locations))
	require.ElementsMatch(t, mockStore.uploadLocations, locations)
}

func TestBackupRecordingStorageDeleteRemovesRemoteCopies(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "call.wav")
	require.NoError(t, os.WriteFile(localPath, []byte("test"), 0o600))

	locFile := locationMetadataPath(localPath)
	require.NoError(t, os.WriteFile(locFile, []byte(`["s3://bucket/call.wav"]`), 0o600))

	mockStore := &mockBackupStorage{
		location: "s3://bucket",
	}
	storage := &backupRecordingStorage{
		logger:  logrus.New(),
		storage: mockStore,
	}

	err := storage.Delete("call-1", nil, localPath)
	require.NoError(t, err)
	require.Equal(t, []string{"s3://bucket/call.wav"}, mockStore.deletedPaths)

	_, err = os.Stat(locFile)
	require.True(t, os.IsNotExist(err), "expected location manifest to be removed")
}

func TestSaveRemoteRecordingLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")

	locations := []string{
		"s3://bucket/recording.wav",
		"gs://backup-bucket/recording.wav",
		"azure://container/recording.wav",
	}

	err := saveRemoteRecordingLocations(localPath, locations)
	require.NoError(t, err)

	metaPath := locationMetadataPath(localPath)
	require.FileExists(t, metaPath)

	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)

	var loaded []string
	require.NoError(t, json.Unmarshal(data, &loaded))
	require.Equal(t, locations, loaded)
}

func TestSaveRemoteRecordingLocations_EmptyLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")

	err := saveRemoteRecordingLocations(localPath, []string{})
	require.NoError(t, err)

	metaPath := locationMetadataPath(localPath)
	_, err = os.Stat(metaPath)
	require.True(t, os.IsNotExist(err), "should not create file for empty locations")
}

func TestSaveRemoteRecordingLocations_EmptyPath(t *testing.T) {
	err := saveRemoteRecordingLocations("", []string{"s3://bucket/file.wav"})
	require.NoError(t, err, "should gracefully handle empty path")
}

func TestLoadRemoteRecordingLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	metaPath := locationMetadataPath(localPath)

	expected := []string{
		"s3://bucket/recording.wav",
		"gs://backup/recording.wav",
	}
	data, err := json.Marshal(expected)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(metaPath, data, 0o600))

	loaded, err := loadRemoteRecordingLocations(localPath)
	require.NoError(t, err)
	require.Equal(t, expected, loaded)
}

func TestLoadRemoteRecordingLocations_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "nonexistent.wav")

	locations, err := loadRemoteRecordingLocations(localPath)
	require.NoError(t, err, "should not error on missing file")
	require.Nil(t, locations)
}

func TestLoadRemoteRecordingLocations_EmptyPath(t *testing.T) {
	locations, err := loadRemoteRecordingLocations("")
	require.NoError(t, err)
	require.Nil(t, locations)
}

func TestLoadRemoteRecordingLocations_MalformedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	metaPath := locationMetadataPath(localPath)

	require.NoError(t, os.WriteFile(metaPath, []byte("not valid json"), 0o600))

	_, err := loadRemoteRecordingLocations(localPath)
	require.Error(t, err, "should error on malformed JSON")
}

func TestRemoveRemoteRecordingLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	metaPath := locationMetadataPath(localPath)

	require.NoError(t, os.WriteFile(metaPath, []byte(`["s3://bucket/file.wav"]`), 0o600))
	require.FileExists(t, metaPath)

	err := removeRemoteRecordingLocations(localPath)
	require.NoError(t, err)

	_, err = os.Stat(metaPath)
	require.True(t, os.IsNotExist(err), "metadata file should be removed")
}

func TestRemoveRemoteRecordingLocations_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "nonexistent.wav")

	err := removeRemoteRecordingLocations(localPath)
	require.NoError(t, err, "should not error when file doesn't exist")
}

func TestRemoveRemoteRecordingLocations_EmptyPath(t *testing.T) {
	err := removeRemoteRecordingLocations("")
	require.NoError(t, err, "should gracefully handle empty path")
}

func TestBackupRecordingStorageDelete_MultipleLocations(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(localPath, []byte("test"), 0o600))

	locFile := locationMetadataPath(localPath)
	locations := []string{
		"s3://bucket/recording.wav",
		"gs://backup/recording.wav",
		"azure://container/recording.wav",
	}
	data, _ := json.Marshal(locations)
	require.NoError(t, os.WriteFile(locFile, data, 0o600))

	mockStore := &mockBackupStorage{}
	storage := &backupRecordingStorage{
		logger:  logrus.New(),
		storage: mockStore,
	}

	err := storage.Delete("call-1", nil, localPath)
	require.NoError(t, err)

	// All locations should be deleted
	require.ElementsMatch(t, locations, mockStore.deletedPaths)

	// Metadata file should be removed
	_, err = os.Stat(locFile)
	require.True(t, os.IsNotExist(err))
}

func TestBackupRecordingStorageDelete_NoMetadataFile(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(localPath, []byte("test"), 0o600))

	mockStore := &mockBackupStorage{}
	storage := &backupRecordingStorage{
		logger:  logrus.New(),
		storage: mockStore,
	}

	// Should not error when no metadata file exists
	err := storage.Delete("call-1", nil, localPath)
	require.NoError(t, err)
	require.Empty(t, mockStore.deletedPaths, "should not attempt any deletions")
}

func TestLocationMetadataPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "WAV file",
			input:    "/recordings/call.wav",
			expected: "/recordings/call.wav.locations",
		},
		{
			name:     "SIPREC file",
			input:    "/recordings/call.siprec",
			expected: "/recordings/call.siprec.locations",
		},
		{
			name:     "No extension",
			input:    "/recordings/call",
			expected: "/recordings/call.locations",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := locationMetadataPath(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBackupRecordingStorage_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "recording.wav")
	require.NoError(t, os.WriteFile(localPath, []byte("audio data"), 0o600))

	mockStore := &mockBackupStorage{
		uploadLocations: []string{
			"s3://primary/recording.wav",
			"s3://backup/recording.wav",
		},
	}
	storage := &backupRecordingStorage{
		logger:  logrus.New(),
		storage: mockStore,
	}

	// Upload
	err := storage.Upload("call-uuid", &siprec.RecordingSession{ID: "session-1"}, localPath)
	require.NoError(t, err)

	// Verify metadata was saved
	metaPath := locationMetadataPath(localPath)
	require.FileExists(t, metaPath)

	// Load and verify locations
	locations, err := loadRemoteRecordingLocations(localPath)
	require.NoError(t, err)
	require.ElementsMatch(t, mockStore.uploadLocations, locations)

	// Delete
	err = storage.Delete("call-uuid", nil, localPath)
	require.NoError(t, err)

	// Verify all locations were deleted
	require.ElementsMatch(t, mockStore.uploadLocations, mockStore.deletedPaths)

	// Verify metadata was removed
	_, err = os.Stat(metaPath)
	require.True(t, os.IsNotExist(err))
}
