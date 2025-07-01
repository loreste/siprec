package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeepgramProviderEnhanced_Initialize(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	tests := []struct {
		name      string
		apiKey    string
		config    *DeepgramConfig
		expectErr bool
	}{
		{
			name:      "valid initialization",
			apiKey:    "test-api-key",
			config:    DefaultDeepgramConfig(),
			expectErr: false,
		},
		{
			name:      "missing API key",
			apiKey:    "",
			config:    DefaultDeepgramConfig(),
			expectErr: true,
		},
		{
			name:   "invalid sample rate",
			apiKey: "test-api-key",
			config: &DeepgramConfig{
				SampleRate: -1,
				Channels:   1,
				Model:      "nova-2",
				Encoding:   "linear16",
			},
			expectErr: true,
		},
		{
			name:   "invalid model",
			apiKey: "test-api-key",
			config: &DeepgramConfig{
				SampleRate: 16000,
				Channels:   1,
				Model:      "invalid-model",
				Encoding:   "linear16",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set API key environment
			if tt.apiKey != "" {
				t.Setenv("DEEPGRAM_API_KEY", tt.apiKey)
			} else {
				t.Setenv("DEEPGRAM_API_KEY", "")
			}

			provider := NewDeepgramProviderEnhancedWithConfig(logger, tt.config)
			err := provider.Initialize()

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "deepgram-enhanced", provider.Name())
			}
		})
	}
}

func TestDeepgramProviderEnhanced_Configuration(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	provider := NewDeepgramProviderEnhanced(logger)

	// Test default configuration
	config := provider.GetConfig()
	assert.Equal(t, "nova-2", config.Model)
	assert.Equal(t, "en", config.Language)
	assert.Equal(t, 16000, config.SampleRate)
	assert.True(t, config.Diarize)
	assert.True(t, config.InterimResults)

	// Test configuration update
	newConfig := &DeepgramConfig{
		Model:      "enhanced",
		Language:   "es",
		SampleRate: 22050,
		Channels:   2,
		Encoding:   "linear16",
		Diarize:    false,
	}

	err := provider.UpdateConfig(newConfig)
	assert.NoError(t, err)

	updatedConfig := provider.GetConfig()
	assert.Equal(t, "enhanced", updatedConfig.Model)
	assert.Equal(t, "es", updatedConfig.Language)
	assert.Equal(t, 22050, updatedConfig.SampleRate)
	assert.False(t, updatedConfig.Diarize)

	// Test invalid configuration update
	invalidConfig := &DeepgramConfig{
		SampleRate: -1,
		Channels:   0,
	}

	err = provider.UpdateConfig(invalidConfig)
	assert.Error(t, err)
}

