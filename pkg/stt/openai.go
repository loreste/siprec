package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
	config           *config.OpenAISTTConfig
	callback         TranscriptionCallback
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.OpenAISTTConfig) *OpenAIProvider {
	return &OpenAIProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
	}
}

// Name returns the provider name
func (p *OpenAIProvider) Name() string {
	return "openai"
}

// Initialize initializes the OpenAI client
func (p *OpenAIProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("OpenAI STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("OpenAI STT is disabled, skipping initialization")
		return nil
	}

	if p.config.APIKey == "" {
		return fmt.Errorf("OpenAI API key is required")
	}

	p.logger.WithFields(logrus.Fields{
		"base_url":        p.config.BaseURL,
		"model":           p.config.Model,
		"response_format": p.config.ResponseFormat,
		"language":        p.config.Language,
		"temperature":     p.config.Temperature,
	}).Info("OpenAI provider initialized successfully")
	return nil
}

// StreamToText streams audio data to OpenAI
func (p *OpenAIProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	apiURL := strings.TrimRight(p.config.BaseURL, "/") + "/audio/transcriptions"

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()

		writeField := func(field, value string) error {
			if value == "" {
				return nil
			}
			if err := writer.WriteField(field, value); err != nil {
				return err
			}
			return nil
		}

		if err := writeField("model", p.config.Model); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write model field: %w", err))
			return
		}
		if err := writeField("response_format", p.config.ResponseFormat); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write response_format field: %w", err))
			return
		}
		if err := writeField("language", p.config.Language); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write language field: %w", err))
			return
		}
		if err := writeField("prompt", p.config.Prompt); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to write prompt field: %w", err))
			return
		}
		if p.config.Temperature > 0 {
			if err := writeField("temperature", strconv.FormatFloat(p.config.Temperature, 'f', -1, 64)); err != nil {
				pw.CloseWithError(fmt.Errorf("failed to write temperature field: %w", err))
				return
			}
		}

		fileWriter, err := writer.CreateFormFile("file", fmt.Sprintf("%s.wav", callUUID))
		if err != nil {
			pw.CloseWithError(fmt.Errorf("failed to create file part: %w", err))
			return
		}

		if _, err := io.Copy(fileWriter, audioStream); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to copy audio stream: %w", err))
			return
		}

		if err := writer.Close(); err != nil {
			pw.CloseWithError(fmt.Errorf("failed to finalize multipart payload: %w", err))
			return
		}
	}()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create OpenAI request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if p.config.OrganizationID != "" {
		req.Header.Set("OpenAI-Organization", p.config.OrganizationID)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request to OpenAI Whisper API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OpenAI Whisper API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode OpenAI response: %w", err)
	}

	if transcript, ok := result["text"].(string); ok && transcript != "" {
		// Create metadata
		metadata := map[string]interface{}{
			"provider":        p.Name(),
			"model":           p.config.Model,
			"word_count":      len(strings.Fields(transcript)),
			"response_format": p.config.ResponseFormat,
			"language":        p.config.Language,
			"temperature":     p.config.Temperature,
		}

		// Add additional metadata from verbose response
		if p.config.ResponseFormat == "verbose_json" {
			if duration, ok := result["duration"].(float64); ok {
				metadata["duration"] = duration
			}
			if language, ok := result["language"].(string); ok {
				metadata["detected_language"] = language
			}
			if segments, ok := result["segments"].([]interface{}); ok {
				metadata["segments"] = segments
			}
			if words, ok := result["words"].([]interface{}); ok {
				metadata["words"] = words
			}
		}

		p.logger.WithFields(logrus.Fields{
			"call_uuid":     callUUID,
			"transcription": transcript,
			"duration":      metadata["duration"],
			"language":      metadata["detected_language"],
		}).Info("OpenAI transcription received")

		// Call callback if available
		if p.callback != nil {
			p.callback(callUUID, transcript, true, metadata)
		}

		// Publish to transcription service for real-time streaming
		if p.transcriptionSvc != nil {
			p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
		}
	} else {
		return fmt.Errorf("no transcription found in OpenAI response")
	}

	return nil
}

// SetCallback sets the callback function for transcription results
func (p *OpenAIProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}
