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
	logger   *logrus.Logger
	apiKey   string
	apiURL   string
	callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})
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
	RequestID string `json:"request_id"`
	Results   struct {
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
	} `json:"results"`
	Metadata struct {
		RequestID      string                 `json:"request_id"`
		TransactionKey string                 `json:"transaction_key"`
		SHA256         string                 `json:"sha256"`
		Created        string                 `json:"created"`
		Duration       float64                `json:"duration"`
		Channels       int                    `json:"channels"`
		Models         []string               `json:"models"`
		ModelInfo      map[string]interface{} `json:"model_info"`
	} `json:"metadata"`
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

	// Extract transcription if available and process it
	if len(deepgramResp.Results.Channels) > 0 && len(deepgramResp.Results.Channels[0].Alternatives) > 0 {
		alternative := deepgramResp.Results.Channels[0].Alternatives[0]
		transcript := alternative.Transcript

		if transcript != "" {
			// Create metadata
			metadata := map[string]interface{}{
				"provider":   "deepgram",
				"confidence": alternative.Confidence,
				"request_id": deepgramResp.RequestID,
				"model":      deepgramResp.Metadata.ModelInfo,
				"duration":   deepgramResp.Metadata.Duration,
				"channels":   deepgramResp.Metadata.Channels,
				"words":      alternative.Words,
				"paragraphs": alternative.Paragraphs,
			}

			// Add utterances if available
			if len(deepgramResp.Results.Utterances) > 0 {
				metadata["utterances"] = deepgramResp.Results.Utterances
			}

			p.logger.WithFields(logrus.Fields{
				"transcript": transcript,
				"call_uuid":  callUUID,
				"confidence": alternative.Confidence,
				"words":      len(alternative.Words),
			}).Info("Transcription received from Deepgram")

			// Call callback if available
			if p.callback != nil {
				p.callback(callUUID, transcript, true, metadata)
			}
		}
	}

	return nil
}

// SetCallback sets the callback function for transcription results
func (p *DeepgramProvider) SetCallback(callback func(callUUID, transcription string, isFinal bool, metadata map[string]interface{})) {
	p.callback = callback
}
