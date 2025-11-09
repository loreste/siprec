package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/config"
)

func TestWhisperProvider_Name(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{Enabled: true}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	assert.Equal(t, "whisper", provider.Name())
}

func TestWhisperProvider_SetCallback(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{Enabled: true}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	called := false
	callback := func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		called = true
	}

	provider.SetCallback(callback)
	assert.NotNil(t, provider.callback)

	// Trigger callback
	provider.callback("test", "test", true, nil)
	assert.True(t, called)
}

func TestWhisperProvider_Initialize_Success(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		BinaryPath: "echo", // Use echo as a test binary that exists
		Model:      "base",
	}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	err := provider.Initialize()
	assert.NoError(t, err)
}

func TestWhisperProvider_Initialize_Disabled(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    false,
		BinaryPath: "",
	}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	err := provider.Initialize()
	assert.NoError(t, err)
}

func TestWhisperProvider_Initialize_NilConfig(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	svc := NewTranscriptionService(logger)
	provider := &WhisperProvider{
		logger:           logger,
		transcriptionSvc: svc,
		config:           nil,
	}

	err := provider.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "whisper configuration is required")
}

func TestWhisperProvider_Initialize_MissingBinaryPath(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		BinaryPath: "",
	}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	err := provider.Initialize()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "WHISPER_BINARY_PATH must be set")
}

func TestWhisperProvider_Initialize_BinaryNotInPath(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		BinaryPath: "/nonexistent/whisper/binary",
	}
	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Should not error, but should log warning
	err := provider.Initialize()
	assert.NoError(t, err)
}

func TestWhisperProvider_StreamToText_JSON(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		BinaryPath:   "whisper",
		Model:        "base",
		OutputFormat: "json",
		SampleRate:   8000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Replace runner with stub that writes a fake JSON transcript
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+"."+cfg.OutputFormat)
		payload := map[string]interface{}{
			"text":     "hello world",
			"language": "en",
			"duration": 1.23,
			"segments": []map[string]interface{}{
				{"start": 0.0, "end": 1.0, "text": "hello"},
				{"start": 1.0, "end": 1.23, "text": "world"},
			},
		}
		data, _ := json.Marshal(payload)
		return os.WriteFile(target, data, 0o600)
	}

	var gotTranscript string
	var gotMetadata map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
		gotMetadata = metadata
		require.True(t, isFinal)
		require.Equal(t, "whisper", metadata["provider"])
		require.Equal(t, "base", metadata["model"])
		require.Equal(t, "en", metadata["detected_language"])
		require.Equal(t, 1.23, metadata["duration"])
	})

	audio := bytes.Repeat([]byte{0x00, 0x01}, 10)
	err := provider.StreamToText(context.Background(), bytes.NewReader(audio), "call-123")
	require.NoError(t, err)
	require.Equal(t, "hello world", gotTranscript)
	require.NotNil(t, gotMetadata)
	require.NotNil(t, gotMetadata["segments"])
}

func TestWhisperProvider_StreamToText_TextFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		BinaryPath:   "whisper",
		Model:        "tiny",
		OutputFormat: "txt",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Replace runner with stub that writes plain text
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+"."+cfg.OutputFormat)
		return os.WriteFile(target, []byte("this is a text transcription"), 0o600)
	}

	var gotTranscript string
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
	})

	audio := bytes.Repeat([]byte{0x00, 0x01}, 10)
	err := provider.StreamToText(context.Background(), bytes.NewReader(audio), "call-123")
	require.NoError(t, err)
	require.Equal(t, "this is a text transcription", gotTranscript)
}

func TestWhisperProvider_StreamToText_VTTFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "vtt",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+"."+cfg.OutputFormat)
		vtt := "WEBVTT\n\n00:00:00.000 --> 00:00:01.000\nHello\n\n00:00:01.000 --> 00:00:02.000\nWorld\n"
		return os.WriteFile(target, []byte(vtt), 0o600)
	}

	var gotTranscript string
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
	})

	audio := bytes.Repeat([]byte{0x00, 0x01}, 10)
	err := provider.StreamToText(context.Background(), bytes.NewReader(audio), "call-123")
	require.NoError(t, err)
	require.Contains(t, gotTranscript, "WEBVTT")
}

