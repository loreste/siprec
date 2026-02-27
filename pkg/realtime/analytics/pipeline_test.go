package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestPipelineProcessesEvent(t *testing.T) {
	store := NewMockStateStore()
	pipeline := NewPipeline(logrus.New(), store,
		NewSentimentProcessor(),
		NewKeywordProcessor([]string{"the", "and"}),
		NewComplianceProcessor([]ComplianceRule{
			{ID: "rule1", Description: "Must mention policy", Severity: "high", Contains: []string{"policy"}},
		}),
		NewAgentMetricsProcessor([]string{"agent"}),
	)

	event := &TranscriptEvent{
		CallID:     "call-123",
		Speaker:    "agent",
		Text:       "Our policy is great",
		IsFinal:    false,
		Confidence: 0.9,
		Timestamp:  time.Now(),
	}

	snapshot, err := pipeline.Process(context.Background(), event)
	if err != nil {
		t.Fatalf("pipeline process failed: %v", err)
	}
	if snapshot == nil {
		t.Fatalf("expected snapshot, got nil")
	}

	if len(snapshot.SentimentTrend) == 0 {
		t.Fatalf("expected sentiment trend to be recorded")
	}
	if len(snapshot.Keywords) == 0 {
		t.Fatalf("expected keywords to be recorded")
	}
}

func TestPipelineHandlesNilEvent(t *testing.T) {
	pipeline := NewPipeline(logrus.New(), nil)
	snapshot, err := pipeline.Process(context.Background(), nil)
	
	if err != nil {
		t.Fatalf("expected no error for nil event, got: %v", err)
	}
	if snapshot != nil {
		t.Fatalf("expected nil snapshot for nil event")
	}
}

func TestPipelineCreatesNewState(t *testing.T) {
	store := NewMockStateStore()
	pipeline := NewPipeline(logrus.New(), store)
	
	event := &TranscriptEvent{
		CallID: "new-call",
		Text:   "Hello world",
	}
	
	snapshot, err := pipeline.Process(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.CallID != "new-call" {
		t.Fatalf("expected CallID to be 'new-call', got: %s", snapshot.CallID)
	}
}

func TestPipelineCompleteCall(t *testing.T) {
	store := NewMockStateStore()
	pipeline := NewPipeline(logrus.New(), store)
	
	// Setup initial state
	state := &State{
		CallID:       "test-call",
		QualityScore: 0.85,
		Keywords:     map[string]int{"test": 1},
	}
	_ = store.Set("test-call", state)

	// Complete the call
	snapshot, err := pipeline.CompleteCall("test-call")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.QualityScore != 0.85 {
		t.Fatalf("expected quality score 0.85, got: %f", snapshot.QualityScore)
	}
	
	// Verify state was deleted by trying to get it
	deletedState, err := store.Get("test-call")
	if err != nil {
		t.Fatalf("unexpected error checking deleted state: %v", err)
	}
	if deletedState != nil {
		t.Fatal("expected state to be deleted after call completion")
	}
}

func TestPipelineContinuesOnProcessorError(t *testing.T) {
	store := NewMockStateStore()
	logger := logrus.New()
	
	// Create a processor that fails
	failingProcessor := &mockProcessor{
		processFunc: func(ctx context.Context, event *TranscriptEvent, state *State) error {
			return errors.New("processor error")
		},
	}
	
	// Create a processor that succeeds
	successProcessor := &mockProcessor{
		processFunc: func(ctx context.Context, event *TranscriptEvent, state *State) error {
			state.QualityScore = 0.75
			return nil
		},
	}
	
	pipeline := NewPipeline(logger, store, failingProcessor, successProcessor)
	
	event := &TranscriptEvent{
		CallID: "error-test",
		Text:   "Test",
	}
	
	snapshot, err := pipeline.Process(context.Background(), event)
	if err != nil {
		t.Fatalf("pipeline should not fail on processor error: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.QualityScore != 0.75 {
		t.Fatalf("expected quality score from success processor, got: %f", snapshot.QualityScore)
	}
}

// Mock processor for testing
type mockProcessor struct {
	processFunc func(ctx context.Context, event *TranscriptEvent, state *State) error
}

func (m *mockProcessor) Process(ctx context.Context, event *TranscriptEvent, state *State) error {
	if m.processFunc != nil {
		return m.processFunc(ctx, event, state)
	}
	return nil
}
