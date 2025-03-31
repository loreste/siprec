package config

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Configuration is the legacy configuration struct for backward compatibility
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
	HTTPPort             int
	HTTPEnabled          bool
	HTTPEnableMetrics    bool
	HTTPEnableAPI        bool
	
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
	
	// Redundancy configuration
	RedundancyEnabled    bool
	SessionTimeout       time.Duration
	StateCheckInterval   time.Duration
	RedundancyStorageType string
}

// ToLegacyConfig converts the new Config struct to the legacy Configuration struct
func (c *Config) ToLegacyConfig(logger *logrus.Logger) *Configuration {
	// Parse the log level
	logLevel, err := logrus.ParseLevel(c.Logging.Level)
	if err != nil {
		logLevel = logrus.InfoLevel
		logger.Warnf("Invalid log level %s, defaulting to info", c.Logging.Level)
	}
	
	return &Configuration{
		// Network configuration
		ExternalIP:  c.Network.ExternalIP,
		InternalIP:  c.Network.InternalIP,
		Ports:       c.Network.Ports,
		EnableSRTP:  c.Network.EnableSRTP,
		RTPPortMin:  c.Network.RTPPortMin,
		RTPPortMax:  c.Network.RTPPortMax,
		TLSCertFile: c.Network.TLSCertFile,
		TLSKeyFile:  c.Network.TLSKeyFile,
		TLSPort:     c.Network.TLSPort,
		EnableTLS:   c.Network.EnableTLS,
		BehindNAT:   c.Network.BehindNAT,
		STUNServers: c.Network.STUNServers,
		
		// HTTP server configuration
		HTTPPort:          c.HTTP.Port,
		HTTPEnabled:       c.HTTP.Enabled,
		HTTPEnableMetrics: c.HTTP.EnableMetrics,
		HTTPEnableAPI:     c.HTTP.EnableAPI,
		
		// Recording configuration
		RecordingDir:         c.Recording.Directory,
		RecordingMaxDuration: c.Recording.MaxDuration,
		RecordingCleanupDays: c.Recording.CleanupDays,
		
		// Speech-to-text configuration
		SupportedVendors: c.STT.SupportedVendors,
		SupportedCodecs:  c.STT.SupportedCodecs,
		DefaultVendor:    c.STT.DefaultVendor,
		
		// Resource limits
		MaxConcurrentCalls: c.Resources.MaxConcurrentCalls,
		
		// Logging
		LogLevel: logLevel,
		
		// AMQP configuration
		AMQPUrl:       c.Messaging.AMQPUrl,
		AMQPQueueName: c.Messaging.AMQPQueueName,
		
		// Redundancy configuration
		RedundancyEnabled:     c.Redundancy.Enabled,
		SessionTimeout:        c.Redundancy.SessionTimeout,
		StateCheckInterval:    c.Redundancy.SessionCheckInterval,
		RedundancyStorageType: c.Redundancy.StorageType,
	}
}

// LoadConfig is a legacy function that loads the configuration in the old format
// for backward compatibility
func LoadConfig(logger *logrus.Logger) (*Configuration, error) {
	// Load the new config
	config, err := Load(logger)
	if err != nil {
		return nil, err
	}
	
	// Convert to legacy config
	return config.ToLegacyConfig(logger), nil
}