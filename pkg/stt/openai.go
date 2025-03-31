package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/sirupsen/logrus"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	logger *logrus.Logger
	apiKey string
	apiURL string
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(logger *logrus.Logger) *OpenAIProvider {
	return &OpenAIProvider{
		logger: logger,
		apiURL: "https://api.openai.com/v1/audio/transcriptions",
	}
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Initialize initializes the OpenAI client
func (p *OpenAIProvider) Initialize() error {
	p.apiKey = os.Getenv("OPENAI_API_KEY")
	if p.apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is not set in the environment")
	}
	p.logger.Info("OpenAI provider initialized successfully")
	return nil
}

// StreamToText streams audio data to OpenAI
func (p *OpenAIProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	// Construct the OpenAI Whisper API request
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, audioStream)
	if err != nil {
		return fmt.Errorf("failed to create OpenAI request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
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
		p.logger.WithFields(logrus.Fields{
			"call_uuid":     callUUID,
			"transcription": transcript,
		}).Info("OpenAI transcription received")

		// Send transcription to message queue or callback
		// Implementation will be added later
	} else {
		return fmt.Errorf("no transcription found in OpenAI response")
	}

	return nil
}
