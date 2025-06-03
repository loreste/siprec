package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/stt"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
)

// Example demonstrating enhanced Google Speech-to-Text integration with all features

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Example 1: Basic Google Speech-to-Text setup
	fmt.Println("=== Example 1: Basic Google Speech-to-Text Setup ===")
	basicGoogleExample(logger)

	// Example 2: Enhanced Google with advanced configuration
	fmt.Println("\n=== Example 2: Enhanced Google with Advanced Features ===")
	enhancedGoogleExample(logger)

	// Example 3: Real-time gRPC streaming with speaker diarization
	fmt.Println("\n=== Example 3: Real-time Streaming with Speaker Diarization ===")
	streamingWithDiarizationExample(logger)

	// Example 4: Provider manager integration
	fmt.Println("\n=== Example 4: Provider Manager Integration ===")
	providerManagerExample(logger)

	// Example 5: Advanced features demonstration
	fmt.Println("\n=== Example 5: Advanced Features (Custom Models, Phrase Hints, etc.) ===")
	advancedFeaturesExample(logger)

	// Example 6: Multi-language recognition
	fmt.Println("\n=== Example 6: Multi-language Recognition ===")
	multiLanguageExample(logger)
}

func basicGoogleExample(logger *logrus.Logger) {
	// Create basic Google provider
	provider := stt.NewGoogleProvider(logger, nil)

	// Set callback for transcription results
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		confidence := metadata["confidence"].(float32)
		speakerTag := metadata["speaker_tag"].(int32)

		fmt.Printf("📝 Transcription [%s]: %s (Final: %t, Confidence: %.2f, Speaker: %d)\n",
			callUUID, transcription, isFinal, confidence, speakerTag)

		if isFinal {
			wg.Done()
		}
	})

	// Initialize provider (requires GOOGLE_APPLICATION_CREDENTIALS)
	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize Google provider: %v", err)
		log.Printf("Ensure GOOGLE_APPLICATION_CREDENTIALS is set to your service account JSON file")
		return
	}

	// Simulate audio stream
	audioData := strings.NewReader("mock audio data representing speech for Google")

	// Process audio
	ctx := context.Background()
	if err := provider.StreamToText(ctx, audioData, "google-call-001"); err != nil {
		log.Printf("Streaming failed: %v", err)
		return
	}

	// Wait for transcription
	wg.Wait()
	fmt.Println("✅ Basic Google example completed")
}

