package database

import (
	"time"
)

// Session represents a SIPREC recording session
type Session struct {
	ID            string     `db:"id" json:"id"`
	CallID        string     `db:"call_id" json:"call_id"`
	SessionID     string     `db:"session_id" json:"session_id"`
	Status        string     `db:"status" json:"status"`       // active, completed, failed, terminated
	Transport     string     `db:"transport" json:"transport"` // udp, tcp, tls
	SourceIP      string     `db:"source_ip" json:"source_ip"`
	SourcePort    int        `db:"source_port" json:"source_port"`
	LocalIP       string     `db:"local_ip" json:"local_ip"`
	LocalPort     int        `db:"local_port" json:"local_port"`
	StartTime     time.Time  `db:"start_time" json:"start_time"`
	EndTime       *time.Time `db:"end_time" json:"end_time,omitempty"`
	Duration      *int64     `db:"duration" json:"duration,omitempty"` // seconds
	RecordingPath string     `db:"recording_path" json:"recording_path"`
	MetadataXML   *string    `db:"metadata_xml" json:"metadata_xml,omitempty"`
	SDP           *string    `db:"sdp" json:"sdp,omitempty"`
	Participants  int        `db:"participants" json:"participants"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

// Participant represents a call participant
type Participant struct {
	ID            string     `db:"id" json:"id"`
	SessionID     string     `db:"session_id" json:"session_id"`
	ParticipantID string     `db:"participant_id" json:"participant_id"` // from SIPREC metadata
	Type          string     `db:"type" json:"type"`                     // caller, callee, observer
	NameID        *string    `db:"name_id" json:"name_id,omitempty"`
	DisplayName   *string    `db:"display_name" json:"display_name,omitempty"`
	AOR           *string    `db:"aor" json:"aor,omitempty"` // Address of Record
	StreamID      *string    `db:"stream_id" json:"stream_id,omitempty"`
	JoinTime      time.Time  `db:"join_time" json:"join_time"`
	LeaveTime     *time.Time `db:"leave_time" json:"leave_time,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

// Stream represents an audio/video stream
type Stream struct {
	ID          string     `db:"id" json:"id"`
	SessionID   string     `db:"session_id" json:"session_id"`
	StreamID    string     `db:"stream_id" json:"stream_id"` // from SIPREC metadata
	Label       string     `db:"label" json:"label"`
	Mode        string     `db:"mode" json:"mode"`           // separate, mixed
	Direction   string     `db:"direction" json:"direction"` // sendonly, recvonly, sendrecv
	Codec       *string    `db:"codec" json:"codec,omitempty"`
	SampleRate  *int       `db:"sample_rate" json:"sample_rate,omitempty"`
	Channels    *int       `db:"channels" json:"channels,omitempty"`
	StartTime   time.Time  `db:"start_time" json:"start_time"`
	EndTime     *time.Time `db:"end_time" json:"end_time,omitempty"`
	PacketCount *int64     `db:"packet_count" json:"packet_count,omitempty"`
	ByteCount   *int64     `db:"byte_count" json:"byte_count,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}

// CDR represents a Call Data Record
type CDR struct {
	ID               string     `db:"id" json:"id"`
	SessionID        string     `db:"session_id" json:"session_id"`
	CallID           string     `db:"call_id" json:"call_id"`
	CallerID         *string    `db:"caller_id" json:"caller_id,omitempty"`
	CalleeID         *string    `db:"callee_id" json:"callee_id,omitempty"`
	StartTime        time.Time  `db:"start_time" json:"start_time"`
	EndTime          *time.Time `db:"end_time" json:"end_time,omitempty"`
	Duration         *int64     `db:"duration" json:"duration,omitempty"` // seconds
	RecordingPath    string     `db:"recording_path" json:"recording_path"`
	RecordingSize    *int64     `db:"recording_size" json:"recording_size,omitempty"` // bytes
	TranscriptionID  *string    `db:"transcription_id" json:"transcription_id,omitempty"`
	Quality          *float64   `db:"quality" json:"quality,omitempty"` // 0.0-1.0
	Transport        string     `db:"transport" json:"transport"`
	SourceIP         string     `db:"source_ip" json:"source_ip"`
	Codec            *string    `db:"codec" json:"codec,omitempty"`
	SampleRate       *int       `db:"sample_rate" json:"sample_rate,omitempty"`
	ParticipantCount int        `db:"participant_count" json:"participant_count"`
	StreamCount      int        `db:"stream_count" json:"stream_count"`
	Status           string     `db:"status" json:"status"` // completed, failed, partial
	ErrorMessage     *string    `db:"error_message" json:"error_message,omitempty"`
	BillingCode      *string    `db:"billing_code" json:"billing_code,omitempty"`
	CostCenter       *string    `db:"cost_center" json:"cost_center,omitempty"`
	VendorType       *string    `db:"vendor_type" json:"vendor_type,omitempty"`         // avaya, cisco, oracle, genesys, asterisk, freeswitch, opensips, generic
	UCID             *string    `db:"ucid" json:"ucid,omitempty"`                       // Universal Call ID (Avaya/Oracle)
	OracleUCID       *string    `db:"oracle_ucid" json:"oracle_ucid,omitempty"`         // Oracle SBC UCID
	ConversationID   *string    `db:"conversation_id" json:"conversation_id,omitempty"` // Oracle Conversation ID
	CiscoSessionID   *string    `db:"cisco_session_id" json:"cisco_session_id,omitempty"`
	// Genesys-specific fields
	GenesysInteractionID  *string `db:"genesys_interaction_id" json:"genesys_interaction_id,omitempty"`
	GenesysConversationID *string `db:"genesys_conversation_id" json:"genesys_conversation_id,omitempty"`
	GenesysQueueName      *string `db:"genesys_queue_name" json:"genesys_queue_name,omitempty"`
	GenesysAgentID        *string `db:"genesys_agent_id" json:"genesys_agent_id,omitempty"`
	GenesysCampaignID     *string `db:"genesys_campaign_id" json:"genesys_campaign_id,omitempty"`
	// Asterisk-specific fields
	AsteriskUniqueID    *string `db:"asterisk_unique_id" json:"asterisk_unique_id,omitempty"`
	AsteriskLinkedID    *string `db:"asterisk_linked_id" json:"asterisk_linked_id,omitempty"`
	AsteriskChannelID   *string `db:"asterisk_channel_id" json:"asterisk_channel_id,omitempty"`
	AsteriskAccountCode *string `db:"asterisk_account_code" json:"asterisk_account_code,omitempty"`
	AsteriskContext     *string `db:"asterisk_context" json:"asterisk_context,omitempty"`
	// FreeSWITCH-specific fields
	FreeSWITCHUUID        *string `db:"freeswitch_uuid" json:"freeswitch_uuid,omitempty"`
	FreeSWITCHCoreUUID    *string `db:"freeswitch_core_uuid" json:"freeswitch_core_uuid,omitempty"`
	FreeSWITCHChannelName *string `db:"freeswitch_channel_name" json:"freeswitch_channel_name,omitempty"`
	FreeSWITCHProfileName *string `db:"freeswitch_profile_name" json:"freeswitch_profile_name,omitempty"`
	FreeSWITCHAccountCode *string `db:"freeswitch_account_code" json:"freeswitch_account_code,omitempty"`
	// OpenSIPS-specific fields
	OpenSIPSDialogID      *string `db:"opensips_dialog_id" json:"opensips_dialog_id,omitempty"`
	OpenSIPSTransactionID *string `db:"opensips_transaction_id" json:"opensips_transaction_id,omitempty"`
	OpenSIPSCallID        *string `db:"opensips_call_id" json:"opensips_call_id,omitempty"`
	// NICE-specific fields
	NICEInteractionID *string `db:"nice_interaction_id" json:"nice_interaction_id,omitempty"`
	NICESessionID     *string `db:"nice_session_id" json:"nice_session_id,omitempty"`
	NICERecordingID   *string `db:"nice_recording_id" json:"nice_recording_id,omitempty"`
	NICEContactID     *string `db:"nice_contact_id" json:"nice_contact_id,omitempty"`
	NICEAgentID       *string `db:"nice_agent_id" json:"nice_agent_id,omitempty"`
	NICECallID        *string `db:"nice_call_id" json:"nice_call_id,omitempty"`
	// Avaya-specific fields
	AvayaUCID       *string `db:"avaya_ucid" json:"avaya_ucid,omitempty"`
	AvayaConfID     *string `db:"avaya_conf_id" json:"avaya_conf_id,omitempty"`
	AvayaStationID  *string `db:"avaya_station_id" json:"avaya_station_id,omitempty"`
	AvayaAgentID    *string `db:"avaya_agent_id" json:"avaya_agent_id,omitempty"`
	AvayaVDN        *string `db:"avaya_vdn" json:"avaya_vdn,omitempty"`
	AvayaSkillGroup *string `db:"avaya_skill_group" json:"avaya_skill_group,omitempty"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at" json:"updated_at"`
}

// Event represents system events for auditing
type Event struct {
	ID        string                 `db:"id" json:"id"`
	SessionID *string                `db:"session_id" json:"session_id,omitempty"`
	Type      string                 `db:"type" json:"type"`   // session_start, session_end, error, etc.
	Level     string                 `db:"level" json:"level"` // info, warning, error, critical
	Message   string                 `db:"message" json:"message"`
	Source    string                 `db:"source" json:"source"` // sip_handler, audio_processor, etc.
	SourceIP  *string                `db:"source_ip" json:"source_ip,omitempty"`
	UserAgent *string                `db:"user_agent" json:"user_agent,omitempty"`
	Metadata  map[string]interface{} `db:"metadata" json:"metadata,omitempty"`
	CreatedAt time.Time              `db:"created_at" json:"created_at"`
}

// Transcription represents transcription records
type Transcription struct {
	ID         string     `db:"id" json:"id"`
	SessionID  string     `db:"session_id" json:"session_id"`
	StreamID   *string    `db:"stream_id" json:"stream_id,omitempty"`
	Provider   string     `db:"provider" json:"provider"` // google, aws, azure, etc.
	Language   string     `db:"language" json:"language"`
	Text       string     `db:"text" json:"text"`
	Confidence *float64   `db:"confidence" json:"confidence,omitempty"`
	StartTime  time.Time  `db:"start_time" json:"start_time"`
	EndTime    *time.Time `db:"end_time" json:"end_time,omitempty"`
	WordCount  int        `db:"word_count" json:"word_count"`
	Speaker    *string    `db:"speaker" json:"speaker,omitempty"`
	IsFinal    bool       `db:"is_final" json:"is_final"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `db:"updated_at" json:"updated_at"`
}

// User represents system users for authentication
type User struct {
	ID           string     `db:"id" json:"id"`
	Username     string     `db:"username" json:"username"`
	Email        string     `db:"email" json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"`
	Role         string     `db:"role" json:"role"` // admin, operator, viewer
	IsActive     bool       `db:"is_active" json:"is_active"`
	LastLogin    *time.Time `db:"last_login" json:"last_login,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

// APIKey represents API authentication keys
type APIKey struct {
	ID          string     `db:"id" json:"id"`
	UserID      string     `db:"user_id" json:"user_id"`
	Name        string     `db:"name" json:"name"`
	KeyHash     string     `db:"key_hash" json:"-"`
	Permissions []string   `db:"permissions" json:"permissions"` // JSON array
	IsActive    bool       `db:"is_active" json:"is_active"`
	ExpiresAt   *time.Time `db:"expires_at" json:"expires_at,omitempty"`
	LastUsed    *time.Time `db:"last_used" json:"last_used,omitempty"`
	CreatedAt   time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at" json:"updated_at"`
}

// SearchIndex represents full-text search indices
type SearchIndex struct {
	ID        string    `db:"id" json:"id"`
	Type      string    `db:"type" json:"type"` // session, cdr, transcription
	EntityID  string    `db:"entity_id" json:"entity_id"`
	Content   string    `db:"content" json:"content"`
	Metadata  string    `db:"metadata" json:"metadata"` // JSON
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}
