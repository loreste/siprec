package analytics

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// WebSocketBroadcaster defines the interface for WebSocket broadcasting
type WebSocketBroadcaster interface {
	BroadcastAnalytics(callID string, snapshot *AnalyticsSnapshot)
	BroadcastEvent(callID string, eventType string, event interface{})
}

// WebSocketSubscriber forwards analytics updates to WebSocket clients
type WebSocketSubscriber struct {
	logger      *logrus.Logger
	broadcaster WebSocketBroadcaster
}

// NewWebSocketSubscriber creates a new WebSocket subscriber
func NewWebSocketSubscriber(logger *logrus.Logger, broadcaster WebSocketBroadcaster) *WebSocketSubscriber {
	return &WebSocketSubscriber{
		logger:      logger,
		broadcaster: broadcaster,
	}
}

// OnAnalytics handles analytics snapshot updates
func (s *WebSocketSubscriber) OnAnalytics(callID string, snapshot *AnalyticsSnapshot) {
	if s.broadcaster == nil || snapshot == nil {
		return
	}

	s.broadcaster.BroadcastAnalytics(callID, snapshot)

	// Send specific events for significant changes
	s.checkForSignificantEvents(callID, snapshot)
}

// checkForSignificantEvents detects and broadcasts significant events
func (s *WebSocketSubscriber) checkForSignificantEvents(callID string, snapshot *AnalyticsSnapshot) {
	// Check for sentiment changes
	if len(snapshot.SentimentTrend) > 0 {
		latest := snapshot.SentimentTrend[len(snapshot.SentimentTrend)-1]
		if latest.Score < -0.5 {
			s.broadcaster.BroadcastEvent(callID, "sentiment_alert", map[string]interface{}{
				"severity":  "high",
				"score":     latest.Score,
				"label":     latest.Label,
				"message":   "Highly negative sentiment detected",
				"timestamp": snapshot.UpdatedAt,
			})
		} else if latest.Score > 0.7 {
			s.broadcaster.BroadcastEvent(callID, "sentiment_positive", map[string]interface{}{
				"score":     latest.Score,
				"label":     latest.Label,
				"message":   "Positive sentiment detected",
				"timestamp": snapshot.UpdatedAt,
			})
		}
	}

	// Check for compliance violations
	if len(snapshot.Compliance) > 0 {
		for _, violation := range snapshot.Compliance {
			if violation.Severity == "high" || violation.Severity == "critical" {
				s.broadcaster.BroadcastEvent(callID, "compliance_violation", map[string]interface{}{
					"rule_id":     violation.RuleID,
					"severity":    violation.Severity,
					"description": violation.Description,
					"timestamp":   violation.Timestamp,
				})
			}
		}
	}

	// Check for quality score alerts
	if snapshot.QualityScore < 0.5 && snapshot.QualityScore > 0 {
		s.broadcaster.BroadcastEvent(callID, "quality_alert", map[string]interface{}{
			"score":   snapshot.QualityScore,
			"message": "Low call quality detected",
		})
	}

	if snapshot.Audio.MOS > 0 && snapshot.Audio.MOS < 2.5 {
		s.broadcaster.BroadcastEvent(callID, "audio_quality_alert", map[string]interface{}{
			"mos":         snapshot.Audio.MOS,
			"packet_loss": snapshot.Audio.PacketLoss,
			"jitter_ms":   snapshot.Audio.JitterMs,
			"message":     "Audio quality degraded",
		})
	}

	if len(snapshot.Events) > 0 {
		for _, event := range snapshot.Events {
			evtPayload := map[string]interface{}{
				"confidence": event.Confidence,
				"timestamp":  event.Timestamp,
			}
			for k, v := range event.Details {
				evtPayload[k] = v
			}
			s.broadcaster.BroadcastEvent(callID, fmt.Sprintf("audio_%s", event.Type), evtPayload)
		}
	}

	// Check for significant keywords
	if len(snapshot.Keywords) > 5 {
		topKeywords := snapshot.Keywords
		if len(topKeywords) > 10 {
			topKeywords = topKeywords[:10]
		}
		s.broadcaster.BroadcastEvent(callID, "keywords_update", map[string]interface{}{
			"keywords": topKeywords,
			"count":    len(snapshot.Keywords),
		})
	}

	// Check agent metrics thresholds
	totalDuration := snapshot.Metrics.TotalTalkTime + snapshot.Metrics.TotalSilenceTime
	silenceRatio := 0.0
	if totalDuration > 0 {
		silenceRatio = float64(snapshot.Metrics.TotalSilenceTime) / float64(totalDuration)
	}
	if silenceRatio > 0.3 {
		s.broadcaster.BroadcastEvent(callID, "agent_alert", map[string]interface{}{
			"type":    "high_silence",
			"ratio":   silenceRatio,
			"message": "High silence ratio detected",
		})
	}

	if snapshot.Metrics.InterruptionCount > 5 {
		s.broadcaster.BroadcastEvent(callID, "agent_alert", map[string]interface{}{
			"type":    "interruptions",
			"count":   snapshot.Metrics.InterruptionCount,
			"message": "Multiple interruptions detected",
		})
	}
}

// CallCompleted handles call completion events
func (s *WebSocketSubscriber) CallCompleted(callID string, finalSnapshot *AnalyticsSnapshot) {
	if s.broadcaster == nil {
		return
	}
	if finalSnapshot == nil {
		return
	}

	// Send call completion event
	s.broadcaster.BroadcastEvent(callID, "call_completed", map[string]interface{}{
		"call_id":       callID,
		"quality_score": finalSnapshot.QualityScore,
		"talk_time":     finalSnapshot.Metrics.TotalTalkTime,
		"silence_time":  finalSnapshot.Metrics.TotalSilenceTime,
		"sentiment":     s.calculateAverageSentiment(finalSnapshot),
		"keywords":      len(finalSnapshot.Keywords),
		"violations":    len(finalSnapshot.Compliance),
	})

	// Send final analytics snapshot
	s.broadcaster.BroadcastAnalytics(callID, finalSnapshot)
}

// calculateAverageSentiment calculates the average sentiment score
func (s *WebSocketSubscriber) calculateAverageSentiment(snapshot *AnalyticsSnapshot) float64 {
	if len(snapshot.SentimentTrend) == 0 {
		return 0
	}

	var sum float64
	for _, sentiment := range snapshot.SentimentTrend {
		sum += sentiment.Score
	}
	return sum / float64(len(snapshot.SentimentTrend))
}

// TranscriptionReceived handles new transcription events
func (s *WebSocketSubscriber) TranscriptionReceived(callID string, text string, isFinal bool, speaker string) {
	if s.broadcaster == nil {
		return
	}

	// Broadcast transcription event
	s.broadcaster.BroadcastEvent(callID, "transcription", map[string]interface{}{
		"text":     text,
		"is_final": isFinal,
		"speaker":  speaker,
	})
}
