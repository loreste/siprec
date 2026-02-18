package siprec

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveStreamParticipant_NilMetadata(t *testing.T) {
	var m *RSMetadata
	assert.Nil(t, m.ResolveStreamParticipant("0"))
}

func TestResolveStreamParticipant_EmptyLabel(t *testing.T) {
	m := &RSMetadata{}
	assert.Nil(t, m.ResolveStreamParticipant(""))
}

func TestResolveStreamParticipant_ViaParticipantStreamAssoc(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "Alice", Role: "caller"},
			{ID: "p2", DisplayName: "Bob", Role: "agent"},
		},
		ParticipantStreamAssoc: []RSParticipantStreamAssoc{
			{ParticipantID: "p1", StreamID: "0", Send: []string{"0"}},
			{ParticipantID: "p2", StreamID: "1", Send: []string{"1"}},
		},
	}

	p := m.ResolveStreamParticipant("0")
	assert.NotNil(t, p)
	assert.Equal(t, "Alice", p.DisplayName)
	assert.Equal(t, "caller", p.Role)

	p2 := m.ResolveStreamParticipant("1")
	assert.NotNil(t, p2)
	assert.Equal(t, "Bob", p2.DisplayName)
}

func TestResolveStreamParticipant_ViaParticipantStreamAssocLegacyFields(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", Name: "Alice"},
		},
		ParticipantStreamAssoc: []RSParticipantStreamAssoc{
			{Participant: "p1", Stream: "leg0"},
		},
	}

	p := m.ResolveStreamParticipant("leg0")
	assert.NotNil(t, p)
	assert.Equal(t, "Alice", p.Name)
}

func TestResolveStreamParticipant_ViaStreamParticipantRef(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "Carol", Role: "agent"},
		},
		Streams: []Stream{
			{Label: "stream0", ParticipantRef: []string{"p1"}},
		},
	}

	p := m.ResolveStreamParticipant("stream0")
	assert.NotNil(t, p)
	assert.Equal(t, "Carol", p.DisplayName)
	assert.Equal(t, "agent", p.Role)
}

func TestResolveStreamParticipant_ViaStreamID(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "Dave"},
		},
		Streams: []Stream{
			{StreamID: "s1", ParticipantRef: []string{"p1"}},
		},
	}

	p := m.ResolveStreamParticipant("s1")
	assert.NotNil(t, p)
	assert.Equal(t, "Dave", p.DisplayName)
}

func TestResolveStreamParticipant_ViaParticipantSend(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "Eve", Role: "caller", Send: []string{"0"}},
			{ID: "p2", DisplayName: "Frank", Role: "agent", Send: []string{"1"}},
		},
	}

	p := m.ResolveStreamParticipant("0")
	assert.NotNil(t, p)
	assert.Equal(t, "Eve", p.DisplayName)

	p2 := m.ResolveStreamParticipant("1")
	assert.NotNil(t, p2)
	assert.Equal(t, "Frank", p2.DisplayName)
}

func TestResolveStreamParticipant_NoMatch(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "Grace", Send: []string{"0"}},
		},
	}

	assert.Nil(t, m.ResolveStreamParticipant("99"))
}

func TestResolveStreamParticipant_PriorityOrder(t *testing.T) {
	// ParticipantStreamAssoc should take priority over Stream.ParticipantRef and Send
	m := &RSMetadata{
		Participants: []RSParticipant{
			{ID: "p1", DisplayName: "AssocMatch", Role: "caller"},
			{ID: "p2", DisplayName: "RefMatch", Role: "agent", Send: []string{"0"}},
		},
		ParticipantStreamAssoc: []RSParticipantStreamAssoc{
			{ParticipantID: "p1", StreamID: "0"},
		},
		Streams: []Stream{
			{Label: "0", ParticipantRef: []string{"p2"}},
		},
	}

	p := m.ResolveStreamParticipant("0")
	assert.NotNil(t, p)
	assert.Equal(t, "AssocMatch", p.DisplayName, "ParticipantStreamAssoc should have highest priority")
}

