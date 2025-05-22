package config

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/errors"
)

// Config represents the complete application configuration
type Config struct {
	Network    NetworkConfig    `json:"network"`
	HTTP       HTTPConfig       `json:"http"`
	Recording  RecordingConfig  `json:"recording"`
	STT        STTConfig        `json:"stt"`
	Resources  ResourceConfig   `json:"resources"`
	Logging    LoggingConfig    `json:"logging"`
	Messaging  MessagingConfig  `json:"messaging"`
	Redundancy RedundancyConfig `json:"redundancy"`
}

// NetworkConfig holds network-related configurations
type NetworkConfig struct {
	// External IP address for SIP/RTP (auto = auto-detect)
	ExternalIP string `json:"external_ip" env:"EXTERNAL_IP" default:"auto"`

	// Internal IP address for binding (auto = auto-detect)
	InternalIP string `json:"internal_ip" env:"INTERNAL_IP" default:"auto"`

	// SIP ports to listen on
	Ports []int `json:"ports" env:"PORTS" default:"5060,5061"`

	// Whether SRTP is enabled
	EnableSRTP bool `json:"enable_srtp" env:"ENABLE_SRTP" default:"false"`

	// RTP port range minimum
	RTPPortMin int `json:"rtp_port_min" env:"RTP_PORT_MIN" default:"10000"`

	// RTP port range maximum
	RTPPortMax int `json:"rtp_port_max" env:"RTP_PORT_MAX" default:"20000"`

	// TLS certificate file
	TLSCertFile string `json:"tls_cert_file" env:"TLS_CERT_PATH"`

	// TLS key file
	TLSKeyFile string `json:"tls_key_file" env:"TLS_KEY_PATH"`

	// TLS port
	TLSPort int `json:"tls_port" env:"TLS_PORT" default:"5062"`

	// Whether TLS is enabled
	EnableTLS bool `json:"enable_tls" env:"ENABLE_TLS" default:"false"`

	// Whether the server is behind NAT
	BehindNAT bool `json:"behind_nat" env:"BEHIND_NAT" default:"false"`

	// STUN servers for NAT traversal
	STUNServers []string `json:"stun_servers" env:"STUN_SERVER"`

	// Whether audio processing is enabled
	EnableAudioProcessing bool `json:"enable_audio_processing" env:"ENABLE_AUDIO_PROCESSING" default:"true"`
}

// HTTPConfig holds HTTP server configurations
type HTTPConfig struct {
	// HTTP port
	Port int `json:"port" env:"HTTP_PORT" default:"8080"`

	// Whether HTTP server is enabled
	Enabled bool `json:"enabled" env:"HTTP_ENABLED" default:"true"`

	// Whether metrics endpoint is enabled
	EnableMetrics bool `json:"enable_metrics" env:"HTTP_ENABLE_METRICS" default:"true"`

	// Whether API endpoints are enabled
	EnableAPI bool `json:"enable_api" env:"HTTP_ENABLE_API" default:"true"`

	// Read timeout for HTTP requests
	ReadTimeout time.Duration `json:"read_timeout" env:"HTTP_READ_TIMEOUT" default:"10s"`

	// Write timeout for HTTP responses
	WriteTimeout time.Duration `json:"write_timeout" env:"HTTP_WRITE_TIMEOUT" default:"30s"`
}

// RecordingConfig holds recording-related configurations
type RecordingConfig struct {
	// Directory to store recordings
	Directory string `json:"directory" env:"RECORDING_DIR" default:"./recordings"`

	// Maximum duration for recordings
	MaxDuration time.Duration `json:"max_duration" env:"RECORDING_MAX_DURATION_HOURS" default:"4h"`

	// Days to keep recordings before cleanup
	CleanupDays int `json:"cleanup_days" env:"RECORDING_CLEANUP_DAYS" default:"30"`
}

// STTConfig holds speech-to-text configurations
type STTConfig struct {
	// Supported STT vendors
	SupportedVendors []string `json:"supported_vendors" env:"SUPPORTED_VENDORS" default:"google"`

	// Supported audio codecs
	SupportedCodecs []string `json:"supported_codecs" env:"SUPPORTED_CODECS" default:"PCMU,PCMA,G722"`

	// Default STT vendor
	DefaultVendor string `json:"default_vendor" env:"DEFAULT_SPEECH_VENDOR" default:"google"`
}

// ResourceConfig holds resource limitation configurations
type ResourceConfig struct {
	// Maximum concurrent calls
	MaxConcurrentCalls int `json:"max_concurrent_calls" env:"MAX_CONCURRENT_CALLS" default:"500"`
}

