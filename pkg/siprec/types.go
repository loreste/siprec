package siprec

import (
	"encoding/xml"
	"time"
)

// RecordingSession represents a SIPREC recording session
// Enhanced to support RFC 6341 and RFC 7866
type RecordingSession struct {
	ID                 string
	Participants       []Participant
	AssociatedTime     time.Time
	SequenceNumber     int
	RecordingType      string // full, selective, etc.
	RecordingState     string // recording, paused, etc.
	MediaStreamTypes   []string
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
	ReplacesSessionID  string            // ID of session this one replaces
	PauseResumeAllowed bool              // Whether pausing is allowed
	RealTimeMedia      bool              // Whether this is real-time (vs stored)
	FailoverID         string            // ID for failover tracking
	// Enhanced production fields
	CreatedAt          time.Time         // When this session was created
	UpdatedAt          time.Time         // Last time this session was updated
	ErrorCount         int               // Number of errors encountered during session
	IsValid            bool              // Whether this session is valid
	SourceIP           string            // IP address of the SRC (recording client)
	Callbacks          []string          // List of callback URLs for notifications
	ErrorState         bool              // Whether session is in error state
	ErrorMessage       string            // Last error message
	RetryCount         int               // Number of retry attempts
	Timeout            time.Duration     // Session timeout
	LogicalResourceID  string            // ID for load balancing/clustering
}

// Participant represents a participant in a recording session
// Enhanced for RFC 6341 and RFC 7866 compliance
type Participant struct {
	ID               string
	Name             string
	DisplayName      string
	CommunicationIDs []CommunicationID
	Role             string      // passive, active, focus, etc. (RFC 7866)
	Languages        []string    // Participant's languages (RFC 6341)
	MediaStreams     []string    // Stream IDs this participant is involved in
	JoinTime         time.Time   // When participant joined (RFC 6341)
	LeaveTime        time.Time   // When participant left (RFC 6341) 
	SessionPriority  int         // Priority value for this participant (RFC 6341)
	PartialSession   bool        // Whether participant was present for partial session only
	Anonymized       bool        // Whether participant identity is anonymized (RFC 6341)
	RecordingAware   bool        // Whether participant is aware of recording (RFC 6341/7866)
	ConsentObtained  bool        // Whether consent was obtained (RFC 6341/7866)
	Affiliations     []string    // Organizational affiliations (RFC 6341)
	ConfRole         string      // Role in conference - chair, moderator, etc. (RFC 6341)
}

// CommunicationID represents a communication identifier for a participant
// Enhanced for RFC 6341 and RFC 7866
type CommunicationID struct {
	Type        string // tel, sip, etc.
	Value       string
	Purpose     string // from, to, etc.
	Priority    int    // Priority of this communication ID
	DisplayName string // Display name for this identifier
	ValidFrom   time.Time // When this ID became valid
	ValidTo     time.Time // When this ID expires
	Anonymous   bool   // Whether this ID is anonymized
}

// RSMetadata represents the root element of the rs-metadata XML document
// Follows the schema defined in RFC 7865 and RFC 7866
type RSMetadata struct {
	XMLName               xml.Name        `xml:"urn:ietf:params:xml:ns:recording:1 recording"`
	SessionID             string          `xml:"session,attr"`
	State                 string          `xml:"state,attr"`
	Reason                string          `xml:"reason,attr,omitempty"`         // Why recording state changed (RFC 7866)
	Sequence              int             `xml:"sequence,attr,omitempty"`       // For state transitions (RFC 7866)
	ReasonRef             string          `xml:"reasonref,attr,omitempty"`      // URI reference for reason (RFC 7866)
	Expires               string          `xml:"expires,attr,omitempty"`        // When recording expires (ISO datetime)
	MediaLabel            string          `xml:"label,attr,omitempty"`          // For selective recording (RFC 7866)
	Group                 []Group         `xml:"group"`
	Participants          []RSParticipant `xml:"participant"`
	Streams               []Stream        `xml:"stream"`
	SessionRecordingAssoc RSAssociation   `xml:"sessionrecordingassoc"`
}

// Group represents a group of participants in rs-metadata
type Group struct {
	ID              string   `xml:"id,attr"`
	ParticipantRefs []string `xml:"participantsessionassoc"`
}

// RSParticipant represents a participant in rs-metadata
// Complies with RFC 7865 and RFC 7866
type RSParticipant struct {
	ID        string      `xml:"id,attr"`
	NameID    string      `xml:"nameID,attr,omitempty"`
	Name      string      `xml:"name,omitempty"`
	DisplayName string    `xml:"display-name,omitempty"` // RFC 7866 - participant's display name
	Aor       []Aor       `xml:"aor"`
	Associate string      `xml:"associate,attr,omitempty"` // RFC 7866 - indicates association with other participants
	Role      string      `xml:"role,attr,omitempty"` // RFC 7866 - role of participant (active, passive, etc.)
	Send      []string    `xml:"send,omitempty"` // RFC 7866 - stream labels participant is sending to
	Receive   []string    `xml:"receive,omitempty"` // RFC 7866 - stream labels participant is receiving
}

// Aor represents an Address of Record in rs-metadata
type Aor struct {
	Value    string `xml:",chardata"`
	URI      string `xml:"uri,attr,omitempty"`  // RFC 7866 - URI format of AOR
	Display  string `xml:"display,attr,omitempty"` // RFC 7866 - display name for AOR
	Priority int    `xml:"priority,attr,omitempty"` // RFC 7866 - priority of AOR
}

// Stream represents a media stream in rs-metadata
// Updated for RFC 7866 compliance
type Stream struct {
	Label      string `xml:"label,attr"`
	StreamID   string `xml:"streamid,attr"`
	Mode       string `xml:"mode,attr,omitempty"` // RFC 7866 - "separate" or "mixed"
	Type       string `xml:"type,attr,omitempty"` // RFC 7866 - media type (audio, video, text, etc.)
	Mixing     struct {
		MixedStreams []string `xml:"mixedstream,omitempty"` // RFC 7866 - for mixed streams
	} `xml:"mixing,omitempty"`
}

// RSAssociation represents a session recording association in rs-metadata
// Compliant with RFC 7866
type RSAssociation struct {
	SessionID   string `xml:"sessionid,attr"`
	Group       string `xml:"group,attr,omitempty"`    // RFC 7866 - group ID for association
	CallID      string `xml:"callid,attr,omitempty"`   // RFC 7866 - SIP Call-ID for the session
	FixedID     string `xml:"fixedid,attr,omitempty"`  // RFC 7866 - Fixed identifier for association
	IdentityRef string `xml:"identityref,attr,omitempty"` // RFC 7866 - reference to recording identity
}