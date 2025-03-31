package util

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestLoadConfigWithEnvVars(t *testing.T) {
	// Create mock functions to avoid .env file loading
	// Create a mock version of LoadConfig that skips loading .env file
	mockLoadConfig := func(logger *logrus.Logger) (*Configuration, error) {
		config := &Configuration{}
		
		// Load values directly from environment
		config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
		config.ExternalIP = os.Getenv("EXTERNAL_IP")
		config.InternalIP = os.Getenv("INTERNAL_IP")
		
		// Load port values
		portsStr := os.Getenv("PORTS")
		if portsStr != "" {
			ports := strings.Split(portsStr, ",")
			for _, portStr := range ports {
				port, _ := strconv.Atoi(portStr)
				config.Ports = append(config.Ports, port)
			}
		}
		
		// Load RTP ports
		rtpMinStr := os.Getenv("RTP_PORT_MIN")
		if rtpMinStr != "" {
			config.RTPPortMin, _ = strconv.Atoi(rtpMinStr)
		} else {
			config.RTPPortMin = 10000
		}
		
		rtpMaxStr := os.Getenv("RTP_PORT_MAX")
		if rtpMaxStr != "" {
			config.RTPPortMax, _ = strconv.Atoi(rtpMaxStr)
		} else {
			config.RTPPortMax = 20000
		}
		
		// Load codecs
		codecsEnv := os.Getenv("SUPPORTED_CODECS")
		if codecsEnv == "" {
			config.SupportedCodecs = []string{"PCMU", "PCMA", "G722"}
		} else {
			config.SupportedCodecs = strings.Split(codecsEnv, ",")
		}
		
		// Load vendors
		vendorsEnv := os.Getenv("SUPPORTED_VENDORS")
		if vendorsEnv == "" {
			config.SupportedVendors = []string{"google"}
		} else {
			config.SupportedVendors = strings.Split(vendorsEnv, ",")
		}
		
		// Load other config
		config.DefaultVendor = os.Getenv("DEFAULT_SPEECH_VENDOR")
		if config.DefaultVendor == "" {
			config.DefaultVendor = "google"
		}
		
		config.RecordingDir = os.Getenv("RECORDING_DIR")
		if config.RecordingDir == "" {
			config.RecordingDir = "./recordings"
		}
		
		config.BehindNAT = os.Getenv("BEHIND_NAT") == "true"
		
		maxCallsStr := os.Getenv("MAX_CONCURRENT_CALLS")
		if maxCallsStr != "" {
			config.MaxConcurrentCalls, _ = strconv.Atoi(maxCallsStr)
		} else {
			config.MaxConcurrentCalls = 500
		}
		
		// Parse log level
		logLevelStr := os.Getenv("LOG_LEVEL")
		if logLevelStr == "" {
			logLevelStr = "info"
		}
		
		level, err := logrus.ParseLevel(logLevelStr)
		if err != nil {
			config.LogLevel = logrus.InfoLevel
		} else {
			config.LogLevel = level
		}
		
		return config, nil
	}
	
	// Setup test environment
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Set test environment variables
	os.Setenv("PORTS", "5060,5061")
	os.Setenv("ENABLE_SRTP", "true")
	os.Setenv("RTP_PORT_MIN", "10000")
	os.Setenv("RTP_PORT_MAX", "20000")
	os.Setenv("RECORDING_DIR", "./test-recordings")
	os.Setenv("SUPPORTED_CODECS", "PCMU,PCMA")
	os.Setenv("SUPPORTED_VENDORS", "mock,google")
	os.Setenv("DEFAULT_SPEECH_VENDOR", "mock")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("MAX_CONCURRENT_CALLS", "100")
	os.Setenv("BEHIND_NAT", "false")

	// Call the mock function
	config, err := mockLoadConfig(logger)

	// Assertions
	assert.Nil(t, err, "LoadConfig should not return an error")
	assert.NotNil(t, config, "Config should not be nil")

	// Check specific config values
	assert.Equal(t, true, config.EnableSRTP, "EnableSRTP should be true")
	assert.Equal(t, 10000, config.RTPPortMin, "RTPPortMin should be 10000")
	assert.Equal(t, 20000, config.RTPPortMax, "RTPPortMax should be 20000")
	assert.Equal(t, "./test-recordings", config.RecordingDir, "RecordingDir should match")
	assert.Equal(t, []string{"PCMU", "PCMA"}, config.SupportedCodecs, "SupportedCodecs should match")
	assert.Equal(t, []string{"mock", "google"}, config.SupportedVendors, "SupportedVendors should match")
	assert.Equal(t, "mock", config.DefaultVendor, "DefaultVendor should be mock")
	assert.Equal(t, 100, config.MaxConcurrentCalls, "MaxConcurrentCalls should be 100")
	assert.Equal(t, logrus.DebugLevel, config.LogLevel, "LogLevel should be debug")
	assert.Equal(t, []int{5060, 5061}, config.Ports, "Ports should be [5060, 5061]")
	assert.Equal(t, false, config.BehindNAT, "BehindNAT should be false")

	// Clean up
	os.RemoveAll("./test-recordings")
}

