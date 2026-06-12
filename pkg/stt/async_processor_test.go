package stt

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newAsyncTestLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return logger
}

func writeTestAudioFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "audio.raw")
	// 32000 bytes == 1 second of 16-bit 16kHz mono PCM
	if err := os.WriteFile(path, make([]byte, 32000), 0o600); err != nil {
		t.Fatalf("failed to write test audio file: %v", err)
	}
	return path
}

// staticProvider is a test provider that publishes deterministic final
// transcription segments through the transcription service.
type staticProvider struct {
	name     string
	svc      *TranscriptionService
	segments []string
	metadata []map[string]interface{}
	err      error
}

func (p *staticProvider) Initialize() error { return nil }
func (p *staticProvider) Name() string      { return p.name }

func (p *staticProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if _, err := io.Copy(io.Discard, audioStream); err != nil {
		return err
	}
	if p.err != nil {
		return p.err
	}
	for i, segment := range p.segments {
		var metadata map[string]interface{}
		if i < len(p.metadata) {
			metadata = p.metadata[i]
		}
		// Synchronous publish keeps segment ordering deterministic in tests;
		// production providers publish asynchronously with real time gaps.
		p.svc.PublishTranscriptionSync(callUUID, segment, true, metadata)
		// Interleave an interim result to ensure it is ignored.
		p.svc.PublishTranscriptionSync(callUUID, "interim "+segment, false, nil)
	}
	return nil
}

func newAsyncTestProcessor(t *testing.T, provider Provider) (*AsyncSTTProcessor, *TranscriptionService) {
	t.Helper()

	logger := newAsyncTestLogger()
	manager := NewProviderManager(logger, provider.Name(), nil)
	if err := manager.RegisterProvider(provider); err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	svc := NewTranscriptionService(logger)
	t.Cleanup(svc.Shutdown)

	processor := NewAsyncSTTProcessor(manager, logger, nil)
	processor.SetTranscriptionService(svc)
	return processor, svc
}

func TestExecuteSTTJobReturnsRealTranscription(t *testing.T) {
	logger := newAsyncTestLogger()
	svc := NewTranscriptionService(logger)
	t.Cleanup(svc.Shutdown)

	provider := &staticProvider{
		name:     "static",
		svc:      svc,
		segments: []string{"hello world", "second segment here"},
		metadata: []map[string]interface{}{
			{"confidence": 0.9, "word_count": 2, "language": "en-US", "model": "test-model"},
			{"confidence": 0.7, "word_count": 3},
		},
	}

	manager := NewProviderManager(logger, "static", nil)
	if err := manager.RegisterProvider(provider); err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	processor := NewAsyncSTTProcessor(manager, logger, nil)
	processor.SetTranscriptionService(svc)

	job := &STTJob{
		ID:        "job-1",
		CallUUID:  "call-1",
		AudioPath: writeTestAudioFile(t),
		Provider:  "static",
		Language:  "en",
	}

	result, err := processor.executeSTTJob(context.Background(), job)
	if err != nil {
		t.Fatalf("executeSTTJob failed: %v", err)
	}

	if result.Text != "hello world second segment here" {
		t.Errorf("unexpected transcription text: %q", result.Text)
	}
	if result.WordCount != 5 {
		t.Errorf("expected word count 5, got %d", result.WordCount)
	}
	if want := (0.9 + 0.7) / 2; result.Confidence != want {
		t.Errorf("expected confidence %v, got %v", want, result.Confidence)
	}
	if result.Language != "en-US" {
		t.Errorf("expected language en-US, got %q", result.Language)
	}
	if result.Provider != "static" {
		t.Errorf("expected provider static, got %q", result.Provider)
	}
	if result.ModelUsed != "test-model" {
		t.Errorf("expected model test-model, got %q", result.ModelUsed)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if !result.Segments[0].IsFinal || result.Segments[0].Text != "hello world" {
		t.Errorf("unexpected first segment: %+v", result.Segments[0])
	}
	// 32000 bytes of 16-bit 16kHz mono PCM is one second of audio.
	if result.Duration != time.Second {
		t.Errorf("expected estimated duration 1s, got %v", result.Duration)
	}
	if strings.Contains(result.Text, "interim") {
		t.Errorf("interim segments must not be captured: %q", result.Text)
	}
}

func TestExecuteSTTJobProviderFailure(t *testing.T) {
	logger := newAsyncTestLogger()
	svc := NewTranscriptionService(logger)
	t.Cleanup(svc.Shutdown)

	provider := &staticProvider{
		name: "failing",
		svc:  svc,
		err:  io.ErrUnexpectedEOF,
	}

	processor, _ := newAsyncTestProcessor(t, provider)

	job := &STTJob{
		ID:        "job-2",
		CallUUID:  "call-2",
		AudioPath: writeTestAudioFile(t),
		Provider:  "failing",
	}

	result, err := processor.executeSTTJob(context.Background(), job)
	if err == nil {
		t.Fatal("expected provider failure to be returned as an error")
	}
	if result != nil {
		t.Errorf("expected nil result on provider failure, got %+v", result)
	}
}

func TestExecuteSTTJobRequiresTranscriptionService(t *testing.T) {
	logger := newAsyncTestLogger()
	svc := NewTranscriptionService(logger)
	t.Cleanup(svc.Shutdown)

	provider := &staticProvider{name: "static", svc: svc, segments: []string{"text"}}
	manager := NewProviderManager(logger, "static", nil)
	if err := manager.RegisterProvider(provider); err != nil {
		t.Fatalf("failed to register provider: %v", err)
	}

	processor := NewAsyncSTTProcessor(manager, logger, nil)

	job := &STTJob{
		ID:        "job-3",
		CallUUID:  "call-3",
		AudioPath: writeTestAudioFile(t),
		Provider:  "static",
	}

	if _, err := processor.executeSTTJob(context.Background(), job); err == nil {
		t.Fatal("expected an error when no transcription service is configured")
	}
}

func TestTranscriptionCollectorIgnoresOtherCalls(t *testing.T) {
	collector := newTranscriptionCollector("call-a")

	collector.OnTranscription("call-b", "other call", true, nil)
	collector.OnTranscription("call-a", "  ", true, nil)
	collector.OnTranscription("call-a", "kept", true, map[string]interface{}{"confidence": 1.0})

	result := collector.buildResult(&STTJob{ID: "job", CallUUID: "call-a", Provider: "p"}, 0)
	if result.Text != "kept" {
		t.Errorf("expected only matching final segments to be kept, got %q", result.Text)
	}
	if result.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %v", result.Confidence)
	}
}
