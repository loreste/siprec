package test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
)

// LoadEnvironment loads the .env file from the project root
func LoadEnvironment(logger *logrus.Logger) (string, error) {
	// Try multiple strategies to find the project root

	// Strategy 1: Check if .env exists in current directory
	if _, err := os.Stat(".env"); err == nil {
		projectRoot, err := filepath.Abs(".")
		if err != nil {
			return "", fmt.Errorf("error getting absolute path: %v", err)
		}

		err = godotenv.Load(".env")
		if err != nil {
			return "", fmt.Errorf("failed to load .env file from current directory: %v", err)
		}

		return projectRoot, nil
	}

	// Strategy 2: Check if .env exists in parent directory
	projectRoot, err := filepath.Abs("..")
	if err != nil {
		return "", fmt.Errorf("error getting parent directory: %v", err)
	}

	envPath := filepath.Join(projectRoot, ".env")
	if _, err := os.Stat(envPath); err == nil {
		err = godotenv.Load(envPath)
		if err != nil {
			return "", fmt.Errorf("failed to load .env file from parent directory: %v", err)
		}

		return projectRoot, nil
	}

	// Strategy 3: Find the project root by traversing up from runtime caller
	_, filename, _, ok := runtime.Caller(0)
	if ok {
		// Get the directory containing this file
		dir := filepath.Dir(filename)

		// Try to find the project root by looking for a go.mod file or a .git directory
		for {
			// Check if this directory has a go.mod or .git directory
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				// Try loading .env from this directory
				envPath := filepath.Join(dir, ".env")
				if _, err := os.Stat(envPath); err == nil {
					err = godotenv.Load(envPath)
					if err != nil {
						logger.Warnf("Found .env at %s but failed to load: %v", envPath, err)
					} else {
						return dir, nil
					}
				}
			}

			// Go up one directory
			parent := filepath.Dir(dir)
			if parent == dir {
				// We've reached the root, time to stop
				break
			}
			dir = parent

			// Stop if we've gone too far (more than 5 levels up)
			if strings.Count(filename, string(os.PathSeparator))-strings.Count(dir, string(os.PathSeparator)) > 5 {
				break
			}
		}
	}

	// Strategy 4: Check common project directories
	paths := []string{
		// Try to find the siprec directory in common locations, without hardcoding the user directory
		filepath.Join(projectRoot, "siprec"),
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		// Add user's home directory paths
		paths = append(paths,
			filepath.Join(homeDir, "opensource", "siprec"),
			filepath.Join(homeDir, "siprec"),
			filepath.Join(homeDir, "go", "src", "siprec-server"),
		)
	}

	for _, path := range paths {
		envPath := filepath.Join(path, ".env")
		if _, err := os.Stat(envPath); err == nil {
			err = godotenv.Load(envPath)
			if err != nil {
				logger.Warnf("Found .env at %s but failed to load: %v", envPath, err)
				continue
			}

			return path, nil
		}
	}

	return "", fmt.Errorf("could not find .env file in any of the expected locations")
}

// GetEnv gets an environment variable
func GetEnv(key string) string {
	return os.Getenv(key)
}

