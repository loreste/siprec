package siprec

import (
	"encoding/xml"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
)

// RecordingSession represents a SIPREC recording session
// Enhanced to support RFC 6341 and RFC 7866
type RecordingSession struct {
	ID                string
	SIPID             string // SIP Call-ID associated with this recording session
	Participants      []Participant
	AssociatedTime    time.Time
	SequenceNumber    int
	RecordingType     string // full, selective, etc.
	RecordingState    string // recording, paused, etc.
	Direction         string // Direction of the recording (inbound, outbound, unknown)
	StateReason       string
	StateReasonRef    string
	StateExpires      time.Time
	MediaStreamTypes  []string
	SessionGroups     []SessionGroupAssociation
	PolicyUpdates     []PolicyUpdate
	SessionGroupRoles map[string]string
	PolicyStates      map[string]PolicyAckStatus
	// RFC 6341 fields
	PolicyID           string            // Recording policy identifier
	RetentionPeriod    time.Duration     // How long recording should be kept
	RecordingAgent     string            // Identity of recording entity
	RecordingAgentCert []byte            // Certificate of recording entity
	SecurityMechanism  string            // Mechanism used to secure recording
	Reason             string            // Reason for recording
	Priority           int               // Recording priority
	StartTime          time.Time         // When recording started
	EndTime            time.Time         // When recording ended
	ExtendedMetadata   map[string]string // Additional metadata
	// RFC 7245/7866 fields
	ReplacesSessionID  string // ID of session this one replaces
	PauseResumeAllowed bool   // Whether pausing is allowed
	RealTimeMedia      bool   // Whether this is real-time (vs stored)
	FailoverID         string // ID for failover tracking
	// Enhanced production fields
	CreatedAt         time.Time     // When this session was created
	UpdatedAt         time.Time     // Last time this session was updated
	ErrorCount        int           // Number of errors encountered during session
	IsValid           bool          // Whether this session is valid
	SourceIP          string        // IP address of the SRC (recording client)
	Callbacks         []string      // List of callback URLs for notifications
	ErrorState        bool          // Whether session is in error state
	ErrorMessage      string        // Last error message
	RetryCount        int           // Number of retry attempts
	Timeout           time.Duration // Session timeout
	LogicalResourceID string        // ID for load balancing/clustering
	OriginalRequest   *sip.Request  // The original SIP request that started this session

	// Per-call configuration (can override global settings)
	CustomRTPTimeout   time.Duration // Per-call RTP timeout (0 = use global)
	CustomMaxDuration  time.Duration // Per-call max duration (0 = use global)
	CustomRetention    time.Duration // Per-call retention period (0 = use global)
	TimeoutSource      string        // Where timeout was configured: "global", "metadata", "header"
}

// Participant represents a participant in a recording session
// Enhanced for RFC 6341 and RFC 7866 compliance
type Participant struct {
	ID               string
	Name             string
	DisplayName      string
	CommunicationIDs []CommunicationID
	Role             string    // passive, active, focus, etc. (RFC 7866)
	Languages        []string  // Participant's languages (RFC 6341)
	MediaStreams     []string  // Stream IDs this participant is involved in
	JoinTime         time.Time // When participant joined (RFC 6341)
	LeaveTime        time.Time // When participant left (RFC 6341)
	SessionPriority  int       // Priority value for this participant (RFC 6341)
	PartialSession   bool      // Whether participant was present for partial session only
	Anonymized       bool      // Whether participant identity is anonymized (RFC 6341)
	RecordingAware   bool      // Whether participant is aware of recording (RFC 6341/7866)
	ConsentObtained  bool      // Whether consent was obtained (RFC 6341/7866)
	Affiliations     []string  // Organizational affiliations (RFC 6341)
	ConfRole         string    // Role in conference - chair, moderator, etc. (RFC 6341)
	// Enhanced identification fields
	GeographicLocation string  // Geographic location of participant
	UserAgent          string  // User agent string for device identification
	CallCharges        float64 // Call charges associated with participant
	CallingParty       bool    // Whether this participant initiated the call
	CalledParty        bool    // Whether this participant received the call
	// Quality metrics
	AudioQuality float64       // MOS score for audio quality (1.0-5.0)
	NetworkDelay time.Duration // Network delay for this participant
	PacketLoss   float64       // Packet loss percentage (0.0-100.0)
	Jitter       time.Duration // Jitter measurements
}

