package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
)

var openaiAPIKey string

// Initialize OpenAI client
func initOpenAIClient() error {
	openaiAPIKey = os.Getenv("OPENAI_API_KEY")
	if openaiAPIKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is not set in the environment")
	}
	return nil
}

// streamToOpenAI sends audio to OpenAI's Whisper API for transcription
func streamToOpenAI(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Construct the OpenAI Whisper API request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/audio/transcriptions", audioStream)
	if err != nil {
		return fmt.Errorf("failed to create OpenAI request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+openaiAPIKey)
	req.Header.Set("Content-Type", "audio/wav")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to OpenAI Whisper API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenAI Whisper API returned non-200 status code: %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode OpenAI response: %w", err)
	}

	if transcript, ok := result["text"].(string); ok {
		logrus.WithFields(logrus.Fields{"call_uuid": callUUID, "transcription": transcript}).Info("OpenAI transcription received")
		sendTranscriptionToAMQP(transcript, callUUID)
	} else {
		return fmt.Errorf("no transcription found in OpenAI response")
	}

	return nil
}
