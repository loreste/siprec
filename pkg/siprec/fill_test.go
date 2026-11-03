package siprec

import (
	"encoding/xml"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const metadataWithoutSessionOrParticipants = `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" state="active" sequence="1"/>`

func unmarshalMetadata(t *testing.T, raw string) *RSMetadata {
	t.Helper()
	var metadata RSMetadata
	require.NoError(t, xml.Unmarshal([]byte(raw), &metadata))
	return &metadata
}

func TestFillMissingUsesCallIDAsSessionID(t *testing.T) {
	metadata := unmarshalMetadata(t, metadataWithoutSessionOrParticipants)

	FillMissing(metadata, FillContext{CallID: "abc123@sbc.example.com"})

	assert.Equal(t, "abc123@sbc.example.com", metadata.SessionID)
	require.Len(t, metadata.Sessions, 1)
	assert.Equal(t, "abc123@sbc.example.com", metadata.Sessions[0].ID)
	assert.Equal(t, "abc123@sbc.example.com", metadata.SessionRecordingAssoc.SessionID)
	assert.Equal(t, "abc123@sbc.example.com", metadata.SessionRecordingAssoc.CallID)
	assert.Equal(t, []string{FilledSessionID}, metadata.FilledFields)
}

func TestFillMissingKeepsExistingValues(t *testing.T) {
	metadata := unmarshalMetadata(t, `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="orig-session" state="active">
  <session session_id="orig-session"/>
  <participant participant_id="p1"><aor>sip:alice@example.com</aor></participant>
</recording>`)

	FillMissing(metadata, FillContext{CallID: "call-1", CallingParticipant: "<sip:bob@example.com>"})

	assert.Equal(t, "orig-session", metadata.SessionID)
	require.Len(t, metadata.Participants, 1)
	assert.Equal(t, "p1", metadata.Participants[0].ID)
	assert.Empty(t, metadata.FilledFields)
}

func TestFillMissingNamesLoneSessionElement(t *testing.T) {
	metadata := unmarshalMetadata(t, `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" state="active">
  <session><sipSessionID>x</sipSessionID></session>
</recording>`)

	FillMissing(metadata, FillContext{CallID: "call-2"})

	require.Len(t, metadata.Sessions, 1)
	assert.Equal(t, "call-2", metadata.Sessions[0].ID)
}

func TestFillMissingAddsCallingParticipant(t *testing.T) {
	tests := []struct {
		name        string
		header      string
		wantDisplay string
		wantURI     string
	}{
		{"name-addr", `"Alice Smith" <sip:+15551234567@pbx.example.com>;tag=1`, "Alice Smith", "sip:+15551234567@pbx.example.com"},
		{"angle brackets only", "<tel:+15551234567>", "", "tel:+15551234567"},
		{"bare uri", "sip:alice@example.com", "", "sip:alice@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := unmarshalMetadata(t, metadataWithoutSessionOrParticipants)

			FillMissing(metadata, FillContext{CallID: "call-3", CallingParticipant: tt.header})

			require.Len(t, metadata.Participants, 1)
			participant := metadata.Participants[0]
			assert.Equal(t, CallingParticipantID, participant.ID)
			assert.Equal(t, tt.wantDisplay, participant.DisplayName)
			require.Len(t, participant.Aor, 1)
			assert.Equal(t, tt.wantURI, participant.Aor[0].Value)
			assert.Contains(t, metadata.FilledFields, FilledParticipant)
		})
	}
}

func TestFillMissingWithoutCallingParticipantAddsNoParticipant(t *testing.T) {
	metadata := unmarshalMetadata(t, metadataWithoutSessionOrParticipants)

	FillMissing(metadata, FillContext{CallID: "call-4"})

	assert.Empty(t, metadata.Participants)
	assert.NotContains(t, metadata.FilledFields, FilledParticipant)
}

func TestFilledMetadataPassesValidation(t *testing.T) {
	metadata := unmarshalMetadata(t, metadataWithoutSessionOrParticipants)

	strict := ValidateSiprecMessage(metadata)
	assert.Contains(t, strict.Errors, "missing recording session ID")
	assert.Contains(t, strict.Errors, "no participants provided in metadata")

	FillMissing(metadata, FillContext{CallID: "call-5"})
	result := ValidateSiprecMessageWithOptions(metadata, ValidationOptions{AllowMissingParticipants: true})

	assert.Empty(t, result.Errors)
	assert.Contains(t, result.Warnings, "no participants provided in metadata (continuing without participants)")
}

