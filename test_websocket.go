package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/stt"
)

// EmptyReader is a mock reader that returns no data
type EmptyReader struct{}

func (r *EmptyReader) Read(p []byte) (n int, err error) {
	// Sleep to prevent high CPU usage
	time.Sleep(100 * time.Millisecond)
	return 0, nil
}

func main() {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Create transcription service
	transcriptionSvc := stt.NewTranscriptionService(logger)

	// Create mock provider
	mockProvider := stt.NewMockProvider(logger)
	mockProvider.SetTranscriptionService(transcriptionSvc)
	mockProvider.Initialize()

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a channel to receive signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Generate test call UUIDs
	testCalls := []string{
		uuid.New().String(),
		uuid.New().String(),
	}

	// Log the test call UUIDs
	fmt.Println("==================================")
	fmt.Println("Test Call UUIDs for WebSocket Demo")
	fmt.Println("==================================")
	for i, callUUID := range testCalls {
		fmt.Printf("Call %d: %s\n", i+1, callUUID)
	}
	fmt.Println("==================================")
	fmt.Println("1. Go to http://localhost:9090/websocket-client in your browser")
	fmt.Println("2. Enter one of the Call UUIDs above to subscribe to a specific call")
	fmt.Println("3. Or leave empty to receive all transcriptions")
	fmt.Println("4. Click 'Connect' to start receiving transcriptions")
	fmt.Println("==================================")
	fmt.Println("Press Ctrl+C to stop the demo")
	fmt.Println("")

	// Create a listener to log transcriptions
	logger.Info("Starting mock transcription generation")
	
	// Start streaming for each test call
	for _, callUUID := range testCalls {
		go func(uuid string) {
			shortID := strings.Split(uuid, "-")[0]
			logger.WithField("call_uuid", shortID).Info("Starting mock transcription stream")
			
			// Create an empty reader that never returns EOF
			reader := &EmptyReader{}
			
			// Stream to the mock provider
			if err := mockProvider.StreamToText(ctx, reader, uuid); err != nil && err != io.EOF {
				logger.WithError(err).Error("Error streaming to mock provider")
			}
		}(callUUID)
	}

	// Wait for signal
	<-sigChan
	logger.Info("Received interrupt signal, shutting down...")
}