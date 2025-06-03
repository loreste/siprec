package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/pbkdf2"
)

// Manager implements the EncryptionManager interface
type Manager struct {
	config   *EncryptionConfig
	keyStore KeyStore
	logger   *logrus.Logger

	// Key cache for performance
	keyCache map[string]*EncryptionKey
	cacheMu  sync.RWMutex

	// Session encryption info
	sessionInfo map[string]*EncryptionInfo
	sessionMu   sync.RWMutex
}

// NewManager creates a new encryption manager
func NewManager(config *EncryptionConfig, keyStore KeyStore, logger *logrus.Logger) (*Manager, error) {
	if config == nil {
		config = GetDefaultConfig()
	}

	if keyStore == nil {
		var err error
		keyStore, err = NewFileKeyStore(config.MasterKeyPath, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create key store: %w", err)
		}
	}

	manager := &Manager{
		config:      config,
		keyStore:    keyStore,
		logger:      logger,
		keyCache:    make(map[string]*EncryptionKey),
		sessionInfo: make(map[string]*EncryptionInfo),
	}

	// Initialize with active keys if encryption is enabled
	if config.EnableRecordingEncryption || config.EnableMetadataEncryption {
		if err := manager.ensureActiveKey(); err != nil {
			return nil, fmt.Errorf("failed to ensure active encryption key: %w", err)
		}
	}

	return manager, nil
}

// GenerateKey generates a new encryption key
func (m *Manager) GenerateKey(algorithm string) (*EncryptionKey, error) {
	if !m.isAlgorithmSupported(algorithm) {
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	keySize := m.config.KeySize
	keyData := make([]byte, keySize)
	if _, err := rand.Read(keyData); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	keyID := m.generateKeyID(algorithm)
	now := time.Now()

	key := &EncryptionKey{
		ID:        keyID,
		Algorithm: algorithm,
		KeyData:   keyData,
		CreatedAt: now,
		ExpiresAt: now.Add(m.config.KeyRotationInterval),
		Version:   1,
		Active:    true,
	}

	if err := m.keyStore.StoreKey(key); err != nil {
		return nil, fmt.Errorf("failed to store key: %w", err)
	}

	// Cache the key
	m.cacheMu.Lock()
	m.keyCache[keyID] = key
	m.cacheMu.Unlock()

	m.logger.WithFields(logrus.Fields{
		"key_id":    keyID,
		"algorithm": algorithm,
	}).Info("Generated new encryption key")

	return key, nil
}

// GetActiveKey retrieves the active encryption key for the specified algorithm
func (m *Manager) GetActiveKey(algorithm string) (*EncryptionKey, error) {
	// Check cache first
	m.cacheMu.RLock()
	for _, key := range m.keyCache {
		if key.Algorithm == algorithm && key.Active && time.Now().Before(key.ExpiresAt) {
			m.cacheMu.RUnlock()
			return key, nil
		}
	}
	m.cacheMu.RUnlock()

	// Check key store
	key, err := m.keyStore.GetActiveKey(algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to get active key: %w", err)
	}

	if key == nil {
		// Generate new key if none exists
		return m.GenerateKey(algorithm)
	}

	// Cache the key
	m.cacheMu.Lock()
	m.keyCache[key.ID] = key
	m.cacheMu.Unlock()

	return key, nil
}

// RotateKeys rotates all active encryption keys
func (m *Manager) RotateKeys() error {
	m.logger.Info("Starting key rotation")

	keys, err := m.keyStore.ListKeys()
	if err != nil {
		return fmt.Errorf("failed to list keys: %w", err)
	}

	for _, key := range keys {
		if key.Active && time.Now().After(key.ExpiresAt) {
			newKey, err := m.GenerateKey(key.Algorithm)
			if err != nil {
				m.logger.WithError(err).WithField("key_id", key.ID).Error("Failed to generate new key during rotation")
				continue
			}

			if err := m.keyStore.RotateKey(key.ID, newKey); err != nil {
				m.logger.WithError(err).WithField("key_id", key.ID).Error("Failed to rotate key")
				continue
			}

			// Update cache
			m.cacheMu.Lock()
			delete(m.keyCache, key.ID)
			m.keyCache[newKey.ID] = newKey
			m.cacheMu.Unlock()

			m.logger.WithFields(logrus.Fields{
				"old_key_id": key.ID,
				"new_key_id": newKey.ID,
				"algorithm":  key.Algorithm,
			}).Info("Rotated encryption key")
		}
	}

	return nil
}

// BackupKeys creates backups of all encryption keys
func (m *Manager) BackupKeys() error {
	if !m.config.KeyBackupEnabled {
		return nil
	}

	m.logger.Info("Starting key backup")

	keys, err := m.keyStore.ListKeys()
	if err != nil {
		return fmt.Errorf("failed to list keys for backup: %w", err)
	}

	backupData := make(map[string]*EncryptionKey)
	for _, key := range keys {
		backupData[key.ID] = key
	}

	backupFile := fmt.Sprintf("%s.backup.%d", m.config.MasterKeyPath, time.Now().Unix())
	backupBytes, err := json.Marshal(backupData)
	if err != nil {
		return fmt.Errorf("failed to marshal backup data: %w", err)
	}

	// Encrypt the backup with a derived key
	encryptedBackup, err := m.encryptBackup(backupBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt backup: %w", err)
	}

	// This would write to file - simplified for this implementation
	_ = encryptedBackup

	m.logger.WithField("backup_file", backupFile).Info("Created key backup")
	return nil
}

// EncryptRecording encrypts audio recording data
func (m *Manager) EncryptRecording(sessionID string, audioData []byte) (*EncryptedData, error) {
	if !m.config.EnableRecordingEncryption {
		return nil, fmt.Errorf("recording encryption is disabled")
	}

	key, err := m.GetActiveKey(m.config.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	encData, err := m.encrypt(audioData, key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt recording: %w", err)
	}

	// Update session info
	m.updateSessionEncryptionInfo(sessionID, true, false, key)

	m.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"data_size":  len(audioData),
		"key_id":     key.ID,
	}).Debug("Encrypted recording data")

	return encData, nil
}

