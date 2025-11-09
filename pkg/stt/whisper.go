package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/config"
	"siprec-server/pkg/media"
	"siprec-server/pkg/metrics"
)

type whisperRunner func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error

// WhisperProvider uses the open-source Whisper CLI to transcribe audio.
// The binary can run locally or on a remote server accessed via SSH/HTTP wrapper.
// BinaryPath can point to any executable that accepts Whisper CLI arguments.
type WhisperProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
	config           *config.WhisperSTTConfig
	callback         TranscriptionCallback
	runner           whisperRunner
	semaphore        chan struct{} // For rate limiting concurrent calls
}

// NewWhisperProvider constructs a Whisper provider backed by the CLI referenced in config.
func NewWhisperProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.WhisperSTTConfig) *WhisperProvider {
	// Determine the concurrency limit
	var semaphore chan struct{}
	maxConcurrent := cfg.MaxConcurrentCalls

	if maxConcurrent == -1 {
		// Auto mode: use number of CPU cores
		maxConcurrent = runtime.NumCPU()
		logger.WithField("max_concurrent", maxConcurrent).Info("Whisper rate limiting set to auto (CPU cores)")
	} else if maxConcurrent > 0 {
		logger.WithField("max_concurrent", maxConcurrent).Info("Whisper rate limiting enabled")
	} else {
		logger.Info("Whisper rate limiting disabled (unlimited concurrent calls)")
	}

	if maxConcurrent > 0 {
		semaphore = make(chan struct{}, maxConcurrent)
	}

	return &WhisperProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		runner:           defaultWhisperRunner,
		semaphore:        semaphore,
	}
}

// Name returns the provider identifier.
func (p *WhisperProvider) Name() string {
	return "whisper"
}

// SetCallback registers the callback used for real-time publication.
func (p *WhisperProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}

// Initialize validates the configuration before the provider is registered.
func (p *WhisperProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("whisper configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Whisper STT disabled; skipping initialization")
		return nil
	}

	if p.config.BinaryPath == "" {
		return fmt.Errorf("WHISPER_BINARY_PATH must be set when Whisper STT is enabled")
	}

	// Check if binary exists
	binaryPath, err := exec.LookPath(p.config.BinaryPath)
	if err != nil {
		p.logger.WithError(err).Warn("Whisper binary not found in PATH; transcription may fail at runtime")
	} else {
		// Attempt to verify binary by checking version
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		versionCmd := exec.CommandContext(ctx, binaryPath, "--version")
		output, err := versionCmd.CombinedOutput()
		if err != nil {
			// Version check failed - could be remote server or different whisper implementation
			p.logger.WithError(err).Debug("Could not verify Whisper version (this is normal for remote servers or custom wrappers)")
		} else {
			version := strings.TrimSpace(string(output))
			p.logger.WithField("version", version).Info("Whisper binary version detected")
		}
	}

	p.logger.WithFields(logrus.Fields{
		"binary":       p.config.BinaryPath,
		"model":        p.config.Model,
		"task":         p.config.Task,
		"outputFormat": p.config.OutputFormat,
	}).Info("Whisper provider initialized")
	return nil
}