func TestFilledFieldsNotSerialised(t *testing.T) {
	metadata := unmarshalMetadata(t, metadataWithoutSessionOrParticipants)
	FillMissing(metadata, FillContext{CallID: "call-6"})

	response, err := CreateMetadataResponse(metadata)

	require.NoError(t, err)
	assert.Contains(t, response, `session="call-6"`)
	assert.NotContains(t, response, "FilledFields")
}

func TestFillMissingRejectsInvalidParticipantHeaders(t *testing.T) {
	for _, value := range []string{"Alice", "<sip:alice@example.com", "sip:alice@", "sip:alice@bad host", "http://example.com", "*", "sip:a:password@example.com", "<sip:a@example.com>, <sip:b@example.com>", "<sip:a@example.com> garbage", "sip:a@example.com\r\nX-Injected: yes", "tel:5551234", "tel:+abc", "sip:alice@example.com:99999"} {
		t.Run(value, func(t *testing.T) {
			metadata := &RSMetadata{}
			FillMissing(metadata, FillContext{CallID: "call", CallingParticipant: value})
			require.Empty(t, metadata.Participants)
			require.NotContains(t, metadata.FilledFields, FilledParticipant)
		})
	}
}
func TestFillMissingDoesNotGuessAmongSessions(t *testing.T) {
	metadata := &RSMetadata{Sessions: []RSSession{{ID: "one"}, {ID: "two"}}}
	FillMissing(metadata, FillContext{CallID: "call"})
	require.Empty(t, metadata.SessionID)
	require.Equal(t, "one", metadata.Sessions[0].ID)
	require.Equal(t, "two", metadata.Sessions[1].ID)
	require.NotEmpty(t, ValidateSiprecMessage(metadata).Errors)
}
func TestFillMissingPreservesEstablishedSession(t *testing.T) {
	metadata := &RSMetadata{}
	ctx := FillContext{CallID: "sip-dialog", SessionID: "recording-id", CallingParticipant: "sip:a@example.com"}
	FillMissing(metadata, ctx)
	require.Equal(t, "recording-id", metadata.SessionID)
	require.Equal(t, "sip-dialog", metadata.SessionRecordingAssoc.CallID)
	require.True(t, metadata.ParticipantSynthesized(CallingParticipantID))
	fields := append([]string(nil), metadata.FilledFields...)
	FillMissing(metadata, ctx)
	require.Equal(t, fields, metadata.FilledFields)
}

func TestFallbackParticipantAddressEdgeCases(t *testing.T) {
	for _, value := range []string{"sip:a%XX@example.com", "sip:a%00@example.com", "sip:a%0d%0a@example.com", "sip:a@[example.com]", "tel:+15551234;ext=56", "<tel:+15551234;ext=56>"} {
		t.Run(value, func(t *testing.T) { _, uri := parseNameAddr(value); require.Empty(t, uri) })
	}
	for _, value := range []string{"sip:alice@example.com.", "sips:alice@example.com", "sip:alice@[2001:db8::1]", "sip:alice@example.com;tag=abc"} {
		t.Run(value, func(t *testing.T) { _, uri := parseNameAddr(value); require.NotEmpty(t, uri) })
	}
}

func FuzzFallbackParticipantAddress(f *testing.F) {
	for _, seed := range []string{"", "sip:alice@example.com", `"Alice" <sip:alice@example.com>;tag=1`, "<tel:+15551234>", "sip:a@[::1]", "<sip:alice@example.com;transport=tcp>", "sip:a%0d%0a@example.com", "<sip:a@example.com>, <sip:b@example.com>", "\x00\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		_, uri := parseNameAddr(value)
		if uri == "" {
			return
		}
		if strings.IndexFunc(uri, unicode.IsControl) >= 0 {
			t.Fatal("accepted control characters")
		}
		_, again := parseNameAddr("<" + uri + ">")
		if again != uri {
			t.Fatalf("address does not round trip: %q -> %q", uri, again)
		}
		metadata := &RSMetadata{}
		ctx := FillContext{CallID: "dialog", CallingParticipant: value}
		FillMissing(metadata, ctx)
		fields := append([]string(nil), metadata.FilledFields...)
		FillMissing(metadata, ctx)
		require.Equal(t, fields, metadata.FilledFields)
		require.Len(t, metadata.Participants, 1)
	})
}
