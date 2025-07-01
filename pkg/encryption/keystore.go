package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// FileKeyStore implements KeyStore interface using file system
type FileKeyStore struct {
	basePath    string
	logger      *logrus.Logger
	keys        map[string]*EncryptionKey
	mu          sync.RWMutex
	kmsProvider KMSProvider // KMS provider for key encryption
	dataKey     []byte      // Current data encryption key
	encDataKey  []byte      // Encrypted data key for storage
}

// NewFileKeyStore creates a new file-based key store
func NewFileKeyStore(basePath string, kmsProvider KMSProvider, logger *logrus.Logger) (*FileKeyStore, error) {
	if basePath == "" {
		basePath = "./keys"
	}

	// Ensure the directory exists
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key store directory: %w", err)
	}

	store := &FileKeyStore{
		basePath:    basePath,
		logger:      logger,
		keys:        make(map[string]*EncryptionKey),
		kmsProvider: kmsProvider,
	}

	// Initialize data encryption key
	if err := store.initializeDataKey(); err != nil {
		return nil, fmt.Errorf("failed to initialize data key: %w", err)
	}

	// Load existing keys
	if err := store.loadKeys(); err != nil {
		return nil, fmt.Errorf("failed to load existing keys: %w", err)
	}

	return store, nil
}

// StoreKey stores an encryption key
func (fs *FileKeyStore) StoreKey(key *EncryptionKey) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Store in memory
	fs.keys[key.ID] = key

	// Store to file
	keyFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.key", key.ID))

	// Create a safe version without the actual key data for persistence
	safeKey := &struct {
		ID        string    `json:"id"`
		Algorithm string    `json:"algorithm"`
		CreatedAt time.Time `json:"created_at"`
		ExpiresAt time.Time `json:"expires_at"`
		Version   int       `json:"version"`
		Active    bool      `json:"active"`
	}{
		ID:        key.ID,
		Algorithm: key.Algorithm,
		CreatedAt: key.CreatedAt,
		ExpiresAt: key.ExpiresAt,
		Version:   key.Version,
		Active:    key.Active,
	}

	keyData, err := json.MarshalIndent(safeKey, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal key metadata: %w", err)
	}

	if err := os.WriteFile(keyFile, keyData, 0600); err != nil {
		return fmt.Errorf("failed to write key file: %w", err)
	}

	// Store the actual key data in a separate encrypted file
	keyDataFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.keydata", key.ID))
	if err := fs.storeKeyData(keyDataFile, key.KeyData); err != nil {
		return fmt.Errorf("failed to store key data: %w", err)
	}

	fs.logger.WithFields(logrus.Fields{
		"key_id":    key.ID,
		"algorithm": key.Algorithm,
		"file":      keyFile,
	}).Debug("Stored encryption key")

	return nil
}

// GetKey retrieves an encryption key by ID
func (fs *FileKeyStore) GetKey(keyID string) (*EncryptionKey, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	key, exists := fs.keys[keyID]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", keyID)
	}

	return key, nil
}

// GetActiveKey retrieves the active key for the specified algorithm
func (fs *FileKeyStore) GetActiveKey(algorithm string) (*EncryptionKey, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var activeKey *EncryptionKey
	var latestTime time.Time

	for _, key := range fs.keys {
		if key.Algorithm == algorithm && key.Active && time.Now().Before(key.ExpiresAt) {
			if activeKey == nil || key.CreatedAt.After(latestTime) {
				activeKey = key
				latestTime = key.CreatedAt
			}
		}
	}

	return activeKey, nil
}

// ListKeys returns all stored keys
func (fs *FileKeyStore) ListKeys() ([]*EncryptionKey, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	keys := make([]*EncryptionKey, 0, len(fs.keys))
	for _, key := range fs.keys {
		keys = append(keys, key)
	}

	return keys, nil
}

// DeleteKey removes an encryption key
func (fs *FileKeyStore) DeleteKey(keyID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// Remove from memory
	delete(fs.keys, keyID)

	// Remove files
	keyFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.key", keyID))
	keyDataFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.keydata", keyID))

	if err := os.Remove(keyFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove key file: %w", err)
	}

	if err := os.Remove(keyDataFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove key data file: %w", err)
	}

	fs.logger.WithField("key_id", keyID).Debug("Deleted encryption key")

	return nil
}

