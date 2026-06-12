package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/smithy-go"
	"github.com/sirupsen/logrus"
)

// Sentinel errors used to distinguish lookup outcomes between providers
var (
	// errAWSNotConfigured indicates the process is not configured for AWS,
	// so the AWS Secrets Manager provider was skipped entirely
	errAWSNotConfigured = errors.New("AWS environment not configured")

	// errSecretNotFound indicates the secret does not exist in the backing store
	errSecretNotFound = errors.New("secret not found")
)

const awsSecretsManagerTimeout = 5 * time.Second

// CredentialProvider handles secure credential retrieval
type CredentialProvider struct {
	cache  map[string]*cachedCredential
	mu     sync.RWMutex
	logger *logrus.Logger

	// Lazily initialized AWS Secrets Manager client
	awsOnce    sync.Once
	awsRegion  string
	awsClient  *secretsmanager.Client
	awsInitErr error
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
	} else if err != nil && !errors.Is(err, errAWSNotConfigured) && !errors.Is(err, errSecretNotFound) {
		// Transport/authorization failures are logged (without credential
		// values) so operators can diagnose them; lookup falls through to
		// the next provider
		cp.logger.WithError(err).WithField("credential", name).Warn("AWS Secrets Manager lookup failed")
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

// getFromAWSSecretsManager retrieves a credential from AWS Secrets Manager.
//
// The lookup uses the AWS SDK v2 default configuration chain (environment,
// shared config, IAM role) for credentials and region, and calls the Secrets
// Manager GetSecretValue API via the official service client. Both
// SecretString and SecretBinary payloads are supported.
//
// Returns errAWSNotConfigured when the process is not configured for AWS,
// errSecretNotFound when the secret does not exist, and a descriptive error
// for transport/authorization failures. Secret values are never logged.
func (cp *CredentialProvider) getFromAWSSecretsManager(name string) (string, error) {
	// Check if we're running in AWS (or explicitly configured for it)
	if os.Getenv("AWS_EXECUTION_ENV") == "" &&
		os.Getenv("AWS_REGION") == "" &&
		os.Getenv("AWS_DEFAULT_REGION") == "" {
		// Not in AWS, return specific error so we fall through to next provider
		return "", errAWSNotConfigured
	}

	ctx, cancel := context.WithTimeout(context.Background(), awsSecretsManagerTimeout)
	defer cancel()

	client, err := cp.secretsManagerClient(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	if cp.awsRegion == "" {
		return "", errAWSNotConfigured
	}

	return cp.getSecretValue(ctx, client, name)
}

// secretsManagerClient lazily initializes and caches the AWS Secrets Manager
// service client built from the SDK default configuration chain
func (cp *CredentialProvider) secretsManagerClient(ctx context.Context) (*secretsmanager.Client, error) {
	cp.awsOnce.Do(func() {
		cfg, err := awsconfig.LoadDefaultConfig(ctx)
		if err != nil {
			cp.awsInitErr = err
			return
		}

		// Fall back to environment variables for the region when the
		// default chain does not resolve one
		if cfg.Region == "" {
			if region := os.Getenv("AWS_REGION"); region != "" {
				cfg.Region = region
			} else if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
				cfg.Region = region
			}
		}

		cp.awsRegion = cfg.Region
		cp.awsClient = secretsmanager.NewFromConfig(cfg, func(o *secretsmanager.Options) {
			if endpoint := secretsManagerEndpoint(); endpoint != "" {
				o.BaseEndpoint = aws.String(endpoint)
			}
		})
	})

	return cp.awsClient, cp.awsInitErr
}

// secretsManagerEndpoint resolves a Secrets Manager endpoint override from
// the standard AWS endpoint environment variables (used for testing and
// private/FIPS endpoints). Returns the empty string when no override is set,
// in which case the SDK resolves the regional endpoint itself.
func secretsManagerEndpoint() string {
	if endpoint := os.Getenv("AWS_ENDPOINT_URL_SECRETS_MANAGER"); endpoint != "" {
		return endpoint
	}
	return os.Getenv("AWS_ENDPOINT_URL")
}

// getSecretValue calls the Secrets Manager GetSecretValue API using the
// official AWS SDK v2 service client
func (cp *CredentialProvider) getSecretValue(ctx context.Context, client *secretsmanager.Client, name string) (string, error) {
	result, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(name),
	})
	if err != nil {
		var notFound *types.ResourceNotFoundException
		if errors.As(err, &notFound) {
			return "", fmt.Errorf("secret %q: %w", name, errSecretNotFound)
		}

		// Never include the raw SDK error message for API errors since it
		// echoes service response details; report only the error code
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			return "", fmt.Errorf("Secrets Manager returned error (%s)", apiErr.ErrorCode())
		}

		return "", fmt.Errorf("Secrets Manager request failed: %w", err)
	}

	switch {
	case result.SecretString != nil:
		return *result.SecretString, nil
	case len(result.SecretBinary) > 0:
		// The SDK base64-decodes SecretBinary automatically
		return string(result.SecretBinary), nil
	default:
		return "", fmt.Errorf("secret %q has no value: %w", name, errSecretNotFound)
	}
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
	credFile := filepath.Clean(fmt.Sprintf("%s/credentials.json", configDir))
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
