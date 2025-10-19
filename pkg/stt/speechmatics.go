package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/config"
)

// SpeechmaticsProvider implements the Provider interface for Speechmatics.
type SpeechmaticsProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
	config           *config.SpeechmaticsSTTConfig
	httpClient       *http.Client
	callback         TranscriptionCallback
}

// NewSpeechmaticsProvider constructs a Speechmatics provider instance.
func NewSpeechmaticsProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.SpeechmaticsSTTConfig) *SpeechmaticsProvider {
	timeout := 60 * time.Second
	if cfg != nil && cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	return &SpeechmaticsProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Name returns the provider name.
func (p *SpeechmaticsProvider) Name() string {
	return "speechmatics"
}

// Initialize validates configuration for Speechmatics.
func (p *SpeechmaticsProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("Speechmatics STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Speechmatics STT is disabled, skipping initialization")
		return nil
	}

	if strings.TrimSpace(p.config.APIKey) == "" {
		return fmt.Errorf("Speechmatics API key is required when STT is enabled")
	}

	if p.config.Timeout > 0 {
		p.httpClient.Timeout = p.config.Timeout
	}

	p.logger.WithFields(logrus.Fields{
		"base_url":           p.config.BaseURL,
		"language":           p.config.Language,
		"model":              p.config.Model,
		"diarization":        p.config.EnableDiarization,
		"punctuation":        p.config.EnablePunctuation,
		"channel_separation": p.config.EnableChannelSeparation,
		"timeout_seconds":    p.httpClient.Timeout.Seconds(),
	}).Info("Speechmatics provider initialized")

	return nil
}

// StreamToText sends audio to Speechmatics and returns the transcription.
func (p *SpeechmaticsProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if p.config == nil || !p.config.Enabled {
		return fmt.Errorf("Speechmatics STT provider is disabled")
	}

	baseURL := strings.TrimRight(p.config.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://asr.api.speechmatics.com/v2"
	}

	jobURL := baseURL + "/jobs"

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()

		transcriptionConfig := map[string]interface{}{
			"type":     "transcription",
			"language": p.config.Language,
			"operating_point": map[string]interface{}{
				"model": p.config.Model,
			},
			"enable_partials": false,
		}

		if p.config.EnableDiarization {
			transcriptionConfig["diarization"] = "speaker"
		}
		if !p.config.EnablePunctuation {
			transcriptionConfig["punctuation"] = false
		}
		if p.config.EnableChannelSeparation {
			transcriptionConfig["channel_separation"] = true
		}

		configPayload, err := json.Marshal(transcriptionConfig)
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to marshal Speechmatics config: %w", err))
			return
		}

		if err := writer.WriteField("transcription_config", string(configPayload)); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to write Speechmatics config field: %w", err))
			return
		}

		fileWriter, err := writer.CreateFormFile("data_file", fmt.Sprintf("%s.wav", callUUID))
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to create Speechmatics audio part: %w", err))
			return
		}

		if _, err := io.Copy(fileWriter, audioStream); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to copy audio stream to Speechmatics payload: %w", err))
			return
		}

		if err := writer.Close(); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("failed to finalize Speechmatics payload: %w", err))
			return
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jobURL, pr)
	if err != nil {
		return fmt.Errorf("failed to create Speechmatics request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.config.APIKey))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Speechmatics API: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Speechmatics response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Speechmatics STT request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var submitResp speechmaticsJobResponse
	if err := json.Unmarshal(bodyBytes, &submitResp); err != nil {
		return fmt.Errorf("failed to decode Speechmatics response: %w", err)
	}

	transcript := extractSpeechmaticsTranscript(&submitResp)
	if transcript == "" && submitResp.ID != "" {
		// Fetch transcript text from job endpoint if not provided inline.
		text, fetchErr := p.fetchTranscript(ctx, baseURL, submitResp.ID)
		if fetchErr != nil {
			return fetchErr
		}
		transcript = text
	}

	if transcript == "" {
		return fmt.Errorf("Speechmatics response did not include transcription text")
	}

	metadata := map[string]interface{}{
		"provider":           p.Name(),
		"language":           p.config.Language,
		"model":              p.config.Model,
		"diarization":        p.config.EnableDiarization,
		"punctuation":        p.config.EnablePunctuation,
		"channel_separation": p.config.EnableChannelSeparation,
	}

	if submitResp.ID != "" {
		metadata["job_id"] = submitResp.ID
	}
	if submitResp.Duration > 0 {
		metadata["duration"] = submitResp.Duration
	}

	p.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"language":  metadata["language"],
		"model":     metadata["model"],
	}).Info("Speechmatics transcription completed")

	if p.callback != nil {
		p.callback(callUUID, transcript, true, metadata)
	}

	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
	}

	return nil
}

// SetCallback sets the callback function for transcription results.
func (p *SpeechmaticsProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}

func (p *SpeechmaticsProvider) fetchTranscript(ctx context.Context, baseURL, jobID string) (string, error) {
	transcriptURL := strings.TrimRight(baseURL, "/") + path.Clean("/jobs/"+jobID+"/transcript?format=txt")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, transcriptURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create Speechmatics transcript request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(p.config.APIKey))
	req.Header.Set("Accept", "text/plain")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Speechmatics transcript: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		return "", fmt.Errorf("Speechmatics transcript request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	textBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Speechmatics transcript body: %w", err)
	}

	return strings.TrimSpace(string(textBytes)), nil
}

func extractSpeechmaticsTranscript(resp *speechmaticsJobResponse) string {
	if resp == nil {
		return ""
	}

	// Attempt to read aggregated results
	if len(resp.Results) > 0 {
		var buffer bytes.Buffer
		for _, result := range resp.Results {
			for _, alternative := range result.Alternatives {
				if altText := strings.TrimSpace(alternative.Text); altText != "" {
					if buffer.Len() > 0 {
						buffer.WriteString(" ")
					}
					buffer.WriteString(altText)
				}
			}
		}
		if buffer.Len() > 0 {
			return buffer.String()
		}
	}

	return strings.TrimSpace(resp.Summary)
}

type speechmaticsJobResponse struct {
	ID       string                    `json:"id"`
	Summary  string                    `json:"summary"`
	Duration float64                   `json:"audio_duration"`
	Results  []speechmaticsResultChunk `json:"results"`
}

type speechmaticsResultChunk struct {
	Alternatives []speechmaticsAlternative `json:"alternatives"`
}

type speechmaticsAlternative struct {
	Text string `json:"text"`
}