// CommunicationID represents a communication identifier for a participant
// Enhanced for RFC 6341 and RFC 7866
type CommunicationID struct {
	Type        string // tel, sip, etc.
	Value       string
	Purpose     string    // from, to, etc.
	Priority    int       // Priority of this communication ID
	DisplayName string    // Display name for this identifier
	ValidFrom   time.Time // When this ID became valid
	ValidTo     time.Time // When this ID expires
	Anonymous   bool      // Whether this ID is anonymized
}

// RSMetadata represents the root element of the rs-metadata XML document
// Follows the schema defined in RFC 7865 and RFC 7866
type RSMetadata struct {
	XMLName                  xml.Name                    `xml:"urn:ietf:params:xml:ns:recording:1 recording"`
	DataMode                 string                      `xml:"datamode,omitempty"`
	DataModeAlt              string                      `xml:"dataMode,omitempty"` // Avaya uses camelCase
	SessionID                string                      `xml:"session,attr,omitempty"`
	State                    string                      `xml:"state,attr,omitempty"`
	Reason                   string                      `xml:"reason,attr,omitempty"`
	Sequence                 int                         `xml:"sequence,attr,omitempty"`
	ReasonRef                string                      `xml:"reasonref,attr,omitempty"`
	Expires                  string                      `xml:"expires,attr,omitempty"`
	MediaLabel               string                      `xml:"label,attr,omitempty"`
	Direction                string                      `xml:"direction,attr,omitempty"`
	Group                    []Group                     `xml:"group"`
	Sessions                 []RSSession                 `xml:"session"`
	RecordingSessions        []RSRecordingSession        `xml:"recordingsession"`
	Participants             []RSParticipant             `xml:"participant"`
	Streams                  []Stream                    `xml:"stream"`
	ParticipantStreamAssoc   []RSParticipantStreamAssoc  `xml:"participantstreamassoc"`
	ParticipantSessionAssoc  []RSParticipantSessionAssoc `xml:"participantsessionassoc"`
	SessionRecordingAssoc    RSAssociation               `xml:"sessionrecordingassoc"`
	SessionGroupAssociations []SessionGroupAssociation   `xml:"sessiongroupassoc"`
	PolicyUpdates            []PolicyUpdate              `xml:"policy"`
}

// SessionGroupAssociation captures membership of a recording session in a session group.
type SessionGroupAssociation struct {
	SessionGroupID string `xml:"sessiongroupid,attr"`
	SessionID      string `xml:"sessionid,attr"`
	Role           string `xml:"role,attr,omitempty"`
}

// PolicyUpdate captures policy acknowledgement state exchanged via metadata.
type PolicyUpdate struct {
	PolicyID     string `xml:"policyid,attr"`
	Status       string `xml:"status,attr"`
	Acknowledged bool   `xml:"acknowledged,attr"`
	Timestamp    string `xml:"timestamp,attr,omitempty"`
}

// PolicyAckStatus captures acknowledgement state for a policy update.
type PolicyAckStatus struct {
	Status       string
	Acknowledged bool
	ReportedAt   time.Time
	RawTimestamp string
}

// Group represents a group of participants in rs-metadata
type Group struct {
	ID                   string         `xml:"group_id,attr,omitempty"`
	LegacyID             string         `xml:"id,attr,omitempty"`
	AssociateTime        string         `xml:"associate-time,omitempty"`
	SessionRefs          []string       `xml:"session-ref"`
	ParticipantRefs      []string       `xml:"participant-ref"`
	LegacyParticipantRef []string       `xml:"participantsessionassoc"`
	Extensions           []XMLExtension `xml:",any"`
}

