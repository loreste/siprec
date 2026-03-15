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

func TestResourceConfigDefaults(t *testing.T) {
	config := newDefaultConfig()

	// Verify resource config defaults
	if config.Resources.MaxConcurrentCalls != 500 {
		t.Errorf("Expected MaxConcurrentCalls 500, got %d", config.Resources.MaxConcurrentCalls)
	}

	if config.Resources.MaxRTPStreams != 1500 {
		t.Errorf("Expected MaxRTPStreams 1500, got %d", config.Resources.MaxRTPStreams)
	}

	if config.Resources.WorkerPoolSize != 0 {
		t.Errorf("Expected WorkerPoolSize 0 (auto), got %d", config.Resources.WorkerPoolSize)
	}

	if config.Resources.MaxMemoryMB != 0 {
		t.Errorf("Expected MaxMemoryMB 0 (unlimited), got %d", config.Resources.MaxMemoryMB)
	}

	if config.Resources.HorizontalScaling != false {
		t.Error("Expected HorizontalScaling false by default")
	}

	if config.Resources.NodeID != "" {
		t.Errorf("Expected empty NodeID, got %s", config.Resources.NodeID)
	}
}

func TestLawfulInterceptConfigDefaults(t *testing.T) {
	config := newDefaultConfig()

	// Verify lawful intercept config defaults
	if config.LawfulIntercept.Enabled != false {
		t.Error("Expected LawfulIntercept.Enabled false by default")
	}

	if config.LawfulIntercept.MutualTLS != true {
		t.Error("Expected LawfulIntercept.MutualTLS true by default")
	}

	if config.LawfulIntercept.AuditLogPath != "/var/log/siprec/li_audit.log" {
		t.Errorf("Expected AuditLogPath /var/log/siprec/li_audit.log, got %s", config.LawfulIntercept.AuditLogPath)
	}
}

func TestSpeakerDiarizationConfigDefaults(t *testing.T) {
	config := newDefaultConfig()

	// Verify speaker diarization config defaults
	if config.SpeakerDiarization.Enabled != true {
		t.Error("Expected SpeakerDiarization.Enabled true by default")
	}

	if config.SpeakerDiarization.MaxSpeakers != 10 {
		t.Errorf("Expected MaxSpeakers 10, got %d", config.SpeakerDiarization.MaxSpeakers)
	}

	if config.SpeakerDiarization.SimilarityThreshold != 0.7 {
		t.Errorf("Expected SimilarityThreshold 0.7, got %f", config.SpeakerDiarization.SimilarityThreshold)
	}

	if config.SpeakerDiarization.VoiceFeatures != true {
		t.Error("Expected VoiceFeatures true by default")
	}

	if config.SpeakerDiarization.CrossSessionTracking != false {
		t.Error("Expected CrossSessionTracking false by default")
	}

	if config.SpeakerDiarization.ProfileRetentionDays != 30 {
		t.Errorf("Expected ProfileRetentionDays 30, got %d", config.SpeakerDiarization.ProfileRetentionDays)
	}
}

