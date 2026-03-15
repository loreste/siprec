package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// ConfigFileType represents the type of configuration file
type ConfigFileType string

const (
	ConfigFileTypeYAML ConfigFileType = "yaml"
	ConfigFileTypeJSON ConfigFileType = "json"
)

// LoadFromFile loads configuration from a YAML or JSON file
// Environment variables will override file values when set
func LoadFromFile(logger *logrus.Logger, configPath string) (*Config, error) {
	// Clean the path to prevent path traversal
	cleanPath := filepath.Clean(configPath)

	// Determine file type from extension
	ext := strings.ToLower(filepath.Ext(cleanPath))
	var fileType ConfigFileType
	switch ext {
	case ".yaml", ".yml":
		fileType = ConfigFileTypeYAML
	case ".json":
		fileType = ConfigFileTypeJSON
	default:
		// Default to YAML
		fileType = ConfigFileTypeYAML
	}

	// Read the config file
	// #nosec G304 -- Config path is provided by user/operator via CLI flag or environment variable
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, err
	}

	// Start with default configuration
	config := newDefaultConfig()

	// Parse the config file on top of defaults
	switch fileType {
	case ConfigFileTypeYAML:
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, err
		}
	case ConfigFileTypeJSON:
		if err := json.Unmarshal(data, config); err != nil {
			return nil, err
		}
	}

	logger.WithFields(logrus.Fields{
		"path": cleanPath,
		"type": fileType,
	}).Info("Loaded configuration from file")

	// Apply environment variable overrides
	applyEnvOverrides(logger, config)

	return config, nil
}

// newDefaultConfig returns a Config with sensible defaults
func newDefaultConfig() *Config {
	return &Config{
		Network: NetworkConfig{
			ExternalIP: "auto",
			InternalIP: "auto",
			Host:       "0.0.0.0",
			Ports:      []int{5060, 5061},
			RTPPortMin: 10000,
			RTPPortMax: 20000,
			RTPTimeout: 30 * time.Second,
		},
		HTTP: HTTPConfig{
			Port:          8080,
			Enabled:       true,
			EnableMetrics: true,
			EnableAPI:     true,
			ReadTimeout:   10 * time.Second,
			WriteTimeout:  30 * time.Second,
		},
		Recording: RecordingConfig{
			Directory:   "./recordings",
			MaxDuration: 4 * time.Hour,
			CleanupDays: 30,
			CombineLegs: true,
			Format:      "wav",
		},
		STT: STTConfig{
			DefaultVendor:    "google",
			SupportedVendors: []string{"google", "deepgram"},
			SupportedCodecs:  []string{"PCMU", "PCMA", "G722", "G729", "OPUS"},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Resources: ResourceConfig{
			MaxConcurrentCalls: 500,
		},
		Redundancy: RedundancyConfig{
			Enabled:              true,
			SessionTimeout:       30 * time.Second,
			SessionCheckInterval: 10 * time.Second,
			StorageType:          "memory",
		},
		Encryption: EncryptionConfig{
			Algorithm:          "AES-256-GCM",
			EncryptionKeyStore: "memory",
			KeySize:            32,
			NonceSize:          12,
			SaltSize:           32,
			PBKDF2Iterations:   100000,
		},
		Performance: PerformanceConfig{
			Enabled:         true,
			MemoryLimitMB:   512,
			CPULimit:        80,
			GCThresholdMB:   100,
			MonitorInterval: 30 * time.Second,
			EnableAutoGC:    true,
		},
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:             true,
			STTFailureThreshold: 3,
			STTTimeout:          30 * time.Second,
			STTRequestTimeout:   45 * time.Second,
		},
	}
}

// FindConfigFile looks for a configuration file in standard locations
func FindConfigFile() string {
	// Check command line flag first (via CONFIG_FILE env var)
	if configFile := os.Getenv("CONFIG_FILE"); configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			return configFile
		}
	}

	// Standard config file locations in order of priority
	locations := []string{
		"config.yaml",
		"config.yml",
		"config.json",
		"/etc/siprec/config.yaml",
		"/etc/siprec/config.yml",
		"/etc/siprec/config.json",
		"$HOME/.siprec/config.yaml",
		"$HOME/.siprec/config.yml",
		"$HOME/.siprec/config.json",
	}

	for _, loc := range locations {
		// Expand environment variables in path
		expanded := os.ExpandEnv(loc)
		if _, err := os.Stat(expanded); err == nil {
			return expanded
		}
	}

	return ""
}