// RSParticipant represents a participant in rs-metadata
// Complies with RFC 7865 and RFC 7866
type RSParticipant struct {
	ID          string         `xml:"participant_id,attr,omitempty"`
	LegacyID    string         `xml:"id,attr,omitempty"`
	NameID      string         `xml:"nameID,attr,omitempty"`
	Name        string         `xml:"name,omitempty"`
	DisplayName string         `xml:"display-name,omitempty"`
	Aor         []Aor          `xml:"aor"`
	NameInfos   []RSNameID     `xml:"nameID"`
	Associate   string         `xml:"associate,attr,omitempty"`
	Role        string         `xml:"role,attr,omitempty"`
	Send        []string       `xml:"send,omitempty"`
	Receive     []string       `xml:"receive,omitempty"`
	Extensions  []XMLExtension `xml:",any"`
}

// Aor represents an Address of Record in rs-metadata
type Aor struct {
	Value    string `xml:",chardata"`
	URI      string `xml:"uri,attr,omitempty"`
	Display  string `xml:"display,attr,omitempty"`
	Priority int    `xml:"priority,attr,omitempty"`
}

// Stream represents a media stream in rs-metadata
// Updated for RFC 7866 compliance
type Stream struct {
	Label          string   `xml:"label,attr,omitempty"`
	LabelElement   string   `xml:"label,omitempty"`
	StreamID       string   `xml:"streamid,attr,omitempty"`
	StreamIDAlt    string   `xml:"stream_id,attr,omitempty"`
	ID             string   `xml:"id,attr,omitempty"`
	Session        string   `xml:"session,attr,omitempty"`
	SessionAlt     string   `xml:"session_id,attr,omitempty"` // Avaya uses session_id
	Mode           string   `xml:"mode,attr,omitempty"`
	ModeElement    string   `xml:"mode,omitempty"` // Avaya uses <mode> element
	Type           string   `xml:"type,attr,omitempty"`
	AssociateTime  string   `xml:"associate-time,omitempty"`
	ParticipantRef []string `xml:"participant-ref"`
	Mixing         struct {
		MixedStreams []string `xml:"mixedstream,omitempty"`
	} `xml:"mixing,omitempty"`
	Extensions []XMLExtension `xml:",any"`
}

// RSAssociation represents a session recording association in rs-metadata
// Compliant with RFC 7866
type RSAssociation struct {
	SessionID    string `xml:"sessionid,attr,omitempty"`
	SessionIDAlt string `xml:"session_id,attr,omitempty"`
	Group        string `xml:"group,attr,omitempty"`
	GroupAlt     string `xml:"group_id,attr,omitempty"`
	CallID       string `xml:"callid,attr,omitempty"`
	CallIDAlt    string `xml:"call_id,attr,omitempty"`
	FixedID      string `xml:"fixedid,attr,omitempty"`
	IdentityRef  string `xml:"identityref,attr,omitempty"`
}

// RSSession models a session element within rs-metadata per RFC 7865.
type RSSession struct {
	ID                 string         `xml:"session_id,attr,omitempty"`
	LegacyID           string         `xml:"id,attr,omitempty"`
	Type               string         `xml:"type,attr,omitempty"`
	AssociateTime      string         `xml:"associate-time,omitempty"`
	SIPSessionID       string         `xml:"sipSessionID,omitempty"`
	SessionName        string         `xml:"session-name,omitempty"`
	SessionDescription string         `xml:"session-description,omitempty"`
	ParticipantRefs    []string       `xml:"participant-ref"`
	GroupRefs          []string       `xml:"group-ref"`
	StreamRefs         []string       `xml:"stream-ref"`
	Extensions         []XMLExtension `xml:",any"`
}

