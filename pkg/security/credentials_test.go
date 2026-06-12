package security

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSecretsManagerTestServer returns an httptest server emulating the AWS
// Secrets Manager GetSecretValue API for a fixed set of secrets
func newSecretsManagerTestServer(t *testing.T, secrets map[string]string, binarySecrets map[string][]byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secretsmanager.GetSecretValue", r.Header.Get("X-Amz-Target"))
		assert.Equal(t, "application/x-amz-json-1.1", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "AWS4-HMAC-SHA256", "request must be SigV4 signed")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var req struct {
			SecretID string `json:"SecretId"`
		}
		require.NoError(t, json.Unmarshal(body, &req))

		w.Header().Set("Content-Type", "application/x-amz-json-1.1")

		if value, ok := secrets[req.SecretID]; ok {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ARN":          "arn:aws:secretsmanager:us-east-1:123456789012:secret:" + req.SecretID,
				"Name":         req.SecretID,
				"SecretString": value,
			})
			return
		}

		if value, ok := binarySecrets[req.SecretID]; ok {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ARN":          "arn:aws:secretsmanager:us-east-1:123456789012:secret:" + req.SecretID,
				"Name":         req.SecretID,
				"SecretBinary": base64.StdEncoding.EncodeToString(value),
			})
			return
		}

		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"__type":"ResourceNotFoundException","message":"Secrets Manager can't find the specified secret."}`)
	}))
}

func newTestCredentialProvider() *CredentialProvider {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	return NewCredentialProvider(logger)
}

func setupAWSTestEnv(t *testing.T, endpoint string) {
	t.Helper()
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIAIOSFODNN7EXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_ENDPOINT_URL_SECRETS_MANAGER", endpoint)
}

func TestGetFromAWSSecretsManagerSecretString(t *testing.T) {
	server := newSecretsManagerTestServer(t, map[string]string{
		"db.password": "s3cr3t-value",
	}, nil)
	defer server.Close()

	setupAWSTestEnv(t, server.URL)

	cp := newTestCredentialProvider()
	value, err := cp.getFromAWSSecretsManager("db.password")
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t-value", value)
}

func TestGetFromAWSSecretsManagerSecretBinary(t *testing.T) {
	server := newSecretsManagerTestServer(t, nil, map[string][]byte{
		"binary.secret": []byte("binary-payload"),
	})
	defer server.Close()

	setupAWSTestEnv(t, server.URL)

	cp := newTestCredentialProvider()
	value, err := cp.getFromAWSSecretsManager("binary.secret")
	require.NoError(t, err)
	assert.Equal(t, "binary-payload", value)
}

func TestGetFromAWSSecretsManagerNotFound(t *testing.T) {
	server := newSecretsManagerTestServer(t, nil, nil)
	defer server.Close()

	setupAWSTestEnv(t, server.URL)

	cp := newTestCredentialProvider()
	_, err := cp.getFromAWSSecretsManager("missing.secret")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSecretNotFound), "expected not-found error, got: %v", err)
}

func TestGetFromAWSSecretsManagerTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"__type":"InternalServiceError","message":"boom"}`)
	}))
	defer server.Close()

	setupAWSTestEnv(t, server.URL)

	cp := newTestCredentialProvider()
	_, err := cp.getFromAWSSecretsManager("any.secret")
	require.Error(t, err)
	assert.False(t, errors.Is(err, errSecretNotFound))
	assert.False(t, errors.Is(err, errAWSNotConfigured))
	// The error must not leak credential material
	assert.NotContains(t, err.Error(), "boom")
}

func TestGetFromAWSSecretsManagerNotInAWS(t *testing.T) {
	t.Setenv("AWS_EXECUTION_ENV", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	cp := newTestCredentialProvider()
	_, err := cp.getFromAWSSecretsManager("any.secret")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errAWSNotConfigured))
}

func TestGetCredentialUsesAWSSecretsManagerAndCache(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		fmt.Fprint(w, `{"Name":"api.key","SecretString":"from-aws"}`)
	}))
	defer server.Close()

	setupAWSTestEnv(t, server.URL)
	// Ensure the env-var provider does not satisfy the lookup first
	t.Setenv("API_KEY", "")

	cp := newTestCredentialProvider()

	value, err := cp.GetCredential("api.key")
	require.NoError(t, err)
	assert.Equal(t, "from-aws", value)
	assert.Equal(t, 1, requestCount)

	// Second lookup must be served from the TTL cache
	value, err = cp.GetCredential("api.key")
	require.NoError(t, err)
	assert.Equal(t, "from-aws", value)
	assert.Equal(t, 1, requestCount)
}
