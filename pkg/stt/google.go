package stt

import (
	"context"
	"fmt"
	"io"
	"strings"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/sirupsen/logrus"
	"google.golang.org/api/option"
	"siprec-server/pkg/config"
)

// GoogleProvider implements the Provider interface for Google Speech-to-Text
type GoogleProvider struct {
	logger           *logrus.Logger
	client           *speech.Client
	transcriptionSvc *TranscriptionService
	config           *config.GoogleSTTConfig

	// Callback function for transcription results
	callback TranscriptionCallback
}

// NewGoogleProvider creates a new Google Speech-to-Text provider
func NewGoogleProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.GoogleSTTConfig) *GoogleProvider {
	return &GoogleProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
	}
}

// Name returns the provider name
func (p *GoogleProvider) Name() string {
	return "google"
}

// Initialize initializes the Google Speech-to-Text client
func (p *GoogleProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("Google STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Google STT is disabled, skipping initialization")
		return nil
	}

	var clientOptions []option.ClientOption

	// Use API key if provided, otherwise use credentials file
	if p.config.APIKey != "" {
		clientOptions = append(clientOptions, option.WithAPIKey(p.config.APIKey))
		p.logger.Debug("Using Google STT API key authentication")
	} else if p.config.CredentialsFile != "" {
		clientOptions = append(clientOptions, option.WithCredentialsFile(p.config.CredentialsFile))
		p.logger.WithField("credentials_file", p.config.CredentialsFile).Debug("Using Google STT credentials file")
	} else {
		p.logger.Warn("No Google STT credentials provided (API key or credentials file)")
		return fmt.Errorf("Google STT requires either API key or credentials file")
	}

	var err error
	p.client, err = speech.NewClient(context.Background(), clientOptions...)
	if err != nil {
		p.logger.WithError(err).Error("Failed to create Google Speech client")
		return fmt.Errorf("failed to create Google Speech client: %w", err)
	}

	p.logger.WithFields(logrus.Fields{
		"language":            p.config.Language,
		"sample_rate":         p.config.SampleRate,
		"model":               p.config.Model,
		"enhanced_models":     p.config.EnhancedModels,
		"auto_punctuation":    p.config.EnableAutomaticPunctuation,
		"word_time_offsets":   p.config.EnableWordTimeOffsets,
	}).Info("Google Speech-to-Text client initialized successfully")
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

	// Build recognition config from our settings
	recognitionConfig := &speechpb.RecognitionConfig{
		Encoding:                   speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:           int32(p.config.SampleRate),
		LanguageCode:              p.config.Language,
		EnableAutomaticPunctuation: p.config.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:     p.config.EnableWordTimeOffsets,
		MaxAlternatives:           int32(p.config.MaxAlternatives),
		ProfanityFilter:           p.config.ProfanityFilter,
	}

	// Set model if specified
	if p.config.Model != "" {
		recognitionConfig.Model = p.config.Model
	}

	// Use enhanced models if enabled
	if p.config.EnhancedModels {
		recognitionConfig.UseEnhanced = true
	}

	streamingConfig := &speechpb.StreamingRecognitionConfig{
		Config:         recognitionConfig,
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

							// Create enhanced metadata
							metadata := map[string]interface{}{
								"provider":        p.Name(),
								"confidence":      alt.Confidence,
								"word_count":      len(strings.Fields(transcription)),
								"language_code":   result.LanguageCode,
								"result_end_time": result.ResultEndTime,
								// "speaker_tag":      result.SpeakerTag, // Not available in basic result
							}

							// Add word-level information if available
							if len(alt.Words) > 0 {
								words := make([]map[string]interface{}, len(alt.Words))
								for i, word := range alt.Words {
									wordInfo := map[string]interface{}{
										"word":       word.Word,
										"confidence": word.Confidence,
										// "speaker_tag": word.SpeakerTag, // Not available in basic word info
									}
									if word.StartTime != nil {
										wordInfo["start_time"] = word.StartTime.AsDuration()
									}
									if word.EndTime != nil {
										wordInfo["end_time"] = word.EndTime.AsDuration()
									}
									words[i] = wordInfo
								}
								metadata["words"] = words
							}

							// Call callback if available
							if p.callback != nil {
								p.callback(callUUID, transcription, true, metadata)
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
								"provider":      p.Name(),
								"interim":       true,
								"confidence":    alt.Confidence,
								"language_code": result.LanguageCode,
								// "speaker_tag":   result.SpeakerTag, // Not available in basic result
							}

							// Call callback for interim result
							if p.callback != nil {
								p.callback(callUUID, transcription, false, metadata)
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

// SetCallback sets the callback function for transcription results
func (p *GoogleProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}
