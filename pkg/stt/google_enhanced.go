package stt

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GoogleProviderEnhanced implements the Provider interface for Google Speech-to-Text with advanced features
type GoogleProviderEnhanced struct {
	logger *logrus.Logger
	client *speech.Client
	config *GoogleConfig

	// Connection management
	connections     map[string]*GoogleConnection
	connectionMutex sync.RWMutex
	clientOptions   []option.ClientOption

	// Callback function for transcription results
	callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})

	// Circuit breaker and retry logic
	retryConfig    *RetryConfig
	circuitBreaker *CircuitBreaker

	// Performance monitoring
	metrics *GoogleMetrics
}

// GoogleConfig holds configuration for Google Speech-to-Text provider
type GoogleConfig struct {
	// Authentication
	CredentialsFile string `json:"credentials_file"` // Path to service account JSON
	ProjectID       string `json:"project_id"`       // Google Cloud Project ID

	// Model and Language
	Model                string   `json:"model"`                 // Model selection (latest_long, latest_short, command_and_search, etc.)
	LanguageCode         string   `json:"language_code"`         // Primary language (en-US, es-ES, etc.)
	AlternativeLanguages []string `json:"alternative_languages"` // Alternative language codes

	// Audio Configuration
	Encoding          speechpb.RecognitionConfig_AudioEncoding `json:"encoding"`            // Audio encoding
	SampleRateHertz   int32                                    `json:"sample_rate_hertz"`   // Audio sample rate
	AudioChannelCount int32                                    `json:"audio_channel_count"` // Number of audio channels

	// Speech Recognition Features
	EnableSpeakerDiarization   bool  `json:"enable_speaker_diarization"`   // Speaker diarization
	DiarizationSpeakerCount    int32 `json:"diarization_speaker_count"`    // Expected number of speakers
	MinSpeakerCount            int32 `json:"min_speaker_count"`            // Minimum speaker count
	MaxSpeakerCount            int32 `json:"max_speaker_count"`            // Maximum speaker count
	EnableAutomaticPunctuation bool  `json:"enable_automatic_punctuation"` // Automatic punctuation
	EnableWordTimeOffsets      bool  `json:"enable_word_time_offsets"`     // Word-level timestamps
	EnableWordConfidence       bool  `json:"enable_word_confidence"`       // Word-level confidence
	EnableSpokenPunctuation    bool  `json:"enable_spoken_punctuation"`    // Spoken punctuation
	EnableSpokenEmojis         bool  `json:"enable_spoken_emojis"`         // Spoken emojis

	// Custom Models and Vocabulary
	UseEnhanced    bool     `json:"use_enhanced"`     // Use enhanced models (premium)
	CustomClassIDs []string `json:"custom_class_ids"` // Custom class IDs
	PhraseHints    []string `json:"phrase_hints"`     // Phrase hints for better recognition
	BoostValue     float32  `json:"boost_value"`      // Boost value for phrase hints (1-20)

	// Streaming Configuration
	InterimResults       bool          `json:"interim_results"`        // Enable interim results
	SingleUtterance      bool          `json:"single_utterance"`       // Single utterance mode
	VoiceActivityTimeout time.Duration `json:"voice_activity_timeout"` // Voice activity detection timeout

	// Advanced Features
	EnableProfanityFilter   bool                              `json:"enable_profanity_filter"`  // Profanity filtering
	MetadataFields          []string                          `json:"metadata_fields"`          // Audio metadata fields
	AdaptationPhraseSets    []string                          `json:"adaptation_phrase_sets"`   // Speech adaptation phrase sets
	TranscriptNormalization *speechpb.TranscriptNormalization `json:"transcript_normalization"` // Transcript normalization

	// Performance and Quality
	MaxAlternatives   int32         `json:"max_alternatives"`   // Maximum alternative transcriptions
	BufferSize        int           `json:"buffer_size"`        // Audio buffer size
	FlushInterval     time.Duration `json:"flush_interval"`     // Audio flush interval
	ConnectionTimeout time.Duration `json:"connection_timeout"` // gRPC connection timeout
	RequestTimeout    time.Duration `json:"request_timeout"`    // Request timeout
}

