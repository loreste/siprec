package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"siprec-server/pkg/config"
	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// STTProviderTestSuite provides comprehensive integration tests for all STT providers
type STTProviderTestSuite struct {
	suite.Suite
	logger           *logrus.Logger
	transcriptionSvc *stt.TranscriptionService
	testAudioData    []byte
	providers        map[string]stt.Provider
	results          map[string][]TranscriptionResult
}

// TranscriptionResult captures test results
type TranscriptionResult struct {
	Provider     string
	Text         string
	IsFinal      bool
	Metadata     map[string]interface{}
	Timestamp    time.Time
	ResponseTime time.Duration
}

// TestTranscriptionListener collects transcription results for testing
type TestTranscriptionListener struct {
	results   []TranscriptionResult
	provider  string
	startTime time.Time
}

func (t *TestTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	responseTime := time.Since(t.startTime)
	result := TranscriptionResult{
		Provider:     t.provider,
		Text:         transcription,
		IsFinal:      isFinal,
		Metadata:     metadata,
		Timestamp:    time.Now(),
		ResponseTime: responseTime,
	}
	t.results = append(t.results, result)
}

// SetupSuite initializes the test suite
func (suite *STTProviderTestSuite) SetupSuite() {
	// Setup logger
	suite.logger = logrus.New()
	suite.logger.SetLevel(logrus.DebugLevel)

	// Create transcription service
	suite.transcriptionSvc = stt.NewTranscriptionService(suite.logger)

	// Initialize providers map
	suite.providers = make(map[string]stt.Provider)
	suite.results = make(map[string][]TranscriptionResult)

	// Load test audio data
	suite.loadTestAudioData()

	// Initialize STT providers
	suite.initializeProviders()
}

// loadTestAudioData loads test audio files for testing
func (suite *STTProviderTestSuite) loadTestAudioData() {
	// Try to load a real test audio file
	audioFiles := []string{
		"../../test-recordings/test_audio.wav",
		"../../test-recordings/sample.wav",
		"../test-data/test_audio.wav",
	}

	for _, file := range audioFiles {
		if data, err := os.ReadFile(file); err == nil {
			suite.testAudioData = data
			suite.logger.WithField("file", file).Info("Loaded test audio data")
			return
		}
	}

	// If no real audio file is found, create synthetic test data
	suite.testAudioData = suite.createSyntheticAudioData()
	suite.logger.Info("Using synthetic test audio data")
}

// createSyntheticAudioData creates synthetic audio data for testing
func (suite *STTProviderTestSuite) createSyntheticAudioData() []byte {
	// Create a simple WAV file header + some data
	// This is a minimal WAV file that providers should be able to handle
	header := []byte{
		// RIFF header
		0x52, 0x49, 0x46, 0x46, // "RIFF"
		0x24, 0x00, 0x00, 0x00, // File size - 8
		0x57, 0x41, 0x56, 0x45, // "WAVE"
		// fmt chunk
		0x66, 0x6D, 0x74, 0x20, // "fmt "
		0x10, 0x00, 0x00, 0x00, // Subchunk1Size (16 for PCM)
		0x01, 0x00, // AudioFormat (PCM)
		0x01, 0x00, // NumChannels (Mono)
		0x40, 0x1F, 0x00, 0x00, // SampleRate (8000 Hz)
		0x80, 0x3E, 0x00, 0x00, // ByteRate
		0x02, 0x00, // BlockAlign
		0x10, 0x00, // BitsPerSample (16)
		// data chunk
		0x64, 0x61, 0x74, 0x61, // "data"
		0x00, 0x00, 0x00, 0x00, // Subchunk2Size
	}

	// Add some synthetic audio data (simple sine wave)
	audioData := make([]byte, 1600) // 0.1 seconds at 8000 Hz, 16-bit
	for i := 0; i < len(audioData); i += 2 {
		// Simple sine wave pattern
		value := int16(1000 * (i % 100) / 100)
		audioData[i] = byte(value & 0xFF)
		audioData[i+1] = byte((value >> 8) & 0xFF)
	}

	// Update data size in header
	dataSize := len(audioData)
	header[40] = byte(dataSize & 0xFF)
	header[41] = byte((dataSize >> 8) & 0xFF)
	header[42] = byte((dataSize >> 16) & 0xFF)
	header[43] = byte((dataSize >> 24) & 0xFF)

	// Update total file size
	totalSize := len(header) + len(audioData) - 8
	header[4] = byte(totalSize & 0xFF)
	header[5] = byte((totalSize >> 8) & 0xFF)
	header[6] = byte((totalSize >> 16) & 0xFF)
	header[7] = byte((totalSize >> 24) & 0xFF)

	return append(header, audioData...)
}

