package encryption

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

// KMSProvider defines the interface for key management services
type KMSProvider interface {
	// GenerateDataKey generates a new data encryption key
	GenerateDataKey(ctx context.Context) ([]byte, []byte, error)

	// DecryptDataKey decrypts an encrypted data key
	DecryptDataKey(ctx context.Context, encryptedKey []byte) ([]byte, error)

	// RotateMasterKey rotates the master key
	RotateMasterKey(ctx context.Context) error
}

// LocalKMSProvider implements KMSProvider using local secure storage
// This is for environments without cloud KMS access
type LocalKMSProvider struct {
	masterKeyPath string
	logger        *logrus.Logger
	masterKey     []byte
	mu            sync.RWMutex
}

// NewLocalKMSProvider creates a new local KMS provider
func NewLocalKMSProvider(keyPath string, logger *logrus.Logger) (*LocalKMSProvider, error) {
	provider := &LocalKMSProvider{
		masterKeyPath: keyPath,
		logger:        logger,
	}

	// Initialize master key
	if err := provider.initializeMasterKey(); err != nil {
		return nil, err
	}

	return provider, nil
}

// initializeMasterKey loads or generates the master key
func (p *LocalKMSProvider) initializeMasterKey() error {
	// Try to load from environment variable first (for production)
	if masterKeyB64 := os.Getenv("SIPREC_MASTER_KEY"); masterKeyB64 != "" {
		masterKey, err := base64.StdEncoding.DecodeString(masterKeyB64)
		if err != nil {
			return fmt.Errorf("failed to decode master key from env: %w", err)
		}
		if len(masterKey) != 32 {
			return fmt.Errorf("invalid master key length: expected 32, got %d", len(masterKey))
		}
		p.masterKey = masterKey
		p.logger.Info("Loaded master key from environment variable")
		return nil
	}

	// Try to load from file
	if data, err := os.ReadFile(p.masterKeyPath); err == nil && len(data) == 32 {
		p.masterKey = data
		p.logger.Info("Loaded master key from file")
		return nil
	}

	// Generate new master key
	p.masterKey = make([]byte, 32)
	if _, err := rand.Read(p.masterKey); err != nil {
		return fmt.Errorf("failed to generate master key: %w", err)
	}

	// Store for development only
	if err := os.WriteFile(p.masterKeyPath, p.masterKey, 0600); err != nil {
		return fmt.Errorf("failed to store master key: %w", err)
	}

	p.logger.Warn("Generated new master key - ensure this is properly backed up")
	return nil
}

// GenerateDataKey generates a new data encryption key
func (p *LocalKMSProvider) GenerateDataKey(ctx context.Context) ([]byte, []byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Generate new data key
	dataKey := make([]byte, 32)
	if _, err := rand.Read(dataKey); err != nil {
		return nil, nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	// Encrypt data key with master key using AES-GCM
	encrypted, err := encryptWithKey(p.masterKey, dataKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encrypt data key: %w", err)
	}

	return dataKey, encrypted, nil
}

// DecryptDataKey decrypts an encrypted data key
func (p *LocalKMSProvider) DecryptDataKey(ctx context.Context, encryptedKey []byte) ([]byte, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	plaintext, err := decryptWithKey(p.masterKey, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	return plaintext, nil
}

// RotateMasterKey rotates the master key
func (p *LocalKMSProvider) RotateMasterKey(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate new master key
	newMasterKey := make([]byte, 32)
	if _, err := rand.Read(newMasterKey); err != nil {
		return fmt.Errorf("failed to generate new master key: %w", err)
	}

	// In production, this would re-encrypt all data keys
	// For now, just update the master key
	oldKey := p.masterKey
	p.masterKey = newMasterKey

	// Clear old key from memory
	for i := range oldKey {
		oldKey[i] = 0
	}

	// Store new key
	if err := os.WriteFile(p.masterKeyPath, p.masterKey, 0600); err != nil {
		return fmt.Errorf("failed to store new master key: %w", err)
	}

	p.logger.Info("Master key rotated successfully")
	return nil
}
