package config

import (
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestConfigLoading(t *testing.T) {
	// Set up test environment variables
	os.Setenv("EXTERNAL_IP", "192.168.1.1")
	os.Setenv("INTERNAL_IP", "10.0.0.1")
	os.Setenv("PORTS", "5060,5062,6060")
	os.Setenv("ENABLE_SRTP", "true")
	os.Setenv("RTP_PORT_MIN", "10001")
	os.Setenv("RTP_PORT_MAX", "20001")
	os.Setenv("ENABLE_TLS", "true")
	os.Setenv("TLS_PORT", "5063")
	os.Setenv("TLS_CERT_PATH", "./certs/cert.pem")
	os.Setenv("TLS_KEY_PATH", "./certs/key.pem")
	os.Setenv("BEHIND_NAT", "true")
	os.Setenv("STUN_SERVER", "stun.custom.com:3478")

	os.Setenv("HTTP_PORT", "8081")
	os.Setenv("HTTP_ENABLED", "true")
	os.Setenv("HTTP_ENABLE_METRICS", "true")
	os.Setenv("HTTP_ENABLE_API", "true")
	os.Setenv("HTTP_READ_TIMEOUT", "15s")
	os.Setenv("HTTP_WRITE_TIMEOUT", "45s")

	os.Setenv("RECORDING_DIR", "./test-recordings")
	os.Setenv("RECORDING_MAX_DURATION_HOURS", "6")
	os.Setenv("RECORDING_CLEANUP_DAYS", "45")

	os.Setenv("SUPPORTED_VENDORS", "google,deepgram,openai")
	os.Setenv("SUPPORTED_CODECS", "PCMU,PCMA,G722,OPUS")
	os.Setenv("DEFAULT_SPEECH_VENDOR", "deepgram")

	os.Setenv("MAX_CONCURRENT_CALLS", "1000")

	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("LOG_FORMAT", "text")

	os.Setenv("AMQP_URL", "amqp://guest:guest@localhost:5672/")
	os.Setenv("AMQP_QUEUE_NAME", "siprec-transcriptions")

	os.Setenv("ENABLE_REDUNDANCY", "true")
	os.Setenv("SESSION_TIMEOUT", "45s")
	os.Setenv("SESSION_CHECK_INTERVAL", "15s")
	os.Setenv("REDUNDANCY_STORAGE_TYPE", "memory")

	// Create logger for testing
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Clean up when test finishes
	defer func() {
		// Unset environment variables
		vars := []string{
			"EXTERNAL_IP", "INTERNAL_IP", "PORTS", "ENABLE_SRTP", "RTP_PORT_MIN",
			"RTP_PORT_MAX", "ENABLE_TLS", "TLS_PORT", "TLS_CERT_PATH", "TLS_KEY_PATH",
			"BEHIND_NAT", "STUN_SERVER", "HTTP_PORT", "HTTP_ENABLED", "HTTP_ENABLE_METRICS",
			"HTTP_ENABLE_API", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "RECORDING_DIR",
			"RECORDING_MAX_DURATION_HOURS", "RECORDING_CLEANUP_DAYS", "SUPPORTED_VENDORS",
			"SUPPORTED_CODECS", "DEFAULT_SPEECH_VENDOR", "MAX_CONCURRENT_CALLS", "LOG_LEVEL",
			"LOG_FORMAT", "AMQP_URL", "AMQP_QUEUE_NAME", "ENABLE_REDUNDANCY", "SESSION_TIMEOUT",
			"SESSION_CHECK_INTERVAL", "REDUNDANCY_STORAGE_TYPE",
		}

		for _, v := range vars {
			os.Unsetenv(v)
		}

		// Clean up created directories
		os.RemoveAll("./test-recordings")
	}()

	// Load configuration
	config, err := Load(logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Verify network configuration
	assert.Equal(t, "192.168.1.1", config.Network.ExternalIP)
	assert.Equal(t, "10.0.0.1", config.Network.InternalIP)
	assert.Equal(t, []int{5060, 5062, 6060}, config.Network.Ports)
	assert.True(t, config.Network.EnableSRTP)
	assert.Equal(t, 10001, config.Network.RTPPortMin)
	assert.Equal(t, 20001, config.Network.RTPPortMax)
	assert.True(t, config.Network.EnableTLS)
	assert.Equal(t, 5063, config.Network.TLSPort)
	assert.Equal(t, "./certs/cert.pem", config.Network.TLSCertFile)
	assert.Equal(t, "./certs/key.pem", config.Network.TLSKeyFile)
	assert.True(t, config.Network.BehindNAT)
	assert.Equal(t, []string{"stun.custom.com:3478"}, config.Network.STUNServers)

	// Verify HTTP configuration
	assert.Equal(t, 8081, config.HTTP.Port)
	assert.True(t, config.HTTP.Enabled)
	assert.True(t, config.HTTP.EnableMetrics)
	assert.True(t, config.HTTP.EnableAPI)
	assert.Equal(t, 15*time.Second, config.HTTP.ReadTimeout)
	assert.Equal(t, 45*time.Second, config.HTTP.WriteTimeout)

	// Verify recording configuration
	assert.Equal(t, "./test-recordings", config.Recording.Directory)
	assert.Equal(t, 6*time.Hour, config.Recording.MaxDuration)
	assert.Equal(t, 45, config.Recording.CleanupDays)

	// Verify STT configuration
	assert.Equal(t, []string{"google", "deepgram", "openai"}, config.STT.SupportedVendors)
	assert.Equal(t, []string{"PCMU", "PCMA", "G722", "OPUS"}, config.STT.SupportedCodecs)
	assert.Equal(t, "deepgram", config.STT.DefaultVendor)

	// Verify resource configuration
	assert.Equal(t, 1000, config.Resources.MaxConcurrentCalls)

	// Verify logging configuration
	assert.Equal(t, "debug", config.Logging.Level)
	assert.Equal(t, "text", config.Logging.Format)

	// Verify messaging configuration
	assert.Equal(t, "amqp://guest:guest@localhost:5672/", config.Messaging.AMQPUrl)
	assert.Equal(t, "siprec-transcriptions", config.Messaging.AMQPQueueName)

	// Verify redundancy configuration
	assert.True(t, config.Redundancy.Enabled)
	assert.Equal(t, 45*time.Second, config.Redundancy.SessionTimeout)
	assert.Equal(t, 15*time.Second, config.Redundancy.SessionCheckInterval)
	assert.Equal(t, "memory", config.Redundancy.StorageType)

	// Verify the created directory
	_, err = os.Stat("./test-recordings")
	assert.NoError(t, err)
}

func TestDefaultConfiguration(t *testing.T) {
	// Ensure no environment variables are set
	vars := []string{
		"EXTERNAL_IP", "INTERNAL_IP", "PORTS", "ENABLE_SRTP", "RTP_PORT_MIN",
		"RTP_PORT_MAX", "ENABLE_TLS", "TLS_PORT", "TLS_CERT_PATH", "TLS_KEY_PATH",
		"BEHIND_NAT", "STUN_SERVER", "HTTP_PORT", "HTTP_ENABLED", "HTTP_ENABLE_METRICS",
		"HTTP_ENABLE_API", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "RECORDING_DIR",
		"RECORDING_MAX_DURATION_HOURS", "RECORDING_CLEANUP_DAYS", "SUPPORTED_VENDORS",
		"SUPPORTED_CODECS", "DEFAULT_SPEECH_VENDOR", "MAX_CONCURRENT_CALLS", "LOG_LEVEL",
		"LOG_FORMAT", "AMQP_URL", "AMQP_QUEUE_NAME", "ENABLE_REDUNDANCY", "SESSION_TIMEOUT",
		"SESSION_CHECK_INTERVAL", "REDUNDANCY_STORAGE_TYPE",
	}

	for _, v := range vars {
		os.Unsetenv(v)
	}

	// Create logger for testing
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Load configuration
	config, err := Load(logger)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	// Verify that defaults are set correctly
	// IP should be either "auto", "127.0.0.1", or a valid detected IP
	assert.True(t, config.Network.ExternalIP == "auto" || config.Network.ExternalIP == "127.0.0.1" || len(config.Network.ExternalIP) > 0, "External IP should be set")
	assert.True(t, config.Network.InternalIP == "auto" || config.Network.InternalIP == "127.0.0.1" || len(config.Network.InternalIP) > 0, "Internal IP should be set")
	assert.ElementsMatch(t, []int{5060, 5061}, config.Network.Ports)
	assert.False(t, config.Network.EnableSRTP)
	assert.Equal(t, 10000, config.Network.RTPPortMin)
	assert.Equal(t, 20000, config.Network.RTPPortMax)
	assert.False(t, config.Network.EnableTLS)
	assert.Equal(t, 5062, config.Network.TLSPort)
	assert.Equal(t, "", config.Network.TLSCertFile)
	assert.Equal(t, "", config.Network.TLSKeyFile)
	assert.False(t, config.Network.BehindNAT)

	// Verify HTTP defaults
	assert.Equal(t, 8080, config.HTTP.Port)
	assert.True(t, config.HTTP.Enabled)
	assert.True(t, config.HTTP.EnableMetrics)
	assert.True(t, config.HTTP.EnableAPI)
	assert.Equal(t, 10*time.Second, config.HTTP.ReadTimeout)
	assert.Equal(t, 30*time.Second, config.HTTP.WriteTimeout)

	// Verify recording defaults
	assert.Equal(t, "./recordings", config.Recording.Directory)
	assert.Equal(t, 4*time.Hour, config.Recording.MaxDuration)
	assert.Equal(t, 30, config.Recording.CleanupDays)

	// Verify STT defaults
	assert.Equal(t, []string{"google", "openai"}, config.STT.SupportedVendors)
	assert.ElementsMatch(t, []string{"PCMU", "PCMA", "G722", "OPUS"}, config.STT.SupportedCodecs)
	assert.Equal(t, "google", config.STT.DefaultVendor)

	// Verify resource defaults
	assert.Equal(t, 500, config.Resources.MaxConcurrentCalls)

	// Verify logging defaults
	assert.Equal(t, "info", config.Logging.Level)
	assert.Equal(t, "json", config.Logging.Format)

	// Verify redundancy defaults
	assert.True(t, config.Redundancy.Enabled)
	assert.Equal(t, 30*time.Second, config.Redundancy.SessionTimeout)
	assert.Equal(t, 10*time.Second, config.Redundancy.SessionCheckInterval)
	assert.Equal(t, "memory", config.Redundancy.StorageType)
}

func TestLegacyCompatibility(t *testing.T) {
	// Set some environment variables for testing
	os.Setenv("EXTERNAL_IP", "192.168.1.1")
	os.Setenv("INTERNAL_IP", "10.0.0.1")
	os.Setenv("PORTS", "5060,5062")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("ENABLE_REDUNDANCY", "true")

	// Clean up when test finishes
	defer func() {
		os.Unsetenv("EXTERNAL_IP")
		os.Unsetenv("INTERNAL_IP")
		os.Unsetenv("PORTS")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("ENABLE_REDUNDANCY")
	}()

	// Create logger for testing
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Load the legacy configuration
	legacyConfig, err := LoadConfig(logger)
	assert.NoError(t, err)
	assert.NotNil(t, legacyConfig)

	// Verify the legacy configuration
	assert.Equal(t, "192.168.1.1", legacyConfig.ExternalIP)
	assert.Equal(t, "10.0.0.1", legacyConfig.InternalIP)
	assert.Equal(t, []int{5060, 5062}, legacyConfig.Ports)
	assert.Equal(t, logrus.DebugLevel, legacyConfig.LogLevel)
	assert.True(t, legacyConfig.RedundancyEnabled)

	// Now load the new configuration for comparison
	newConfig, err := Load(logger)
	assert.NoError(t, err)
	assert.NotNil(t, newConfig)

	// Convert the new configuration to legacy
	convertedConfig := newConfig.ToLegacyConfig(logger)

	// Verify that the converted configuration matches the legacy configuration
	assert.Equal(t, legacyConfig.ExternalIP, convertedConfig.ExternalIP)
	assert.Equal(t, legacyConfig.InternalIP, convertedConfig.InternalIP)
	assert.Equal(t, legacyConfig.Ports, convertedConfig.Ports)
	assert.Equal(t, legacyConfig.LogLevel, convertedConfig.LogLevel)
	assert.Equal(t, legacyConfig.RedundancyEnabled, convertedConfig.RedundancyEnabled)
}
