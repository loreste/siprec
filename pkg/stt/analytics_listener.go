package stt

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/realtime/analytics"
)

// AnalyticsListener bridges the transcription service to the analytics dispatcher.
type AnalyticsListener struct {
	logger     *logrus.Logger
	dispatcher *analytics.Dispatcher
}

// NewAnalyticsListener creates a new analytics listener.
func NewAnalyticsListener(logger *logrus.Logger, dispatcher *analytics.Dispatcher) *AnalyticsListener {
	return &AnalyticsListener{logger: logger, dispatcher: dispatcher}
}

// OnTranscription implements the TranscriptionListener interface.
func (l *AnalyticsListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if l.dispatcher == nil {
		return
	}

	event := &analytics.TranscriptEvent{
		CallID:    callUUID,
		Text:      transcription,
		IsFinal:   isFinal,
		Metadata:  metadata,
		Timestamp: time.Now(),
	}

	if metadata != nil {
		if speaker := extractSpeaker(metadata); speaker != "" {
			event.Speaker = speaker
		}
		if conf, ok := metadata["confidence"].(float64); ok {
			event.Confidence = conf
		}
	}

	l.dispatcher.HandleTranscript(context.Background(), event)
}

func extractSpeaker(metadata map[string]interface{}) string {
	if speaker, ok := metadata["speaker"].(string); ok {
		return speaker
	}
	if speaker, ok := metadata["speaker_id"].(string); ok {
		return speaker
	}
	if speakerFloat, ok := metadata["speaker_id"].(float64); ok {
		return fmt.Sprintf("%.0f", speakerFloat)
	}
	if speakerTag, ok := metadata["speaker_tag"].(int); ok {
		return fmt.Sprintf("%d", speakerTag)
	}
	if speakerTagFloat, ok := metadata["speaker_tag"].(float64); ok {
		return fmt.Sprintf("%.0f", speakerTagFloat)
	}
	return ""
}
