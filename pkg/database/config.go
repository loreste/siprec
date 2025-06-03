package database

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// LoadMySQLConfig loads MySQL configuration from environment variables
func LoadMySQLConfig(logger *logrus.Logger) MySQLConfig {
	config := MySQLConfig{
		Host:            getEnvOrDefault("DB_HOST", "localhost"),
		Port:            getEnvIntOrDefault("DB_PORT", 3306),
		Database:        getEnvOrDefault("DB_NAME", "siprec"),
		Username:        getEnvOrDefault("DB_USERNAME", "siprec"),
		Password:        getEnvOrDefault("DB_PASSWORD", ""),
		MaxOpenConns:    getEnvIntOrDefault("DB_MAX_OPEN_CONNS", 25),
		MaxIdleConns:    getEnvIntOrDefault("DB_MAX_IDLE_CONNS", 5),
		ConnMaxLifetime: getEnvDurationOrDefault("DB_CONN_MAX_LIFETIME", 5*time.Minute),
		ConnMaxIdleTime: getEnvDurationOrDefault("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
		SSLMode:         getEnvOrDefault("DB_SSL_MODE", "false"),
		Charset:         getEnvOrDefault("DB_CHARSET", "utf8mb4"),
		ParseTime:       getEnvBoolOrDefault("DB_PARSE_TIME", true),
		Loc:             getEnvOrDefault("DB_TIMEZONE", "UTC"),
	}

	logger.WithFields(logrus.Fields{
		"host":              config.Host,
		"port":              config.Port,
		"database":          config.Database,
		"username":          config.Username,
		"max_open_conns":    config.MaxOpenConns,
		"max_idle_conns":    config.MaxIdleConns,
		"conn_max_lifetime": config.ConnMaxLifetime,
		"conn_max_idle":     config.ConnMaxIdleTime,
		"ssl_mode":          config.SSLMode,
		"charset":           config.Charset,
	}).Info("MySQL configuration loaded")

	return config
}

// ValidateConfig validates the MySQL configuration
func ValidateConfig(config MySQLConfig) error {
	if config.Host == "" {
		return fmt.Errorf("database host is required")
	}

	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("invalid database port: %d", config.Port)
	}

	if config.Database == "" {
		return fmt.Errorf("database name is required")
	}

	if config.Username == "" {
		return fmt.Errorf("database username is required")
	}

	if config.MaxOpenConns <= 0 {
		return fmt.Errorf("max open connections must be positive: %d", config.MaxOpenConns)
	}

	if config.MaxIdleConns < 0 {
		return fmt.Errorf("max idle connections cannot be negative: %d", config.MaxIdleConns)
	}

	if config.MaxIdleConns > config.MaxOpenConns {
		return fmt.Errorf("max idle connections (%d) cannot exceed max open connections (%d)",
			config.MaxIdleConns, config.MaxOpenConns)
	}

	if config.ConnMaxLifetime <= 0 {
		return fmt.Errorf("connection max lifetime must be positive: %v", config.ConnMaxLifetime)
	}

	if config.ConnMaxIdleTime <= 0 {
		return fmt.Errorf("connection max idle time must be positive: %v", config.ConnMaxIdleTime)
	}

	if config.Charset == "" {
		return fmt.Errorf("database charset is required")
	}

	return nil
}

// Helper functions

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvBoolOrDefault(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// Database configuration validation and setup utilities

// InitializeDatabase initializes the database connection and runs migrations
func InitializeDatabase(logger *logrus.Logger) (*MySQLDatabase, *Repository, error) {
	// Load configuration
	config := LoadMySQLConfig(logger)

	// Validate configuration
	if err := ValidateConfig(config); err != nil {
		return nil, nil, fmt.Errorf("invalid database configuration: %w", err)
	}

	// Create database connection
	db, err := NewMySQLDatabase(config, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Run migrations
	if err := db.Migrate(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	// Create repository
	repo := NewRepository(db, logger)

	logger.Info("Database initialization completed successfully")
	return db, repo, nil
}

// Health check configuration
type HealthConfig struct {
	Enabled           bool
	CheckInterval     time.Duration
	FailureThreshold  int
	RecoveryThreshold int
}

// LoadHealthConfig loads health check configuration
func LoadHealthConfig() HealthConfig {
	return HealthConfig{
		Enabled:           getEnvBoolOrDefault("DB_HEALTH_ENABLED", true),
		CheckInterval:     getEnvDurationOrDefault("DB_HEALTH_INTERVAL", 30*time.Second),
		FailureThreshold:  getEnvIntOrDefault("DB_HEALTH_FAILURE_THRESHOLD", 3),
		RecoveryThreshold: getEnvIntOrDefault("DB_HEALTH_RECOVERY_THRESHOLD", 2),
	}
}