// GetEnvWithDefault gets an environment variable with a default value
func GetEnvWithDefault(key, defaultValue string) string {
	value := GetEnv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// RunEnvironmentCheck verifies environment setup for the application
func RunEnvironmentCheck() {
	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logger.SetOutput(os.Stdout)

	// Check for LOG_LEVEL environment variable
	logLevel := GetEnvWithDefault("LOG_LEVEL", "info")
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		logger.Warnf("Invalid LOG_LEVEL value '%s', defaulting to info", logLevel)
		level = logrus.InfoLevel
	}
	logger.SetLevel(level)

	// Try current working directory for .env
	workingDir, err := os.Getwd()
	if err == nil {
		logger.Infof("Current working directory: %s", workingDir)
	}

	// Load environment with more detailed error reporting
	projectRoot, err := LoadEnvironment(logger)
	if err != nil {
		logger.Errorf("Environment setup warning: %v", err)
		logger.Warn("Continuing with default values. Some features may not work correctly.")

		// Set working directory as project root
		projectRoot, _ = filepath.Abs(".")
	} else {
		// Log successful loading
		logger.Info("Successfully loaded .env file")
	}

	// Print environment variables relevant to redundancy
	fmt.Println("Environment Variables:")
	fmt.Println("======================")

	// Define the environment variables to check
	envVars := map[string]string{
		"EXTERNAL_IP":             GetEnvWithDefault("EXTERNAL_IP", "auto"),
		"INTERNAL_IP":             GetEnvWithDefault("INTERNAL_IP", "auto"),
		"PORTS":                   GetEnvWithDefault("PORTS", "5060,5061"),
		"RTP_PORT_MIN":            GetEnvWithDefault("RTP_PORT_MIN", "10000"),
		"RTP_PORT_MAX":            GetEnvWithDefault("RTP_PORT_MAX", "20000"),
		"RECORDING_DIR":           GetEnvWithDefault("RECORDING_DIR", "./recordings"),
		"ENABLE_REDUNDANCY":       GetEnvWithDefault("ENABLE_REDUNDANCY", "true"),
		"SESSION_TIMEOUT":         GetEnvWithDefault("SESSION_TIMEOUT", "30s"),
		"SESSION_CHECK_INTERVAL":  GetEnvWithDefault("SESSION_CHECK_INTERVAL", "10s"),
		"REDUNDANCY_STORAGE_TYPE": GetEnvWithDefault("REDUNDANCY_STORAGE_TYPE", "memory"),
	}

	// Print all environment variables
	for key, value := range envVars {
		fmt.Printf("%s: %s\n", key, value)
	}

	// Check if important directories exist
	recordingDir := envVars["RECORDING_DIR"]
	if recordingDir == "" {
		recordingDir = "./recordings" // Set default if empty
	}

	// Handle relative paths
	var recordingDirPath string
	if filepath.IsAbs(recordingDir) {
		recordingDirPath = recordingDir
	} else {
		recordingDirPath = filepath.Join(projectRoot, recordingDir)
	}

	// Verify recording directory
	if _, err := os.Stat(recordingDirPath); os.IsNotExist(err) {
		logger.Warnf("Recording directory %s does not exist, attempting to create it", recordingDirPath)
		if err := os.MkdirAll(recordingDirPath, 0755); err != nil {
			logger.Errorf("Failed to create recording directory: %v", err)
			logger.Warn("Recordings may not work correctly")
		} else {
			logger.Infof("Created recording directory: %s", recordingDirPath)
		}
	} else if err != nil {
		logger.Errorf("Error checking recording directory: %v", err)
	} else {
		logger.Infof("Recording directory exists: %s", recordingDirPath)
	}

	// Check session directory
	sessionsDir := "sessions"
	var sessionsDirPath string
	if filepath.IsAbs(sessionsDir) {
		sessionsDirPath = sessionsDir
	} else {
		sessionsDirPath = filepath.Join(projectRoot, sessionsDir)
	}

	// Verify sessions directory
	if _, err := os.Stat(sessionsDirPath); os.IsNotExist(err) {
		logger.Warnf("Sessions directory %s does not exist, attempting to create it", sessionsDirPath)
		if err := os.MkdirAll(sessionsDirPath, 0755); err != nil {
			logger.Errorf("Failed to create sessions directory: %v", err)
			logger.Warn("Session persistence may not work correctly")
		} else {
			logger.Infof("Created sessions directory: %s", sessionsDirPath)
		}
	} else if err != nil {
		logger.Errorf("Error checking sessions directory: %v", err)
	} else {
		logger.Infof("Sessions directory exists: %s", sessionsDirPath)
	}

	// Verify redundancy configuration
	if envVars["ENABLE_REDUNDANCY"] == "true" {
		logger.Info("Session redundancy is ENABLED")

		// Verify SESSION_TIMEOUT format
		sessionTimeout := envVars["SESSION_TIMEOUT"]
		if sessionTimeout == "" {
			logger.Warn("SESSION_TIMEOUT is not set, using default of 30s")
		}

		// Verify SESSION_CHECK_INTERVAL format
		sessionCheckInterval := envVars["SESSION_CHECK_INTERVAL"]
		if sessionCheckInterval == "" {
			logger.Warn("SESSION_CHECK_INTERVAL is not set, using default of 10s")
		}

		// Verify REDUNDANCY_STORAGE_TYPE
		storageType := envVars["REDUNDANCY_STORAGE_TYPE"]
		if storageType == "" {
			logger.Warn("REDUNDANCY_STORAGE_TYPE is not set, using default of 'memory'")
		} else if storageType != "memory" && storageType != "redis" {
			logger.Warnf("Invalid REDUNDANCY_STORAGE_TYPE '%s', only 'memory' and 'redis' are supported", storageType)
		}
	} else {
		logger.Warn("Session redundancy is DISABLED")
	}

	logger.Info("Environment check completed successfully")
}
