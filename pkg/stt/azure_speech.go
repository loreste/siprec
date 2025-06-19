package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// AzureSpeechProvider implements the Provider interface for Microsoft Azure Speech Services with streaming support
type AzureSpeechProvider struct {
	logger           *logrus.Logger
	httpClient       *http.Client
	accessToken      string
	tokenExpiry      time.Time
	transcriptionSvc *TranscriptionService
	config           *config.AzureSTTConfig
	mutex            sync.RWMutex
	callback         TranscriptionCallback
	callbackMutex    sync.RWMutex
	activeStreams    map[string]*AzureStreamSession
	streamsMutex     sync.RWMutex
}

// AzureStreamSession represents an active streaming session
type AzureStreamSession struct {
	callUUID    string
	conn        *websocket.Conn
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.Mutex
	lastActivity time.Time
}

// AzureStreamingResponse represents Azure Speech Services streaming response
type AzureStreamingResponse struct {
	RecognitionStatus string                 `json:"RecognitionStatus"`
	DisplayText       string                 `json:"DisplayText"`
	Offset            int64                  `json:"Offset"`
	Duration          int64                  `json:"Duration"`
	NBest             []AzureNBestResult     `json:"NBest,omitempty"`
	ResultType        string                 `json:"resultType,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// AzureNBestResult represents alternative recognition results
type AzureNBestResult struct {
	Confidence  float64 `json:"Confidence"`
	Lexical     string  `json:"Lexical"`
	ITN         string  `json:"ITN"`
	MaskedITN   string  `json:"MaskedITN"`
	Display     string  `json:"Display"`
	Words       []AzureWordDetail `json:"Words,omitempty"`
}

// AzureWordDetail provides word-level information
type AzureWordDetail struct {
	Word       string  `json:"Word"`
	Offset     int64   `json:"Offset"`
	Duration   int64   `json:"Duration"`
	Confidence float64 `json:"Confidence,omitempty"`
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

// NewAzureSpeechProvider creates a new Azure Speech Services provider with streaming support
func NewAzureSpeechProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.AzureSTTConfig) *AzureSpeechProvider {
	return &AzureSpeechProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		activeStreams: make(map[string]*AzureStreamSession),
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

// SetCallback sets the callback function for transcription results
func (p *AzureSpeechProvider) SetCallback(callback TranscriptionCallback) {
	p.callbackMutex.Lock()
	defer p.callbackMutex.Unlock()
	p.callback = callback
}

// StreamToText streams audio data to Azure Speech Services with real-time support
func (p *AzureSpeechProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	p.mutex.RLock()
	if p.config.SubscriptionKey == "" {
		p.mutex.RUnlock()
		return ErrInitializationFailed
	}
	p.mutex.RUnlock()

	logger := p.logger.WithField("call_uuid", callUUID)
	logger.Info("Starting Azure Speech Services streaming transcription")

	// Ensure we have a valid token
	if err := p.ensureValidToken(); err != nil {
		logger.WithError(err).Error("Failed to ensure valid access token")
		return fmt.Errorf("token error: %w", err)
	}

	// Try WebSocket streaming first, fallback to HTTP if needed
	if err := p.streamWithWebSocket(ctx, audioStream, callUUID); err != nil {
		logger.WithError(err).Warn("WebSocket streaming failed, falling back to HTTP")
		return p.streamWithHTTP(ctx, audioStream, callUUID)
	}

	return nil
}

// streamWithWebSocket handles real-time streaming via WebSocket
func (p *AzureSpeechProvider) streamWithWebSocket(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Create WebSocket URL
	wsURL, err := p.buildWebSocketURL()
	if err != nil {
		return fmt.Errorf("failed to build WebSocket URL: %w", err)
	}

	// Set up WebSocket headers
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+p.accessToken)
	headers.Set("X-ConnectionId", callUUID)

	// Create WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to Azure Speech WebSocket: %w", err)
	}
	defer conn.Close()

	// Create session context
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create and register session
	session := &AzureStreamSession{
		callUUID:     callUUID,
		conn:         conn,
		ctx:          sessionCtx,
		cancel:       cancel,
		lastActivity: time.Now(),
	}

	p.streamsMutex.Lock()
	p.activeStreams[callUUID] = session
	p.streamsMutex.Unlock()

	defer func() {
		p.streamsMutex.Lock()
		delete(p.activeStreams, callUUID)
		p.streamsMutex.Unlock()
	}()

	// Start response handler
	responseChan := make(chan *AzureStreamingResponse, 10)
	errorChan := make(chan error, 2)

	go p.handleWebSocketResponses(session, responseChan, errorChan)

	// Send audio configuration
	if err := p.sendAudioConfig(conn); err != nil {
		return fmt.Errorf("failed to send audio config: %w", err)
	}

	// Stream audio data
	return p.streamAudioData(sessionCtx, conn, audioStream, callUUID, errorChan)
}

// streamWithHTTP handles fallback HTTP streaming
func (p *AzureSpeechProvider) streamWithHTTP(ctx context.Context, audioStream io.Reader, callUUID string) error {
	logger := p.logger.WithField("call_uuid", callUUID)
	logger.Info("Using HTTP fallback for Azure Speech Services")

	// Read audio data in chunks for better responsiveness
	buffer := make([]byte, 4096)
	var audioData []byte

	for {
		n, err := audioStream.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read audio stream: %w", err)
		}
		audioData = append(audioData, buffer[:n]...)
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
	req.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=16000")
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

	// Close all active streaming sessions
	p.streamsMutex.Lock()
	for _, session := range p.activeStreams {
		if session.cancel != nil {
			session.cancel()
		}
		if session.conn != nil {
			session.conn.Close()
		}
	}
	p.activeStreams = make(map[string]*AzureStreamSession)
	p.streamsMutex.Unlock()

	p.accessToken = ""
	p.tokenExpiry = time.Time{}
	p.logger.Info("Azure Speech provider closed")

	return nil
}

// buildWebSocketURL constructs the WebSocket URL for real-time streaming
func (p *AzureSpeechProvider) buildWebSocketURL() (string, error) {
	baseURL := fmt.Sprintf("wss://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1", p.config.Region)
	
	params := url.Values{}
	params.Set("language", p.config.Language)
	params.Set("format", "detailed")
	params.Set("profanity", p.config.ProfanityFilter)
	
	if p.config.EnableDetailedResults {
		params.Set("wordLevelTimestamps", "true")
	}
	
	return baseURL + "?" + params.Encode(), nil
}

// sendAudioConfig sends the audio configuration to the WebSocket
func (p *AzureSpeechProvider) sendAudioConfig(conn *websocket.Conn) error {
	config := map[string]interface{}{
		"context": map[string]interface{}{
			"system": map[string]interface{}{
				"version": "1.0.0",
			},
			"os": map[string]interface{}{
				"platform": "Linux",
				"name":     "SIPREC",
				"version":  "1.0.0",
			},
		},
		"recognition": map[string]interface{}{
			"mode":      "conversation",
			"language":  p.config.Language,
			"format":    "detailed",
			"profanity": p.config.ProfanityFilter,
		},
	}
	
	return conn.WriteJSON(config)
}

// streamAudioData streams audio data through the WebSocket connection
func (p *AzureSpeechProvider) streamAudioData(ctx context.Context, conn *websocket.Conn, audioStream io.Reader, callUUID string, errorChan chan error) error {
	buffer := make([]byte, 1024)
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errorChan:
			if err != nil {
				return err
			}
		default:
			n, err := audioStream.Read(buffer)
			if err == io.EOF {
				// Send end-of-stream marker
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, []byte{}); writeErr != nil {
					return fmt.Errorf("failed to send end-of-stream: %w", writeErr)
				}
				return nil
			}
			if err != nil {
				return fmt.Errorf("failed to read audio data: %w", err)
			}
			
			if n > 0 {
				if writeErr := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); writeErr != nil {
					return fmt.Errorf("failed to send audio data: %w", writeErr)
				}
			}
		}
	}
}

// handleWebSocketResponses processes incoming WebSocket responses
func (p *AzureSpeechProvider) handleWebSocketResponses(session *AzureStreamSession, responseChan chan *AzureStreamingResponse, errorChan chan error) {
	defer close(responseChan)
	
	for {
		select {
		case <-session.ctx.Done():
			return
		default:
			var response AzureStreamingResponse
			if err := session.conn.ReadJSON(&response); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					errorChan <- fmt.Errorf("websocket read error: %w", err)
				}
				return
			}
			
			session.mutex.Lock()
			session.lastActivity = time.Now()
			session.mutex.Unlock()
			
			// Process the response
			if err := p.processStreamingResponse(&response, session.callUUID); err != nil {
				p.logger.WithError(err).Error("Failed to process streaming response")
			}
		}
	}
}

// processStreamingResponse processes a streaming response and triggers callbacks
func (p *AzureSpeechProvider) processStreamingResponse(response *AzureStreamingResponse, callUUID string) error {
	if response.RecognitionStatus != "Success" && response.RecognitionStatus != "InitialSilenceTimeout" {
		return nil // Skip non-successful responses
	}
	
	// Determine if this is a final result
	isFinal := response.ResultType == "FinalResult" || response.RecognitionStatus == "Success"
	
	// Extract transcription text
	transcription := response.DisplayText
	if transcription == "" && len(response.NBest) > 0 {
		transcription = response.NBest[0].Display
	}
	
	if transcription == "" {
		return nil // Skip empty transcriptions
	}
	
	// Calculate confidence
	confidence := 1.0
	if len(response.NBest) > 0 {
		confidence = response.NBest[0].Confidence
	}
	
	// Build metadata
	metadata := map[string]interface{}{
		"provider":           "azure-speech",
		"confidence":         confidence,
		"recognition_status": response.RecognitionStatus,
		"result_type":        response.ResultType,
		"offset":            response.Offset,
		"duration":          response.Duration,
	}
	
	// Add word-level details if available
	if len(response.NBest) > 0 && len(response.NBest[0].Words) > 0 {
		words := make([]map[string]interface{}, len(response.NBest[0].Words))
		for i, word := range response.NBest[0].Words {
			words[i] = map[string]interface{}{
				"word":       word.Word,
				"offset":     word.Offset,
				"duration":   word.Duration,
				"confidence": word.Confidence,
			}
		}
		metadata["words"] = words
	}
	
	// Trigger callback
	p.callbackMutex.RLock()
	callback := p.callback
	p.callbackMutex.RUnlock()
	
	if callback != nil {
		callback(callUUID, transcription, isFinal, metadata)
	}
	
	// Send to transcription service
	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcription, isFinal, metadata)
	}
	
	return nil
}
