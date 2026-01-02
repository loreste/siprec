package messaging

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to generate temporary self-signed certs for testing
func generateTestCert(t *testing.T, dir string) (certPath, keyPath string) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	// Write cert
	certPath = filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certPath)
	require.NoError(t, err)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certOut.Close()

	// Write key
	keyPath = filepath.Join(dir, "key.pem")
	keyOut, err := os.Create(keyPath)
	require.NoError(t, err)
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes})
	keyOut.Close()

	return certPath, keyPath
}

func TestBuildAMQPTLSConfig(t *testing.T) {
	// Create temp dir for certs
	tempDir, err := os.MkdirTemp("", "tls_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	certPath, keyPath := generateTestCert(t, tempDir)

	tests := []struct {
		name        string
		config      AMQPTLSConfig
		shouldError bool
		verify      func(*testing.T, *AMQPTLSConfig)
	}{
		{
			name: "Insecure Skip Verify",
			config: AMQPTLSConfig{
				Enabled:    true,
				SkipVerify: true,
			},
			shouldError: false,
		},
		{
			name: "With Cert and Key",
			config: AMQPTLSConfig{
				Enabled:  true,
				CertFile: certPath,
				KeyFile:  keyPath,
			},
			shouldError: false,
		},
		{
			name: "With CA File",
			config: AMQPTLSConfig{
				Enabled: true,
				CAFile:  certPath, // Using the cert as CA for rudimentary check
			},
			shouldError: false,
		},
		{
			name: "Missing Key File",
			config: AMQPTLSConfig{
				Enabled:  true,
				CertFile: certPath,
			},
			shouldError: true,
		},
		{
			name: "Invalid Cert Path",
			config: AMQPTLSConfig{
				Enabled:  true,
				CertFile: "non_existent.pem",
				KeyFile:  keyPath,
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Access internal function via export in same package
			tlsConfig, err := buildAMQPTLSConfig(tt.config)
			if tt.shouldError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.NotNil(t, tlsConfig)

			if tt.config.SkipVerify {
				assert.True(t, tlsConfig.InsecureSkipVerify)
			}
			if tt.config.CertFile != "" && tt.config.KeyFile != "" {
				assert.NotEmpty(t, tlsConfig.Certificates)
			}
			if tt.config.CAFile != "" {
				assert.NotNil(t, tlsConfig.RootCAs)
			}
		})
	}
}
