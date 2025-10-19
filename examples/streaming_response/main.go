package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/config"
	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
)

// Example demonstrating comprehensive streaming response support across all STT providers

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	fmt.Println("=== SIPREC Streaming Response Support Demo ===")

	// Example 1: Azure Speech Streaming with WebSocket
	fmt.Println("\n=== Example 1: Azure Speech Real-time WebSocket Streaming ===")
	azureStreamingExample(logger)

	// Example 2: Amazon Transcribe Streaming
	fmt.Println("\n=== Example 2: Amazon Transcribe Real-time Streaming ===")
	amazonStreamingExample(logger)

	// Example 3: Google Enhanced Streaming
	fmt.Println("\n=== Example 3: Google Enhanced WebSocket Streaming ===")
	googleStreamingExample(logger)

	// Example 4: Deepgram Enhanced Streaming
	fmt.Println("\n=== Example 4: Deepgram Enhanced WebSocket Streaming ===")
	deepgramStreamingExample(logger)

	// Example 5: OpenAI with Callback Support
	fmt.Println("\n=== Example 5: OpenAI with Streaming Callback ===")
	openaiStreamingExample(logger)

	// Example 6: Multi-Provider Streaming Comparison
	fmt.Println("\n=== Example 6: Multi-Provider Streaming Comparison ===")
	multiProviderStreamingExample(logger)
}

func azureStreamingExample(logger *logrus.Logger) {
	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create Azure STT config
	azureConfig := &config.AzureSTTConfig{
		Enabled:               true,
		SubscriptionKey:       "demo-key",
		Region:                "eastus",
		Language:              "en-US",
		EnableDetailedResults: true,
		ProfanityFilter:       "masked",
		OutputFormat:          "detailed",
	}

	// Create Azure provider with streaming support
	provider := stt.NewAzureSpeechProvider(logger, transcriptionSvc, azureConfig)

	// Set up real-time callback for interim and final results
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		status := "INTERIM"
		if isFinal {
			status = "FINAL"
		}

		confidence := metadata["confidence"].(float64)
		fmt.Printf("🔊 Azure [%s] %s: %s (Confidence: %.2f)\n",
			callUUID, status, transcription, confidence)

		// Show word-level details for final results
		if isFinal && metadata["words"] != nil {
			words := metadata["words"].([]map[string]interface{})
			fmt.Printf("   📝 Word details: %d words with timing information\n", len(words))
		}

		if isFinal {
			wg.Done()
		}
	})

	// Simulate streaming audio data
	audioData := strings.NewReader("This is a test of Azure Speech Services real-time WebSocket streaming with interim results and word-level timing information.")

	// Stream with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := provider.StreamToText(ctx, audioData, "azure-stream-001"); err != nil {
		log.Printf("Azure streaming failed: %v", err)
		return
	}

	wg.Wait()
	fmt.Println("✅ Azure Speech WebSocket streaming completed")
}

func amazonStreamingExample(logger *logrus.Logger) {
	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create Amazon STT config
	amazonConfig := &config.AmazonSTTConfig{
		Enabled:                     true,
		AccessKeyID:                 "demo-access-key",
		SecretAccessKey:             "demo-secret-key",
		Region:                      "us-east-1",
		Language:                    "en-US",
		MediaFormat:                 "pcm",
		SampleRate:                  16000,
		EnableChannelIdentification: true,
		EnableSpeakerIdentification: true,
		VocabularyName:              "",
	}

	// Create Amazon provider with streaming support
	provider := stt.NewAmazonTranscribeProvider(logger, transcriptionSvc, amazonConfig)

	// Set up real-time callback
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		status := "INTERIM"
		if isFinal {
			status = "FINAL"
		}

		fmt.Printf("📡 Amazon [%s] %s: %s\n", callUUID, status, transcription)

		// Show channel and speaker information
		if channelID, ok := metadata["channel_id"]; ok {
			fmt.Printf("   🎙️  Channel: %v\n", channelID)
		}

		if items, ok := metadata["items"].([]map[string]interface{}); ok && len(items) > 0 {
			fmt.Printf("   📊 Items: %d with speaker identification\n", len(items))
		}

		if isFinal {
			wg.Done()
		}
	})

	// Simulate streaming audio data
	audioData := strings.NewReader("This demonstrates Amazon Transcribe real-time streaming with speaker identification and channel detection capabilities.")

	// Stream with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := provider.StreamToText(ctx, audioData, "amazon-stream-001"); err != nil {
		log.Printf("Amazon streaming failed: %v", err)
		return
	}

	wg.Wait()
	fmt.Println("✅ Amazon Transcribe streaming completed")
}

func googleStreamingExample(logger *logrus.Logger) {
	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create Google STT config
	googleConfig := &config.GoogleSTTConfig{
		Enabled:                    true,
		Language:                   "en-US",
		SampleRate:                 16000,
		EnhancedModels:             true,
		Model:                      "latest_long",
		EnableAutomaticPunctuation: true,
		EnableWordTimeOffsets:      true,
		MaxAlternatives:            1,
		ProfanityFilter:            false,
	}

	// Create Google provider
	provider := stt.NewGoogleProvider(logger, transcriptionSvc, googleConfig)

	// Set up callback for enhanced features
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		status := "INTERIM"
		if isFinal {
			status = "FINAL"
		}

		confidence := metadata["confidence"].(float32)
		speakerTag := metadata["speaker_tag"].(int32)

		fmt.Printf("🎯 Google [%s] %s: %s (Speaker: %d, Confidence: %.2f)\n",
			callUUID, status, transcription, speakerTag, confidence)

		if isFinal {
			wg.Done()
		}
	})

	// Simulate streaming audio data
	audioData := strings.NewReader("This showcases Google Speech-to-Text enhanced streaming with automatic punctuation and speaker diarization.")

	// Stream with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := provider.StreamToText(ctx, audioData, "google-stream-001"); err != nil {
		log.Printf("Google streaming failed: %v", err)
		return
	}

	wg.Wait()
	fmt.Println("✅ Google Speech streaming completed")
}