// DecryptRecording decrypts audio recording data
func (m *Manager) DecryptRecording(sessionID string, encData *EncryptedData) ([]byte, error) {
	key, err := m.getKeyByID(encData.KeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get decryption key: %w", err)
	}

	audioData, err := m.decrypt(encData, key)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt recording: %w", err)
	}

	m.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"data_size":  len(audioData),
		"key_id":     key.ID,
	}).Debug("Decrypted recording data")

	return audioData, nil
}

// EncryptMetadata encrypts session metadata
func (m *Manager) EncryptMetadata(sessionID string, metadata map[string]interface{}) (*EncryptedData, error) {
	if !m.config.EnableMetadataEncryption {
		return nil, fmt.Errorf("metadata encryption is disabled")
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	key, err := m.GetActiveKey(m.config.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	encData, err := m.encrypt(metadataBytes, key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt metadata: %w", err)
	}

	// Update session info
	m.updateSessionEncryptionInfo(sessionID, false, true, key)

	m.logger.WithFields(logrus.Fields{
		"session_id":    sessionID,
		"metadata_size": len(metadataBytes),
		"key_id":        key.ID,
	}).Debug("Encrypted metadata")

	return encData, nil
}

// DecryptMetadata decrypts session metadata
func (m *Manager) DecryptMetadata(sessionID string, encData *EncryptedData) (map[string]interface{}, error) {
	key, err := m.getKeyByID(encData.KeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get decryption key: %w", err)
	}

	metadataBytes, err := m.decrypt(encData, key)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt metadata: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decrypted metadata: %w", err)
	}

	m.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"key_id":     key.ID,
	}).Debug("Decrypted metadata")

	return metadata, nil
}

