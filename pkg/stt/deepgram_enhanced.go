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
type DeepgramWebSocketResponse struct {
	Type         string  `json:"type"` // "Results" or "UtteranceEnd" or "SpeechStarted"
	ChannelIndex []int   `json:"channel_index"`
	Duration     float64 `json:"duration"`
	Start        float64 `json:"start"`
	IsFinal      bool    `json:"is_final"`
	SpeechFinal  bool    `json:"speech_final"`
	Channel      struct {
		Alternatives []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
			Words      []struct {
				Word       string  `json:"word"`
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
				Speaker    int     `json:"speaker,omitempty"`
			} `json:"words"`
		} `json:"alternatives"`
	} `json:"channel"`
	Metadata struct {
		RequestID string `json:"request_id"`
		ModelName string `json:"model_name"`
		ModelUUID string `json:"model_uuid"`
	} `json:"metadata"`
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
		return fmt.Errorf("deepgram API returned status %d", resp.StatusCode)
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
	conn, _, err := websocket.DefaultDialer.DialContext(connCtx, wsURL.String(), headers)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to dial WebSocket: %w", err)
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
func (p *DeepgramProviderEnhanced) buildQueryParams() url.Values {
	query := url.Values{}

	// Basic parameters
	query.Set("model", p.config.Model)
	query.Set("language", p.config.Language)
	query.Set("version", p.config.Version)
	query.Set("tier", p.config.Tier)

	// Audio parameters
	query.Set("encoding", p.config.Encoding)
	query.Set("sample_rate", fmt.Sprintf("%d", p.config.SampleRate))
	query.Set("channels", fmt.Sprintf("%d", p.config.Channels))

	// Feature parameters
	query.Set("punctuate", fmt.Sprintf("%t", p.config.Punctuate))
	query.Set("diarize", fmt.Sprintf("%t", p.config.Diarize))
	query.Set("smart_format", fmt.Sprintf("%t", p.config.SmartFormat))
	query.Set("profanity_filter", fmt.Sprintf("%t", p.config.ProfanityFilter))
	query.Set("utterances", fmt.Sprintf("%t", p.config.Utterances))
	query.Set("interim_results", fmt.Sprintf("%t", p.config.InterimResults))

	// Advanced features
	query.Set("vad_events", fmt.Sprintf("%t", p.config.VAD))
	query.Set("endpointing", fmt.Sprintf("%t", p.config.Endpointing))
	query.Set("include_metadata", fmt.Sprintf("%t", p.config.Confidence))
	query.Set("timestamps", fmt.Sprintf("%t", p.config.Timestamps))
	query.Set("paragraphs", fmt.Sprintf("%t", p.config.Paragraphs))
	query.Set("sentences", fmt.Sprintf("%t", p.config.Sentences))

	// Redaction
	if len(p.config.Redact) > 0 {
		query.Set("redact", strings.Join(p.config.Redact, ","))
	}

	// Keywords
	if len(p.config.Keywords) > 0 {
		query.Set("keywords", strings.Join(p.config.Keywords, ","))
	}

	// Custom model
	if p.config.CustomModel != "" {
		query.Set("model", p.config.CustomModel)
	}

	return query
}

// getContentType returns the appropriate content type for the audio encoding
func (p *DeepgramProviderEnhanced) getContentType() string {
	switch p.config.Encoding {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mp3"
	case "flac":
		return "audio/flac"
	case "opus":
		return "audio/ogg; codecs=opus"
	default:
		return "audio/wav"
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

		c.lastActivity = time.Now()

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
		default:
			c.logger.WithField("type", response.Type).Debug("Unknown response type")
		}
	}
}

// processResults processes transcription results
func (c *DeepgramConnection) processResults(response *DeepgramWebSocketResponse, callback func(string, string, bool, map[string]interface{})) {
	if len(response.Channel.Alternatives) == 0 {
		return
	}

	alternative := response.Channel.Alternatives[0]
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