func TestDeepgramProviderEnhanced_HTTPStreaming(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authentication
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "Token test-api-key", auth)

		// Verify query parameters
		query := r.URL.Query()
		assert.Equal(t, "nova-2", query.Get("model"))
		assert.Equal(t, "en", query.Get("language"))
		assert.Equal(t, "true", query.Get("diarize"))

		// Return mock response
		response := DeepgramResponse{
			RequestID: "test-request-123",
			Results: struct {
				Channels []struct {
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
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					} `json:"alternatives"`
				} `json:"channels"`
				Utterances []struct {
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
					Channel    int     `json:"channel"`
					Transcript string  `json:"transcript"`
					Words      []struct {
						Word       string  `json:"word"`
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
						Speaker    int     `json:"speaker,omitempty"`
					} `json:"words"`
					Speaker int `json:"speaker,omitempty"`
				} `json:"utterances"`
			}{
				Channels: []struct {
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
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					} `json:"alternatives"`
				}{{
					Alternatives: []struct {
						Transcript string  `json:"transcript"`
						Confidence float64 `json:"confidence"`
						Words      []struct {
							Word       string  `json:"word"`
							Start      float64 `json:"start"`
							End        float64 `json:"end"`
							Confidence float64 `json:"confidence"`
							Speaker    int     `json:"speaker,omitempty"`
						} `json:"words"`
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					}{{
						Transcript: "Hello, this is a test transcription.",
						Confidence: 0.95,
						Words: []struct {
							Word       string  `json:"word"`
							Start      float64 `json:"start"`
							End        float64 `json:"end"`
							Confidence float64 `json:"confidence"`
							Speaker    int     `json:"speaker,omitempty"`
						}{
							{Word: "Hello", Start: 0.0, End: 0.5, Confidence: 0.99, Speaker: 1},
							{Word: "this", Start: 0.6, End: 0.8, Confidence: 0.95, Speaker: 1},
							{Word: "is", Start: 0.9, End: 1.0, Confidence: 0.98, Speaker: 1},
							{Word: "a", Start: 1.1, End: 1.2, Confidence: 0.97, Speaker: 1},
							{Word: "test", Start: 1.3, End: 1.7, Confidence: 0.96, Speaker: 1},
							{Word: "transcription", Start: 1.8, End: 2.5, Confidence: 0.94, Speaker: 1},
						},
					}},
				}},
			},
			Metadata: struct {
				RequestID      string                 `json:"request_id"`
				TransactionKey string                 `json:"transaction_key"`
				SHA256         string                 `json:"sha256"`
				Created        string                 `json:"created"`
				Duration       float64                `json:"duration"`
				Channels       int                    `json:"channels"`
				Models         []string               `json:"models"`
				ModelInfo      map[string]interface{} `json:"model_info"`
				// Language detection fields for enhanced accent detection
				Language           string  `json:"language,omitempty"`
				LanguageConfidence float64 `json:"language_confidence,omitempty"`
				DetectedLanguages  []struct {
					Language   string  `json:"language"`
					Confidence float64 `json:"confidence"`
				} `json:"detected_languages,omitempty"`
			}{
				RequestID: "test-request-123",
				Duration:  2.5,
				Channels:  1,
				ModelInfo: map[string]interface{}{"name": "nova-2", "version": "1.0"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set up provider
	t.Setenv("DEEPGRAM_API_KEY", "test-api-key")
	provider := NewDeepgramProviderEnhanced(logger)
	provider.apiURL = server.URL // Override with test server URL

	err := provider.Initialize()
	require.NoError(t, err)

	// Set up callback to capture results
	var receivedTranscription string
	var receivedMetadata map[string]interface{}
	var callbackWG sync.WaitGroup
	callbackWG.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		receivedTranscription = transcription
		receivedMetadata = metadata
		assert.True(t, isFinal)
		assert.Equal(t, "test-call-123", callUUID)
		callbackWG.Done()
	})

	// Create audio stream (mock)
	audioData := strings.NewReader("mock audio data")

	// Test streaming
	ctx := context.Background()
	err = provider.StreamToText(ctx, audioData, "test-call-123")
	assert.NoError(t, err)

	// Wait for callback
	callbackWG.Wait()

	// Verify results
	assert.Equal(t, "Hello, this is a test transcription.", receivedTranscription)
	assert.NotNil(t, receivedMetadata)
	assert.Equal(t, "deepgram", receivedMetadata["provider"])
	assert.Equal(t, 0.95, receivedMetadata["confidence"])
	assert.Equal(t, "test-request-123", receivedMetadata["request_id"])
}

