package siprec

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const rfc7865SampleMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns='urn:ietf:params:xml:ns:recording:1'>
  <datamode>complete</datamode>
  <group group_id="7+OTCyoxTmqmqyA/1weDAg==">
    <associate-time>2010-12-16T23:41:07Z</associate-time>
    <call-center xmlns='urn:ietf:params:xml:ns:callcenter'>
      <supervisor>sip:alice@atlanta.com</supervisor>
    </call-center>
    <mydata xmlns='http://example.com/my'>
      <structure>FOO!</structure>
      <whatever>bar</whatever>
    </mydata>
  </group>
  <session session_id="hVpd7YQgRW2nD22h7q60JQ==">
    <sipSessionID>ab30317f1a784dc48ff824d0d3715d86; remote=47755a9de7794ba387653f2099600ef2</sipSessionID>
    <group-ref>7+OTCyoxTmqmqyA/1weDAg==</group-ref>
  </session>
  <participant participant_id="srfBElmCRp2QB23b7Mpk0w==">
    <nameID aor="sip:bob@biloxi.com">
      <name xml:lang="it">Bob</name>
    </nameID>
  </participant>
</recording>`

func TestConvertRSParticipantToParticipant(t *testing.T) {
	// Create test RSParticipant
	rsParticipant := RSParticipant{
		ID:          "participant1",
		Name:        "John Doe",
		DisplayName: "Agent John",
		Aor: []Aor{
			{Value: "sip:john@example.com"},
			{Value: "tel:+12345678901"},
		},
		NameInfos: []RSNameID{
			{
				AOR:     "sip:john@example.com",
				URI:     "sip:john@example.com",
				Display: "Agent John",
				Names: []LocalizedName{
					{Value: "John Doe"},
				},
			},
		},
	}

	// Convert to Participant
	participant := ConvertRSParticipantToParticipant(rsParticipant)

	// Verify the conversion
	assert.Equal(t, "participant1", participant.ID, "Participant ID should match")
	assert.Equal(t, "John Doe", participant.Name, "Participant Name should match")
	assert.Equal(t, "Agent John", participant.DisplayName, "Participant DisplayName should match")
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
	assert.Contains(t, metadata, `state="recording"`, "Metadata should preserve provided state")
	assert.Contains(t, metadata, `<participant participant_id="participant1">`, "Metadata should contain participant")
	assert.Contains(t, metadata, "<name>John Doe</name>", "Metadata should contain participant name")
	assert.Contains(t, metadata, "<aor>sip:john@example.com</aor>", "Metadata should contain participant AOR")

	// Ensure original metadata was not mutated
	assert.Equal(t, "recording", rsMetadata.State, "Original metadata state should remain unchanged")
}

func TestUnmarshalRFC7865Metadata(t *testing.T) {
	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(rfc7865SampleMetadata), &metadata))

	assert.Equal(t, "complete", metadata.DataMode)
	require.Len(t, metadata.Group, 1)
	assert.Equal(t, "7+OTCyoxTmqmqyA/1weDAg==", metadata.Group[0].ID)
	require.Len(t, metadata.Sessions, 1)
	assert.Equal(t, "hVpd7YQgRW2nD22h7q60JQ==", metadata.Sessions[0].ID)
	require.Len(t, metadata.Participants, 1)
	participant := metadata.Participants[0]
	assert.Equal(t, "srfBElmCRp2QB23b7Mpk0w==", participant.ID)
	assert.Equal(t, "Bob", participant.Name)
	require.NotEmpty(t, participant.Aor)
	assert.Equal(t, "sip:bob@biloxi.com", participant.Aor[0].Value)
	assert.Equal(t, "hVpd7YQgRW2nD22h7q60JQ==", metadata.SessionID)
}

func TestValidateSiprecMessageAllowsMissingState(t *testing.T) {
	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(rfc7865SampleMetadata), &metadata))

	result := ValidateSiprecMessage(&metadata)
	require.Empty(t, result.Errors, "RFC 7865 sample should not produce validation errors")
	assert.NotEmpty(t, result.Warnings, "Missing state should yield a warning")
	found := false
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "missing recording state attribute") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected warning about missing recording state")
	assert.Equal(t, "", metadata.State, "Validation must not modify original metadata state")
}

func TestUnmarshalPreservesStateAndSequence(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns='urn:ietf:params:xml:ns:recording:1' state='paused' sequence='27'>
  <session session_id="abc"/>
  <participant participant_id="p1">
    <nameID aor="sip:alice@example.com">
      <name>Alice</name>
    </nameID>
  </participant>
</recording>`

	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(xmlData), &metadata))
	assert.Equal(t, "paused", metadata.State)
	assert.Equal(t, 27, metadata.Sequence)

	result := ValidateSiprecMessage(&metadata)
	require.Empty(t, result.Errors)
	assert.Equal(t, "paused", metadata.State, "validation should not mutate state")
	assert.Equal(t, 27, metadata.Sequence, "validation should not mutate sequence")
}

