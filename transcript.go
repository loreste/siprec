package main

import (
	"context"
	"io"
	"time"

	"github.com/sirupsen/logrus"
)

// Define a struct to handle different Speech-to-Text providers
type SpeechToTextProvider struct {
	GoogleClient   func(ctx context.Context, audioStream io.Reader, callUUID string)
	DeepgramClient func(ctx context.Context, audioStream io.Reader, callUUID string) error
	OpenAIClient   func(ctx context.Context, audioStream io.Reader, callUUID string) error
}

// StreamToProvider is a generic function to stream audio to the desired provider
func StreamToProvider(ctx context.Context, provider string, audioStream io.Reader, callUUID string) {
	// Get start time for latency tracking
	startTime := time.Now()

	// Log start of transcription
	logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"provider":  provider,
	}).Info("Starting transcription")

	switch provider {
	case "google":
		if speechToText.GoogleClient != nil {
			logger.WithField("call_uuid", callUUID).Debug("Using Google Speech-to-Text")
			speechToText.GoogleClient(ctx, audioStream, callUUID)
		} else {
			logger.WithField("call_uuid", callUUID).Error("Google Speech-to-Text client not initialized")
		}
	case "deepgram":
		if speechToText.DeepgramClient != nil {
			logger.WithField("call_uuid", callUUID).Debug("Using Deepgram Speech-to-Text")
			if err := speechToText.DeepgramClient(ctx, audioStream, callUUID); err != nil {
				logger.WithError(err).WithField("call_uuid", callUUID).Error("Deepgram transcription failed")
			}
		} else {
			logger.WithField("call_uuid", callUUID).Error("Deepgram client not initialized")
		}
	case "openai":
		if speechToText.OpenAIClient != nil {
			logger.WithField("call_uuid", callUUID).Debug("Using OpenAI Speech-to-Text")
			if err := speechToText.OpenAIClient(ctx, audioStream, callUUID); err != nil {
				logger.WithError(err).WithField("call_uuid", callUUID).Error("OpenAI transcription failed")
			}
		} else {
			logger.WithField("call_uuid", callUUID).Error("OpenAI client not initialized")
		}
	default:
		logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"provider":  provider,
		}).Error("Unknown Speech-to-Text provider")
	}

	// Log transcription completion and latency
	elapsed := time.Since(startTime)
	logger.WithFields(logrus.Fields{
		"call_uuid":   callUUID,
		"provider":    provider,
		"duration_ms": elapsed.Milliseconds(),
	}).Info("Transcription completed")
}

// ProcessTranscription handles transcription results
func ProcessTranscription(transcription string, callUUID string) {
	// Check if the call still exists
	if forwarderValue, exists := activeCalls.Load(callUUID); exists {
		forwarder := forwarderValue.(*RTPForwarder)

		// Skip processing if recording is paused
		if forwarder.recordingPaused {
			logger.WithField("call_uuid", callUUID).Debug("Skipping transcription processing - recording paused")
			return
		}

		// Send to the transcription channel if needed
		select {
		case forwarder.transcriptChan <- transcription:
			logger.WithField("call_uuid", callUUID).Debug("Sent transcription to channel")
		default:
			// Channel is full or not being consumed, just log
			logger.WithField("call_uuid", callUUID).Debug("Transcription channel full or not consumed")
		}
	}

	// Send to AMQP regardless of call status (might be useful for post-processing)
	sendTranscriptionToAMQP(transcription, callUUID)
}

// Define a global variable for the SpeechToTextProvider
var speechToText = SpeechToTextProvider{
	GoogleClient:   streamToGoogleSpeech,
	DeepgramClient: streamToDeepgram,
	OpenAIClient:   streamToOpenAI,
}