// LoggingConfig holds logging-related configurations
type LoggingConfig struct {
	// Log level
	Level string `json:"level" env:"LOG_LEVEL" default:"info"`

	// Log format (json or text)
	Format string `json:"format" env:"LOG_FORMAT" default:"json"`

	// Log output file (empty = stdout)
	OutputFile string `json:"output_file" env:"LOG_OUTPUT_FILE"`
}

// MessagingConfig holds messaging-related configurations
type MessagingConfig struct {
	// AMQP URL
	AMQPUrl string `json:"amqp_url" env:"AMQP_URL"`

	// AMQP queue name
	AMQPQueueName string `json:"amqp_queue_name" env:"AMQP_QUEUE_NAME"`
}

// RedundancyConfig holds session redundancy configurations
type RedundancyConfig struct {
	// Whether session redundancy is enabled
	Enabled bool `json:"enabled" env:"ENABLE_REDUNDANCY" default:"true"`

	// Session timeout
	SessionTimeout time.Duration `json:"session_timeout" env:"SESSION_TIMEOUT" default:"30s"`

	// Session check interval
	SessionCheckInterval time.Duration `json:"session_check_interval" env:"SESSION_CHECK_INTERVAL" default:"10s"`

	// Storage type for redundancy (memory, redis)
	StorageType string `json:"storage_type" env:"REDUNDANCY_STORAGE_TYPE" default:"memory"`
}

// Load loads the configuration from environment variables or .env file
func Load(logger *logrus.Logger) (*Config, error) {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		logger.WithError(err).Warn("Failed to get current working directory")
		wd = "unknown"
	}

	// Define possible locations for .env file
	possibleEnvFiles := []string{
		".env",                    // Current directory
		"../.env",                 // Parent directory
		filepath.Join(wd, ".env"), // Absolute path
	}

	// Try loading .env file from each possible location
	var loadedFrom string
	var loadErr error

	for _, envFile := range possibleEnvFiles {
		// Try to load this .env file
		if _, statErr := os.Stat(envFile); statErr == nil {
			absPath, _ := filepath.Abs(envFile)
			logger.WithField("path", absPath).Debug("Attempting to load .env file")

			if loadErr = godotenv.Load(envFile); loadErr == nil {
				loadedFrom = absPath
				break
			}
		}
	}

	// If all attempts failed, try default Load() which uses working directory
	if loadedFrom == "" {
		if loadErr = godotenv.Load(); loadErr == nil {
			if _, statErr := os.Stat(".env"); statErr == nil {
				loadedFrom, _ = filepath.Abs(".env")
			}
		}
	}

	// Report results
	if loadedFrom != "" {
		logger.WithFields(logrus.Fields{
			"working_dir": wd,
			"path":        loadedFrom,
		}).Info("Successfully loaded .env file")
	} else {
		logger.WithField("working_dir", wd).Warn("No .env file found, using environment variables only")
	}

	// Initialize config with default values
	config := &Config{}

	// Load network configuration
	if err := loadNetworkConfig(logger, &config.Network); err != nil {
		return nil, errors.Wrap(err, "failed to load network configuration")
	}

	// Load HTTP configuration
	if err := loadHTTPConfig(logger, &config.HTTP); err != nil {
		return nil, errors.Wrap(err, "failed to load HTTP configuration")
	}

	// Load recording configuration
	if err := loadRecordingConfig(logger, &config.Recording); err != nil {
		return nil, errors.Wrap(err, "failed to load recording configuration")
	}

	// Load STT configuration
	if err := loadSTTConfig(logger, &config.STT); err != nil {
		return nil, errors.Wrap(err, "failed to load STT configuration")
	}

	// Load resource configuration
	if err := loadResourceConfig(logger, &config.Resources); err != nil {
		return nil, errors.Wrap(err, "failed to load resource configuration")
	}

	// Load logging configuration
	if err := loadLoggingConfig(logger, &config.Logging); err != nil {
		return nil, errors.Wrap(err, "failed to load logging configuration")
	}

	// Load messaging configuration
	if err := loadMessagingConfig(logger, &config.Messaging); err != nil {
		return nil, errors.Wrap(err, "failed to load messaging configuration")
	}

	// Load redundancy configuration
	if err := loadRedundancyConfig(logger, &config.Redundancy); err != nil {
		return nil, errors.Wrap(err, "failed to load redundancy configuration")
	}

	// Validate the complete configuration
	if err := validateConfig(logger, config); err != nil {
		return nil, errors.Wrap(err, "configuration validation failed")
	}

	// Ensure required directories exist
	if err := ensureDirectories(logger, config); err != nil {
		return nil, errors.Wrap(err, "failed to create required directories")
	}

	return config, nil
}

