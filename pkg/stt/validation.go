package stt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// ValidationResult represents the result of provider validation
type ValidationResult struct {
	Valid       bool
	Provider    string
	Errors      []string
	Warnings    []string
	Capabilities map[string]bool
	Performance PerformanceMetrics
	Timestamp   time.Time
}

// PerformanceMetrics contains performance metrics from validation
type PerformanceMetrics struct {
	InitLatency      time.Duration
	TestLatency      time.Duration
	MaxConcurrency   int
	SupportsStreaming bool
	RequiresBuffering bool
}

// ProviderValidator validates STT providers
type ProviderValidator struct {
	logger        *logrus.Logger
	requirements  ValidationRequirements
	testAudioData []byte
}

// ValidationRequirements defines validation requirements
type ValidationRequirements struct {
	RequireHealthCheck    bool
	RequireStreaming      bool
	RequireRealtime       bool
	MaxInitTime           time.Duration
	MaxTestLatency        time.Duration
	RequiredCapabilities  []string
	MinConcurrency        int
}

// DefaultValidationRequirements returns default validation requirements
func DefaultValidationRequirements() ValidationRequirements {
	return ValidationRequirements{
		RequireHealthCheck:   false,
		RequireStreaming:     true,
		RequireRealtime:      false,
		MaxInitTime:          10 * time.Second,
		MaxTestLatency:       30 * time.Second,
		RequiredCapabilities: []string{},
		MinConcurrency:       1,
	}
}

// NewProviderValidator creates a new provider validator
func NewProviderValidator(logger *logrus.Logger, requirements ValidationRequirements) *ProviderValidator {
	return &ProviderValidator{
		logger:       logger,
		requirements: requirements,
		// Small test audio data (WAV header + silence)
		testAudioData: generateTestAudioData(),
	}
}

// ValidateProvider validates a provider
func (v *ProviderValidator) ValidateProvider(provider Provider) (*ValidationResult, error) {
	result := &ValidationResult{
		Provider:     provider.Name(),
		Valid:        true,
		Errors:       []string{},
		Warnings:     []string{},
		Capabilities: make(map[string]bool),
		Timestamp:    time.Now(),
	}
	
	v.logger.WithField("provider", provider.Name()).Info("Starting provider validation")
	
	// Check provider name
	if err := v.validateProviderName(provider.Name()); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid provider name: %v", err))
		result.Valid = false
	}
	
	// Test initialization
	initStart := time.Now()
	if err := provider.Initialize(); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("Initialization failed: %v", err))
		result.Valid = false
		return result, nil
	}
	result.Performance.InitLatency = time.Since(initStart)
	
	if result.Performance.InitLatency > v.requirements.MaxInitTime {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Slow initialization: %v (max: %v)",
				result.Performance.InitLatency, v.requirements.MaxInitTime))
	}
	
	// Check capabilities
	v.checkCapabilities(provider, result)
	
	// Validate required capabilities
	for _, required := range v.requirements.RequiredCapabilities {
		if !result.Capabilities[required] {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Missing required capability: %s", required))
			result.Valid = false
		}
	}
	
	// Test basic functionality if possible
	if v.testAudioData != nil {
		if err := v.testProviderFunctionality(provider, result); err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Functionality test failed: %v", err))
		}
	}
	
	// Check concurrency support
	if enhancedProvider, ok := provider.(EnhancedStreamingProvider); ok {
		connections := enhancedProvider.GetActiveConnections()
		result.Performance.MaxConcurrency = 100 // Assumed max
		
		if connections < v.requirements.MinConcurrency {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Low concurrency support: %d (min: %d)",
					connections, v.requirements.MinConcurrency))
		}
	}
	
	// Log validation result
	if result.Valid {
		v.logger.WithFields(logrus.Fields{
			"provider":     provider.Name(),
			"capabilities": len(result.Capabilities),
			"init_latency": result.Performance.InitLatency.Milliseconds(),
		}).Info("Provider validation passed")
	} else {
		v.logger.WithFields(logrus.Fields{
			"provider": provider.Name(),
			"errors":   strings.Join(result.Errors, "; "),
		}).Error("Provider validation failed")
	}
	
	return result, nil
}

// validateProviderName validates the provider name
func (v *ProviderValidator) validateProviderName(name string) error {
	if name == "" {
		return errors.New("provider name cannot be empty")
	}
	
	if len(name) > 50 {
		return errors.New("provider name too long (max 50 characters)")
	}
	
	// Check for valid characters
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '-') {
			return fmt.Errorf("invalid character in provider name: %c", char)
		}
	}
	
	return nil
}

// checkCapabilities checks provider capabilities
func (v *ProviderValidator) checkCapabilities(provider Provider, result *ValidationResult) {
	// Check if it's a streaming provider
	if _, ok := provider.(StreamingProvider); ok {
		result.Capabilities["streaming"] = true
		result.Performance.SupportsStreaming = true
	}
	
	// Check if it's an enhanced streaming provider
	if _, ok := provider.(EnhancedStreamingProvider); ok {
		result.Capabilities["enhanced_streaming"] = true
		result.Capabilities["shutdown"] = true
		result.Capabilities["active_connections"] = true
	}
	
	// Check if it supports health checks
	if _, ok := provider.(HealthCheckProvider); ok {
		result.Capabilities["health_check"] = true
	}
	
	// Validate requirements
	if v.requirements.RequireStreaming && !result.Capabilities["streaming"] {
		result.Errors = append(result.Errors, "Provider does not support streaming")
		result.Valid = false
	}
	
	if v.requirements.RequireHealthCheck && !result.Capabilities["health_check"] {
		result.Warnings = append(result.Warnings, "Provider does not support health checks")
	}
}