// StreamToText buffers the PCM stream to a temporary WAV file and invokes the Whisper CLI.
func (p *WhisperProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if !p.config.Enabled {
		return fmt.Errorf("whisper provider is disabled")
	}

	// Acquire semaphore slot if rate limiting is enabled
	if p.semaphore != nil {
		select {
		case p.semaphore <- struct{}{}:
			defer func() { <-p.semaphore }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	audioFile, err := os.CreateTemp("", "whisper-audio-*.wav")
	if err != nil {
		return fmt.Errorf("failed to create temporary audio file: %w", err)
	}
	defer func() {
		// Track aggregate disk usage: decrement on removal
		if info, err := os.Stat(audioFile.Name()); err == nil {
			metrics.SubWhisperTempFileUsage(info.Size())
		}
		os.Remove(audioFile.Name())
	}()

	wavWriter, err := media.NewWAVWriter(audioFile, p.config.SampleRate, p.config.Channels)
	if err != nil {
		audioFile.Close()
		return fmt.Errorf("failed to initialize WAV writer: %w", err)
	}

	if err := p.bufferPCM(ctx, wavWriter, audioStream); err != nil {
		audioFile.Close()
		return err
	}

	if err := wavWriter.Finalize(); err != nil {
		audioFile.Close()
		return fmt.Errorf("failed to finalize WAV file: %w", err)
	}

	if err := audioFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp audio file: %w", err)
	}

	// Track aggregate disk usage: increment after file is written
	if info, err := os.Stat(audioFile.Name()); err == nil {
		metrics.AddWhisperTempFileUsage(info.Size())
	}

	outputDir, err := os.MkdirTemp("", "whisper-output-*")
	if err != nil {
		return fmt.Errorf("failed to create whisper output directory: %w", err)
	}
	defer os.RemoveAll(outputDir)

	runCtx := ctx
	cancel := func() {}
	if p.config.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, p.config.Timeout)
	}

	// Start metrics timer
	recordDuration := metrics.ObserveWhisperCLIDuration(p.config.Model)

	err = p.runner(runCtx, p.config, audioFile.Name(), outputDir)
	cancel()

	if err != nil {
		// Check if it was a timeout
		if errors.Is(err, context.DeadlineExceeded) {
			metrics.RecordWhisperTimeout(p.config.Model)
			recordDuration("timeout")
		} else {
			recordDuration("error")
		}
		return err
	}
	recordDuration("success")

	transcript, metadata, err := p.extractTranscription(outputDir, audioFile.Name())
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	metadata["provider"] = p.Name()
	metadata["model"] = p.config.Model
	metadata["task"] = p.config.Task
	metadata["language_hint"] = p.config.Language

	// Record output format usage
	metrics.RecordWhisperOutputFormat(p.config.OutputFormat)

	p.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"provider":  p.Name(),
		"model":     p.config.Model,
	}).Info("Whisper transcription completed")

	if p.callback != nil {
		p.callback(callUUID, transcript, true, metadata)
	}
	if p.transcriptionSvc != nil {
		p.transcriptionSvc.PublishTranscription(callUUID, transcript, true, metadata)
	}

	return nil
}

func (p *WhisperProvider) bufferPCM(ctx context.Context, wavWriter *media.WAVWriter, audioStream io.Reader) error {
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, err := audioStream.Read(buffer)
		if n > 0 {
			if _, writeErr := wavWriter.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to buffer audio for whisper: %w", writeErr)
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read PCM audio: %w", err)
		}
	}
	return nil
}

func (p *WhisperProvider) extractTranscription(outputDir, audioPath string) (string, map[string]interface{}, error) {
	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	format := strings.ToLower(p.config.OutputFormat)
	if format == "" {
		format = "json"
	}

	target := filepath.Join(outputDir, fmt.Sprintf("%s.%s", base, format))
	data, err := os.ReadFile(target)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read whisper output (%s): %w", target, err)
	}

	metadata := make(map[string]interface{})
	switch format {
	case "json", "verbose_json":
		var payload struct {
			Text     string                   `json:"text"`
			Language string                   `json:"language"`
			Duration float64                  `json:"duration"`
			Segments []map[string]interface{} `json:"segments"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return "", nil, fmt.Errorf("failed to parse whisper JSON output: %w", err)
		}
		if payload.Language != "" {
			metadata["detected_language"] = payload.Language
		}
		if payload.Duration > 0 {
			metadata["duration"] = payload.Duration
		}
		if len(payload.Segments) > 0 {
			metadata["segments"] = payload.Segments
		}
		text := strings.TrimSpace(payload.Text)
		if text == "" {
			return "", nil, fmt.Errorf("whisper output had no transcription text")
		}
		return text, metadata, nil
	default:
		text := strings.TrimSpace(string(data))
		if text == "" {
			return "", nil, fmt.Errorf("whisper output file %s was empty", target)
		}
		return text, metadata, nil
	}
}

func defaultWhisperRunner(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
	args := []string{audioPath, "--model", cfg.Model, "--output_dir", outputDir, "--output_format", cfg.OutputFormat}

	task := cfg.Task
	if cfg.Translate {
		task = "translate"
	}
	if task != "" {
		args = append(args, "--task", task)
	}

	if cfg.Language != "" {
		args = append(args, "--language", cfg.Language)
	}

	if strings.TrimSpace(cfg.ExtraArgs) != "" {
		args = append(args, strings.Fields(cfg.ExtraArgs)...)
	}

	cmd := exec.CommandContext(ctx, cfg.BinaryPath, args...)
	var combined bytes.Buffer
	cmd.Stdout = &combined
	cmd.Stderr = &combined
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("whisper command failed: %w: %s", err, combined.String())
	}
	return nil
}
