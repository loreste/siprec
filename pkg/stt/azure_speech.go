package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// AzureSpeechProvider implements the Provider interface for Microsoft Azure Speech Services using REST API
type AzureSpeechProvider struct {
	logger           *logrus.Logger
	httpClient       *http.Client
	accessToken      string
	tokenExpiry      time.Time
	transcriptionSvc *TranscriptionService
	config           *config.AzureSTTConfig
	mutex            sync.RWMutex
}

// AzureSpeechConfig holds configuration for Azure Speech Services
type AzureSpeechConfig struct {
	SubscriptionKey       string
	Region                string
	LanguageCode          string
	EndpointURL           string
	EnableProfanityFilter bool
	EnableDiarization     bool
	MaxSpeakers           int
	PhraseListGrammar     []string
	EnableWordTimestamps  bool
	EnableSentiment       bool
	EnableLanguageID      bool
	CandidateLanguages    []string
	TokenRefreshInterval  time.Duration
}

// AzureRecognitionResponse represents the Azure Speech API response
type AzureRecognitionResponse struct {
	RecognitionStatus string `json:"RecognitionStatus"`
	DisplayText       string `json:"DisplayText"`
	Offset            int64  `json:"Offset"`
	Duration          int64  `json:"Duration"`
	NBest             []struct {
		Confidence float64 `json:"Confidence"`
		Lexical    string  `json:"Lexical"`
		ITN        string  `json:"ITN"`
		MaskedITN  string  `json:"MaskedITN"`
		Display    string  `json:"Display"`
		Words      []struct {
			Word       string  `json:"Word"`
			Offset     int64   `json:"Offset"`
			Duration   int64   `json:"Duration"`
			Confidence float64 `json:"Confidence,omitempty"`
		} `json:"Words,omitempty"`
		Sentiment *struct {
			Negative float64 `json:"negative"`
			Neutral  float64 `json:"neutral"`
			Positive float64 `json:"positive"`
		} `json:"Sentiment,omitempty"`
	} `json:"NBest"`
	Speaker *struct {
		SpeakerID string `json:"Speaker"`
	} `json:"Speaker,omitempty"`
}

// AzureAuthResponse represents the authentication token response
type AzureAuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// DefaultAzureSpeechConfig returns default configuration for Azure Speech Services
func DefaultAzureSpeechConfig() AzureSpeechConfig {
	return AzureSpeechConfig{
		Region:                "eastus",
		LanguageCode:          "en-US",
		EnableProfanityFilter: false,
		EnableDiarization:     false,
		MaxSpeakers:           10,
		EnableWordTimestamps:  true,
		EnableSentiment:       false,
		EnableLanguageID:      false,
		TokenRefreshInterval:  9 * time.Minute, // Tokens expire in 10 minutes
	}
}

// NewAzureSpeechProvider creates a new Azure Speech Services provider
func NewAzureSpeechProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.AzureSTTConfig) *AzureSpeechProvider {
	return &AzureSpeechProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the provider name
func (p *AzureSpeechProvider) Name() string {
	return "azure-speech"
}

// Initialize initializes the Azure Speech Services client
func (p *AzureSpeechProvider) Initialize() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Get subscription key and region
	if p.config.SubscriptionKey == "" {
		return fmt.Errorf("Azure Speech subscription key is required")
	}

	if p.config.Region == "" {
		return fmt.Errorf("Azure Speech region is required")
	}

	// Get initial access token
	if err := p.refreshAccessToken(); err != nil {
		return fmt.Errorf("failed to get access token: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"region":               p.config.Region,
		"language":             p.config.Language,
		"detailed_results":     p.config.EnableDetailedResults,
		"profanity_filter":     p.config.ProfanityFilter,
		"output_format":        p.config.OutputFormat,
	}).Info("Azure Speech Services provider initialized successfully")

	return nil
}