// GoogleConnection represents a gRPC streaming connection to Google Speech-to-Text
type GoogleConnection struct {
	callUUID     string
	stream       speechpb.Speech_StreamingRecognizeClient
	mutex        sync.RWMutex
	lastActivity time.Time
	active       bool
	cancel       context.CancelFunc
	audioChan    chan []byte
	logger       *logrus.Entry
	config       *GoogleConfig
	resultsChan  chan *speechpb.StreamingRecognizeResponse
}

// GoogleMetrics tracks performance metrics for Google Speech-to-Text
type GoogleMetrics struct {
	TotalRequests       int64         `json:"total_requests"`
	SuccessfulRequests  int64         `json:"successful_requests"`
	FailedRequests      int64         `json:"failed_requests"`
	TotalTranscriptions int64         `json:"total_transcriptions"`
	InterimResults      int64         `json:"interim_results"`
	FinalResults        int64         `json:"final_results"`
	AverageLatency      time.Duration `json:"average_latency"`
	AverageConfidence   float64       `json:"average_confidence"`
	ActiveConnections   int64         `json:"active_connections"`
	mutex               sync.RWMutex
}

// NewGoogleProviderEnhanced creates a new enhanced Google Speech-to-Text provider
func NewGoogleProviderEnhanced(logger *logrus.Logger) *GoogleProviderEnhanced {
	return &GoogleProviderEnhanced{
		logger:         logger,
		config:         DefaultGoogleConfig(),
		connections:    make(map[string]*GoogleConnection),
		clientOptions:  []option.ClientOption{},
		retryConfig:    DefaultRetryConfig(),
		circuitBreaker: NewCircuitBreaker(5, 30*time.Second),
		metrics:        &GoogleMetrics{},
	}
}

// NewGoogleProviderEnhancedWithConfig creates a new Google provider with custom configuration
func NewGoogleProviderEnhancedWithConfig(logger *logrus.Logger, config *GoogleConfig) *GoogleProviderEnhanced {
	provider := NewGoogleProviderEnhanced(logger)
	provider.config = config
	return provider
}

// DefaultGoogleConfig returns default configuration for Google Speech-to-Text
func DefaultGoogleConfig() *GoogleConfig {
	return &GoogleConfig{
		Model:                      "latest_long",
		LanguageCode:               "en-US",
		Encoding:                   speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:            16000,
		AudioChannelCount:          1,
		EnableSpeakerDiarization:   true,
		DiarizationSpeakerCount:    2,
		MinSpeakerCount:            1,
		MaxSpeakerCount:            6,
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets:      true,
		EnableWordConfidence:       true,
		EnableSpokenPunctuation:    false,
		EnableSpokenEmojis:         false,
		UseEnhanced:                true,
		BoostValue:                 4.0,
		InterimResults:             true,
		SingleUtterance:            false,
		VoiceActivityTimeout:       5 * time.Second,
		EnableProfanityFilter:      false,
		MaxAlternatives:            1,
		BufferSize:                 4096,
		FlushInterval:              100 * time.Millisecond,
		ConnectionTimeout:          30 * time.Second,
		RequestTimeout:             5 * time.Minute,
	}
}

// Name returns the provider name
func (p *GoogleProviderEnhanced) Name() string {
	return "google-enhanced"
}