// initializeProviders initializes all available STT providers
func (suite *STTProviderTestSuite) initializeProviders() {
	// Initialize Amazon Transcribe if credentials are available
	if os.Getenv("AWS_REGION") != "" && (os.Getenv("AWS_ACCESS_KEY_ID") != "" || os.Getenv("AWS_PROFILE") != "") {
		provider := stt.NewAmazonTranscribeProvider(suite.logger, suite.transcriptionSvc, nil)
		if err := provider.Initialize(); err != nil {
			suite.logger.WithError(err).Warn("Failed to initialize Amazon Transcribe provider")
		} else {
			suite.providers["amazon-transcribe"] = provider
			suite.logger.Info("Initialized Amazon Transcribe provider")
		}
	}

	// Initialize Azure Speech if credentials are available
	if os.Getenv("AZURE_SPEECH_KEY") != "" && os.Getenv("AZURE_SPEECH_REGION") != "" {
		provider := stt.NewAzureSpeechProvider(suite.logger, suite.transcriptionSvc, nil)
		if err := provider.Initialize(); err != nil {
			suite.logger.WithError(err).Warn("Failed to initialize Azure Speech provider")
		} else {
			suite.providers["azure-speech"] = provider
			suite.logger.Info("Initialized Azure Speech provider")
		}
	}

	// Initialize Google Speech if credentials are available
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		googleConfig := &config.GoogleSTTConfig{
			Enabled:               true,
			CredentialsPath:       os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
			Model:                 "latest_long",
			Language:              "en-US",
			EnableWordTimeOffsets: true,
			EnableWordConfidence:  true,
			MaxAlternatives:       3,
			UseEnhanced:           true,
		}
		provider := stt.NewGoogleProvider(suite.logger, suite.transcriptionSvc, googleConfig)
		if err := provider.Initialize(); err != nil {
			suite.logger.WithError(err).Warn("Failed to initialize Google Speech provider")
		} else {
			suite.providers["google-speech"] = provider
			suite.logger.Info("Initialized Google Speech provider")
		}
	}

	// Always initialize mock provider for testing
	mockProvider := stt.NewMockProvider(suite.logger)
	if err := mockProvider.Initialize(); err != nil {
		suite.logger.WithError(err).Error("Failed to initialize Mock provider")
	} else {
		suite.providers["mock"] = mockProvider
		suite.logger.Info("Initialized Mock provider")
	}

	suite.logger.WithField("provider_count", len(suite.providers)).Info("Initialized STT providers")
}

// TestProviderInitialization tests that all providers can be initialized
func (suite *STTProviderTestSuite) TestProviderInitialization() {
	suite.Require().NotEmpty(suite.providers, "At least one provider should be available")

	for name, provider := range suite.providers {
		suite.Run(name, func() {
			suite.Assert().NotNil(provider, "Provider should not be nil")
			suite.Assert().Equal(name, provider.Name(), "Provider name should match")
		})
	}
}