// applyEnvOverrides applies environment variable overrides to the config
// Environment variables take precedence over file values
func applyEnvOverrides(logger *logrus.Logger, config *Config) {
	applyEnvOverridesRecursive(logger, reflect.ValueOf(config).Elem(), "")
}

// applyEnvOverridesRecursive recursively applies env overrides to struct fields
func applyEnvOverridesRecursive(logger *logrus.Logger, v reflect.Value, prefix string) {
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if !field.CanSet() {
			continue
		}

		// Get env tag
		envTag := fieldType.Tag.Get("env")

		// Handle nested structs
		if field.Kind() == reflect.Struct && envTag == "" {
			applyEnvOverridesRecursive(logger, field, prefix)
			continue
		}

		// Skip if no env tag
		if envTag == "" {
			continue
		}

		// Get environment variable value
		envValue := os.Getenv(envTag)
		if envValue == "" {
			continue
		}

		// Set the field value based on type
		if err := setFieldFromEnv(field, envValue); err != nil {
			logger.WithFields(logrus.Fields{
				"env":   envTag,
				"value": envValue,
				"error": err,
			}).Warn("Failed to apply environment override")
		} else {
			logger.WithFields(logrus.Fields{
				"env":   envTag,
				"field": fieldType.Name,
			}).Debug("Applied environment override")
		}
	}
}

// setFieldFromEnv sets a reflect.Value from a string environment variable
func setFieldFromEnv(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Special handling for time.Duration
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(value)
			if err != nil {
				return err
			}
			field.SetInt(int64(d))
		} else {
			i, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return err
			}
			field.SetInt(i)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return err
		}
		field.SetUint(u)

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		field.SetFloat(f)

	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(b)

	case reflect.Slice:
		// Handle []string and []int
		parts := strings.Split(value, ",")
		switch field.Type().Elem().Kind() {
		case reflect.String:
			slice := make([]string, len(parts))
			for i, p := range parts {
				slice[i] = strings.TrimSpace(p)
			}
			field.Set(reflect.ValueOf(slice))
		case reflect.Int:
			slice := make([]int, 0, len(parts))
			for _, p := range parts {
				if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
					slice = append(slice, i)
				}
			}
			field.Set(reflect.ValueOf(slice))
		}
	}

	return nil
}

// WriteExampleConfig writes an example configuration file
func WriteExampleConfig(path string) error {
	config := &Config{
		Network: NetworkConfig{
			ExternalIP: "auto",
			InternalIP: "auto",
			Host:       "0.0.0.0",
			Ports:      []int{5060, 5061},
			RTPPortMin: 10000,
			RTPPortMax: 20000,
			RTPTimeout: 30 * time.Second,
			EnableTLS:  false,
			EnableSRTP: false,
		},
		HTTP: HTTPConfig{
			Port:          8080,
			Enabled:       true,
			EnableMetrics: true,
			EnableAPI:     true,
			ReadTimeout:   10 * time.Second,
			WriteTimeout:  30 * time.Second,
		},
		Recording: RecordingConfig{
			Directory:   "./recordings",
			MaxDuration: 4 * time.Hour,
			CleanupDays: 30,
			CombineLegs: true,
			Format:      "wav",
			Quality:     5,
		},
		STT: STTConfig{
			DefaultVendor:    "google",
			SupportedVendors: []string{"google", "deepgram", "azure", "aws", "openai"},
			SupportedCodecs:  []string{"PCMU", "PCMA", "G722", "G729", "OPUS"},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Resources: ResourceConfig{
			MaxConcurrentCalls: 500,
		},
		Redundancy: RedundancyConfig{
			Enabled:              true,
			SessionTimeout:       30 * time.Second,
			SessionCheckInterval: 10 * time.Second,
			StorageType:          "memory",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	// Add header comment
	header := `# SIPREC Server Configuration
# This file contains all configuration options for the SIPREC server.
# Environment variables can override any value set here.
#
# For production deployments:
#   - Set CONFIG_FILE environment variable to point to this file
#   - Use environment variables for secrets (API keys, passwords)
#   - Review all security settings before deployment
#
# Documentation: https://github.com/loreste/siprec/docs/configuration.md

`
	return os.WriteFile(path, []byte(header+string(data)), 0600)
}