// Initialize initializes the enhanced Google Speech-to-Text client
func (p *GoogleProviderEnhanced) Initialize() error {
	// Setup authentication
	if err := p.setupAuthentication(); err != nil {
		return fmt.Errorf("authentication setup failed: %w", err)
	}

	// Validate configuration
	if err := p.validateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create client with enhanced options
	ctx, cancel := context.WithTimeout(context.Background(), p.config.ConnectionTimeout)
	defer cancel()

	var err error
	p.client, err = speech.NewClient(ctx, p.clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create Google Speech client: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"model":           p.config.Model,
		"language":        p.config.LanguageCode,
		"diarization":     p.config.EnableSpeakerDiarization,
		"enhanced_models": p.config.UseEnhanced,
		"interim_results": p.config.InterimResults,
		"sample_rate":     p.config.SampleRateHertz,
	}).Info("Enhanced Google Speech-to-Text provider initialized successfully")

	return nil
}

// setupAuthentication configures authentication for Google Cloud
func (p *GoogleProviderEnhanced) setupAuthentication() error {
	// Check for service account file
	if p.config.CredentialsFile != "" {
		if _, err := os.Stat(p.config.CredentialsFile); err != nil {
			return fmt.Errorf("credentials file not found: %s", p.config.CredentialsFile)
		}
		p.clientOptions = append(p.clientOptions, option.WithCredentialsFile(p.config.CredentialsFile))
		p.logger.WithField("credentials_file", p.config.CredentialsFile).Info("Using service account credentials")
		return nil
	}

	// Check for environment variable
	if credFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); credFile != "" {
		if _, err := os.Stat(credFile); err != nil {
			return fmt.Errorf("credentials file from environment not found: %s", credFile)
		}
		p.logger.WithField("credentials_file", credFile).Info("Using credentials from environment")
		return nil
	}

	// Check for default credentials (in cloud environments)
	p.logger.Info("Using default Google Cloud credentials")
	return nil
}

// validateConfig validates the Google Speech-to-Text configuration
func (p *GoogleProviderEnhanced) validateConfig() error {
	if p.config.SampleRateHertz <= 0 {
		return fmt.Errorf("sample rate must be positive, got %d", p.config.SampleRateHertz)
	}

	if p.config.AudioChannelCount <= 0 {
		return fmt.Errorf("audio channel count must be positive, got %d", p.config.AudioChannelCount)
	}

	validModels := []string{"latest_long", "latest_short", "command_and_search", "phone_call", "video", "default"}
	if !containsString(validModels, p.config.Model) {
		return fmt.Errorf("invalid model: %s, valid models: %v", p.config.Model, validModels)
	}

	if p.config.EnableSpeakerDiarization {
		if p.config.MinSpeakerCount <= 0 || p.config.MaxSpeakerCount <= 0 {
			return fmt.Errorf("speaker counts must be positive for diarization")
		}
		if p.config.MinSpeakerCount > p.config.MaxSpeakerCount {
			return fmt.Errorf("min speaker count cannot exceed max speaker count")
		}
	}

	if p.config.BoostValue < 1.0 || p.config.BoostValue > 20.0 {
		return fmt.Errorf("boost value must be between 1.0 and 20.0, got %f", p.config.BoostValue)
	}

	return nil
}

// SetCallback sets the callback function for transcription results
func (p *GoogleProviderEnhanced) SetCallback(callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})) {
	p.callback = callback
}

// StreamToText streams audio data to Google Speech-to-Text using enhanced streaming
func (p *GoogleProviderEnhanced) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Check circuit breaker
	if !p.circuitBreaker.canExecute() {
		return fmt.Errorf("circuit breaker is open, requests are blocked")
	}

	// Update metrics
	p.updateMetrics(func(m *GoogleMetrics) {
		m.TotalRequests++
	})

	// Create streaming connection with retry logic
	err := p.streamWithRetry(ctx, audioStream, callUUID)
	if err != nil {
		p.circuitBreaker.recordFailure()
		p.updateMetrics(func(m *GoogleMetrics) {
			m.FailedRequests++
		})
		return err
	}

	// Record success
	p.circuitBreaker.recordSuccess()
	p.updateMetrics(func(m *GoogleMetrics) {
		m.SuccessfulRequests++
	})

	return nil
}

