package stt

import (
	"strings"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/media"
)

// PIIAudioTranscriptionBridge bridges transcription PII detection to audio PII marking
// This listener receives PII-filtered transcriptions and marks corresponding audio timestamps
type PIIAudioTranscriptionBridge struct {
	logger           *logrus.Logger
	rtpForwarderFunc func(callUUID string) *media.RTPForwarder // Function to get RTP forwarder by call UUID
	enabled          bool
}

// NewPIIAudioTranscriptionBridge creates a new bridge for PII audio marking
func NewPIIAudioTranscriptionBridge(logger *logrus.Logger, rtpForwarderFunc func(callUUID string) *media.RTPForwarder, enabled bool) *PIIAudioTranscriptionBridge {
	return &PIIAudioTranscriptionBridge{
		logger:           logger,
		rtpForwarderFunc: rtpForwarderFunc,
		enabled:          enabled,
	}
}

// OnTranscription implements the TranscriptionListener interface
// It receives transcriptions that have already been processed by PII detection
func (b *PIIAudioTranscriptionBridge) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if !b.enabled {
		return
	}

	// Check if PII was detected in this transcription
	piiDetected, hasPII := metadata["pii_detected"].(bool)
	if !hasPII || !piiDetected {
		return // No PII detected, no audio marking needed
	}

	// Get PII type information
	piiTypes, hasTypes := metadata["pii_types"].([]string)
	if !hasTypes || len(piiTypes) == 0 {
		return
	}

	// Get the RTP forwarder for this call
	forwarder := b.rtpForwarderFunc(callUUID)
	if forwarder == nil {
		b.logger.WithField("call_uuid", callUUID).Warn("RTP forwarder not found for PII audio marking")
		return
	}

	// Check if the forwarder has a PII audio marker
	if forwarder.PIIAudioMarker == nil {
		b.logger.WithField("call_uuid", callUUID).Debug("PII audio marker not available for call")
		return
	}

	// Extract original transcription from metadata if available
	// The transcription parameter here is already redacted, so we need the original
	originalTranscription := transcription
	if originalText, hasOriginal := metadata["original_transcription"].(string); hasOriginal {
		originalTranscription = originalText
	}

	// Mark PII in audio timeline
	piiTypeStr := strings.Join(piiTypes, ",")
	forwarder.PIIAudioMarker.MarkPII(piiTypeStr, originalTranscription, transcription, isFinal)

	b.logger.WithFields(logrus.Fields{
		"call_uuid":   callUUID,
		"pii_types":   piiTypes,
		"is_final":    isFinal,
		"redacted":    transcription != originalTranscription,
	}).Debug("PII audio marker updated from transcription")
}

// GetStats returns statistics about the audio transcription bridge
func (b *PIIAudioTranscriptionBridge) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"enabled": b.enabled,
		"type":    "pii_audio_transcription_bridge",
	}
}