// RSRecordingSession captures the recordingsession element defined in RFC 7865.
type RSRecordingSession struct {
	ID            string         `xml:"id,attr,omitempty"`
	SessionID     string         `xml:"session_id,attr,omitempty"`
	State         string         `xml:"state,omitempty"`
	Reason        string         `xml:"reason,omitempty"`
	AssociateTime string         `xml:"associate-time,omitempty"`
	Extensions    []XMLExtension `xml:",any"`
}

// RSParticipantStreamAssoc captures participantstreamassoc relationships.
type RSParticipantStreamAssoc struct {
	ID            string         `xml:"id,attr,omitempty"`
	Participant   string         `xml:"participant,omitempty"`
	ParticipantID string         `xml:"participant_id,attr,omitempty"`
	Stream        string         `xml:"stream,omitempty"`
	StreamID      string         `xml:"stream_id,attr,omitempty"`
	Send          []string       `xml:"send"`
	Receive       []string       `xml:"recv"`
	Direction     string         `xml:"direction,omitempty"`
	Extensions    []XMLExtension `xml:",any"`
}

// RSNameID models the nameID element (with optional localized names).
type RSNameID struct {
	ID         string          `xml:"id,attr,omitempty"`
	AOR        string          `xml:"aor,attr,omitempty"`
	URI        string          `xml:"uri,attr,omitempty"`
	Display    string          `xml:"display,attr,omitempty"`
	Context    string          `xml:"context,attr,omitempty"`
	Names      []LocalizedName `xml:"name"`
	Extensions []XMLExtension  `xml:",any"`
}

// LocalizedName stores a name value with optional xml:lang attribute.
type LocalizedName struct {
	Lang  string `xml:"http://www.w3.org/XML/1998/namespace lang,attr,omitempty"`
	Value string `xml:",chardata"`
}

// XMLExtension preserves arbitrary extension elements.
type XMLExtension struct {
	XMLName  xml.Name
	InnerXML string `xml:",innerxml"`
}

// OracleExtensionData holds Oracle SBC specific extension data extracted from SIPREC metadata.
// These are vendor-specific fields in the http://acmepacket.com/siprec/extensiondata namespace.
type OracleExtensionData struct {
	UCID         string // Universal Call ID (hex encoded)
	CallerOrig   bool   // Whether this is a caller-originated call
	CallingParty bool   // Whether this participant is the calling party
}

// ExtractOracleExtensions extracts Oracle SBC extension data from XMLExtension elements.
// Oracle uses the namespace http://acmepacket.com/siprec/extensiondata with elements like:
// - <apkt:ucid>00FA080018803B69810C6D;encoding=hex</apkt:ucid>
// - <apkt:callerOrig>true</apkt:callerOrig>
// - <apkt:callingParty>true</apkt:callingParty>
func ExtractOracleExtensions(extensions []XMLExtension) *OracleExtensionData {
	if len(extensions) == 0 {
		return nil
	}

	data := &OracleExtensionData{}
	found := false

	for _, ext := range extensions {
		// Check for Oracle/ACME namespace
		if ext.XMLName.Space == "http://acmepacket.com/siprec/extensiondata" ||
			strings.HasPrefix(ext.XMLName.Local, "apkt:") ||
			strings.Contains(ext.InnerXML, "acmepacket.com/siprec/extensiondata") {

			inner := ext.InnerXML

			// Extract UCID
			if ucid := extractXMLElement(inner, "ucid"); ucid != "" {
				data.UCID = ucid
				found = true
			}

			// Extract callerOrig
			if callerOrig := extractXMLElement(inner, "callerOrig"); callerOrig != "" {
				data.CallerOrig = strings.EqualFold(callerOrig, "true")
				found = true
			}

			// Extract callingParty
			if callingParty := extractXMLElement(inner, "callingParty"); callingParty != "" {
				data.CallingParty = strings.EqualFold(callingParty, "true")
				found = true
			}
		}
	}

	if !found {
		return nil
	}
	return data
}