// streamWithRetry handles streaming with retry logic
func (p *GoogleProviderEnhanced) streamWithRetry(ctx context.Context, audioStream io.Reader, callUUID string) error {
	var lastErr error

	for attempt := 0; attempt <= p.retryConfig.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			delay := time.Duration(float64(p.retryConfig.InitialDelay) *
				powFloat(p.retryConfig.BackoffFactor, float64(attempt-1)))
			if delay > p.retryConfig.MaxDelay {
				delay = p.retryConfig.MaxDelay
			}

			p.logger.WithFields(logrus.Fields{
				"attempt":   attempt,
				"delay":     delay,
				"call_uuid": callUUID,
			}).Info("Retrying Google Speech-to-Text request")

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		lastErr = p.streamWithConnection(ctx, audioStream, callUUID)
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

// streamWithConnection handles streaming with a single connection
func (p *GoogleProviderEnhanced) streamWithConnection(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Create streaming connection
	conn, err := p.createStreamingConnection(ctx, callUUID)
	if err != nil {
		return fmt.Errorf("failed to create streaming connection: %w", err)
	}

	// Store connection
	p.connectionMutex.Lock()
	p.connections[callUUID] = conn
	p.connectionMutex.Unlock()

	// Update metrics
	p.updateMetrics(func(m *GoogleMetrics) {
		m.ActiveConnections++
	})

	// Ensure cleanup
	defer func() {
		p.connectionMutex.Lock()
		delete(p.connections, callUUID)
		p.connectionMutex.Unlock()

		p.updateMetrics(func(m *GoogleMetrics) {
			m.ActiveConnections--
		})

		conn.close()
	}()

	// Start message handlers
	go conn.handleResults(p.callback)
	go conn.handleAudioStream(ctx, audioStream)

	// Wait for completion
	return conn.waitForCompletion(ctx)
}

// createStreamingConnection creates a new streaming connection to Google Speech-to-Text
func (p *GoogleProviderEnhanced) createStreamingConnection(ctx context.Context, callUUID string) (*GoogleConnection, error) {
	// Create streaming recognize request
	stream, err := p.client.StreamingRecognize(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start streaming recognize: %w", err)
	}

	// Create context with cancel
	connCtx, cancel := context.WithCancel(ctx)
	_ = connCtx // Used for potential future context operations

	// Create connection wrapper
	conn := &GoogleConnection{
		callUUID:     callUUID,
		stream:       stream,
		lastActivity: time.Now(),
		active:       true,
		cancel:       cancel,
		audioChan:    make(chan []byte, p.config.BufferSize),
		logger:       p.logger.WithField("call_uuid", callUUID),
		config:       p.config,
		resultsChan:  make(chan *speechpb.StreamingRecognizeResponse, 10),
	}

	// Send initial configuration
	if err := conn.sendInitialConfig(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to send initial config: %w", err)
	}

	p.logger.WithField("call_uuid", callUUID).Info("Google Speech-to-Text streaming connection established")
	return conn, nil
}

// buildRecognitionConfig builds the Google Speech-to-Text recognition configuration
func (p *GoogleProviderEnhanced) buildRecognitionConfig() *speechpb.RecognitionConfig {
	config := &speechpb.RecognitionConfig{
		Encoding:                   p.config.Encoding,
		SampleRateHertz:            p.config.SampleRateHertz,
		AudioChannelCount:          p.config.AudioChannelCount,
		LanguageCode:               p.config.LanguageCode,
		MaxAlternatives:            p.config.MaxAlternatives,
		ProfanityFilter:            p.config.EnableProfanityFilter,
		EnableAutomaticPunctuation: p.config.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:      p.config.EnableWordTimeOffsets,
		EnableWordConfidence:       p.config.EnableWordConfidence,
		// EnableSpokenPunctuation:    p.config.EnableSpokenPunctuation, // Not available in this API version
		// EnableSpokenEmojis:         p.config.EnableSpokenEmojis, // Not available in this API version
		Model:       p.config.Model,
		UseEnhanced: p.config.UseEnhanced,
	}

	// Alternative languages
	if len(p.config.AlternativeLanguages) > 0 {
		config.AlternativeLanguageCodes = p.config.AlternativeLanguages
	}

	// Speaker diarization
	if p.config.EnableSpeakerDiarization {
		config.DiarizationConfig = &speechpb.SpeakerDiarizationConfig{
			EnableSpeakerDiarization: true,
			MinSpeakerCount:          p.config.MinSpeakerCount,
			MaxSpeakerCount:          p.config.MaxSpeakerCount,
		}

		if p.config.DiarizationSpeakerCount > 0 {
			config.DiarizationConfig.SpeakerTag = p.config.DiarizationSpeakerCount
		}
	}

	// Speech contexts (phrase hints)
	if len(p.config.PhraseHints) > 0 {
		config.SpeechContexts = []*speechpb.SpeechContext{
			{
				Phrases: p.config.PhraseHints,
				Boost:   p.config.BoostValue,
			},
		}
	}

	// Speech adaptation
	if len(p.config.AdaptationPhraseSets) > 0 {
		config.Adaptation = &speechpb.SpeechAdaptation{
			PhraseSets: []*speechpb.PhraseSet{},
		}
		for _, phraseSet := range p.config.AdaptationPhraseSets {
			config.Adaptation.PhraseSets = append(config.Adaptation.PhraseSets, &speechpb.PhraseSet{
				Name: phraseSet,
			})
		}
	}

	// Transcript normalization
	if p.config.TranscriptNormalization != nil {
		config.TranscriptNormalization = p.config.TranscriptNormalization
	}

	return config
}

// isRetryableError checks if an error is retryable
func (p *GoogleProviderEnhanced) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check gRPC status codes
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted:
			return true
		case codes.Internal:
			// Some internal errors are retryable
			return true
		}
	}

	// Check for network-related errors
	errorStr := err.Error()
	for _, retryableErr := range p.retryConfig.RetryableErrors {
		if containsString([]string{errorStr}, retryableErr) {
			return true
		}
	}

	return false
}

