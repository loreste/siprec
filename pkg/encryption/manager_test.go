package encryption

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce test noise

	tests := []struct {
		name        string
		config      *EncryptionConfig
		expectError bool
	}{
		{
			name:        "default config",
			config:      GetDefaultConfig(),
			expectError: false,
		},
		{
			name:        "nil config",
			config:      nil,
			expectError: false, // Should use default
		},
		{
			name: "enabled encryption",
			config: &EncryptionConfig{
				EnableRecordingEncryption: true,
				EnableMetadataEncryption:  true,
				Algorithm:                 "AES-256-GCM",
				KeySize:                   32,
				NonceSize:                 12,
				SaltSize:                  32,
				PBKDF2Iterations:          100000,
				MasterKeyPath:             "./test-keys",
				EncryptionKeyStore:        "memory",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keyStore := NewMemoryKeyStore()
			manager, err := NewManager(tt.config, keyStore, logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, manager)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manager)
			}
		})
	}
}

func TestKeyGeneration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := GetDefaultConfig()
	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	tests := []struct {
		name        string
		algorithm   string
		expectError bool
	}{
		{
			name:        "AES-256-GCM",
			algorithm:   "AES-256-GCM",
			expectError: false,
		},
		{
			name:        "ChaCha20-Poly1305",
			algorithm:   "ChaCha20-Poly1305",
			expectError: false,
		},
		{
			name:        "unsupported algorithm",
			algorithm:   "INVALID-ALGO",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := manager.GenerateKey(tt.algorithm)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, key)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, key)
				assert.Equal(t, tt.algorithm, key.Algorithm)
				assert.Equal(t, config.KeySize, len(key.KeyData))
				assert.True(t, key.Active)
				assert.NotEmpty(t, key.ID)
			}
		})
	}
}

func TestEncryptDecryptRecording(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
		NonceSize:                 12,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	testData := []byte("This is test audio data for encryption testing")
	sessionID := "test-session-123"

	// Test encryption
	encryptedData, err := manager.EncryptRecording(sessionID, testData)
	assert.NoError(t, err)
	assert.NotNil(t, encryptedData)
	assert.Equal(t, config.Algorithm, encryptedData.Algorithm)
	assert.NotEmpty(t, encryptedData.KeyID)
	assert.NotEmpty(t, encryptedData.Nonce)
	assert.NotEmpty(t, encryptedData.Ciphertext)
	assert.NotEqual(t, testData, encryptedData.Ciphertext)

	// Test decryption
	decryptedData, err := manager.DecryptRecording(sessionID, encryptedData)
	assert.NoError(t, err)
	assert.Equal(t, testData, decryptedData)
}

func TestEncryptDecryptMetadata(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableMetadataEncryption: true,
		Algorithm:                "AES-256-GCM",
		KeySize:                  32,
		NonceSize:                12,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	testMetadata := map[string]interface{}{
		"session_id":   "test-session-456",
		"participants": []string{"Alice", "Bob"},
		"codec":        "PCMU",
		"sample_rate":  8000,
	}
	sessionID := "test-session-456"

	// Test encryption
	encryptedData, err := manager.EncryptMetadata(sessionID, testMetadata)
	assert.NoError(t, err)
	assert.NotNil(t, encryptedData)
	assert.Equal(t, config.Algorithm, encryptedData.Algorithm)
	assert.NotEmpty(t, encryptedData.KeyID)

	// Test decryption
	decryptedMetadata, err := manager.DecryptMetadata(sessionID, encryptedData)
	assert.NoError(t, err)

	// JSON unmarshaling converts types, so we need to check individual fields
	assert.Equal(t, testMetadata["session_id"], decryptedMetadata["session_id"])
	assert.Equal(t, testMetadata["codec"], decryptedMetadata["codec"])
	assert.Equal(t, float64(testMetadata["sample_rate"].(int)), decryptedMetadata["sample_rate"])

	// Check participants array
	participants := decryptedMetadata["participants"].([]interface{})
	assert.Len(t, participants, 2)
	assert.Equal(t, "Alice", participants[0])
	assert.Equal(t, "Bob", participants[1])
}

func TestKeyRotation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		Algorithm:           "AES-256-GCM",
		KeySize:             32,
		KeyRotationInterval: 1 * time.Millisecond, // Very short for testing
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	// Generate initial key
	key1, err := manager.GenerateKey(config.Algorithm)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(2 * time.Millisecond)

	// Generate another key
	key2, err := manager.GenerateKey(config.Algorithm)
	require.NoError(t, err)

	// Keys should be different
	assert.NotEqual(t, key1.ID, key2.ID)

	// Test rotation
	err = manager.RotateKeys()
	assert.NoError(t, err)

	// Get active key after rotation
	activeKey, err := manager.GetActiveKey(config.Algorithm)
	require.NoError(t, err)
	assert.NotNil(t, activeKey)
}

func TestEncryptionDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: false,
		EnableMetadataEncryption:  false,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	assert.False(t, manager.IsEncryptionEnabled())

	testData := []byte("test data")
	sessionID := "test-session"

	// Should return error when encryption is disabled
	_, err = manager.EncryptRecording(sessionID, testData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recording encryption is disabled")

	testMetadata := map[string]interface{}{"test": "data"}
	_, err = manager.EncryptMetadata(sessionID, testMetadata)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "metadata encryption is disabled")
}

func TestChaCha20Poly1305Encryption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "ChaCha20-Poly1305",
		KeySize:                   32,
		NonceSize:                 12,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	testData := []byte("ChaCha20-Poly1305 test data")
	sessionID := "chacha-test-session"

	// Test encryption
	encryptedData, err := manager.EncryptRecording(sessionID, testData)
	assert.NoError(t, err)
	assert.Equal(t, "ChaCha20-Poly1305", encryptedData.Algorithm)

	// Test decryption
	decryptedData, err := manager.DecryptRecording(sessionID, encryptedData)
	assert.NoError(t, err)
	assert.Equal(t, testData, decryptedData)
}

func TestStreamEncryptionAndDecryption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	cases := []struct {
		name      string
		algorithm string
	}{
		{"AES-256 stream", "AES-256-GCM"},
		{"ChaCha20 stream", "ChaCha20-Poly1305"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			config := &EncryptionConfig{
				EnableRecordingEncryption: true,
				Algorithm:                 tt.algorithm,
				KeySize:                   32,
			}

			keyStore := NewMemoryKeyStore()
			manager, err := NewManager(config, keyStore, logger)
			require.NoError(t, err)

			sessionID := fmt.Sprintf("stream-session-%s", tt.algorithm)
			stream, err := manager.CreateEncryptionStream(sessionID)
			require.NoError(t, err)
			require.NotNil(t, stream)

			info, err := manager.GetEncryptionInfo(sessionID)
			require.NoError(t, err)
			require.NotNil(t, info)
			assert.True(t, info.StreamEncryption)
			assert.Nil(t, info.StreamNonce)
			assert.NotEmpty(t, info.StreamNonceHash)
			assert.Equal(t, tt.algorithm, info.Algorithm)

			rawInfo, exists := manager.getRawEncryptionInfo(sessionID)
			require.True(t, exists)
			assert.NotEmpty(t, rawInfo.StreamNonce)
			assert.Equal(t, info.StreamNonceHash, rawInfo.StreamNonceHash)
			assert.Equal(t, tt.algorithm, rawInfo.Algorithm)

			plaintext := []byte("real-time audio sample data")
			ciphertext := make([]byte, len(plaintext))
			stream.XORKeyStream(ciphertext, plaintext)
			assert.NotEqual(t, plaintext, ciphertext)

			decStream, err := manager.CreateDecryptionStream(sessionID, info.KeyID)
			require.NoError(t, err)
			require.NotNil(t, decStream)

			decrypted := make([]byte, len(ciphertext))
			decStream.XORKeyStream(decrypted, ciphertext)
			assert.Equal(t, plaintext, decrypted)

			manager.CleanupSession(sessionID)

			_, err = manager.CreateDecryptionStream(sessionID, info.KeyID)
			require.Error(t, err)

			cleanInfo, err := manager.GetEncryptionInfo(sessionID)
			require.NoError(t, err)
			assert.False(t, cleanInfo.StreamEncryption)
			assert.Nil(t, cleanInfo.StreamNonce)
			assert.Empty(t, cleanInfo.StreamNonceHash)
		})
	}
}

func TestStreamSessionCleanup(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	sessionIDs := []string{"cleanup-one", "cleanup-two", "cleanup-three"}

	for _, id := range sessionIDs {
		stream, err := manager.CreateEncryptionStream(id)
		require.NoError(t, err)
		require.NotNil(t, stream)
	}

	for _, id := range sessionIDs {
		manager.CleanupSession(id)

		_, err := manager.CreateDecryptionStream(id, "")
		assert.Error(t, err)

		info, err := manager.GetEncryptionInfo(id)
		require.NoError(t, err)
		assert.False(t, info.StreamEncryption)
		assert.Nil(t, info.StreamNonce)
		assert.Empty(t, info.StreamNonceHash)
	}
}

