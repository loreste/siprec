package stt

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/transcribestreaming"
	"github.com/aws/aws-sdk-go-v2/service/transcribestreaming/types"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// AmazonTranscribeProvider implements the Provider interface for Amazon Transcribe
type AmazonTranscribeProvider struct {
	logger           *logrus.Logger
	client           *transcribestreaming.Client
	transcriptionSvc *TranscriptionService
	config           *config.AmazonSTTConfig
	mutex            sync.RWMutex
}


// NewAmazonTranscribeProvider creates a new Amazon Transcribe provider
func NewAmazonTranscribeProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.AmazonSTTConfig) *AmazonTranscribeProvider {
	return &AmazonTranscribeProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
	}
}

// Name returns the provider name
func (p *AmazonTranscribeProvider) Name() string {
	return "amazon-transcribe"
}

// Initialize initializes the Amazon Transcribe client
func (p *AmazonTranscribeProvider) Initialize() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.config == nil {
		return fmt.Errorf("Amazon STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Amazon STT is disabled, skipping initialization")
		return nil
	}

	// Check for AWS credentials
	if p.config.AccessKeyID == "" || p.config.SecretAccessKey == "" {
		return fmt.Errorf("Amazon STT requires AWS access key ID and secret access key")
	}

	region := p.config.Region
	if region == "" {
		region = "us-east-1"
	}

	// Load AWS configuration
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithRetryMaxAttempts(3),
		awsconfig.WithRetryMode(aws.RetryModeStandard),
		awsconfig.WithCredentialsProvider(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     p.config.AccessKeyID,
				SecretAccessKey: p.config.SecretAccessKey,
			}, nil
		})),
	)
	if err != nil {
		p.logger.WithError(err).Error("Failed to load AWS configuration")
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create Transcribe Streaming client
	p.client = transcribestreaming.NewFromConfig(cfg)

	p.logger.WithFields(logrus.Fields{
		"region":      region,
		"language":    p.config.Language,
		"media_format": p.config.MediaFormat,
		"sample_rate": p.config.SampleRate,
		"vocabulary":  p.config.VocabularyName,
		"channel_id":  p.config.EnableChannelIdentification,
		"speaker_id":  p.config.EnableSpeakerIdentification,
	}).Info("Amazon Transcribe provider initialized successfully")

	return nil
}

// StreamToText streams audio data to Amazon Transcribe
func (p *AmazonTranscribeProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	p.mutex.RLock()
	if p.client == nil {
		p.mutex.RUnlock()
		return ErrInitializationFailed
	}
	p.mutex.RUnlock()

	logger := p.logger.WithField("call_uuid", callUUID)
	logger.Info("Starting Amazon Transcribe streaming transcription")

	// Create stream transcription input
	input := &transcribestreaming.StartStreamTranscriptionInput{
		LanguageCode:         types.LanguageCode(p.config.Language),
		MediaSampleRateHertz: aws.Int32(int32(p.config.SampleRate)),
		MediaEncoding:        types.MediaEncodingPcm, // Default to PCM
	}

	// Set media encoding based on format
	switch p.config.MediaFormat {
	case "wav":
		input.MediaEncoding = types.MediaEncodingPcm
	case "flac":
		input.MediaEncoding = types.MediaEncodingFlac
	default:
		input.MediaEncoding = types.MediaEncodingPcm
	}

	// Add optional configuration
	if p.config.EnableChannelIdentification {
		input.EnableChannelIdentification = p.config.EnableChannelIdentification
	}
	if p.config.VocabularyName != "" {
		input.VocabularyName = aws.String(p.config.VocabularyName)
	}
	if p.config.EnableSpeakerIdentification {
		input.ShowSpeakerLabel = p.config.EnableSpeakerIdentification
	}

	// Start the transcription stream
	resp, err := p.client.StartStreamTranscription(ctx, input)
	if err != nil {
		logger.WithError(err).Error("Failed to start Amazon Transcribe stream")
		return fmt.Errorf("failed to start transcription stream: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Channels for coordination
	errChan := make(chan error, 2)
	doneChan := make(chan struct{})

	// Audio sender goroutine
	go func() {
		defer func() {
			if closeErr := resp.GetStream().Close(); closeErr != nil {
				logger.WithError(closeErr).Debug("Failed to close stream")
			}
		}()

		buffer := make([]byte, 1024)
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-doneChan:
				return
			default:
				n, readErr := audioStream.Read(buffer)
				if readErr == io.EOF {
					logger.Debug("Audio stream ended")
					return
				}
				if readErr != nil {
					logger.WithError(readErr).Error("Failed to read from audio stream")
					errChan <- readErr
					return
				}

				if n > 0 {
					audioEvent := &types.AudioStreamMemberAudioEvent{
						Value: types.AudioEvent{
							AudioChunk: buffer[:n],
						},
					}

					if sendErr := resp.GetStream().Send(streamCtx, audioEvent); sendErr != nil {
						logger.WithError(sendErr).Error("Failed to send audio to Amazon Transcribe")
						errChan <- sendErr
						return
					}
				}
			}
		}
	}()

	// Response receiver goroutine
	go func() {
		defer close(doneChan)

		for event := range resp.GetStream().Events() {
			select {
			case <-streamCtx.Done():
				return
			default:
				if event != nil {
					p.processTranscriptionEvent(event, callUUID, logger)
				}
			}
		}

		if streamErr := resp.GetStream().Err(); streamErr != nil {
			logger.WithError(streamErr).Error("Amazon Transcribe stream error")
			errChan <- streamErr
		}
	}()

	// Monitor for completion or errors
	select {
	case err := <-errChan:
		cancel()
		return err
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case <-doneChan:
		cancel()
		return nil
	}
}