func TestValidateRealWorldSIPRECMetadata(t *testing.T) {
	const realWorldMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns='urn:ietf:params:xml:ns:recording:1'>
 <datamode>complete</datamode>
 <session session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <sipSessionID>jcukek0rhnnsq0sqrru2rk0kkuekjej2@10.18.5.64</sipSessionID>
 </session>
 <participant participant_id="XHWWTVWST5G/3G6NVExIIA==">
  <nameID aor="sip:+123@192.168.18.10;transport=udp;user=phone">
   <name>+123</name>
  </nameID>
 </participant>
 <participant participant_id="vioNTaFfTB+i4a8sSi4M+Q==">
  <nameID aor="sip:+321@192.168.22.131;transport=udp;user=phone">
   <name>+321</name>
  </nameID>
 </participant>
 <stream stream_id="7RFzdpqQRHCi5c+zpgK48g==" session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <label>1</label>
 </stream>
 <stream stream_id="JoCeAfr4SAuAaPHyFcX/Yw==" session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <label>0</label>
 </stream>
 <sessionrecordingassoc session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <associate-time>2025-10-22T12:16:48+0300</associate-time>
 </sessionrecordingassoc>
 <participantsessionassoc participant_id="XHWWTVWST5G/3G6NVExIIA==" session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <associate-time>2025-10-22T12:16:48+0300</associate-time>
 </participantsessionassoc>
 <participantsessionassoc participant_id="vioNTaFfTB+i4a8sSi4M+Q==" session_id="y5I25Gf2RuG9NQJyJkL1rw==">
  <associate-time>2025-10-22T12:16:48+0300</associate-time>
 </participantsessionassoc>
 <participantstreamassoc participant_id="XHWWTVWST5G/3G6NVExIIA==">
  <send>7RFzdpqQRHCi5c+zpgK48g==</send>
  <recv>JoCeAfr4SAuAaPHyFcX/Yw==</recv>
 </participantstreamassoc>
 <participantstreamassoc participant_id="vioNTaFfTB+i4a8sSi4M+Q==">
  <send>JoCeAfr4SAuAaPHyFcX/Yw==</send>
  <recv>7RFzdpqQRHCi5c+zpgK48g==</recv>
 </participantstreamassoc>
</recording>`

	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(realWorldMetadata), &metadata))

	assert.Equal(t, "complete", metadata.DataMode)
	assert.Equal(t, "y5I25Gf2RuG9NQJyJkL1rw==", metadata.SessionID)
	require.Len(t, metadata.Participants, 2)
	require.Len(t, metadata.Streams, 2)
	require.Len(t, metadata.ParticipantSessionAssoc, 2, "Should parse participantsessionassoc elements")
	assert.Equal(t, "XHWWTVWST5G/3G6NVExIIA==", metadata.ParticipantSessionAssoc[0].ParticipantID)
	assert.Equal(t, "y5I25Gf2RuG9NQJyJkL1rw==", metadata.ParticipantSessionAssoc[0].SessionID)

	result := ValidateSiprecMessage(&metadata)
	require.Empty(t, result.Errors, "Real-world SIPREC metadata should not produce validation errors")
	assert.NotEmpty(t, result.Warnings, "Should have warnings about missing state")
}

func TestMinimalRFC7865Compliance(t *testing.T) {
	// Test absolute minimum RFC 7865 elements: session and participant only
	const minimalMetadata = `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns='urn:ietf:params:xml:ns:recording:1'>
  <session session_id="minimal-session-123">
    <sipSessionID>minimal@test.com</sipSessionID>
  </session>
  <participant participant_id="p1">
    <nameID aor="sip:user@example.com">
      <name>Test User</name>
    </nameID>
  </participant>
</recording>`

	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(minimalMetadata), &metadata))

	assert.Equal(t, "minimal-session-123", metadata.SessionID)
	require.Len(t, metadata.Participants, 1)
	assert.Equal(t, "p1", metadata.Participants[0].ID)
	assert.Equal(t, "Test User", metadata.Participants[0].Name)

	result := ValidateSiprecMessage(&metadata)
	require.Empty(t, result.Errors, "Minimal RFC 7865 metadata should not produce validation errors")
	// Warnings are acceptable (missing state, missing streams, etc.)
	assert.NotEmpty(t, result.Warnings, "Should have warnings about missing optional elements")
}

func TestCreateMetadataResponseDefaults(t *testing.T) {
	rsMetadata := &RSMetadata{
		SessionID: "session123",
	}

	metadata, err := CreateMetadataResponse(rsMetadata)
	require.NoError(t, err, "CreateMetadataResponse should succeed without explicit state")

	assert.Contains(t, metadata, `state="active"`, "Metadata should default state to active")
	assert.Contains(t, metadata, `sequence="1"`, "Metadata should default sequence to 1")
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

func TestValidateSiprecMessageReasonMap(t *testing.T) {
	metadata := &RSMetadata{
		XMLName:   xml.Name{Space: "urn:ietf:params:xml:ns:recording:1", Local: "recording"},
		SessionID: "session-1",
		State:     "active",
		Reason:    "error",
		SessionRecordingAssoc: RSAssociation{
			SessionID: "session-1",
		},
	}

	result := ValidateSiprecMessage(metadata)
	require.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "not valid for state", "Should reject invalid reason/state combinations")
}

func TestValidateSiprecMessagePolicyAcknowledgementWarnings(t *testing.T) {
	metadata := &RSMetadata{
		XMLName:   xml.Name{Space: "urn:ietf:params:xml:ns:recording:1", Local: "recording"},
		SessionID: "session-2",
		State:     "active",
		SessionRecordingAssoc: RSAssociation{
			SessionID: "session-2",
		},
		Participants: []RSParticipant{
			{
				ID:   "participant-1",
				Name: "Agent Smith",
				Aor: []Aor{
					{Value: "sip:agent@example.com"},
				},
				Role: "active",
			},
		},
		PolicyUpdates: []PolicyUpdate{
			{
				PolicyID:     "policyA",
				Status:       "acknowledged",
				Acknowledged: false,
			},
		},
	}

	result := ValidateSiprecMessage(metadata)
	require.Empty(t, result.Errors, "Should not treat missing acknowledgement as hard error")
	require.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "policyA", "Should highlight acknowledgement inconsistency in warning")
}