// refreshAccessToken obtains a new access token from Azure
func (p *AzureSpeechProvider) refreshAccessToken() error {
	authURL := fmt.Sprintf("https://%s.api.cognitive.microsoft.com/sts/v1.0/issueToken", p.config.Region)

	req, err := http.NewRequest("POST", authURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Ocp-Apim-Subscription-Key", p.config.SubscriptionKey)
	req.Header.Set("Content-Length", "0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to get auth token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth request failed with status: %d", resp.StatusCode)
	}

	tokenBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read auth response: %w", err)
	}

	p.accessToken = string(tokenBytes)
	p.tokenExpiry = time.Now().Add(9 * time.Minute) // Tokens expire in 10 minutes

	p.logger.Debug("Azure Speech access token refreshed")
	return nil
}

// ensureValidToken ensures we have a valid access token
func (p *AzureSpeechProvider) ensureValidToken() error {
	if time.Now().After(p.tokenExpiry) {
		return p.refreshAccessToken()
	}
	return nil
}

// StreamToText streams audio data to Azure Speech Services
func (p *AzureSpeechProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	p.mutex.RLock()
	if p.config.SubscriptionKey == "" {
		p.mutex.RUnlock()
		return ErrInitializationFailed
	}
	p.mutex.RUnlock()

	logger := p.logger.WithField("call_uuid", callUUID)
	logger.Info("Starting Azure Speech Services transcription")

	// Ensure we have a valid token
	if err := p.ensureValidToken(); err != nil {
		logger.WithError(err).Error("Failed to ensure valid access token")
		return fmt.Errorf("token error: %w", err)
	}

	// Read all audio data into buffer
	audioData, err := io.ReadAll(audioStream)
	if err != nil {
		logger.WithError(err).Error("Failed to read audio stream")
		return fmt.Errorf("failed to read audio: %w", err)
	}

	if len(audioData) == 0 {
		logger.Warn("Empty audio data received")
		return fmt.Errorf("no audio data to process")
	}

	// Build request URL with parameters
	requestURL := p.buildRequestURL()

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewReader(audioData))
	if err != nil {
		logger.WithError(err).Error("Failed to create HTTP request")
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Bearer "+p.accessToken)
	req.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=8000")
	req.Header.Set("Accept", "application/json")

	// Send request
	resp, err := p.httpClient.Do(req)
	if err != nil {
		logger.WithError(err).Error("Failed to send request to Azure Speech")
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"response":    string(body),
		}).Error("Azure Speech API returned error")
		return fmt.Errorf("API error: %d - %s", resp.StatusCode, string(body))
	}

	// Parse response
	var azureResponse AzureRecognitionResponse
	if err := json.NewDecoder(resp.Body).Decode(&azureResponse); err != nil {
		logger.WithError(err).Error("Failed to decode Azure Speech response")
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Process recognition results
	return p.processRecognitionResponse(azureResponse, callUUID, logger)
}

// buildRequestURL constructs the request URL with parameters
func (p *AzureSpeechProvider) buildRequestURL() string {
	// Use endpoint URL from config, or create default
	baseURL := p.config.EndpointURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1", p.config.Region)
	}

	params := []string{
		"language=" + p.config.Language,
		"format=" + p.config.OutputFormat,
	}

	// Set profanity filter
	params = append(params, "profanity="+p.config.ProfanityFilter)

	// Enable detailed results if configured
	if p.config.EnableDetailedResults {
		params = append(params, "wordLevelTimestamps=true")
	}

	if len(params) > 0 {
		return baseURL + "?" + strings.Join(params, "&")
	}

	return baseURL
}

