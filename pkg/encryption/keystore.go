package encryption

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// FileKeyStore implements KeyStore interface using file system
type FileKeyStore struct {
	basePath string
	logger   *logrus.Logger
	keys     map[string]*EncryptionKey
	mu       sync.RWMutex
}

// NewFileKeyStore creates a new file-based key store
func NewFileKeyStore(basePath string, logger *logrus.Logger) (*FileKeyStore, error) {
	if basePath == "" {
		basePath = "./keys"
	}

	// Ensure the directory exists
	if err := os.MkdirAll(basePath, 0700); err != nil {
		return nil, fmt.Errorf("failed to create key store directory: %w", err)
	}

	store := &FileKeyStore{
		basePath: basePath,
		logger:   logger,
		keys:     make(map[string]*EncryptionKey),
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
	defer fs.mu.Unlock()

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
			return fmt.Errorf("failed to marshal old key metadata: %w", err)
		}

		if err := os.WriteFile(keyFile, keyData, 0600); err != nil {
			fs.logger.WithError(err).WithField("key_id", oldKeyID).Warn("Failed to update old key file")
		}
	}

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
	// In a production implementation, this should encrypt the key data
	// with a master key or use hardware security modules
	// For this implementation, we'll use simple obfuscation
	
	obfuscated := make([]byte, len(keyData))
	for i, b := range keyData {
		obfuscated[i] = b ^ 0xAA // Simple XOR obfuscation
	}

	return os.WriteFile(filename, obfuscated, 0600)
}

func (fs *FileKeyStore) loadKeyData(filename string) ([]byte, error) {
	obfuscated, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read key data file: %w", err)
	}

	// Reverse the obfuscation
	keyData := make([]byte, len(obfuscated))
	for i, b := range obfuscated {
		keyData[i] = b ^ 0xAA
	}

	return keyData, nil
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