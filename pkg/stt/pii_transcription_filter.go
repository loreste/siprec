package stt

import (
	"sync"

	"github.com/sirupsen/logrus"
	"siprec-server/pkg/pii"
)

// PIITranscriptionFilter implements the TranscriptionListener interface
// and filters PII from transcriptions before passing them to wrapped listeners
type PIITranscriptionFilter struct {
	logger    *logrus.Logger
	detector  *pii.PIIDetector
	listeners []TranscriptionListener
	enabled   bool
	mutex     sync.RWMutex
}

// NewPIITranscriptionFilter creates a new PII filtering transcription listener
func NewPIITranscriptionFilter(logger *logrus.Logger, detector *pii.PIIDetector, enabled bool) *PIITranscriptionFilter {
	return &PIITranscriptionFilter{
		logger:    logger,
		detector:  detector,
		listeners: make([]TranscriptionListener, 0),
		enabled:   enabled,
	}
}

// AddListener adds a wrapped listener that will receive filtered transcriptions
func (f *PIITranscriptionFilter) AddListener(listener TranscriptionListener) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	
	f.listeners = append(f.listeners, listener)
	f.logger.WithField("listener_count", len(f.listeners)).Debug("Added listener to PII filter")
}

// OnTranscription implements the TranscriptionListener interface
// It applies PII detection and redaction before forwarding to wrapped listeners
func (f *PIITranscriptionFilter) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Skip empty transcriptions
	if transcription == "" {
		return
	}

	var processedTranscription string
	var piiDetected bool

	// Apply PII detection if enabled
	if f.enabled && f.detector != nil {
		result := f.detector.DetectAndRedact(transcription)
		processedTranscription = result.RedactedText
		piiDetected = result.HasPII

		// Log PII detection results
		if piiDetected {
			f.logger.WithFields(logrus.Fields{
				"call_uuid":      callUUID,
				"is_final":       isFinal,
				"pii_matches":    len(result.Matches),
				"original_length": len(transcription),
				"redacted_length": len(processedTranscription),
			}).Info("PII detected and redacted in transcription")

			// Add PII detection metadata (create a copy to avoid race conditions)
			if metadata == nil {
				metadata = make(map[string]interface{})
			} else {
				// Create a copy of the metadata map to avoid concurrent modification
				originalMetadata := metadata
				metadata = make(map[string]interface{})
				for k, v := range originalMetadata {
					metadata[k] = v
				}
			}
			metadata["pii_detected"] = true
			metadata["pii_matches"] = len(result.Matches)
			metadata["pii_types"] = extractPIITypes(result.Matches)
			metadata["original_transcription"] = transcription // Preserve original for audio marking
		} else {
			f.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"is_final":  isFinal,
			}).Debug("No PII detected in transcription")
		}
	} else {
		// PII detection disabled, pass through unchanged
		processedTranscription = transcription
		f.logger.WithFields(logrus.Fields{
			"call_uuid": callUUID,
			"is_final":  isFinal,
		}).Debug("PII detection disabled, transcription passed through unchanged")
	}

	// Forward the processed transcription to all wrapped listeners
	// Take a read lock to safely access listeners slice
	f.mutex.RLock()
	listeners := make([]TranscriptionListener, len(f.listeners))
	copy(listeners, f.listeners)
	f.mutex.RUnlock()

	for _, listener := range listeners {
		// Create a copy of metadata for each listener to prevent race conditions
		var metadataCopy map[string]interface{}
		if metadata != nil {
			metadataCopy = make(map[string]interface{})
			for k, v := range metadata {
				metadataCopy[k] = v
			}
		}

		// Use recovered call to prevent individual listener failures from affecting others
		func() {
			defer func() {
				if r := recover(); r != nil {
					f.logger.WithFields(logrus.Fields{
						"call_uuid": callUUID,
						"recover":   r,
					}).Error("Recovered from panic in wrapped transcription listener")
				}
			}()
			
			listener.OnTranscription(callUUID, processedTranscription, isFinal, metadataCopy)
		}()
	}
}

// extractPIITypes extracts the unique PII types from matches for metadata
func extractPIITypes(matches []pii.PIIMatch) []string {
	typeMap := make(map[string]bool)
	for _, match := range matches {
		typeMap[string(match.Type)] = true
	}

	var types []string
	for piiType := range typeMap {
		types = append(types, piiType)
	}
	return types
}

// GetStats returns statistics about PII filtering
func (f *PIITranscriptionFilter) GetStats() map[string]interface{} {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	
	stats := map[string]interface{}{
		"enabled":        f.enabled,
		"listener_count": len(f.listeners),
	}

	if f.detector != nil {
		detectorStats := f.detector.GetStats()
		for key, value := range detectorStats {
			stats["detector_"+key] = value
		}
	}

	return stats
}