// updateMetrics safely updates provider metrics
func (p *GoogleProviderEnhanced) updateMetrics(updater func(*GoogleMetrics)) {
	p.metrics.mutex.Lock()
	defer p.metrics.mutex.Unlock()
	updater(p.metrics)
}

// GetMetrics returns current provider metrics
func (p *GoogleProviderEnhanced) GetMetrics() GoogleMetrics {
	p.metrics.mutex.RLock()
	defer p.metrics.mutex.RUnlock()
	
	// Return a copy without the mutex to avoid copying the lock
	return GoogleMetrics{
		TotalRequests:       p.metrics.TotalRequests,
		SuccessfulRequests:  p.metrics.SuccessfulRequests,
		FailedRequests:      p.metrics.FailedRequests,
		TotalTranscriptions: p.metrics.TotalTranscriptions,
		InterimResults:      p.metrics.InterimResults,
		FinalResults:        p.metrics.FinalResults,
		AverageLatency:      p.metrics.AverageLatency,
		AverageConfidence:   p.metrics.AverageConfidence,
		ActiveConnections:   p.metrics.ActiveConnections,
	}
}

// GetActiveConnections returns the number of active connections
func (p *GoogleProviderEnhanced) GetActiveConnections() int {
	p.connectionMutex.RLock()
	defer p.connectionMutex.RUnlock()
	return len(p.connections)
}

// GetConfig returns the current configuration
func (p *GoogleProviderEnhanced) GetConfig() *GoogleConfig {
	return p.config
}