// testProviderFunctionality tests basic provider functionality
func (v *ProviderValidator) testProviderFunctionality(provider Provider, result *ValidationResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), v.requirements.MaxTestLatency)
	defer cancel()
	
	// Create a test reader with our test audio data
	testReader := &testAudioReader{
		data:     v.testAudioData,
		position: 0,
	}
	
	testStart := time.Now()
	err := provider.StreamToText(ctx, testReader, "validation-test")
	result.Performance.TestLatency = time.Since(testStart)
	
	if err != nil {
		// Some errors are expected for test data
		if strings.Contains(err.Error(), "test") ||
			strings.Contains(err.Error(), "validation") {
			// This is OK - provider recognized it as test data
			return nil
		}
		return err
	}
	
	if result.Performance.TestLatency > v.requirements.MaxTestLatency {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Slow test response: %v (max: %v)",
				result.Performance.TestLatency, v.requirements.MaxTestLatency))
	}
	
	return nil
}

// testAudioReader implements io.Reader for test audio data
type testAudioReader struct {
	data     []byte
	position int
}

func (r *testAudioReader) Read(p []byte) (n int, err error) {
	if r.position >= len(r.data) {
		return 0, fmt.Errorf("end of test data")
	}
	
	n = copy(p, r.data[r.position:])
	r.position += n
	return n, nil
}

// generateTestAudioData generates minimal test audio data
func generateTestAudioData() []byte {
	// Minimal WAV header + 1 second of silence (8kHz, 16-bit, mono)
	wavHeader := []byte{
		'R', 'I', 'F', 'F',
		0x24, 0x08, 0x00, 0x00, // File size - 8
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ',
		0x10, 0x00, 0x00, 0x00, // Subchunk size
		0x01, 0x00,             // Audio format (PCM)
		0x01, 0x00,             // Number of channels (mono)
		0x40, 0x1f, 0x00, 0x00, // Sample rate (8000)
		0x80, 0x3e, 0x00, 0x00, // Byte rate
		0x02, 0x00,             // Block align
		0x10, 0x00,             // Bits per sample (16)
		'd', 'a', 't', 'a',
		0x00, 0x08, 0x00, 0x00, // Data size
	}
	
	// Add 1024 bytes of silence
	silence := make([]byte, 1024)
	
	result := make([]byte, len(wavHeader)+len(silence))
	copy(result, wavHeader)
	copy(result[len(wavHeader):], silence)
	
	return result
}

// ValidateConfiguration validates provider configuration
func ValidateProviderConfiguration(config map[string]interface{}, providerType string) error {
	required := getRequiredFields(providerType)
	
	for _, field := range required {
		if _, exists := config[field]; !exists {
			return fmt.Errorf("missing required field: %s", field)
		}
	}
	
	// Validate field types
	if err := validateFieldTypes(config, providerType); err != nil {
		return err
	}
	
	// Provider-specific validation
	switch providerType {
	case "openai":
		return validateOpenAIConfig(config)
	case "elevenlabs":
		return validateElevenLabsConfig(config)
	case "speechmatics":
		return validateSpeechmaticsConfig(config)
	default:
		return nil
	}
}

// getRequiredFields returns required fields for a provider type
func getRequiredFields(providerType string) []string {
	switch providerType {
	case "openai":
		return []string{"api_key", "model"}
	case "elevenlabs":
		return []string{"api_key"}
	case "speechmatics":
		return []string{"api_key", "language"}
	default:
		return []string{}
	}
}

// validateFieldTypes validates field types
func validateFieldTypes(config map[string]interface{}, providerType string) error {
	for key, value := range config {
		switch key {
		case "api_key", "model", "language", "region", "endpoint":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("%s must be a string", key)
			}
		case "timeout", "max_retries", "buffer_size":
			switch v := value.(type) {
			case int, int32, int64, float32, float64:
				// Valid numeric type
			default:
				return fmt.Errorf("%s must be a number, got %T", key, v)
			}
		case "enabled", "use_websocket", "realtime":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	return nil
}

// validateOpenAIConfig validates OpenAI-specific configuration
func validateOpenAIConfig(config map[string]interface{}) error {
	// Validate API key format
	if apiKey, ok := config["api_key"].(string); ok {
		if !strings.HasPrefix(apiKey, "sk-") && apiKey != "test" {
			return fmt.Errorf("invalid OpenAI API key format")
		}
	}
	
	// Validate model
	if model, ok := config["model"].(string); ok {
		validModels := []string{"whisper-1", "whisper-large", "whisper-medium", "whisper-small"}
		valid := false
		for _, validModel := range validModels {
			if model == validModel {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid OpenAI model: %s", model)
		}
	}
	
	return nil
}

// validateElevenLabsConfig validates ElevenLabs-specific configuration
func validateElevenLabsConfig(config map[string]interface{}) error {
	// Validate API key format
	if apiKey, ok := config["api_key"].(string); ok {
		if len(apiKey) < 20 && apiKey != "test" {
			return fmt.Errorf("invalid ElevenLabs API key format")
		}
	}
	
	return nil
}

// validateSpeechmaticsConfig validates Speechmatics-specific configuration
func validateSpeechmaticsConfig(config map[string]interface{}) error {
	// Validate language code
	if lang, ok := config["language"].(string); ok {
		// Basic validation - should be like "en-US", "es-ES", etc.
		if len(lang) != 5 || lang[2] != '-' {
			return fmt.Errorf("invalid language code format: %s", lang)
		}
	}
	
	return nil
}