func TestEnterpriseConfigFromYAML(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "enterprise.yaml")

	yamlContent := `
resources:
  max_concurrent_calls: 25000
  max_rtp_streams: 75000
  worker_pool_size: 64
  max_memory_mb: 16384
  horizontal_scaling: true
  node_id: "node-1"

lawful_intercept:
  enabled: true
  delivery_endpoint: "https://lea.example.com/deliver"
  encryption_key_path: "/etc/siprec/li.key"
  warrant_verification_endpoint: "https://warrant.example.com/verify"
  audit_log_path: "/var/log/siprec/li_audit.log"
  mutual_tls: true
  client_cert_path: "/etc/siprec/client.crt"
  client_key_path: "/etc/siprec/client.key"
  retention_days: 730

speaker_diarization:
  enabled: true
  max_speakers: 20
  similarity_threshold: 0.85
  voice_features: true
  cross_session_tracking: true
  profile_retention_days: 90
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify resource config
	if config.Resources.MaxConcurrentCalls != 25000 {
		t.Errorf("Expected MaxConcurrentCalls 25000, got %d", config.Resources.MaxConcurrentCalls)
	}
	if config.Resources.MaxRTPStreams != 75000 {
		t.Errorf("Expected MaxRTPStreams 75000, got %d", config.Resources.MaxRTPStreams)
	}
	if config.Resources.WorkerPoolSize != 64 {
		t.Errorf("Expected WorkerPoolSize 64, got %d", config.Resources.WorkerPoolSize)
	}
	if config.Resources.MaxMemoryMB != 16384 {
		t.Errorf("Expected MaxMemoryMB 16384, got %d", config.Resources.MaxMemoryMB)
	}
	if config.Resources.HorizontalScaling != true {
		t.Error("Expected HorizontalScaling true")
	}
	if config.Resources.NodeID != "node-1" {
		t.Errorf("Expected NodeID node-1, got %s", config.Resources.NodeID)
	}

	// Verify lawful intercept config
	if config.LawfulIntercept.Enabled != true {
		t.Error("Expected LawfulIntercept.Enabled true")
	}
	if config.LawfulIntercept.DeliveryEndpoint != "https://lea.example.com/deliver" {
		t.Errorf("Expected DeliveryEndpoint https://lea.example.com/deliver, got %s", config.LawfulIntercept.DeliveryEndpoint)
	}
	if config.LawfulIntercept.RetentionDays != 730 {
		t.Errorf("Expected RetentionDays 730, got %d", config.LawfulIntercept.RetentionDays)
	}

	// Verify speaker diarization config
	if config.SpeakerDiarization.MaxSpeakers != 20 {
		t.Errorf("Expected MaxSpeakers 20, got %d", config.SpeakerDiarization.MaxSpeakers)
	}
	if config.SpeakerDiarization.SimilarityThreshold != 0.85 {
		t.Errorf("Expected SimilarityThreshold 0.85, got %f", config.SpeakerDiarization.SimilarityThreshold)
	}
	if config.SpeakerDiarization.CrossSessionTracking != true {
		t.Error("Expected CrossSessionTracking true")
	}
	if config.SpeakerDiarization.ProfileRetentionDays != 90 {
		t.Errorf("Expected ProfileRetentionDays 90, got %d", config.SpeakerDiarization.ProfileRetentionDays)
	}
}

func TestEnterpriseConfigFromJSON(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "enterprise.json")

	jsonContent := `{
  "resources": {
    "max_concurrent_calls": 50000,
    "max_rtp_streams": 150000,
    "worker_pool_size": 128,
    "max_memory_mb": 32768,
    "horizontal_scaling": true,
    "node_id": "node-2"
  },
  "lawful_intercept": {
    "enabled": true,
    "delivery_endpoint": "https://lea2.example.com/deliver",
    "mutual_tls": true,
    "retention_days": 365
  },
  "speaker_diarization": {
    "enabled": true,
    "max_speakers": 15,
    "similarity_threshold": 0.75,
    "cross_session_tracking": false
  }
}`

	if err := os.WriteFile(configPath, []byte(jsonContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify resource config
	if config.Resources.MaxConcurrentCalls != 50000 {
		t.Errorf("Expected MaxConcurrentCalls 50000, got %d", config.Resources.MaxConcurrentCalls)
	}
	if config.Resources.NodeID != "node-2" {
		t.Errorf("Expected NodeID node-2, got %s", config.Resources.NodeID)
	}

	// Verify lawful intercept config
	if config.LawfulIntercept.DeliveryEndpoint != "https://lea2.example.com/deliver" {
		t.Errorf("Expected DeliveryEndpoint https://lea2.example.com/deliver, got %s", config.LawfulIntercept.DeliveryEndpoint)
	}

	// Verify speaker diarization config
	if config.SpeakerDiarization.MaxSpeakers != 15 {
		t.Errorf("Expected MaxSpeakers 15, got %d", config.SpeakerDiarization.MaxSpeakers)
	}
}

func TestEnterpriseConfigEnvOverrides(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
resources:
  max_concurrent_calls: 500

lawful_intercept:
  enabled: false

speaker_diarization:
  max_speakers: 10
`

	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Set environment variable overrides
	os.Setenv("MAX_CONCURRENT_CALLS", "100000")
	os.Setenv("MAX_RTP_STREAMS", "300000")
	os.Setenv("HORIZONTAL_SCALING", "true")
	os.Setenv("NODE_ID", "env-node-1")
	os.Setenv("LI_ENABLED", "true")
	os.Setenv("LI_DELIVERY_ENDPOINT", "https://env-lea.example.com")
	os.Setenv("DIARIZATION_MAX_SPEAKERS", "25")
	os.Setenv("DIARIZATION_THRESHOLD", "0.9")
	os.Setenv("DIARIZATION_CROSS_SESSION", "true")

	defer func() {
		os.Unsetenv("MAX_CONCURRENT_CALLS")
		os.Unsetenv("MAX_RTP_STREAMS")
		os.Unsetenv("HORIZONTAL_SCALING")
		os.Unsetenv("NODE_ID")
		os.Unsetenv("LI_ENABLED")
		os.Unsetenv("LI_DELIVERY_ENDPOINT")
		os.Unsetenv("DIARIZATION_MAX_SPEAKERS")
		os.Unsetenv("DIARIZATION_THRESHOLD")
		os.Unsetenv("DIARIZATION_CROSS_SESSION")
	}()

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify environment overrides for resources
	if config.Resources.MaxConcurrentCalls != 100000 {
		t.Errorf("Expected MaxConcurrentCalls 100000 (env override), got %d", config.Resources.MaxConcurrentCalls)
	}
	if config.Resources.MaxRTPStreams != 300000 {
		t.Errorf("Expected MaxRTPStreams 300000 (env override), got %d", config.Resources.MaxRTPStreams)
	}
	if config.Resources.HorizontalScaling != true {
		t.Error("Expected HorizontalScaling true (env override)")
	}
	if config.Resources.NodeID != "env-node-1" {
		t.Errorf("Expected NodeID env-node-1 (env override), got %s", config.Resources.NodeID)
	}

	// Verify environment overrides for lawful intercept
	if config.LawfulIntercept.Enabled != true {
		t.Error("Expected LawfulIntercept.Enabled true (env override)")
	}
	if config.LawfulIntercept.DeliveryEndpoint != "https://env-lea.example.com" {
		t.Errorf("Expected DeliveryEndpoint https://env-lea.example.com (env override), got %s", config.LawfulIntercept.DeliveryEndpoint)
	}

	// Verify environment overrides for speaker diarization
	if config.SpeakerDiarization.MaxSpeakers != 25 {
		t.Errorf("Expected MaxSpeakers 25 (env override), got %d", config.SpeakerDiarization.MaxSpeakers)
	}
	if config.SpeakerDiarization.SimilarityThreshold != 0.9 {
		t.Errorf("Expected SimilarityThreshold 0.9 (env override), got %f", config.SpeakerDiarization.SimilarityThreshold)
	}
	if config.SpeakerDiarization.CrossSessionTracking != true {
		t.Error("Expected CrossSessionTracking true (env override)")
	}
}

func TestWriteExampleConfigIncludesEnterpriseFeatures(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "example-enterprise.yaml")

	if err := WriteExampleConfig(configPath); err != nil {
		t.Fatalf("Failed to write example config: %v", err)
	}

	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config, err := LoadFromFile(logger, configPath)
	if err != nil {
		t.Fatalf("Failed to load example config: %v", err)
	}

	// Verify enterprise features are included in example config
	if config.Resources.MaxRTPStreams != 1500 {
		t.Errorf("Expected MaxRTPStreams 1500 in example config, got %d", config.Resources.MaxRTPStreams)
	}

	if config.LawfulIntercept.MutualTLS != true {
		t.Error("Expected LawfulIntercept.MutualTLS true in example config")
	}

	if config.SpeakerDiarization.Enabled != true {
		t.Error("Expected SpeakerDiarization.Enabled true in example config")
	}

	if config.SpeakerDiarization.MaxSpeakers != 10 {
		t.Errorf("Expected MaxSpeakers 10 in example config, got %d", config.SpeakerDiarization.MaxSpeakers)
	}
}