// UpdateConfig updates the provider configuration
func (p *GoogleProviderEnhanced) UpdateConfig(config *GoogleConfig) error {
	if err := p.validateConfigUpdate(config); err != nil {
		return err
	}
	p.config = config
	p.logger.Info("Google Speech-to-Text configuration updated")
	return nil
}

// validateConfigUpdate validates a configuration update
func (p *GoogleProviderEnhanced) validateConfigUpdate(config *GoogleConfig) error {
	if config.SampleRateHertz <= 0 {
		return fmt.Errorf("sample rate must be positive")
	}
	if config.AudioChannelCount <= 0 {
		return fmt.Errorf("audio channel count must be positive")
	}
	return nil
}

// Shutdown gracefully shuts down the Google provider
func (p *GoogleProviderEnhanced) Shutdown(ctx context.Context) error {
	p.logger.Info("Shutting down Google Speech-to-Text provider")

	// Close all active connections
	p.connectionMutex.Lock()
	for callUUID, conn := range p.connections {
		p.logger.WithField("call_uuid", callUUID).Debug("Closing streaming connection")
		conn.close()
	}
	p.connections = make(map[string]*GoogleConnection)
	p.connectionMutex.Unlock()

	// Close client
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			p.logger.WithError(err).Warn("Error closing Google Speech client")
		}
	}

	p.logger.Info("Google Speech-to-Text provider shutdown complete")
	return nil
}

// GoogleConnection methods

// sendInitialConfig sends the initial streaming configuration
func (c *GoogleConnection) sendInitialConfig() error {
	// Build recognition config
	recognitionConfig := &speechpb.RecognitionConfig{
		Encoding:                   c.config.Encoding,
		SampleRateHertz:            c.config.SampleRateHertz,
		AudioChannelCount:          c.config.AudioChannelCount,
		LanguageCode:               c.config.LanguageCode,
		MaxAlternatives:            c.config.MaxAlternatives,
		ProfanityFilter:            c.config.EnableProfanityFilter,
		EnableAutomaticPunctuation: c.config.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:      c.config.EnableWordTimeOffsets,
		EnableWordConfidence:       c.config.EnableWordConfidence,
		// EnableSpokenPunctuation:    c.config.EnableSpokenPunctuation, // Not available in this API version
		// EnableSpokenEmojis:         c.config.EnableSpokenEmojis, // Not available in this API version
		Model:       c.config.Model,
		UseEnhanced: c.config.UseEnhanced,
	}

	// Add speaker diarization if enabled
	if c.config.EnableSpeakerDiarization {
		recognitionConfig.DiarizationConfig = &speechpb.SpeakerDiarizationConfig{
			EnableSpeakerDiarization: true,
			MinSpeakerCount:          c.config.MinSpeakerCount,
			MaxSpeakerCount:          c.config.MaxSpeakerCount,
		}
	}

	// Add phrase hints if provided
	if len(c.config.PhraseHints) > 0 {
		recognitionConfig.SpeechContexts = []*speechpb.SpeechContext{
			{
				Phrases: c.config.PhraseHints,
				Boost:   c.config.BoostValue,
			},
		}
	}

	// Create streaming config
	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config:          recognitionConfig,
		InterimResults:  c.config.InterimResults,
		SingleUtterance: c.config.SingleUtterance,
		// VoiceActivityTimeout: Use different configuration method for timeout
	}

	// Send initial request
	initialRequest := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: streamingConfig,
		},
	}

	return c.stream.Send(initialRequest)
}

