package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

type Config struct {
	ExternalIP       string
	InternalIP       string
	Ports            []int
	EnableSRTP       bool
	RTPPortMin       int
	RTPPortMax       int
	RecordingDir     string
	SupportedVendors []string
	SupportedCodecs  []string
	DefaultVendor    string
	TLSCertFile      string       // Path to the TLS certificate file
	TLSKeyFile       string       // Path to the TLS key file
	TLSPort          int          // Port for SIP over TLS
	EnableTLS        bool         // Enable or disable TLS
	LogLevel         logrus.Level // Log level for structured logging
}

var (
	config Config
)

func loadConfig() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		logger.Fatalf("Error loading .env file: %v", err)
	}

	// Load general configuration
	config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
	config.ExternalIP = os.Getenv("EXTERNAL_IP")
	config.InternalIP = os.Getenv("INTERNAL_IP")
	config.SupportedCodecs = strings.Split(os.Getenv("SUPPORTED_CODECS"), ",")
	config.SupportedVendors = strings.Split(os.Getenv("SUPPORTED_VENDORS"), ",")
	config.DefaultVendor = os.Getenv("SPEECH_VENDOR")

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
			logger.Fatalf("Invalid port in PORTS: %v", err)
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
		logger.Fatal("RECORDING_DIR not set in .env file")
	}

	// Load TLS configuration
	config.TLSCertFile = os.Getenv("TLS_CERT_FILE")
	config.TLSKeyFile = os.Getenv("TLS_KEY_FILE")
	if config.TLSCertFile == "" || config.TLSKeyFile == "" {
		logger.Warn("TLS_CERT_FILE or TLS_KEY_FILE not set, TLS support will be disabled")
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
	logger.SetLevel(config.LogLevel)
	logger.Infof("Log level set to %s", logger.GetLevel().String())
}
