package security

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// CredentialProvider handles secure credential retrieval
type CredentialProvider struct {
	cache  map[string]*cachedCredential
	mu     sync.RWMutex
	logger *logrus.Logger
}

type cachedCredential struct {
	value     string
	expiresAt time.Time
}

// NewCredentialProvider creates a new credential provider
func NewCredentialProvider(logger *logrus.Logger) *CredentialProvider {
	return &CredentialProvider{
		cache:  make(map[string]*cachedCredential),
		logger: logger,
	}
}

// GetCredential retrieves a credential from various sources
func (cp *CredentialProvider) GetCredential(name string) (string, error) {
	// Check cache first
	cp.mu.RLock()
	if cached, ok := cp.cache[name]; ok && cached.expiresAt.After(time.Now()) {
		cp.mu.RUnlock()
		return cached.value, nil
	}
	cp.mu.RUnlock()

	// Try multiple sources in order of preference
	
	// 1. Environment variable
	envName := strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
	if value := os.Getenv(envName); value != "" {
		cp.cacheCredential(name, value, 5*time.Minute)
		cp.logger.WithField("source", "env").Debug("Retrieved credential from environment")
		return value, nil
	}

	// 2. AWS Secrets Manager (if running in AWS)
	if value, err := cp.getFromAWSSecretsManager(name); err == nil && value != "" {
		cp.cacheCredential(name, value, 15*time.Minute)
		cp.logger.WithField("source", "aws_secrets").Debug("Retrieved credential from AWS Secrets Manager")
		return value, nil
	}

	// 3. Kubernetes Secret (if running in K8s)
	if value, err := cp.getFromKubernetesSecret(name); err == nil && value != "" {
		cp.cacheCredential(name, value, 5*time.Minute)
		cp.logger.WithField("source", "k8s_secret").Debug("Retrieved credential from Kubernetes")
		return value, nil
	}

	// 4. Local secure file (development/fallback)
	if value, err := cp.getFromSecureFile(name); err == nil && value != "" {
		cp.cacheCredential(name, value, 1*time.Minute)
		cp.logger.WithField("source", "file").Debug("Retrieved credential from secure file")
		return value, nil
	}

	return "", fmt.Errorf("credential '%s' not found in any source", name)
}

// cacheCredential stores a credential in the cache
func (cp *CredentialProvider) cacheCredential(name, value string, ttl time.Duration) {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	
	cp.cache[name] = &cachedCredential{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
}

// getFromAWSSecretsManager retrieves credential from AWS Secrets Manager
func (cp *CredentialProvider) getFromAWSSecretsManager(name string) (string, error) {
	// Check if we're running in AWS
	if os.Getenv("AWS_EXECUTION_ENV") == "" && os.Getenv("AWS_REGION") == "" {
		return "", fmt.Errorf("not running in AWS environment")
	}

	// In production, this would use the AWS SDK
	// For now, return empty to avoid external dependencies
	return "", fmt.Errorf("AWS Secrets Manager integration not implemented")
}

// getFromKubernetesSecret retrieves credential from Kubernetes secret
func (cp *CredentialProvider) getFromKubernetesSecret(name string) (string, error) {
	// Check if we're running in Kubernetes
	if _, err := os.Stat("/var/run/secrets/kubernetes.io"); os.IsNotExist(err) {
		return "", fmt.Errorf("not running in Kubernetes environment")
	}

	// Look for mounted secret files
	secretPath := fmt.Sprintf("/var/run/secrets/siprec/%s", name)
	if data, err := os.ReadFile(secretPath); err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	return "", fmt.Errorf("Kubernetes secret not found")
}

// getFromSecureFile retrieves credential from a secure local file
func (cp *CredentialProvider) getFromSecureFile(name string) (string, error) {
	// Use a secure directory with restricted permissions
	configDir := os.Getenv("SIPREC_CONFIG_DIR")
	if configDir == "" {
		configDir = "/etc/siprec/credentials"
	}

	// Read credentials file
	credFile := fmt.Sprintf("%s/credentials.json", configDir)
	data, err := os.ReadFile(credFile)
	if err != nil {
		return "", err
	}

	// Parse JSON
	var creds map[string]string
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}

	if value, ok := creds[name]; ok {
		return value, nil
	}

	return "", fmt.Errorf("credential not found in file")
}

// ClearCache clears the credential cache
func (cp *CredentialProvider) ClearCache() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	
	// Clear sensitive data before removing
	for _, cached := range cp.cache {
		// Overwrite the credential value
		cached.value = strings.Repeat("X", len(cached.value))
	}
	
	cp.cache = make(map[string]*cachedCredential)
}

// Global credential provider instance
var (
	globalCredProvider *CredentialProvider
	credProviderOnce   sync.Once
)

// GetCredential retrieves a credential using the global provider
func GetCredential(name string) (string, error) {
	credProviderOnce.Do(func() {
		logger := logrus.New()
		globalCredProvider = NewCredentialProvider(logger)
	})
	
	return globalCredProvider.GetCredential(name)
}

// SetupCredentialProvider sets up the global credential provider with a logger
func SetupCredentialProvider(logger *logrus.Logger) {
	credProviderOnce.Do(func() {
		globalCredProvider = NewCredentialProvider(logger)
	})
}