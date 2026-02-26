package encryption

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockKMSProvider is a simple mock KMS provider for testing
type mockKMSProvider struct {
	masterKey []byte
}

func newMockKMSProvider() *mockKMSProvider {
	// Generate a fixed master key for testing
	masterKey := make([]byte, 32)
	_, _ = rand.Read(masterKey)
	return &mockKMSProvider{masterKey: masterKey}
}

func (m *mockKMSProvider) GenerateDataKey(ctx context.Context) ([]byte, []byte, error) {
	// Generate new data key
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return nil, nil, err
	}
	
	// For testing, just return the data key as both plaintext and "encrypted"
	// In a real implementation, this would encrypt dataKey with masterKey
	encrypted, err := encryptWithKey(m.masterKey, dataKey)
	if err != nil {
		return nil, nil, err
	}
	
	return dataKey, encrypted, nil
}

func (m *mockKMSProvider) DecryptDataKey(ctx context.Context, encryptedKey []byte) ([]byte, error) {
	// For testing, decrypt the data key
	return decryptWithKey(m.masterKey, encryptedKey)
}

func (m *mockKMSProvider) RotateMasterKey(ctx context.Context) error {
	// Generate new master key
	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return err
	}
	m.masterKey = newKey
	return nil
}

func TestMemoryKeyStore(t *testing.T) {
	store := NewMemoryKeyStore()

	// Test empty store
	keys, err := store.ListKeys()
	assert.NoError(t, err)
	assert.Empty(t, keys)

	// Test storing a key
	key := &EncryptionKey{
		ID:        "test-key-1",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   1,
		Active:    true,
	}

	err = store.StoreKey(key)
	assert.NoError(t, err)

	// Test retrieving the key
	retrievedKey, err := store.GetKey("test-key-1")
	assert.NoError(t, err)
	assert.Equal(t, key.ID, retrievedKey.ID)
	assert.Equal(t, key.Algorithm, retrievedKey.Algorithm)
	assert.Equal(t, key.Active, retrievedKey.Active)

	// Test getting active key
	activeKey, err := store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Equal(t, key.ID, activeKey.ID)

	// Test listing keys
	keys, err = store.ListKeys()
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Equal(t, key.ID, keys[0].ID)

	// Test deleting a key
	err = store.DeleteKey("test-key-1")
	assert.NoError(t, err)

	// Key should no longer exist
	_, err = store.GetKey("test-key-1")
	assert.Error(t, err)

	keys, err = store.ListKeys()
	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestMemoryKeyStoreRotation(t *testing.T) {
	store := NewMemoryKeyStore()

	// Create old key
	oldKey := &EncryptionKey{
		ID:        "old-key",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
		Version:   1,
		Active:    true,
	}

	err := store.StoreKey(oldKey)
	require.NoError(t, err)

	// Create new key
	newKey := &EncryptionKey{
		ID:        "new-key",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   2,
		Active:    true,
	}

	// Test rotation
	err = store.RotateKey("old-key", newKey)
	assert.NoError(t, err)

	// Old key should be inactive
	retrievedOldKey, err := store.GetKey("old-key")
	assert.NoError(t, err)
	assert.False(t, retrievedOldKey.Active)

	// New key should be active
	retrievedNewKey, err := store.GetKey("new-key")
	assert.NoError(t, err)
	assert.True(t, retrievedNewKey.Active)

	// Active key should be the new one
	activeKey, err := store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Equal(t, newKey.ID, activeKey.ID)
}

func TestFileKeyStore(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "siprec-key-store-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create file key store with mock KMS provider
	kmsProvider := newMockKMSProvider()
	store, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	require.NoError(t, err)

	// Test empty store
	keys, err := store.ListKeys()
	assert.NoError(t, err)
	assert.Empty(t, keys)

	// Test storing a key
	key := &EncryptionKey{
		ID:        "file-test-key-1",
		Algorithm: "AES-256-GCM",
		KeyData:   []byte("test-key-data-32-bytes-long!!"),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   1,
		Active:    true,
	}

	err = store.StoreKey(key)
	assert.NoError(t, err)

	// Check that files were created
	keyFile := filepath.Join(tempDir, "file-test-key-1.key")
	keyDataFile := filepath.Join(tempDir, "file-test-key-1.keydata")

	assert.FileExists(t, keyFile)
	assert.FileExists(t, keyDataFile)

	// Test retrieving the key
	retrievedKey, err := store.GetKey("file-test-key-1")
	assert.NoError(t, err)
	assert.Equal(t, key.ID, retrievedKey.ID)
	assert.Equal(t, key.Algorithm, retrievedKey.Algorithm)
	assert.Equal(t, key.KeyData, retrievedKey.KeyData)
	assert.Equal(t, key.Active, retrievedKey.Active)

	// Test persistence by creating a new store instance
	store2, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	require.NoError(t, err)

	// Key should be loaded automatically
	keys, err = store2.ListKeys()
	assert.NoError(t, err)
	assert.Len(t, keys, 1)
	assert.Equal(t, key.ID, keys[0].ID)

	// Test deleting a key
	err = store2.DeleteKey("file-test-key-1")
	assert.NoError(t, err)

	// Files should be removed
	assert.NoFileExists(t, keyFile)
	assert.NoFileExists(t, keyDataFile)
}

func TestFileKeyStoreRotation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "siprec-key-rotation-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	kmsProvider := newMockKMSProvider()
	store, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	require.NoError(t, err)

	// Create old key
	oldKey := &EncryptionKey{
		ID:        "rotation-old-key",
		Algorithm: "AES-256-GCM",
		KeyData:   []byte("old-key-data-32-bytes-long!!!"),
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		Version:   1,
		Active:    true,
	}

	err = store.StoreKey(oldKey)
	require.NoError(t, err)

	// Create new key
	newKey := &EncryptionKey{
		ID:        "rotation-new-key",
		Algorithm: "AES-256-GCM",
		KeyData:   []byte("new-key-data-32-bytes-long!!!"),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   2,
		Active:    true,
	}

	// Test rotation
	err = store.RotateKey("rotation-old-key", newKey)
	assert.NoError(t, err)

	// Verify rotation by creating new store instance
	store2, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	require.NoError(t, err)

	// Old key should be inactive
	retrievedOldKey, err := store2.GetKey("rotation-old-key")
	assert.NoError(t, err)
	assert.False(t, retrievedOldKey.Active)

	// New key should be active
	retrievedNewKey, err := store2.GetKey("rotation-new-key")
	assert.NoError(t, err)
	assert.True(t, retrievedNewKey.Active)

	// Active key should be the new one
	activeKey, err := store2.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Equal(t, newKey.ID, activeKey.ID)
}

