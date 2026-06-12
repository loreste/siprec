package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/chacha20"
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

	// Stream session state
	streamInfo map[string]*streamState
	streamMu   sync.RWMutex

	// Key backup state
	backupMu        sync.Mutex
	lastBackupPaths []string
}

// streamState tracks metadata required to recreate stream ciphers
type streamState struct {
	keyID     string
	algorithm string
	nonce     []byte
	createdAt time.Time
}

// NewManager creates a new encryption manager
func NewManager(config *EncryptionConfig, keyStore KeyStore, logger *logrus.Logger) (*Manager, error) {
	if config == nil {
		config = GetDefaultConfig()
	}

	if keyStore == nil {
		var err error
		// Create a local KMS provider for the file keystore
		kmsProvider, err := NewLocalKMSProvider(config.MasterKeyPath, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create KMS provider: %w", err)
		}

		keyStore, err = NewFileKeyStore(config.MasterKeyPath, kmsProvider, logger)
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
		streamInfo:  make(map[string]*streamState),
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

// keyBackupRecord is the serialized form of a key inside an encrypted backup.
// Unlike EncryptionKey, it intentionally includes the raw key material because
// the entire record is encrypted with a key derived from the backup password
// before it is written to disk.
type keyBackupRecord struct {
	ID         string    `json:"id"`
	Algorithm  string    `json:"algorithm"`
	KeyData    []byte    `json:"key_data"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Version    int       `json:"version"`
	Active     bool      `json:"active"`
	BackedUpAt time.Time `json:"backed_up_at"`
}

// BackupKeys creates encrypted backups of all encryption keys.
// One file per key is written atomically to the configured backup directory
// with 0600 permissions, named "<keyID>-<timestamp>.keybak". The paths of the
// most recent backup run are recorded and available via LastBackupPaths.
func (m *Manager) BackupKeys() error {
	if !m.config.KeyBackupEnabled {
		return nil
	}

	m.logger.Info("Starting key backup")

	keys, err := m.keyStore.ListKeys()
	if err != nil {
		return fmt.Errorf("failed to list keys for backup: %w", err)
	}

	backupDir := m.backupDirectory()
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("failed to create backup directory %s: %w", backupDir, err)
	}

	timestamp := time.Now().UTC().Format("20060102T150405Z")
	paths := make([]string, 0, len(keys))

	for _, key := range keys {
		record := &keyBackupRecord{
			ID:         key.ID,
			Algorithm:  key.Algorithm,
			KeyData:    key.KeyData,
			CreatedAt:  key.CreatedAt,
			ExpiresAt:  key.ExpiresAt,
			Version:    key.Version,
			Active:     key.Active,
			BackedUpAt: time.Now().UTC(),
		}

		recordBytes, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal backup record for key %s: %w", key.ID, err)
		}

		// Encrypt the backup with a key derived from the backup password
		encryptedBackup, err := m.encryptBackup(recordBytes)
		if err != nil {
			return fmt.Errorf("failed to encrypt backup for key %s: %w", key.ID, err)
		}

		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-%s.keybak", key.ID, timestamp))
		if err := writeFileAtomic(backupPath, encryptedBackup, 0600); err != nil {
			return fmt.Errorf("failed to write backup for key %s: %w", key.ID, err)
		}

		paths = append(paths, backupPath)

		m.logger.WithFields(logrus.Fields{
			"key_id":      key.ID,
			"backup_file": backupPath,
		}).Info("Created key backup")
	}

	m.backupMu.Lock()
	m.lastBackupPaths = paths
	m.backupMu.Unlock()

	m.logger.WithFields(logrus.Fields{
		"backup_dir": backupDir,
		"key_count":  len(paths),
	}).Info("Key backup completed")

	return nil
}

// LastBackupPaths returns the file paths written by the most recent
// successful BackupKeys run.
func (m *Manager) LastBackupPaths() []string {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()
	return append([]string(nil), m.lastBackupPaths...)
}

// RestoreKeyFromBackup reads an encrypted key backup file, decrypts it with
// the configured backup password, validates the restored key, stores it in
// the key store, and returns it.
func (m *Manager) RestoreKeyFromBackup(path string) (*EncryptionKey, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %w", err)
	}

	recordBytes, err := m.decryptBackup(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt backup: %w", err)
	}

	var record keyBackupRecord
	if err := json.Unmarshal(recordBytes, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup record: %w", err)
	}

	// Validate the restored key before accepting it
	if record.ID == "" {
		return nil, fmt.Errorf("invalid backup: missing key ID")
	}
	if !m.isAlgorithmSupported(record.Algorithm) {
		return nil, fmt.Errorf("invalid backup: unsupported algorithm %q", record.Algorithm)
	}
	if len(record.KeyData) == 0 {
		return nil, fmt.Errorf("invalid backup: missing key material")
	}
	if len(record.KeyData) != m.config.KeySize {
		return nil, fmt.Errorf("invalid backup: key size %d does not match configured size %d", len(record.KeyData), m.config.KeySize)
	}

	key := &EncryptionKey{
		ID:        record.ID,
		Algorithm: record.Algorithm,
		KeyData:   record.KeyData,
		CreatedAt: record.CreatedAt,
		ExpiresAt: record.ExpiresAt,
		Version:   record.Version,
		Active:    record.Active,
	}

	if err := m.keyStore.StoreKey(key); err != nil {
		return nil, fmt.Errorf("failed to store restored key: %w", err)
	}

	// Cache the restored key
	m.cacheMu.Lock()
	m.keyCache[key.ID] = key
	m.cacheMu.Unlock()

	m.logger.WithFields(logrus.Fields{
		"key_id":      key.ID,
		"algorithm":   key.Algorithm,
		"backup_file": path,
	}).Info("Restored encryption key from backup")

	return key, nil
}

// backupDirectory returns the configured backup directory, defaulting to
// <MasterKeyPath>/backups when not set.
func (m *Manager) backupDirectory() string {
	if m.config.KeyBackupDir != "" {
		return m.config.KeyBackupDir
	}

	base := m.config.MasterKeyPath
	if base == "" {
		base = "./keys"
	}
	return filepath.Join(base, "backups")
}

// writeFileAtomic writes data to path atomically: it writes to a temporary
// file in the same directory, fsyncs it, then renames it into place.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmpFile, err := os.CreateTemp(dir, ".keybak-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpName := tmpFile.Name()

	// Clean up the temporary file on any failure path
	cleanup := func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}

	if err := tmpFile.Chmod(perm); err != nil {
		cleanup()
		return fmt.Errorf("failed to set permissions on temporary file: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		cleanup()
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to rename temporary file into place: %w", err)
	}

	// Fsync the directory so the rename itself is durable (best-effort)
	if dirFile, err := os.Open(filepath.Clean(dir)); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

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

	key, err := m.GetActiveKey(m.config.Algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to get encryption key: %w", err)
	}

	stream, nonce, err := m.newStreamCipher(key, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryption stream: %w", err)
	}

	// Track stream state so decryption stream can be recreated
	m.streamMu.Lock()
	m.streamInfo[sessionID] = &streamState{
		keyID:     key.ID,
		algorithm: key.Algorithm,
		nonce:     append([]byte(nil), nonce...),
		createdAt: time.Now(),
	}
	m.streamMu.Unlock()

	// Update session encryption info to reflect stream usage
	m.updateSessionEncryptionInfo(sessionID, true, false, key)
	m.updateStreamEncryptionInfo(sessionID, key, nonce)

	m.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"key_id":     key.ID,
		"algorithm":  key.Algorithm,
	}).Debug("Created encryption stream")

	return stream, nil
}

// CreateDecryptionStream creates a stream cipher for real-time decryption
func (m *Manager) CreateDecryptionStream(sessionID string, keyID string) (cipher.Stream, error) {
	m.streamMu.RLock()
	state, exists := m.streamInfo[sessionID]
	m.streamMu.RUnlock()
	if !exists {
		rawInfo, infoExists := m.getRawEncryptionInfo(sessionID)
		if !infoExists || !rawInfo.StreamEncryption || len(rawInfo.StreamNonce) == 0 {
			return nil, fmt.Errorf("stream state not found for session %s", sessionID)
		}

		if keyID != "" && keyID != rawInfo.KeyID {
			return nil, fmt.Errorf("stream state key mismatch for session %s", sessionID)
		}

		key, err := m.getKeyByID(rawInfo.KeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to get decryption key: %w", err)
		}

		stream, _, err := m.newStreamCipher(key, rawInfo.StreamNonce)
		if err != nil {
			return nil, fmt.Errorf("failed to create decryption stream: %w", err)
		}

		state = &streamState{
			keyID:     key.ID,
			algorithm: key.Algorithm,
			nonce:     append([]byte(nil), rawInfo.StreamNonce...),
			createdAt: time.Now(),
		}

		m.streamMu.Lock()
		m.streamInfo[sessionID] = state
		m.streamMu.Unlock()

		m.logger.WithFields(logrus.Fields{
			"session_id": sessionID,
			"key_id":     key.ID,
			"algorithm":  key.Algorithm,
		}).Debug("Rehydrated decryption stream from session info")

		return stream, nil
	}

	if keyID != "" && keyID != state.keyID {
		return nil, fmt.Errorf("stream state key mismatch for session %s", sessionID)
	}

	key, err := m.getKeyByID(state.keyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get decryption key: %w", err)
	}

	stream, _, err := m.newStreamCipher(key, state.nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to create decryption stream: %w", err)
	}

	m.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"key_id":     key.ID,
		"algorithm":  key.Algorithm,
	}).Debug("Created decryption stream")

	return stream, nil
}

// CleanupSession releases cached encryption metadata for a session
func (m *Manager) CleanupSession(sessionID string) {
	m.sessionMu.Lock()
	delete(m.sessionInfo, sessionID)
	m.sessionMu.Unlock()

	m.streamMu.Lock()
	delete(m.streamInfo, sessionID)
	m.streamMu.Unlock()
}

// IsEncryptionEnabled returns whether encryption is enabled
func (m *Manager) IsEncryptionEnabled() bool {
	return m.config.EnableRecordingEncryption || m.config.EnableMetadataEncryption
}

// GetEncryptionInfo returns encryption information for a session
func (m *Manager) GetEncryptionInfo(sessionID string) (*EncryptionInfo, error) {
	m.sessionMu.RLock()
	info, exists := m.sessionInfo[sessionID]
	if !exists {
		m.sessionMu.RUnlock()
		return &EncryptionInfo{
			SessionID:          sessionID,
			RecordingEncrypted: false,
			MetadataEncrypted:  false,
		}, nil
	}

	sanitized := cloneEncryptionInfo(info, true)
	m.sessionMu.RUnlock()

	return sanitized, nil
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

	// #nosec G407 -- using AES-256-GCM which is a secure modern algorithm
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

	// #nosec G407 -- using ChaCha20-Poly1305 which is a secure modern algorithm
	plaintext, err := aead.Open(nil, encData.Nonce, encData.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}

	return plaintext, nil
}

func (m *Manager) generateKeyID(algorithm string) string {
	timestamp := time.Now().Unix()
	randomBytes := make([]byte, 16)
	// #nosec G104 -- rand.Read always returns len(p) and nil error on supported platforms
	rand.Read(randomBytes)
	hashInput := fmt.Sprintf("%s-%d-%s", algorithm, timestamp, hex.EncodeToString(randomBytes))
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

func (m *Manager) updateStreamEncryptionInfo(sessionID string, key *EncryptionKey, nonce []byte) {
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

	info.StreamEncryption = true
	info.StreamNonce = append([]byte(nil), nonce...)
	now := time.Now()
	info.StreamCreatedAt = &now
	info.Algorithm = key.Algorithm
	info.KeyID = key.ID
	info.KeyVersion = key.Version
	hash := sha256.Sum256(info.StreamNonce)
	info.StreamNonceHash = hex.EncodeToString(hash[:])
}

func (m *Manager) getRawEncryptionInfo(sessionID string) (*EncryptionInfo, bool) {
	m.sessionMu.RLock()
	info, exists := m.sessionInfo[sessionID]
	m.sessionMu.RUnlock()
	if !exists {
		return nil, false
	}

	return cloneEncryptionInfo(info, false), true
}

func (m *Manager) newStreamCipher(key *EncryptionKey, providedNonce []byte) (cipher.Stream, []byte, error) {
	switch key.Algorithm {
	case "AES-256-GCM", "AES-256-CBC":
		block, err := aes.NewCipher(key.KeyData)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create AES cipher: %w", err)
		}

		nonce := providedNonce
		if nonce == nil {
			nonce = make([]byte, aes.BlockSize)
			if _, err := rand.Read(nonce); err != nil {
				return nil, nil, fmt.Errorf("failed to generate stream nonce: %w", err)
			}
		} else if len(nonce) != aes.BlockSize {
			return nil, nil, fmt.Errorf("invalid nonce size for AES stream: %d", len(nonce))
		} else {
			nonce = append([]byte(nil), nonce...)
		}

		stream := cipher.NewCTR(block, nonce)
		return stream, nonce, nil

	case "ChaCha20-Poly1305":
		nonce := providedNonce
		if nonce == nil {
			nonce = make([]byte, chacha20.NonceSize)
			if _, err := rand.Read(nonce); err != nil {
				return nil, nil, fmt.Errorf("failed to generate stream nonce: %w", err)
			}
		} else if len(nonce) != chacha20.NonceSize {
			return nil, nil, fmt.Errorf("invalid nonce size for ChaCha20 stream: %d", len(nonce))
		} else {
			nonce = append([]byte(nil), nonce...)
		}

		stream, err := chacha20.NewUnauthenticatedCipher(key.KeyData, nonce)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create ChaCha20 stream: %w", err)
		}

		return stream, nonce, nil
	default:
		return nil, nil, fmt.Errorf("unsupported stream algorithm: %s", key.Algorithm)
	}
}

func cloneEncryptionInfo(info *EncryptionInfo, sanitize bool) *EncryptionInfo {
	if info == nil {
		return nil
	}

	copyInfo := *info
	if info.StreamCreatedAt != nil {
		ts := *info.StreamCreatedAt
		copyInfo.StreamCreatedAt = &ts
	}

	if info.StreamNonce != nil {
		copyInfo.StreamNonce = append([]byte(nil), info.StreamNonce...)
	}

	if sanitize {
		copyInfo.StreamNonce = nil
	}

	return &copyInfo
}

func (m *Manager) encryptBackup(data []byte) ([]byte, error) {
	// Use PBKDF2 to derive a key from master password for backup encryption
	password := m.config.BackupPassword
	if password == "" {
		return nil, fmt.Errorf("backup password not configured: set ENCRYPTION_BACKUP_PASSWORD environment variable")
	}

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

	// Include salt and KDF parameters in the encrypted data so the backup
	// remains restorable even if the configuration changes later
	encData.Salt = salt
	encData.Metadata = map[string]string{
		"kdf":        "PBKDF2-SHA256",
		"iterations": strconv.Itoa(m.config.PBKDF2Iterations),
		"key_size":   strconv.Itoa(m.config.KeySize),
	}

	encBytes, err := json.Marshal(encData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal encrypted backup: %w", err)
	}

	return encBytes, nil
}

// decryptBackup decrypts an encrypted key backup produced by encryptBackup
func (m *Manager) decryptBackup(encBytes []byte) ([]byte, error) {
	password := m.config.BackupPassword
	if password == "" {
		return nil, fmt.Errorf("backup password not configured: set ENCRYPTION_BACKUP_PASSWORD environment variable")
	}

	var encData EncryptedData
	if err := json.Unmarshal(encBytes, &encData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal encrypted backup: %w", err)
	}

	if len(encData.Salt) == 0 {
		return nil, fmt.Errorf("invalid backup: missing key derivation salt")
	}

	// Recover KDF parameters from the backup metadata, falling back to the
	// current configuration for backups without embedded parameters
	iterations := m.config.PBKDF2Iterations
	keySize := m.config.KeySize
	if encData.Metadata != nil {
		if v, err := strconv.Atoi(encData.Metadata["iterations"]); err == nil && v > 0 {
			iterations = v
		}
		if v, err := strconv.Atoi(encData.Metadata["key_size"]); err == nil && v > 0 {
			keySize = v
		}
	}

	key := pbkdf2.Key([]byte(password), encData.Salt, iterations, keySize, sha256.New)

	tempKey := &EncryptionKey{
		Algorithm: encData.Algorithm,
		KeyData:   key,
	}

	plaintext, err := m.decrypt(&encData, tempKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt backup: %w", err)
	}

	return plaintext, nil
}