// extractXMLElement extracts the content of an XML element from a string.
// Handles both prefixed (apkt:element) and non-prefixed (element) forms.
func extractXMLElement(xml string, elementName string) string {
	// Try with apkt: prefix first
	patterns := []string{
		"<apkt:" + elementName + ">",
		"<" + elementName + ">",
	}

	for _, startTag := range patterns {
		if idx := strings.Index(xml, startTag); idx >= 0 {
			contentStart := idx + len(startTag)
			// Find the closing tag
			endTag1 := "</apkt:" + elementName + ">"
			endTag2 := "</" + elementName + ">"

			endIdx := strings.Index(xml[contentStart:], endTag1)
			if endIdx < 0 {
				endIdx = strings.Index(xml[contentStart:], endTag2)
			}

			if endIdx >= 0 {
				content := xml[contentStart : contentStart+endIdx]
				// Clean up the content (remove encoding info like ;encoding=hex)
				if semicolonIdx := strings.Index(content, ";"); semicolonIdx > 0 {
					content = content[:semicolonIdx]
				}
				return strings.TrimSpace(content)
			}
		}
	}
	return ""
}

// RSParticipantSessionAssoc captures participantsessionassoc relationships per RFC 7865.
// Associates a participant with a communication session.
type RSParticipantSessionAssoc struct {
	ParticipantID string         `xml:"participant_id,attr,omitempty"`
	SessionID     string         `xml:"session_id,attr,omitempty"`
	AssociateTime string         `xml:"associate-time,omitempty"`
	Extensions    []XMLExtension `xml:",any"`
}

