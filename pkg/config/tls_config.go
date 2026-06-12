package config

import (
	"time"
)

// TLSConfig holds TLS configuration
type TLSConfig struct {
	Enabled                bool          `yaml:"enabled" env:"ENABLE_TLS" default:"false"`
	CertFile               string        `yaml:"cert_file" env:"TLS_CERT_FILE"`
	KeyFile                string        `yaml:"key_file" env:"TLS_KEY_FILE"`
	CAFile                 string        `yaml:"ca_file" env:"TLS_CA_FILE"`
	ClientAuth             string        `yaml:"client_auth" env:"TLS_CLIENT_AUTH" default:"none"` // none, request, require
	MinVersion             string        `yaml:"min_version" env:"TLS_MIN_VERSION" default:"1.3"`
	CipherSuites           []string      `yaml:"cipher_suites" env:"TLS_CIPHER_SUITES"`
	PreferServerCiphers    bool          `yaml:"prefer_server_ciphers" env:"TLS_PREFER_SERVER_CIPHERS" default:"true"`
	SessionTicketsDisabled bool          `yaml:"session_tickets_disabled" env:"TLS_SESSION_TICKETS_DISABLED" default:"false"`
	SessionTimeout         time.Duration `yaml:"session_timeout" env:"TLS_SESSION_TIMEOUT" default:"10m"`
	InsecureSkipVerify     bool          `yaml:"insecure_skip_verify" env:"TLS_INSECURE_SKIP_VERIFY" default:"false"`
}