func enhancedGoogleExample(logger *logrus.Logger) {
	// Create custom configuration with advanced features
	config := &stt.GoogleConfig{
		Model:             "latest_long", // Best for longer audio
		LanguageCode:      "en-US",
		Encoding:          speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:   16000,
		AudioChannelCount: 1,

		// Speaker Diarization
		EnableSpeakerDiarization: true,
		MinSpeakerCount:          1,
		MaxSpeakerCount:          6,
		DiarizationSpeakerCount:  2,

		// Enhanced Features
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets:      true,
		EnableWordConfidence:       true,
		EnableSpokenPunctuation:    false,
		EnableSpokenEmojis:         false,
		UseEnhanced:                true, // Premium enhanced models

		// Real-time Features
		InterimResults:       true,
		SingleUtterance:      false,
		VoiceActivityTimeout: 5 * time.Second,

		// Quality and Performance
		MaxAlternatives:       1,
		EnableProfanityFilter: false,
		BufferSize:            8192,
		FlushInterval:         50 * time.Millisecond,
		ConnectionTimeout:     30 * time.Second,
		RequestTimeout:        5 * time.Minute,

		// Custom Vocabulary
		PhraseHints: []string{
			"SIPREC", "Google Speech", "transcription",
			"real-time", "streaming", "audio processing",
		},
		BoostValue: 10.0, // Strong boost for custom phrases
	}

	// Create enhanced provider
	provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

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
			confidence := metadata["confidence"].(float32)
			provider := metadata["provider"].(string)
			speakerTag := metadata["speaker_tag"].(int32)

			// Check for word-level information
			wordInfo := ""
			if words, ok := metadata["words"].([]map[string]interface{}); ok && len(words) > 0 {
				wordCount := len(words)
				wordInfo = fmt.Sprintf(" [%d words]", wordCount)

				// Show timing for first word
				if startTime, ok := words[0]["start_time"].(time.Time); ok {
					if endTime, ok := words[0]["end_time"].(time.Time); ok {
						duration := endTime.Sub(startTime)
						wordInfo += fmt.Sprintf(" [First word: %v]", duration)
					}
				}
			}

			result := fmt.Sprintf("🎤 [%s %s] %s (Speaker %d, Confidence: %.2f, Provider: %s)%s",
				callUUID, status, transcription, speakerTag, confidence, provider, wordInfo)

			results = append(results, result)
			fmt.Println(result)

			if isFinal {
				wg.Done()
			}
		}
	})

	// Initialize provider
	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize enhanced Google provider: %v", err)
		return
	}

	// Simulate multiple audio streams
	wg.Add(2)

	go func() {
		audioData := strings.NewReader("First audio stream with speaker identification and real-time processing")
		ctx := context.Background()
		if err := provider.StreamToText(ctx, audioData, "enhanced-google-001"); err != nil {
			log.Printf("Stream 1 failed: %v", err)
		}
	}()

	go func() {
		audioData := strings.NewReader("Second audio stream demonstrating SIPREC integration with Google Speech")
		ctx := context.Background()
		if err := provider.StreamToText(ctx, audioData, "enhanced-google-002"); err != nil {
			log.Printf("Stream 2 failed: %v", err)
		}
	}()

	// Wait for completion
	wg.Wait()

	resultsMutex.Lock()
	fmt.Printf("✅ Enhanced Google example completed with %d results\n", len(results))

	// Show provider metrics
	metrics := provider.GetMetrics()
	fmt.Printf("📊 Metrics: Total: %d, Success: %d, Failed: %d, Active: %d\n",
		metrics.TotalRequests, metrics.SuccessfulRequests, metrics.FailedRequests, metrics.ActiveConnections)
	resultsMutex.Unlock()
}

func streamingWithDiarizationExample(logger *logrus.Logger) {
	// Configure for optimal speaker diarization
	config := &stt.GoogleConfig{
		Model:             "latest_long", // Best for diarization
		LanguageCode:      "en-US",
		Encoding:          speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:   16000,
		AudioChannelCount: 1,

		// Optimized for speaker diarization
		EnableSpeakerDiarization: true,
		MinSpeakerCount:          2,
		MaxSpeakerCount:          4,
		DiarizationSpeakerCount:  3,

		// Enhanced timing and confidence
		EnableWordTimeOffsets:      true,
		EnableWordConfidence:       true,
		EnableAutomaticPunctuation: true,
		UseEnhanced:                true,

		// Real-time streaming
		InterimResults:       true,
		VoiceActivityTimeout: 3 * time.Second,
		FlushInterval:        25 * time.Millisecond, // Very responsive
	}

	provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

	// Track speakers and timing
	var speakers = make(map[int32][]string)
	var speakersMutex sync.Mutex
	var wg sync.WaitGroup

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if !isFinal || transcription == "" {
			return
		}

		speakerTag := metadata["speaker_tag"].(int32)
		confidence := metadata["confidence"].(float32)

		speakersMutex.Lock()
		speakers[speakerTag] = append(speakers[speakerTag], transcription)
		speakersMutex.Unlock()

		fmt.Printf("🗣️  Speaker %d [%s]: %s (Confidence: %.2f)\n",
			speakerTag, callUUID, transcription, confidence)

		// Show word-level speaker information
		if words, ok := metadata["words"].([]map[string]interface{}); ok {
			fmt.Printf("   🔍 Word Analysis:\n")
			for i, wordData := range words {
				if i >= 3 { // Show first 3 words
					fmt.Printf("      ... (%d more words)\n", len(words)-3)
					break
				}

				word := wordData["word"].(string)
				wordSpeaker := wordData["speaker_tag"].(int32)
				wordConf := wordData["confidence"].(float32)

				startTime := wordData["start_time"].(time.Time)
				endTime := wordData["end_time"].(time.Time)
				duration := endTime.Sub(startTime)

				fmt.Printf("      %s: Speaker %d, %.2fs, Conf: %.3f\n",
					word, wordSpeaker, duration.Seconds(), wordConf)
			}
		}

		wg.Done()
	})

	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize diarization provider: %v", err)
		return
	}

	// Simulate conversation with multiple speakers
	fmt.Println("🎭 Processing multi-speaker conversation...")
	wg.Add(1)

	conversationAudio := `
	This is speaker one talking about Google Speech-to-Text capabilities.
	Now speaker two is responding about the quality of transcription accuracy.
	Speaker one continues discussing real-time processing and SIPREC integration.
	`
	audioData := strings.NewReader(conversationAudio)

	ctx := context.Background()
	if err := provider.StreamToText(ctx, audioData, "diarization-call"); err != nil {
		log.Printf("Diarization streaming failed: %v", err)
		return
	}

	wg.Wait()

	// Show speaker summary
	speakersMutex.Lock()
	fmt.Printf("\n📊 Speaker Summary:\n")
	for speakerID, utterances := range speakers {
		fmt.Printf("   Speaker %d: %d utterances\n", speakerID, len(utterances))
		for i, utterance := range utterances {
			fmt.Printf("     %d. %s\n", i+1, utterance)
		}
	}
	speakersMutex.Unlock()

	fmt.Println("✅ Speaker diarization example completed")
}