// processTranscriptionEvent processes incoming transcription events
func (p *AmazonTranscribeProvider) processTranscriptionEvent(event types.TranscriptResultStream, callUUID string, logger *logrus.Entry) {
	switch v := event.(type) {
	case *types.TranscriptResultStreamMemberTranscriptEvent:
		p.processTranscriptEvent(v.Value, callUUID, logger)
	default:
		logger.WithField("event_type", fmt.Sprintf("%T", v)).Debug("Unknown transcription event type")
	}
}

// processTranscriptEvent processes transcript events and publishes transcriptions
func (p *AmazonTranscribeProvider) processTranscriptEvent(event types.TranscriptEvent, callUUID string, logger *logrus.Entry) {
	if event.Transcript == nil || event.Transcript.Results == nil {
		return
	}

	for _, result := range event.Transcript.Results {
		if result.Alternatives == nil {
			continue
		}

		for _, alternative := range result.Alternatives {
			if alternative.Transcript == nil || *alternative.Transcript == "" {
				continue
			}

			transcript := *alternative.Transcript
			isFinal := !result.IsPartial

			// Create metadata
			metadata := map[string]interface{}{
				"provider":   p.Name(),
				"word_count": len(strings.Fields(transcript)),
				"is_partial": result.IsPartial,
				"start_time": result.StartTime,
				"end_time":   result.EndTime,
			}

			// Add result ID if available
			if result.ResultId != nil {
				metadata["result_id"] = *result.ResultId
			}

			// Add channel ID if available
			if result.ChannelId != nil {
				metadata["channel_id"] = *result.ChannelId
			}

			// Add language code if available
			if result.LanguageCode != "" {
				metadata["language_code"] = string(result.LanguageCode)
			}

			// Add item details for word-level information
			if alternative.Items != nil && len(alternative.Items) > 0 {
				items := make([]map[string]interface{}, 0, len(alternative.Items))
				for _, item := range alternative.Items {
					itemData := map[string]interface{}{
						"start_time":              item.StartTime,
						"end_time":                item.EndTime,
						"vocabulary_filter_match": item.VocabularyFilterMatch,
					}

					if item.Content != nil {
						itemData["content"] = *item.Content
					}
					if item.Type != "" {
						itemData["type"] = string(item.Type)
					}
					if item.Confidence != nil {
						itemData["confidence"] = *item.Confidence
					}
					if item.Speaker != nil {
						itemData["speaker"] = *item.Speaker
					}
					if item.Stable != nil {
						itemData["stable"] = *item.Stable
					}

					items = append(items, itemData)
				}
				metadata["items"] = items
			}

			// Add entity information if available
			if alternative.Entities != nil && len(alternative.Entities) > 0 {
				entities := make([]map[string]interface{}, 0, len(alternative.Entities))
				for _, entity := range alternative.Entities {
					entityData := map[string]interface{}{
						"start_time": entity.StartTime,
						"end_time":   entity.EndTime,
					}

					if entity.Category != nil {
						entityData["category"] = *entity.Category
					}
					if entity.Content != nil {
						entityData["content"] = *entity.Content
					}
					if entity.Confidence != nil {
						entityData["confidence"] = *entity.Confidence
					}
					if entity.Type != nil {
						entityData["type"] = *entity.Type
					}

					entities = append(entities, entityData)
				}
				metadata["entities"] = entities
			}

			// Log transcription
			logger.WithFields(logrus.Fields{
				"transcript": transcript,
				"is_final":   isFinal,
				"channel_id": metadata["channel_id"],
			}).Info("Received transcription from Amazon Transcribe")

			// Publish to transcription service
			if p.transcriptionSvc != nil {
				p.transcriptionSvc.PublishTranscription(callUUID, transcript, isFinal, metadata)
			}
		}
	}
}

// UpdateConfig allows runtime configuration updates
func (p *AmazonTranscribeProvider) UpdateConfig(cfg *config.AmazonSTTConfig) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.config = cfg
	p.logger.WithFields(logrus.Fields{
		"language":    cfg.Language,
		"sample_rate": cfg.SampleRate,
		"media_format": cfg.MediaFormat,
	}).Info("Updated Amazon Transcribe configuration")
}

// GetConfig returns the current configuration
func (p *AmazonTranscribeProvider) GetConfig() *config.AmazonSTTConfig {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	return p.config
}

// SetVocabulary sets the vocabulary name for enhanced accuracy
func (p *AmazonTranscribeProvider) SetVocabulary(vocabularyName string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.config.VocabularyName = vocabularyName
	p.logger.WithField("vocabulary", vocabularyName).Info("Set Amazon Transcribe vocabulary")
}

// Close gracefully closes the provider and cleans up resources
func (p *AmazonTranscribeProvider) Close() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.client != nil {
		p.logger.Info("Amazon Transcribe provider closed")
		p.client = nil
	}

	return nil
}
