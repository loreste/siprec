package main

import (
	"context"
	"io"
	"log"
)

// Define a struct to handle different Speech-to-Text providers
type SpeechToTextProvider struct {
	GoogleClient   func(ctx context.Context, audioStream io.Reader, callUUID string)
	DeepgramClient func(ctx context.Context, audioStream io.Reader, callUUID string) error
	OpenAIClient   func(ctx context.Context, audioStream io.Reader, callUUID string) error
}

// StreamToProvider is a generic function to stream audio to the desired provider
func StreamToProvider(ctx context.Context, provider string, audioStream io.Reader, callUUID string) {
	switch provider {
	case "google":
		if speechToText.GoogleClient != nil {
			log.Printf("Starting transcription using Google for call: %s", callUUID)
			speechToText.GoogleClient(ctx, audioStream, callUUID)
		} else {
			log.Printf("Google Speech-to-Text client not initialized for call: %s", callUUID)
		}
	case "deepgram":
		if err := speechToText.DeepgramClient(ctx, audioStream, callUUID); err != nil {
			log.Printf("Deepgram transcription failed for call %s: %v", callUUID, err)
		} else {
			log.Printf("Deepgram transcription completed for call: %s", callUUID)
		}
	case "openai":
		if err := speechToText.OpenAIClient(ctx, audioStream, callUUID); err != nil {
			log.Printf("OpenAI transcription failed for call %s: %v", callUUID, err)
		} else {
			log.Printf("OpenAI transcription completed for call: %s", callUUID)
		}
	default:
		log.Printf("Unknown provider '%s' for call: %s", provider, callUUID)
	}
}

// Define a global variable for the SpeechToTextProvider
var speechToText = SpeechToTextProvider{
	GoogleClient:   streamToGoogleSpeech,
	DeepgramClient: streamToDeepgram,
	OpenAIClient:   streamToOpenAI,
}
