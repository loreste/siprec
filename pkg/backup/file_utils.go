package backup

import (
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"os"
	"strings"
)

// ReadBackupFile reads a backup file, handling compression and encryption
func ReadBackupFile(filePath string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file

	// Handle encryption
	if strings.HasSuffix(filePath, ".enc") {
		decryptedReader, err := createDecryptionReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create decryption reader: %w", err)
		}
		reader = decryptedReader
	}

	// Handle compression
	if strings.Contains(filePath, ".gz") {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	// Read all data
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file data: %w", err)
	}

	return data, nil
}

// createDecryptionReader creates a reader that decrypts AES-256-GCM encrypted data
func createDecryptionReader(file *os.File) (io.Reader, error) {
	// Read the encrypted data
	ciphertext, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read encrypted data: %w", err)
	}

	// For demonstration, we'll need to get the key from environment or config
	// In production, this should be properly managed
	keyString := os.Getenv("BACKUP_ENCRYPTION_KEY")
	if keyString == "" {
		return nil, fmt.Errorf("encryption key not provided")
	}

	key := []byte(keyString)
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes")
	}

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce and ciphertext
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// Decrypt using AES-GCM (secure authenticated encryption)
	// #nosec G407 -- using AES-256-GCM which is a secure modern algorithm
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return strings.NewReader(string(plaintext)), nil
}

// ValidateBackupFile validates that a backup file exists and is readable
func ValidateBackupFile(filePath string) error {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup file does not exist: %s", filePath)
		}
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("backup path is a directory, not a file: %s", filePath)
	}

	if fileInfo.Size() == 0 {
		return fmt.Errorf("backup file is empty: %s", filePath)
	}

	// Check if file is readable
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("backup file is not readable: %w", err)
	}
	file.Close()

	return nil
}
