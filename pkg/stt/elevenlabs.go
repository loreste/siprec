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
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/config"
)

// ElevenLabsProvider implements speech-to-text support for ElevenLabs.
type ElevenLabsProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
	config           *config.ElevenLabsSTTConfig
	httpClient       *http.Client
	callback         TranscriptionCallback
}

// NewElevenLabsProvider creates a new ElevenLabs provider instance.
func NewElevenLabsProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.ElevenLabsSTTConfig) *ElevenLabsProvider {
	timeout := 45 * time.Second
	if cfg != nil && cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	return &ElevenLabsProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the provider name.
func (p *ElevenLabsProvider) Name() string {
	return "elevenlabs"
}

// Initialize validates configuration and prepares the provider.
func (p *ElevenLabsProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("ElevenLabs STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("ElevenLabs STT is disabled, skipping initialization")
		return nil
	}

	if p.config.APIKey == "" {
		return fmt.Errorf("ElevenLabs API key is required when STT is enabled")
	}

	if p.config.Timeout > 0 {
		p.httpClient.Timeout = p.config.Timeout
	}

	p.logger.WithFields(logrus.Fields{
		"base_url":          p.config.BaseURL,
		"model_id":          p.config.ModelID,
		"language":          p.config.Language,
		"diarization":       p.config.EnableDiarization,
		"word_timestamps":   p.config.EnableTimestamps,
		"smart_punctuation": p.config.EnablePunctuation,
		"paragraphs":        p.config.EnableParagraphs,
		"timeout_seconds":   p.httpClient.Timeout.Seconds(),
	}).Info("ElevenLabs provider initialized")

	return nil
}

// StreamToText sends audio to ElevenLabs for transcription.
func (p *ElevenLabsProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if p.config == nil || !p.config.Enabled {
		return fmt.Errorf("ElevenLabs STT provider is disabled")
	}

	baseURL := strings.TrimRight(p.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.elevenlabs.io"
	}
	apiURL := baseURL + "/v1/speech-to-text"

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()

		writeField := func(field, value string) error {
			if value == "" {
				return nil
			}
			return writer.WriteField(field, value)
		}

		var err error
		if err = writeField("model_id", p.config.ModelID); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write model_id: %w", err))
			return
		}
		if err = writeField("language", p.config.Language); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write language: %w", err))
			return
		}
		if err = writeField("diarize", strconv.FormatBool(p.config.EnableDiarization)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write diarize flag: %w", err))
			return
		}
		if err = writeField("timestamps", strconv.FormatBool(p.config.EnableTimestamps)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write timestamps flag: %w", err))
			return
		}
		if err = writeField("punctuate", strconv.FormatBool(p.config.EnablePunctuation)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write punctuate flag: %w", err))
			return
		}
		if err = writeField("paragraphs", strconv.FormatBool(p.config.EnableParagraphs)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write paragraphs flag: %w", err))
			return
		}

		var fileWriter io.Writer
		if fileWriter, err = writer.CreateFormFile("file", fmt.Sprintf("%s.wav", callUUID)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to create ElevenLabs audio part: %w", err))
			return
		}

		if _, err = io.Copy(fileWriter, audioStream); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to copy audio stream to ElevenLabs payload: %w", err))
			return
		}

		if err = writer.Close(); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to finalize ElevenLabs payload: %w", err))
			return
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create ElevenLabs request: %w", err)
	}

	req.Header.Set("xi-api-key", p.config.APIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call ElevenLabs speech-to-text API: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read ElevenLabs response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ElevenLabs STT request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return fmt.Errorf("failed to decode ElevenLabs response: %w", err)
	}

	transcript, _ := result["text"].(string)
	if transcript == "" {
		return fmt.Errorf("ElevenLabs response did not include transcription text")
	}

	metadata := map[string]interface{}{
		"provider":          p.Name(),
		"model_id":          p.config.ModelID,
		"language":          p.config.Language,
		"diarization":       p.config.EnableDiarization,
		"word_timestamps":   p.config.EnableTimestamps,
		"smart_punctuation": p.config.EnablePunctuation,
		"paragraphs":        p.config.EnableParagraphs,
	}

	for _, key := range []string{"duration", "words", "segments", "paragraphs", "confidence"} {
		if value, ok := result[key]; ok {
			metadata[key] = value
		}
	}

	p.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"language":  metadata["language"],
		"model_id":  metadata["model_id"],
	}).Info("ElevenLabs transcription completed")

	if p.callback != nil {
		p.callback(callUUID, transcript, true, metadata)
	}

	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
	}

	return nil
}

// SetCallback registers a callback for transcription results.
func (p *ElevenLabsProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}
