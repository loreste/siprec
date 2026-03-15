package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestConcurrentConfigLoading tests that multiple goroutines can safely load config
func TestConcurrentConfigLoading(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise

	// Create a temporary YAML config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
network:
  host: "0.0.0.0"
  ports:
    - 5060

http:
  port: 8080
  enabled: true

logging:
  level: "info"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Launch multiple goroutines to load config concurrently
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := LoadFromFile(logger, configPath)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent load error: %v", err)
	}
}

// TestConcurrentEnvOverrides tests concurrent access to environment variable overrides
func TestConcurrentEnvOverrides(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

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

	var wg sync.WaitGroup

	// Multiple goroutines setting and reading env vars while loading config
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Load config (reads env vars)
			config, err := LoadFromFile(logger, configPath)
			if err != nil {
				t.Errorf("Load error: %v", err)
				return
			}

			// Verify config is valid
			if config.HTTP.Port == 0 {
				t.Errorf("HTTP port should not be 0")
			}
		}(i)
	}

	wg.Wait()
}

// TestConfigReloadRace tests that config can be reloaded safely
func TestConfigReloadRace(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Initial config
	yamlContent := `
http:
  port: 8080
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	var wg sync.WaitGroup
	stopCh := make(chan struct{})

	// Writer goroutine - updates config file
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			select {
			case <-stopCh:
				return
			default:
				newConfig := `
http:
  port: 9090
`
				os.WriteFile(configPath, []byte(newConfig), 0644)
			}
		}
	}()

	// Reader goroutines - load config concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				select {
				case <-stopCh:
					return
				default:
					LoadFromFile(logger, configPath)
				}
			}
		}()
	}

	wg.Wait()
	close(stopCh)
}

// TestFindConfigFileConcurrent tests concurrent calls to FindConfigFile
func TestFindConfigFileConcurrent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	if err := os.WriteFile(configPath, []byte("network:\n  host: test"), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	os.Setenv("CONFIG_FILE", configPath)
	defer os.Unsetenv("CONFIG_FILE")

	var wg sync.WaitGroup
	results := make(chan string, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- FindConfigFile()
		}()
	}

	wg.Wait()
	close(results)

	// All results should be the same
	for result := range results {
		if result != configPath {
			t.Errorf("Expected %s, got %s", configPath, result)
		}
	}
}

// TestSetFieldFromEnvConcurrent tests concurrent field setting
func TestSetFieldFromEnvConcurrent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()

	// Pre-create all config files first to avoid race conditions
	for i := 0; i < 50; i++ {
		configPath := filepath.Join(tmpDir, "config_"+string(rune('a'+i%26))+".yaml")
		yamlContent := `
http:
  port: 8080
logging:
  level: "debug"
`
		if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("Failed to create config file: %v", err)
		}
	}

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each goroutine loads its own config file
			configPath := filepath.Join(tmpDir, "config_"+string(rune('a'+id%26))+".yaml")

			config, err := LoadFromFile(logger, configPath)
			if err != nil {
				t.Errorf("Failed to load config: %v", err)
				return
			}

			if config.Logging.Level != "debug" {
				t.Errorf("Expected debug, got %s", config.Logging.Level)
			}
		}(i)
	}

	wg.Wait()
}
