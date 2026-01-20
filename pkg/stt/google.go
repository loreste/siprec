package stt

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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

	// Connection management
	activeStreams map[string]*GoogleStreamSession
	streamsMutex  sync.RWMutex
}

// GoogleStreamSession represents an active streaming session
type GoogleStreamSession struct {
	callUUID      string
	ctx           context.Context
	cancel        context.CancelFunc
	stream        speechpb.Speech_StreamingRecognizeClient
	lastActivity  time.Time
	mutex         sync.RWMutex
	finalReceived chan struct{}
	streamDone    chan struct{}
}

// NewGoogleProvider creates a new Google Speech-to-Text provider
func NewGoogleProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.GoogleSTTConfig) *GoogleProvider {
	return &GoogleProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		activeStreams:    make(map[string]*GoogleStreamSession),
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
func (p *GoogleProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) (err error) {
	// Panic recovery to prevent crashes from affecting the main server
	defer func() {
		if r := recover(); r != nil {
			p.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"panic":     r,
			}).Error("Recovered from panic in Google STT StreamToText")
			err = fmt.Errorf("panic recovered in Google STT streaming: %v", r)
		}
	}()

	if p.client == nil {
		return ErrInitializationFailed
	}

	logger := p.logger.WithField("call_uuid", callUUID)

	// Create session context with cancel
	sessionCtx, cancel := context.WithCancel(ctx)

	stream, err := p.client.StreamingRecognize(sessionCtx)
	if err != nil {
		cancel()
		logger.WithError(err).Error("Failed to start Google Speech-to-Text stream")
		return err
	}

	// Create session for tracking
	session := &GoogleStreamSession{
		callUUID:      callUUID,
		ctx:           sessionCtx,
		cancel:        cancel,
		stream:        stream,
		lastActivity:  time.Now(),
		finalReceived: make(chan struct{}),
		streamDone:    make(chan struct{}),
	}

	// Register session
	p.streamsMutex.Lock()
	p.activeStreams[callUUID] = session
	p.streamsMutex.Unlock()

	// Cleanup on exit
	defer func() {
		p.streamsMutex.Lock()
		delete(p.activeStreams, callUUID)
		p.streamsMutex.Unlock()
		cancel()
	}()

	// Build recognition config from our settings
	recognitionConfig := &speechpb.RecognitionConfig{
		Encoding:                   speechpb.RecognitionConfig_LINEAR16,
		SampleRateHertz:            int32(p.config.SampleRate),
		LanguageCode:               p.config.Language,
		EnableAutomaticPunctuation: p.config.EnableAutomaticPunctuation,
		EnableWordTimeOffsets:      p.config.EnableWordTimeOffsets,
		MaxAlternatives:            int32(p.config.MaxAlternatives),
		ProfanityFilter:            p.config.ProfanityFilter,
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
		logger.WithError(err).Error("Failed to send streaming config")
		return err
	}

	// Create channels for coordinating goroutines
	errChan := make(chan error, 2)
	audioSendDone := make(chan struct{})

	// Start receiving transcription results
	go p.receiveResults(session, errChan, logger)

	// Start reading and sending audio
	go func() {
		defer close(audioSendDone)
		buffer := make([]byte, 4096)
		for {
			select {
			case <-sessionCtx.Done():
				stream.CloseSend()
				return
			default:
				n, err := audioStream.Read(buffer)
				if err == io.EOF {
					logger.Debug("Audio stream EOF reached, closing send")
					stream.CloseSend()
					return
				}
				if err != nil {
					logger.WithError(err).Error("Failed to read audio stream")
					errChan <- err
					return
				}

				if n > 0 {
					session.mutex.Lock()
					session.lastActivity = time.Now()
					session.mutex.Unlock()

					if err := stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: buffer[:n],
						},
					}); err != nil {
						logger.WithError(err).Error("Failed to send audio content to Google Speech-to-Text")
						errChan <- err
						return
					}
				}
			}
		}
	}()

	// Wait for audio sending to complete
	select {
	case <-audioSendDone:
		logger.Debug("Audio sending completed, waiting for final results")
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for final transcription results with timeout
	waitTimeout := 5 * time.Second
	select {
	case <-session.finalReceived:
		logger.Debug("Received final transcription")
	case <-session.streamDone:
		logger.Debug("Stream completed")
	case <-time.After(waitTimeout):
		logger.Debug("Timeout waiting for final transcription")
	case <-ctx.Done():
		logger.Debug("Context cancelled while waiting for final results")
	}

	return nil
}