func providerManagerExample(logger *logrus.Logger) {
	// Create provider manager
	manager := stt.NewProviderManager(logger, "google-enhanced")

	// Register enhanced Google provider
	googleProvider := stt.NewGoogleProviderEnhanced(logger)
	if err := manager.RegisterProvider(googleProvider); err != nil {
		log.Printf("Failed to register Google provider: %v", err)
		return
	}

	// Test provider selection
	provider, exists := manager.GetProvider("google-enhanced")
	if !exists {
		log.Println("Google provider not found")
		return
	}

	fmt.Printf("📋 Using provider: %s\n", provider.Name())

	// Set up callback through provider
	if googleProvider, ok := provider.(*stt.GoogleProviderEnhanced); ok {
		var wg sync.WaitGroup
		wg.Add(1)

		googleProvider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
			if isFinal {
				fmt.Printf("🏢 Manager result [%s]: %s (Provider: %s)\n",
					callUUID, transcription, metadata["provider"])
				wg.Done()
			}
		})

		// Stream through manager
		audioData := strings.NewReader("Audio processed through Google provider manager")
		ctx := context.Background()

		if err := manager.StreamToProvider(ctx, "google-enhanced", audioData, "manager-call"); err != nil {
			log.Printf("Manager streaming failed: %v", err)
			return
		}

		wg.Wait()
	}

	fmt.Println("✅ Provider manager example completed")
}

