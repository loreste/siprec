package config

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

// STTValidationResult represents the result of STT configuration validation
type STTValidationResult struct {
	Provider string   `json:"provider"`
	Valid    bool     `json:"valid"`
	Enabled  bool     `json:"enabled"`
	Warnings []string `json:"warnings,omitempty"`
	Errors   []string `json:"errors,omitempty"`
}

// STTConfigValidation represents the overall STT configuration validation
type STTConfigValidation struct {
	Valid            bool                   `json:"valid"`
	DefaultProvider  string                 `json:"default_provider"`
	EnabledProviders []string               `json:"enabled_providers"`
	Results          []STTValidationResult  `json:"results"`
	Summary          map[string]interface{} `json:"summary"`
}

// ValidateSTTConfig validates the STT configuration and returns detailed results
func ValidateSTTConfig(config *STTConfig, logger *logrus.Logger) *STTConfigValidation {
	validation := &STTConfigValidation{
		Valid:           true,
		DefaultProvider: config.DefaultVendor,
		Results:         make([]STTValidationResult, 0),
		Summary:         make(map[string]interface{}),
	}

	// Validate each provider
	validation.Results = append(validation.Results, validateGoogleSTT(&config.Google))
	validation.Results = append(validation.Results, validateDeepgramSTT(&config.Deepgram))
	validation.Results = append(validation.Results, validateAzureSTT(&config.Azure))
	validation.Results = append(validation.Results, validateAmazonSTT(&config.Amazon))
	validation.Results = append(validation.Results, validateOpenAISTT(&config.OpenAI))

	// Count enabled providers and collect enabled provider names
	enabledCount := 0
	for _, result := range validation.Results {
		if result.Enabled {
			enabledCount++
			validation.EnabledProviders = append(validation.EnabledProviders, result.Provider)
		}
		if !result.Valid {
			validation.Valid = false
		}
	}

	// Validate general configuration
	generalValidation := validateGeneralSTTConfig(config, enabledCount)
	if !generalValidation.Valid {
		validation.Valid = false
	}
	validation.Results = append(validation.Results, generalValidation)

	// Generate summary
	validation.Summary["total_providers"] = len(validation.Results) - 1 // Exclude general validation
	validation.Summary["enabled_providers"] = enabledCount
	validation.Summary["default_provider"] = config.DefaultVendor
	validation.Summary["supported_vendors"] = config.SupportedVendors
	validation.Summary["supported_codecs"] = config.SupportedCodecs

	return validation
}

// validateGeneralSTTConfig validates general STT configuration
func validateGeneralSTTConfig(config *STTConfig, enabledCount int) STTValidationResult {
	result := STTValidationResult{
		Provider: "general",
		Enabled:  true,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	// Check if at least one provider is enabled
	if enabledCount == 0 {
		result.Errors = append(result.Errors, "No STT providers are enabled")
		result.Valid = false
	}

	// Check if default provider is in supported vendors
	found := false
	for _, vendor := range config.SupportedVendors {
		if vendor == config.DefaultVendor {
			found = true
			break
		}
	}
	if !found {
		result.Errors = append(result.Errors, fmt.Sprintf("Default vendor '%s' is not in supported vendors list", config.DefaultVendor))
		result.Valid = false
	}

	// Check if default provider is actually enabled
	defaultEnabled := false
	switch config.DefaultVendor {
	case "google":
		defaultEnabled = config.Google.Enabled
	case "deepgram":
		defaultEnabled = config.Deepgram.Enabled
	case "azure":
		defaultEnabled = config.Azure.Enabled
	case "amazon":
		defaultEnabled = config.Amazon.Enabled
	case "openai":
		defaultEnabled = config.OpenAI.Enabled
	}

	if !defaultEnabled {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Default provider '%s' is not enabled", config.DefaultVendor))
	}

	// Validate supported codecs
	validCodecs := []string{"PCMU", "PCMA", "G722", "G729", "GSM"}
	for _, codec := range config.SupportedCodecs {
		codecValid := false
		for _, validCodec := range validCodecs {
			if strings.EqualFold(codec, validCodec) {
				codecValid = true
				break
			}
		}
		if !codecValid {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Codec '%s' may not be supported by all providers", codec))
		}
	}

	return result
}

// validateGoogleSTT validates Google STT configuration
func validateGoogleSTT(config *GoogleSTTConfig) STTValidationResult {
	result := STTValidationResult{
		Provider: "google",
		Enabled:  config.Enabled,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	if !config.Enabled {
		return result
	}

	// Check authentication
	if config.CredentialsFile == "" && config.APIKey == "" {
		result.Errors = append(result.Errors, "Either GOOGLE_APPLICATION_CREDENTIALS or GOOGLE_STT_API_KEY must be set")
		result.Valid = false
	}

	if config.APIKey != "" && config.ProjectID == "" {
		result.Warnings = append(result.Warnings, "GOOGLE_PROJECT_ID should be set when using API key authentication")
	}

	// Validate model
	validModels := []string{"latest_long", "latest_short", "command_and_search"}
	modelValid := false
	for _, validModel := range validModels {
		if config.Model == validModel {
			modelValid = true
			break
		}
	}
	if !modelValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Model '%s' may not be supported", config.Model))
	}

	// Validate sample rate
	if config.SampleRate != 8000 && config.SampleRate != 16000 && config.SampleRate != 32000 && config.SampleRate != 48000 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Sample rate %d may not be optimal", config.SampleRate))
	}

	return result
}

