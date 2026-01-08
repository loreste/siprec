package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// Package-level buffer pools for memory efficiency
var (
	// Audio buffer pool for WebSocket streaming
	audioBufferPool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 4096) // Fixed size audio buffers
		},
	}

	// Metadata buffer pool for JSON processing
	metadataBufferPool = sync.Pool{
		New: func() interface{} {
			return make(map[string]interface{}, 16) // Pre-sized metadata maps
		},
	}
)

// DeepgramProviderEnhanced implements the Provider interface for Deepgram with WebSocket streaming
type DeepgramProviderEnhanced struct {
	logger *logrus.Logger
	apiKey string
	apiURL string
	wsURL  string
	config *DeepgramConfig

	// Connection management
	connections     map[string]*DeepgramConnection
	connectionMutex sync.RWMutex
	client          *http.Client

	// Callback function for transcription results
	callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})

	// Circuit breaker and retry logic
	retryConfig    *RetryConfig
	circuitBreaker *CircuitBreaker
}

// DeepgramConfig holds configuration for Deepgram provider
type DeepgramConfig struct {
	// Model configuration
	Model    string `json:"model"`    // nova-2, nova, enhanced, base
	Language string `json:"language"` // Language code (en, es, etc.)
	Version  string `json:"version"`  // Model version
	Tier     string `json:"tier"`     // nova, enhanced, base

	// Audio processing
	Encoding   string `json:"encoding"`    // linear16, mulaw, alaw, etc.
	SampleRate int    `json:"sample_rate"` // Audio sample rate
	Channels   int    `json:"channels"`    // Number of audio channels

	// Features
	Punctuate       bool     `json:"punctuate"`        // Enable punctuation
	Diarize         bool     `json:"diarize"`          // Enable speaker diarization
	SmartFormat     bool     `json:"smart_format"`     // Enable smart formatting
	ProfanityFilter bool     `json:"profanity_filter"` // Enable profanity filtering
	Redact          []string `json:"redact"`           // PII redaction categories
	Utterances      bool     `json:"utterances"`       // Enable utterance detection
	InterimResults  bool     `json:"interim_results"`  // Enable interim results

	// Custom vocabulary and models
	Keywords    []string `json:"keywords"`     // Custom keywords
	CustomVocab []string `json:"custom_vocab"` // Custom vocabulary
	CustomModel string   `json:"custom_model"` // Custom model ID

	// Advanced features
	VAD         bool `json:"vad"`         // Voice activity detection
	Endpointing bool `json:"endpointing"` // Automatic endpointing
	Confidence  bool `json:"confidence"`  // Include confidence scores
	Timestamps  bool `json:"timestamps"`  // Include word timestamps
	Paragraphs  bool `json:"paragraphs"`  // Enable paragraph detection
	Sentences   bool `json:"sentences"`   // Enable sentence detection

	// Performance tuning
	KeepAlive     bool          `json:"keep_alive"`     // Keep WebSocket connections alive
	BufferSize    int           `json:"buffer_size"`    // Audio buffer size
	FlushInterval time.Duration `json:"flush_interval"` // How often to flush audio
}

// DeepgramConnection represents a WebSocket connection to Deepgram
type DeepgramConnection struct {
	callUUID     string
	conn         *websocket.Conn
	mutex        sync.RWMutex
	lastActivity time.Time
	active       bool
	cancel       context.CancelFunc
	audioChan    chan []byte
	logger       *logrus.Entry
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries      int           `json:"max_retries"`
	InitialDelay    time.Duration `json:"initial_delay"`
	MaxDelay        time.Duration `json:"max_delay"`
	BackoffFactor   float64       `json:"backoff_factor"`
	RetryableErrors []string      `json:"retryable_errors"`
}

// CircuitBreaker implements circuit breaker pattern
type CircuitBreaker struct {
	mutex        sync.RWMutex
	failureCount int
	lastFailTime time.Time
	state        CircuitState
	threshold    int
	timeout      time.Duration
}

type CircuitState int

const (
	Closed CircuitState = iota
	Open
	HalfOpen
)

