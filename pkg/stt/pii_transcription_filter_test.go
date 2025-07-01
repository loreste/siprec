package stt

import (
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/pii"
)

// MockTranscriptionListener for testing
type MockTranscriptionListener struct {
	transcriptions []struct {
		callUUID      string
		transcription string
		isFinal       bool
		metadata      map[string]interface{}
	}
	mutex sync.Mutex
}

func (m *MockTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.transcriptions = append(m.transcriptions, struct {
		callUUID      string
		transcription string
		isFinal       bool
		metadata      map[string]interface{}
	}{callUUID, transcription, isFinal, metadata})
}

func (m *MockTranscriptionListener) GetTranscriptions() []struct {
	callUUID      string
	transcription string
	isFinal       bool
	metadata      map[string]interface{}
} {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	result := make([]struct {
		callUUID      string
		transcription string
		isFinal       bool
		metadata      map[string]interface{}
	}, len(m.transcriptions))
	copy(result, m.transcriptions)
	return result
}

func TestPIITranscriptionFilter(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise in tests

	t.Run("PII detection enabled", func(t *testing.T) {
		// Create PII detector
		config := &pii.Config{
			EnabledTypes:   []pii.PIIType{pii.PIITypeSSN, pii.PIITypeCreditCard},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		detector, err := pii.NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		// Create PII filter
		filter := NewPIITranscriptionFilter(logger, detector, true)

		// Create mock listener
		mockListener := &MockTranscriptionListener{}
		filter.AddListener(mockListener)

		// Send transcription with PII
		metadata := map[string]interface{}{"test": "value"}
		filter.OnTranscription("test-call", "My SSN is 456-78-9012", true, metadata)

		// Check results
		transcriptions := mockListener.GetTranscriptions()
		if len(transcriptions) != 1 {
			t.Fatalf("Expected 1 transcription, got %d", len(transcriptions))
		}

		result := transcriptions[0]
		if result.callUUID != "test-call" {
			t.Errorf("Expected callUUID 'test-call', got '%s'", result.callUUID)
		}

		if result.transcription == "My SSN is 456-78-9012" {
			t.Error("Expected PII to be redacted")
		}

		if !result.isFinal {
			t.Error("Expected isFinal to be true")
		}

		// Check PII metadata was added
		if result.metadata["pii_detected"] != true {
			t.Error("Expected pii_detected to be true")
		}

		// Original metadata should be preserved
		if result.metadata["test"] != "value" {
			t.Error("Expected original metadata to be preserved")
		}
	})

	t.Run("PII detection disabled", func(t *testing.T) {
		// Create PII filter with detection disabled
		filter := NewPIITranscriptionFilter(logger, nil, false)

		// Create mock listener
		mockListener := &MockTranscriptionListener{}
		filter.AddListener(mockListener)

		// Send transcription with PII
		filter.OnTranscription("test-call", "My SSN is 456-78-9012", true, nil)

		// Check results
		transcriptions := mockListener.GetTranscriptions()
		if len(transcriptions) != 1 {
			t.Fatalf("Expected 1 transcription, got %d", len(transcriptions))
		}

		result := transcriptions[0]
		if result.transcription != "My SSN is 456-78-9012" {
			t.Error("Expected transcription to pass through unchanged when PII detection is disabled")
		}
	})

	t.Run("Concurrent access safety", func(t *testing.T) {
		// Create PII detector
		config := &pii.Config{
			EnabledTypes:   []pii.PIIType{pii.PIITypeSSN},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		detector, err := pii.NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		// Create PII filter
		filter := NewPIITranscriptionFilter(logger, detector, true)

		// Create multiple mock listeners
		numListeners := 5
		listeners := make([]*MockTranscriptionListener, numListeners)
		for i := 0; i < numListeners; i++ {
			listeners[i] = &MockTranscriptionListener{}
			filter.AddListener(listeners[i])
		}

		// Send transcriptions concurrently
		numGoroutines := 10
		numTranscriptions := 100
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numTranscriptions; j++ {
					callUUID := "test-call"
					transcription := "My SSN is 456-78-9012"
					metadata := map[string]interface{}{
						"goroutine": goroutineID,
						"seq":       j,
					}
					filter.OnTranscription(callUUID, transcription, true, metadata)
				}
			}(i)
		}

		// Wait for all goroutines to complete
		wg.Wait()

		// Check that all listeners received all transcriptions
		expectedCount := numGoroutines * numTranscriptions
		for i, listener := range listeners {
			transcriptions := listener.GetTranscriptions()
			if len(transcriptions) != expectedCount {
				t.Errorf("Listener %d: expected %d transcriptions, got %d", i, expectedCount, len(transcriptions))
			}

			// Verify PII was redacted in all transcriptions
			for _, trans := range transcriptions {
				if trans.transcription == "My SSN is 456-78-9012" {
					t.Errorf("Listener %d: PII was not redacted in transcription", i)
				}
				if trans.metadata["pii_detected"] != true {
					t.Errorf("Listener %d: PII detection metadata missing", i)
				}
			}
		}
	})

	t.Run("Metadata isolation", func(t *testing.T) {
		// Create PII detector
		config := &pii.Config{
			EnabledTypes:   []pii.PIIType{pii.PIITypeSSN},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		detector, err := pii.NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		// Create PII filter
		filter := NewPIITranscriptionFilter(logger, detector, true)

		// Create multiple mock listeners
		listener1 := &MockTranscriptionListener{}
		listener2 := &MockTranscriptionListener{}
		filter.AddListener(listener1)
		filter.AddListener(listener2)

		// Send transcription with metadata
		originalMetadata := map[string]interface{}{"test": "value"}
		filter.OnTranscription("test-call", "My SSN is 456-78-9012", true, originalMetadata)

		// Get results from both listeners
		trans1 := listener1.GetTranscriptions()[0]
		trans2 := listener2.GetTranscriptions()[0]

		// Modify metadata from first listener
		trans1.metadata["modified"] = "by_listener1"

		// Check that second listener's metadata wasn't affected
		if trans2.metadata["modified"] != nil {
			t.Errorf("Metadata was not properly isolated between listeners: trans2.metadata = %+v", trans2.metadata)
		}

		// Original metadata should remain unchanged
		if originalMetadata["modified"] != nil {
			t.Error("Original metadata was modified")
		}
	})

	t.Run("Stats method", func(t *testing.T) {
		// Create PII detector
		config := &pii.Config{
			EnabledTypes:   []pii.PIIType{pii.PIITypeSSN, pii.PIITypeCreditCard},
			RedactionChar:  "*",
			PreserveFormat: true,
		}
		detector, err := pii.NewPIIDetector(logger, config)
		if err != nil {
			t.Fatalf("Failed to create PII detector: %v", err)
		}

		// Create PII filter
		filter := NewPIITranscriptionFilter(logger, detector, true)

		// Add some listeners
		filter.AddListener(&MockTranscriptionListener{})
		filter.AddListener(&MockTranscriptionListener{})

		// Get stats
		stats := filter.GetStats()

		if stats["enabled"] != true {
			t.Error("Expected enabled to be true")
		}

		if stats["listener_count"] != 2 {
			t.Errorf("Expected listener_count to be 2, got %v", stats["listener_count"])
		}

		// Check that detector stats are included
		if stats["detector_enabled_types"] == nil {
			t.Error("Expected detector stats to be included")
		}
	})
}

func TestPIIFilterWithEmptyTranscription(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Create PII detector
	config := &pii.Config{
		EnabledTypes:   []pii.PIIType{pii.PIITypeSSN},
		RedactionChar:  "*",
		PreserveFormat: true,
	}
	detector, err := pii.NewPIIDetector(logger, config)
	if err != nil {
		t.Fatalf("Failed to create PII detector: %v", err)
	}

	// Create PII filter
	filter := NewPIITranscriptionFilter(logger, detector, true)

	// Create mock listener
	mockListener := &MockTranscriptionListener{}
	filter.AddListener(mockListener)

	// Send empty transcription
	filter.OnTranscription("test-call", "", true, nil)

	// Check that no transcription was forwarded
	transcriptions := mockListener.GetTranscriptions()
	if len(transcriptions) != 0 {
		t.Errorf("Expected no transcriptions for empty input, got %d", len(transcriptions))
	}
}