// loadNetworkConfig loads the network configuration section
func loadNetworkConfig(logger *logrus.Logger, config *NetworkConfig) error {
	// Load external IP
	config.ExternalIP = getEnv("EXTERNAL_IP", "auto")
	if config.ExternalIP == "auto" {
		// Auto-detect external IP
		config.ExternalIP = getExternalIP(logger)
		logger.WithField("external_ip", config.ExternalIP).Info("Auto-detected external IP")
	}

	// Load internal IP
	config.InternalIP = getEnv("INTERNAL_IP", "auto")
	if config.InternalIP == "auto" {
		// Auto-detect internal IP
		config.InternalIP = getInternalIP(logger)
		logger.WithField("internal_ip", config.InternalIP).Info("Auto-detected internal IP")
	}

	// Load SIP ports
	portsStr := getEnv("PORTS", "5060,5061")
	logger.WithField("ports_env", portsStr).Debug("Loaded PORTS environment variable")
	portsSlice := strings.Split(portsStr, ",")

	for _, portStr := range portsSlice {
		portStr = strings.TrimSpace(portStr)
		if portStr == "" {
			continue
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid port in PORTS: %s", portStr))
		}

		if port < 1 || port > 65535 {
			return errors.New(fmt.Sprintf("port out of range in PORTS: %d", port))
		}

		config.Ports = append(config.Ports, port)
	}

	// If no valid ports were specified, use defaults
	if len(config.Ports) == 0 {
		config.Ports = []int{5060, 5061}
		logger.Warn("No valid ports specified, using defaults: 5060, 5061")
	} else {
		logger.WithField("sip_ports", config.Ports).Info("Configured SIP ports")
	}

	// Load RTP port range
	rtpMinStr := getEnv("RTP_PORT_MIN", "10000")
	rtpMin, err := strconv.Atoi(rtpMinStr)
	if err != nil || rtpMin < 1024 || rtpMin > 65000 {
		logger.Warn("Invalid RTP_PORT_MIN value, using default: 10000")
		config.RTPPortMin = 10000
	} else {
		config.RTPPortMin = rtpMin
	}

	rtpMaxStr := getEnv("RTP_PORT_MAX", "20000")
	rtpMax, err := strconv.Atoi(rtpMaxStr)
	if err != nil || rtpMax <= config.RTPPortMin || rtpMax > 65535 {
		logger.Warn("Invalid RTP_PORT_MAX value, using default: 20000")
		config.RTPPortMax = 20000
	} else {
		config.RTPPortMax = rtpMax
	}

	// Ensure there are enough ports in the range
	if (config.RTPPortMax - config.RTPPortMin) < 100 {
		logger.Warn("RTP port range too small, at least 100 ports are recommended")
	}

	// Load TLS configuration
	config.TLSCertFile = getEnv("TLS_CERT_PATH", "")
	config.TLSKeyFile = getEnv("TLS_KEY_PATH", "")

	tlsPortStr := getEnv("TLS_PORT", "5062")
	tlsPort, err := strconv.Atoi(tlsPortStr)
	if err != nil || tlsPort < 1 || tlsPort > 65535 {
		logger.Warn("Invalid TLS_PORT value, using default: 5062")
		config.TLSPort = 5062
	} else {
		config.TLSPort = tlsPort
	}

	// Load feature flags
	config.EnableTLS = getEnvBool("ENABLE_TLS", false)
	config.EnableSRTP = getEnvBool("ENABLE_SRTP", false)
	config.BehindNAT = getEnvBool("BEHIND_NAT", false)

	// If TLS is enabled, ensure certificates are provided
	if config.EnableTLS && (config.TLSCertFile == "" || config.TLSKeyFile == "") {
		return errors.New("TLS is enabled but certificate or key file is missing. Please provide both TLS_CERT_PATH and TLS_KEY_PATH environment variables")
	}

	// Load STUN servers
	stunServersStr := getEnv("STUN_SERVER", "")
	if stunServersStr == "" {
		// Default Google STUN servers
		config.STUNServers = []string{
			"stun.l.google.com:19302",
			"stun1.l.google.com:19302",
			"stun2.l.google.com:19302",
			"stun3.l.google.com:19302",
			"stun4.l.google.com:19302",
		}
		logger.Info("Using default Google STUN servers")
	} else {
		config.STUNServers = strings.Split(stunServersStr, ",")
		for i, server := range config.STUNServers {
			config.STUNServers[i] = strings.TrimSpace(server)
		}
	}

	return nil
}