// DeepgramWebSocketResponse defines the structure for WebSocket streaming responses
// Note: The channel field is polymorphic - it's an object for Results but an array for SpeechStarted
type DeepgramWebSocketResponse struct {
	Type         string          `json:"type"` // "Results", "UtteranceEnd", "SpeechStarted", or "Metadata"
	ChannelIndex []int           `json:"channel_index"`
	Duration     float64         `json:"duration"`
	Start        float64         `json:"start"`
	IsFinal      bool            `json:"is_final"`
	SpeechFinal  bool            `json:"speech_final"`
	Channel      json.RawMessage `json:"channel,omitempty"` // Can be object or array depending on message type
	Metadata     struct {
		RequestID string `json:"request_id"`
		ModelName string `json:"model_name"`
		ModelUUID string `json:"model_uuid"`
		ModelInfo *struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Arch    string `json:"arch"`
		} `json:"model_info,omitempty"`
	} `json:"metadata"`
	FromFinalize bool    `json:"from_finalize,omitempty"`
	Timestamp    float64 `json:"timestamp,omitempty"`     // For SpeechStarted
	LastWordEnd  float64 `json:"last_word_end,omitempty"` // For UtteranceEnd
}

// ParseChannel safely parses the channel field for Results messages
func (r *DeepgramWebSocketResponse) ParseChannel() *DeepgramChannel {
	if len(r.Channel) == 0 {
		return nil
	}
	// Check if it's an object (starts with '{')
	if r.Channel[0] == '{' {
		var ch DeepgramChannel
		if err := json.Unmarshal(r.Channel, &ch); err == nil {
			return &ch
		}
	}
	return nil
}

// DeepgramChannel represents the channel object in Results messages
type DeepgramChannel struct {
	Alternatives []DeepgramAlternative `json:"alternatives"`
}

// DeepgramAlternative represents a transcription alternative
type DeepgramAlternative struct {
	Transcript string         `json:"transcript"`
	Confidence float64        `json:"confidence"`
	Languages  []string       `json:"languages,omitempty"`
	Words      []DeepgramWord `json:"words,omitempty"`
}