func TestFileKeyStoreMultipleAlgorithms(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	tempDir, err := os.MkdirTemp("", "siprec-multi-algo-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	kmsProvider := newMockKMSProvider()
	store, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	require.NoError(t, err)

	// Create keys for different algorithms
	aesKey := &EncryptionKey{
		ID:        "aes-key",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   1,
		Active:    true,
	}

	chachaKey := &EncryptionKey{
		ID:        "chacha-key",
		Algorithm: "ChaCha20-Poly1305",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Version:   1,
		Active:    true,
	}

	err = store.StoreKey(aesKey)
	require.NoError(t, err)

	err = store.StoreKey(chachaKey)
	require.NoError(t, err)

	// Test getting active keys for each algorithm
	activeAES, err := store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Equal(t, aesKey.ID, activeAES.ID)

	activeChacha, err := store.GetActiveKey("ChaCha20-Poly1305")
	assert.NoError(t, err)
	assert.Equal(t, chachaKey.ID, activeChacha.ID)

	// Test getting active key for non-existent algorithm
	activeNone, err := store.GetActiveKey("INVALID-ALGO")
	assert.NoError(t, err)
	assert.Nil(t, activeNone)

	// Test listing all keys
	allKeys, err := store.ListKeys()
	assert.NoError(t, err)
	assert.Len(t, allKeys, 2)
}

func TestKeyStoreEdgeCases(t *testing.T) {
	store := NewMemoryKeyStore()

	// Test getting non-existent key
	_, err := store.GetKey("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")

	// Test deleting non-existent key
	err = store.DeleteKey("non-existent")
	assert.NoError(t, err) // Should not error

	// Test getting active key with no keys
	activeKey, err := store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Nil(t, activeKey)

	// Test with expired key
	expiredKey := &EncryptionKey{
		ID:        "expired-key",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: time.Now().Add(-25 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
		Version:   1,
		Active:    true,
	}

	err = store.StoreKey(expiredKey)
	require.NoError(t, err)

	// Should not return expired key as active
	activeKey, err = store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Nil(t, activeKey)
}

func TestFileKeyStoreCorruption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	tempDir, err := os.MkdirTemp("", "siprec-corruption-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create corrupted key file
	corruptedKeyFile := filepath.Join(tempDir, "corrupted.key")
	err = os.WriteFile(corruptedKeyFile, []byte("invalid json"), 0600)
	require.NoError(t, err)

	// Store should handle corrupted files gracefully
	kmsProvider := newMockKMSProvider()
	store, err := NewFileKeyStore(tempDir, kmsProvider, logger)
	assert.NoError(t, err) // Should not fail on corrupted files

	// Should have no keys loaded
	keys, err := store.ListKeys()
	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestConcurrentKeyStoreAccess(t *testing.T) {
	store := NewMemoryKeyStore()

	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	// Test concurrent operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				keyID := fmt.Sprintf("concurrent-key-%d-%d", id, j)

				key := &EncryptionKey{
					ID:        keyID,
					Algorithm: "AES-256-GCM",
					KeyData:   make([]byte, 32),
					CreatedAt: time.Now(),
					ExpiresAt: time.Now().Add(24 * time.Hour),
					Version:   1,
					Active:    true,
				}

				// Store key
				err := store.StoreKey(key)
				assert.NoError(t, err)

				// Retrieve key
				retrievedKey, err := store.GetKey(keyID)
				assert.NoError(t, err)
				assert.Equal(t, keyID, retrievedKey.ID)

				// Delete key
				err = store.DeleteKey(keyID)
				assert.NoError(t, err)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// All keys should be deleted
	keys, err := store.ListKeys()
	assert.NoError(t, err)
	assert.Empty(t, keys)
}

func TestKeyStoreWithMultipleVersions(t *testing.T) {
	store := NewMemoryKeyStore()

	now := time.Now()

	// Create multiple versions of keys with same algorithm
	key1 := &EncryptionKey{
		ID:        "key-v1",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: now.Add(-2 * time.Hour),
		ExpiresAt: now.Add(22 * time.Hour),
		Version:   1,
		Active:    true,
	}

	key2 := &EncryptionKey{
		ID:        "key-v2",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: now.Add(-1 * time.Hour),
		ExpiresAt: now.Add(23 * time.Hour),
		Version:   2,
		Active:    true,
	}

	key3 := &EncryptionKey{
		ID:        "key-v3",
		Algorithm: "AES-256-GCM",
		KeyData:   make([]byte, 32),
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
		Version:   3,
		Active:    true,
	}

	// Store all keys
	require.NoError(t, store.StoreKey(key1))
	require.NoError(t, store.StoreKey(key2))
	require.NoError(t, store.StoreKey(key3))

	// Should return the most recent active key
	activeKey, err := store.GetActiveKey("AES-256-GCM")
	assert.NoError(t, err)
	assert.Equal(t, key3.ID, activeKey.ID) // Most recent
}
