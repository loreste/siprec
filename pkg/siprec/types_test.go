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