func TestStreamRecoveryFromSessionInfo(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	sessionID := "rehydrate-session"
	stream, err := manager.CreateEncryptionStream(sessionID)
	require.NoError(t, err)
	require.NotNil(t, stream)

	// Drop in-memory stream state to simulate restart
	manager.streamMu.Lock()
	delete(manager.streamInfo, sessionID)
	manager.streamMu.Unlock()

	rawInfo, exists := manager.getRawEncryptionInfo(sessionID)
	require.True(t, exists)
	require.NotEmpty(t, rawInfo.StreamNonce)

	recoveredStream, err := manager.CreateDecryptionStream(sessionID, rawInfo.KeyID)
	require.NoError(t, err)
	require.NotNil(t, recoveredStream)

	plaintext := []byte("resumed audio payload")
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)

	decrypted := make([]byte, len(ciphertext))
	recoveredStream.XORKeyStream(decrypted, ciphertext)
	assert.Equal(t, plaintext, decrypted)
}

func TestStreamEncryptionSurvivesKeyRotation(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
		KeyRotationInterval:       1 * time.Millisecond,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	sessionID := "rotation-session"
	stream, err := manager.CreateEncryptionStream(sessionID)
	require.NoError(t, err)
	require.NotNil(t, stream)

	plaintext := []byte("rotation audio payload")
	ciphertext := make([]byte, len(plaintext))
	stream.XORKeyStream(ciphertext, plaintext)

	// Allow the active key to expire and rotate
	time.Sleep(2 * time.Millisecond)
	err = manager.RotateKeys()
	require.NoError(t, err)

	decStream, err := manager.CreateDecryptionStream(sessionID, "")
	require.NoError(t, err)
	require.NotNil(t, decStream)

	decrypted := make([]byte, len(ciphertext))
	decStream.XORKeyStream(decrypted, ciphertext)
	assert.Equal(t, plaintext, decrypted)
}

func TestEncryptionInfo(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		EnableMetadataEncryption:  true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	sessionID := "info-test-session"

	// Initially no encryption info
	info, err := manager.GetEncryptionInfo(sessionID)
	assert.NoError(t, err)
	assert.False(t, info.RecordingEncrypted)
	assert.False(t, info.MetadataEncrypted)

	// Encrypt some data to update session info
	testData := []byte("test data")
	_, err = manager.EncryptRecording(sessionID, testData)
	assert.NoError(t, err)

	testMetadata := map[string]interface{}{"test": "metadata"}
	_, err = manager.EncryptMetadata(sessionID, testMetadata)
	assert.NoError(t, err)

	// Check updated info
	info, err = manager.GetEncryptionInfo(sessionID)
	assert.NoError(t, err)
	assert.True(t, info.RecordingEncrypted)
	assert.True(t, info.MetadataEncrypted)
	assert.Equal(t, sessionID, info.SessionID)
	assert.Equal(t, config.Algorithm, info.Algorithm)
	assert.NotEmpty(t, info.KeyID)
}

func TestConcurrentEncryption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	const numGoroutines = 10
	const numOperations = 50

	// Test concurrent encryption operations
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				sessionID := fmt.Sprintf("session-%d-%d", id, j)
				testData := []byte(fmt.Sprintf("test data %d-%d", id, j))

				encryptedData, err := manager.EncryptRecording(sessionID, testData)
				assert.NoError(t, err)

				decryptedData, err := manager.DecryptRecording(sessionID, encryptedData)
				assert.NoError(t, err)
				assert.Equal(t, testData, decryptedData)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

func TestLargeDataEncryption(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	config := &EncryptionConfig{
		EnableRecordingEncryption: true,
		Algorithm:                 "AES-256-GCM",
		KeySize:                   32,
	}

	keyStore := NewMemoryKeyStore()
	manager, err := NewManager(config, keyStore, logger)
	require.NoError(t, err)

	// Test with large data (1MB)
	testData := make([]byte, 1024*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	sessionID := "large-data-session"

	start := time.Now()
	encryptedData, err := manager.EncryptRecording(sessionID, testData)
	encryptTime := time.Since(start)

	assert.NoError(t, err)
	assert.NotNil(t, encryptedData)

	start = time.Now()
	decryptedData, err := manager.DecryptRecording(sessionID, encryptedData)
	decryptTime := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, testData, decryptedData)

	t.Logf("Encryption time for 1MB: %v", encryptTime)
	t.Logf("Decryption time for 1MB: %v", decryptTime)

	// Performance should be reasonable (less than 100ms for 1MB on modern hardware)
	assert.Less(t, encryptTime, 100*time.Millisecond)
	assert.Less(t, decryptTime, 100*time.Millisecond)
}