func deepgramStreamingExample(logger *logrus.Logger) {
	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create Deepgram STT config
	deepgramConfig := &config.DeepgramSTTConfig{
		Enabled:   true,
		APIKey:    "demo-api-key",
		Language:  "en-US",
		Model:     "nova-2",
		Tier:      "nova",
		Version:   "latest",
		Punctuate: true,
	}

	// Create Deepgram provider
	provider := stt.NewDeepgramProvider(logger, transcriptionSvc, deepgramConfig, nil)

	// Set up callback for real-time results
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		status := "INTERIM"
		if isFinal {
			status = "FINAL"
		}

		confidence := metadata["confidence"].(float64)
		fmt.Printf("⚡ Deepgram [%s] %s: %s (Confidence: %.2f)\n",
			callUUID, status, transcription, confidence)

		if isFinal {
			wg.Done()
		}
	})

	// Simulate streaming audio data
	audioData := strings.NewReader("This demonstrates Deepgram real-time streaming with nova-2 model and enhanced accuracy.")

	// Stream with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := provider.StreamToText(ctx, audioData, "deepgram-stream-001"); err != nil {
		log.Printf("Deepgram streaming failed: %v", err)
		return
	}

	wg.Wait()
	fmt.Println("✅ Deepgram streaming completed")
}

func openaiStreamingExample(logger *logrus.Logger) {
	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create OpenAI STT config
	openaiConfig := &config.OpenAISTTConfig{
		Enabled:        true,
		APIKey:         "demo-api-key",
		BaseURL:        "https://api.openai.com/v1",
		Model:          "whisper-1",
		ResponseFormat: "json",
		Language:       "en",
		Temperature:    0.0,
		OrganizationID: "",
	}

	// Create OpenAI provider
	provider := stt.NewOpenAIProvider(logger, transcriptionSvc, openaiConfig)

	// Set up callback (OpenAI provides final results only)
	var wg sync.WaitGroup
	wg.Add(1)

	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		fmt.Printf("🤖 OpenAI [%s] FINAL: %s\n", callUUID, transcription)

		if language, ok := metadata["language"]; ok {
			fmt.Printf("   🌍 Detected language: %s\n", language)
		}

		if duration, ok := metadata["duration"]; ok {
			fmt.Printf("   ⏱️  Duration: %v\n", duration)
		}

		wg.Done()
	})

	// Simulate streaming audio data
	audioData := strings.NewReader("This shows OpenAI Whisper integration with callback support for final results.")

	// Stream with context
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := provider.StreamToText(ctx, audioData, "openai-stream-001"); err != nil {
		log.Printf("OpenAI streaming failed: %v", err)
		return
	}

	wg.Wait()
	fmt.Println("✅ OpenAI streaming completed")
}

func multiProviderStreamingExample(logger *logrus.Logger) {
	fmt.Println("🔄 Testing multiple providers with standardized streaming interface...")

	// Create provider manager
	_ = stt.NewProviderManager(logger, "azure-speech", []string{"azure-speech"})

	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create all providers
	providers := []stt.StreamingProvider{
		stt.NewAzureSpeechProvider(logger, transcriptionSvc, &config.AzureSTTConfig{
			Enabled: true, SubscriptionKey: "demo-key", Region: "eastus", Language: "en-US",
		}),
		stt.NewAmazonTranscribeProvider(logger, transcriptionSvc, &config.AmazonSTTConfig{
			Enabled: true, AccessKeyID: "demo-key", SecretAccessKey: "demo-secret",
			Region: "us-east-1", Language: "en-US", MediaFormat: "pcm", SampleRate: 16000,
		}),
		stt.NewGoogleProvider(logger, transcriptionSvc, &config.GoogleSTTConfig{
			Enabled: true, Language: "en-US", SampleRate: 16000,
		}),
		stt.NewDeepgramProvider(logger, transcriptionSvc, &config.DeepgramSTTConfig{
			Enabled: true, APIKey: "demo-key", Language: "en-US", Model: "nova-2",
		}, nil),
		stt.NewOpenAIProvider(logger, transcriptionSvc, &config.OpenAISTTConfig{
			Enabled: true, APIKey: "demo-key", BaseURL: "https://api.openai.com/v1", Model: "whisper-1",
		}),
	}

	var wg sync.WaitGroup

	// Test all providers with the same streaming interface
	for i, provider := range providers {
		wg.Add(1)

		go func(idx int, p stt.StreamingProvider) {
			defer wg.Done()

			// Set callback for each provider
			p.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
				if isFinal {
					providerName := metadata["provider"].(string)
					fmt.Printf("✨ Provider %s [%s]: %s\n", providerName, callUUID, transcription)
				}
			})

			// Stream audio data
			audioData := strings.NewReader(fmt.Sprintf("This is test audio for provider %d using standardized streaming interface.", idx+1))
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := p.StreamToText(ctx, audioData, fmt.Sprintf("multi-stream-%d", idx+1)); err != nil {
				log.Printf("Provider %s streaming failed: %v", p.Name(), err)
			}
		}(i, provider)
	}

	wg.Wait()
	fmt.Println("✅ Multi-provider streaming comparison completed")
	fmt.Println("🎉 All providers support standardized streaming interface with callbacks!")
}
