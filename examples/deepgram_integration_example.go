package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
)

// Example demonstrating enhanced Deepgram integration with all features

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Example 1: Basic Deepgram setup with default configuration
	fmt.Println("=== Example 1: Basic Deepgram Setup ===")
	basicDeepgramExample(logger)

	// Example 2: Enhanced Deepgram with custom configuration
	fmt.Println("\n=== Example 2: Enhanced Deepgram with Custom Config ===")
	enhancedDeepgramExample(logger)

	// Example 3: Real-time WebSocket streaming
	fmt.Println("\n=== Example 3: Real-time WebSocket Streaming ===")
	websocketStreamingExample(logger)

	// Example 4: Provider manager with multiple STT providers
	fmt.Println("\n=== Example 4: Provider Manager Integration ===")
	providerManagerExample(logger)

	// Example 5: Advanced features demonstration
	fmt.Println("\n=== Example 5: Advanced Features (Diarization, Keywords, etc.) ===")
	advancedFeaturesExample(logger)
}

func basicDeepgramExample(logger *logrus.Logger) {
	// Create basic Deepgram provider
	provider := stt.NewDeepgramProvider(logger)

	// Set callback for transcription results
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		fmt.Printf("📝 Transcription [%s]: %s (Final: %t, Confidence: %.2f)\n",
			callUUID, transcription, isFinal, metadata["confidence"].(float64))
		wg.Done()
	})

	// Initialize provider (requires DEEPGRAM_API_KEY environment variable)
	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize Deepgram provider: %v", err)
		return
	}

	// Simulate audio stream
	audioData := strings.NewReader("mock audio data representing speech")

	// Process audio
	ctx := context.Background()
	if err := provider.StreamToText(ctx, audioData, "call-001"); err != nil {
		log.Printf("Streaming failed: %v", err)
		return
	}

	// Wait for transcription
	wg.Wait()
	fmt.Println("✅ Basic Deepgram example completed")
}

func enhancedDeepgramExample(logger *logrus.Logger) {
	// Create custom configuration with advanced features
	config := &stt.DeepgramConfig{
		Model:      "nova-2", // Latest model
		Language:   "en-US",  // Specific language variant
		Version:    "latest",
		Tier:       "nova",
		Encoding:   "linear16",
		SampleRate: 16000,
		Channels:   1,

		// Enhanced features
		Punctuate:       true,
		Diarize:         true, // Speaker identification
		SmartFormat:     true, // Smart formatting (dates, times, etc.)
		ProfanityFilter: false,
		Utterances:      true, // Utterance-level results
		InterimResults:  true, // Real-time interim results

		// Advanced AI features
		VAD:         true, // Voice activity detection
		Endpointing: true, // Automatic speech endpoint detection
		Confidence:  true, // Include confidence scores
		Timestamps:  true, // Word-level timestamps
		Paragraphs:  true, // Paragraph detection
		Sentences:   true, // Sentence detection

		// Custom vocabulary
		Keywords: []string{"SIPREC", "transcription", "Deepgram"},
		Redact:   []string{"pci", "ssn"}, // PII redaction

		// Performance tuning
		KeepAlive:     true,
		BufferSize:    8192,
		FlushInterval: 50 * time.Millisecond,
	}

	// Create enhanced provider
	provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)

	// Set callback for rich transcription results
	var results []string
	var resultsMutex sync.Mutex
	var wg sync.WaitGroup

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		resultsMutex.Lock()
		defer resultsMutex.Unlock()

		if transcription != "" {
			status := "interim"
			if isFinal {
				status = "final"
			}

			// Extract rich metadata
			confidence := metadata["confidence"].(float64)
			provider := metadata["provider"].(string)

			// Check for speaker information
			speakerInfo := ""
			if words, ok := metadata["words"].([]interface{}); ok && len(words) > 0 {
				if word, ok := words[0].(map[string]interface{}); ok {
					if speaker, ok := word["speaker"]; ok {
						speakerInfo = fmt.Sprintf(" [Speaker %v]", speaker)
					}
				}
			}

			result := fmt.Sprintf("🎤 [%s %s] %s%s (Confidence: %.2f, Provider: %s)",
				callUUID, status, transcription, speakerInfo, confidence, provider)

			results = append(results, result)
			fmt.Println(result)

			if isFinal {
				wg.Done()
			}
		} else if metadata["event_type"] == "utterance_end" {
			fmt.Printf("🔚 Utterance ended for %s (Duration: %.2fs)\n",
				callUUID, metadata["duration"].(float64))
		}
	})

	// Initialize provider
	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize enhanced Deepgram provider: %v", err)
		return
	}

	// Simulate multiple audio streams
	wg.Add(2)

	go func() {
		audioData := strings.NewReader("First audio stream with speaker one")
		ctx := context.Background()
		if err := provider.StreamToText(ctx, audioData, "enhanced-call-001"); err != nil {
			log.Printf("Stream 1 failed: %v", err)
		}
	}()

	go func() {
		audioData := strings.NewReader("Second audio stream with speaker two")
		ctx := context.Background()
		if err := provider.StreamToText(ctx, audioData, "enhanced-call-002"); err != nil {
			log.Printf("Stream 2 failed: %v", err)
		}
	}()

	// Wait for completion
	wg.Wait()

	resultsMutex.Lock()
	fmt.Printf("✅ Enhanced Deepgram example completed with %d results\n", len(results))
	resultsMutex.Unlock()
}

