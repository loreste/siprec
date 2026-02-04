package sip

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/sirupsen/logrus"

	sessions "siprec-server/pkg/session"
	"siprec-server/pkg/siprec"
)

// SharedSessionStore adapts the session manager store so the SIP handler
// and higher-level session manager share the same persistence layer.
type SharedSessionStore struct {
	store  sessions.SessionStore
	nodeID string
	logger *logrus.Logger
}

// NewSharedSessionStore creates a handler session store backed by the
// provided session manager store. nodeID is optional but improves filtering.
func NewSharedSessionStore(store sessions.SessionStore, nodeID string, logger *logrus.Logger) *SharedSessionStore {
	return &SharedSessionStore{store: store, nodeID: nodeID, logger: logger}
}

// Save persists a call snapshot into the shared session store.
func (s *SharedSessionStore) Save(key string, data *CallData) error {
	if s.store == nil {
		return errors.New("shared session store not initialised")
	}

	snapshot := newCallSnapshot(data)
	sessionData := snapshot.toSessionData(key, s.nodeID)
	return s.store.Store(key, sessionData)
}

// Load retrieves a call snapshot and rebuilds CallData.
func (s *SharedSessionStore) Load(key string) (*CallData, error) {
	if s.store == nil {
		return nil, errors.New("shared session store not initialised")
	}

	sessionData, err := s.store.Get(key)
	if err != nil {
		return nil, err
	}
	return callDataFromSessionData(sessionData)
}

// Delete removes a persisted call snapshot.
func (s *SharedSessionStore) Delete(key string) error {
	if s.store == nil {
		return errors.New("shared session store not initialised")
	}
	return s.store.Delete(key)
}

// List returns the identifiers for sessions stored for this node.
func (s *SharedSessionStore) List() ([]string, error) {
	if s.store == nil {
		return nil, errors.New("shared session store not initialised")
	}

	sessionsList, err := s.store.List(s.nodeID)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(sessionsList))
	for _, sd := range sessionsList {
		if sd != nil && sd.SessionID != "" {
			ids = append(ids, sd.SessionID)
		}
	}
	return ids, nil
}

// Close closes the underlying session store when supported.
func (s *SharedSessionStore) Close() error {
	if closer, ok := s.store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// callSnapshot captures the serialisable subset of CallData.
type callSnapshot struct {
	Recording     recordingSnapshot `json:"recording"`
	Dialog        dialogSnapshot    `json:"dialog"`
	LastActivity  time.Time         `json:"last_activity"`
	RemoteAddress string            `json:"remote_address,omitempty"`
}

type recordingSnapshot struct {
	ID               string               `json:"id"`
	SIPID            string               `json:"sip_id,omitempty"`
	State            string               `json:"state,omitempty"`
	StartTime        time.Time            `json:"start_time,omitempty"`
	EndTime          time.Time            `json:"end_time,omitempty"`
	Participants     []siprec.Participant `json:"participants,omitempty"`
	MediaStreamTypes []string             `json:"media_types,omitempty"`
	Direction        string               `json:"direction,omitempty"`

	// Vendor-specific metadata for failover preservation
	ExtendedMetadata map[string]string `json:"extended_metadata,omitempty"`
}

type dialogSnapshot struct {
	CallID    string   `json:"call_id,omitempty"`
	LocalTag  string   `json:"local_tag,omitempty"`
	RemoteTag string   `json:"remote_tag,omitempty"`
	LocalURI  string   `json:"local_uri,omitempty"`
	RemoteURI string   `json:"remote_uri,omitempty"`
	LocalSeq  int      `json:"local_seq,omitempty"`
	RemoteSeq int      `json:"remote_seq,omitempty"`
	Contact   string   `json:"contact,omitempty"`
	RouteSet  []string `json:"route_set,omitempty"`
}

func (d dialogSnapshot) isZero() bool {
	return d.CallID == "" && d.LocalTag == "" && d.RemoteTag == "" &&
		d.LocalURI == "" && d.RemoteURI == "" && d.LocalSeq == 0 && d.RemoteSeq == 0 &&
		d.Contact == "" && len(d.RouteSet) == 0
}

func newCallSnapshot(data *CallData) callSnapshot {
	snapshot := callSnapshot{
		LastActivity:  time.Now(),
		RemoteAddress: data.RemoteAddress,
	}

	if !data.LastActivity.IsZero() {
		snapshot.LastActivity = data.LastActivity
	}

	if data.RecordingSession != nil {
		snapshot.Recording = recordingSnapshot{
			ID:               data.RecordingSession.ID,
			SIPID:            data.RecordingSession.SIPID,
			State:            data.RecordingSession.RecordingState,
			StartTime:        data.RecordingSession.StartTime,
			EndTime:          data.RecordingSession.EndTime,
			Participants:     cloneParticipants(data.RecordingSession.Participants),
			MediaStreamTypes: append([]string(nil), data.RecordingSession.MediaStreamTypes...),
			Direction:        data.RecordingSession.Direction,
			ExtendedMetadata: cloneStringMap(data.RecordingSession.ExtendedMetadata),
		}
	}

	if data.DialogInfo != nil {
		snapshot.Dialog = dialogSnapshot{
			CallID:    data.DialogInfo.CallID,
			LocalTag:  data.DialogInfo.LocalTag,
			RemoteTag: data.DialogInfo.RemoteTag,
			LocalURI:  data.DialogInfo.LocalURI,
			RemoteURI: data.DialogInfo.RemoteURI,
			LocalSeq:  data.DialogInfo.LocalSeq,
			RemoteSeq: data.DialogInfo.RemoteSeq,
			Contact:   data.DialogInfo.Contact,
			RouteSet:  append([]string(nil), data.DialogInfo.RouteSet...),
		}
	}

	return snapshot
}

func (s callSnapshot) toSessionData(sessionID, nodeID string) *sessions.SessionData {
	startTime := s.Recording.StartTime
	if startTime.IsZero() {
		startTime = s.LastActivity
	}
	if startTime.IsZero() {
		startTime = time.Now()
	}

	status := s.Recording.State
	if status == "" {
		status = "active"
	}

	metadata := map[string]interface{}{
		"call_snapshot": s,
	}

	sd := &sessions.SessionData{
		SessionID:        sessionID,
		CallID:           s.Dialog.CallID,
		Status:           status,
		StartTime:        startTime,
		LastUpdate:       s.LastActivity,
		RecordingPath:    "",
		Metadata:         metadata,
		NodeID:           nodeID,
		ExtendedMetadata: cloneStringMap(s.Recording.ExtendedMetadata),
	}

	// Extract vendor-specific fields from ExtendedMetadata for easier access
	if s.Recording.ExtendedMetadata != nil {
		if v, ok := s.Recording.ExtendedMetadata["sip_vendor_type"]; ok {
			sd.VendorType = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_oracle_ucid"]; ok {
			sd.OracleUCID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_oracle_conversation_id"]; ok {
			sd.OracleConversationID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_cisco_session_id"]; ok {
			sd.CiscoSessionID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_ucid"]; ok {
			sd.UCID = v
		}
		// Avaya-specific fields
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_ucid"]; ok {
			sd.AvayaUCID = v
		} else if sd.VendorType == "avaya" && sd.UCID != "" {
			// Fallback: Check for Avaya UCID in the generic UCID field when vendor is Avaya
			sd.AvayaUCID = sd.UCID
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_conf_id"]; ok {
			sd.AvayaConfID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_station_id"]; ok {
			sd.AvayaStationID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_agent_id"]; ok {
			sd.AvayaAgentID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_vdn"]; ok {
			sd.AvayaVDN = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_avaya_skill_group"]; ok {
			sd.AvayaSkillGroup = v
		}
		// NICE-specific fields
		if v, ok := s.Recording.ExtendedMetadata["sip_nice_interaction_id"]; ok {
			sd.NICEInteractionID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_nice_session_id"]; ok {
			sd.NICESessionID = v
		}
		if v, ok := s.Recording.ExtendedMetadata["sip_nice_recording_id"]; ok {
			sd.NICERecordingID = v
		}
	}

	return sd
}

