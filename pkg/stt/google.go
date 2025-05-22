package stt

import (
	"context"
	"io"
	"os"
	"strings"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
)

// GoogleProvider implements the Provider interface for Google Speech-to-Text
type GoogleProvider struct {
	logger           *logrus.Logger
	client           *speech.Client
	transcriptionSvc *TranscriptionService
}

// NewGoogleProvider creates a new Google Speech-to-Text provider
func NewGoogleProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService) *GoogleProvider {
	return &GoogleProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
	}
}

// Name returns the provider name
func (p *GoogleProvider) Name() string {
	return "google"
}

// Initialize initializes the Google Speech-to-Text client
func (p *GoogleProvider) Initialize() error {
	// Check for credentials
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") == "" {
		p.logger.Warn("GOOGLE_APPLICATION_CREDENTIALS environment variable not set")
	}

	var err error
	p.client, err = speech.NewClient(context.Background())
	if err != nil {
		p.logger.WithError(err).Error("Failed to create Google Speech client")
		return err
	}

	p.logger.Info("Google Speech-to-Text client initialized successfully")
	return nil
}

// StreamToText streams audio data to Google Speech-to-Text
func (p *GoogleProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if p.client == nil {
		return ErrInitializationFailed
	}

	stream, err := p.client.StreamingRecognize(ctx)
	if err != nil {
		p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to start Google Speech-to-Text stream")
		return err
	}

	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config: &speechpb.RecognitionConfig{
			Encoding:        speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz: 8000,
			LanguageCode:    "en-US",
		},
		InterimResults: true,
	}

	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: streamingConfig,
		},
	}); err != nil {
		p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send streaming config")
		return err
	}

	// Create channels for coordinating goroutines
	errChan := make(chan error, 2)
	doneChan := make(chan struct{})

	// Start reading from the audio stream
	go func() {
		defer close(doneChan)
		buffer := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				stream.CloseSend()
				return
			default:
				n, err := audioStream.Read(buffer)
				if err == io.EOF {
					stream.CloseSend()
					return
				}
				if err != nil {
					p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read audio stream")
					errChan <- err
					return
				}

				if err := stream.Send(&speechpb.StreamingRecognizeRequest{
					StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
						AudioContent: buffer[:n],
					},
				}); err != nil {
					p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to send audio content to Google Speech-to-Text")
					errChan <- err
					return
				}
			}
		}
	}()

	// Start receiving transcription results
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-doneChan:
				return
			default:
				resp, err := stream.Recv()
				if err == io.EOF {
					return
				}
				if err != nil {
					p.logger.WithError(err).WithField("call_uuid", callUUID).Error("Error receiving streaming response")
					errChan <- err
					return
				}

				for _, result := range resp.Results {
					for _, alt := range result.Alternatives {
						transcription := alt.Transcript
						if result.IsFinal {
							p.logger.WithFields(logrus.Fields{
								"call_uuid":     callUUID,
								"transcription": transcription,
								"final":         true,
							}).Info("Received final transcription")

							// Create metadata
							metadata := map[string]interface{}{
								"provider":   p.Name(),
								"confidence": alt.Confidence,
								"word_count": len(strings.Fields(transcription)),
							}

							// Publish to transcription service for real-time streaming
							if p.transcriptionSvc != nil {
								p.transcriptionSvc.PublishTranscription(callUUID, transcription, true, metadata)
							}
						} else {
							p.logger.WithFields(logrus.Fields{
								"call_uuid":     callUUID,
								"transcription": transcription,
								"final":         false,
							}).Debug("Received interim transcription")

							// Create metadata for interim result
							metadata := map[string]interface{}{
								"provider": p.Name(),
								"interim":  true,
							}

							// Publish interim results for real-time streaming
							if p.transcriptionSvc != nil {
								p.transcriptionSvc.PublishTranscription(callUUID, transcription, false, metadata)
							}
						}
					}
				}
			}
		}
	}()

	// Wait for completion or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-doneChan:
		return nil
	}
}