// Normalize aligns parsed metadata with backward-compatible expectations.
func (m *RSMetadata) Normalize() {
	if m == nil {
		return
	}

	// Handle dataMode case variations (Avaya uses camelCase)
	if m.DataMode == "" && m.DataModeAlt != "" {
		m.DataMode = m.DataModeAlt
	}
	m.DataModeAlt = ""

	for i := range m.Group {
		if m.Group[i].ID == "" {
			m.Group[i].ID = m.Group[i].LegacyID
		}
		m.Group[i].LegacyID = ""
		if len(m.Group[i].LegacyParticipantRef) > 0 {
			m.Group[i].ParticipantRefs = append(m.Group[i].ParticipantRefs, m.Group[i].LegacyParticipantRef...)
			m.Group[i].LegacyParticipantRef = nil
		}
	}

	for i := range m.Sessions {
		if m.Sessions[i].ID == "" {
			m.Sessions[i].ID = m.Sessions[i].LegacyID
		}
		m.Sessions[i].LegacyID = ""
	}

	for i := range m.RecordingSessions {
		if m.RecordingSessions[i].SessionID == "" {
			m.RecordingSessions[i].SessionID = m.RecordingSessions[i].ID
		}
		if m.RecordingSessions[i].State != "" && m.State == "" {
			m.State = m.RecordingSessions[i].State
		}
		if m.RecordingSessions[i].Reason != "" && m.Reason == "" {
			m.Reason = m.RecordingSessions[i].Reason
		}
	}

	for i := range m.Participants {
		p := &m.Participants[i]
		if p.ID == "" {
			p.ID = p.LegacyID
		}
		p.LegacyID = ""

		if len(p.Aor) == 0 && len(p.NameInfos) > 0 {
			for _, ni := range p.NameInfos {
				if ni.AOR != "" || ni.URI != "" {
					value := ni.AOR
					if value == "" {
						value = ni.URI
					}
					p.Aor = append(p.Aor, Aor{
						Value:   value,
						URI:     ni.URI,
						Display: ni.Display,
					})
				}
			}
		}

		if p.Name == "" && len(p.NameInfos) > 0 {
			for _, ni := range p.NameInfos {
				for _, name := range ni.Names {
					if strings.TrimSpace(name.Value) != "" {
						p.Name = strings.TrimSpace(name.Value)
						break
					}
				}
				if p.Name != "" {
					break
				}
			}
		}

		if p.DisplayName == "" {
			if p.Name != "" {
				p.DisplayName = p.Name
			} else if len(p.NameInfos) > 0 && strings.TrimSpace(p.NameInfos[0].Display) != "" {
				p.DisplayName = strings.TrimSpace(p.NameInfos[0].Display)
			}
		}
	}

	for i := range m.Streams {
		if m.Streams[i].StreamID == "" {
			switch {
			case m.Streams[i].StreamIDAlt != "":
				m.Streams[i].StreamID = m.Streams[i].StreamIDAlt
			case m.Streams[i].ID != "":
				m.Streams[i].StreamID = m.Streams[i].ID
			}
		}
		if m.Streams[i].Label == "" {
			m.Streams[i].Label = m.Streams[i].LabelElement
		}
		// Handle mode as element (Avaya uses <mode>separate</mode>)
		if m.Streams[i].Mode == "" && m.Streams[i].ModeElement != "" {
			m.Streams[i].Mode = m.Streams[i].ModeElement
		}
		// Handle session_id attribute variant (Avaya)
		if m.Streams[i].Session == "" && m.Streams[i].SessionAlt != "" {
			m.Streams[i].Session = m.Streams[i].SessionAlt
		}
		// Clean up alternative fields
		m.Streams[i].StreamIDAlt = ""
		m.Streams[i].ID = ""
		m.Streams[i].LabelElement = ""
		m.Streams[i].ModeElement = ""
		m.Streams[i].SessionAlt = ""
	}

	if m.SessionRecordingAssoc.SessionID == "" {
		m.SessionRecordingAssoc.SessionID = m.SessionRecordingAssoc.SessionIDAlt
	}
	if m.SessionRecordingAssoc.Group == "" {
		m.SessionRecordingAssoc.Group = m.SessionRecordingAssoc.GroupAlt
	}
	if m.SessionRecordingAssoc.CallID == "" {
		m.SessionRecordingAssoc.CallID = m.SessionRecordingAssoc.CallIDAlt
	}
	m.SessionRecordingAssoc.SessionIDAlt = ""
	m.SessionRecordingAssoc.GroupAlt = ""
	m.SessionRecordingAssoc.CallIDAlt = ""

	if m.SessionID == "" {
		m.SessionID = m.SessionRecordingAssoc.SessionID
	}
	if m.SessionID == "" && len(m.Sessions) > 0 {
		m.SessionID = m.Sessions[0].ID
	}
	if m.SessionID == "" && len(m.RecordingSessions) > 0 {
		if m.RecordingSessions[0].SessionID != "" {
			m.SessionID = m.RecordingSessions[0].SessionID
		} else if m.RecordingSessions[0].ID != "" {
			m.SessionID = m.RecordingSessions[0].ID
		}
	}
}