func websocketStreamingExample(logger *logrus.Logger) {
	// Configure for real-time streaming
	config := stt.DefaultDeepgramConfig()
	config.InterimResults = true
	config.KeepAlive = true
	config.FlushInterval = 25 * time.Millisecond // Very responsive
	config.VAD = true                            // Voice activity detection
	config.Endpointing = true                    // Auto-endpoint detection

	provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)

	// Track interim and final results separately
	var interimCount, finalCount int
	var countMutex sync.Mutex
	var wg sync.WaitGroup

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		countMutex.Lock()
		defer countMutex.Unlock()

		if transcription != "" {
			if isFinal {
				finalCount++
				fmt.Printf("🎯 FINAL [%s]: %s\n", callUUID, transcription)
				wg.Done()
			} else {
				interimCount++
				fmt.Printf("⚡ INTERIM [%s]: %s\n", callUUID, transcription)
			}
		}
	})

	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize WebSocket streaming provider: %v", err)
		return
	}

	// Simulate real-time audio streaming
	fmt.Println("🌐 Starting WebSocket streaming simulation...")
	wg.Add(1)

	// Create a longer audio stream to demonstrate real-time processing
	audioContent := `
	This is a longer audio stream that would normally be processed in real-time.
	The WebSocket connection allows for immediate interim results as speech is detected.
	This enables responsive user interfaces and real-time transcription displays.
	`
	audioData := strings.NewReader(audioContent)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	if err := provider.StreamToText(ctx, audioData, "websocket-call"); err != nil {
		log.Printf("WebSocket streaming failed: %v", err)
		return
	}

	wg.Wait()
	duration := time.Since(start)

	countMutex.Lock()
	fmt.Printf("✅ WebSocket streaming completed in %v\n", duration)
	fmt.Printf("   📊 Results: %d interim, %d final\n", interimCount, finalCount)
	fmt.Printf("   🔗 Active connections: %d\n", provider.GetActiveConnections())
	countMutex.Unlock()

	// Demonstrate graceful shutdown
	if err := provider.Shutdown(context.Background()); err != nil {
		log.Printf("Shutdown failed: %v", err)
	} else {
		fmt.Println("   🛑 Graceful shutdown completed")
	}
}

func providerManagerExample(logger *logrus.Logger) {
	// Create provider manager
	manager := stt.NewProviderManager(logger, "deepgram-enhanced")

	// Register enhanced Deepgram provider
	deepgramProvider := stt.NewDeepgramProviderEnhanced(logger)
	if err := manager.RegisterProvider(deepgramProvider); err != nil {
		log.Printf("Failed to register Deepgram provider: %v", err)
		return
	}

	// Could register other providers here (Azure, Google, etc.)
	// azureProvider := stt.NewAzureProvider(logger)
	// manager.RegisterProvider(azureProvider)

	// Test provider selection
	provider, exists := manager.GetProvider("deepgram-enhanced")
	if !exists {
		log.Println("Deepgram provider not found")
		return
	}

	fmt.Printf("📋 Using provider: %s\n", provider.Name())

	// Set up callback through provider
	if deepgramProvider, ok := provider.(*stt.DeepgramProviderEnhanced); ok {
		var wg sync.WaitGroup
		wg.Add(1)

		deepgramProvider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
			fmt.Printf("🏢 Manager result [%s]: %s (Provider: %s)\n",
				callUUID, transcription, metadata["provider"])
			if isFinal {
				wg.Done()
			}
		})

		// Stream through manager
		audioData := strings.NewReader("Audio processed through provider manager")
		ctx := context.Background()

		if err := manager.StreamToProvider(ctx, "deepgram-enhanced", audioData, "manager-call"); err != nil {
			log.Printf("Manager streaming failed: %v", err)
			return
		}

		wg.Wait()
	}

	fmt.Println("✅ Provider manager example completed")
}