// handleAudioStream processes the incoming audio stream
func (c *GoogleConnection) handleAudioStream(ctx context.Context, audioStream io.Reader) {
	defer close(c.audioChan)

	buffer := make([]byte, c.config.BufferSize)
	ticker := time.NewTicker(c.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, err := audioStream.Read(buffer)
			if err == io.EOF {
				c.logger.Debug("Audio stream ended")
				return
			}
			if err != nil {
				c.logger.WithError(err).Error("Audio stream read error")
				return
			}

			if n > 0 {
				audioData := make([]byte, n)
				copy(audioData, buffer[:n])

				select {
				case c.audioChan <- audioData:
					c.lastActivity = time.Now()
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// handleResults processes the streaming recognition results
func (c *GoogleConnection) handleResults(callback func(string, string, bool, map[string]interface{})) {
	defer c.stream.CloseSend()

	// Start goroutine to send audio data
	go func() {
		for {
			select {
			case audioData, ok := <-c.audioChan:
				if !ok {
					return
				}

				request := &speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: audioData,
					},
				}

				if err := c.stream.Send(request); err != nil {
					c.logger.WithError(err).Error("Failed to send audio data")
					return
				}
			}
		}
	}()

	// Process results
	for {
		resp, err := c.stream.Recv()
		if err == io.EOF {
			c.logger.Debug("Recognition stream ended")
			break
		}
		if err != nil {
			c.logger.WithError(err).Error("Error receiving recognition response")
			break
		}

		c.lastActivity = time.Now()

		// Process each result
		for _, result := range resp.Results {
			for _, alternative := range result.Alternatives {
				transcript := alternative.Transcript
				if transcript == "" {
					continue
				}

				// Create rich metadata
				metadata := c.buildResultMetadata(result, alternative, resp)

				c.logger.WithFields(logrus.Fields{
					"transcript": transcript,
					"is_final":   result.IsFinal,
					"confidence": alternative.Confidence,
					"words":      len(alternative.Words),
					// "speaker_tag":  result.SpeakerTag, // Not available in standard result
				}).Debug("Google Speech recognition result")

				// Call callback
				if callback != nil {
					callback(c.callUUID, transcript, result.IsFinal, metadata)
				}
			}
		}
	}
}

// buildResultMetadata creates comprehensive metadata for the recognition result
func (c *GoogleConnection) buildResultMetadata(result *speechpb.StreamingRecognitionResult, alternative *speechpb.SpeechRecognitionAlternative, response *speechpb.StreamingRecognizeResponse) map[string]interface{} {
	metadata := map[string]interface{}{
		"provider":   "google",
		"confidence": alternative.Confidence,
		"is_final":   result.IsFinal,
		// "speaker_tag":  result.SpeakerTag, // Not available in standard result
		"result_end_time": result.ResultEndTime,
		"language_code":   result.LanguageCode,
	}

	// Add word-level information
	if len(alternative.Words) > 0 {
		words := make([]map[string]interface{}, len(alternative.Words))
		for i, word := range alternative.Words {
			wordInfo := map[string]interface{}{
				"word":        word.Word,
				"confidence":  word.Confidence,
				"speaker_tag": word.SpeakerTag,
			}

			// Add timing if available
			if word.StartTime != nil {
				wordInfo["start_time"] = word.StartTime.AsDuration()
			}
			if word.EndTime != nil {
				wordInfo["end_time"] = word.EndTime.AsDuration()
			}

			words[i] = wordInfo
		}
		metadata["words"] = words
	}

	// Add response metadata
	if response.TotalBilledTime != nil {
		metadata["total_billed_time"] = response.TotalBilledTime.AsDuration()
	}

	return metadata
}

// waitForCompletion waits for the connection to complete or timeout
func (c *GoogleConnection) waitForCompletion(ctx context.Context) error {
	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("request timeout after %v", c.config.RequestTimeout)
		}
		return timeoutCtx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// close closes the streaming connection
func (c *GoogleConnection) close() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.active {
		c.active = false
		c.cancel()
		if c.stream != nil {
			c.stream.CloseSend()
		}
		c.logger.Info("Google Speech streaming connection closed")
	}
}

// Helper functions

// containsString checks if a slice contains a string
func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// powFloat calculates x^y for float64
func powFloat(x, y float64) float64 {
	result := 1.0
	for i := 0; i < int(y); i++ {
		result *= x
	}
	return result
}