func TestExtractOracleExtensions_UCID(t *testing.T) {
	// Test Oracle UCID extraction from extension XML
	extensions := []XMLExtension{
		{
			XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
			InnerXML: `<apkt:ucid>00FA080018803B69810C6D;encoding=hex</apkt:ucid><apkt:callerOrig>true</apkt:callerOrig>`,
		},
	}

	data := ExtractOracleExtensions(extensions)
	assert.NotNil(t, data)
	assert.Equal(t, "00FA080018803B69810C6D", data.UCID)
	assert.True(t, data.CallerOrig)
}

func TestExtractOracleExtensions_CallingParty(t *testing.T) {
	// Test Oracle callingParty extraction
	extensions := []XMLExtension{
		{
			XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
			InnerXML: `<apkt:callingParty>true</apkt:callingParty>`,
		},
	}

	data := ExtractOracleExtensions(extensions)
	assert.NotNil(t, data)
	assert.True(t, data.CallingParty)
}

func TestExtractOracleExtensions_NoOracleData(t *testing.T) {
	// Test with non-Oracle extensions
	extensions := []XMLExtension{
		{
			XMLName:  xml.Name{Space: "http://nice.com/extension", Local: "nicedata"},
			InnerXML: `<interaction-id>12345</interaction-id>`,
		},
	}

	data := ExtractOracleExtensions(extensions)
	assert.Nil(t, data)
}

func TestExtractOracleExtensions_Empty(t *testing.T) {
	data := ExtractOracleExtensions(nil)
	assert.Nil(t, data)

	data = ExtractOracleExtensions([]XMLExtension{})
	assert.Nil(t, data)
}

func TestGetOracleSessionExtensions(t *testing.T) {
	m := &RSMetadata{
		Sessions: []RSSession{
			{
				ID: "session1",
				Extensions: []XMLExtension{
					{
						XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
						InnerXML: `<apkt:ucid>ABCD1234;encoding=hex</apkt:ucid><apkt:callerOrig>false</apkt:callerOrig>`,
					},
				},
			},
		},
	}

	data := m.GetOracleSessionExtensions()
	assert.NotNil(t, data)
	assert.Equal(t, "ABCD1234", data.UCID)
	assert.False(t, data.CallerOrig)
}

func TestGetOracleParticipantExtensions(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{
				ID: "p1",
				Extensions: []XMLExtension{
					{
						XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
						InnerXML: `<apkt:callingParty>true</apkt:callingParty>`,
					},
				},
			},
			{
				ID: "p2",
				Extensions: []XMLExtension{
					{
						XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
						InnerXML: `<apkt:callingParty>false</apkt:callingParty>`,
					},
				},
			},
		},
	}

	exts := m.GetOracleParticipantExtensions()
	assert.NotNil(t, exts)
	assert.Len(t, exts, 2)
	assert.True(t, exts["p1"].CallingParty)
	assert.False(t, exts["p2"].CallingParty)
}

func TestIdentifyCallingParticipant(t *testing.T) {
	m := &RSMetadata{
		Participants: []RSParticipant{
			{
				ID: "caller-001",
				Extensions: []XMLExtension{
					{
						XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
						InnerXML: `<apkt:callingParty>true</apkt:callingParty>`,
					},
				},
			},
			{
				ID: "callee-002",
				Extensions: []XMLExtension{
					{
						XMLName:  xml.Name{Space: "http://acmepacket.com/siprec/extensiondata", Local: "extensiondata"},
						InnerXML: `<apkt:callingParty>false</apkt:callingParty>`,
					},
				},
			},
		},
	}

	callerID := m.IdentifyCallingParticipant()
	assert.Equal(t, "caller-001", callerID)
}

func TestNormalize_DataModeCamelCase(t *testing.T) {
	// Test that Avaya's camelCase dataMode is normalized
	m := &RSMetadata{
		DataModeAlt: "complete",
	}
	m.Normalize()
	assert.Equal(t, "complete", m.DataMode)
	assert.Equal(t, "", m.DataModeAlt)
}

func TestNormalize_DataModeLowerCase(t *testing.T) {
	// Test that lowercase datamode is preserved
	m := &RSMetadata{
		DataMode: "partial",
	}
	m.Normalize()
	assert.Equal(t, "partial", m.DataMode)
}