// loadHTTPConfig loads the HTTP server configuration section
func loadHTTPConfig(logger *logrus.Logger, config *HTTPConfig) error {
	// Load HTTP port
	httpPortStr := getEnv("HTTP_PORT", "8080")
	httpPort, err := strconv.Atoi(httpPortStr)
	if err != nil || httpPort < 1 || httpPort > 65535 {
		logger.Warn("Invalid HTTP_PORT value, using default: 8080")
		config.Port = 8080
	} else {
		config.Port = httpPort
	}

	// Load feature flags
	config.Enabled = getEnvBool("HTTP_ENABLED", true)
	config.EnableMetrics = getEnvBool("HTTP_ENABLE_METRICS", true)
	config.EnableAPI = getEnvBool("HTTP_ENABLE_API", true)

	// Load timeouts
	readTimeoutStr := getEnv("HTTP_READ_TIMEOUT", "10s")
	readTimeout, err := time.ParseDuration(readTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_READ_TIMEOUT value, using default: 10s")
		config.ReadTimeout = 10 * time.Second
	} else {
		config.ReadTimeout = readTimeout
	}

	writeTimeoutStr := getEnv("HTTP_WRITE_TIMEOUT", "30s")
	writeTimeout, err := time.ParseDuration(writeTimeoutStr)
	if err != nil {
		logger.Warn("Invalid HTTP_WRITE_TIMEOUT value, using default: 30s")
		config.WriteTimeout = 30 * time.Second
	} else {
		config.WriteTimeout = writeTimeout
	}

	return nil
}

// loadRecordingConfig loads the recording configuration section
func loadRecordingConfig(logger *logrus.Logger, config *RecordingConfig) error {
	// Load recording directory
	config.Directory = getEnv("RECORDING_DIR", "./recordings")

	// Load recording max duration
	maxDurationStr := getEnv("RECORDING_MAX_DURATION_HOURS", "4")
	maxDuration, err := strconv.Atoi(maxDurationStr)
	if err != nil || maxDuration < 1 {
		logger.Warn("Invalid RECORDING_MAX_DURATION_HOURS value, using default: 4 hours")
		config.MaxDuration = 4 * time.Hour
	} else {
		config.MaxDuration = time.Duration(maxDuration) * time.Hour
	}

	// Load recording cleanup days
	cleanupDaysStr := getEnv("RECORDING_CLEANUP_DAYS", "30")
	cleanupDays, err := strconv.Atoi(cleanupDaysStr)
	if err != nil || cleanupDays < 1 {
		logger.Warn("Invalid RECORDING_CLEANUP_DAYS value, using default: 30 days")
		config.CleanupDays = 30
	} else {
		config.CleanupDays = cleanupDays
	}

	return nil
}

// loadSTTConfig loads the speech-to-text configuration section
func loadSTTConfig(logger *logrus.Logger, config *STTConfig) error {
	// Load supported vendors
	vendorsStr := getEnv("SUPPORTED_VENDORS", "google")
	if vendorsStr == "" {
		config.SupportedVendors = []string{"google"}
		logger.Info("No STT vendors specified, defaulting to: google")
	} else {
		vendors := strings.Split(vendorsStr, ",")
		for i, vendor := range vendors {
			vendors[i] = strings.TrimSpace(vendor)
		}
		config.SupportedVendors = vendors
	}

	// Load supported codecs
	codecsStr := getEnv("SUPPORTED_CODECS", "PCMU,PCMA,G722")
	if codecsStr == "" {
		config.SupportedCodecs = []string{"PCMU", "PCMA", "G722"}
		logger.Info("No codecs specified, defaulting to: PCMU, PCMA, G722")
	} else {
		codecs := strings.Split(codecsStr, ",")
		for i, codec := range codecs {
			codecs[i] = strings.TrimSpace(codec)
		}
		config.SupportedCodecs = codecs
	}

	// Load default vendor
	config.DefaultVendor = getEnv("DEFAULT_SPEECH_VENDOR", "google")
	if config.DefaultVendor == "" {
		logger.Warn("DEFAULT_SPEECH_VENDOR not set, using default: google")
		config.DefaultVendor = "google"
	}

	// Validate that the default vendor is in the supported vendors list
	found := false
	for _, vendor := range config.SupportedVendors {
		if vendor == config.DefaultVendor {
			found = true
			break
		}
	}

	if !found {
		logger.Warnf("Default vendor '%s' is not in the supported vendors list, adding it", config.DefaultVendor)
		config.SupportedVendors = append(config.SupportedVendors, config.DefaultVendor)
	}

	return nil
}