func TestDeepgramProviderEnhanced_WebSocketStreaming(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create mock WebSocket server
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify authentication
		auth := r.Header.Get("Authorization")
		assert.Equal(t, "Token test-api-key", auth)

		// Upgrade to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Send mock interim result
		interimResponse := DeepgramWebSocketResponse{
			Type:     "Results",
			IsFinal:  false,
			Duration: 1.0,
			Start:    0.0,
			Channel: struct {
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
			}{
				Alternatives: []struct {
					Transcript string  `json:"transcript"`
					Confidence float64 `json:"confidence"`
					Words      []struct {
						Word       string  `json:"word"`
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
						Speaker    int     `json:"speaker,omitempty"`
					} `json:"words"`
				}{{
					Transcript: "Hello",
					Confidence: 0.85,
				}},
			},
			Metadata: struct {
				RequestID string `json:"request_id"`
				ModelName string `json:"model_name"`
				ModelUUID string `json:"model_uuid"`
			}{
				RequestID: "ws-test-123",
				ModelName: "nova-2",
			},
		}

		// Send interim result
		time.Sleep(100 * time.Millisecond)
		if err := conn.WriteJSON(interimResponse); err != nil {
			t.Errorf("Failed to send interim result: %v", err)
			return
		}

		// Send final result
		finalResponse := interimResponse
		finalResponse.IsFinal = true
		finalResponse.SpeechFinal = true
		finalResponse.Channel.Alternatives[0].Transcript = "Hello, how are you today?"
		finalResponse.Channel.Alternatives[0].Confidence = 0.95

		time.Sleep(100 * time.Millisecond)
		if err := conn.WriteJSON(finalResponse); err != nil {
			t.Errorf("Failed to send final result: %v", err)
			return
		}

		// Send utterance end
		utteranceEnd := DeepgramWebSocketResponse{
			Type:     "UtteranceEnd",
			Duration: 2.5,
			Start:    0.0,
			Metadata: struct {
				RequestID string `json:"request_id"`
				ModelName string `json:"model_name"`
				ModelUUID string `json:"model_uuid"`
			}{
				RequestID: "ws-test-123",
			},
		}

		time.Sleep(100 * time.Millisecond)
		if err := conn.WriteJSON(utteranceEnd); err != nil {
			t.Errorf("Failed to send utterance end: %v", err)
			return
		}

		// Read audio data (but don't process it in this test)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	// Convert HTTP URL to WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Set up provider
	t.Setenv("DEEPGRAM_API_KEY", "test-api-key")
	provider := NewDeepgramProviderEnhanced(logger)
	provider.wsURL = wsURL // Override with test server URL

	err := provider.Initialize()
	require.NoError(t, err)

	// Set up callback to capture results
	var results []struct {
		transcription string
		isFinal       bool
		metadata      map[string]interface{}
	}
	var resultsMutex sync.Mutex
	var callbackWG sync.WaitGroup

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		resultsMutex.Lock()
		results = append(results, struct {
			transcription string
			isFinal       bool
			metadata      map[string]interface{}
		}{transcription, isFinal, metadata})
		resultsMutex.Unlock()

		assert.Equal(t, "test-ws-call", callUUID)
		callbackWG.Done()
	})

	// Expect interim, final, and utterance end callbacks
	callbackWG.Add(3)

	// Create audio stream
	audioData := strings.NewReader("mock audio data for websocket")

	// Test WebSocket streaming
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = provider.streamWithWebSocket(ctx, audioData, "test-ws-call")
	assert.NoError(t, err)

	// Wait for callbacks
	callbackWG.Wait()

	// Verify results
	resultsMutex.Lock()
	assert.Len(t, results, 3)

	// Check interim result
	assert.Equal(t, "Hello", results[0].transcription)
	assert.False(t, results[0].isFinal)
	assert.Equal(t, "deepgram", results[0].metadata["provider"])

	// Check final result
	assert.Equal(t, "Hello, how are you today?", results[1].transcription)
	assert.True(t, results[1].isFinal)
	assert.Equal(t, 0.95, results[1].metadata["confidence"])

	// Check utterance end
	assert.Equal(t, "", results[2].transcription)
	assert.True(t, results[2].isFinal)
	assert.Equal(t, "utterance_end", results[2].metadata["event_type"])
	resultsMutex.Unlock()

	// Verify connection count
	assert.Equal(t, 0, provider.GetActiveConnections()) // Should be 0 after cleanup
}

func TestDeepgramProviderEnhanced_RetryLogic(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create server that fails initially then succeeds
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Simulate temporary failure
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("temporary failure"))
			return
		}

		// Success response
		response := DeepgramResponse{
			RequestID: "retry-test-123",
			Results: struct {
				Channels []struct {
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
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					} `json:"alternatives"`
				} `json:"channels"`
				Utterances []struct {
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
					Channel    int     `json:"channel"`
					Transcript string  `json:"transcript"`
					Words      []struct {
						Word       string  `json:"word"`
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
						Speaker    int     `json:"speaker,omitempty"`
					} `json:"words"`
					Speaker int `json:"speaker,omitempty"`
				} `json:"utterances"`
			}{
				Channels: []struct {
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
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					} `json:"alternatives"`
				}{{
					Alternatives: []struct {
						Transcript string  `json:"transcript"`
						Confidence float64 `json:"confidence"`
						Words      []struct {
							Word       string  `json:"word"`
							Start      float64 `json:"start"`
							End        float64 `json:"end"`
							Confidence float64 `json:"confidence"`
							Speaker    int     `json:"speaker,omitempty"`
						} `json:"words"`
						Paragraphs struct {
							Transcript string `json:"transcript"`
							Paragraphs []struct {
								Sentences []struct {
									Text  string  `json:"text"`
									Start float64 `json:"start"`
									End   float64 `json:"end"`
								} `json:"sentences"`
							} `json:"paragraphs"`
						} `json:"paragraphs"`
					}{{
						Transcript: "Retry successful!",
						Confidence: 0.98,
					}},
				}},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set up provider with custom retry config
	t.Setenv("DEEPGRAM_API_KEY", "test-api-key")
	provider := NewDeepgramProviderEnhanced(logger)
	provider.apiURL = server.URL
	provider.wsURL = "ws://invalid-ws-url" // Force WebSocket to fail
	provider.retryConfig = &RetryConfig{
		MaxRetries:      3,
		InitialDelay:    10 * time.Millisecond,
		MaxDelay:        100 * time.Millisecond,
		BackoffFactor:   2.0,
		RetryableErrors: []string{"temporary failure", "internal server error"},
	}

	err := provider.Initialize()
	require.NoError(t, err)

	// Set up callback
	var receivedTranscription string
	var callbackWG sync.WaitGroup
	callbackWG.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		receivedTranscription = transcription
		callbackWG.Done()
	})

	// Test retry logic
	audioData := strings.NewReader("mock audio data")
	ctx := context.Background()

	start := time.Now()
	err = provider.StreamToText(ctx, audioData, "retry-test")
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)                   // Should have made 3 attempts
	assert.True(t, duration > 20*time.Millisecond) // Should have some delay from retries

	// Wait for callback
	callbackWG.Wait()
	assert.Equal(t, "Retry successful!", receivedTranscription)
}

