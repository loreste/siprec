package realtime

import (
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAMQPPublisher implements AMQPPublisher for testing
type MockAMQPPublisher struct {
	mu           sync.Mutex
	events       []TranscriptionEvent
	started      bool
	publishError error
	eventChan    chan TranscriptionEvent
}

func NewMockAMQPPublisher() *MockAMQPPublisher {
	return &MockAMQPPublisher{
		events:    make([]TranscriptionEvent, 0),
		started:   true,
		eventChan: make(chan TranscriptionEvent, 100),
	}
}

func (m *MockAMQPPublisher) PublishTranscriptionEvent(event TranscriptionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)

	// Non-blocking send to channel for synchronization in tests
	select {
	case m.eventChan <- event:
	default:
	}

	return m.publishError
}

func (m *MockAMQPPublisher) IsStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

func (m *MockAMQPPublisher) SetStarted(started bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = started
}

func (m *MockAMQPPublisher) GetEvents() []TranscriptionEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]TranscriptionEvent, len(m.events))
	copy(copied, m.events)
	return copied
}

func (m *MockAMQPPublisher) WaitForEvents(count int, timeout time.Duration) []TranscriptionEvent {
	deadline := time.Now().Add(timeout)
	for {
		if len(m.GetEvents()) >= count {
			return m.GetEvents()
		}
		if time.Now().After(deadline) {
			return m.GetEvents()
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStreamingTranscriber_AMQPPublishing(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create mock publisher
	mockPublisher := NewMockAMQPPublisher()

	// Create config with lower timeouts for testing
	config := DefaultStreamingConfig()
	config.ProcessingTimeout = 100 * time.Millisecond
	config.BatchSize = 1 // Process immediately

	// Create transcriber with AMQP publisher
	transcriber := NewStreamingTranscriberWithAMQP(
		"test-session",
		"test-call",
		config,
		logger,
		mockPublisher,
	)

	// Start transcriber
	err := transcriber.Start()
	require.NoError(t, err)
	defer func() { _ = transcriber.Stop() }()

	// Simulate audio processing
	// We need enough data to trigger the mock transcription logic in processAudioChunk
	// In the real code (transcription.go), it checks for len(audioData) < 1000
	audioData := make([]byte, 2000)

	err = transcriber.ProcessAudio(audioData)
	require.NoError(t, err)

	// Wait for events to be published
	// We expect multiple events: internal processing events and AMQP events
	// The StreamingTranscriber sends events to its internal channel, which then distributes to AMQP
	// 1. Session Start
	// 2. Transcription Result (async)
	events := mockPublisher.WaitForEvents(2, 2*time.Second)

	// Verify events were received
	assert.NotEmpty(t, events, "Should have received events on AMQP publisher")

	// Verify events were received
	assert.NotEmpty(t, events, "Should have received events on AMQP publisher")

	// Find the transcription event (ignoring session start/end)
	var foundTranscription bool
	for _, event := range events {
		if event.Type == EventTypePartialTranscript {
			foundTranscription = true
			assert.Equal(t, "test-session", event.SessionID)
			assert.Equal(t, "test-call", event.CallID)
			assert.Equal(t, "Mock transcription result", event.Data.Text)
			break
		}
	}
	assert.True(t, foundTranscription, "Did not find expected partial transcript event")
}

func TestStreamingTranscriber_DistributeEvent(t *testing.T) {
	// dedicated test for the distributeEvent method to ensure it calls the publisher
	logger := logrus.New()
	mockPublisher := NewMockAMQPPublisher()

	config := DefaultStreamingConfig()
	transcriber := NewStreamingTranscriberWithAMQP(
		"test-session",
		"test-call",
		config,
		logger,
		mockPublisher,
	)

	// Manually start it just to set state, though we are testing distributeEvent directly
	transcriber.isActive = true

	event := TranscriptionEvent{
		Type:      EventTypeFinalTranscript,
		SessionID: "session-123",
		CallID:    "call-123",
		Data: TranscriptionEventData{
			Text:    "Hello world",
			IsFinal: true,
		},
	}

	// Call distributeEvent directly to verify wiring
	transcriber.distributeEvent(event)

	// Wait a bit as publish is async in goroutine
	events := mockPublisher.WaitForEvents(1, 1*time.Second)

	require.Len(t, events, 1)
	assert.Equal(t, "Hello world", events[0].Data.Text)
	assert.Equal(t, EventTypeFinalTranscript, events[0].Type)
}
