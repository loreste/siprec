package siprec

import (
	"strings"
	"testing"
	"time"
)

func TestCreateFailoverMetadata(t *testing.T) {
	// Create a sample recording session
	session := &RecordingSession{
		ID:             "test-session-123",
		RecordingState: "recording",
		SequenceNumber: 1,
		Participants: []Participant{
			{
				ID:          "participant1",
				Name:        "Alice",
				DisplayName: "Alice Smith",
				Role:        "active",
				CommunicationIDs: []CommunicationID{
					{
						Type:        "sip",
						Value:       "sip:alice@example.com",
						DisplayName: "Alice",
					},
				},
			},
		},
	}

	// Generate failover metadata
	metadata := CreateFailoverMetadata(session)

	// Validate the generated metadata
	if metadata.SessionID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, metadata.SessionID)
	}

	if metadata.State != session.RecordingState {
		t.Errorf("Expected state %s, got %s", session.RecordingState, metadata.State)
	}

	if metadata.Sequence != session.SequenceNumber+1 {
		t.Errorf("Expected sequence %d, got %d", session.SequenceNumber+1, metadata.Sequence)
	}

	if metadata.Reason != "failover" {
		t.Errorf("Expected reason 'failover', got %s", metadata.Reason)
	}

	if len(metadata.Participants) != 1 {
		t.Errorf("Expected 1 participant, got %d", len(metadata.Participants))
	}

	if metadata.SessionRecordingAssoc.SessionID != session.ID {
		t.Errorf("Expected association session ID %s, got %s", session.ID, metadata.SessionRecordingAssoc.SessionID)
	}

	// Verify FixedID was generated
	if metadata.SessionRecordingAssoc.FixedID == "" {
		t.Errorf("Expected a non-empty FixedID")
	}
}

func TestParseFailoverMetadata(t *testing.T) {
	// Create test metadata
	metadata := &RSMetadata{
		SessionID: "test-session-123",
		SessionRecordingAssoc: RSAssociation{
			SessionID: "test-session-123",
			FixedID:   "failover-id-456",
		},
	}

	// Parse the metadata
	sessionID, failoverID, err := ParseFailoverMetadata(metadata)
	if err != nil {
		t.Fatalf("ParseFailoverMetadata failed: %v", err)
	}

	// Validate the parsed values
	if sessionID != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got %s", sessionID)
	}

	if failoverID != "failover-id-456" {
		t.Errorf("Expected failover ID 'failover-id-456', got %s", failoverID)
	}

	// Test error cases
	// Missing FixedID
	badMetadata1 := &RSMetadata{
		SessionID: "test-session-123",
		SessionRecordingAssoc: RSAssociation{
			SessionID: "test-session-123",
		},
	}
	_, _, err = ParseFailoverMetadata(badMetadata1)
	if err == nil {
		t.Errorf("Expected error for missing failover ID, got nil")
	}

	// Missing SessionID
	badMetadata2 := &RSMetadata{
		SessionRecordingAssoc: RSAssociation{
			FixedID: "failover-id-456",
		},
	}
	_, _, err = ParseFailoverMetadata(badMetadata2)
	if err == nil {
		t.Errorf("Expected error for missing session ID, got nil")
	}

	// Nil metadata
	_, _, err = ParseFailoverMetadata(nil)
	if err == nil {
		t.Errorf("Expected error for nil metadata, got nil")
	}
}

func TestCreateReplacesHeader(t *testing.T) {
	// Create a test session
	session := &RecordingSession{
		ID: "call-123",
	}

	// Test with dialog ID
	dialogID := "call-123;to-tag=tag1;from-tag=tag2"
	result := CreateReplacesHeader(session, dialogID, false)
	expected := "call-123;to-tag=tag1;from-tag=tag2"
	if result != expected {
		t.Errorf("Expected Replaces header '%s', got '%s'", expected, result)
	}

	// Test with early flag
	result = CreateReplacesHeader(session, dialogID, true)
	expected = "call-123;to-tag=tag1;from-tag=tag2;early-only"
	if result != expected {
		t.Errorf("Expected Replaces header '%s', got '%s'", expected, result)
	}

	// Test without dialog ID
	result = CreateReplacesHeader(session, "", false)
	expected = "call-123"
	if result != expected {
		t.Errorf("Expected Replaces header '%s', got '%s'", expected, result)
	}
}

