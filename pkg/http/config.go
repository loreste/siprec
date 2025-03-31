package http

import "time"

// Config holds the HTTP server configuration
type Config struct {
	// Port is the HTTP server port
	Port int `json:"port" env:"HTTP_PORT" default:"8080"`
	
	// Enabled determines if the HTTP server should be started
	Enabled bool `json:"enabled" env:"HTTP_ENABLED" default:"true"`
	
	// Path is the base path for all endpoints
	Path string `json:"path" env:"HTTP_PATH" default:"/"`
	
	// EnableMetrics determines if metrics should be enabled
	EnableMetrics bool `json:"enable_metrics" env:"HTTP_ENABLE_METRICS" default:"true"`
	
	// EnableAPI determines if API endpoints should be enabled
	EnableAPI bool `json:"enable_api" env:"HTTP_ENABLE_API" default:"true"`
	
	// MetricsPath is the path for metrics endpoint
	MetricsPath string `json:"metrics_path" env:"HTTP_METRICS_PATH" default:"/metrics"`
	
	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration `json:"read_timeout" env:"HTTP_READ_TIMEOUT" default:"10s"`
	
	// WriteTimeout is the maximum duration before timing out writes of the response
	WriteTimeout time.Duration `json:"write_timeout" env:"HTTP_WRITE_TIMEOUT" default:"30s"`
	
	// IdleTimeout is the maximum amount of time to wait for the next request
	IdleTimeout time.Duration `json:"idle_timeout" env:"HTTP_IDLE_TIMEOUT" default:"60s"`
	
	// ShutdownTimeout is the maximum duration to wait for the server to shutdown
	ShutdownTimeout time.Duration `json:"shutdown_timeout" env:"HTTP_SHUTDOWN_TIMEOUT" default:"5s"`
	
	// TLS configuration
	TLSEnabled bool   `json:"tls_enabled" env:"HTTP_TLS_ENABLED" default:"false"`
	TLSCertFile string `json:"tls_cert_file" env:"HTTP_TLS_CERT_FILE"`
	TLSKeyFile  string `json:"tls_key_file" env:"HTTP_TLS_KEY_FILE"`
}

// NewDefaultConfig returns a new default configuration
func NewDefaultConfig() *Config {
	return &Config{
		Port:            8080,
		Enabled:         true,
		Path:            "/",
		EnableMetrics:   true,
		EnableAPI:       true,
		MetricsPath:     "/metrics",
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
		TLSEnabled:      false,
	}
}

// DefaultConfig returns default configuration for the HTTP server (for compatibility)
func DefaultConfig() *Config {
	return NewDefaultConfig()
}