// receiveResults handles receiving transcription results from Google
func (p *GoogleProvider) receiveResults(session *GoogleStreamSession, errChan chan error, logger *logrus.Entry) {
	defer close(session.streamDone)

	lastFinalTime := time.Now()

	for {
		select {
		case <-session.ctx.Done():
			return
		default:
			resp, err := session.stream.Recv()
			if err == io.EOF {
				logger.Debug("Google STT stream EOF")
				return
			}
			if err != nil {
				// Don't report error if context was cancelled
				if session.ctx.Err() == nil {
					logger.WithError(err).Debug("Google STT stream ended")
				}
				return
			}

			session.mutex.Lock()
			session.lastActivity = time.Now()
			session.mutex.Unlock()

			for _, result := range resp.Results {
				for _, alt := range result.Alternatives {
					transcription := alt.Transcript
					if transcription == "" {
						continue
					}

					if result.IsFinal {
						lastFinalTime = time.Now()
						logger.WithFields(logrus.Fields{
							"transcription": transcription,
							"confidence":    alt.Confidence,
						}).Info("Received final transcription")

						// Create enhanced metadata
						metadata := map[string]interface{}{
							"provider":        p.Name(),
							"confidence":      alt.Confidence,
							"word_count":      len(strings.Fields(transcription)),
							"language_code":   result.LanguageCode,
							"result_end_time": result.ResultEndTime,
						}

						// Add word-level information if available
						if len(alt.Words) > 0 {
							words := make([]map[string]interface{}, len(alt.Words))
							for i, word := range alt.Words {
								wordInfo := map[string]interface{}{
									"word":       word.Word,
									"confidence": word.Confidence,
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

						// Publish transcription - prefer callback (wrapper handles AMQP delivery)
						if p.callback != nil {
							p.callback(session.callUUID, transcription, true, metadata)
						} else if p.transcriptionSvc != nil {
							p.transcriptionSvc.PublishTranscription(session.callUUID, transcription, true, metadata)
						}

						// Signal final received
						select {
						case <-session.finalReceived:
							// Already closed
						default:
							// Only close on the last final result (after some delay)
							go func() {
								time.Sleep(500 * time.Millisecond)
								if time.Since(lastFinalTime) >= 400*time.Millisecond {
									select {
									case <-session.finalReceived:
									default:
										close(session.finalReceived)
									}
								}
							}()
						}
					} else {
						logger.WithFields(logrus.Fields{
							"transcription": transcription,
						}).Debug("Received interim transcription")

						// Create metadata for interim result
						metadata := map[string]interface{}{
							"provider":      p.Name(),
							"interim":       true,
							"confidence":    alt.Confidence,
							"language_code": result.LanguageCode,
						}

						// Publish interim transcription - prefer callback (wrapper handles AMQP delivery)
						if p.callback != nil {
							p.callback(session.callUUID, transcription, false, metadata)
						} else if p.transcriptionSvc != nil {
							p.transcriptionSvc.PublishTranscription(session.callUUID, transcription, false, metadata)
						}
					}
				}
			}
		}
	}
}

// GetActiveConnections returns the number of active streaming sessions
func (p *GoogleProvider) GetActiveConnections() int {
	p.streamsMutex.RLock()
	defer p.streamsMutex.RUnlock()
	return len(p.activeStreams)
}

// Shutdown gracefully shuts down all active streams
func (p *GoogleProvider) Shutdown(ctx context.Context) error {
	p.logger.Info("Shutting down Google STT provider")

	p.streamsMutex.Lock()
	for callUUID, session := range p.activeStreams {
		p.logger.WithField("call_uuid", callUUID).Debug("Closing Google STT session")
		if session.cancel != nil {
			session.cancel()
		}
	}
	p.activeStreams = make(map[string]*GoogleStreamSession)
	p.streamsMutex.Unlock()

	// Close the client
	if p.client != nil {
		if err := p.client.Close(); err != nil {
			p.logger.WithError(err).Warn("Error closing Google STT client")
		}
	}

	p.logger.Info("Google STT provider shutdown complete")
	return nil
}

// SetCallback sets the callback function for transcription results
func (p *GoogleProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}