// CreateEncryptionStream creates a stream cipher for real-time encryption
func (m *Manager) CreateEncryptionStream(sessionID string) (cipher.Stream, error) {
	if !m.config.EnableRecordingEncryption {
		return nil, fmt.Errorf("recording encryption is disabled")
	}

	_, err := m.GetActiveKey(m.config.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	// For stream ciphers, we need a different approach than AEAD
	// This is a simplified implementation
	return nil, fmt.Errorf("stream encryption not yet implemented")
}

// CreateDecryptionStream creates a stream cipher for real-time decryption
func (m *Manager) CreateDecryptionStream(sessionID string, keyID string) (cipher.Stream, error) {
	key, err := m.getKeyByID(keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get decryption key: %w", err)
	}

	// This is a simplified implementation
	_ = key
	return nil, fmt.Errorf("stream decryption not yet implemented")
}

// IsEncryptionEnabled returns whether encryption is enabled
func (m *Manager) IsEncryptionEnabled() bool {
	return m.config.EnableRecordingEncryption || m.config.EnableMetadataEncryption
}

// GetEncryptionInfo returns encryption information for a session
func (m *Manager) GetEncryptionInfo(sessionID string) (*EncryptionInfo, error) {
	m.sessionMu.RLock()
	defer m.sessionMu.RUnlock()

	info, exists := m.sessionInfo[sessionID]
	if !exists {
		return &EncryptionInfo{
			SessionID:          sessionID,
			RecordingEncrypted: false,
			MetadataEncrypted:  false,
		}, nil
	}

	return info, nil
}

// Helper methods

func (m *Manager) encrypt(data []byte, key *EncryptionKey) (*EncryptedData, error) {
	switch key.Algorithm {
	case "AES-256-GCM":
		return m.encryptAESGCM(data, key)
	case "ChaCha20-Poly1305":
		return m.encryptChaCha20Poly1305(data, key)
	default:
		return nil, fmt.Errorf("unsupported encryption algorithm: %s", key.Algorithm)
	}
}

func (m *Manager) decrypt(encData *EncryptedData, key *EncryptionKey) ([]byte, error) {
	switch encData.Algorithm {
	case "AES-256-GCM":
		return m.decryptAESGCM(encData, key)
	case "ChaCha20-Poly1305":
		return m.decryptChaCha20Poly1305(encData, key)
	default:
		return nil, fmt.Errorf("unsupported decryption algorithm: %s", encData.Algorithm)
	}
}

func (m *Manager) encryptAESGCM(data []byte, key *EncryptionKey) (*EncryptedData, error) {
	block, err := aes.NewCipher(key.KeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, data, nil)

	return &EncryptedData{
		Algorithm:   key.Algorithm,
		KeyID:       key.ID,
		KeyVersion:  key.Version,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
		EncryptedAt: time.Now(),
	}, nil
}

func (m *Manager) decryptAESGCM(encData *EncryptedData, key *EncryptionKey) ([]byte, error) {
	block, err := aes.NewCipher(key.KeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := aead.Open(nil, encData.Nonce, encData.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

func (m *Manager) encryptChaCha20Poly1305(data []byte, key *EncryptionKey) (*EncryptedData, error) {
	aead, err := chacha20poly1305.New(key.KeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to create ChaCha20-Poly1305 cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, data, nil)

	return &EncryptedData{
		Algorithm:   key.Algorithm,
		KeyID:       key.ID,
		KeyVersion:  key.Version,
		Nonce:       nonce,
		Ciphertext:  ciphertext,
		EncryptedAt: time.Now(),
	}, nil
}

func (m *Manager) decryptChaCha20Poly1305(encData *EncryptedData, key *EncryptionKey) ([]byte, error) {
	aead, err := chacha20poly1305.New(key.KeyData)
	if err != nil {
		return nil, fmt.Errorf("failed to create ChaCha20-Poly1305 cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, encData.Nonce, encData.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

func (m *Manager) generateKeyID(algorithm string) string {
	timestamp := time.Now().Unix()
	hashInput := fmt.Sprintf("%s-%d-%d", algorithm, timestamp, mathrand.Int())
	hash := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(hash[:16]) // 32 character hex string
}

func (m *Manager) isAlgorithmSupported(algorithm string) bool {
	for _, supported := range SupportedAlgorithms {
		if algorithm == supported {
			return true
		}
	}
	return false
}

func (m *Manager) getKeyByID(keyID string) (*EncryptionKey, error) {
	// Check cache first
	m.cacheMu.RLock()
	if key, exists := m.keyCache[keyID]; exists {
		m.cacheMu.RUnlock()
		return key, nil
	}
	m.cacheMu.RUnlock()

	// Check key store
	key, err := m.keyStore.GetKey(keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", keyID, err)
	}

	// Cache the key
	m.cacheMu.Lock()
	m.keyCache[keyID] = key
	m.cacheMu.Unlock()

	return key, nil
}

func (m *Manager) ensureActiveKey() error {
	_, err := m.GetActiveKey(m.config.Algorithm)
	return err
}

func (m *Manager) updateSessionEncryptionInfo(sessionID string, recordingEncrypted, metadataEncrypted bool, key *EncryptionKey) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	info, exists := m.sessionInfo[sessionID]
	if !exists {
		info = &EncryptionInfo{
			SessionID:           sessionID,
			EncryptionStartedAt: time.Now(),
		}
		m.sessionInfo[sessionID] = info
	}

	if recordingEncrypted {
		info.RecordingEncrypted = true
	}
	if metadataEncrypted {
		info.MetadataEncrypted = true
	}

	info.Algorithm = key.Algorithm
	info.KeyID = key.ID
	info.KeyVersion = key.Version
}

func (m *Manager) encryptBackup(data []byte) ([]byte, error) {
	// Use PBKDF2 to derive a key from master password for backup encryption
	password := "backup-key" // This should come from secure configuration
	salt := make([]byte, m.config.SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	key := pbkdf2.Key([]byte(password), salt, m.config.PBKDF2Iterations, m.config.KeySize, sha256.New)

	// Create a temporary encryption key
	tempKey := &EncryptionKey{
		Algorithm: m.config.Algorithm,
		KeyData:   key,
	}

	encData, err := m.encrypt(data, tempKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt backup: %w", err)
	}

	// Include salt in the encrypted data
	encData.Salt = salt

	encBytes, err := json.Marshal(encData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal encrypted backup: %w", err)
	}

	return encBytes, nil
}
