package lawfulintercept

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"sync"
)

// ContentEncryptor handles encryption of intercepted content for LEA delivery
type ContentEncryptor struct {
	publicKey  *rsa.PublicKey
	privateKey *rsa.PrivateKey // Optional, for testing/key rotation
	keyID      string
	mutex      sync.RWMutex
}

// EncryptedContent represents encrypted content with metadata
type EncryptedContent struct {
	Version       string `json:"version"`
	KeyID         string `json:"key_id"`
	Algorithm     string `json:"algorithm"`
	EncryptedKey  []byte `json:"encrypted_key"`  // AES key encrypted with RSA
	Nonce         []byte `json:"nonce"`          // GCM nonce
	Ciphertext    []byte `json:"ciphertext"`     // AES-GCM encrypted content
	ContentHash   string `json:"content_hash"`   // SHA-256 of plaintext for integrity
}

// NewContentEncryptor creates a new content encryptor from a PEM-encoded public key file
func NewContentEncryptor(keyPath string) (*ContentEncryptor, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read encryption key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	var publicKey *rsa.PublicKey

	switch block.Type {
	case "PUBLIC KEY":
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}
		var ok bool
		publicKey, ok = pub.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key is not RSA public key")
		}
	case "RSA PUBLIC KEY":
		var err error
		publicKey, err = x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA public key: %w", err)
		}
	case "CERTIFICATE":
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse certificate: %w", err)
		}
		var ok bool
		publicKey, ok = cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("certificate does not contain RSA public key")
		}
	default:
		return nil, fmt.Errorf("unsupported key type: %s", block.Type)
	}

	// Generate key ID from public key hash
	keyHash := sha256.Sum256(x509.MarshalPKCS1PublicKey(publicKey))
	keyID := fmt.Sprintf("%x", keyHash[:8])

	return &ContentEncryptor{
		publicKey: publicKey,
		keyID:     keyID,
	}, nil
}

// NewContentEncryptorFromKey creates an encryptor from an RSA public key directly
func NewContentEncryptorFromKey(publicKey *rsa.PublicKey, keyID string) *ContentEncryptor {
	return &ContentEncryptor{
		publicKey: publicKey,
		keyID:     keyID,
	}
}

// Encrypt encrypts intercepted content using hybrid encryption (RSA + AES-GCM)
func (ce *ContentEncryptor) Encrypt(content *InterceptedContent) ([]byte, error) {
	ce.mutex.RLock()
	publicKey := ce.publicKey
	keyID := ce.keyID
	ce.mutex.RUnlock()

	// Marshal content to JSON
	plaintext, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal content: %w", err)
	}

	// Generate random AES-256 key
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %w", err)
	}

	// Encrypt AES key with RSA-OAEP
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AES key: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt content
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Compute content hash for integrity verification
	contentHash := sha256.Sum256(plaintext)

	// Package encrypted content
	encrypted := EncryptedContent{
		Version:      "1.0",
		KeyID:        keyID,
		Algorithm:    "RSA-OAEP-AES-256-GCM",
		EncryptedKey: encryptedKey,
		Nonce:        nonce,
		Ciphertext:   ciphertext,
		ContentHash:  fmt.Sprintf("%x", contentHash),
	}

	return json.Marshal(encrypted)
}

// EncryptRaw encrypts raw bytes using hybrid encryption
func (ce *ContentEncryptor) EncryptRaw(data []byte) ([]byte, error) {
	ce.mutex.RLock()
	publicKey := ce.publicKey
	keyID := ce.keyID
	ce.mutex.RUnlock()

	// Generate random AES-256 key
	aesKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, aesKey); err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %w", err)
	}

	// Encrypt AES key with RSA-OAEP
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt AES key: %w", err)
	}

	// Create AES-GCM cipher
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt data
	ciphertext := gcm.Seal(nil, nonce, data, nil)

	// Compute hash for integrity
	hash := sha256.Sum256(data)

	encrypted := EncryptedContent{
		Version:      "1.0",
		KeyID:        keyID,
		Algorithm:    "RSA-OAEP-AES-256-GCM",
		EncryptedKey: encryptedKey,
		Nonce:        nonce,
		Ciphertext:   ciphertext,
		ContentHash:  fmt.Sprintf("%x", hash),
	}

	return json.Marshal(encrypted)
}

// GetKeyID returns the current encryption key ID
func (ce *ContentEncryptor) GetKeyID() string {
	ce.mutex.RLock()
	defer ce.mutex.RUnlock()
	return ce.keyID
}

// RotateKey updates the encryption key
func (ce *ContentEncryptor) RotateKey(keyPath string) error {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read new key: %w", err)
	}

	block, _ := pem.Decode(keyData)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %w", err)
	}

	publicKey, ok := pub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("key is not RSA public key")
	}

	keyHash := sha256.Sum256(x509.MarshalPKCS1PublicKey(publicKey))
	keyID := fmt.Sprintf("%x", keyHash[:8])

	ce.mutex.Lock()
	ce.publicKey = publicKey
	ce.keyID = keyID
	ce.mutex.Unlock()

	return nil
}