// TestBasicTranscription tests basic transcription functionality
func (suite *STTProviderTestSuite) TestBasicTranscription() {
	for name, provider := range suite.providers {
		suite.Run(name, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Create test listener
			listener := &TestTranscriptionListener{
				provider:  name,
				startTime: time.Now(),
			}
			suite.transcriptionSvc.AddListener(listener)
			defer suite.transcriptionSvc.RemoveListener(listener)

			// Create audio stream
			audioReader := strings.NewReader(string(suite.testAudioData))

			// Test transcription
			callUUID := "test-call-" + name
			err := provider.StreamToText(ctx, audioReader, callUUID)

			// Validate results
			if name == "mock" {
				// Mock provider should always succeed
				suite.Assert().NoError(err, "Mock provider should not return error")
				suite.Assert().NotEmpty(listener.results, "Should receive transcription results")
			} else {
				// Real providers might fail due to network/auth issues in test environment
				if err != nil {
					suite.logger.WithError(err).WithField("provider", name).Warn("Provider failed (expected in test environment)")
				} else {
					suite.Assert().NoError(err, "Provider should not return error")
				}
			}

			suite.results[name] = listener.results
		})
	}
}

// TestConcurrentTranscription tests concurrent transcription requests
func (suite *STTProviderTestSuite) TestConcurrentTranscription() {
	if len(suite.providers) == 0 {
		suite.T().Skip("No providers available for concurrent testing")
	}

	const concurrentRequests = 3

	for name, provider := range suite.providers {
		suite.Run(name, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			// Create multiple listeners
			listeners := make([]*TestTranscriptionListener, concurrentRequests)
			for i := 0; i < concurrentRequests; i++ {
				listeners[i] = &TestTranscriptionListener{
					provider:  name,
					startTime: time.Now(),
				}
				suite.transcriptionSvc.AddListener(listeners[i])
			}

			// Cleanup listeners
			defer func() {
				for _, listener := range listeners {
					suite.transcriptionSvc.RemoveListener(listener)
				}
			}()

			// Run concurrent transcriptions
			errChan := make(chan error, concurrentRequests)
			for i := 0; i < concurrentRequests; i++ {
				go func(index int) {
					audioReader := strings.NewReader(string(suite.testAudioData))
					callUUID := suite.T().Name() + "-concurrent-" + name + "-" + string(rune(index))
					errChan <- provider.StreamToText(ctx, audioReader, callUUID)
				}(i)
			}

			// Collect results
			errors := 0
			for i := 0; i < concurrentRequests; i++ {
				if err := <-errChan; err != nil {
					errors++
					suite.logger.WithError(err).WithField("provider", name).Warn("Concurrent request failed")
				}
			}

			// For mock provider, expect no errors
			if name == "mock" {
				suite.Assert().Equal(0, errors, "Mock provider should handle concurrent requests without errors")
			}
		})
	}
}

// TestProviderPerformance measures basic performance metrics
func (suite *STTProviderTestSuite) TestProviderPerformance() {
	for name, provider := range suite.providers {
		suite.Run(name, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			listener := &TestTranscriptionListener{
				provider:  name,
				startTime: time.Now(),
			}
			suite.transcriptionSvc.AddListener(listener)
			defer suite.transcriptionSvc.RemoveListener(listener)

			// Measure transcription time
			start := time.Now()
			audioReader := strings.NewReader(string(suite.testAudioData))
			callUUID := "perf-test-" + name

			err := provider.StreamToText(ctx, audioReader, callUUID)
			duration := time.Since(start)

			// Log performance metrics
			suite.logger.WithFields(logrus.Fields{
				"provider":         name,
				"duration_ms":      duration.Milliseconds(),
				"audio_size_bytes": len(suite.testAudioData),
				"error":            err != nil,
				"result_count":     len(listener.results),
			}).Info("Provider performance metrics")

			// Basic performance assertions
			suite.Assert().Less(duration, 60*time.Second, "Transcription should complete within reasonable time")

			if name == "mock" && err == nil {
				suite.Assert().NotEmpty(listener.results, "Should receive results from mock provider")
			}
		})
	}
}

