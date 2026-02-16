package siprec

import (
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
