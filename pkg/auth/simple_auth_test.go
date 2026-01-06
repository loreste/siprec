package auth

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestSimpleAuthenticator_AddUser_HashesPassword(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Add a user
	err := auth.AddUser("testuser", "mypassword123", "admin")
	require.NoError(t, err)

	// Verify password is hashed (not stored in plaintext)
	auth.mutex.RLock()
	user := auth.users["testuser"]
	auth.mutex.RUnlock()

	require.NotNil(t, user)
	assert.NotEqual(t, "mypassword123", user.PasswordHash, "Password should not be stored in plaintext")
	assert.True(t, len(user.PasswordHash) > 50, "Password hash should be long (bcrypt format)")

	// Verify it's a valid bcrypt hash
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("mypassword123"))
	assert.NoError(t, err, "Password hash should validate against original password")

	// Verify wrong password fails
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("wrongpassword"))
	assert.Error(t, err, "Wrong password should fail validation")
}

func TestSimpleAuthenticator_Login_VerifiesHashedPassword(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Add a user
	err := auth.AddUser("testuser", "correctpassword", "admin")
	require.NoError(t, err)

	// Login with correct password should succeed
	token, err := auth.Login("testuser", "correctpassword")
	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Validate the token
	claims, err := auth.ValidateToken(token)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", claims.Username)
	assert.Equal(t, "admin", claims.Role)
}

func TestSimpleAuthenticator_Login_RejectsWrongPassword(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Add a user
	err := auth.AddUser("testuser", "correctpassword", "admin")
	require.NoError(t, err)

	// Login with wrong password should fail
	token, err := auth.Login("testuser", "wrongpassword")
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "invalid username or password")
}

func TestSimpleAuthenticator_Login_RejectsUnknownUser(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Login with non-existent user should fail
	token, err := auth.Login("nonexistent", "anypassword")
	assert.Error(t, err)
	assert.Empty(t, token)
	assert.Contains(t, err.Error(), "invalid username or password")
}

func TestSimpleAuthenticator_Login_TimingAttackPrevention(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Add a user
	err := auth.AddUser("testuser", "correctpassword", "admin")
	require.NoError(t, err)

	// Both should take similar time (not testing timing precisely, just that code runs)
	_, err1 := auth.Login("testuser", "wrongpassword")
	_, err2 := auth.Login("nonexistent", "anypassword")

	// Both should fail with same error message
	assert.Error(t, err1)
	assert.Error(t, err2)
	assert.Equal(t, err1.Error(), err2.Error(), "Error messages should be identical to prevent user enumeration")
}

func TestSimpleAuthenticator_APIKey(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	auth := NewSimpleAuthenticator("test-secret", "test-issuer", time.Hour, logger)

	// Add a user
	err := auth.AddUser("testuser", "password", "operator")
	require.NoError(t, err)

	// Generate API key
	apiKey, err := auth.GenerateAPIKey("testuser")
	require.NoError(t, err)
	assert.NotEmpty(t, apiKey)

	// Validate API key
	userInfo, err := auth.ValidateAPIKey(apiKey)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", userInfo.Username)
	assert.Equal(t, "operator", userInfo.Role)

	// Invalid API key should fail
	_, err = auth.ValidateAPIKey("invalid-key")
	assert.Error(t, err)
}
