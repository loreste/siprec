package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/sirupsen/logrus"
)

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled              bool          `yaml:"enabled" env:"ENABLE_TLS" default:"false"`
	CertFile             string        `yaml:"cert_file" env:"TLS_CERT_FILE"`
	KeyFile              string        `yaml:"key_file" env:"TLS_KEY_FILE"`
	CAFile               string        `yaml:"ca_file" env:"TLS_CA_FILE"`
	ClientAuth           string        `yaml:"client_auth" env:"TLS_CLIENT_AUTH" default:"none"` // none, request, require
	MinVersion           string        `yaml:"min_version" env:"TLS_MIN_VERSION" default:"1.3"`
	CipherSuites         []string      `yaml:"cipher_suites" env:"TLS_CIPHER_SUITES"`
	PreferServerCiphers  bool          `yaml:"prefer_server_ciphers" env:"TLS_PREFER_SERVER_CIPHERS" default:"true"`
	SessionTicketsDisabled bool        `yaml:"session_tickets_disabled" env:"TLS_SESSION_TICKETS_DISABLED" default:"false"`
	SessionTimeout       time.Duration `yaml:"session_timeout" env:"TLS_SESSION_TIMEOUT" default:"10m"`
	InsecureSkipVerify   bool          `yaml:"insecure_skip_verify" env:"TLS_INSECURE_SKIP_VERIFY" default:"false"`
}

// GetTLSConfig creates a *tls.Config from TLSConfig
func (tc *TLSConfig) GetTLSConfig(logger *logrus.Logger) (*tls.Config, error) {
	if !tc.Enabled {
		return nil, nil
	}

	// Create base TLS config with secure defaults
	tlsConfig := &tls.Config{
		MinVersion:               tls.VersionTLS13, // Default to TLS 1.3
		PreferServerCipherSuites: tc.PreferServerCiphers,
		SessionTicketsDisabled:   tc.SessionTicketsDisabled,
		InsecureSkipVerify:      tc.InsecureSkipVerify,
	}

	// Set minimum version
	switch tc.MinVersion {
	case "1.2":
		tlsConfig.MinVersion = tls.VersionTLS12
		logger.Warning("TLS 1.2 is deprecated, consider upgrading to TLS 1.3")
	case "1.3":
		tlsConfig.MinVersion = tls.VersionTLS13
	default:
		tlsConfig.MinVersion = tls.VersionTLS13
		logger.WithField("version", tc.MinVersion).Warning("Unknown TLS version, defaulting to 1.3")
	}

	// Load certificates
	if tc.CertFile != "" && tc.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tc.CertFile, tc.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS certificates: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		logger.WithFields(logrus.Fields{
			"cert": tc.CertFile,
			"key":  tc.KeyFile,
		}).Info("Loaded TLS certificates")
	}

	// Load CA certificate if provided
	if tc.CAFile != "" {
		caCert, err := ioutil.ReadFile(tc.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = caCertPool
		tlsConfig.RootCAs = caCertPool
		logger.WithField("ca", tc.CAFile).Info("Loaded CA certificate")
	}

	// Set client authentication
	switch tc.ClientAuth {
	case "none":
		tlsConfig.ClientAuth = tls.NoClientCert
	case "request":
		tlsConfig.ClientAuth = tls.RequestClientCert
	case "require":
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		if tlsConfig.ClientCAs == nil {
			return nil, fmt.Errorf("client certificate verification requires CA certificate")
		}
	default:
		tlsConfig.ClientAuth = tls.NoClientCert
	}

	// Set cipher suites for TLS 1.2 (TLS 1.3 uses fixed cipher suites)
	if tlsConfig.MinVersion == tls.VersionTLS12 && len(tc.CipherSuites) > 0 {
		cipherSuites := []uint16{}
		for _, suite := range tc.CipherSuites {
			if cipherID, ok := getCipherSuiteID(suite); ok {
				cipherSuites = append(cipherSuites, cipherID)
			} else {
				logger.WithField("cipher", suite).Warning("Unknown cipher suite")
			}
		}
		if len(cipherSuites) > 0 {
			tlsConfig.CipherSuites = cipherSuites
		}
	} else if tlsConfig.MinVersion == tls.VersionTLS13 {
		// TLS 1.3 cipher suites (these are the only ones available)
		logger.Info("Using TLS 1.3 default cipher suites: TLS_AES_128_GCM_SHA256, TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256")
	}

	// Log TLS configuration
	logger.WithFields(logrus.Fields{
		"min_version":    tc.MinVersion,
		"client_auth":    tc.ClientAuth,
		"cipher_suites":  len(tlsConfig.CipherSuites),
		"session_tickets": !tc.SessionTicketsDisabled,
	}).Info("TLS configuration loaded")

	return tlsConfig, nil
}

// getCipherSuiteID returns the cipher suite ID for a given name
func getCipherSuiteID(name string) (uint16, bool) {
	// Map of cipher suite names to IDs (TLS 1.2 only, TLS 1.3 has fixed suites)
	cipherSuites := map[string]uint16{
		"TLS_RSA_WITH_AES_128_GCM_SHA256":               tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_RSA_WITH_AES_256_GCM_SHA384":               tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	id, ok := cipherSuites[name]
	return id, ok
}

// DefaultTLSConfig returns a secure default TLS configuration
func DefaultTLSConfig() *TLSConfig {
	return &TLSConfig{
		Enabled:             false,
		MinVersion:          "1.3",
		PreferServerCiphers: true,
		ClientAuth:          "none",
		CipherSuites: []string{
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
			"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
			"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
			"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
		},
	}
}