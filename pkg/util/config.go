package util

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// Configuration defines the structure for storing application configuration
type Configuration struct {
	// Network configuration
	ExternalIP  string
	InternalIP  string
	Ports       []int
	EnableSRTP  bool
	RTPPortMin  int
	RTPPortMax  int
	TLSCertFile string
	TLSKeyFile  string
	TLSPort     int
	EnableTLS   bool
	BehindNAT   bool
	STUNServers []string

	// HTTP server configuration
	HTTPPort          int
	HTTPEnabled       bool
	HTTPEnableMetrics bool
	HTTPEnableAPI     bool

	// Recording configuration
	RecordingDir         string
	RecordingMaxDuration time.Duration
	RecordingCleanupDays int

	// Speech-to-text configuration
	SupportedVendors []string
	SupportedCodecs  []string
	DefaultVendor    string

	// Resource limits
	MaxConcurrentCalls int

	// Logging
	LogLevel logrus.Level

	// AMQP configuration
	AMQPUrl       string
	AMQPQueueName string
}

// LoadConfig loads the application configuration from environment variables
func LoadConfig(logger *logrus.Logger) (*Configuration, error) {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf("error loading .env file: %w", err)
	}

	config := &Configuration{}

	// Load general configuration
	config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
	config.ExternalIP = os.Getenv("EXTERNAL_IP")
	config.InternalIP = os.Getenv("INTERNAL_IP")

	// Load codecs
	codecsEnv := os.Getenv("SUPPORTED_CODECS")
	if codecsEnv == "" {
		config.SupportedCodecs = []string{"PCMU", "PCMA", "G722", "G729"}
		logger.Info("No SUPPORTED_CODECS specified, defaulting to PCMU, PCMA, G722, G729")
	} else {
		config.SupportedCodecs = strings.Split(codecsEnv, ",")
	}

	// Load vendors
	vendorsEnv := os.Getenv("SUPPORTED_VENDORS")
	if vendorsEnv == "" {
		config.SupportedVendors = []string{"google", "openai"}
	} else {
		config.SupportedVendors = strings.Split(vendorsEnv, ",")
	}

	config.DefaultVendor = os.Getenv("DEFAULT_SPEECH_VENDOR")

	// Auto-detect IP if not set
	if config.ExternalIP == "auto" || config.ExternalIP == "" {
		// Use a best-effort method to determine external IP
		config.ExternalIP = getExternalIP(logger)
		logger.WithField("external_ip", config.ExternalIP).Info("Auto-detected external IP")
	}

	if config.InternalIP == "auto" || config.InternalIP == "" {
		config.InternalIP = getInternalIP(logger)
		logger.WithField("internal_ip", config.InternalIP).Info("Auto-detected internal IP")
	}

	// Load NAT configuration
	config.BehindNAT = os.Getenv("BEHIND_NAT") == "true"

	// Set STUN servers
	stunServer := os.Getenv("STUN_SERVER")
	if stunServer == "" {
		// Use Google's STUN servers by default
		config.STUNServers = []string{
			"stun.l.google.com:19302",
			"stun1.l.google.com:19302",
			"stun2.l.google.com:19302",
			"stun3.l.google.com:19302",
			"stun4.l.google.com:19302",
		}
		logger.Info("Using default Google STUN servers")
	} else {
		// Parse comma-separated list of STUN servers
		config.STUNServers = strings.Split(stunServer, ",")
	}

	// Resource limits
	maxCalls := os.Getenv("MAX_CONCURRENT_CALLS")
	if maxCalls != "" {
		config.MaxConcurrentCalls, _ = strconv.Atoi(maxCalls)
	} else {
		config.MaxConcurrentCalls = 500 // Default value
	}

	// Load recording config
	recordingMaxDuration := os.Getenv("RECORDING_MAX_DURATION_HOURS")
	if recordingMaxDuration != "" {
		hours, _ := strconv.Atoi(recordingMaxDuration)
		config.RecordingMaxDuration = time.Duration(hours) * time.Hour
	} else {
		config.RecordingMaxDuration = 4 * time.Hour // Default 4 hours
	}

	recordingCleanupDays := os.Getenv("RECORDING_CLEANUP_DAYS")
	if recordingCleanupDays != "" {
		config.RecordingCleanupDays, _ = strconv.Atoi(recordingCleanupDays)
	} else {
		config.RecordingCleanupDays = 30 // Default 30 days
	}

	// Validate SPEECH_VENDOR
	if config.DefaultVendor == "" {
		logger.Warn("SPEECH_VENDOR not set; using 'google' as default")
		config.DefaultVendor = "google" // Set to 'google' as a default fallback
	}

	// Load SIP ports
	ports := strings.Split(os.Getenv("PORTS"), ",")
	for _, portStr := range ports {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid port in PORTS: %w", err)
		}
		config.Ports = append(config.Ports, port)
	}

	// Load RTP port range with error handling
	var err error
	config.RTPPortMin, err = strconv.Atoi(os.Getenv("RTP_PORT_MIN"))
	if err != nil || config.RTPPortMin <= 0 {
		logger.Warn("Invalid or missing RTP_PORT_MIN; setting default to 10000")
		config.RTPPortMin = 10000
	}

	config.RTPPortMax, err = strconv.Atoi(os.Getenv("RTP_PORT_MAX"))
	if err != nil || config.RTPPortMax <= config.RTPPortMin {
		logger.Warn("Invalid or missing RTP_PORT_MAX; setting default to 20000")
		config.RTPPortMax = 20000
	}

	// Load recording directory
	config.RecordingDir = os.Getenv("RECORDING_DIR")
	if config.RecordingDir == "" {
		logger.Warn("RECORDING_DIR not set in .env file; using ./recordings")
		config.RecordingDir = "./recordings"
	}

	// Load TLS configuration
	config.TLSCertFile = os.Getenv("TLS_CERT_PATH")
	config.TLSKeyFile = os.Getenv("TLS_KEY_PATH")
	if config.TLSCertFile == "" || config.TLSKeyFile == "" {
		logger.Warn("TLS_CERT_PATH or TLS_KEY_PATH not set, TLS support will be disabled")
	}

	// Load TLS port (if specified)
	tlsPortStr := os.Getenv("TLS_PORT")
	if tlsPortStr != "" {
		config.TLSPort, err = strconv.Atoi(tlsPortStr)
		if err != nil {
			logger.Warn("Invalid TLS_PORT specified; TLS support will be disabled")
			config.TLSPort = 0
		}
	} else {
		config.TLSPort = 0 // Default to 0 if not specified
	}

	// Enable or disable TLS based on the environment variable
	config.EnableTLS = os.Getenv("ENABLE_TLS") == "true"

	// Set the log level from the environment variable
	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr == "" {
		logLevelStr = "info" // Default to "info" level
	}
	level, err := logrus.ParseLevel(logLevelStr)
	if err != nil {
		logger.Warnf("Invalid LOG_LEVEL '%s', defaulting to 'info'", logLevelStr)
		config.LogLevel = logrus.InfoLevel
	} else {
		config.LogLevel = level
	}

	// Load AMQP configuration
	config.AMQPUrl = os.Getenv("AMQP_URL")
	config.AMQPQueueName = os.Getenv("AMQP_QUEUE_NAME")

	// Load HTTP configuration
	httpPortStr := os.Getenv("HTTP_PORT")
	if httpPortStr != "" {
		config.HTTPPort, err = strconv.Atoi(httpPortStr)
		if err != nil {
			logger.Warn("Invalid HTTP_PORT specified; using default port 8080")
			config.HTTPPort = 8080
		}
	} else {
		config.HTTPPort = 8080 // Default HTTP port
	}

	// HTTP enabled/disabled
	config.HTTPEnabled = true
	if os.Getenv("HTTP_ENABLED") == "false" {
		config.HTTPEnabled = false
	}

	// HTTP metrics and API endpoints
	config.HTTPEnableMetrics = os.Getenv("HTTP_ENABLE_METRICS") != "false" // Enabled by default
	config.HTTPEnableAPI = os.Getenv("HTTP_ENABLE_API") != "false"         // Enabled by default

	// Ensure recording directory exists
	if _, err := os.Stat(config.RecordingDir); os.IsNotExist(err) {
		logger.Infof("Creating recording directory: %s", config.RecordingDir)
		if err := os.MkdirAll(config.RecordingDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create recording directory: %w", err)
		}
	}

	return config, nil
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
