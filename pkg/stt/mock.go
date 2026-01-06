package stt

import (
	"context"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// MockProvider implements a mock speech-to-text provider for testing
type MockProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
}

// NewMockProvider creates a new mock provider
func NewMockProvider(logger *logrus.Logger) *MockProvider {
	return &MockProvider{
		logger: logger,
	}
}

// SetTranscriptionService sets the transcription service
func (p *MockProvider) SetTranscriptionService(svc *TranscriptionService) {
	p.transcriptionSvc = svc
}

// Name returns the provider name
func (p *MockProvider) Name() string {
	return "mock"
}

// Initialize initializes the mock provider
func (p *MockProvider) Initialize() error {
	p.logger.Info("Mock STT provider initialized")
	return nil
}

// StreamToText simulates processing an audio stream and returning mock transcriptions
func (p *MockProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	p.logger.WithField("call_uuid", callUUID).Info("Mock STT provider processing audio stream")

	// Create a ticker to simulate real-time transcription
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Sample mock responses
	mockTranscriptions := []string{
		"Hello, this is a test transcription.",
		"The quick brown fox jumps over the lazy dog.",
		"Speech to text conversion is working in real-time via WebSockets.",
		"This is a mock transcription provider for demonstration purposes.",
		"WebSockets allow real-time communication between the server and client.",
		"SIPREC is a SIP-based protocol for recording calls in contact centers.",
		"Real-time transcription allows for immediate analysis of conversations.",
		"The transcription service publishes both interim and final results.",
		"Subscribers can filter transcriptions by specific call UUIDs.",
	}

	transcriptionIndex := 0

	// Create a channel to signal when streaming is done
	streamDone := make(chan struct{})

	// Simulate reading from the audio stream
	go func() {
		defer close(streamDone)
		buffer := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Just read and discard data from the stream
				_, err := audioStream.Read(buffer)
				if err != nil {
					if err != io.EOF {
						p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Error reading audio stream")
					}
					return
				}
				// Sleep to avoid high CPU usage
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// Simulate generating transcriptions at intervals
	for {
		select {
		case <-ctx.Done():
			p.logger.WithField("call_uuid", callUUID).Info("Mock STT processing stopped")
			return nil
		case <-streamDone:
			// Emit at least one transcription when stream ends
			if p.transcriptionSvc != nil {
				finalTranscription := mockTranscriptions[transcriptionIndex]
				p.transcriptionSvc.PublishTranscription(callUUID, finalTranscription, true, map[string]interface{}{
					"provider":   p.Name(),
					"confidence": 0.95,
					"word_count": len(strings.Fields(finalTranscription)),
					"final":      true,
				})
				p.logger.WithFields(logrus.Fields{
					"call_uuid":     callUUID,
					"transcription": finalTranscription,
				}).Info("Mock final transcription emitted on stream end")
			}
			p.logger.WithField("call_uuid", callUUID).Info("Mock STT stream finished (EOF)")
			return nil
		case <-ticker.C:
			transcription := mockTranscriptions[transcriptionIndex]
			transcriptionIndex = (transcriptionIndex + 1) % len(mockTranscriptions)

			p.logger.WithFields(logrus.Fields{
				"call_uuid":     callUUID,
				"transcription": transcription,
			}).Info("Mock transcription generated")

			// First send an interim transcription (incomplete)
			if p.transcriptionSvc != nil {
				words := strings.Split(transcription, " ")
				if len(words) > 3 {
					interim := strings.Join(words[:len(words)/2], " ")
					p.transcriptionSvc.PublishTranscription(callUUID, interim, false, map[string]interface{}{
						"provider": p.Name(),
						"interim":  true,
					})

					// Wait a bit before sending final, but respect context/done channels
					select {
					case <-ctx.Done():
						return nil
					case <-streamDone:
						return nil
					case <-time.After(time.Duration(500+rand.Intn(1500)) * time.Millisecond):
						// Continue
					}
				}

				// Then send the final transcription
				p.transcriptionSvc.PublishTranscription(callUUID, transcription, true, map[string]interface{}{
					"provider":   p.Name(),
					"confidence": 0.95,
					"word_count": len(strings.Fields(transcription)),
				})
			}
		}
	}
}