// ResolveStreamParticipant finds the participant associated with a stream label or stream ID.
// Resolution order:
// 1. ParticipantStreamAssoc — explicit associations with send/receive (checks Stream, StreamID, and Send elements)
// 2. Stream.ParticipantRef — direct participant references on stream elements
// 3. RSParticipant.Send — participant's send stream list
// 4. ParticipantStreamAssoc.Send matching stream ID that maps to the given label
func (m *RSMetadata) ResolveStreamParticipant(streamLabel string) *RSParticipant {
	if m == nil || streamLabel == "" {
		return nil
	}

	// Build a map of stream ID -> stream label for Avaya-style lookups
	streamIDToLabel := make(map[string]string)
	streamLabelToID := make(map[string]string)
	for _, stream := range m.Streams {
		if stream.StreamID != "" && stream.Label != "" {
			streamIDToLabel[stream.StreamID] = stream.Label
			streamLabelToID[stream.Label] = stream.StreamID
		}
	}

	// 1. Check ParticipantStreamAssoc for explicit mapping
	for _, assoc := range m.ParticipantStreamAssoc {
		streamRef := assoc.Stream
		if streamRef == "" {
			streamRef = assoc.StreamID
		}

		// Direct match on Stream/StreamID attribute
		matched := streamRef == streamLabel

		// Also check Send[] elements (Avaya uses <send>streamID</send>)
		if !matched {
			for _, sendRef := range assoc.Send {
				// Match if Send contains the label directly
				if sendRef == streamLabel {
					matched = true
					break
				}
				// Match if Send contains a stream ID whose label matches
				if label, ok := streamIDToLabel[sendRef]; ok && label == streamLabel {
					matched = true
					break
				}
				// Match if streamLabel is actually a stream ID
				if sendRef == streamLabel {
					matched = true
					break
				}
			}
		}

		if matched {
			participantRef := assoc.Participant
			if participantRef == "" {
				participantRef = assoc.ParticipantID
			}
			if participantRef != "" {
				for i := range m.Participants {
					if m.Participants[i].ID == participantRef {
						return &m.Participants[i]
					}
				}
			}
		}
	}

	// 2. Check Stream.ParticipantRef for direct references
	for _, stream := range m.Streams {
		label := stream.Label
		if label == "" {
			label = stream.StreamID
		}
		if label == streamLabel && len(stream.ParticipantRef) > 0 {
			for i := range m.Participants {
				if m.Participants[i].ID == stream.ParticipantRef[0] {
					return &m.Participants[i]
				}
			}
		}
	}

	// 3. Check RSParticipant.Send for stream references
	for i := range m.Participants {
		for _, sendRef := range m.Participants[i].Send {
			if sendRef == streamLabel {
				return &m.Participants[i]
			}
		}
	}

	return nil
}

// UnmarshalXML customizes decoding to ensure normalization post-unmarshal.
func (m *RSMetadata) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type alias RSMetadata
	var aux alias
	if err := d.DecodeElement(&aux, &start); err != nil {
		return err
	}
	*m = RSMetadata(aux)
	m.Normalize()
	return nil
}

// GetOracleSessionExtensions extracts Oracle-specific session-level extension data.
// Returns UCID and callerOrig from session extensiondata if present.
func (m *RSMetadata) GetOracleSessionExtensions() *OracleExtensionData {
	if m == nil {
		return nil
	}

	// Check sessions for Oracle extensions
	for _, sess := range m.Sessions {
		if data := ExtractOracleExtensions(sess.Extensions); data != nil {
			return data
		}
	}

	// Check recording sessions
	for _, rs := range m.RecordingSessions {
		if data := ExtractOracleExtensions(rs.Extensions); data != nil {
			return data
		}
	}

	return nil
}

// GetOracleParticipantExtensions extracts Oracle-specific participant extension data.
// Returns a map of participant ID to their OracleExtensionData (callingParty info).
func (m *RSMetadata) GetOracleParticipantExtensions() map[string]*OracleExtensionData {
	if m == nil {
		return nil
	}

	result := make(map[string]*OracleExtensionData)
	for _, p := range m.Participants {
		if data := ExtractOracleExtensions(p.Extensions); data != nil {
			id := p.ID
			if id == "" {
				id = p.LegacyID
			}
			if id != "" {
				result[id] = data
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// IdentifyCallingParticipant returns the participant ID of the calling party.
// Uses Oracle's callingParty extension if available.
func (m *RSMetadata) IdentifyCallingParticipant() string {
	if m == nil {
		return ""
	}

	participantExts := m.GetOracleParticipantExtensions()
	if participantExts == nil {
		return ""
	}

	for id, ext := range participantExts {
		if ext.CallingParty {
			return id
		}
	}
	return ""
}