func TestParseReplacesHeader(t *testing.T) {
	// Test valid header
	replacesHeader := "call-123;to-tag=tag1;from-tag=tag2"
	callID, toTag, fromTag, earlyOnly, err := ParseReplacesHeader(replacesHeader)
	if err != nil {
		t.Fatalf("ParseReplacesHeader failed: %v", err)
	}
	if callID != "call-123" {
		t.Errorf("Expected call ID 'call-123', got '%s'", callID)
	}
	if toTag != "tag1" {
		t.Errorf("Expected to-tag 'tag1', got '%s'", toTag)
	}
	if fromTag != "tag2" {
		t.Errorf("Expected from-tag 'tag2', got '%s'", fromTag)
	}
	if earlyOnly {
		t.Errorf("Expected earlyOnly to be false")
	}

	// Test with early-only flag
	replacesHeader = "call-123;to-tag=tag1;from-tag=tag2;early-only"
	_, _, _, earlyOnly, err = ParseReplacesHeader(replacesHeader)
	if err != nil {
		t.Fatalf("ParseReplacesHeader failed: %v", err)
	}
	if !earlyOnly {
		t.Errorf("Expected earlyOnly to be true")
	}

	// Test invalid header
	_, _, _, _, err = ParseReplacesHeader("invalid")
	if err == nil {
		t.Errorf("Expected error for invalid header, got nil")
	}

	// Test empty header
	_, _, _, _, err = ParseReplacesHeader("")
	if err == nil {
		t.Errorf("Expected error for empty header, got nil")
	}
}

func TestSerializeMetadata(t *testing.T) {
	// Create a simple metadata object
	metadata := &RSMetadata{
		SessionID: "test-session-123",
		State:     "recording",
		Participants: []RSParticipant{
			{
				ID:   "p1",
				Name: "Alice",
			},
		},
	}

	// Serialize to XML
	xml, err := SerializeMetadata(metadata)
	if err != nil {
		t.Fatalf("SerializeMetadata failed: %v", err)
	}

	// Verify content
	if !strings.Contains(xml, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Errorf("Expected XML to contain declaration")
	}
	if !strings.Contains(xml, `session="test-session-123"`) {
		t.Errorf("Expected XML to contain session ID")
	}
	if !strings.Contains(xml, `state="recording"`) {
		t.Errorf("Expected XML to contain state")
	}
	if !strings.Contains(xml, `id="p1"`) {
		t.Errorf("Expected XML to contain participant ID")
	}

	// Test nil metadata
	_, err = SerializeMetadata(nil)
	if err == nil {
		t.Errorf("Expected error for nil metadata, got nil")
	}
}

func TestRecoverSession(t *testing.T) {
	// Create test metadata
	metadata := &RSMetadata{
		SessionID: "test-session-123",
		State:     "recording",
		Sequence:  2,
		Participants: []RSParticipant{
			{
				ID:     "p1",
				Name:   "Alice",
				NameID: "Alice Smith",
				Aor: []Aor{
					{
						Value: "sip:alice@example.com",
					},
				},
			},
		},
		SessionRecordingAssoc: RSAssociation{
			SessionID: "test-session-123",
			FixedID:   "failover-id-456",
		},
	}

	// Recover session
	session, err := RecoverSession(metadata)
	if err != nil {
		t.Fatalf("RecoverSession failed: %v", err)
	}

	// Validate recovered session
	if session.ID != "test-session-123" {
		t.Errorf("Expected session ID 'test-session-123', got '%s'", session.ID)
	}
	if session.RecordingState != "recording" {
		t.Errorf("Expected state 'recording', got '%s'", session.RecordingState)
	}
	if session.SequenceNumber != 2 {
		t.Errorf("Expected sequence 2, got %d", session.SequenceNumber)
	}
	if session.FailoverID != "failover-id-456" {
		t.Errorf("Expected failover ID 'failover-id-456', got '%s'", session.FailoverID)
	}
	if len(session.Participants) != 1 {
		t.Fatalf("Expected 1 participant, got %d", len(session.Participants))
	}
	if session.Participants[0].ID != "p1" {
		t.Errorf("Expected participant ID 'p1', got '%s'", session.Participants[0].ID)
	}
	if session.Participants[0].Name != "Alice" {
		t.Errorf("Expected participant name 'Alice', got '%s'", session.Participants[0].Name)
	}

	// Test error cases
	// Nil metadata
	_, err = RecoverSession(nil)
	if err == nil {
		t.Errorf("Expected error for nil metadata, got nil")
	}
}

