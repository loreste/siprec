package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"io"

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
	TLSCertFile      string
	TLSKeyFile       string
	TLSPort          int
	EnableTLS        bool
	LogLevel         logrus.Level

	// NAT Configuration
	BehindNAT            bool
	STUNServers          []string
	MaxConcurrentCalls   int
	RecordingMaxDuration time.Duration
}

var (
	config Config
)

// Default STUN configuration
var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun2.l.google.com:19302",
	"stun3.l.google.com:19302",
	"stun4.l.google.com:19302",
}

func loadConfig() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		logger.Fatalf("Error loading .env file: %v", err)
	}

	// Load general configuration
	config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
	config.ExternalIP = os.Getenv("EXTERNAL_IP")
	config.InternalIP = os.Getenv("INTERNAL_IP")

	// Load codecs
	codecsEnv := os.Getenv("SUPPORTED_CODECS")
	if codecsEnv == "" {
		config.SupportedCodecs = []string{"PCMU", "PCMA", "G722"}
		logger.Info("No SUPPORTED_CODECS specified, defaulting to PCMU, PCMA, G722")
	} else {
		config.SupportedCodecs = strings.Split(codecsEnv, ",")
	}

	// Load vendors
	config.SupportedVendors = strings.Split(os.Getenv("SUPPORTED_VENDORS"), ",")
	config.DefaultVendor = os.Getenv("DEFAULT_SPEECH_VENDOR")

	// Auto-detect IP if not set
	if config.ExternalIP == "auto" || config.ExternalIP == "" {
		// Use a best-effort method to determine external IP
		config.ExternalIP = getExternalIP()
		logger.WithField("external_ip", config.ExternalIP).Info("Auto-detected external IP")
	}

	if config.InternalIP == "auto" || config.InternalIP == "" {
		config.InternalIP = getInternalIP()
		logger.WithField("internal_ip", config.InternalIP).Info("Auto-detected internal IP")
	}

	// Load NAT configuration
	config.BehindNAT = os.Getenv("BEHIND_NAT") == "true"

	// Set STUN servers
	stunServer := os.Getenv("STUN_SERVER")
	if stunServer == "" {
		// Use Google's STUN servers by default
		config.STUNServers = defaultSTUNServers
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

	// Load recording max duration
	recordingMaxDuration := os.Getenv("RECORDING_MAX_DURATION_HOURS")
	if recordingMaxDuration != "" {
		hours, _ := strconv.Atoi(recordingMaxDuration)
		config.RecordingMaxDuration = time.Duration(hours) * time.Hour
	} else {
		config.RecordingMaxDuration = 4 * time.Hour // Default 4 hours
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
	logger.SetLevel(config.LogLevel)
	logger.Infof("Log level set to %s", logger.GetLevel().String())

	// Ensure recording directory exists
	if _, err := os.Stat(config.RecordingDir); os.IsNotExist(err) {
		logger.Infof("Creating recording directory: %s", config.RecordingDir)
		if err := os.MkdirAll(config.RecordingDir, 0755); err != nil {
			logger.Fatalf("Failed to create recording directory: %v", err)
		}
	}
}

// Helper function to get external IP
func getExternalIP() string {
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
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				continue
			}
			return strings.TrimSpace(string(body))
		}
	}

	logger.Warn("Could not auto-detect external IP, using localhost as fallback")
	return "127.0.0.1" // Fallback
}

// Helper function to get internal IP
func getInternalIP() string {
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

// CheckRecordingDiskSpace checks available disk space for recordings
func CheckRecordingDiskSpace() (percentUsed int, err error) {
	var stat syscall.Statfs_t
	err = syscall.Statfs(config.RecordingDir, &stat)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk statistics: %w", err)
	}

	// Calculate disk usage percentage
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	// Avoid division by zero
	if total == 0 {
		return 0, fmt.Errorf("invalid total disk space reported as zero")
	}

	percentUsed = int(float64(used) / float64(total) * 100)

	logger.WithFields(logrus.Fields{
		"total_space_gb": float64(total) / (1024 * 1024 * 1024),
		"used_space_gb":  float64(used) / (1024 * 1024 * 1024),
		"percent_used":   percentUsed,
		"path":           config.RecordingDir,
	}).Debug("Disk space check completed")

	return percentUsed, nil
}

// Helper to get next STUN server with simple round-robin
func getNextSTUNServer() string {
	if len(config.STUNServers) == 0 {
		return "stun.l.google.com:19302" // Default to Google's primary STUN server
	}

	// Simple round-robin selection
	stunServer := config.STUNServers[0]

	// Rotate the servers (move first to last)
	config.STUNServers = append(config.STUNServers[1:], config.STUNServers[0])

	return stunServer
}
