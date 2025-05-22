package siprec

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConvertRSParticipantToParticipant(t *testing.T) {
	// Create test RSParticipant
	rsParticipant := RSParticipant{
		ID:     "participant1",
		NameID: "name1",
		Name:   "John Doe",
		Aor: []Aor{
			{Value: "sip:john@example.com"},
			{Value: "tel:+12345678901"},
		},
	}

	// Convert to Participant
	participant := ConvertRSParticipantToParticipant(rsParticipant)

	// Verify the conversion
	assert.Equal(t, "participant1", participant.ID, "Participant ID should match")
	assert.Equal(t, "John Doe", participant.Name, "Participant Name should match")
	assert.Equal(t, "name1", participant.DisplayName, "Participant DisplayName should match")
	assert.Len(t, participant.CommunicationIDs, 2, "Should have 2 CommunicationIDs")

	// Check first CommunicationID (SIP)
	assert.Equal(t, "sip", participant.CommunicationIDs[0].Type, "First CommunicationID type should be sip")
	assert.Equal(t, "sip:john@example.com", participant.CommunicationIDs[0].Value, "First CommunicationID value should match")

	// Check second CommunicationID (SIP) - the function always creates SIP types
	assert.Equal(t, "sip", participant.CommunicationIDs[1].Type, "Second CommunicationID type should be sip")
	assert.Equal(t, "tel:+12345678901", participant.CommunicationIDs[1].Value, "Second CommunicationID value should match")
}

func TestCreateMultipartResponse(t *testing.T) {
	// Create test data
	sdp := "v=0\r\no=- 123456 2 IN IP4 192.168.1.100\r\ns=SIPREC Test\r\nc=IN IP4 192.168.1.100\r\nt=0 0\r\nm=audio 10000 RTP/AVP 0 8\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:8 PCMA/8000\r\n"

	metadata := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="some-uuid" state="recording">
  <participant id="participant1">
    <name>John Doe</name>
    <aor>sip:john@example.com</aor>
  </participant>
</recording>`

	// Create multipart response
	contentType, body := CreateMultipartResponse(sdp, metadata)

	// Verify content type contains boundary
	assert.Contains(t, contentType, "multipart/mixed", "Content-Type should be multipart/mixed")
	assert.Contains(t, contentType, "boundary=", "Content-Type should contain boundary parameter")

	// Extract boundary (for debugging if needed)
	_ = contentType[strings.Index(contentType, "boundary=")+9:]

	// Verify body contains both parts
	assert.Contains(t, body, "Content-Type: application/sdp", "Body should contain SDP part")
	assert.Contains(t, body, "Content-Type: application/rs-metadata+xml", "Body should contain metadata part")
	assert.Contains(t, body, sdp, "Body should contain SDP content")
	assert.Contains(t, body, metadata, "Body should contain metadata content")
	assert.Contains(t, body, "--", "Body should contain boundary markers")
}

func TestCreateMetadataResponse(t *testing.T) {
	// Create test RSMetadata
	rsMetadata := &RSMetadata{
		SessionID: "session123",
		State:     "recording",
		Participants: []RSParticipant{
			{
				ID:   "participant1",
				Name: "John Doe",
				Aor: []Aor{
					{Value: "sip:john@example.com"},
				},
			},
		},
	}

	// Create metadata response
	metadata, err := CreateMetadataResponse(rsMetadata)

	// Verify results
	assert.NoError(t, err, "CreateMetadataResponse should not return an error")
	assert.NotEmpty(t, metadata, "Metadata response should not be empty")
	assert.Contains(t, metadata, `xmlns="urn:ietf:params:xml:ns:recording:1"`, "Metadata should contain namespace")
	assert.Contains(t, metadata, `session="session123"`, "Metadata should contain session ID")
	// The state is actually converted to "active" in the implementation
	assert.Contains(t, metadata, `state="active"`, "Metadata should contain state")
	assert.Contains(t, metadata, `<participant id="participant1">`, "Metadata should contain participant")
	assert.Contains(t, metadata, "<name>John Doe</name>", "Metadata should contain participant name")
	assert.Contains(t, metadata, "<aor>sip:john@example.com</aor>", "Metadata should contain participant AOR")
}

func TestRecordingSession(t *testing.T) {
	// Create test RecordingSession
	session := &RecordingSession{
		ID:             "session123",
		AssociatedTime: time.Now(),
		RecordingState: "recording",
		RecordingType:  "full",
		Participants: []Participant{
			{
				ID:          "participant1",
				Name:        "John Doe",
				DisplayName: "John Doe",
				Role:        "active",
				CommunicationIDs: []CommunicationID{
					{
						Type:    "sip",
						Value:   "sip:john@example.com",
						Purpose: "from",
					},
				},
			},
		},
	}

	// Test basic properties
	assert.Equal(t, "session123", session.ID, "Session ID should match")
	assert.Equal(t, "recording", session.RecordingState, "Recording state should match")
	assert.Equal(t, "full", session.RecordingType, "Recording type should match")
	assert.Len(t, session.Participants, 1, "Should have 1 participant")

	// Test participant properties
	participant := session.Participants[0]
	assert.Equal(t, "participant1", participant.ID, "Participant ID should match")
	assert.Equal(t, "John Doe", participant.Name, "Participant name should match")
	assert.Equal(t, "active", participant.Role, "Participant role should match")
	assert.Len(t, participant.CommunicationIDs, 1, "Should have 1 communication ID")

	// Test communication ID properties
	commID := participant.CommunicationIDs[0]
	assert.Equal(t, "sip", commID.Type, "Communication ID type should match")
	assert.Equal(t, "sip:john@example.com", commID.Value, "Communication ID value should match")
	assert.Equal(t, "from", commID.Purpose, "Communication ID purpose should match")
}
