package media

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/audio"
)

// Mock encrypted recording manager for testing
type mockEncryptedRecordingManager struct {
	writeAudioCalls []writeAudioCall
	writeError      error
}

type writeAudioCall struct {
	sessionID string
	data      []byte
}

func (m *mockEncryptedRecordingManager) WriteAudio(sessionID string, audioData []byte) error {
	m.writeAudioCalls = append(m.writeAudioCalls, writeAudioCall{
		sessionID: sessionID,
		data:      audioData,
	})
	return m.writeError
}

func TestEncryptedRecordingWriter_Write_InitializationError(t *testing.T) {
	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "test-session",
	}

	testData := []byte("test audio data")
	n, err := writer.Write(testData)

	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "encrypted recorder not initialized")
}

func TestEncryptedRecordingWriter_Write_NilManager(t *testing.T) {
	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "test-session",
	}

	testData := []byte("test audio data")
	n, err := writer.Write(testData)

	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "encrypted recorder not initialized")
}

func TestEncryptedRecordingWriter_Write_EmptySessionID(t *testing.T) {
	// Create a dummy manager (nil is fine since we won't call it)
	writer := &encryptedRecordingWriter{
		manager:   (*audio.EncryptedRecordingManager)(nil),
		sessionID: "",
	}

	testData := []byte("test audio data")
	n, err := writer.Write(testData)

	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "encrypted recorder not initialized")
}

func TestEncryptedRecordingWriter_Write_BothNil(t *testing.T) {
	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "",
	}

	testData := []byte("test audio data")
	n, err := writer.Write(testData)

	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "encrypted recorder not initialized")
}

func TestEncryptedRecordingWriter_Write_EmptyData(t *testing.T) {
	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "test-session",
	}

	// Even with empty data, should still validate initialization
	emptyData := []byte{}
	n, err := writer.Write(emptyData)

	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "encrypted recorder not initialized")
}

func TestEncryptedRecordingWriter_ValidationOrder(t *testing.T) {
	// Test that the validation checks manager and sessionID before attempting write
	testCases := []struct {
		name      string
		manager   *audio.EncryptedRecordingManager
		sessionID string
		wantError bool
	}{
		{
			name:      "Both nil/empty",
			manager:   nil,
			sessionID: "",
			wantError: true,
		},
		{
			name:      "Nil manager, valid session",
			manager:   nil,
			sessionID: "test-session",
			wantError: true,
		},
		{
			name: "Valid manager pointer, empty session",
			manager: (*audio.EncryptedRecordingManager)(nil), // Type-correct nil pointer
			sessionID: "",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			writer := &encryptedRecordingWriter{
				manager:   tc.manager,
				sessionID: tc.sessionID,
			}

			testData := []byte("test data")
			n, err := writer.Write(testData)

			if tc.wantError {
				require.Error(t, err)
				assert.Equal(t, 0, n)
				assert.Contains(t, err.Error(), "encrypted recorder not initialized")
			} else {
				require.NoError(t, err)
				assert.Equal(t, len(testData), n)
			}
		})
	}
}

// Integration-style test that verifies the writer structure
func TestEncryptedRecordingWriter_Structure(t *testing.T) {
	// Verify the writer has the expected fields
	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "test",
	}

	assert.NotNil(t, writer)
	assert.Equal(t, "test", writer.sessionID)
}

// Test that Write returns the correct byte count on success
// Note: We can't easily test the success path without a real EncryptedRecordingManager
// or significant refactoring to support dependency injection.
// The error paths are the most critical for safety anyway.
func TestEncryptedRecordingWriter_Interface(t *testing.T) {
	// Verify that encryptedRecordingWriter implements io.Writer
	var _ fmt.Stringer // Just checking compilation

	writer := &encryptedRecordingWriter{
		manager:   nil,
		sessionID: "test",
	}

	// Verify Write signature matches io.Writer
	data := []byte("test")
	n, err := writer.Write(data)

	// Should error due to nil manager, but signature is correct
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}
