package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ConfigValidator handles configuration validation
type ConfigValidator struct {
	logger *logrus.Logger
	errors []ValidationError
	warnings []ValidationWarning
}

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string `json:"field"`
	Value   interface{} `json:"value"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// ValidationWarning represents a configuration validation warning
type ValidationWarning struct {
	Field   string `json:"field"`
	Value   interface{} `json:"value"`
	Message string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ValidationResult represents the result of configuration validation
type ValidationResult struct {
	Valid    bool                `json:"valid"`
	Errors   []ValidationError   `json:"errors,omitempty"`
	Warnings []ValidationWarning `json:"warnings,omitempty"`
	Summary  string              `json:"summary"`
}

// NewConfigValidator creates a new configuration validator
func NewConfigValidator(logger *logrus.Logger) *ConfigValidator {
	return &ConfigValidator{
		logger: logger,
		errors: make([]ValidationError, 0),
		warnings: make([]ValidationWarning, 0),
	}
}

// ValidateConfig validates the entire configuration
func (v *ConfigValidator) ValidateConfig(config *Config) *ValidationResult {
	// Reset validation state
	v.errors = make([]ValidationError, 0)
	v.warnings = make([]ValidationWarning, 0)

	// Validate different sections
	v.validateNetworkConfig(config)
	v.validateAudioConfig(config)
	v.validateSTTConfig(config)
	v.validateHTTPConfig(config)
	v.validateSecurityConfig(config)
	v.validateStorageConfig(config)
	v.validatePerformanceConfig(config)
	v.validateLoggingConfig(config)

	// Create result
	result := &ValidationResult{
		Valid:    len(v.errors) == 0,
		Errors:   v.errors,
		Warnings: v.warnings,
	}

	// Generate summary
	result.Summary = v.generateSummary()

	if len(v.errors) > 0 {
		v.logger.WithField("error_count", len(v.errors)).Error("Configuration validation failed")
		for _, err := range v.errors {
			v.logger.WithFields(logrus.Fields{
				"field": err.Field,
				"value": err.Value,
				"rule":  err.Rule,
			}).Error(err.Message)
		}
	}

	if len(v.warnings) > 0 {
		v.logger.WithField("warning_count", len(v.warnings)).Warning("Configuration validation completed with warnings")
		for _, warning := range v.warnings {
			v.logger.WithFields(logrus.Fields{
				"field": warning.Field,
				"value": warning.Value,
			}).Warning(warning.Message)
		}
	}

	return result
}

// validateNetworkConfig validates network-related configuration
func (v *ConfigValidator) validateNetworkConfig(config *Config) {
	// Validate ports
	if len(config.Network.Ports) == 0 {
		v.addError("sip_ports", config.Network.Ports, "required", "At least one SIP port must be configured")
	}

	for _, port := range config.Network.Ports {
		if !v.isValidPort(port) {
			v.addError("sip_ports", port, "range", fmt.Sprintf("Invalid SIP port %d (must be 1-65535)", port))
		}
		if port < 1024 && os.Getuid() != 0 {
			v.addWarning("sip_ports", port, fmt.Sprintf("Port %d requires root privileges", port), "Consider using ports > 1024")
		}
	}

	// Validate RTP port range
	if config.Network.RTPPortMin >= config.Network.RTPPortMax {
		v.addError("rtp_ports", map[string]int{"min": config.Network.RTPPortMin, "max": config.Network.RTPPortMax}, 
			"range", "RTP port minimum must be less than maximum")
	}

	if config.Network.RTPPortMax - config.Network.RTPPortMin < 100 {
		v.addWarning("rtp_ports", config.Network.RTPPortMax - config.Network.RTPPortMin, 
			"Small RTP port range may limit concurrent calls", "Consider using at least 1000 ports")
	}

	// Validate IP addresses
	if config.Network.ExternalIP != "auto" && config.Network.ExternalIP != "" {
		if !v.isValidIP(config.Network.ExternalIP) {
			v.addError("external_ip", config.Network.ExternalIP, "format", "Invalid IP address format")
		}
	}

	if config.Network.InternalIP != "auto" && config.Network.InternalIP != "" {
		if !v.isValidIP(config.Network.InternalIP) {
			v.addError("internal_ip", config.Network.InternalIP, "format", "Invalid IP address format")
		}
	}

	// Validate STUN servers
	for _, stunServer := range config.Network.STUNServers {
		if !v.isValidSTUNServer(stunServer) {
			v.addError("stun_servers", stunServer, "format", "Invalid STUN server format (should be host:port)")
		}
	}
}

// validateAudioConfig validates audio processing configuration
func (v *ConfigValidator) validateAudioConfig(config *Config) {
	// Validate codec support
	if len(config.STT.SupportedCodecs) == 0 {
		v.addWarning("supported_codecs", config.STT.SupportedCodecs, "No audio codecs configured", "At least one codec should be supported")
	}

	validCodecs := []string{"PCMU", "PCMA", "G722", "G729", "OPUS"}
	for _, codec := range config.STT.SupportedCodecs {
		if !v.isValidCodec(codec, validCodecs) {
			v.addError("supported_codecs", codec, "supported", fmt.Sprintf("Unsupported codec %s", codec))
		}
	}
}

// validateSTTConfig validates speech-to-text configuration
func (v *ConfigValidator) validateSTTConfig(config *Config) {
	// Validate STT vendors
	if len(config.STT.SupportedVendors) == 0 {
		v.addWarning("stt_vendors", config.STT.SupportedVendors, "No STT vendors configured", "STT functionality will be disabled")
	}

	validVendors := []string{"google", "azure", "aws", "deepgram", "openai", "mock"}
	for _, vendor := range config.STT.SupportedVendors {
		if !v.contains(validVendors, vendor) {
			v.addError("stt_vendors", vendor, "supported", fmt.Sprintf("Unsupported STT vendor %s", vendor))
		}
	}

	// Validate default vendor
	if config.STT.DefaultVendor != "" && !v.contains(config.STT.SupportedVendors, config.STT.DefaultVendor) {
		v.addError("stt_default_vendor", config.STT.DefaultVendor, "reference", "Default STT vendor must be in the list of configured vendors")
	}

	// Validate language code - skip for now as this field doesn't exist in current Config
	if false && !v.isValidLanguageCode("") {
		// Language code validation skipped for now
	}
}

// validateHTTPConfig validates HTTP server configuration
func (v *ConfigValidator) validateHTTPConfig(config *Config) {
	// Validate HTTP port
	if config.HTTP.Enabled {
		if !v.isValidPort(config.HTTP.Port) {
			v.addError("http_port", config.HTTP.Port, "range", "Invalid HTTP port")
		}

		if config.HTTP.Port == 80 && os.Getuid() != 0 {
			v.addWarning("http_port", config.HTTP.Port, "Port 80 requires root privileges", "Consider using port 8080")
		}

		// Validate timeouts
		if config.HTTP.ReadTimeout < time.Second || config.HTTP.ReadTimeout > 5*time.Minute {
			v.addError("http_read_timeout", config.HTTP.ReadTimeout, "range", "HTTP read timeout must be between 1s and 5m")
		}

		if config.HTTP.WriteTimeout < time.Second || config.HTTP.WriteTimeout > 5*time.Minute {
			v.addError("http_write_timeout", config.HTTP.WriteTimeout, "range", "HTTP write timeout must be between 1s and 5m")
		}

		if config.HTTP.TLSEnabled {
			if config.HTTP.TLSCertFile == "" {
				v.addError("http_tls_cert_file", config.HTTP.TLSCertFile, "required", "HTTP TLS certificate file is required when TLS is enabled")
			} else if !v.fileExists(config.HTTP.TLSCertFile) {
				v.addError("http_tls_cert_file", config.HTTP.TLSCertFile, "exists", "HTTP TLS certificate file does not exist")
			}

			if config.HTTP.TLSKeyFile == "" {
				v.addError("http_tls_key_file", config.HTTP.TLSKeyFile, "required", "HTTP TLS key file is required when TLS is enabled")
			} else if !v.fileExists(config.HTTP.TLSKeyFile) {
				v.addError("http_tls_key_file", config.HTTP.TLSKeyFile, "exists", "HTTP TLS key file does not exist")
			}
		}
	}
}

// validateSecurityConfig validates security configuration
func (v *ConfigValidator) validateSecurityConfig(config *Config) {
	// Validate TLS configuration
	if config.Network.EnableTLS {
		if config.Network.TLSCertFile == "" {
			v.addError("tls_cert_file", config.Network.TLSCertFile, "required", "TLS certificate file is required when TLS is enabled")
		} else if !v.fileExists(config.Network.TLSCertFile) {
			v.addError("tls_cert_file", config.Network.TLSCertFile, "exists", "TLS certificate file does not exist")
		}

		if config.Network.TLSKeyFile == "" {
			v.addError("tls_key_file", config.Network.TLSKeyFile, "required", "TLS key file is required when TLS is enabled")
		} else if !v.fileExists(config.Network.TLSKeyFile) {
			v.addError("tls_key_file", config.Network.TLSKeyFile, "exists", "TLS key file does not exist")
		}
	}

	// SRTP validation skipped - configuration handled at runtime
}

// validateStorageConfig validates storage configuration
func (v *ConfigValidator) validateStorageConfig(config *Config) {
	// Validate recording directory
	if config.Recording.Directory == "" {
		v.addError("recording_dir", config.Recording.Directory, "required", "Recording directory is required")
	} else {
		if !v.directoryExists(config.Recording.Directory) {
			v.addWarning("recording_dir", config.Recording.Directory, "Recording directory does not exist", "Directory will be created if possible")
		}

		if !v.isWritableDirectory(config.Recording.Directory) {
			v.addError("recording_dir", config.Recording.Directory, "writable", "Recording directory is not writable")
		}
	}

	// Validate cleanup settings
	if config.Recording.CleanupDays < 0 {
		v.addError("recording_cleanup_days", config.Recording.CleanupDays, "range", "Recording cleanup days cannot be negative")
	}

	if config.Recording.CleanupDays == 0 {
		v.addWarning("recording_cleanup_days", config.Recording.CleanupDays, "Automatic cleanup is disabled", "Old recordings will accumulate")
	}

	// Validate max duration
	if config.Recording.MaxDuration < time.Minute {
		v.addError("recording_max_duration", config.Recording.MaxDuration, "range", "Recording max duration must be at least 1 minute")
	}

	if config.Recording.MaxDuration > 24*time.Hour {
		v.addWarning("recording_max_duration", config.Recording.MaxDuration, "Very long max duration", "Consider shorter durations to prevent large files")
	}
}

// validatePerformanceConfig validates performance-related configuration
func (v *ConfigValidator) validatePerformanceConfig(config *Config) {
	// Validate concurrent calls limit
	if config.Resources.MaxConcurrentCalls < 1 {
		v.addError("max_concurrent_calls", config.Resources.MaxConcurrentCalls, "range", "Max concurrent calls must be at least 1")
	}

	if config.Resources.MaxConcurrentCalls > 10000 {
		v.addWarning("max_concurrent_calls", config.Resources.MaxConcurrentCalls, "Very high concurrent calls limit", "Ensure system resources can handle this load")
	}

	// Other performance validations can be added as needed
}

// validateLoggingConfig validates logging configuration
func (v *ConfigValidator) validateLoggingConfig(config *Config) {
	// Validate log level
	validLevels := []string{"trace", "debug", "info", "warn", "warning", "error", "fatal", "panic"}
	if config.Logging.Level != "" && !v.contains(validLevels, strings.ToLower(config.Logging.Level)) {
		v.addError("log_level", config.Logging.Level, "supported", "Invalid log level")
	}

	// Validate log format
	validFormats := []string{"text", "json", "logfmt"}
	if config.Logging.Format != "" && !v.contains(validFormats, strings.ToLower(config.Logging.Format)) {
		v.addError("log_format", config.Logging.Format, "supported", "Invalid log format")
	}

	// Validate log file path if specified
	if config.Logging.OutputFile != "" {
		logDir := filepath.Dir(config.Logging.OutputFile)
		if !v.directoryExists(logDir) {
			v.addWarning("log_file", config.Logging.OutputFile, "Log directory does not exist", "Directory will be created if possible")
		}
	}
}

// Helper validation functions

func (v *ConfigValidator) isValidPort(port int) bool {
	return port > 0 && port <= 65535
}

func (v *ConfigValidator) isValidIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

func (v *ConfigValidator) isValidSTUNServer(server string) bool {
	parts := strings.Split(server, ":")
	if len(parts) != 2 {
		return false
	}

	host, portStr := parts[0], parts[1]
	if host == "" {
		return false
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}

	return v.isValidPort(port)
}

func (v *ConfigValidator) isValidCodec(codec string, validCodecs []string) bool {
	return v.contains(validCodecs, strings.ToUpper(codec))
}

func (v *ConfigValidator) isValidLanguageCode(code string) bool {
	// Simple regex for language codes like "en-US", "fr-FR", etc.
	matched, _ := regexp.MatchString(`^[a-z]{2}-[A-Z]{2}$`, code)
	return matched
}

func (v *ConfigValidator) fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (v *ConfigValidator) directoryExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (v *ConfigValidator) isWritableDirectory(path string) bool {
	if path == "" {
		return false
	}

	// Try to create a temporary file to test writability
	testFile := filepath.Join(path, ".write_test")
	file, err := os.Create(testFile)
	if err != nil {
		return false
	}
	file.Close()
	os.Remove(testFile)
	return true
}

func (v *ConfigValidator) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (v *ConfigValidator) addError(field string, value interface{}, rule, message string) {
	v.errors = append(v.errors, ValidationError{
		Field:   field,
		Value:   value,
		Rule:    rule,
		Message: message,
	})
}

func (v *ConfigValidator) addWarning(field string, value interface{}, message, suggestion string) {
	v.warnings = append(v.warnings, ValidationWarning{
		Field:      field,
		Value:      value,
		Message:    message,
		Suggestion: suggestion,
	})
}

func (v *ConfigValidator) generateSummary() string {
	if len(v.errors) == 0 && len(v.warnings) == 0 {
		return "Configuration validation passed successfully"
	}

	summary := ""
	if len(v.errors) > 0 {
		summary += fmt.Sprintf("%d validation error(s)", len(v.errors))
	}

	if len(v.warnings) > 0 {
		if summary != "" {
			summary += " and "
		}
		summary += fmt.Sprintf("%d warning(s)", len(v.warnings))
	}

	return summary + " found"
}
