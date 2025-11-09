package backup

import (
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyStorage struct {
	location    string
	deleteCalls []string
	deleteError error
}

func (d *dummyStorage) Upload(string, string) ([]string, error) { return nil, nil }
func (d *dummyStorage) Download(string, string) error           { return nil }
func (d *dummyStorage) List() ([]StoredBackup, error)           { return nil, nil }

func (d *dummyStorage) Delete(remotePath string) error {
	d.deleteCalls = append(d.deleteCalls, remotePath)
	return d.deleteError
}

func (d *dummyStorage) GetLocation() string { return d.location }

func TestMultiBackupStorageDeleteTargetsMatchingScheme(t *testing.T) {
	local := &dummyStorage{location: "local"}
	s3 := &dummyStorage{location: "s3://bucket"}
	multi := &MultiBackupStorage{
		storages: []BackupStorage{local, s3},
		logger:   logrus.New(),
	}

	err := multi.Delete("s3://bucket/recordings/file.wav")
	require.NoError(t, err)

	require.Empty(t, local.deleteCalls, "local storage should not see s3 deletions")
	require.Equal(t, []string{"s3://bucket/recordings/file.wav"}, s3.deleteCalls)
}

func TestMultiBackupStorageDeleteNoMatch(t *testing.T) {
	local := &dummyStorage{location: "local"}
	multi := &MultiBackupStorage{
		storages: []BackupStorage{local},
		logger:   logrus.New(),
	}

	err := multi.Delete("gcs://bucket/file.wav")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no storage backend configured")
}

func TestExtractScheme(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "S3 path",
			path:     "s3://bucket/path/file.siprec",
			expected: "s3",
		},
		{
			name:     "GCS path",
			path:     "gs://bucket/path/file.siprec",
			expected: "gs",
		},
		{
			name:     "Azure path",
			path:     "azure://container/path/file.siprec",
			expected: "azure",
		},
		{
			name:     "Local path",
			path:     "local://recordings/file.siprec",
			expected: "local",
		},
		{
			name:     "No scheme",
			path:     "/absolute/path/file.siprec",
			expected: "",
		},
		{
			name:     "Empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "Malformed scheme",
			path:     "s3:/bucket/file.siprec",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractScheme(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStorageMatchesScheme(t *testing.T) {
	tests := []struct {
		name     string
		location string
		scheme   string
		expected bool
	}{
		{
			name:     "S3 matches s3 scheme",
			location: "s3",
			scheme:   "s3",
			expected: true,
		},
		{
			name:     "GCS does not match gs scheme",
			location: "gcs",
			scheme:   "gs",
			expected: false, // Different schemes even though "gcs" starts with "gs"
		},
		{
			name:     "GCS URL matches gcs scheme",
			location: "gcs://bucket",
			scheme:   "gcs",
			expected: true,
		},
		{
			name:     "Azure matches azure scheme",
			location: "azure",
			scheme:   "azure",
			expected: true,
		},
		{
			name:     "Local matches local scheme",
			location: "local",
			scheme:   "local",
			expected: true,
		},
		{
			name:     "S3 URL matches s3 scheme",
			location: "s3://bucket",
			scheme:   "s3",
			expected: true,
		},
		{
			name:     "S3 does not match gs scheme",
			location: "s3",
			scheme:   "gs",
			expected: false,
		},
		{
			name:     "Empty scheme never matches",
			location: "s3",
			scheme:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storageMatchesScheme(tt.location, tt.scheme)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMultiBackupStorageDelete_MultipleBackendsSameScheme(t *testing.T) {
	s3Primary := &dummyStorage{location: "s3"}
	s3Backup := &dummyStorage{location: "s3-replica"}

	multi := &MultiBackupStorage{
		storages: []BackupStorage{s3Primary, s3Backup},
		logger:   logrus.New(),
	}

	err := multi.Delete("s3://bucket/file.siprec")

	require.NoError(t, err)
	assert.Equal(t, []string{"s3://bucket/file.siprec"}, s3Primary.deleteCalls)
	assert.Equal(t, []string{"s3://bucket/file.siprec"}, s3Backup.deleteCalls)
}

func TestMultiBackupStorageDelete_PartialFailure(t *testing.T) {
	s3Success := &dummyStorage{location: "s3"}
	s3Fail := &dummyStorage{
		location:    "s3-backup",
		deleteError: fmt.Errorf("network timeout"),
	}

	multi := &MultiBackupStorage{
		storages: []BackupStorage{s3Success, s3Fail},
		logger:   logrus.New(),
	}

	err := multi.Delete("s3://bucket/file.siprec")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "network timeout")

	// Both should have attempted deletion
	assert.Equal(t, []string{"s3://bucket/file.siprec"}, s3Success.deleteCalls)
	assert.Equal(t, []string{"s3://bucket/file.siprec"}, s3Fail.deleteCalls)
}

func TestMultiBackupStorageDelete_LocalScheme(t *testing.T) {
	local := &dummyStorage{location: "local"}
	s3 := &dummyStorage{location: "s3"}

	multi := &MultiBackupStorage{
		storages: []BackupStorage{local, s3},
		logger:   logrus.New(),
	}

	err := multi.Delete("local://recordings/file.siprec")

	require.NoError(t, err)
	assert.Equal(t, []string{"local://recordings/file.siprec"}, local.deleteCalls)
	assert.Empty(t, s3.deleteCalls)
}

func TestMultiBackupStorageDelete_DifferentSchemes(t *testing.T) {
	local := &dummyStorage{location: "local"}
	s3 := &dummyStorage{location: "s3"}
	gcs := &dummyStorage{location: "gs"} // Use "gs" to match gs:// scheme
	azure := &dummyStorage{location: "azure"}

	multi := &MultiBackupStorage{
		storages: []BackupStorage{local, s3, gcs, azure},
		logger:   logrus.New(),
	}

	tests := []struct {
		path             string
		expectCalls      string
		expectNoCalls    []string
	}{
		{
			path:          "s3://bucket/file.siprec",
			expectCalls:   "s3",
			expectNoCalls: []string{"local", "gcs", "azure"},
		},
		{
			path:          "gs://bucket/file.siprec",
			expectCalls:   "gs",
			expectNoCalls: []string{"local", "s3", "azure"},
		},
		{
			path:          "azure://container/file.siprec",
			expectCalls:   "azure",
			expectNoCalls: []string{"local", "s3", "gcs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			// Reset
			local.deleteCalls = nil
			s3.deleteCalls = nil
			gcs.deleteCalls = nil
			azure.deleteCalls = nil

			err := multi.Delete(tt.path)
			require.NoError(t, err)

			// Check expected backend was called
			switch tt.expectCalls {
			case "s3":
				assert.NotEmpty(t, s3.deleteCalls)
			case "gs":
				assert.NotEmpty(t, gcs.deleteCalls)
			case "azure":
				assert.NotEmpty(t, azure.deleteCalls)
			}

			// Check other backends were NOT called
			for _, noCalls := range tt.expectNoCalls {
				switch noCalls {
				case "local":
					assert.Empty(t, local.deleteCalls)
				case "s3":
					assert.Empty(t, s3.deleteCalls)
				case "gs", "gcs":
					assert.Empty(t, gcs.deleteCalls)
				case "azure":
					assert.Empty(t, azure.deleteCalls)
				}
			}
		})
	}
}
