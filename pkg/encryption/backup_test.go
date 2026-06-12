package encryption

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBackupTestConfig(t *testing.T) *EncryptionConfig {
	t.Helper()

	config := GetDefaultConfig()
	config.BackupPassword = "test-backup-password"
	config.KeyBackupDir = t.TempDir()
	config.KeyBackupEnabled = true
	// Lower iterations to keep the test fast while still exercising the KDF
	config.PBKDF2Iterations = 1000
	return config
}

func TestBackupKeysWritesEncryptedFiles(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := newBackupTestConfig(t)
	manager, err := NewManager(config, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	key, err := manager.GenerateKey("AES-256-GCM")
	require.NoError(t, err)

	require.NoError(t, manager.BackupKeys())

	paths := manager.LastBackupPaths()
	require.Len(t, paths, 1)

	// File name pattern: <keyID>-<timestamp>.keybak
	pattern := regexp.MustCompile(`^` + regexp.QuoteMeta(key.ID) + `-\d{8}T\d{6}Z\.keybak$`)
	assert.True(t, pattern.MatchString(filepath.Base(paths[0])), "unexpected backup file name: %s", paths[0])
	assert.Equal(t, config.KeyBackupDir, filepath.Dir(paths[0]))

	// File must exist with 0600 permissions
	info, err := os.Stat(paths[0])
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Backup contents must be encrypted (no raw key material on disk)
	data, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	assert.NotContains(t, string(data), string(key.KeyData))

	// No leftover temporary files
	entries, err := os.ReadDir(config.KeyBackupDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := newBackupTestConfig(t)
	manager, err := NewManager(config, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	original, err := manager.GenerateKey("AES-256-GCM")
	require.NoError(t, err)

	require.NoError(t, manager.BackupKeys())
	paths := manager.LastBackupPaths()
	require.Len(t, paths, 1)

	// Restore into a completely fresh manager/key store
	restoredManager, err := NewManager(config, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	restored, err := restoredManager.RestoreKeyFromBackup(paths[0])
	require.NoError(t, err)

	assert.Equal(t, original.ID, restored.ID)
	assert.Equal(t, original.Algorithm, restored.Algorithm)
	assert.Equal(t, original.KeyData, restored.KeyData)
	assert.Equal(t, original.Version, restored.Version)
	assert.Equal(t, original.Active, restored.Active)
	assert.WithinDuration(t, original.CreatedAt, restored.CreatedAt, 0)
	assert.WithinDuration(t, original.ExpiresAt, restored.ExpiresAt, 0)

	// The restored key must be usable for decryption of data encrypted
	// with the original key
	encData, err := manager.encrypt([]byte("audio payload"), original)
	require.NoError(t, err)

	plaintext, err := restoredManager.decrypt(encData, restored)
	require.NoError(t, err)
	assert.Equal(t, []byte("audio payload"), plaintext)
}

func TestRestoreKeyFromBackupWrongPassword(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := newBackupTestConfig(t)
	manager, err := NewManager(config, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	_, err = manager.GenerateKey("AES-256-GCM")
	require.NoError(t, err)
	require.NoError(t, manager.BackupKeys())
	paths := manager.LastBackupPaths()
	require.Len(t, paths, 1)

	wrongConfig := *config
	wrongConfig.BackupPassword = "wrong-password"
	wrongManager, err := NewManager(&wrongConfig, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	_, err = wrongManager.RestoreKeyFromBackup(paths[0])
	assert.Error(t, err)
}

func TestBackupKeysDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := newBackupTestConfig(t)
	config.KeyBackupEnabled = false

	manager, err := NewManager(config, NewMemoryKeyStore(), logger)
	require.NoError(t, err)

	_, err = manager.GenerateKey("AES-256-GCM")
	require.NoError(t, err)

	require.NoError(t, manager.BackupKeys())
	assert.Empty(t, manager.LastBackupPaths())

	entries, err := os.ReadDir(config.KeyBackupDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
