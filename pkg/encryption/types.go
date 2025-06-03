package encryption

import (
	"crypto/cipher"
	"time"
)

// EncryptionConfig holds the configuration for encryption features
type EncryptionConfig struct {
	// Enable/disable encryption
	EnableRecordingEncryption bool `json:"enable_recording_encryption" mapstructure:"enable_recording_encryption"`
	EnableMetadataEncryption  bool `json:"enable_metadata_encryption" mapstructure:"enable_metadata_encryption"`

	// Algorithm configuration
	Algorithm           string `json:"algorithm" mapstructure:"algorithm"`                         // AES-256-GCM, ChaCha20-Poly1305
	KeyDerivationMethod string `json:"key_derivation_method" mapstructure:"key_derivation_method"` // PBKDF2, Argon2id

	// Key management
	MasterKeyPath       string        `json:"master_key_path" mapstructure:"master_key_path"`
	KeyRotationInterval time.Duration `json:"key_rotation_interval" mapstructure:"key_rotation_interval"`
	KeyBackupEnabled    bool          `json:"key_backup_enabled" mapstructure:"key_backup_enabled"`

	// Security parameters
	KeySize          int `json:"key_size" mapstructure:"key_size"`                   // 256 for AES-256
	NonceSize        int `json:"nonce_size" mapstructure:"nonce_size"`               // 12 for GCM, 24 for ChaCha20
	SaltSize         int `json:"salt_size" mapstructure:"salt_size"`                 // 32 for key derivation
	PBKDF2Iterations int `json:"pbkdf2_iterations" mapstructure:"pbkdf2_iterations"` // 100000 minimum

	// Storage encryption
	EncryptionKeyStore string `json:"encryption_key_store" mapstructure:"encryption_key_store"` // file, env, vault
}

// EncryptionKey represents an encryption key with metadata
type EncryptionKey struct {
	ID        string    `json:"id"`
	Algorithm string    `json:"algorithm"`
	KeyData   []byte    `json:"-"` // Never serialize the actual key
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Version   int       `json:"version"`
	Active    bool      `json:"active"`
}

// EncryptedData represents encrypted content with metadata
type EncryptedData struct {
	Algorithm   string            `json:"algorithm"`
	KeyID       string            `json:"key_id"`
	KeyVersion  int               `json:"key_version"`
	Nonce       []byte            `json:"nonce"`
	Salt        []byte            `json:"salt,omitempty"`
	Ciphertext  []byte            `json:"ciphertext"`
	Tag         []byte            `json:"tag,omitempty"` // For AEAD modes
	Metadata    map[string]string `json:"metadata,omitempty"`
	EncryptedAt time.Time         `json:"encrypted_at"`
}

// EncryptionManager interface defines encryption operations
type EncryptionManager interface {
	// Key management
	GenerateKey(algorithm string) (*EncryptionKey, error)
	GetActiveKey(algorithm string) (*EncryptionKey, error)
	RotateKeys() error
	BackupKeys() error

	// Encryption operations
	EncryptRecording(sessionID string, audioData []byte) (*EncryptedData, error)
	DecryptRecording(sessionID string, encData *EncryptedData) ([]byte, error)

	EncryptMetadata(sessionID string, metadata map[string]interface{}) (*EncryptedData, error)
	DecryptMetadata(sessionID string, encData *EncryptedData) (map[string]interface{}, error)

	// Stream encryption for real-time data
	CreateEncryptionStream(sessionID string) (cipher.Stream, error)
	CreateDecryptionStream(sessionID string, keyID string) (cipher.Stream, error)

	// Configuration
	IsEncryptionEnabled() bool
	GetEncryptionInfo(sessionID string) (*EncryptionInfo, error)
}

// EncryptionInfo provides information about encryption for a session
type EncryptionInfo struct {
	SessionID           string    `json:"session_id"`
	RecordingEncrypted  bool      `json:"recording_encrypted"`
	MetadataEncrypted   bool      `json:"metadata_encrypted"`
	Algorithm           string    `json:"algorithm"`
	KeyID               string    `json:"key_id"`
	KeyVersion          int       `json:"key_version"`
	EncryptionStartedAt time.Time `json:"encryption_started_at"`
}

// KeyStore interface for different key storage backends
type KeyStore interface {
	StoreKey(key *EncryptionKey) error
	GetKey(keyID string) (*EncryptionKey, error)
	GetActiveKey(algorithm string) (*EncryptionKey, error)
	ListKeys() ([]*EncryptionKey, error)
	DeleteKey(keyID string) error
	RotateKey(oldKeyID string, newKey *EncryptionKey) error
}

// FileHeader represents the header for encrypted files
type FileHeader struct {
	Magic       [8]byte `json:"magic"`        // "SIPREC01"
	Version     uint16  `json:"version"`      // Format version
	Algorithm   string  `json:"algorithm"`    // Encryption algorithm
	KeyID       string  `json:"key_id"`       // Key identifier
	KeyVersion  int     `json:"key_version"`  // Key version
	NonceSize   uint16  `json:"nonce_size"`   // Size of nonce
	TagSize     uint16  `json:"tag_size"`     // Size of authentication tag
	MetadataLen uint32  `json:"metadata_len"` // Length of metadata section
	DataLen     uint64  `json:"data_len"`     // Length of encrypted data
}

// Default configuration values
const (
	DefaultAlgorithm           = "AES-256-GCM"
	DefaultKeyDerivationMethod = "PBKDF2"
	DefaultKeySize             = 32 // 256 bits
	DefaultNonceSize           = 12 // 96 bits for GCM
	DefaultSaltSize            = 32 // 256 bits
	DefaultPBKDF2Iterations    = 100000
	DefaultKeyRotationInterval = 24 * time.Hour
	DefaultEncryptionKeyStore  = "file"

	// File magic for encrypted recordings
	FileMagic = "SIPREC01"
)

// Supported algorithms
var SupportedAlgorithms = []string{
	"AES-256-GCM",
	"AES-256-CBC",
	"ChaCha20-Poly1305",
}

// GetDefaultConfig returns default encryption configuration
func GetDefaultConfig() *EncryptionConfig {
	return &EncryptionConfig{
		EnableRecordingEncryption: false,
		EnableMetadataEncryption:  false,
		Algorithm:                 DefaultAlgorithm,
		KeyDerivationMethod:       DefaultKeyDerivationMethod,
		KeySize:                   DefaultKeySize,
		NonceSize:                 DefaultNonceSize,
		SaltSize:                  DefaultSaltSize,
		PBKDF2Iterations:          DefaultPBKDF2Iterations,
		KeyRotationInterval:       DefaultKeyRotationInterval,
		EncryptionKeyStore:        DefaultEncryptionKeyStore,
		KeyBackupEnabled:          true,
	}
}
