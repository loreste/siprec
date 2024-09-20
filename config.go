package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
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
	TLSCertFile      string // Path to the TLS certificate file
	TLSKeyFile       string // Path to the TLS key file
	TLSPort          int    // Port for SIP over TLS
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
	config.DefaultVendor = os.Getenv("DEFAULT_VENDOR") // Load default speech vendor

	// Load SIP ports
	ports := strings.Split(os.Getenv("PORTS"), ",")
	for _, portStr := range ports {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			logger.Fatalf("Invalid port in PORTS: %v", err)
		}
		config.Ports = append(config.Ports, port)
	}

	// Load RTP port range
	config.RTPPortMin, _ = strconv.Atoi(os.Getenv("RTP_PORT_MIN"))
	config.RTPPortMax, _ = strconv.Atoi(os.Getenv("RTP_PORT_MAX"))

	// Load recording directory
	config.RecordingDir = os.Getenv("RECORDING_DIR")
	if config.RecordingDir == "" {
		logger.Fatal("RECORDING_DIR not set in .env file")
	}

	// Load TLS configuration
	config.TLSCertFile = os.Getenv("TLS_CERT_FILE") // Path to the TLS certificate file
	config.TLSKeyFile = os.Getenv("TLS_KEY_FILE")   // Path to the TLS key file
	if config.TLSCertFile == "" || config.TLSKeyFile == "" {
		logger.Warn("TLS_CERT_FILE or TLS_KEY_FILE not set, TLS support will be disabled")
	}

	// Load TLS port (if specified)
	tlsPortStr := os.Getenv("TLS_PORT")
	if tlsPortStr != "" {
		config.TLSPort, _ = strconv.Atoi(tlsPortStr)
	} else {
		config.TLSPort = 0 // Default to 0 if not specified, meaning TLS will not be enabled
	}
}
