package encryption

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
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

// AWSKMSProvider implements KMSProvider using AWS KMS
type AWSKMSProvider struct {
	client    *kms.KMS
	keyID     string
	logger    *logrus.Logger
	cache     *keyCache
}

// keyCache caches decrypted keys for performance
type keyCache struct {
	keys map[string]cacheEntry
	mu   sync.RWMutex
}

type cacheEntry struct {
	key       []byte
	expiresAt time.Time
}

// NewAWSKMSProvider creates a new AWS KMS provider
func NewAWSKMSProvider(keyID string, logger *logrus.Logger) (*AWSKMSProvider, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	return &AWSKMSProvider{
		client: kms.New(sess),
		keyID:  keyID,
		logger: logger,
		cache: &keyCache{
			keys: make(map[string]cacheEntry),
		},
	}, nil
}

// GenerateDataKey generates a new data encryption key using AWS KMS
func (p *AWSKMSProvider) GenerateDataKey(ctx context.Context) ([]byte, []byte, error) {
	input := &kms.GenerateDataKeyInput{
		KeyId:   aws.String(p.keyID),
		KeySpec: aws.String(kms.DataKeySpecAes256),
	}

	result, err := p.client.GenerateDataKeyWithContext(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate data key: %w", err)
	}

	return result.Plaintext, result.CiphertextBlob, nil
}

// DecryptDataKey decrypts an encrypted data key using AWS KMS
func (p *AWSKMSProvider) DecryptDataKey(ctx context.Context, encryptedKey []byte) ([]byte, error) {
	// Check cache first
	cacheKey := base64.StdEncoding.EncodeToString(encryptedKey)
	
	p.cache.mu.RLock()
	if entry, ok := p.cache.keys[cacheKey]; ok && entry.expiresAt.After(time.Now()) {
		p.cache.mu.RUnlock()
		return entry.key, nil
	}
	p.cache.mu.RUnlock()

	// Decrypt using KMS
	input := &kms.DecryptInput{
		CiphertextBlob: encryptedKey,
	}

	result, err := p.client.DecryptWithContext(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data key: %w", err)
	}

	// Cache the result
	p.cache.mu.Lock()
	p.cache.keys[cacheKey] = cacheEntry{
		key:       result.Plaintext,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	p.cache.mu.Unlock()

	// Schedule cleanup
	go p.cleanupExpiredKeys()

	return result.Plaintext, nil
}

// RotateMasterKey initiates master key rotation in AWS KMS
func (p *AWSKMSProvider) RotateMasterKey(ctx context.Context) error {
	input := &kms.EnableKeyRotationInput{
		KeyId: aws.String(p.keyID),
	}

	_, err := p.client.EnableKeyRotationWithContext(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to enable key rotation: %w", err)
	}

	p.logger.Info("Master key rotation enabled in AWS KMS")
	return nil
}

// cleanupExpiredKeys removes expired keys from cache
func (p *AWSKMSProvider) cleanupExpiredKeys() {
	p.cache.mu.Lock()
	defer p.cache.mu.Unlock()

	now := time.Now()
	for key, entry := range p.cache.keys {
		if entry.expiresAt.Before(now) {
			// Clear key from memory before deletion
			for i := range entry.key {
				entry.key[i] = 0
			}
			delete(p.cache.keys, key)
		}
	}
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

// CreateKMSProvider creates the appropriate KMS provider based on configuration
func CreateKMSProvider(config *KMSConfig, logger *logrus.Logger) (KMSProvider, error) {
	switch config.Provider {
	case "aws":
		if config.AWSKeyID == "" {
			return nil, fmt.Errorf("AWS KMS key ID is required")
		}
		return NewAWSKMSProvider(config.AWSKeyID, logger)
	
	case "local":
		return NewLocalKMSProvider(config.LocalKeyPath, logger)
	
	default:
		return nil, fmt.Errorf("unsupported KMS provider: %s", config.Provider)
	}
}

// KMSConfig holds KMS configuration
type KMSConfig struct {
	Provider     string // "aws", "gcp", "azure", "local"
	AWSKeyID     string // AWS KMS key ID
	GCPKeyName   string // GCP KMS key name
	AzureKeyURL  string // Azure Key Vault key URL
	LocalKeyPath string // Local master key path
}