func TestParseAvayaSIPRECMetadata(t *testing.T) {
	// Test parsing Avaya-style SIPREC metadata
	avayaXML := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
<dataMode>complete</dataMode>
<session session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962"></session>
<sessionrecordingassoc session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962">
<associate-time>2024-10-25T15:34:27</associate-time>
</sessionrecordingassoc>
<participant participant_id="0d0fb413d30ed217374a">
<nameID aor="sip:3004001011@c1cx.com"></nameID>
</participant>
<participantsessionassoc participant_id="0d0fb413d30ed217374a" session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962">
<associate-time>2024-10-25T15:34:27</associate-time>
</participantsessionassoc>
<participantstreamassoc participant_id="0d0fb413d30ed217374a">
<send>14c6c353ec5dbb080ea5</send>
</participantstreamassoc>
<participant participant_id="abcdef12345678901234">
<nameID aor="sip:ASBCE@asbce.com"></nameID>
</participant>
<participantsessionassoc participant_id="abcdef12345678901234" session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962">
<associate-time>2024-10-25T15:34:27</associate-time>
</participantsessionassoc>
<participantstreamassoc participant_id="abcdef12345678901234">
<send>199f2245677840031182</send>
</participantstreamassoc>
<stream stream_id="14c6c353ec5dbb080ea5" session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962">
<label>10</label>
</stream>
<stream stream_id="199f2245677840031182" session_id="04FA08002C0007671C00D3434F7C61633065646565372D323165642D346239642D616534352D3863353262653530383738312D317C5745427C446576696E7C434841547C62613661333930372D396364342D343831612D616338342D313930633735663664623962">
<label>20</label>
</stream>
</recording>`

	var m RSMetadata
	err := xml.Unmarshal([]byte(avayaXML), &m)
	assert.NoError(t, err)

	// Verify dataMode was normalized from camelCase
	assert.Equal(t, "complete", m.DataMode)

	// Verify session parsing
	assert.Len(t, m.Sessions, 1)
	assert.Contains(t, m.Sessions[0].ID, "04FA08002C0007671C00D3434F7C")

	// Verify participants
	assert.Len(t, m.Participants, 2)
	assert.Equal(t, "0d0fb413d30ed217374a", m.Participants[0].ID)
	assert.Equal(t, "abcdef12345678901234", m.Participants[1].ID)

	// Verify nameID with aor attribute
	assert.Len(t, m.Participants[0].NameInfos, 1)
	assert.Equal(t, "sip:3004001011@c1cx.com", m.Participants[0].NameInfos[0].AOR)

	// Verify streams with label as element (not attribute)
	assert.Len(t, m.Streams, 2)
	assert.Equal(t, "14c6c353ec5dbb080ea5", m.Streams[0].StreamID)
	assert.Equal(t, "10", m.Streams[0].Label) // Normalized from LabelElement
	assert.Equal(t, "199f2245677840031182", m.Streams[1].StreamID)
	assert.Equal(t, "20", m.Streams[1].Label)

	// Verify participant stream associations with send element
	assert.Len(t, m.ParticipantStreamAssoc, 2)
	assert.Equal(t, "0d0fb413d30ed217374a", m.ParticipantStreamAssoc[0].ParticipantID)
	assert.Contains(t, m.ParticipantStreamAssoc[0].Send, "14c6c353ec5dbb080ea5")

	// Verify stream participant resolution using stream label (from SDP a=label:10)
	// The metadata maps: participant 0d0fb -> sends stream 14c6c... -> which has label "10"
	p := m.ResolveStreamParticipant("10")
	assert.NotNil(t, p, "Should resolve participant for stream label '10'")
	assert.Equal(t, "0d0fb413d30ed217374a", p.ID)

	// Also test with stream label "20"
	p2 := m.ResolveStreamParticipant("20")
	assert.NotNil(t, p2, "Should resolve participant for stream label '20'")
	assert.Equal(t, "abcdef12345678901234", p2.ID)
}

func TestParseAvayaSIPRECMetadata_ThreeStreams(t *testing.T) {
	// Test parsing Avaya-style SIPREC metadata with 3 streams (mixed stream scenario)
	// Avaya can send 3 streams: caller audio, callee audio, and a mixed stream
	avayaXML := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
<dataMode>complete</dataMode>
<session session_id="AVAYA-3STREAM-SESSION-001"></session>
<sessionrecordingassoc session_id="AVAYA-3STREAM-SESSION-001">
<associate-time>2024-10-25T15:34:27</associate-time>
</sessionrecordingassoc>
<participant participant_id="caller-participant">
<nameID aor="sip:caller@avaya.com">
<name>Caller Name</name>
</nameID>
</participant>
<participant participant_id="callee-participant">
<nameID aor="sip:callee@avaya.com">
<name>Callee Name</name>
</nameID>
</participant>
<participantsessionassoc participant_id="caller-participant" session_id="AVAYA-3STREAM-SESSION-001">
<associate-time>2024-10-25T15:34:27</associate-time>
</participantsessionassoc>
<participantsessionassoc participant_id="callee-participant" session_id="AVAYA-3STREAM-SESSION-001">
<associate-time>2024-10-25T15:34:27</associate-time>
</participantsessionassoc>
<participantstreamassoc participant_id="caller-participant">
<send>stream-caller-audio</send>
</participantstreamassoc>
<participantstreamassoc participant_id="callee-participant">
<send>stream-callee-audio</send>
</participantstreamassoc>
<stream stream_id="stream-caller-audio" session_id="AVAYA-3STREAM-SESSION-001">
<label>10</label>
<mode>separate</mode>
</stream>
<stream stream_id="stream-callee-audio" session_id="AVAYA-3STREAM-SESSION-001">
<label>20</label>
<mode>separate</mode>
</stream>
<stream stream_id="stream-mixed-audio" session_id="AVAYA-3STREAM-SESSION-001">
<label>30</label>
<mode>mixed</mode>
</stream>
</recording>`

	var m RSMetadata
	err := xml.Unmarshal([]byte(avayaXML), &m)
	assert.NoError(t, err)

	// Verify dataMode was normalized from camelCase
	assert.Equal(t, "complete", m.DataMode)

	// Verify session parsing
	assert.Len(t, m.Sessions, 1)
	assert.Equal(t, "AVAYA-3STREAM-SESSION-001", m.Sessions[0].ID)

	// Verify participants
	assert.Len(t, m.Participants, 2)
	assert.Equal(t, "caller-participant", m.Participants[0].ID)
	assert.Equal(t, "callee-participant", m.Participants[1].ID)

	// Verify 3 streams
	assert.Len(t, m.Streams, 3, "Should have 3 streams")

	// Stream 1: Caller audio
	assert.Equal(t, "stream-caller-audio", m.Streams[0].StreamID)
	assert.Equal(t, "10", m.Streams[0].Label)
	assert.Equal(t, "separate", m.Streams[0].Mode)

	// Stream 2: Callee audio
	assert.Equal(t, "stream-callee-audio", m.Streams[1].StreamID)
	assert.Equal(t, "20", m.Streams[1].Label)
	assert.Equal(t, "separate", m.Streams[1].Mode)

	// Stream 3: Mixed audio
	assert.Equal(t, "stream-mixed-audio", m.Streams[2].StreamID)
	assert.Equal(t, "30", m.Streams[2].Label)
	assert.Equal(t, "mixed", m.Streams[2].Mode)

	// Verify stream participant resolution for all 3 streams
	p1 := m.ResolveStreamParticipant("10")
	assert.NotNil(t, p1, "Should resolve participant for stream label '10'")
	assert.Equal(t, "caller-participant", p1.ID)

	p2 := m.ResolveStreamParticipant("20")
	assert.NotNil(t, p2, "Should resolve participant for stream label '20'")
	assert.Equal(t, "callee-participant", p2.ID)

	// Mixed stream (label 30) typically doesn't have a direct participant association
	// but the lookup should not panic
	p3 := m.ResolveStreamParticipant("30")
	assert.Nil(t, p3, "Mixed stream should not have direct participant association")
}