func TestDeepgramProviderEnhanced_CircuitBreaker(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create provider with circuit breaker
	provider := NewDeepgramProviderEnhanced(logger)
	provider.circuitBreaker = NewCircuitBreaker(2, 100*time.Millisecond) // Low threshold for testing
	provider.wsURL = "ws://invalid-url"                                  // Force WebSocket failures
	provider.apiURL = "http://invalid-url"                               // Force HTTP failures

	// Test circuit breaker states
	cb := provider.circuitBreaker

	// Initial state should be Closed
	assert.True(t, cb.canExecute())
	assert.Equal(t, Closed, cb.state)

	// Record failures to open circuit
	cb.recordFailure()
	assert.True(t, cb.canExecute())
	assert.Equal(t, Closed, cb.state)

	cb.recordFailure()
	assert.False(t, cb.canExecute())
	assert.Equal(t, Open, cb.state)

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)
	assert.True(t, cb.canExecute())

	// Record success to close circuit
	cb.recordSuccess()
	assert.True(t, cb.canExecute())
	assert.Equal(t, Closed, cb.state)
}

func TestDeepgramProviderEnhanced_Shutdown(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	t.Setenv("DEEPGRAM_API_KEY", "test-api-key")
	provider := NewDeepgramProviderEnhanced(logger)

	err := provider.Initialize()
	require.NoError(t, err)

	// Create mock connections
	provider.connectionMutex.Lock()
	mockConn1 := &DeepgramConnection{
		callUUID: "test-1",
		active:   true,
		cancel:   func() {},
		logger:   logger.WithField("call_uuid", "test-1"),
	}
	mockConn2 := &DeepgramConnection{
		callUUID: "test-2",
		active:   true,
		cancel:   func() {},
		logger:   logger.WithField("call_uuid", "test-2"),
	}
	provider.connections["test-1"] = mockConn1
	provider.connections["test-2"] = mockConn2
	provider.connectionMutex.Unlock()

	// Verify connections exist
	assert.Equal(t, 2, provider.GetActiveConnections())

	// Test shutdown
	ctx := context.Background()
	err = provider.Shutdown(ctx)
	assert.NoError(t, err)

	// Verify all connections are closed
	assert.Equal(t, 0, provider.GetActiveConnections())
}

