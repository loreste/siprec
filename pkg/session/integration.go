package session

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// IntegrationConfig holds configuration for session manager integration
type IntegrationConfig struct {
	RedisEnabled    bool          `yaml:"redis_enabled" env:"REDIS_ENABLED" default:"false"`
	RedisAddress    string        `yaml:"redis_address" env:"REDIS_ADDRESS" default:"localhost:6379"`
	RedisPassword   string        `yaml:"redis_password" env:"REDIS_PASSWORD"`
	RedisDatabase   int           `yaml:"redis_database" env:"REDIS_DATABASE" default:"0"`
	RedisPoolSize   int           `yaml:"redis_pool_size" env:"REDIS_POOL_SIZE" default:"10"`
	RedisTTL        time.Duration `yaml:"redis_ttl" env:"REDIS_SESSION_TTL" default:"24h"`
	NodeID          string        `yaml:"node_id" env:"NODE_ID" default:"siprec-1"`
	EnableFailover  bool          `yaml:"enable_failover" env:"ENABLE_FAILOVER" default:"true"`
	EnableBackup    bool          `yaml:"enable_backup" env:"ENABLE_BACKUP" default:"false"`
	SessionTimeout  time.Duration `yaml:"session_timeout" env:"SESSION_TIMEOUT" default:"1h"`
}

// LoadIntegrationConfig loads configuration from environment variables
func LoadIntegrationConfig() *IntegrationConfig {
	config := &IntegrationConfig{
		RedisEnabled:   getEnvBool("REDIS_ENABLED", false),
		RedisAddress:   getEnvString("REDIS_ADDRESS", "localhost:6379"),
		RedisPassword:  getEnvString("REDIS_PASSWORD", ""),
		RedisDatabase:  getEnvInt("REDIS_DATABASE", 0),
		RedisPoolSize:  getEnvInt("REDIS_POOL_SIZE", 10),
		RedisTTL:       getEnvDuration("REDIS_SESSION_TTL", 24*time.Hour),
		NodeID:         getEnvString("NODE_ID", "siprec-1"),
		EnableFailover: getEnvBool("ENABLE_FAILOVER", true),
		EnableBackup:   getEnvBool("ENABLE_BACKUP", false),
		SessionTimeout: getEnvDuration("SESSION_TIMEOUT", time.Hour),
	}

	return config
}

// InitializeSessionManager creates and configures the session manager
func InitializeSessionManager(logger *logrus.Logger) (*SessionManager, error) {
	config := LoadIntegrationConfig()

	if !config.RedisEnabled {
		logger.Info("Redis session store disabled, using memory-only session management")
		return nil, nil
	}

	// Configure Redis
	redisConfig := RedisConfig{
		Address:      config.RedisAddress,
		Password:     config.RedisPassword,
		Database:     config.RedisDatabase,
		PoolSize:     config.RedisPoolSize,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		TTL:          config.RedisTTL,
	}

	// Create Redis store
	redisStore, err := NewRedisSessionStore(redisConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis session store: %w", err)
	}

	// Create session manager configuration
	managerConfig := &ManagerConfig{
		NodeID:            config.NodeID,
		HeartbeatInterval: 30 * time.Second,
		CleanupInterval:   5 * time.Minute,
		SessionTimeout:    config.SessionTimeout,
		EnableFailover:    config.EnableFailover,
		EnableBackup:      config.EnableBackup,
	}

	// Create session manager
	sessionManager := NewSessionManager(redisStore, managerConfig, logger)

	logger.WithFields(logrus.Fields{
		"redis_address": config.RedisAddress,
		"node_id":       config.NodeID,
		"failover":      config.EnableFailover,
		"backup":        config.EnableBackup,
	}).Info("Redis session manager initialized")

	return sessionManager, nil
}

// GetSIPRECSessionFromManager converts session manager data to SIPREC format
func GetSIPRECSessionFromManager(sessionData *SessionData) *SIPRECSession {
	if sessionData == nil {
		return nil
	}

	return &SIPRECSession{
		SessionID:     sessionData.SessionID,
		CallID:        sessionData.CallID,
		Status:        sessionData.Status,
		StartTime:     sessionData.StartTime,
		LastUpdate:    sessionData.LastUpdate,
		RecordingPath: sessionData.RecordingPath,
		Metadata:      sessionData.Metadata,
		NodeID:        sessionData.NodeID,
	}
}

// CreateSessionDataFromSIPREC converts SIPREC session to session manager format
func CreateSessionDataFromSIPREC(siprecSession *SIPRECSession) *SessionData {
	if siprecSession == nil {
		return nil
	}

	return &SessionData{
		SessionID:     siprecSession.SessionID,
		CallID:        siprecSession.CallID,
		Status:        siprecSession.Status,
		StartTime:     siprecSession.StartTime,
		LastUpdate:    siprecSession.LastUpdate,
		RecordingPath: siprecSession.RecordingPath,
		Metadata:      siprecSession.Metadata,
		NodeID:        siprecSession.NodeID,
	}
}

// SIPRECSession represents a SIPREC session in legacy format
type SIPRECSession struct {
	SessionID     string                 `json:"session_id"`
	CallID        string                 `json:"call_id"`
	Status        string                 `json:"status"`
	StartTime     time.Time              `json:"start_time"`
	LastUpdate    time.Time              `json:"last_update"`
	RecordingPath string                 `json:"recording_path"`
	Metadata      map[string]interface{} `json:"metadata"`
	NodeID        string                 `json:"node_id"`
}

// Helper functions for environment variable parsing

func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
