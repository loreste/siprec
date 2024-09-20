package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/sirupsen/logrus" // Use logrus for structured logging
)

var (
	deepgramAPIKey string
	deepgramAPIURL = "https://api.deepgram.com/v1/listen"
)

// Initialize the Deepgram client by loading the API key from the environment
func initDeepgramClient() error {
	deepgramAPIKey = os.Getenv("DEEPGRAM_API_KEY")
	if deepgramAPIKey == "" {
		return fmt.Errorf("DEEPGRAM_API_KEY is not set in the environment")
	}
	return nil
}

// Define the structure for the Deepgram API response
type DeepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// streamToDeepgram handles real-time audio streaming to the Deepgram API
func streamToDeepgram(ctx context.Context, audioStream io.Reader, callUUID string) error {
	req, err := http.NewRequestWithContext(ctx, "POST", deepgramAPIURL, audioStream)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set necessary headers for the request
	req.Header.Set("Authorization", "Token "+deepgramAPIKey)
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
		return fmt.Errorf("Deepgram API returned non-200 status code: %d", resp.StatusCode)
	}

	// Parse the response body
	var deepgramResp DeepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&deepgramResp); err != nil {
		return fmt.Errorf("failed to decode Deepgram response: %w", err)
	}

	// Extract transcription if available and log it
	if len(deepgramResp.Results.Channels) > 0 && len(deepgramResp.Results.Channels[0].Alternatives) > 0 {
		transcript := deepgramResp.Results.Channels[0].Alternatives[0].Transcript
		logrus.WithFields(logrus.Fields{
			"transcript": transcript,
			"call_uuid":  callUUID,
		}).Info("Transcription received")

		// Send transcription to AMQP
		sendTranscriptionToAMQP(transcript, callUUID)
	}

	return nil
}