// RotateKey replaces an old key with a new one
func (fs *FileKeyStore) RotateKey(oldKeyID string, newKey *EncryptionKey) error {
	fs.mu.Lock()
	
	// Mark old key as inactive
	if oldKey, exists := fs.keys[oldKeyID]; exists {
		oldKey.Active = false

		// Update the file
		keyFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.key", oldKeyID))
		safeKey := &struct {
			ID        string    `json:"id"`
			Algorithm string    `json:"algorithm"`
			CreatedAt time.Time `json:"created_at"`
			ExpiresAt time.Time `json:"expires_at"`
			Version   int       `json:"version"`
			Active    bool      `json:"active"`
		}{
			ID:        oldKey.ID,
			Algorithm: oldKey.Algorithm,
			CreatedAt: oldKey.CreatedAt,
			ExpiresAt: oldKey.ExpiresAt,
			Version:   oldKey.Version,
			Active:    false,
		}

		keyData, err := json.MarshalIndent(safeKey, "", "  ")
		if err != nil {
			fs.mu.Unlock()
			return fmt.Errorf("failed to marshal old key metadata: %w", err)
		}

		if err := os.WriteFile(keyFile, keyData, 0600); err != nil {
			fs.logger.WithError(err).WithField("key_id", oldKeyID).Warn("Failed to update old key file")
		}
	}
	
	// Unlock before calling StoreKey to avoid deadlock
	fs.mu.Unlock()

	// Store new key
	return fs.StoreKey(newKey)
}

// Private methods

func (fs *FileKeyStore) loadKeys() error {
	entries, err := os.ReadDir(fs.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Directory doesn't exist yet, no keys to load
		}
		return fmt.Errorf("failed to read key store directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".key" {
			// Skip hidden files (like .data.key)
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			
			keyID := entry.Name()[:len(entry.Name())-4] // Remove .key extension

			if err := fs.loadKey(keyID); err != nil {
				fs.logger.WithError(err).WithField("key_id", keyID).Warn("Failed to load key")
				continue
			}
		}
	}

	fs.logger.WithField("key_count", len(fs.keys)).Debug("Loaded encryption keys")
	return nil
}

func (fs *FileKeyStore) loadKey(keyID string) error {
	keyFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.key", keyID))
	keyDataFile := filepath.Join(fs.basePath, fmt.Sprintf("%s.keydata", keyID))

	// Load key metadata
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("failed to read key file: %w", err)
	}

	var keyMeta struct {
		ID        string    `json:"id"`
		Algorithm string    `json:"algorithm"`
		CreatedAt time.Time `json:"created_at"`
		ExpiresAt time.Time `json:"expires_at"`
		Version   int       `json:"version"`
		Active    bool      `json:"active"`
	}

	if err := json.Unmarshal(keyData, &keyMeta); err != nil {
		return fmt.Errorf("failed to unmarshal key metadata: %w", err)
	}

	// Load actual key data
	actualKeyData, err := fs.loadKeyData(keyDataFile)
	if err != nil {
		return fmt.Errorf("failed to load key data: %w", err)
	}

	key := &EncryptionKey{
		ID:        keyMeta.ID,
		Algorithm: keyMeta.Algorithm,
		KeyData:   actualKeyData,
		CreatedAt: keyMeta.CreatedAt,
		ExpiresAt: keyMeta.ExpiresAt,
		Version:   keyMeta.Version,
		Active:    keyMeta.Active,
	}

	fs.keys[keyID] = key
	return nil
}

func (fs *FileKeyStore) storeKeyData(filename string, keyData []byte) error {
	// Use AES-GCM encryption with a derived key from the master key
	// Generate a random nonce for this encryption
	nonce := make([]byte, 12) // GCM standard nonce size
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Use the data encryption key directly (already derived from KMS)
	dataKey := fs.getDataKey()
	if dataKey == nil {
		return fmt.Errorf("data encryption key not initialized")
	}

	// Create AES cipher
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("failed to create GCM: %w", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nil, nonce, keyData, nil)
	
	// Combine nonce and ciphertext
	result := make([]byte, len(nonce)+len(ciphertext))
	copy(result, nonce)
	copy(result[len(nonce):], ciphertext)

	// Note: dataKey is not cleared here as it's a long-lived key
	// It will be cleared when the keystore is closed or rotated

	return os.WriteFile(filename, result, 0600)
}

func (fs *FileKeyStore) loadKeyData(filename string) ([]byte, error) {
	encryptedData, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read key data file: %w", err)
	}

	// Check minimum size (nonce + tag)
	if len(encryptedData) < 12+16 {
		return nil, fmt.Errorf("invalid encrypted data: too short")
	}

	// Extract nonce and ciphertext
	nonce := encryptedData[:12]
	ciphertext := encryptedData[12:]

	// Use the data encryption key directly
	dataKey := fs.getDataKey()
	if dataKey == nil {
		return nil, fmt.Errorf("data encryption key not initialized")
	}

	// Create AES cipher
	block, err := aes.NewCipher(dataKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Decrypt the data
	keyData, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt key data: %w", err)
	}

	// Note: dataKey is not cleared here as it's a long-lived key
	// It will be cleared when the keystore is closed or rotated

	return keyData, nil
}