func advancedFeaturesExample(logger *logrus.Logger) {
	// Configure for advanced features demonstration
	config := &stt.DeepgramConfig{
		Model:       "nova-2",
		Language:    "en",
		Diarize:     true, // Speaker identification
		SmartFormat: true, // Smart formatting
		Utterances:  true, // Utterance detection
		Confidence:  true, // Confidence scores
		Timestamps:  true, // Word-level timing
		Paragraphs:  true, // Paragraph detection
		Sentences:   true, // Sentence boundaries

		// Custom vocabulary for domain-specific terms
		Keywords: []string{
			"SIPREC", "WebRTC", "transcription", "Deepgram",
			"real-time", "streaming", "audio processing",
		},

		// PII redaction
		Redact: []string{"pci", "ssn", "numbers"},

		// Performance optimization
		Encoding:      "linear16",
		SampleRate:    16000,
		Channels:      2, // Stereo for better speaker separation
		BufferSize:    4096,
		FlushInterval: 100 * time.Millisecond,
	}

	provider := stt.NewDeepgramProviderEnhancedWithConfig(logger, config)

	// Detailed callback to showcase all features
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if transcription == "" {
			// Handle events like utterance_end
			if eventType, ok := metadata["event_type"]; ok {
				fmt.Printf("📡 Event [%s]: %s (Duration: %.2fs)\n",
					callUUID, eventType, metadata["duration"].(float64))
			}
			return
		}

		fmt.Printf("\n🔬 Advanced Analysis [%s]:\n", callUUID)
		fmt.Printf("   Text: %s\n", transcription)
		fmt.Printf("   Final: %t\n", isFinal)
		fmt.Printf("   Confidence: %.3f\n", metadata["confidence"])

		// Speaker information (if available)
		if words, ok := metadata["words"].([]interface{}); ok && len(words) > 0 {
			fmt.Printf("   Word Analysis:\n")
			for i, wordData := range words {
				if i >= 3 { // Show first 3 words
					fmt.Printf("   ... (%d more words)\n", len(words)-3)
					break
				}
				if word, ok := wordData.(map[string]interface{}); ok {
					wordText := word["word"].(string)
					start := word["start"].(float64)
					end := word["end"].(float64)
					wordConf := word["confidence"].(float64)

					speakerInfo := ""
					if speaker, ok := word["speaker"]; ok {
						speakerInfo = fmt.Sprintf(" [Speaker %v]", speaker)
					}

					fmt.Printf("     %s: %.2f-%.2fs (conf: %.3f)%s\n",
						wordText, start, end, wordConf, speakerInfo)
				}
			}
		}

		// Model information
		if modelName, ok := metadata["model_name"]; ok {
			fmt.Printf("   Model: %s\n", modelName)
		}

		// Request tracking
		if requestID, ok := metadata["request_id"]; ok {
			fmt.Printf("   Request ID: %s\n", requestID)
		}

		if isFinal {
			wg.Done()
		}
	})

	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize advanced features provider: %v", err)
		return
	}

	// Simulate conversation with multiple speakers
	conversationAudio := `
	Hello, this is the first speaker talking about SIPREC transcription.
	The phone number is 555-123-4567 and SSN is 123-45-6789.
	Now the second speaker is responding about real-time audio processing.
	`
	audioData := strings.NewReader(conversationAudio)

	ctx := context.Background()
	fmt.Println("🎭 Processing multi-speaker conversation with PII redaction...")

	if err := provider.StreamToText(ctx, audioData, "advanced-call"); err != nil {
		log.Printf("Advanced streaming failed: %v", err)
		return
	}

	wg.Wait()

	// Show provider statistics
	fmt.Printf("\n📊 Provider Statistics:\n")
	fmt.Printf("   Active Connections: %d\n", provider.GetActiveConnections())
	config = provider.GetConfig()
	fmt.Printf("   Configuration: %+v\n", config)

	fmt.Println("✅ Advanced features example completed")
}

// Additional helper functions

func demoErrorHandling(logger *logrus.Logger) {
	fmt.Println("\n=== Error Handling and Resilience Demo ===")

	// Configure provider with aggressive retry settings for demo
	provider := stt.NewDeepgramProviderEnhanced(logger)

	// Customize retry configuration
	provider.SetRetryConfig(&stt.RetryConfig{
		MaxRetries:      5,
		InitialDelay:    50 * time.Millisecond,
		MaxDelay:        2 * time.Second,
		BackoffFactor:   2.0,
		RetryableErrors: []string{"connection", "timeout", "5xx"},
	})

	// Test circuit breaker
	fmt.Println("🔧 Testing circuit breaker resilience...")

	// Simulate multiple failures to trigger circuit breaker
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := provider.StreamToText(ctx, strings.NewReader("test"), fmt.Sprintf("test-%d", i))
		cancel()

		if err != nil {
			fmt.Printf("   ❌ Attempt %d failed: %v\n", i+1, err)
		}
	}

	fmt.Println("✅ Error handling demo completed")
}

func demoConfigurationUpdates(logger *logrus.Logger) {
	fmt.Println("\n=== Dynamic Configuration Updates Demo ===")

	provider := stt.NewDeepgramProviderEnhanced(logger)

	// Show initial config
	config := provider.GetConfig()
	fmt.Printf("📋 Initial Model: %s, Language: %s\n", config.Model, config.Language)

	// Update configuration dynamically
	newConfig := &stt.DeepgramConfig{
		Model:       "enhanced",
		Language:    "es",
		SampleRate:  22050,
		Channels:    2,
		Encoding:    "linear16",
		Diarize:     true,
		Punctuate:   true,
		SmartFormat: true,
	}

	if err := provider.UpdateConfig(newConfig); err != nil {
		log.Printf("Config update failed: %v", err)
		return
	}

	// Show updated config
	updatedConfig := provider.GetConfig()
	fmt.Printf("🔄 Updated Model: %s, Language: %s\n", updatedConfig.Model, updatedConfig.Language)

	fmt.Println("✅ Configuration updates demo completed")
}