func TestProcessStreamRecovery(t *testing.T) {
	// Create test session and metadata
	session := &RecordingSession{
		ID: "test-session-123",
		Participants: []Participant{
			{
				ID: "p1",
			},
		},
	}

	metadata := &RSMetadata{
		Streams: []Stream{
			{
				Label:    "audio1",
				StreamID: "stream1",
				Type:     "audio",
			},
			{
				Label:    "video1",
				StreamID: "stream2",
				Type:     "video",
			},
		},
		Participants: []RSParticipant{
			{
				ID:   "p1",
				Send: []string{"audio1", "video1"},
			},
		},
	}

	// Process stream recovery
	ProcessStreamRecovery(session, metadata)

	// Validate the result
	if len(session.MediaStreamTypes) != 2 {
		t.Fatalf("Expected 2 media stream types, got %d", len(session.MediaStreamTypes))
	}

	foundAudio := false
	foundVideo := false
	for _, streamType := range session.MediaStreamTypes {
		if streamType == "audio" {
			foundAudio = true
		}
		if streamType == "video" {
			foundVideo = true
		}
	}

	if !foundAudio {
		t.Errorf("Expected 'audio' in media stream types")
	}
	if !foundVideo {
		t.Errorf("Expected 'video' in media stream types")
	}

	// Verify streams were associated with participant
	if len(session.Participants[0].MediaStreams) != 2 {
		t.Fatalf("Expected 2 streams for participant, got %d", len(session.Participants[0].MediaStreams))
	}

	foundStream1 := false
	foundStream2 := false
	for _, streamID := range session.Participants[0].MediaStreams {
		if streamID == "stream1" {
			foundStream1 = true
		}
		if streamID == "stream2" {
			foundStream2 = true
		}
	}

	if !foundStream1 {
		t.Errorf("Expected 'stream1' in participant streams")
	}
	if !foundStream2 {
		t.Errorf("Expected 'stream2' in participant streams")
	}
}

func TestValidateSessionContinuity(t *testing.T) {
	// Create test sessions
	original := &RecordingSession{
		ID:         "test-session-123",
		FailoverID: "failover-id-456",
		Participants: []Participant{
			{ID: "p1"},
			{ID: "p2"},
		},
	}

	// Valid recovery
	recovered1 := &RecordingSession{
		ID:         "test-session-123",
		FailoverID: "failover-id-456",
		Participants: []Participant{
			{ID: "p1"},
			{ID: "p2"},
			{ID: "p3"}, // Extra participant is ok
		},
	}
	err := ValidateSessionContinuity(original, recovered1)
	if err != nil {
		t.Errorf("Expected no error for valid recovery, got: %v", err)
	}

	// Invalid: Different session ID
	recovered2 := &RecordingSession{
		ID:         "different-id",
		FailoverID: "failover-id-456",
		Participants: []Participant{
			{ID: "p1"},
			{ID: "p2"},
		},
	}
	err = ValidateSessionContinuity(original, recovered2)
	if err == nil {
		t.Errorf("Expected error for different session ID")
	}

	// Invalid: Different failover ID
	recovered3 := &RecordingSession{
		ID:         "test-session-123",
		FailoverID: "different-failover-id",
		Participants: []Participant{
			{ID: "p1"},
			{ID: "p2"},
		},
	}
	err = ValidateSessionContinuity(original, recovered3)
	if err == nil {
		t.Errorf("Expected error for different failover ID")
	}

	// Invalid: Missing essential participant
	recovered4 := &RecordingSession{
		ID:         "test-session-123",
		FailoverID: "failover-id-456",
		Participants: []Participant{
			{ID: "p1"},
			// p2 is missing
		},
	}
	err = ValidateSessionContinuity(original, recovered4)
	if err == nil {
		t.Errorf("Expected error for missing participant")
	}
}

