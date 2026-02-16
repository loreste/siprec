package messaging

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"siprec-server/pkg/realtime"

	"github.com/sirupsen/logrus"
)

// RealtimeEventPublisher defines the minimal interface required to publish realtime events.
type RealtimeEventPublisher interface {
	PublishTranscriptionEvent(event realtime.TranscriptionEvent) error
	IsStarted() bool
}

// RealtimeTranscriptionListener bridges TranscriptionService callbacks to realtime AMQP publishers.
type RealtimeTranscriptionListener struct {
	logger    logrus.FieldLogger
	publisher RealtimeEventPublisher
}

// NewRealtimeTranscriptionListener creates a new realtime transcription listener.
func NewRealtimeTranscriptionListener(logger logrus.FieldLogger, publisher RealtimeEventPublisher) *RealtimeTranscriptionListener {
	return &RealtimeTranscriptionListener{
		logger:    logger,
		publisher: publisher,
	}
}

// OnTranscription implements stt.TranscriptionListener and forwards events to the realtime publisher.
func (l *RealtimeTranscriptionListener) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if transcription == "" {
		return
	}

	if l.publisher == nil || !l.publisher.IsStarted() {
		return
	}

	sessionID := callUUID
	if candidate := extractString(metadata, "session_id", "sessionID", "sessionId"); candidate != "" {
		sessionID = candidate
	}

	event := realtime.TranscriptionEvent{
		Type:      realtime.EventTypeFinalTranscript,
		SessionID: sessionID,
		CallID:    callUUID,
		Timestamp: time.Now(),
		Data: realtime.TranscriptionEventData{
			Text:         transcription,
			IsFinal:      isFinal,
			Confidence:   extractFloat(metadata, "confidence", "confidence_score"),
			StartTime:    extractFloat(metadata, "start_time", "offset_start"),
			EndTime:      extractFloat(metadata, "end_time", "offset_end"),
			Language:     extractString(metadata, "language", "detected_language"),
			SpeakerID:    extractString(metadata, "speaker_id", "speaker"),
			SpeakerLabel: extractString(metadata, "speaker_label"),
			SpeakerCount: extractInt(metadata, "speaker_count"),
			Metadata:     sanitizeMetadata(metadata),
		},
	}

	if !isFinal {
		event.Type = realtime.EventTypePartialTranscript
	}

	go func(evt realtime.TranscriptionEvent) {
		if err := l.publisher.PublishTranscriptionEvent(evt); err != nil {
			l.logger.WithError(err).WithFields(logrus.Fields{
				"call_uuid":  callUUID,
				"event_type": evt.Type,
			}).Warn("Failed to publish realtime transcription event")
		}
	}(event)
}

func sanitizeMetadata(metadata map[string]interface{}) map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}

	skipKeys := map[string]struct{}{
		"call_uuid":         {},
		"text":              {},
		"transcription":     {},
		"is_final":          {},
		"confidence":        {},
		"confidence_score":  {},
		"start_time":        {},
		"end_time":          {},
		"offset_start":      {},
		"offset_end":        {},
		"language":          {},
		"detected_language": {},
		"speaker_id":        {},
		"speaker":           {},
		"speaker_label":     {},
		"speaker_count":     {},
		"session_id":        {},
		"sessionid":         {},
		"stream_label":      {},
		"participant_name":  {},
		"participant_role":  {},
		"participant_aor":   {},
	}

	clean := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		if _, skip := skipKeys[strings.ToLower(key)]; skip {
			continue
		}

		switch v := value.(type) {
		case string, bool:
			clean[key] = v
		case float64, float32:
			clean[key] = toFloat64(v)
		case int, int32, int64:
			clean[key] = toFloat64(v)
		case uint, uint32, uint64:
			clean[key] = toFloat64(v)
		default:
			// Skip complex or unsupported types to keep payload lightweight.
		}
	}

	if len(clean) == 0 {
		return nil
	}
	return clean
}

func extractString(metadata map[string]interface{}, keys ...string) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			switch v := val.(type) {
			case string:
				if v != "" {
					return v
				}
			case fmt.Stringer:
				str := v.String()
				if str != "" {
					return str
				}
			}
		}
	}
	return ""
}

func extractFloat(metadata map[string]interface{}, keys ...string) float64 {
	if len(metadata) == 0 {
		return 0
	}
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			switch v := val.(type) {
			case float64:
				return v
			case float32:
				return float64(v)
			case int:
				return float64(v)
			case int64:
				return float64(v)
			case uint:
				return float64(v)
			case uint64:
				return float64(v)
			case string:
				if parsed, err := strconv.ParseFloat(v, 64); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func extractInt(metadata map[string]interface{}, keys ...string) int {
	if len(metadata) == 0 {
		return 0
	}
	for _, key := range keys {
		if val, ok := metadata[key]; ok {
			switch v := val.(type) {
			case int:
				return v
			case int32:
				return int(v)
			case int64:
				return int(v)
			case uint:
				return int(v)
			case uint32:
				return int(v)
			case uint64:
				return int(v)
			case float64:
				return int(v)
			case float32:
				return int(v)
			case string:
				if parsed, err := strconv.Atoi(v); err == nil {
					return parsed
				}
			}
		}
	}
	return 0
}

func toFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	default:
		return 0
	}
}