// DeepgramWord represents a word with timing and confidence information
type DeepgramWord struct {
	Word           string  `json:"word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Confidence     float64 `json:"confidence"`
	Language       string  `json:"language,omitempty"`
	PunctuatedWord string  `json:"punctuated_word,omitempty"`
	Speaker        int     `json:"speaker,omitempty"`
}

// NewDeepgramProviderEnhanced creates a new enhanced Deepgram provider
func NewDeepgramProviderEnhanced(logger *logrus.Logger) *DeepgramProviderEnhanced {
	return &DeepgramProviderEnhanced{
		logger:         logger,
		apiURL:         "https://api.deepgram.com/v1/listen",
		wsURL:          "wss://api.deepgram.com/v1/listen",
		config:         DefaultDeepgramConfig(),
		connections:    make(map[string]*DeepgramConnection),
		client:         createHTTPClient(),
		retryConfig:    DefaultRetryConfig(),
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
	}
}

// NewDeepgramProviderEnhancedWithConfig creates a new Deepgram provider with custom configuration
func NewDeepgramProviderEnhancedWithConfig(logger *logrus.Logger, config *DeepgramConfig) *DeepgramProviderEnhanced {
	provider := NewDeepgramProviderEnhanced(logger)
	provider.config = config
	return provider
}

// DefaultDeepgramConfig returns default configuration for Deepgram
func DefaultDeepgramConfig() *DeepgramConfig {
	return &DeepgramConfig{
		Model:           "nova-2",
		Language:        "en",
		Version:         "latest",
		Tier:            "nova",
		Encoding:        "linear16",
		SampleRate:      16000,
		Channels:        1,
		Punctuate:       true,
		Diarize:         true,
		SmartFormat:     true,
		ProfanityFilter: false,
		Utterances:      true,
		InterimResults:  true,
		VAD:             true,
		Endpointing:     true,
		Confidence:      true,
		Timestamps:      true,
		Paragraphs:      false,
		Sentences:       true,
		KeepAlive:       true,
		BufferSize:      4096,
		FlushInterval:   100 * time.Millisecond,
	}
}

// DefaultRetryConfig returns default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:      3,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		BackoffFactor:   2.0,
		RetryableErrors: []string{"connection reset", "timeout", "temporary failure"},
	}
}

// createHTTPClient creates an optimized HTTP client with connection pooling
func createHTTPClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     Closed,
	}
}

// Name returns the provider name
func (p *DeepgramProviderEnhanced) Name() string {
	return "deepgram-enhanced"
}

// Initialize initializes the enhanced Deepgram client
func (p *DeepgramProviderEnhanced) Initialize() error {
	p.apiKey = os.Getenv("DEEPGRAM_API_KEY")
	if p.apiKey == "" {
		return fmt.Errorf("DEEPGRAM_API_KEY is not set in the environment")
	}

	// Validate configuration
	if err := p.validateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"model":           p.config.Model,
		"language":        p.config.Language,
		"diarize":         p.config.Diarize,
		"interim_results": p.config.InterimResults,
		"sample_rate":     p.config.SampleRate,
	}).Info("Enhanced Deepgram provider initialized successfully")

	return nil
}

// SetCallback sets the callback function for transcription results
func (p *DeepgramProviderEnhanced) SetCallback(callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})) {
	p.callback = callback
}

// validateConfig validates the Deepgram configuration
func (p *DeepgramProviderEnhanced) validateConfig() error {
	if p.config.SampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive, got %d", p.config.SampleRate)
	}

	if p.config.Channels <= 0 {
		return fmt.Errorf("channels must be positive, got %d", p.config.Channels)
	}

	validModels := []string{"nova-2", "nova", "enhanced", "base", "general"}
	if !contains(validModels, p.config.Model) {
		return fmt.Errorf("invalid model: %s, valid models: %v", p.config.Model, validModels)
	}

	validEncodings := []string{"linear16", "mulaw", "alaw", "flac", "opus", "mp3", "wav"}
	if !contains(validEncodings, p.config.Encoding) {
		return fmt.Errorf("invalid encoding: %s, valid encodings: %v", p.config.Encoding, validEncodings)
	}

	return nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// StreamToText streams audio data to Deepgram using WebSocket for real-time transcription
func (p *DeepgramProviderEnhanced) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Check circuit breaker
	if !p.circuitBreaker.canExecute() {
		return fmt.Errorf("circuit breaker is open, requests are blocked")
	}

	// Try WebSocket streaming first, fallback to HTTP if needed
	err := p.streamWithWebSocket(ctx, audioStream, callUUID)
	if err != nil {
		p.logger.WithError(err).Warn("WebSocket streaming failed, falling back to HTTP")

		// Update circuit breaker
		p.circuitBreaker.recordFailure()

		// Fallback to HTTP streaming with retry logic
		return p.streamWithHTTPRetry(ctx, audioStream, callUUID)
	}

	// Record success
	p.circuitBreaker.recordSuccess()
	return nil
}

// streamWithWebSocket handles WebSocket-based streaming
func (p *DeepgramProviderEnhanced) streamWithWebSocket(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Create WebSocket connection
	conn, err := p.createWebSocketConnection(ctx, callUUID)
	if err != nil {
		return fmt.Errorf("failed to create WebSocket connection: %w", err)
	}

	// Store connection
	p.connectionMutex.Lock()
	p.connections[callUUID] = conn
	p.connectionMutex.Unlock()

	// Ensure cleanup
	defer func() {
		p.connectionMutex.Lock()
		delete(p.connections, callUUID)
		p.connectionMutex.Unlock()
		conn.close()
	}()

	// Start message handler
	go conn.handleMessages(p.callback)

	// Stream audio data
	return conn.streamAudio(ctx, audioStream)
}

// streamWithHTTPRetry handles HTTP-based streaming with retry logic
func (p *DeepgramProviderEnhanced) streamWithHTTPRetry(ctx context.Context, audioStream io.Reader, callUUID string) error {
	var lastErr error

	for attempt := 0; attempt <= p.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			delay := time.Duration(float64(p.retryConfig.InitialDelay) *
				pow(p.retryConfig.BackoffFactor, float64(attempt-1)))
			if delay > p.retryConfig.MaxDelay {
				delay = p.retryConfig.MaxDelay
			}

			p.logger.WithFields(logrus.Fields{
				"attempt":   attempt,
				"delay":     delay,
				"call_uuid": callUUID,
			}).Info("Retrying Deepgram HTTP request")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = p.streamWithHTTP(ctx, audioStream, callUUID)
		if lastErr == nil {
			return nil
		}

		// Check if error is retryable
		if !p.isRetryableError(lastErr) {
			return lastErr
		}
	}

	return fmt.Errorf("max retries exceeded, last error: %w", lastErr)
}

// streamWithHTTP handles HTTP-based streaming (fallback)
func (p *DeepgramProviderEnhanced) streamWithHTTP(ctx context.Context, audioStream io.Reader, callUUID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, audioStream)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Authorization", "Token "+p.apiKey)
	req.Header.Set("Content-Type", p.getContentType())

	// Add query parameters
	query := p.buildQueryParams()
	req.URL.RawQuery = query.Encode()

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Deepgram: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		// Include descriptive error messages for common HTTP status codes
		switch resp.StatusCode {
		case http.StatusInternalServerError:
			return fmt.Errorf("deepgram API internal server error (status %d)", resp.StatusCode)
		case http.StatusBadGateway:
			return fmt.Errorf("deepgram API bad gateway (status %d)", resp.StatusCode)
		case http.StatusServiceUnavailable:
			return fmt.Errorf("deepgram API service unavailable (status %d)", resp.StatusCode)
		case http.StatusGatewayTimeout:
			return fmt.Errorf("deepgram API gateway timeout (status %d)", resp.StatusCode)
		default:
			return fmt.Errorf("deepgram API returned status %d", resp.StatusCode)
		}
	}

	// Parse response
	var deepgramResp DeepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&deepgramResp); err != nil {
		return fmt.Errorf("failed to decode Deepgram response: %w", err)
	}

	// Process response
	return p.processHTTPResponse(&deepgramResp, callUUID)
}

// createWebSocketConnection creates a new WebSocket connection to Deepgram
func (p *DeepgramProviderEnhanced) createWebSocketConnection(ctx context.Context, callUUID string) (*DeepgramConnection, error) {
	// Build WebSocket URL with parameters
	wsURL, err := url.Parse(p.wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid WebSocket URL: %w", err)
	}

	query := p.buildQueryParams()
	wsURL.RawQuery = query.Encode()

	// Create WebSocket headers
	headers := http.Header{}
	headers.Set("Authorization", "Token "+p.apiKey)

	// Create context with cancel
	connCtx, cancel := context.WithCancel(ctx)

	// Dial WebSocket
	conn, resp, err := websocket.DefaultDialer.DialContext(connCtx, wsURL.String(), headers)
	if err != nil {
		cancel()
		// Capture the HTTP response for debugging handshake failures
		if resp != nil {
			defer resp.Body.Close()
			bodyBytes := make([]byte, 1024)
			n, _ := resp.Body.Read(bodyBytes)
			p.logger.WithFields(logrus.Fields{
				"status_code":   resp.StatusCode,
				"status":        resp.Status,
				"response_body": string(bodyBytes[:n]),
				"error":         err.Error(),
			}).Error("WebSocket handshake failed")
		}
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
	}
	if resp != nil {
		resp.Body.Close()
	}

	// Create connection wrapper
	dgConn := &DeepgramConnection{
		callUUID:     callUUID,
		conn:         conn,
		lastActivity: time.Now(),
		active:       true,
		cancel:       cancel,
		audioChan:    make(chan []byte, p.config.BufferSize),
		logger:       p.logger.WithField("call_uuid", callUUID),
	}

	p.logger.WithField("call_uuid", callUUID).Info("WebSocket connection established")
	return dgConn, nil
}

// buildQueryParams builds query parameters for Deepgram API
// Note: Only parameters valid for streaming API are included
func (p *DeepgramProviderEnhanced) buildQueryParams() url.Values {
	query := url.Values{}

	// Basic parameters - model and language
	query.Set("model", p.config.Model)
	if p.config.Language != "" {
		query.Set("language", p.config.Language)
	}
	// Note: version and tier are NOT valid for streaming API

	// Audio parameters - required for streaming
	query.Set("encoding", p.config.Encoding)
	query.Set("sample_rate", fmt.Sprintf("%d", p.config.SampleRate))
	query.Set("channels", fmt.Sprintf("%d", p.config.Channels))

	// Feature parameters - verified for streaming API
	if p.config.Punctuate {
		query.Set("punctuate", "true")
	}
	if p.config.Diarize {
		query.Set("diarize", "true")
	}
	if p.config.SmartFormat {
		query.Set("smart_format", "true")
	}
	if p.config.ProfanityFilter {
		query.Set("profanity_filter", "true")
	}
	if p.config.Utterances {
		query.Set("utterances", "true")
	}
	if p.config.InterimResults {
		query.Set("interim_results", "true")
	}

	// Advanced features - verified for streaming API
	if p.config.VAD {
		query.Set("vad_events", "true")
	}
	if p.config.Endpointing {
		query.Set("endpointing", "true")
	}
	// Note: include_metadata, timestamps, paragraphs, sentences are NOT valid for streaming API

	// Redaction
	if len(p.config.Redact) > 0 {
		query.Set("redact", strings.Join(p.config.Redact, ","))
	}

	// Keywords
	if len(p.config.Keywords) > 0 {
		query.Set("keywords", strings.Join(p.config.Keywords, ","))
	}

	// Custom model overrides default model
	if p.config.CustomModel != "" {
		query.Set("model", p.config.CustomModel)
	}

	return query
}

// getContentType returns the appropriate content type for the audio encoding
func (p *DeepgramProviderEnhanced) getContentType() string {
	switch p.config.Encoding {
	case "linear16":
		return fmt.Sprintf("audio/l16;rate=%d;channels=%d", p.config.SampleRate, p.config.Channels)
	case "mulaw":
		return fmt.Sprintf("audio/basic;rate=%d", p.config.SampleRate)
	case "alaw":
		return fmt.Sprintf("audio/alaw;rate=%d", p.config.SampleRate)
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mp3"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/ogg; codecs=opus"
	default:
		// Default to linear16 format with sample rate info
		return fmt.Sprintf("audio/l16;rate=%d;channels=%d", p.config.SampleRate, p.config.Channels)
	}
}

// processHTTPResponse processes the HTTP response from Deepgram
func (p *DeepgramProviderEnhanced) processHTTPResponse(resp *DeepgramResponse, callUUID string) error {
	if len(resp.Results.Channels) == 0 || len(resp.Results.Channels[0].Alternatives) == 0 {
		return nil // No transcription available
	}

	alternative := resp.Results.Channels[0].Alternatives[0]
	transcript := alternative.Transcript

	if transcript == "" {
		return nil // Empty transcript
	}

	// Create metadata
	metadata := map[string]interface{}{
		"provider":   "deepgram",
		"confidence": alternative.Confidence,
		"request_id": resp.RequestID,
		"model":      resp.Metadata.ModelInfo,
		"duration":   resp.Metadata.Duration,
		"channels":   resp.Metadata.Channels,
		"words":      alternative.Words,
		"paragraphs": alternative.Paragraphs,
	}

	// Add utterances if available
	if len(resp.Results.Utterances) > 0 {
		metadata["utterances"] = resp.Results.Utterances
	}

	p.logger.WithFields(logrus.Fields{
		"transcript": transcript,
		"call_uuid":  callUUID,
		"confidence": alternative.Confidence,
		"words":      len(alternative.Words),
	}).Info("Transcription received from Deepgram HTTP")

	// Call callback if available
	if p.callback != nil {
		p.callback(callUUID, transcript, true, metadata)
	}

	return nil
}

// isRetryableError checks if an error is retryable
func (p *DeepgramProviderEnhanced) isRetryableError(err error) bool {
	errorStr := strings.ToLower(err.Error())
	for _, retryableErr := range p.retryConfig.RetryableErrors {
		if strings.Contains(errorStr, strings.ToLower(retryableErr)) {
			return true
		}
	}
	return false
}

// pow calculates x^y for float64
func pow(x, y float64) float64 {
	result := 1.0
	for i := 0; i < int(y); i++ {
		result *= x
	}
	return result
}

// DeepgramConnection methods

// handleMessages handles incoming WebSocket messages
func (c *DeepgramConnection) handleMessages(callback func(string, string, bool, map[string]interface{})) {
	defer c.conn.Close()

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.logger.WithError(err).Error("WebSocket read error")
			}
			return
		}

		c.mutex.Lock()
		c.lastActivity = time.Now()
		c.mutex.Unlock()

		// Parse WebSocket response
		var response DeepgramWebSocketResponse
		if err := json.Unmarshal(messageBytes, &response); err != nil {
			c.logger.WithError(err).Error("Failed to parse WebSocket response")
			continue
		}

		// Process response based on type
		switch response.Type {
		case "Results":
			c.processResults(&response, callback)
		case "UtteranceEnd":
			c.processUtteranceEnd(&response, callback)
		case "SpeechStarted":
			c.logger.Debug("Speech started detected")
		case "Metadata":
			c.logger.WithField("request_id", response.Metadata.RequestID).Debug("Received Deepgram metadata")
		default:
			c.logger.WithField("type", response.Type).Debug("Unknown response type")
		}
	}
}

// processResults processes transcription results
func (c *DeepgramConnection) processResults(response *DeepgramWebSocketResponse, callback func(string, string, bool, map[string]interface{})) {
	channel := response.ParseChannel()
	if channel == nil || len(channel.Alternatives) == 0 {
		return
	}

	alternative := channel.Alternatives[0]
	transcript := strings.TrimSpace(alternative.Transcript)

	if transcript == "" {
		return
	}

	// Create metadata
	metadata := map[string]interface{}{
		"provider":     "deepgram",
		"confidence":   alternative.Confidence,
		"request_id":   response.Metadata.RequestID,
		"model_name":   response.Metadata.ModelName,
		"model_uuid":   response.Metadata.ModelUUID,
		"duration":     response.Duration,
		"start":        response.Start,
		"speech_final": response.SpeechFinal,
		"words":        alternative.Words,
	}

	c.logger.WithFields(logrus.Fields{
		"transcript":   transcript,
		"is_final":     response.IsFinal,
		"speech_final": response.SpeechFinal,
		"confidence":   alternative.Confidence,
		"words":        len(alternative.Words),
	}).Debug("WebSocket transcription result")

	// Call callback
	if callback != nil {
		callback(c.callUUID, transcript, response.IsFinal, metadata)
	}
}

// processUtteranceEnd processes utterance end events
func (c *DeepgramConnection) processUtteranceEnd(response *DeepgramWebSocketResponse, callback func(string, string, bool, map[string]interface{})) {
	c.logger.WithFields(logrus.Fields{
		"duration": response.Duration,
		"start":    response.Start,
	}).Debug("Utterance ended")

	// Create metadata for utterance end
	metadata := map[string]interface{}{
		"provider":   "deepgram",
		"event_type": "utterance_end",
		"duration":   response.Duration,
		"start":      response.Start,
		"request_id": response.Metadata.RequestID,
	}

	// Call callback with empty transcript to indicate utterance end
	if callback != nil {
		callback(c.callUUID, "", true, metadata)
	}
}

// streamAudio streams audio data to the WebSocket connection with proper resource management
func (c *DeepgramConnection) streamAudio(ctx context.Context, audioStream io.Reader) error {
	// Create error channel for goroutine communication
	errChan := make(chan error, 2)
	done := make(chan struct{})

	// Start audio reading goroutine with proper cleanup
	go func() {
		defer func() {
			close(c.audioChan)
			close(done)
		}()

		// Get buffer from pool for memory efficiency
		buffer := audioBufferPool.Get().([]byte)
		defer audioBufferPool.Put(buffer)
		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			n, err := audioStream.Read(buffer)
			if err != nil {
				if err != io.EOF {
					c.logger.WithError(err).Error("Audio stream read error")
					errChan <- err
				}
				return
			}

			if n > 0 {
				// Reuse buffer efficiently - avoid unnecessary allocations
				audioData := make([]byte, n)
				copy(audioData, buffer[:n])

				select {
				case c.audioChan <- audioData:
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				}
			}
		}
	}()

	// Send audio data over WebSocket with proper error handling
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errChan:
			return err
		case audioData, ok := <-c.audioChan:
			if !ok {
				// Stream ended normally - send close frame
				if err := c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
					c.logger.WithError(err).Debug("Failed to send close message")
				}
				// Wait briefly for final responses to arrive before closing
				time.Sleep(100 * time.Millisecond)
				return nil
			}

			// Thread-safe write with timeout
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
				return fmt.Errorf("failed to send audio data: %w", err)
			}

			// Thread-safe activity update
			c.mutex.Lock()
			c.lastActivity = time.Now()
			c.mutex.Unlock()
		}
	}
}

// close closes the WebSocket connection with proper cleanup
func (c *DeepgramConnection) close() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.active {
		c.active = false

		// Cancel context first to stop goroutines
		if c.cancel != nil {
			c.cancel()
		}

		// Close WebSocket connection with proper close frame
		if c.conn != nil {
			c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			c.conn.Close()
		}

		// Drain and close audio channel to prevent goroutine leaks
		if c.audioChan != nil {
			go func() {
				for range c.audioChan {
					// Drain remaining data
				}
			}()
		}

		c.logger.Info("WebSocket connection closed gracefully")
	}
}

// CircuitBreaker methods

// canExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) canExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	switch cb.state {
	case Closed:
		return true
	case Open:
		return time.Since(cb.lastFailTime) >= cb.timeout
	case HalfOpen:
		return true
	default:
		return false
	}
}

// recordSuccess records a successful execution
func (cb *CircuitBreaker) recordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount = 0
	cb.state = Closed
}

// recordFailure records a failed execution
func (cb *CircuitBreaker) recordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.failureCount++
	cb.lastFailTime = time.Now()

	if cb.failureCount >= cb.threshold {
		cb.state = Open
	} else if cb.state == HalfOpen {
		cb.state = Open
	}
}

// Shutdown gracefully shuts down the Deepgram provider
func (p *DeepgramProviderEnhanced) Shutdown(ctx context.Context) error {
	p.logger.Info("Shutting down Deepgram provider")

	// Close all active connections
	p.connectionMutex.Lock()
	for callUUID, conn := range p.connections {
		p.logger.WithField("call_uuid", callUUID).Debug("Closing WebSocket connection")
		conn.close()
	}
	p.connections = make(map[string]*DeepgramConnection)
	p.connectionMutex.Unlock()

	p.logger.Info("Deepgram provider shutdown complete")
	return nil
}

// GetActiveConnections returns the number of active WebSocket connections
func (p *DeepgramProviderEnhanced) GetActiveConnections() int {
	p.connectionMutex.RLock()
	defer p.connectionMutex.RUnlock()
	return len(p.connections)
}

// GetConfig returns the current configuration
func (p *DeepgramProviderEnhanced) GetConfig() *DeepgramConfig {
	return p.config
}

// UpdateConfig updates the provider configuration
func (p *DeepgramProviderEnhanced) UpdateConfig(config *DeepgramConfig) error {
	if err := p.validateConfigUpdate(config); err != nil {
		return err
	}
	p.config = config
	p.logger.Info("Deepgram configuration updated")
	return nil
}

// validateConfigUpdate validates a configuration update
func (p *DeepgramProviderEnhanced) validateConfigUpdate(config *DeepgramConfig) error {
	if config.SampleRate <= 0 {
		return fmt.Errorf("sample rate must be positive")
	}
	if config.Channels <= 0 {
		return fmt.Errorf("channels must be positive")
	}
	return nil
}