// validateDeepgramSTT validates Deepgram STT configuration
func validateDeepgramSTT(config *DeepgramSTTConfig) STTValidationResult {
	result := STTValidationResult{
		Provider: "deepgram",
		Enabled:  config.Enabled,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	if !config.Enabled {
		return result
	}

	// Check API key
	if config.APIKey == "" {
		result.Errors = append(result.Errors, "DEEPGRAM_API_KEY must be set")
		result.Valid = false
	}

	// Validate model
	validModels := []string{"nova-2", "nova", "enhanced", "base"}
	modelValid := false
	for _, validModel := range validModels {
		if config.Model == validModel {
			modelValid = true
			break
		}
	}
	if !modelValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Model '%s' may not be supported", config.Model))
	}

	// Validate tier
	validTiers := []string{"nova", "enhanced", "base"}
	tierValid := false
	for _, validTier := range validTiers {
		if config.Tier == validTier {
			tierValid = true
			break
		}
	}
	if !tierValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Tier '%s' may not be supported", config.Tier))
	}

	return result
}

// validateAzureSTT validates Azure STT configuration
func validateAzureSTT(config *AzureSTTConfig) STTValidationResult {
	result := STTValidationResult{
		Provider: "azure",
		Enabled:  config.Enabled,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	if !config.Enabled {
		return result
	}

	// Check credentials
	if config.SubscriptionKey == "" {
		result.Errors = append(result.Errors, "AZURE_SPEECH_KEY must be set")
		result.Valid = false
	}

	if config.Region == "" {
		result.Errors = append(result.Errors, "AZURE_SPEECH_REGION must be set")
		result.Valid = false
	}

	// Validate profanity filter
	validFilters := []string{"masked", "removed", "raw"}
	filterValid := false
	for _, validFilter := range validFilters {
		if config.ProfanityFilter == validFilter {
			filterValid = true
			break
		}
	}
	if !filterValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Profanity filter '%s' may not be supported", config.ProfanityFilter))
	}

	return result
}

// validateAmazonSTT validates Amazon STT configuration
func validateAmazonSTT(config *AmazonSTTConfig) STTValidationResult {
	result := STTValidationResult{
		Provider: "amazon",
		Enabled:  config.Enabled,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	if !config.Enabled {
		return result
	}

	// Check credentials
	if config.AccessKeyID == "" {
		result.Errors = append(result.Errors, "AWS_ACCESS_KEY_ID must be set")
		result.Valid = false
	}

	if config.SecretAccessKey == "" {
		result.Errors = append(result.Errors, "AWS_SECRET_ACCESS_KEY must be set")
		result.Valid = false
	}

	// Validate media format
	validFormats := []string{"wav", "mp3", "mp4", "flac"}
	formatValid := false
	for _, validFormat := range validFormats {
		if config.MediaFormat == validFormat {
			formatValid = true
			break
		}
	}
	if !formatValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Media format '%s' may not be supported", config.MediaFormat))
	}

	// Validate sample rate
	if config.SampleRate != 8000 && config.SampleRate != 16000 && config.SampleRate != 22050 && config.SampleRate != 44100 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Sample rate %d may not be optimal", config.SampleRate))
	}

	return result
}

// validateOpenAISTT validates OpenAI STT configuration
func validateOpenAISTT(config *OpenAISTTConfig) STTValidationResult {
	result := STTValidationResult{
		Provider: "openai",
		Enabled:  config.Enabled,
		Valid:    true,
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	if !config.Enabled {
		return result
	}

	// Check API key
	if config.APIKey == "" {
		result.Errors = append(result.Errors, "OPENAI_API_KEY must be set")
		result.Valid = false
	}

	// Validate model
	if config.Model != "whisper-1" {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Model '%s' may not be supported", config.Model))
	}

	// Validate response format
	validFormats := []string{"json", "text", "srt", "verbose_json", "vtt"}
	formatValid := false
	for _, validFormat := range validFormats {
		if config.ResponseFormat == validFormat {
			formatValid = true
			break
		}
	}
	if !formatValid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Response format '%s' may not be supported", config.ResponseFormat))
	}

	// Validate temperature
	if config.Temperature < 0.0 || config.Temperature > 1.0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Temperature %.2f should be between 0.0 and 1.0", config.Temperature))
	}

	return result
}

// PrintSTTValidation prints a human-readable validation report
func PrintSTTValidation(validation *STTConfigValidation, logger *logrus.Logger) {
	logger.Info("=== STT Configuration Validation Report ===")
	
	// Overall status
	if validation.Valid {
		logger.Info("✓ Overall configuration is VALID")
	} else {
		logger.Error("✗ Overall configuration has ERRORS")
	}
	
	logger.Infof("Default Provider: %s", validation.DefaultProvider)
	logger.Infof("Enabled Providers: %v", validation.EnabledProviders)
	
	// Per-provider results
	for _, result := range validation.Results {
		logger.Infof("\n--- %s Provider ---", strings.ToUpper(result.Provider))
		
		if !result.Enabled && result.Provider != "general" {
			logger.Infof("Status: DISABLED")
			continue
		}
		
		if result.Valid {
			logger.Infof("Status: ✓ VALID")
		} else {
			logger.Errorf("Status: ✗ INVALID")
		}
		
		for _, warning := range result.Warnings {
			logger.Warnf("⚠ Warning: %s", warning)
		}
		
		for _, err := range result.Errors {
			logger.Errorf("✗ Error: %s", err)
		}
	}
	
	// Summary
	logger.Info("\n=== Summary ===")
	for key, value := range validation.Summary {
		logger.Infof("%s: %v", key, value)
	}
}