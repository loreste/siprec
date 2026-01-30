package analytics

import "time"

// TranscriptEvent represents a single transcription chunk passing through the analytics pipeline.
type TranscriptEvent struct {
	CallID     string
	Speaker    string
	Text       string
	IsFinal    bool
	Confidence float64
	Timestamp  time.Time
	Metadata   map[string]interface{}
}

// SentimentResult captures the output of sentiment/emotion analysis.
type SentimentResult struct {
	Label     string
	Score     float64
	Magnitude float64
}

// KeywordResult describes detected keywords/topics in a transcript chunk.
type KeywordResult struct {
	Keywords []string
	Topics   []string
}

// ComplianceResult captures compliance rule evaluation for the chunk.
type ComplianceResult struct {
	Violations []ComplianceViolation
}

// ComplianceViolation represents a single rule violation.
type ComplianceViolation struct {
	RuleID      string
	Description string
	Severity    string
	Timestamp   time.Time
}

// AgentMetrics captures realtime agent performance statistics.
type AgentMetrics struct {
	TotalTalkTime     time.Duration
	TotalSilenceTime  time.Duration
	InterruptionCount int
	LastSpeechAt      time.Time
}

// AudioMetrics captures real-time audio quality information.
type AudioMetrics struct {
	MOS        float64
	VoiceRatio float64
	NoiseFloor float64
	PacketLoss float64
	JitterMs   float64
	Timestamp  time.Time
}

// AcousticEvent describes notable acoustic activity in the stream.
type AcousticEvent struct {
	Type       string
	Confidence float64
	Timestamp  time.Time
	Details    map[string]interface{}
}

// AnalyticsSnapshot represents the aggregated state for a call.
type AnalyticsSnapshot struct {
	CallID         string
	SentimentTrend []SentimentResult
	Keywords       []string
	Topics         []string
	Compliance     []ComplianceViolation
	Metrics        AgentMetrics
	QualityScore   float64
	UpdatedAt      time.Time
	Audio          AudioMetrics
	Events         []AcousticEvent

	// Vendor-specific metadata for Elasticsearch indexing and filtering
	VendorType           string `json:"vendor_type,omitempty"`            // oracle, cisco, avaya, generic
	OracleUCID           string `json:"oracle_ucid,omitempty"`            // Oracle SBC Universal Call ID
	OracleConversationID string `json:"oracle_conversation_id,omitempty"` // Oracle Conversation ID for call correlation
	CiscoSessionID       string `json:"cisco_session_id,omitempty"`       // Cisco Session-ID header
	AvayaUCID            string `json:"avaya_ucid,omitempty"`             // Avaya Universal Call ID
	UCID                 string `json:"ucid,omitempty"`                   // Generic Universal Call ID
}