func TestDeepgramConfig_Validation(t *testing.T) {
	tests := []struct {
		name      string
		config    *DeepgramConfig
		expectErr bool
	}{
		{
			name:      "valid config",
			config:    DefaultDeepgramConfig(),
			expectErr: false,
		},
		{
			name: "zero sample rate",
			config: &DeepgramConfig{
				SampleRate: 0,
				Channels:   1,
				Model:      "nova-2",
				Encoding:   "linear16",
			},
			expectErr: true,
		},
		{
			name: "negative channels",
			config: &DeepgramConfig{
				SampleRate: 16000,
				Channels:   -1,
				Model:      "nova-2",
				Encoding:   "linear16",
			},
			expectErr: true,
		},
		{
			name: "invalid model",
			config: &DeepgramConfig{
				SampleRate: 16000,
				Channels:   1,
				Model:      "invalid",
				Encoding:   "linear16",
			},
			expectErr: true,
		},
		{
			name: "invalid encoding",
			config: &DeepgramConfig{
				SampleRate: 16000,
				Channels:   1,
				Model:      "nova-2",
				Encoding:   "invalid",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := logrus.New()
			provider := NewDeepgramProviderEnhancedWithConfig(logger, tt.config)
			provider.apiKey = "test-key" // Bypass API key check

			err := provider.validateConfig()
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeepgramProviderEnhanced_QueryParamsBuilding(t *testing.T) {
	logger := logrus.New()
	provider := NewDeepgramProviderEnhanced(logger)

	// Test with custom configuration
	config := &DeepgramConfig{
		Model:           "enhanced",
		Language:        "es",
		Version:         "2.0",
		Tier:            "enhanced",
		Encoding:        "flac",
		SampleRate:      22050,
		Channels:        2,
		Punctuate:       false,
		Diarize:         true,
		SmartFormat:     true,
		ProfanityFilter: true,
		Utterances:      false,
		InterimResults:  true,
		VAD:             false,
		Endpointing:     true,
		Confidence:      true,
		Timestamps:      false,
		Paragraphs:      true,
		Sentences:       false,
		Redact:          []string{"pci", "ssn"},
		Keywords:        []string{"hello", "world"},
		CustomModel:     "custom-model-123",
	}

	provider.config = config
	query := provider.buildQueryParams()

	// Verify basic parameters
	assert.Equal(t, "enhanced", query.Get("model"))
	assert.Equal(t, "es", query.Get("language"))
	assert.Equal(t, "2.0", query.Get("version"))
	assert.Equal(t, "enhanced", query.Get("tier"))

	// Verify audio parameters
	assert.Equal(t, "flac", query.Get("encoding"))
	assert.Equal(t, "22050", query.Get("sample_rate"))
	assert.Equal(t, "2", query.Get("channels"))

	// Verify feature parameters
	assert.Equal(t, "false", query.Get("punctuate"))
	assert.Equal(t, "true", query.Get("diarize"))
	assert.Equal(t, "true", query.Get("smart_format"))
	assert.Equal(t, "true", query.Get("profanity_filter"))
	assert.Equal(t, "false", query.Get("utterances"))
	assert.Equal(t, "true", query.Get("interim_results"))

	// Verify advanced features
	assert.Equal(t, "false", query.Get("vad_events"))
	assert.Equal(t, "true", query.Get("endpointing"))
	assert.Equal(t, "true", query.Get("include_metadata"))
	assert.Equal(t, "false", query.Get("timestamps"))
	assert.Equal(t, "true", query.Get("paragraphs"))
	assert.Equal(t, "false", query.Get("sentences"))

	// Verify custom parameters
	assert.Equal(t, "pci,ssn", query.Get("redact"))
	assert.Equal(t, "hello,world", query.Get("keywords"))
	assert.Equal(t, "custom-model-123", query.Get("model")) // Should override base model
}

func TestDeepgramProviderEnhanced_ContentTypeMapping(t *testing.T) {
	logger := logrus.New()
	provider := NewDeepgramProviderEnhanced(logger)

	tests := []struct {
		encoding    string
		contentType string
	}{
		{"wav", "audio/wav"},
		{"mp3", "audio/mp3"},
		{"flac", "audio/flac"},
		{"opus", "audio/ogg; codecs=opus"},
		{"linear16", "audio/wav"}, // Default fallback
		{"unknown", "audio/wav"},  // Default fallback
	}

	for _, tt := range tests {
		t.Run(tt.encoding, func(t *testing.T) {
			provider.config.Encoding = tt.encoding
			contentType := provider.getContentType()
			assert.Equal(t, tt.contentType, contentType)
		})
	}
}

// Benchmark tests
func BenchmarkDeepgramProviderEnhanced_QueryParamsBuilding(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	provider := NewDeepgramProviderEnhanced(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.buildQueryParams()
	}
}

func BenchmarkDeepgramProviderEnhanced_ConfigValidation(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	provider := NewDeepgramProviderEnhanced(logger)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.validateConfig()
	}
}

func BenchmarkCircuitBreaker_CanExecute(b *testing.B) {
	cb := NewCircuitBreaker(5, 30*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.canExecute()
	}
}