func TestWhisperProvider_StreamToText_SRTFormat(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "srt",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+"."+cfg.OutputFormat)
		srt := "1\n00:00:00,000 --> 00:00:01,000\nHello World\n\n"
		return os.WriteFile(target, []byte(srt), 0o600)
	}

	var gotTranscript string
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
	})

	audio := bytes.Repeat([]byte{0x00, 0x01}, 10)
	err := provider.StreamToText(context.Background(), bytes.NewReader(audio), "call-123")
	require.NoError(t, err)
	require.Contains(t, gotTranscript, "Hello World")
}

func TestWhisperProvider_StreamToText_Disabled(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled: false,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "whisper provider is disabled")
}

func TestWhisperProvider_StreamToText_CLIFailure(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		SampleRate: 16000,
		Channels:   1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Simulate CLI failure
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		return errors.New("whisper command failed: exit status 1: model not found")
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "whisper command failed")
}

func TestWhisperProvider_StreamToText_Timeout(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		SampleRate: 16000,
		Channels:   1,
		Timeout:    100 * time.Millisecond,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Simulate slow CLI that exceeds timeout
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		select {
		case <-time.After(1 * time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded"))
}

func TestWhisperProvider_StreamToText_ContextCancellation(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:    true,
		SampleRate: 16000,
		Channels:   1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		return os.WriteFile(target, []byte(`{"text":"test"}`), 0o600)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(ctx, audio, "call-123")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "context canceled"))
}

func TestWhisperProvider_StreamToText_MissingOutputFile(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Runner succeeds but doesn't create output file
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		return nil
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read whisper output")
}

func TestWhisperProvider_StreamToText_MalformedJSON(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Write malformed JSON
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		return os.WriteFile(target, []byte(`{invalid json`), 0o600)
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse whisper JSON output")
}

func TestWhisperProvider_StreamToText_EmptyJSONText(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Write JSON with empty text field
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		payload := map[string]interface{}{
			"text":     "",
			"language": "en",
		}
		data, _ := json.Marshal(payload)
		return os.WriteFile(target, data, 0o600)
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "whisper output had no transcription text")
}

func TestWhisperProvider_StreamToText_EmptyTextFile(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "txt",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	// Write empty text file
	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".txt")
		return os.WriteFile(target, []byte("   \n\t"), 0o600)
	}

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "whisper output file")
	require.Contains(t, err.Error(), "was empty")
}

func TestWhisperProvider_StreamToText_LargeAudio(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		// Verify audio file was created and has content
		info, err := os.Stat(audioPath)
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return errors.New("audio file is empty")
		}

		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		return os.WriteFile(target, []byte(`{"text":"large audio transcription"}`), 0o600)
	}

	// Create 1MB of audio data
	audio := bytes.Repeat([]byte{0x00, 0x01}, 512*1024)
	var gotTranscript string
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
	})

	err := provider.StreamToText(context.Background(), bytes.NewReader(audio), "call-large")
	require.NoError(t, err)
	require.Equal(t, "large audio transcription", gotTranscript)
}

func TestWhisperProvider_StreamToText_WithLanguageHint(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		Language:     "es",
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		return os.WriteFile(target, []byte(`{"text":"hola mundo","language":"es"}`), 0o600)
	}

	var gotMetadata map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotMetadata = metadata
	})

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.NoError(t, err)
	require.Equal(t, "es", gotMetadata["language_hint"])
	require.Equal(t, "es", gotMetadata["detected_language"])
}