// processRecognitionResponse processes the Azure Speech recognition response
func (p *AzureSpeechProvider) processRecognitionResponse(response AzureRecognitionResponse, callUUID string, logger *logrus.Entry) error {
	// Check recognition status
	if response.RecognitionStatus != "Success" {
		logger.WithField("status", response.RecognitionStatus).Warn("Azure Speech recognition not successful")
		if response.RecognitionStatus == "NoMatch" {
			logger.Debug("No speech detected in audio")
			return nil
		}
		return fmt.Errorf("recognition failed with status: %s", response.RecognitionStatus)
	}

	// Use display text as primary transcript
	transcript := response.DisplayText
	if transcript == "" && len(response.NBest) > 0 {
		transcript = response.NBest[0].Display
	}

	if transcript == "" {
		logger.Debug("Empty transcript received from Azure Speech")
		return nil
	}

	// Create metadata
	metadata := map[string]interface{}{
		"provider":           p.Name(),
		"word_count":         len(strings.Fields(transcript)),
		"recognition_status": response.RecognitionStatus,
		"offset_ms":          response.Offset / 10000, // Convert from 100ns ticks to milliseconds
		"duration_ms":        response.Duration / 10000,
	}

	// Process NBest results for additional information
	if len(response.NBest) > 0 {
		best := response.NBest[0]
		metadata["confidence"] = best.Confidence
		metadata["lexical"] = best.Lexical
		metadata["itn"] = best.ITN
		metadata["masked_itn"] = best.MaskedITN

		// Add word-level information if available
		if len(best.Words) > 0 {
			words := make([]map[string]interface{}, 0, len(best.Words))
			for _, word := range best.Words {
				wordData := map[string]interface{}{
					"word":        word.Word,
					"offset_ms":   word.Offset / 10000,
					"duration_ms": word.Duration / 10000,
				}
				if word.Confidence > 0 {
					wordData["confidence"] = word.Confidence
				}
				words = append(words, wordData)
			}
			metadata["words"] = words
		}

		// Add sentiment if available
		if best.Sentiment != nil {
			metadata["sentiment"] = map[string]interface{}{
				"negative": best.Sentiment.Negative,
				"neutral":  best.Sentiment.Neutral,
				"positive": best.Sentiment.Positive,
			}
		}

		// Add all NBest alternatives if multiple
		if len(response.NBest) > 1 {
			alternatives := make([]map[string]interface{}, 0, len(response.NBest))
			for _, alt := range response.NBest {
				altData := map[string]interface{}{
					"confidence": alt.Confidence,
					"display":    alt.Display,
					"lexical":    alt.Lexical,
					"itn":        alt.ITN,
					"masked_itn": alt.MaskedITN,
				}
				alternatives = append(alternatives, altData)
			}
			metadata["alternatives"] = alternatives
		}
	}

	// Add speaker information if available
	if response.Speaker != nil {
		metadata["speaker_id"] = response.Speaker.SpeakerID
	}

	// Log transcription
	logger.WithFields(logrus.Fields{
		"transcript":  transcript,
		"confidence":  metadata["confidence"],
		"duration_ms": metadata["duration_ms"],
		"speaker_id":  metadata["speaker_id"],
	}).Info("Received transcription from Azure Speech")

	// Publish to transcription service
	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
	}

	return nil
}

// UpdateConfig allows runtime configuration updates
func (p *AzureSpeechProvider) UpdateConfig(cfg *config.AzureSTTConfig) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Store old config
	oldConfig := p.config
	p.config = cfg

	// If key configuration changed, refresh token
	if oldConfig.SubscriptionKey != cfg.SubscriptionKey || oldConfig.Region != cfg.Region {
		p.logger.Info("Key configuration changed, refreshing Azure Speech token")
		if err := p.refreshAccessToken(); err != nil {
			return fmt.Errorf("failed to refresh token: %w", err)
		}
	}

	p.logger.WithFields(logrus.Fields{
		"language":        cfg.Language,
		"region":          cfg.Region,
		"profanity_filter": cfg.ProfanityFilter,
	}).Info("Updated Azure Speech configuration")

	return nil
}

// GetConfig returns the current configuration
func (p *AzureSpeechProvider) GetConfig() *config.AzureSTTConfig {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.config
}


// Close gracefully closes the provider and cleans up resources
func (p *AzureSpeechProvider) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.accessToken = ""
	p.tokenExpiry = time.Time{}
	p.logger.Info("Azure Speech provider closed")

	return nil
}