// loadResourceConfig loads the resource configuration section
func loadResourceConfig(logger *logrus.Logger, config *ResourceConfig) error {
	// Load max concurrent calls
	maxCallsStr := getEnv("MAX_CONCURRENT_CALLS", "500")
	maxCalls, err := strconv.Atoi(maxCallsStr)
	if err != nil || maxCalls < 1 {
		logger.Warn("Invalid MAX_CONCURRENT_CALLS value, using default: 500")
		config.MaxConcurrentCalls = 500
	} else {
		config.MaxConcurrentCalls = maxCalls
	}

	return nil
}

// loadLoggingConfig loads the logging configuration section
func loadLoggingConfig(logger *logrus.Logger, config *LoggingConfig) error {
	// Load log level
	config.Level = getEnv("LOG_LEVEL", "info")

	// Validate log level
	_, err := logrus.ParseLevel(config.Level)
	if err != nil {
		logger.Warnf("Invalid LOG_LEVEL '%s', defaulting to 'info'", config.Level)
		config.Level = "info"
	}

	// Load log format
	config.Format = getEnv("LOG_FORMAT", "json")
	if config.Format != "json" && config.Format != "text" {
		logger.Warn("Invalid LOG_FORMAT, must be 'json' or 'text', defaulting to 'json'")
		config.Format = "json"
	}

	// Load log output file
	config.OutputFile = getEnv("LOG_OUTPUT_FILE", "")

	return nil
}

// loadMessagingConfig loads the messaging configuration section
func loadMessagingConfig(logger *logrus.Logger, config *MessagingConfig) error {
	// Load AMQP URL
	config.AMQPUrl = getEnv("AMQP_URL", "")

	// Load AMQP queue name
	config.AMQPQueueName = getEnv("AMQP_QUEUE_NAME", "")

	// Validate AMQP config - both URL and queue name must be provided
	if (config.AMQPUrl != "" && config.AMQPQueueName == "") || (config.AMQPUrl == "" && config.AMQPQueueName != "") {
		logger.Warn("Incomplete AMQP configuration: both AMQP_URL and AMQP_QUEUE_NAME must be provided")
	}

	return nil
}

// loadRedundancyConfig loads the redundancy configuration section
func loadRedundancyConfig(logger *logrus.Logger, config *RedundancyConfig) error {
	// Load redundancy enabled flag
	config.Enabled = getEnvBool("ENABLE_REDUNDANCY", true)

	// Load session timeout
	sessionTimeoutStr := getEnv("SESSION_TIMEOUT", "30s")
	sessionTimeout, err := time.ParseDuration(sessionTimeoutStr)
	if err != nil {
		logger.Warn("Invalid SESSION_TIMEOUT value, using default: 30s")
		config.SessionTimeout = 30 * time.Second
	} else {
		config.SessionTimeout = sessionTimeout
	}

	// Load session check interval
	sessionCheckIntervalStr := getEnv("SESSION_CHECK_INTERVAL", "10s")
	sessionCheckInterval, err := time.ParseDuration(sessionCheckIntervalStr)
	if err != nil {
		logger.Warn("Invalid SESSION_CHECK_INTERVAL value, using default: 10s")
		config.SessionCheckInterval = 10 * time.Second
	} else {
		config.SessionCheckInterval = sessionCheckInterval
	}

	// Load storage type
	config.StorageType = getEnv("REDUNDANCY_STORAGE_TYPE", "memory")
	if config.StorageType != "memory" && config.StorageType != "redis" {
		logger.Warn("Invalid REDUNDANCY_STORAGE_TYPE value, must be 'memory' or 'redis', using default: memory")
		config.StorageType = "memory"
	}

	return nil
}