func advancedFeaturesExample(logger *logrus.Logger) {
	// Configure for advanced features demonstration
	config := &stt.GoogleConfig{
		Model:             "enhanced", // Enhanced model
		LanguageCode:      "en-US",
		Encoding:          speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:   16000,
		AudioChannelCount: 2, // Stereo for better separation

		// Advanced Speech Features
		EnableSpeakerDiarization:   true,
		MinSpeakerCount:            1,
		MaxSpeakerCount:            4,
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets:      true,
		EnableWordConfidence:       true,
		EnableSpokenPunctuation:    true, // Recognize spoken punctuation
		EnableSpokenEmojis:         true, // Recognize spoken emojis
		UseEnhanced:                true, // Premium enhanced models

		// Custom Vocabulary and Adaptation
		PhraseHints: []string{
			"Google Cloud Speech-to-Text", "SIPREC integration",
			"real-time transcription", "speaker diarization",
			"phrase hints", "custom vocabulary", "speech adaptation",
		},
		BoostValue: 15.0, // Strong boost for technical terms

		// Speech Adaptation (if you have phrase sets configured)
		AdaptationPhraseSets: []string{
			// "projects/your-project/locations/global/phraseSets/your-phrase-set"
		},

		// Quality Settings
		MaxAlternatives:       3, // Get multiple alternatives
		EnableProfanityFilter: false,

		// Performance Optimization
		InterimResults:       true,
		VoiceActivityTimeout: 2 * time.Second,
		BufferSize:           8192,
		FlushInterval:        50 * time.Millisecond,
		ConnectionTimeout:    45 * time.Second,
		RequestTimeout:       10 * time.Minute,
	}

	provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

	// Detailed callback to showcase all features
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if transcription == "" {
			return
		}

		fmt.Printf("\n🔬 Advanced Analysis [%s]:\n", callUUID)
		fmt.Printf("   Text: %s\n", transcription)
		fmt.Printf("   Final: %t\n", isFinal)
		fmt.Printf("   Confidence: %.3f\n", metadata["confidence"].(float32))
		fmt.Printf("   Speaker: %d\n", metadata["speaker_tag"].(int32))

		if languageCode, ok := metadata["language_code"].(string); ok {
			fmt.Printf("   Language: %s\n", languageCode)
		}

		// Word-level analysis
		if words, ok := metadata["words"].([]map[string]interface{}); ok && len(words) > 0 {
			fmt.Printf("   Word Analysis:\n")
			for i, wordData := range words {
				if i >= 5 { // Show first 5 words
					fmt.Printf("     ... (%d more words)\n", len(words)-5)
					break
				}

				word := wordData["word"].(string)
				startTime := wordData["start_time"].(time.Time)
				endTime := wordData["end_time"].(time.Time)
				wordConf := wordData["confidence"].(float32)
				speaker := wordData["speaker_tag"].(int32)

				duration := endTime.Sub(startTime)
				fmt.Printf("     %s: %.2fs, Speaker %d, Conf: %.3f\n",
					word, duration.Seconds(), speaker, wordConf)
			}
		}

		// Timing information
		if resultEndTime, ok := metadata["result_end_time"]; ok {
			fmt.Printf("   Result End Time: %v\n", resultEndTime)
		}

		// Billing information
		if billedTime, ok := metadata["total_billed_time"]; ok {
			fmt.Printf("   Billed Time: %v\n", billedTime)
		}

		if isFinal {
			wg.Done()
		}
	})

	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize advanced features provider: %v", err)
		return
	}

	// Simulate complex conversation with technical terms
	complexAudio := `
	Welcome to the Google Cloud Speech-to-Text demonstration. This is speaker one explaining
	the advanced features including speaker diarization and real-time transcription.
	Now speaker two is discussing SIPREC integration and phrase hints for better accuracy.
	The system can recognize spoken punctuation comma and even spoken emojis like smiley face.
	`
	audioData := strings.NewReader(complexAudio)

	ctx := context.Background()
	fmt.Println("🎭 Processing complex conversation with advanced features...")

	if err := provider.StreamToText(ctx, audioData, "advanced-call"); err != nil {
		log.Printf("Advanced streaming failed: %v", err)
		return
	}

	wg.Wait()

	// Show provider configuration
	fmt.Printf("\n📊 Provider Configuration:\n")
	config = provider.GetConfig()
	fmt.Printf("   Model: %s\n", config.Model)
	fmt.Printf("   Enhanced Models: %t\n", config.UseEnhanced)
	fmt.Printf("   Speaker Diarization: %t\n", config.EnableSpeakerDiarization)
	fmt.Printf("   Max Speakers: %d\n", config.MaxSpeakerCount)
	fmt.Printf("   Phrase Hints: %d\n", len(config.PhraseHints))
	fmt.Printf("   Boost Value: %.1f\n", config.BoostValue)

	fmt.Println("✅ Advanced features example completed")
}