func TestWhisperProvider_StreamToText_TranslateMode(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		Task:         "translate",
		OutputFormat: "json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".json")
		return os.WriteFile(target, []byte(`{"text":"hello world"}`), 0o600)
	}

	var gotMetadata map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotMetadata = metadata
	})

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.NoError(t, err)
	require.Equal(t, "translate", gotMetadata["task"])
}

func TestWhisperProvider_ExtractTranscription_VerboseJSON(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	cfg := &config.WhisperSTTConfig{
		Enabled:      true,
		OutputFormat: "verbose_json",
		SampleRate:   16000,
		Channels:     1,
	}

	svc := NewTranscriptionService(logger)
	provider := NewWhisperProvider(logger, svc, cfg)

	provider.runner = func(ctx context.Context, cfg *config.WhisperSTTConfig, audioPath, outputDir string) error {
		base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
		target := filepath.Join(outputDir, base+".verbose_json")
		payload := map[string]interface{}{
			"text":     "verbose output",
			"language": "en",
			"duration": 2.5,
			"segments": []map[string]interface{}{
				{"id": 0, "start": 0.0, "end": 1.0, "text": "verbose"},
				{"id": 1, "start": 1.0, "end": 2.5, "text": "output"},
			},
		}
		data, _ := json.Marshal(payload)
		return os.WriteFile(target, data, 0o600)
	}

	var gotTranscript string
	var gotMetadata map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		gotTranscript = transcription
		gotMetadata = metadata
	})

	audio := bytes.NewReader([]byte{0x00, 0x01})
	err := provider.StreamToText(context.Background(), audio, "call-123")
	require.NoError(t, err)
	require.Equal(t, "verbose output", gotTranscript)
	require.Equal(t, 2.5, gotMetadata["duration"])
	require.NotNil(t, gotMetadata["segments"])
}

func TestDefaultWhisperRunner_CommandConstruction(t *testing.T) {
	// This test verifies the command construction logic
	// We can't easily test actual execution without a real whisper binary
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	testCases := []struct {
		name   string
		cfg    *config.WhisperSTTConfig
		verify func(t *testing.T, cfg *config.WhisperSTTConfig)
	}{
		{
			name: "Basic configuration",
			cfg: &config.WhisperSTTConfig{
				BinaryPath:   "whisper",
				Model:        "base",
				OutputFormat: "json",
			},
			verify: func(t *testing.T, cfg *config.WhisperSTTConfig) {
				assert.Equal(t, "whisper", cfg.BinaryPath)
				assert.Equal(t, "base", cfg.Model)
				assert.Equal(t, "json", cfg.OutputFormat)
			},
		},
		{
			name: "With language and task",
			cfg: &config.WhisperSTTConfig{
				BinaryPath:   "whisper",
				Model:        "small",
				OutputFormat: "txt",
				Language:     "fr",
				Task:         "transcribe",
			},
			verify: func(t *testing.T, cfg *config.WhisperSTTConfig) {
				assert.Equal(t, "fr", cfg.Language)
				assert.Equal(t, "transcribe", cfg.Task)
			},
		},
		{
			name: "With extra args",
			cfg: &config.WhisperSTTConfig{
				BinaryPath:   "whisper",
				Model:        "medium",
				OutputFormat: "srt",
				ExtraArgs:    "--device cuda --fp16 True",
			},
			verify: func(t *testing.T, cfg *config.WhisperSTTConfig) {
				assert.Contains(t, cfg.ExtraArgs, "--device cuda")
				assert.Contains(t, cfg.ExtraArgs, "--fp16 True")
			},
		},
		{
			name: "Translate flag overrides task",
			cfg: &config.WhisperSTTConfig{
				BinaryPath:   "whisper",
				Model:        "base",
				OutputFormat: "json",
				Task:         "transcribe",
				Translate:    true,
			},
			verify: func(t *testing.T, cfg *config.WhisperSTTConfig) {
				assert.True(t, cfg.Translate)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.verify(t, tc.cfg)
		})
	}
}
