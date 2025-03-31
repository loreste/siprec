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

// DeepgramProvider implements the Provider interface for Deepgram
type DeepgramProvider struct {
	logger *logrus.Logger
	apiKey string
	apiURL string
}

// NewDeepgramProvider creates a new Deepgram provider
func NewDeepgramProvider(logger *logrus.Logger) *DeepgramProvider {
	return &DeepgramProvider{
		logger: logger,
		apiURL: "https://api.deepgram.com/v1/listen",
	}
}

// Name returns the provider name
func (p *DeepgramProvider) Name() string {
	return "deepgram"
}

// Initialize initializes the Deepgram client
func (p *DeepgramProvider) Initialize() error {
	p.apiKey = os.Getenv("DEEPGRAM_API_KEY")
	if p.apiKey == "" {
		return fmt.Errorf("DEEPGRAM_API_KEY is not set in the environment")
	}
	p.logger.Info("Deepgram provider initialized successfully")
	return nil
}

// DeepgramResponse defines the structure for the Deepgram API response
type DeepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// StreamToText streams audio data to Deepgram
func (p *DeepgramProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", p.apiURL, audioStream)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set necessary headers for the request
	req.Header.Set("Authorization", "Token "+p.apiKey)
	req.Header.Set("Content-Type", "audio/wav")

	// Add query parameters to the request URL
	query := req.URL.Query()
	query.Add("model", "general")
	query.Add("language", "en")
	query.Add("punctuate", "true")
	query.Add("diarize", "false")
	req.URL.RawQuery = query.Encode()

	// Send the request to Deepgram
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to Deepgram: %w", err)
	}
	defer resp.Body.Close()

	// Check for non-200 response status
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deepgram API returned non-200 status code: %d", resp.StatusCode)
	}

	// Parse the response body
	var deepgramResp DeepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&deepgramResp); err != nil {
		return fmt.Errorf("failed to decode Deepgram response: %w", err)
	}

	// Extract transcription if available and log it
	if len(deepgramResp.Results.Channels) > 0 && len(deepgramResp.Results.Channels[0].Alternatives) > 0 {
		transcript := deepgramResp.Results.Channels[0].Alternatives[0].Transcript
		p.logger.WithFields(logrus.Fields{
			"transcript": transcript,
			"call_uuid":  callUUID,
		}).Info("Transcription received from Deepgram")

		// Send transcription to message queue or callback
		// Implementation will be added later
	}

	return nil
}