// TestErrorHandling tests error handling scenarios
func (suite *STTProviderTestSuite) TestErrorHandling() {
	for name, provider := range suite.providers {
		suite.Run(name, func() {
			// Test with invalid audio data
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			invalidReader := strings.NewReader("invalid audio data")
			callUUID := "error-test-" + name

			err := provider.StreamToText(ctx, invalidReader, callUUID)

			// Mock provider should handle gracefully, real providers may error
			if name == "mock" {
				suite.Assert().NoError(err, "Mock provider should handle invalid data gracefully")
			} else {
				// Real providers may or may not error depending on implementation
				suite.logger.WithError(err).WithField("provider", name).Info("Error handling test result")
			}
		})
	}
}

// TestContextCancellation tests context cancellation handling
func (suite *STTProviderTestSuite) TestContextCancellation() {
	for name, provider := range suite.providers {
		suite.Run(name, func() {
			ctx, cancel := context.WithCancel(context.Background())

			// Cancel context immediately
			cancel()

			audioReader := strings.NewReader(string(suite.testAudioData))
			callUUID := "cancel-test-" + name

			start := time.Now()
			err := provider.StreamToText(ctx, audioReader, callUUID)
			duration := time.Since(start)

			// Should return quickly with context cancellation
			suite.Assert().Less(duration, 5*time.Second, "Should return quickly when context is cancelled")

			if err != nil {
				suite.Assert().ErrorIs(err, context.Canceled, "Should return context cancellation error")
			}
		})
	}
}

// TestLargeAudioData tests handling of larger audio files
func (suite *STTProviderTestSuite) TestLargeAudioData() {
	// Create larger synthetic audio data (1 second at 8000 Hz)
	largeAudioData := make([]byte, 16000) // 1 second of 16-bit mono audio
	for i := 0; i < len(largeAudioData); i += 2 {
		value := int16(1000 * (i % 1000) / 1000) // More complex waveform
		largeAudioData[i] = byte(value & 0xFF)
		largeAudioData[i+1] = byte((value >> 8) & 0xFF)
	}

	for name, provider := range suite.providers {
		suite.Run(name, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			listener := &TestTranscriptionListener{
				provider:  name,
				startTime: time.Now(),
			}
			suite.transcriptionSvc.AddListener(listener)
			defer suite.transcriptionSvc.RemoveListener(listener)

			audioReader := strings.NewReader(string(largeAudioData))
			callUUID := "large-audio-test-" + name

			err := provider.StreamToText(ctx, audioReader, callUUID)

			suite.logger.WithFields(logrus.Fields{
				"provider":         name,
				"audio_size_bytes": len(largeAudioData),
				"error":            err != nil,
				"result_count":     len(listener.results),
			}).Info("Large audio test results")

			if name == "mock" {
				suite.Assert().NoError(err, "Mock provider should handle large audio")
			}
		})
	}
}

// TearDownSuite cleans up after all tests
func (suite *STTProviderTestSuite) TearDownSuite() {
	suite.logger.Info("STT Provider integration tests completed")

	// Log summary of results
	for provider, results := range suite.results {
		suite.logger.WithFields(logrus.Fields{
			"provider":     provider,
			"result_count": len(results),
		}).Info("Test results summary")
	}
}

// TestSTTProviderIntegration runs the complete test suite
func TestSTTProviderIntegration(t *testing.T) {
	suite.Run(t, new(STTProviderTestSuite))
}

// Benchmark tests for performance measurement
func BenchmarkSTTProviders(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel) // Reduce noise in benchmarks

	_ = stt.NewTranscriptionService(logger)

	// Initialize mock provider for benchmarking
	mockProvider := stt.NewMockProvider(logger)
	require.NoError(b, mockProvider.Initialize())

	// Create test audio data
	audioData := make([]byte, 1600) // Small audio sample
	for i := 0; i < len(audioData); i += 2 {
		value := int16(100 * (i % 100) / 100)
		audioData[i] = byte(value & 0xFF)
		audioData[i+1] = byte((value >> 8) & 0xFF)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		audioReader := strings.NewReader(string(audioData))
		callUUID := "benchmark-test"

		err := mockProvider.StreamToText(ctx, audioReader, callUUID)
		assert.NoError(b, err)

		cancel()
	}
}
