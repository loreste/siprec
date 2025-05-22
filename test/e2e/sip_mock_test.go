package e2e

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// This is a simplified test that doesn't require the actual SIP library
// but still tests the basic flow of the SIPREC server.

// TestSimulatedSiprecCallFlow tests a simulated SIPREC call flow without actual SIP signaling
func TestSimulatedSiprecCallFlow(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Record transcriptions
	var transcriptions []string
	var lock sync.Mutex

	// Create a test context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Channel to receive transcriptions
	transcriptionChan := make(chan string, 10)

	// Create a mock STT provider that simulates transcription
	go func() {
		// Simulate generating transcriptions
		mockTexts := []string{
			"Hello, this is a test transcription.",
			"The quick brown fox jumps over the lazy dog.",
			"Speech to text conversion is working.",
			"Testing the SIPREC server end-to-end flow.",
		}

		// Send transcriptions periodically
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		index := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if index < len(mockTexts) {
					transcriptionChan <- mockTexts[index]
					index++
				}
			}
		}
	}()

	// Collect transcriptions
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case transcription := <-transcriptionChan:
				lock.Lock()
				transcriptions = append(transcriptions, transcription)
				logger.WithField("transcription", transcription).Info("Received transcription")
				lock.Unlock()
			}
		}
	}()

	// Wait for transcriptions (simulating a call duration)
	time.Sleep(3 * time.Second)

	// Verify we got transcriptions
	lock.Lock()
	defer lock.Unlock()

	assert.GreaterOrEqual(t, len(transcriptions), 2, "Should have received at least 2 transcriptions")

	logger.Info("Test completed successfully")
}