func TestSetSessionExpiration(t *testing.T) {
	// Create test session
	session := &RecordingSession{
		ID: "test-session-123",
	}

	// Test with start time already set
	startTime := time.Now().Add(-1 * time.Hour)
	session.StartTime = startTime

	// Set expiration
	SetSessionExpiration(session, 24*time.Hour)

	// Validate
	expectedEnd := startTime.Add(24 * time.Hour)
	if session.RetentionPeriod != 24*time.Hour {
		t.Errorf("Expected retention period of 24h, got %v", session.RetentionPeriod)
	}

	// Allow a small tolerance for time calculations
	timeDiff := session.EndTime.Sub(expectedEnd)
	if timeDiff < -time.Second || timeDiff > time.Second {
		t.Errorf("End time not within expected range")
	}

	// Test with no start time set
	session = &RecordingSession{
		ID: "test-session-123",
	}

	before := time.Now()
	SetSessionExpiration(session, 12*time.Hour)
	after := time.Now()

	// Validate
	if session.RetentionPeriod != 12*time.Hour {
		t.Errorf("Expected retention period of 12h, got %v", session.RetentionPeriod)
	}

	// StartTime should be set to current time
	if session.StartTime.Before(before) || session.StartTime.After(after) {
		t.Errorf("Start time should be between test boundaries")
	}

	// EndTime should be start time + 12h
	expectedEnd = session.StartTime.Add(12 * time.Hour)
	timeDiff = session.EndTime.Sub(expectedEnd)
	if timeDiff < -time.Second || timeDiff > time.Second {
		t.Errorf("End time not within expected range")
	}
}

func TestGenerateSessionUpdateNotification(t *testing.T) {
	// Create test session
	now := time.Now()
	session := &RecordingSession{
		ID:             "test-session-123",
		RecordingState: "paused",
		SequenceNumber: 5,
		FailoverID:     "failover-id-456",
		StartTime:      now.Add(-1 * time.Hour),
		EndTime:        now.Add(23 * time.Hour),
		Participants: []Participant{
			{
				ID:          "p1",
				DisplayName: "Alice",
				Role:        "active",
				CommunicationIDs: []CommunicationID{
					{
						Type:        "sip",
						Value:       "sip:alice@example.com",
						DisplayName: "Alice",
					},
				},
			},
		},
	}

	// Generate notification
	metadata := GenerateSessionUpdateNotification(session, "maintenance")

	// Validate
	if metadata.SessionID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, metadata.SessionID)
	}
	if metadata.State != session.RecordingState {
		t.Errorf("Expected state %s, got %s", session.RecordingState, metadata.State)
	}
	if metadata.Sequence != session.SequenceNumber+1 {
		t.Errorf("Expected sequence %d, got %d", session.SequenceNumber+1, metadata.Sequence)
	}
	if metadata.Reason != "maintenance" {
		t.Errorf("Expected reason 'maintenance', got %s", metadata.Reason)
	}

	// Check expiration
	if !strings.Contains(metadata.Expires, session.EndTime.Format(time.RFC3339)[:10]) {
		t.Errorf("Expiration date should contain the session end date")
	}

	// Check participants
	if len(metadata.Participants) != 1 {
		t.Fatalf("Expected 1 participant, got %d", len(metadata.Participants))
	}
	if metadata.Participants[0].ID != "p1" {
		t.Errorf("Expected participant ID 'p1', got %s", metadata.Participants[0].ID)
	}
	if metadata.Participants[0].Role != "active" {
		t.Errorf("Expected role 'active', got %s", metadata.Participants[0].Role)
	}

	// Check association
	if metadata.SessionRecordingAssoc.SessionID != session.ID {
		t.Errorf("Expected association session ID %s, got %s",
			session.ID, metadata.SessionRecordingAssoc.SessionID)
	}
	if metadata.SessionRecordingAssoc.FixedID != session.FailoverID {
		t.Errorf("Expected association fixed ID %s, got %s",
			session.FailoverID, metadata.SessionRecordingAssoc.FixedID)
	}
}
