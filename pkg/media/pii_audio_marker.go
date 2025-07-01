package media

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PIIAudioMarker tracks PII detection events with timestamps for audio redaction
type PIIAudioMarker struct {
	logger    *logrus.Logger
	callUUID  string
	markers   []PIIMarker
	mutex     sync.RWMutex
	startTime time.Time
	enabled   bool
}

// PIIMarker represents a PII detection event with timing information
type PIIMarker struct {
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	PIIType      string    `json:"pii_type"`
	Confidence   float64   `json:"confidence"`
	Transcription string   `json:"transcription"`
	RedactedText string    `json:"redacted_text"`
}

// PIIAudioMarkerMetadata contains metadata for audio redaction
type PIIAudioMarkerMetadata struct {
	CallUUID      string      `json:"call_uuid"`
	RecordingPath string      `json:"recording_path"`
	TotalMarkers  int         `json:"total_markers"`
	Markers       []PIIMarker `json:"markers"`
	CreatedAt     time.Time   `json:"created_at"`
}

// NewPIIAudioMarker creates a new PII audio marker for a call
func NewPIIAudioMarker(logger *logrus.Logger, callUUID string, enabled bool) *PIIAudioMarker {
	return &PIIAudioMarker{
		logger:    logger,
		callUUID:  callUUID,
		markers:   make([]PIIMarker, 0),
		startTime: time.Now(),
		enabled:   enabled,
	}
}

// MarkPII records a PII detection event with timestamp for audio redaction
func (m *PIIAudioMarker) MarkPII(piiType string, transcription string, redactedText string, isFinal bool) {
	if !m.enabled {
		return
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Calculate timing based on when the call started
	// This is a simple approach - in a real implementation, you might want to
	// correlate with actual audio timestamps or RTP timestamps
	currentTime := time.Now()
	
	// Estimate duration of the transcription (rough calculation)
	// Assuming average speaking rate of ~150 words per minute
	words := len([]rune(transcription)) / 5 // Rough word count estimation
	estimatedDuration := time.Duration(float64(words)/2.5) * time.Second // ~150 WPM
	
	if estimatedDuration < 1*time.Second {
		estimatedDuration = 2 * time.Second // Minimum duration for redaction
	}
	if estimatedDuration > 10*time.Second {
		estimatedDuration = 10 * time.Second // Maximum duration to avoid over-redaction
	}

	marker := PIIMarker{
		StartTime:     currentTime.Add(-estimatedDuration),
		EndTime:       currentTime,
		PIIType:       piiType,
		Confidence:    1.0, // High confidence since it came from text detection
		Transcription: transcription,
		RedactedText:  redactedText,
	}

	m.markers = append(m.markers, marker)

	m.logger.WithFields(logrus.Fields{
		"call_uuid":    m.callUUID,
		"pii_type":     piiType,
		"start_time":   marker.StartTime,
		"end_time":     marker.EndTime,
		"duration":     estimatedDuration,
		"is_final":     isFinal,
		"marker_count": len(m.markers),
	}).Info("PII audio marker recorded")
}

// GetMarkers returns all PII markers for the call
func (m *PIIAudioMarker) GetMarkers() []PIIMarker {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	markers := make([]PIIMarker, len(m.markers))
	copy(markers, m.markers)
	return markers
}

// GetMarkerCount returns the number of PII markers
func (m *PIIAudioMarker) GetMarkerCount() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.markers)
}

// GenerateRedactionMetadata creates metadata for audio post-processing
func (m *PIIAudioMarker) GenerateRedactionMetadata(recordingPath string) *PIIAudioMarkerMetadata {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return &PIIAudioMarkerMetadata{
		CallUUID:      m.callUUID,
		RecordingPath: recordingPath,
		TotalMarkers:  len(m.markers),
		Markers:       append([]PIIMarker(nil), m.markers...), // Create copy
		CreatedAt:     time.Now(),
	}
}

// GetRedactionIntervals returns time intervals that should be redacted in the audio
func (m *PIIAudioMarker) GetRedactionIntervals() []RedactionInterval {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	intervals := make([]RedactionInterval, 0, len(m.markers))
	
	for _, marker := range m.markers {
		// Convert absolute times to relative offsets from recording start
		startOffset := marker.StartTime.Sub(m.startTime)
		endOffset := marker.EndTime.Sub(m.startTime)
		
		// Ensure non-negative offsets
		if startOffset < 0 {
			startOffset = 0
		}
		if endOffset < startOffset {
			endOffset = startOffset + 2*time.Second // Minimum redaction duration
		}
		
		intervals = append(intervals, RedactionInterval{
			StartOffset: startOffset,
			EndOffset:   endOffset,
			PIIType:     marker.PIIType,
			Reason:      "PII detected in transcription",
		})
	}

	return intervals
}

// RedactionInterval represents a time interval in the audio that should be redacted
type RedactionInterval struct {
	StartOffset time.Duration `json:"start_offset"`
	EndOffset   time.Duration `json:"end_offset"`
	PIIType     string        `json:"pii_type"`
	Reason      string        `json:"reason"`
}

// MergeOverlappingIntervals combines overlapping redaction intervals to avoid gaps
func MergeOverlappingIntervals(intervals []RedactionInterval) []RedactionInterval {
	if len(intervals) <= 1 {
		return intervals
	}

	// Sort intervals by start time
	for i := 0; i < len(intervals)-1; i++ {
		for j := i + 1; j < len(intervals); j++ {
			if intervals[i].StartOffset > intervals[j].StartOffset {
				intervals[i], intervals[j] = intervals[j], intervals[i]
			}
		}
	}

	merged := make([]RedactionInterval, 0, len(intervals))
	current := intervals[0]

	for i := 1; i < len(intervals); i++ {
		next := intervals[i]
		
		// Check if intervals overlap or are adjacent (within 1 second)
		if next.StartOffset <= current.EndOffset+time.Second {
			// Merge intervals
			if next.EndOffset > current.EndOffset {
				current.EndOffset = next.EndOffset
			}
			// Combine PII types
			if current.PIIType != next.PIIType {
				current.PIIType = current.PIIType + "," + next.PIIType
				current.Reason = "Multiple PII types detected"
			}
		} else {
			// No overlap, add current and move to next
			merged = append(merged, current)
			current = next
		}
	}

	// Add the last interval
	merged = append(merged, current)
	return merged
}

// GetStats returns statistics about PII audio markers
func (m *PIIAudioMarker) GetStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	typeCount := make(map[string]int)
	for _, marker := range m.markers {
		typeCount[marker.PIIType]++
	}

	var totalDuration time.Duration
	for _, marker := range m.markers {
		totalDuration += marker.EndTime.Sub(marker.StartTime)
	}

	return map[string]interface{}{
		"enabled":         m.enabled,
		"total_markers":   len(m.markers),
		"types_detected":  typeCount,
		"total_duration":  totalDuration.String(),
		"call_duration":   time.Since(m.startTime).String(),
		"call_uuid":       m.callUUID,
	}
}