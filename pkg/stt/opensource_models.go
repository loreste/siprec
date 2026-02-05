package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// OpenSourceModelType defines the type of open-source STT model
type OpenSourceModelType string

const (
	ModelGraniteSpeech    OpenSourceModelType = "granite-speech"
	ModelCanaryQwen       OpenSourceModelType = "canary-qwen"
	ModelParakeetTDT      OpenSourceModelType = "parakeet-tdt"
	ModelWhisperTurbo     OpenSourceModelType = "whisper-turbo"
	ModelKyutaiMoshi      OpenSourceModelType = "kyutai-moshi"
	ModelCustom           OpenSourceModelType = "custom"
)

// InferenceBackend defines the inference backend type
type InferenceBackend string

const (
	BackendHTTP      InferenceBackend = "http"
	BackendWebSocket InferenceBackend = "websocket"
	BackendCLI       InferenceBackend = "cli"
	BackendTriton    InferenceBackend = "triton"
	BackendVLLM      InferenceBackend = "vllm"
	BackendTGI       InferenceBackend = "tgi"
	BackendOllama    InferenceBackend = "ollama"
)

// OpenSourceModelConfig holds configuration for open-source STT models
type OpenSourceModelConfig struct {
	// Model identification
	ModelType   OpenSourceModelType `json:"model_type"`
	ModelName   string              `json:"model_name"`
	ModelPath   string              `json:"model_path"`   // Local path or HuggingFace model ID

	// Inference backend
	Backend            InferenceBackend `json:"backend"`
	BaseURL            string           `json:"base_url"`            // API base URL
	TranscribeEndpoint string           `json:"transcribe_endpoint"` // Endpoint path for transcription (e.g., /stt/transcribe)
	WebSocketURL       string           `json:"websocket_url"`       // WebSocket endpoint for streaming

	// Multilingual support - auto-language detection and switching
	UseMultilingual          bool   `json:"use_multilingual"`
	MultilingualWebSocketURL string `json:"multilingual_websocket_url"` // e.g., ws://host:port/ws/multilingual

	// Authentication (optional for some backends)
	APIKey      string              `json:"api_key"`
	AuthHeader  string              `json:"auth_header"`  // Custom auth header name

	// Audio configuration
	SampleRate  int                 `json:"sample_rate"`
	Encoding    string              `json:"encoding"`     // pcm, wav, flac, mp3
	Channels    int                 `json:"channels"`
	Language    string              `json:"language"`

	// Model-specific options
	Options     map[string]interface{} `json:"options"`

	// Performance
	Timeout         time.Duration `json:"timeout"`
	MaxRetries      int           `json:"max_retries"`
	BatchSize       int           `json:"batch_size"`
	UseGPU          bool          `json:"use_gpu"`
	DeviceID        int           `json:"device_id"`

	// CLI-specific (for local execution)
	ExecutablePath  string        `json:"executable_path"`
	ExtraArgs       []string      `json:"extra_args"`

	// Streaming
	EnableStreaming bool          `json:"enable_streaming"`
	ChunkDuration   time.Duration `json:"chunk_duration"`
}

// LanguageState tracks language detection state for a call
type LanguageState struct {
	CurrentLanguage  string   `json:"current_language"`
	PreviousLanguage string   `json:"previous_language"`
	LanguagesUsed    []string `json:"languages_used"`
	SwitchCount      int      `json:"switch_count"`
}

// OpenSourceModelProvider implements the Provider interface for open-source STT models
type OpenSourceModelProvider struct {
	logger           *logrus.Logger
	config           *OpenSourceModelConfig
	transcriptionSvc *TranscriptionService
	httpClient       *http.Client

	// WebSocket connections for streaming
	connections     map[string]*websocket.Conn
	connectionsMu   sync.RWMutex

	// Language state tracking per call (for multilingual mode)
	languageStates  map[string]*LanguageState
	languageStatesMu sync.RWMutex

	// Callback for results
	callback        func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})
	callbackMu      sync.RWMutex

	initialized     bool
	initMu          sync.RWMutex
}

