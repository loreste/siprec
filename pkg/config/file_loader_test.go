package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestLoadFromYAMLFile(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create a temporary YAML config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Note: YAML field names must match the json tags in the struct
	yamlContent := `
network:
  host: "192.168.1.100"
  ports:
    - 5060
    - 5061

http:
  port: 9090
  enabled: true

recording:
  directory: "/var/recordings"

logging:
  level: "debug"
  format: "json"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Load the config
	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify values - using json tag names which YAML also respects
	if config.Network.Host != "192.168.1.100" {
		t.Errorf("Expected host 192.168.1.100, got %s", config.Network.Host)
	}

	if len(config.Network.Ports) != 2 || config.Network.Ports[0] != 5060 {
		t.Errorf("Expected ports [5060, 5061], got %v", config.Network.Ports)
	}

	if config.HTTP.Port != 9090 {
		t.Errorf("Expected HTTP port 9090, got %d", config.HTTP.Port)
	}

	if config.Recording.Directory != "/var/recordings" {
		t.Errorf("Expected recording dir /var/recordings, got %s", config.Recording.Directory)
	}

	if config.Logging.Level != "debug" {
		t.Errorf("Expected log level debug, got %s", config.Logging.Level)
	}
}

func TestLoadFromJSONFile(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create a temporary JSON config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	jsonContent := `{
  "network": {
    "host": "10.0.0.1",
    "ports": [5080],
    "rtp_port_min": 20000,
    "rtp_port_max": 30000
  },
  "http": {
    "port": 8888,
    "enabled": true
  },
  "logging": {
    "level": "warn"
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Load the config
	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify values
	if config.Network.Host != "10.0.0.1" {
		t.Errorf("Expected host 10.0.0.1, got %s", config.Network.Host)
	}

	if config.HTTP.Port != 8888 {
		t.Errorf("Expected HTTP port 8888, got %d", config.HTTP.Port)
	}

	if config.Logging.Level != "warn" {
		t.Errorf("Expected log level warn, got %s", config.Logging.Level)
	}
}

func TestEnvOverrides(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create a temporary YAML config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
http:
  port: 8080

logging:
  level: "info"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set environment variable override
	os.Setenv("HTTP_PORT", "9999")
	os.Setenv("LOG_LEVEL", "error")
	defer os.Unsetenv("HTTP_PORT")
	defer os.Unsetenv("LOG_LEVEL")

	// Load the config
	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Environment variables should override file values
	if config.HTTP.Port != 9999 {
		t.Errorf("Expected HTTP port 9999 (env override), got %d", config.HTTP.Port)
	}

	if config.Logging.Level != "error" {
		t.Errorf("Expected log level error (env override), got %s", config.Logging.Level)
	}
}

func TestFindConfigFile(t *testing.T) {
	// Test with CONFIG_FILE env var
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom-config.yaml")

	if err := os.WriteFile(configPath, []byte("network:\n  host: test"), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	os.Setenv("CONFIG_FILE", configPath)
	defer os.Unsetenv("CONFIG_FILE")

	found := FindConfigFile()
	if found != configPath {
		t.Errorf("Expected to find %s, got %s", configPath, found)
	}
}

func TestWriteExampleConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "example-config.yaml")

	if err := WriteExampleConfig(configPath); err != nil {
		t.Fatalf("Failed to write example config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Example config file was not created")
	}

	// Verify it can be loaded
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load example config: %v", err)
	}

	// Verify some default values
	if config.HTTP.Port != 8080 {
		t.Errorf("Expected default HTTP port 8080, got %d", config.HTTP.Port)
	}

	if config.Network.RTPPortMin != 10000 {
		t.Errorf("Expected default RTP port min 10000, got %d", config.Network.RTPPortMin)
	}
}