func callDataFromSessionData(data *sessions.SessionData) (*CallData, error) {
	snapshot := callSnapshot{
		LastActivity: data.LastUpdate,
	}

	if data.Metadata != nil {
		if raw, ok := data.Metadata["call_snapshot"]; ok {
			bytes, err := json.Marshal(raw)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(bytes, &snapshot); err != nil {
				return nil, err
			}
		}
	}

	callData := &CallData{
		RecordingSession: snapshot.toRecordingSession(),
		LastActivity:     snapshot.LastActivity,
		RemoteAddress:    snapshot.RemoteAddress,
	}

	if !snapshot.Dialog.isZero() {
		callData.DialogInfo = snapshot.toDialogInfo()
	}

	return callData, nil
}

func (s callSnapshot) toRecordingSession() *siprec.RecordingSession {
	if s.Recording.ID == "" && len(s.Recording.MediaStreamTypes) == 0 && s.Recording.State == "" {
		return nil
	}

	session := &siprec.RecordingSession{
		ID:               s.Recording.ID,
		SIPID:            s.Recording.SIPID,
		RecordingState:   s.Recording.State,
		StartTime:        s.Recording.StartTime,
		EndTime:          s.Recording.EndTime,
		Participants:     cloneParticipants(s.Recording.Participants),
		MediaStreamTypes: append([]string(nil), s.Recording.MediaStreamTypes...),
		Direction:        s.Recording.Direction,
		UpdatedAt:        s.LastActivity,
		IsValid:          true,
		ExtendedMetadata: cloneStringMap(s.Recording.ExtendedMetadata),
	}

	return session
}

func (s callSnapshot) toDialogInfo() *DialogInfo {
	return &DialogInfo{
		CallID:    s.Dialog.CallID,
		LocalTag:  s.Dialog.LocalTag,
		RemoteTag: s.Dialog.RemoteTag,
		LocalURI:  s.Dialog.LocalURI,
		RemoteURI: s.Dialog.RemoteURI,
		LocalSeq:  s.Dialog.LocalSeq,
		RemoteSeq: s.Dialog.RemoteSeq,
		Contact:   s.Dialog.Contact,
		RouteSet:  append([]string(nil), s.Dialog.RouteSet...),
	}
}

func cloneParticipants(participants []siprec.Participant) []siprec.Participant {
	if len(participants) == 0 {
		return nil
	}

	cloned := make([]siprec.Participant, len(participants))
	copy(cloned, participants)
	return cloned
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cloned := make(map[string]string, len(m))
	for k, v := range m {
		cloned[k] = v
	}
	return cloned
}