func multiLanguageExample(logger *logrus.Logger) {
	fmt.Println("🌍 Multi-language Recognition Demo")

	// Configure for multi-language recognition
	config := &stt.GoogleConfig{
		Model:                "latest_short",                      // Good for multiple languages
		LanguageCode:         "en-US",                             // Primary language
		AlternativeLanguages: []string{"es-ES", "fr-FR", "de-DE"}, // Alternative languages
		Encoding:             speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:      16000,
		AudioChannelCount:    1,

		EnableAutomaticPunctuation: true,
		EnableWordConfidence:       true,
		InterimResults:             true,
		UseEnhanced:                true,

		// Language-specific phrase hints
		PhraseHints: []string{
			"hello", "hola", "bonjour", "hallo", // Greetings in different languages
			"thank you", "gracias", "merci", "danke",
		},
		BoostValue: 8.0,

		MaxAlternatives: 2, // Get alternatives for language detection
	}

	provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

	var wg sync.WaitGroup
	wg.Add(3) // Expect 3 different language samples

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if !isFinal || transcription == "" {
			return
		}

		confidence := metadata["confidence"].(float32)
		languageCode := metadata["language_code"].(string)

		fmt.Printf("🗣️  [%s] Language: %s, Text: %s (Confidence: %.2f)\n",
			callUUID, languageCode, transcription, confidence)

		wg.Done()
	})

	if err := provider.Initialize(); err != nil {
		log.Printf("Failed to initialize multi-language provider: %v", err)
		return
	}

	// Simulate audio in different languages
	languages := []struct {
		callID string
		text   string
		lang   string
	}{
		{"english-call", "Hello, this is an English transcription test", "English"},
		{"spanish-call", "Hola, esta es una prueba de transcripción en español", "Spanish"},
		{"french-call", "Bonjour, ceci est un test de transcription en français", "French"},
	}

	for _, lang := range languages {
		go func(l struct {
			callID string
			text   string
			lang   string
		}) {
			fmt.Printf("🎵 Processing %s audio...\n", l.lang)
			audioData := strings.NewReader(l.text)
			ctx := context.Background()

			if err := provider.StreamToText(ctx, audioData, l.callID); err != nil {
				log.Printf("%s streaming failed: %v", l.lang, err)
			}
		}(lang)
	}

	wg.Wait()
	fmt.Println("✅ Multi-language example completed")
}

// Additional helper functions for specific use cases

func demoErrorHandlingAndResilience(logger *logrus.Logger) {
	fmt.Println("\n=== Error Handling and Resilience Demo ===")

	// Configure provider with aggressive retry settings
	provider := stt.NewGoogleProviderEnhanced(logger)

	// Customize retry configuration
	provider.SetRetryConfig(&stt.RetryConfig{
		MaxRetries:      5,
		InitialDelay:    100 * time.Millisecond,
		MaxDelay:        5 * time.Second,
		BackoffFactor:   2.0,
		RetryableErrors: []string{"unavailable", "deadline", "resource exhausted"},
	})

	fmt.Println("🔧 Testing circuit breaker and retry resilience...")

	// The circuit breaker will automatically handle failures and recovery
	fmt.Printf("   Circuit breaker threshold: %d failures\n", 5)
	fmt.Printf("   Retry configuration: %+v\n", provider.retryConfig)

	fmt.Println("✅ Error handling demo completed")
}

func demoPerformanceOptimization(logger *logrus.Logger) {
	fmt.Println("\n=== Performance Optimization Demo ===")

	// High-performance configuration
	config := &stt.GoogleConfig{
		Model:                "latest_short",        // Faster model for real-time
		BufferSize:           16384,                 // Larger buffer
		FlushInterval:        10 * time.Millisecond, // Very frequent processing
		ConnectionTimeout:    60 * time.Second,      // Longer connection timeout
		InterimResults:       true,                  // Real-time results
		VoiceActivityTimeout: 1 * time.Second,       // Quick voice detection
	}

	provider := stt.NewGoogleProviderEnhancedWithConfig(logger, config)

	fmt.Printf("⚡ High-performance configuration:\n")
	fmt.Printf("   Model: %s (optimized for speed)\n", config.Model)
	fmt.Printf("   Buffer Size: %d bytes\n", config.BufferSize)
	fmt.Printf("   Flush Interval: %v\n", config.FlushInterval)
	fmt.Printf("   Connection Timeout: %v\n", config.ConnectionTimeout)

	fmt.Println("✅ Performance optimization demo completed")
}
