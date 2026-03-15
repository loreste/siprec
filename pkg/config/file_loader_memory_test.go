package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
)

// TestConfigLoadMemoryLeak tests that repeated config loading doesn't leak memory
func TestConfigLoadMemoryLeak(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
network:
  host: "0.0.0.0"
  ports:
    - 5060
    - 5061

http:
  port: 8080
  enabled: true

recording:
  directory: "./recordings"

logging:
  level: "info"
  format: "json"

stt:
  default_vendor: "google"
  supported_vendors:
    - "google"
    - "deepgram"
    - "azure"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Force GC and get baseline
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// Load config many times
	for i := 0; i < 1000; i++ {
		config, err := LoadFromFile(logger, configPath)
		if err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		// Use config to prevent optimization
		_ = config.HTTP.Port
	}

	// Force GC and measure
	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Check that memory didn't grow excessively
	// Allow for some growth but flag if it's more than 10MB
	memGrowth := int64(m2.Alloc) - int64(m1.Alloc)
	if memGrowth > 10*1024*1024 {
		t.Errorf("Possible memory leak: memory grew by %d bytes after 1000 config loads", memGrowth)
	}

	t.Logf("Memory stats: initial=%d KB, final=%d KB, growth=%d KB",
		m1.Alloc/1024, m2.Alloc/1024, memGrowth/1024)
}

// TestWriteExampleConfigMemory tests that WriteExampleConfig doesn't leak
func TestWriteExampleConfigMemory(t *testing.T) {
	tmpDir := t.TempDir()

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	for i := 0; i < 100; i++ {
		configPath := filepath.Join(tmpDir, "config_"+string(rune('a'+i%26))+".yaml")
		if err := WriteExampleConfig(configPath); err != nil {
			t.Fatalf("Failed to write example config: %v", err)
		}
	}

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	memGrowth := int64(m2.Alloc) - int64(m1.Alloc)
	if memGrowth > 5*1024*1024 {
		t.Errorf("Possible memory leak in WriteExampleConfig: growth=%d bytes", memGrowth)
	}

	t.Logf("Memory stats: initial=%d KB, final=%d KB, growth=%d KB",
		m1.Alloc/1024, m2.Alloc/1024, memGrowth/1024)
}

// TestLargeConfigFile tests loading a large configuration file
func TestLargeConfigFile(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "large_config.yaml")

	// Create a large config with many entries
	var yamlContent string
	yamlContent = "network:\n  host: \"0.0.0.0\"\n  ports:\n"
	for i := 0; i < 100; i++ {
		yamlContent += "    - " + string(rune('0'+i%10)) + "000\n"
	}

	yamlContent += "\nhttp:\n  port: 8080\n"
	yamlContent += "\nlogging:\n  level: \"info\"\n"

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write large config: %v", err)
	}

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load large config: %v", err)
	}

	if len(config.Network.Ports) != 100 {
		t.Errorf("Expected 100 ports, got %d", len(config.Network.Ports))
	}
}

// TestInvalidConfigFiles tests handling of invalid config files
func TestInvalidConfigFiles(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		content  string
		wantErr  bool
		fileType string
	}{
		{
			name:     "invalid yaml",
			content:  "network:\n  host: [invalid",
			wantErr:  true,
			fileType: ".yaml",
		},
		{
			name:     "invalid json",
			content:  `{"network": {"host": }`,
			wantErr:  true,
			fileType: ".json",
		},
		{
			name:     "empty yaml",
			content:  "",
			wantErr:  false,
			fileType: ".yaml",
		},
		{
			name:     "empty json",
			content:  "{}",
			wantErr:  false,
			fileType: ".json",
		},
		{
			name:     "yaml with unknown fields",
			content:  "unknown_field: value\nnetwork:\n  host: test",
			wantErr:  false, // Should ignore unknown fields
			fileType: ".yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := filepath.Join(tmpDir, "config"+tt.fileType)
			if err := os.WriteFile(configPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}

			_, err := LoadFromFile(logger, configPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromFile() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestConfigFilePermissions tests handling of files with different permissions
func TestConfigFilePermissions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := "http:\n  port: 8080\n"

	// Test readable file
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Errorf("Should be able to read file with 0644 permissions: %v", err)
	}

	// Test non-existent file
	_, err = LoadFromFile(logger, filepath.Join(tmpDir, "nonexistent.yaml"))
	if err == nil {
		t.Error("Should fail for non-existent file")
	}
}

// BenchmarkConfigLoad benchmarks config loading performance
func BenchmarkConfigLoad(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
network:
  host: "0.0.0.0"
  ports:
    - 5060

http:
  port: 8080

logging:
  level: "info"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("Failed to write config: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadFromFile(logger, configPath)
		if err != nil {
			b.Fatalf("Failed to load config: %v", err)
		}
	}
}

// BenchmarkEnvOverrides benchmarks environment variable override application
func BenchmarkEnvOverrides(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
http:
  port: 8080
logging:
  level: "info"
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		b.Fatalf("Failed to write config: %v", err)
	}

	os.Setenv("HTTP_PORT", "9090")
	os.Setenv("LOG_LEVEL", "debug")
	defer os.Unsetenv("HTTP_PORT")
	defer os.Unsetenv("LOG_LEVEL")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LoadFromFile(logger, configPath)
		if err != nil {
			b.Fatalf("Failed to load config: %v", err)
		}
	}
}