// validateConfig performs cross-section validation of the configuration
func validateConfig(logger *logrus.Logger, config *Config) error {
	// Validate port conflicts
	for _, sipPort := range config.Network.Ports {
		if sipPort == config.HTTP.Port {
			return errors.New(fmt.Sprintf("port conflict: SIP port %d conflicts with HTTP port", sipPort))
		}

		if config.Network.EnableTLS && sipPort == config.Network.TLSPort {
			return errors.New(fmt.Sprintf("port conflict: SIP port %d conflicts with TLS port", sipPort))
		}
	}

	if config.Network.EnableTLS && config.Network.TLSPort == config.HTTP.Port {
		return errors.New(fmt.Sprintf("port conflict: TLS port %d conflicts with HTTP port", config.Network.TLSPort))
	}

	// Validate RTP port range
	if config.Network.RTPPortMax <= config.Network.RTPPortMin {
		return errors.New("invalid RTP port range: RTP_PORT_MAX must be greater than RTP_PORT_MIN")
	}

	// Validate redundancy configuration
	if config.Redundancy.Enabled {
		if config.Redundancy.SessionTimeout <= 0 {
			return errors.New("invalid SESSION_TIMEOUT: must be a positive duration")
		}

		if config.Redundancy.SessionCheckInterval <= 0 {
			return errors.New("invalid SESSION_CHECK_INTERVAL: must be a positive duration")
		}

		if config.Redundancy.SessionCheckInterval >= config.Redundancy.SessionTimeout {
			logger.Warn("SESSION_CHECK_INTERVAL should be smaller than SESSION_TIMEOUT for effective monitoring")
		}

		if config.Redundancy.StorageType == "redis" && config.Messaging.AMQPUrl == "" {
			logger.Warn("Redis storage type selected but AMQP not configured for notifications")
		}
	}

	// Validate logging configuration
	if config.Logging.OutputFile != "" {
		// Check if the log file can be created/written
		f, err := os.OpenFile(config.Logging.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("cannot write to log file: %s", config.Logging.OutputFile))
		}
		f.Close()
	}

	return nil
}

// ensureDirectories ensures that required directories exist
func ensureDirectories(logger *logrus.Logger, config *Config) error {
	// Ensure recording directory exists
	if err := os.MkdirAll(config.Recording.Directory, 0755); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create recording directory: %s", config.Recording.Directory))
	}

	// Ensure sessions directory exists if redundancy is enabled
	if config.Redundancy.Enabled && config.Redundancy.StorageType == "memory" {
		sessionsDir := "sessions"
		if err := os.MkdirAll(sessionsDir, 0755); err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to create sessions directory: %s", sessionsDir))
		}
	}

	return nil
}

// Apply applies the configuration to the logger
func (c *Config) ApplyLogging(logger *logrus.Logger) error {
	// Set log level
	level, err := logrus.ParseLevel(c.Logging.Level)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("invalid log level: %s", c.Logging.Level))
	}
	logger.SetLevel(level)

	// Set log format
	if c.Logging.Format == "json" {
		logger.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: time.RFC3339Nano,
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyTime:  "timestamp",
				logrus.FieldKeyLevel: "level",
				logrus.FieldKeyMsg:   "message",
			},
		})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: time.RFC3339Nano,
		})
	}

	// Set log output
	if c.Logging.OutputFile != "" {
		f, err := os.OpenFile(c.Logging.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to open log file: %s", c.Logging.OutputFile))
		}
		logger.SetOutput(f)
	} else {
		logger.SetOutput(os.Stdout)
	}

	return nil
}

// Helper function to get an environment variable with a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// Helper function to get a boolean environment variable with a default value
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	switch strings.ToLower(value) {
	case "true", "yes", "1", "on":
		return true
	case "false", "no", "0", "off":
		return false
	default:
		return defaultValue
	}
}

// Helper function to get an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}

	return intValue
}

// Helper function to get a duration environment variable with a default value
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}

	return duration
}

// Helper function to get external IP
func getExternalIP(logger *logrus.Logger) string {
	// Try multiple services to be resilient
	services := []string{
		"https://api.ipify.org",
		"https://ifconfig.me",
		"https://icanhazip.com",
	}

	for _, service := range services {
		resp, err := http.Get(service)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body := make([]byte, 100)
			n, err := resp.Body.Read(body)
			if err != nil && err.Error() != "EOF" {
				continue
			}
			return strings.TrimSpace(string(body[:n]))
		}
	}

	logger.Warn("Could not auto-detect external IP, using localhost as fallback")
	return "127.0.0.1" // Fallback
}

// Helper function to get internal IP
func getInternalIP(logger *logrus.Logger) string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Warn("Could not get interface addresses, using localhost as fallback")
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	logger.Warn("Could not find non-loopback interface address, using localhost as fallback")
	return "127.0.0.1"
}