// NewOpenSourceModelProvider creates a new open-source model provider
func NewOpenSourceModelProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, config *OpenSourceModelConfig) *OpenSourceModelProvider {
	if config.Timeout == 0 {
		config.Timeout = 60 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.SampleRate == 0 {
		config.SampleRate = 16000
	}
	if config.Channels == 0 {
		config.Channels = 1
	}
	if config.Encoding == "" {
		config.Encoding = "wav"
	}
	if config.ChunkDuration == 0 {
		config.ChunkDuration = 5 * time.Second
	}

	return &OpenSourceModelProvider{
		logger:           logger,
		config:           config,
		transcriptionSvc: transcriptionSvc,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		connections:    make(map[string]*websocket.Conn),
		languageStates: make(map[string]*LanguageState),
	}
}

// Name returns the provider name
func (p *OpenSourceModelProvider) Name() string {
	return "opensource"
}

// Initialize initializes the provider
func (p *OpenSourceModelProvider) Initialize() error {
	p.initMu.Lock()
	defer p.initMu.Unlock()

	if p.initialized {
		return nil
	}

	// Validate configuration based on backend
	switch p.config.Backend {
	case BackendHTTP, BackendTriton, BackendVLLM, BackendTGI, BackendOllama:
		if p.config.BaseURL == "" {
			return fmt.Errorf("%s backend requires base_url", p.config.Backend)
		}
	case BackendWebSocket:
		if p.config.WebSocketURL == "" {
			return fmt.Errorf("websocket backend requires websocket_url")
		}
	case BackendCLI:
		if p.config.ExecutablePath == "" && p.config.ModelPath == "" {
			return fmt.Errorf("cli backend requires executable_path or model_path")
		}
	}

	// Test connectivity for HTTP-based backends
	if p.config.Backend == BackendHTTP || p.config.Backend == BackendTriton ||
		p.config.Backend == BackendVLLM || p.config.Backend == BackendTGI ||
		p.config.Backend == BackendOllama {
		if err := p.testConnection(); err != nil {
			p.logger.WithError(err).Warn("Failed to test connection, will retry on first request")
		}
	}

	p.initialized = true
	p.logger.WithFields(logrus.Fields{
		"model_type": p.config.ModelType,
		"model_name": p.config.ModelName,
		"backend":    p.config.Backend,
		"base_url":   p.config.BaseURL,
	}).Info("Open-source STT model provider initialized")

	return nil
}

// testConnection tests the backend connectivity
func (p *OpenSourceModelProvider) testConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	healthURL := p.config.BaseURL
	if !strings.HasSuffix(healthURL, "/health") && !strings.HasSuffix(healthURL, "/v1/models") {
		healthURL = strings.TrimSuffix(healthURL, "/") + "/health"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return err
	}

	if p.config.APIKey != "" {
		header := p.config.AuthHeader
		if header == "" {
			header = "Authorization"
		}
		req.Header.Set(header, "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

// StreamToText transcribes audio using the configured backend
func (p *OpenSourceModelProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	logger := p.logger.WithFields(logrus.Fields{
		"call_uuid":   callUUID,
		"model_type":  p.config.ModelType,
		"backend":     p.config.Backend,
	})

	logger.Debug("Starting transcription")

	switch p.config.Backend {
	case BackendHTTP, BackendTriton, BackendVLLM, BackendTGI, BackendOllama:
		return p.transcribeHTTP(ctx, audioStream, callUUID)
	case BackendWebSocket:
		return p.transcribeWebSocket(ctx, audioStream, callUUID)
	case BackendCLI:
		return p.transcribeCLI(ctx, audioStream, callUUID)
	default:
		return fmt.Errorf("unsupported backend: %s", p.config.Backend)
	}
}

// transcribeHTTP handles HTTP-based transcription
func (p *OpenSourceModelProvider) transcribeHTTP(ctx context.Context, audioStream io.Reader, callUUID string) error {
	logger := p.logger.WithField("call_uuid", callUUID)

	// Read audio data
	audioData, err := io.ReadAll(audioStream)
	if err != nil {
		return fmt.Errorf("failed to read audio: %w", err)
	}

	// Build request based on backend type
	var req *http.Request
	switch p.config.Backend {
	case BackendOllama:
		req, err = p.buildOllamaRequest(ctx, audioData, callUUID)
	case BackendVLLM, BackendTGI:
		req, err = p.buildOpenAICompatibleRequest(ctx, audioData, callUUID)
	case BackendTriton:
		req, err = p.buildTritonRequest(ctx, audioData, callUUID)
	default:
		req, err = p.buildGenericRequest(ctx, audioData, callUUID)
	}

	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}

	// Execute request with retries
	var resp *http.Response
	for retry := 0; retry <= p.config.MaxRetries; retry++ {
		resp, err = p.httpClient.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break
		}
		if retry < p.config.MaxRetries {
			time.Sleep(time.Duration(retry+1) * time.Second)
			logger.WithError(err).WithField("retry", retry+1).Warn("Retrying request")
		}
	}
	if err != nil {
		return fmt.Errorf("request failed after retries: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("transcription failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	return p.parseHTTPResponse(resp, callUUID)
}

// buildGenericRequest builds a generic multipart form request
func (p *OpenSourceModelProvider) buildGenericRequest(ctx context.Context, audioData []byte, callUUID string) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add audio file
	part, err := writer.CreateFormFile("audio", "audio."+p.config.Encoding)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, err
	}

	// Add model-specific parameters
	if p.config.Language != "" {
		writer.WriteField("language", p.config.Language)
	}
	if p.config.ModelName != "" {
		writer.WriteField("model", p.config.ModelName)
	}
	for key, value := range p.config.Options {
		writer.WriteField(key, fmt.Sprintf("%v", value))
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	// Use configurable endpoint, default to /stt/transcribe if not set
	endpoint := p.config.TranscribeEndpoint
	if endpoint == "" {
		endpoint = "/stt/transcribe"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	url := strings.TrimSuffix(p.config.BaseURL, "/") + endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	p.addAuthHeader(req)

	return req, nil
}

// buildOllamaRequest builds an Ollama-compatible request
func (p *OpenSourceModelProvider) buildOllamaRequest(ctx context.Context, audioData []byte, callUUID string) (*http.Request, error) {
	// Note: Ollama doesn't natively support audio for most models
	// For audio-capable models, we use the generic endpoint with multipart form
	// Future: Support Ollama's native audio models when available
	return p.buildGenericRequest(ctx, audioData, callUUID)
}

// buildOpenAICompatibleRequest builds an OpenAI-compatible request (vLLM, TGI)
func (p *OpenSourceModelProvider) buildOpenAICompatibleRequest(ctx context.Context, audioData []byte, callUUID string) (*http.Request, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", "audio."+p.config.Encoding)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, err
	}

	writer.WriteField("model", p.config.ModelName)
	if p.config.Language != "" {
		writer.WriteField("language", p.config.Language)
	}
	writer.WriteField("response_format", "json")

	if err := writer.Close(); err != nil {
		return nil, err
	}

	url := strings.TrimSuffix(p.config.BaseURL, "/") + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	p.addAuthHeader(req)

	return req, nil
}

// buildTritonRequest builds a Triton Inference Server request
func (p *OpenSourceModelProvider) buildTritonRequest(ctx context.Context, audioData []byte, callUUID string) (*http.Request, error) {
	// Triton uses a specific inference protocol
	payload := map[string]interface{}{
		"inputs": []map[string]interface{}{
			{
				"name":     "audio",
				"shape":    []int{1, len(audioData)},
				"datatype": "BYTES",
				"data":     []string{string(audioData)},
			},
		},
		"outputs": []map[string]interface{}{
			{"name": "transcription"},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/models/%s/infer", strings.TrimSuffix(p.config.BaseURL, "/"), p.config.ModelName)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	p.addAuthHeader(req)

	return req, nil
}

// addAuthHeader adds authentication header to request
func (p *OpenSourceModelProvider) addAuthHeader(req *http.Request) {
	if p.config.APIKey != "" {
		header := p.config.AuthHeader
		if header == "" {
			header = "Authorization"
		}
		if header == "Authorization" {
			req.Header.Set(header, "Bearer "+p.config.APIKey)
		} else {
			req.Header.Set(header, p.config.APIKey)
		}
	}
}

// parseHTTPResponse parses the HTTP response and triggers callbacks
func (p *OpenSourceModelProvider) parseHTTPResponse(resp *http.Response, callUUID string) error {
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract transcription from various response formats
	var transcription string
	if text, ok := result["text"].(string); ok {
		transcription = text
	} else if text, ok := result["transcription"].(string); ok {
		transcription = text
	} else if outputs, ok := result["outputs"].([]interface{}); ok && len(outputs) > 0 {
		if output, ok := outputs[0].(map[string]interface{}); ok {
			if data, ok := output["data"].([]interface{}); ok && len(data) > 0 {
				transcription = fmt.Sprintf("%v", data[0])
			}
		}
	}

	if transcription == "" {
		p.logger.WithField("response", result).Warn("No transcription found in response")
		return nil
	}

	// Trigger callback
	p.callbackMu.RLock()
	callback := p.callback
	p.callbackMu.RUnlock()

	if callback != nil {
		metadata := map[string]interface{}{
			"model_type": string(p.config.ModelType),
			"model_name": p.config.ModelName,
			"backend":    string(p.config.Backend),
		}
		callback(callUUID, transcription, true, metadata)
	}

	// Publish to transcription service
	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcription, true, map[string]interface{}{
			"model": string(p.config.ModelType),
		})
	}

	p.logger.WithFields(logrus.Fields{
		"call_uuid":     callUUID,
		"transcription": transcription[:min(50, len(transcription))],
	}).Debug("Transcription completed")

	return nil
}

// transcribeWebSocket handles WebSocket-based streaming transcription
func (p *OpenSourceModelProvider) transcribeWebSocket(ctx context.Context, audioStream io.Reader, callUUID string) error {
	logger := p.logger.WithField("call_uuid", callUUID)

	// Determine which WebSocket URL to use
	wsURL := p.config.WebSocketURL
	useMultilingual := p.config.UseMultilingual && p.config.MultilingualWebSocketURL != ""
	if useMultilingual {
		wsURL = p.config.MultilingualWebSocketURL
		logger.WithField("multilingual_url", wsURL).Info("Using multilingual WebSocket endpoint for auto-language detection")
	}

	// Initialize language state for this call if using multilingual mode
	if useMultilingual {
		p.languageStatesMu.Lock()
		p.languageStates[callUUID] = &LanguageState{
			LanguagesUsed: make([]string, 0),
		}
		p.languageStatesMu.Unlock()
	}

	// Connect to WebSocket
	headers := http.Header{}

	// Add API key - either as header or query parameter
	if p.config.APIKey != "" {
		if p.config.AuthHeader != "" {
			// Use custom header (e.g., X-API-Key)
			headers.Set(p.config.AuthHeader, p.config.APIKey)
		} else {
			// Default to Authorization: Bearer
			headers.Set("Authorization", "Bearer "+p.config.APIKey)
		}

		// Also append as query parameter for WebSocket connections that require it
		if strings.Contains(wsURL, "?") {
			wsURL = wsURL + "&api_key=" + p.config.APIKey
		} else {
			wsURL = wsURL + "?api_key=" + p.config.APIKey
		}
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	// Store connection
	p.connectionsMu.Lock()
	p.connections[callUUID] = conn
	p.connectionsMu.Unlock()

	defer func() {
		p.connectionsMu.Lock()
		delete(p.connections, callUUID)
		p.connectionsMu.Unlock()

		// Clean up language state if using multilingual mode
		if useMultilingual {
			p.languageStatesMu.Lock()
			delete(p.languageStates, callUUID)
			p.languageStatesMu.Unlock()
		}

		conn.Close()
	}()

	// Send configuration message
	configMsg := map[string]interface{}{
		"type":        "config",
		"model":       p.config.ModelName,
		"language":    p.config.Language,
		"sample_rate": p.config.SampleRate,
		"encoding":    p.config.Encoding,
	}

	// Add multilingual-specific config if enabled
	if useMultilingual {
		configMsg["multilingual"] = true
		configMsg["detect_language"] = true
		// Don't force a specific language - let Whisper auto-detect
		delete(configMsg, "language")
	}

	if err := conn.WriteJSON(configMsg); err != nil {
		return fmt.Errorf("failed to send config: %w", err)
	}

	// Start response handler
	errChan := make(chan error, 1)
	go p.handleWebSocketResponses(ctx, conn, callUUID, useMultilingual, errChan)

	// Stream audio data
	buffer := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		default:
			n, readErr := audioStream.Read(buffer)
			if readErr == io.EOF {
				// Send end-of-stream
				conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"end"}`))
				// Wait for final results
				select {
				case err := <-errChan:
					return err
				case <-time.After(10 * time.Second):
					return nil
				}
			}
			if readErr != nil {
				return fmt.Errorf("failed to read audio: %w", readErr)
			}

			if n > 0 {
				if err := conn.WriteMessage(websocket.BinaryMessage, buffer[:n]); err != nil {
					return fmt.Errorf("failed to send audio: %w", err)
				}
			}
		}
	}
}

// handleWebSocketResponses handles incoming WebSocket messages
func (p *OpenSourceModelProvider) handleWebSocketResponses(ctx context.Context, conn *websocket.Conn, callUUID string, useMultilingual bool, errChan chan error) {
	logger := p.logger.WithField("call_uuid", callUUID)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					errChan <- err
				}
				return
			}

			var response map[string]interface{}
			if err := json.Unmarshal(message, &response); err != nil {
				continue
			}

			// Extract transcription
			var transcription string
			var isFinal bool

			if text, ok := response["text"].(string); ok {
				transcription = text
			} else if text, ok := response["transcript"].(string); ok {
				transcription = text
			}

			if final, ok := response["is_final"].(bool); ok {
				isFinal = final
			} else if response["type"] == "final" {
				isFinal = true
			}

			if transcription != "" {
				// Build metadata
				metadata := map[string]interface{}{
					"model_type": string(p.config.ModelType),
				}

				// Extract language metadata from multilingual responses
				if useMultilingual {
					p.extractLanguageMetadata(callUUID, response, metadata, logger)
				}

				// Trigger callback
				p.callbackMu.RLock()
				callback := p.callback
				p.callbackMu.RUnlock()

				if callback != nil {
					callback(callUUID, transcription, isFinal, metadata)
				}

				// Publish to transcription service with language metadata
				if p.transcriptionSvc != nil {
					p.transcriptionSvc.PublishTranscription(callUUID, transcription, isFinal, metadata)
				}
			}
		}
	}
}

// extractLanguageMetadata extracts and tracks language information from multilingual responses
func (p *OpenSourceModelProvider) extractLanguageMetadata(callUUID string, response map[string]interface{}, metadata map[string]interface{}, logger *logrus.Entry) {
	// Extract language fields from the multilingual endpoint response
	if lang, ok := response["language"].(string); ok && lang != "" {
		metadata["language"] = lang
	}
	if langName, ok := response["language_name"].(string); ok && langName != "" {
		metadata["language_name"] = langName
	}
	if switched, ok := response["language_switched"].(bool); ok {
		metadata["language_switched"] = switched
	}
	if prevLang, ok := response["previous_language"].(string); ok && prevLang != "" {
		metadata["previous_language"] = prevLang
	}
	if recommendedVoice, ok := response["recommended_voice"].(string); ok && recommendedVoice != "" {
		metadata["recommended_voice"] = recommendedVoice
	}

	// Extract languages used array
	if langsUsed, ok := response["languages_used"].([]interface{}); ok {
		langs := make([]string, 0, len(langsUsed))
		for _, l := range langsUsed {
			if langStr, ok := l.(string); ok {
				langs = append(langs, langStr)
			}
		}
		if len(langs) > 0 {
			metadata["languages_used"] = langs
		}
	}

	// Update internal language state tracking
	p.languageStatesMu.Lock()
	state, exists := p.languageStates[callUUID]
	if exists && state != nil {
		if lang, ok := metadata["language"].(string); ok && lang != "" {
			if state.CurrentLanguage != "" && state.CurrentLanguage != lang {
				state.PreviousLanguage = state.CurrentLanguage
				state.SwitchCount++

				// Log language switch event
				logger.WithFields(logrus.Fields{
					"previous_language": state.PreviousLanguage,
					"new_language":      lang,
					"switch_count":      state.SwitchCount,
				}).Info("Language switch detected in conversation")
			}
			state.CurrentLanguage = lang

			// Track unique languages used
			found := false
			for _, l := range state.LanguagesUsed {
				if l == lang {
					found = true
					break
				}
			}
			if !found {
				state.LanguagesUsed = append(state.LanguagesUsed, lang)
			}
		}

		// Add cumulative stats to metadata
		metadata["total_language_switches"] = state.SwitchCount
		metadata["all_languages_used"] = state.LanguagesUsed
	}
	p.languageStatesMu.Unlock()
}

// transcribeCLI handles CLI-based transcription (local models)
func (p *OpenSourceModelProvider) transcribeCLI(ctx context.Context, audioStream io.Reader, callUUID string) error {
	logger := p.logger.WithField("call_uuid", callUUID)
	_ = logger // Suppress unused warning for now, will be used for detailed logging

	// Create temporary file for audio
	tmpFile, err := os.CreateTemp("", "audio-*."+p.config.Encoding)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write audio to temp file
	if _, err := io.Copy(tmpFile, audioStream); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write audio: %w", err)
	}
	tmpFile.Close()

	// Build command based on model type
	var cmd *exec.Cmd
	switch p.config.ModelType {
	case ModelWhisperTurbo:
		cmd = p.buildWhisperCommand(ctx, tmpFile.Name())
	case ModelGraniteSpeech, ModelCanaryQwen, ModelParakeetTDT, ModelKyutaiMoshi:
		cmd = p.buildPythonCommand(ctx, tmpFile.Name())
	default:
		cmd = p.buildGenericCommand(ctx, tmpFile.Name())
	}

	logger.WithField("command", cmd.String()).Debug("Running CLI command")

	// Execute command
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command failed: %s", string(exitErr.Stderr))
		}
		return fmt.Errorf("command execution failed: %w", err)
	}

	// Parse output
	transcription := strings.TrimSpace(string(output))
	if transcription == "" {
		return nil
	}

	// Try to parse as JSON first
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err == nil {
		if text, ok := result["text"].(string); ok {
			transcription = text
		}
	}

	// Trigger callback
	p.callbackMu.RLock()
	callback := p.callback
	p.callbackMu.RUnlock()

	if callback != nil {
		metadata := map[string]interface{}{
			"model_type": string(p.config.ModelType),
		}
		callback(callUUID, transcription, true, metadata)
	}

	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcription, true, nil)
	}

	return nil
}

// buildWhisperCommand builds command for Whisper models
func (p *OpenSourceModelProvider) buildWhisperCommand(ctx context.Context, audioFile string) *exec.Cmd {
	executable := p.config.ExecutablePath
	if executable == "" {
		executable = "whisper"
	}

	args := []string{
		audioFile,
		"--model", p.config.ModelName,
		"--output_format", "json",
	}

	if p.config.Language != "" {
		args = append(args, "--language", p.config.Language)
	}

	if p.config.UseGPU {
		args = append(args, "--device", fmt.Sprintf("cuda:%d", p.config.DeviceID))
	} else {
		args = append(args, "--device", "cpu")
	}

	args = append(args, p.config.ExtraArgs...)

	return exec.CommandContext(ctx, executable, args...)
}

// buildPythonCommand builds command for Python-based models
func (p *OpenSourceModelProvider) buildPythonCommand(ctx context.Context, audioFile string) *exec.Cmd {
	executable := p.config.ExecutablePath
	if executable == "" {
		executable = "python"
	}

	// Build Python script path or inline command
	var args []string
	if p.config.ModelPath != "" {
		args = []string{"-m", p.config.ModelPath}
	} else {
		// Use inline transcription script
		script := p.getPythonScript()
		args = []string{"-c", script}
	}

	args = append(args, "--audio", audioFile)
	args = append(args, "--model", p.config.ModelName)

	if p.config.Language != "" {
		args = append(args, "--language", p.config.Language)
	}

	if p.config.UseGPU {
		args = append(args, "--device", fmt.Sprintf("cuda:%d", p.config.DeviceID))
	}

	args = append(args, p.config.ExtraArgs...)

	return exec.CommandContext(ctx, executable, args...)
}

// buildGenericCommand builds a generic CLI command
func (p *OpenSourceModelProvider) buildGenericCommand(ctx context.Context, audioFile string) *exec.Cmd {
	executable := p.config.ExecutablePath
	args := append([]string{audioFile}, p.config.ExtraArgs...)
	return exec.CommandContext(ctx, executable, args...)
}

// getPythonScript returns an inline Python script for transcription
func (p *OpenSourceModelProvider) getPythonScript() string {
	switch p.config.ModelType {
	case ModelGraniteSpeech:
		return `
import argparse
import json
from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
import torch
import librosa

parser = argparse.ArgumentParser()
parser.add_argument('--audio', required=True)
parser.add_argument('--model', default='ibm-granite/granite-speech-3.3')
parser.add_argument('--language', default='en')
parser.add_argument('--device', default='cpu')
args = parser.parse_args()

device = args.device if 'cuda' in args.device else 'cpu'
processor = AutoProcessor.from_pretrained(args.model)
model = AutoModelForSpeechSeq2Seq.from_pretrained(args.model).to(device)

audio, sr = librosa.load(args.audio, sr=16000)
inputs = processor(audio, sampling_rate=sr, return_tensors="pt").to(device)
generated_ids = model.generate(**inputs)
transcription = processor.batch_decode(generated_ids, skip_special_tokens=True)[0]
print(json.dumps({"text": transcription}))
`
	case ModelCanaryQwen:
		return `
import argparse
import json
import nemo.collections.asr as nemo_asr

parser = argparse.ArgumentParser()
parser.add_argument('--audio', required=True)
parser.add_argument('--model', default='nvidia/canary-1b')
parser.add_argument('--language', default='en')
parser.add_argument('--device', default='cpu')
args = parser.parse_args()

model = nemo_asr.models.EncDecMultiTaskModel.from_pretrained(args.model)
transcription = model.transcribe([args.audio])[0]
print(json.dumps({"text": transcription}))
`
	case ModelParakeetTDT:
		return `
import argparse
import json
import nemo.collections.asr as nemo_asr

parser = argparse.ArgumentParser()
parser.add_argument('--audio', required=True)
parser.add_argument('--model', default='nvidia/parakeet-tdt-0.6b-v2')
parser.add_argument('--language', default='en')
parser.add_argument('--device', default='cpu')
args = parser.parse_args()

model = nemo_asr.models.ASRModel.from_pretrained(args.model)
transcription = model.transcribe([args.audio])[0]
print(json.dumps({"text": transcription}))
`
	case ModelKyutaiMoshi:
		return `
import argparse
import json
from moshi import Moshi

parser = argparse.ArgumentParser()
parser.add_argument('--audio', required=True)
parser.add_argument('--model', default='kyutai/moshi')
parser.add_argument('--language', default='en')
parser.add_argument('--device', default='cpu')
args = parser.parse_args()

model = Moshi.from_pretrained(args.model, device=args.device)
transcription = model.transcribe(args.audio)
print(json.dumps({"text": transcription}))
`
	default:
		return `
import argparse
import json
print(json.dumps({"text": "Transcription not available"}))
`
	}
}

// SetCallback sets the transcription callback
func (p *OpenSourceModelProvider) SetCallback(callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.callback = callback
}

// Close closes the provider and cleans up resources
func (p *OpenSourceModelProvider) Close() error {
	p.connectionsMu.Lock()
	defer p.connectionsMu.Unlock()

	for callUUID, conn := range p.connections {
		p.logger.WithField("call_uuid", callUUID).Debug("Closing WebSocket connection")
		conn.Close()
	}
	p.connections = make(map[string]*websocket.Conn)

	return nil
}

// GetStats returns provider statistics
func (p *OpenSourceModelProvider) GetStats() map[string]interface{} {
	p.connectionsMu.RLock()
	activeConnections := len(p.connections)
	p.connectionsMu.RUnlock()

	return map[string]interface{}{
		"model_type":         string(p.config.ModelType),
		"model_name":         p.config.ModelName,
		"backend":            string(p.config.Backend),
		"active_connections": activeConnections,
		"initialized":        p.initialized,
	}
}

// Helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