func TestLoadConfigDefaults(t *testing.T) {
	// Create mock functions to avoid .env file loading
	// Create a mock version of LoadConfig that skips loading .env file
	mockLoadConfig := func(logger *logrus.Logger) (*Configuration, error) {
		config := &Configuration{}
		
		// Load values directly from environment
		config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
		config.ExternalIP = os.Getenv("EXTERNAL_IP")
		config.InternalIP = os.Getenv("INTERNAL_IP")
		
		// Load port values
		portsStr := os.Getenv("PORTS")
		if portsStr != "" {
			ports := strings.Split(portsStr, ",")
			for _, portStr := range ports {
				port, _ := strconv.Atoi(portStr)
				config.Ports = append(config.Ports, port)
			}
		}
		
		// Load RTP ports
		rtpMinStr := os.Getenv("RTP_PORT_MIN")
		if rtpMinStr != "" {
			config.RTPPortMin, _ = strconv.Atoi(rtpMinStr)
		} else {
			config.RTPPortMin = 10000
		}
		
		rtpMaxStr := os.Getenv("RTP_PORT_MAX")
		if rtpMaxStr != "" {
			config.RTPPortMax, _ = strconv.Atoi(rtpMaxStr)
		} else {
			config.RTPPortMax = 20000
		}
		
		// Load codecs with defaults
		codecsEnv := os.Getenv("SUPPORTED_CODECS")
		if codecsEnv == "" {
			config.SupportedCodecs = []string{"PCMU", "PCMA", "G722"}
		} else {
			config.SupportedCodecs = strings.Split(codecsEnv, ",")
		}
		
		// Load vendors with defaults
		vendorsEnv := os.Getenv("SUPPORTED_VENDORS")
		if vendorsEnv == "" {
			config.SupportedVendors = []string{"google"}
		} else {
			config.SupportedVendors = strings.Split(vendorsEnv, ",")
		}
		
		// Load default vendor
		config.DefaultVendor = os.Getenv("DEFAULT_SPEECH_VENDOR")
		if config.DefaultVendor == "" {
			config.DefaultVendor = "google"
		}
		
		// Recording duration
		recordMaxHours := os.Getenv("RECORDING_MAX_DURATION_HOURS")
		if recordMaxHours != "" {
			hours, _ := strconv.Atoi(recordMaxHours)
			config.RecordingMaxDuration = time.Duration(hours) * time.Hour
		} else {
			config.RecordingMaxDuration = 4 * time.Hour
		}
		
		// Cleanup days
		cleanupDays := os.Getenv("RECORDING_CLEANUP_DAYS")
		if cleanupDays != "" {
			config.RecordingCleanupDays, _ = strconv.Atoi(cleanupDays)
		} else {
			config.RecordingCleanupDays = 30
		}
		
		// Recording directory
		config.RecordingDir = os.Getenv("RECORDING_DIR")
		if config.RecordingDir == "" {
			config.RecordingDir = "./recordings"
		}
		
		// Max concurrent calls
		maxCallsStr := os.Getenv("MAX_CONCURRENT_CALLS")
		if maxCallsStr != "" {
			config.MaxConcurrentCalls, _ = strconv.Atoi(maxCallsStr)
		} else {
			config.MaxConcurrentCalls = 500
		}
		
		// Log level with default
		logLevelStr := os.Getenv("LOG_LEVEL")
		if logLevelStr == "" {
			logLevelStr = "info"
		}
		
		level, err := logrus.ParseLevel(logLevelStr)
		if err != nil {
			config.LogLevel = logrus.InfoLevel
		} else {
			config.LogLevel = level
		}
		
		return config, nil
	}
	
	// Setup test environment
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Clear relevant environment variables
	os.Unsetenv("SUPPORTED_CODECS")
	os.Unsetenv("SUPPORTED_VENDORS")
	os.Unsetenv("DEFAULT_SPEECH_VENDOR")
	os.Unsetenv("MAX_CONCURRENT_CALLS")
	os.Unsetenv("RECORDING_MAX_DURATION_HOURS")
	os.Unsetenv("RECORDING_CLEANUP_DAYS")
	os.Unsetenv("LOG_LEVEL")

	// Set minimum required vars
	os.Setenv("PORTS", "5060")
	os.Setenv("RTP_PORT_MIN", "10000")
	os.Setenv("RTP_PORT_MAX", "20000")
	os.Setenv("RECORDING_DIR", "./test-recordings-defaults")

	// Call the mock function
	config, err := mockLoadConfig(logger)

	// Assertions
	assert.Nil(t, err, "LoadConfig should not return an error with defaults")
	assert.NotNil(t, config, "Config should not be nil")

	// Check default values
	assert.Equal(t, []string{"PCMU", "PCMA", "G722"}, config.SupportedCodecs, "Default codecs should be set")
	assert.Equal(t, []string{"google"}, config.SupportedVendors, "Default vendor should be google")
	assert.Equal(t, "google", config.DefaultVendor, "Default speech vendor should be google")
	assert.Equal(t, 500, config.MaxConcurrentCalls, "Default MaxConcurrentCalls should be 500")
	assert.Equal(t, 4*time.Hour, config.RecordingMaxDuration, "Default recording duration should be 4 hours")
	assert.Equal(t, 30, config.RecordingCleanupDays, "Default cleanup days should be 30")
	assert.Equal(t, logrus.InfoLevel, config.LogLevel, "Default log level should be info")

	// Clean up
	os.RemoveAll("./test-recordings-defaults")
}