// initializeDataKey initializes or loads the data encryption key
func (fs *FileKeyStore) initializeDataKey() error {
	ctx := context.Background()
	dataKeyPath := filepath.Join(fs.basePath, ".data.key")
	
	// Try to load existing encrypted data key
	if encData, err := os.ReadFile(dataKeyPath); err == nil && len(encData) > 0 {
		// Decrypt the data key using KMS
		dataKey, err := fs.kmsProvider.DecryptDataKey(ctx, encData)
		if err != nil {
			return fmt.Errorf("failed to decrypt existing data key: %w", err)
		}
		
		fs.dataKey = dataKey
		fs.encDataKey = encData
		fs.logger.Debug("Loaded and decrypted existing data key")
		return nil
	}
	
	// Generate new data key using KMS
	plaintext, encrypted, err := fs.kmsProvider.GenerateDataKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate data key: %w", err)
	}
	
	fs.dataKey = plaintext
	fs.encDataKey = encrypted
	
	// Store encrypted data key
	if err := os.WriteFile(dataKeyPath, encrypted, 0600); err != nil {
		return fmt.Errorf("failed to store encrypted data key: %w", err)
	}
	
	fs.logger.Info("Generated new data encryption key")
	return nil
}

// getDataKey returns the current data encryption key
func (fs *FileKeyStore) getDataKey() []byte {
	return fs.dataKey
}

// rotateDataKey rotates the data encryption key
func (fs *FileKeyStore) rotateDataKey() error {
	ctx := context.Background()
	
	// Generate new data key
	newPlaintext, newEncrypted, err := fs.kmsProvider.GenerateDataKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate new data key: %w", err)
	}
	
	// Re-encrypt all existing keys with new data key
	for keyID := range fs.keys {
		// Decrypt with old key
		keyDataPath := filepath.Join(fs.basePath, keyID+".key")
		
		// Decrypt with old data key
		keyData, err := fs.loadKeyData(keyDataPath)
		if err != nil {
			fs.logger.WithError(err).WithField("key_id", keyID).Error("Failed to decrypt key during rotation")
			continue
		}
		
		// Update data key temporarily for encryption
		oldDataKey := fs.dataKey
		fs.dataKey = newPlaintext
		
		// Re-encrypt with new data key
		if err := fs.storeKeyData(keyDataPath, keyData); err != nil {
			fs.logger.WithError(err).WithField("key_id", keyID).Error("Failed to re-encrypt key during rotation")
			fs.dataKey = oldDataKey // Restore old key on error
			return err
		}
		
		// Clear decrypted key data
		SecureZeroMemory(keyData)
	}
	
	// Update data keys
	oldDataKey := fs.dataKey
	fs.dataKey = newPlaintext
	fs.encDataKey = newEncrypted
	
	// Store new encrypted data key
	dataKeyPath := filepath.Join(fs.basePath, ".data.key")
	if err := os.WriteFile(dataKeyPath, newEncrypted, 0600); err != nil {
		return fmt.Errorf("failed to store new encrypted data key: %w", err)
	}
	
	// Clear old key from memory
	SecureZeroMemory(oldDataKey)
	
	fs.logger.Info("Data encryption key rotated successfully")
	return nil
}

// MemoryKeyStore implements KeyStore interface using in-memory storage
// Suitable for testing and development
type MemoryKeyStore struct {
	keys map[string]*EncryptionKey
	mu   sync.RWMutex
}

// NewMemoryKeyStore creates a new memory-based key store
func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{
		keys: make(map[string]*EncryptionKey),
	}
}

// StoreKey stores an encryption key in memory
func (ms *MemoryKeyStore) StoreKey(key *EncryptionKey) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.keys[key.ID] = key
	return nil
}

// GetKey retrieves an encryption key by ID
func (ms *MemoryKeyStore) GetKey(keyID string) (*EncryptionKey, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	key, exists := ms.keys[keyID]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", keyID)
	}

	return key, nil
}

// GetActiveKey retrieves the active key for the specified algorithm
func (ms *MemoryKeyStore) GetActiveKey(algorithm string) (*EncryptionKey, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	var activeKey *EncryptionKey
	var latestTime time.Time

	for _, key := range ms.keys {
		if key.Algorithm == algorithm && key.Active && time.Now().Before(key.ExpiresAt) {
			if activeKey == nil || key.CreatedAt.After(latestTime) {
				activeKey = key
				latestTime = key.CreatedAt
			}
		}
	}

	return activeKey, nil
}

// ListKeys returns all stored keys
func (ms *MemoryKeyStore) ListKeys() ([]*EncryptionKey, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	keys := make([]*EncryptionKey, 0, len(ms.keys))
	for _, key := range ms.keys {
		keys = append(keys, key)
	}

	return keys, nil
}

// DeleteKey removes an encryption key
func (ms *MemoryKeyStore) DeleteKey(keyID string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	delete(ms.keys, keyID)
	return nil
}

// RotateKey replaces an old key with a new one
func (ms *MemoryKeyStore) RotateKey(oldKeyID string, newKey *EncryptionKey) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Mark old key as inactive
	if oldKey, exists := ms.keys[oldKeyID]; exists {
		oldKey.Active = false
	}

	// Store new key
	ms.keys[newKey.ID] = newKey
	return nil
}
