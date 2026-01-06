package audit

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/siprec"
	"siprec-server/pkg/telemetry/tracing"
)

const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// Event captures a structured audit record.
type Event struct {
	Category   string
	Action     string
	Outcome    string
	CallID     string
	SessionID  string
	Tenant     string
	Users      []string
	Details    map[string]interface{}
	Timestamp  time.Time
	SIPHeaders *SIPHeadersAudit // SIP header information for SIP-related events
}

// SIPHeadersAudit captures SIP headers for audit trail
type SIPHeadersAudit struct {
	// Core SIP headers
	Method      string `json:"method,omitempty"`
	RequestURI  string `json:"request_uri,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	CallID      string `json:"call_id,omitempty"`
	CSeq        string `json:"cseq,omitempty"`
	Via         string `json:"via,omitempty"`
	Contact     string `json:"contact,omitempty"`
	ContentType string `json:"content_type,omitempty"`

	// Authentication/Authorization headers
	Authorization     string `json:"authorization,omitempty"`
	ProxyAuthorization string `json:"proxy_authorization,omitempty"`
	WWWAuthenticate   string `json:"www_authenticate,omitempty"`

	// Routing headers
	Route       string `json:"route,omitempty"`
	RecordRoute string `json:"record_route,omitempty"`

	// Session/Capabilities headers
	Allow       string `json:"allow,omitempty"`
	Supported   string `json:"supported,omitempty"`
	Require     string `json:"require,omitempty"`
	UserAgent   string `json:"user_agent,omitempty"`
	Server      string `json:"server,omitempty"`

	// Media headers
	ContentLength int    `json:"content_length,omitempty"`
	Accept        string `json:"accept,omitempty"`

	// Response info
	StatusCode   int    `json:"status_code,omitempty"`
	ReasonPhrase string `json:"reason_phrase,omitempty"`

	// Transport info
	Transport    string `json:"transport,omitempty"`
	RemoteAddr   string `json:"remote_addr,omitempty"`
	LocalAddr    string `json:"local_addr,omitempty"`

	// Custom/Vendor headers
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

// ChainWriter can persist tamper-evident audit records.
type ChainWriter interface {
	Append(map[string]interface{}) error
}

var chainWriter ChainWriter

// SetChainWriter registers a tamper-proof audit chain writer.
func SetChainWriter(writer ChainWriter) {
	chainWriter = writer
}

// Log emits a structured audit record enriched with tracing metadata.
func Log(ctx context.Context, logger *logrus.Logger, evt *Event) {
	if logger == nil || evt == nil {
		return
	}

	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	if evt.Details == nil {
		evt.Details = make(map[string]interface{})
	}

	// Enrich with call metadata if available.
	if md := tracing.MetadataFromContext(ctx); md != nil {
		if evt.CallID == "" {
			evt.CallID = md.CallID
		}
		if evt.Tenant == "" {
			evt.Tenant = md.TenantOrUnknown()
		}
		if evt.SessionID == "" {
			evt.SessionID = md.SessionIDOrEmpty()
		}
		if len(evt.Users) == 0 {
			evt.Users = md.UsersOrEmpty()
		}
	}

	if evt.Tenant == "" {
		evt.Tenant = "unknown"
	}

	fields := logrus.Fields{
		"audit":          true,
		"audit_category": evt.Category,
		"audit_action":   evt.Action,
		"audit_outcome":  evt.Outcome,
		"call_id":        evt.CallID,
		"tenant":         evt.Tenant,
		"timestamp":      evt.Timestamp.UTC().Format(time.RFC3339Nano),
	}

	if evt.SessionID != "" {
		fields["session_id"] = evt.SessionID
	}
	if len(evt.Users) > 0 {
		fields["users"] = evt.Users
	}

	// Include SIP headers in audit trail if present
	if evt.SIPHeaders != nil {
		sipFields := sipHeadersToFields(evt.SIPHeaders)
		for k, v := range sipFields {
			fields["sip_"+k] = v
		}
	}

	for k, v := range evt.Details {
		if _, reserved := fields[k]; reserved {
			continue
		}
		fields[k] = v
	}

	if chainWriter != nil {
		payload := make(map[string]interface{}, len(fields))
		for k, v := range fields {
			payload[k] = v
		}
		payload["details"] = evt.Details
		if err := chainWriter.Append(payload); err != nil {
			logger.WithError(err).Warn("Failed to append audit record to chain writer")
		}
	}

	if span := tracing.SpanFromContext(ctx); span != nil {
		if sc := span.SpanContext(); sc.IsValid() {
			fields["trace_id"] = sc.TraceID().String()
			fields["span_id"] = sc.SpanID().String()
		}
	}

	logger.WithFields(fields).Info("audit.event")
}

// TenantFromSession extracts a tenant identifier from a recording session if available.
func TenantFromSession(session *siprec.RecordingSession) string {
	if session == nil {
		return ""
	}

	if session.ExtendedMetadata != nil {
		for _, key := range []string{"tenant_id", "tenant", "customer_id"} {
			if value := strings.TrimSpace(session.ExtendedMetadata[key]); value != "" {
				return value
			}
		}
		if value := strings.TrimSpace(session.ExtendedMetadata["group"]); value != "" {
			return value
		}
	}

	if value := strings.TrimSpace(session.LogicalResourceID); value != "" {
		return value
	}

	if value := strings.TrimSpace(session.PolicyID); value != "" {
		return value
	}

	return ""
}

// UsersFromSession returns a deduplicated list of participant descriptors for auditing.
func UsersFromSession(session *siprec.RecordingSession) []string {
	if session == nil {
		return nil
	}
	return UsersFromParticipants(session.Participants)
}

// UsersFromParticipants converts participants into stable identifiers.
func UsersFromParticipants(participants []siprec.Participant) []string {
	if len(participants) == 0 {
		return nil
	}

	unique := make(map[string]struct{})
	for _, participant := range participants {
		candidate := strings.TrimSpace(participant.DisplayName)
		if candidate == "" {
			candidate = strings.TrimSpace(participant.Name)
		}
		if candidate == "" && len(participant.CommunicationIDs) > 0 {
			candidate = strings.TrimSpace(participant.CommunicationIDs[0].Value)
		}
		if candidate == "" {
			candidate = participant.ID
		}
		if candidate != "" {
			unique[candidate] = struct{}{}
		}
	}

	users := make([]string, 0, len(unique))
	for user := range unique {
		users = append(users, user)
	}
	sort.Strings(users)
	return users
}

// MergeDetails merges additional details into an event's detail map.
func MergeDetails(evt *Event, details map[string]interface{}) {
	if evt == nil || details == nil {
		return
	}
	if evt.Details == nil {
		evt.Details = make(map[string]interface{})
	}
	for k, v := range details {
		evt.Details[k] = v
	}
}

// SpanContextFields helper extracts trace identifiers from a context.
func SpanContextFields(ctx context.Context) (traceID, spanID string) {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if sc.IsValid() {
		traceID = sc.TraceID().String()
		spanID = sc.SpanID().String()
	}
	return
}

// sipHeadersToFields converts SIPHeadersAudit to a map for logging
func sipHeadersToFields(h *SIPHeadersAudit) map[string]interface{} {
	fields := make(map[string]interface{})

	// Core headers
	if h.Method != "" {
		fields["method"] = h.Method
	}
	if h.RequestURI != "" {
		fields["request_uri"] = h.RequestURI
	}
	if h.From != "" {
		fields["from"] = h.From
	}
	if h.To != "" {
		fields["to"] = h.To
	}
	if h.CallID != "" {
		fields["call_id"] = h.CallID
	}
	if h.CSeq != "" {
		fields["cseq"] = h.CSeq
	}
	if h.Via != "" {
		fields["via"] = h.Via
	}
	if h.Contact != "" {
		fields["contact"] = h.Contact
	}
	if h.ContentType != "" {
		fields["content_type"] = h.ContentType
	}

	// Auth headers (redact sensitive info)
	if h.Authorization != "" {
		fields["authorization"] = redactAuthHeader(h.Authorization)
	}
	if h.ProxyAuthorization != "" {
		fields["proxy_authorization"] = redactAuthHeader(h.ProxyAuthorization)
	}
	if h.WWWAuthenticate != "" {
		fields["www_authenticate"] = h.WWWAuthenticate
	}

	// Routing headers
	if h.Route != "" {
		fields["route"] = h.Route
	}
	if h.RecordRoute != "" {
		fields["record_route"] = h.RecordRoute
	}

	// Session headers
	if h.Allow != "" {
		fields["allow"] = h.Allow
	}
	if h.Supported != "" {
		fields["supported"] = h.Supported
	}
	if h.Require != "" {
		fields["require"] = h.Require
	}
	if h.UserAgent != "" {
		fields["user_agent"] = h.UserAgent
	}
	if h.Server != "" {
		fields["server"] = h.Server
	}

	// Media headers
	if h.ContentLength > 0 {
		fields["content_length"] = h.ContentLength
	}
	if h.Accept != "" {
		fields["accept"] = h.Accept
	}

	// Response info
	if h.StatusCode > 0 {
		fields["status_code"] = h.StatusCode
	}
	if h.ReasonPhrase != "" {
		fields["reason_phrase"] = h.ReasonPhrase
	}

	// Transport info
	if h.Transport != "" {
		fields["transport"] = h.Transport
	}
	if h.RemoteAddr != "" {
		fields["remote_addr"] = h.RemoteAddr
	}
	if h.LocalAddr != "" {
		fields["local_addr"] = h.LocalAddr
	}

	// Custom headers
	for k, v := range h.CustomHeaders {
		fields["custom_"+strings.ToLower(strings.ReplaceAll(k, "-", "_"))] = v
	}

	return fields
}

// redactAuthHeader redacts sensitive authentication data from headers
func redactAuthHeader(header string) string {
	// Redact actual credentials but keep auth scheme and realm visible
	if strings.HasPrefix(strings.ToLower(header), "digest") {
		// Keep scheme, realm, and nonce visible, redact response
		parts := strings.Split(header, ",")
		var redacted []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			lower := strings.ToLower(part)
			if strings.HasPrefix(lower, "response=") ||
				strings.HasPrefix(lower, "cnonce=") ||
				strings.HasPrefix(lower, "nc=") {
				// Redact sensitive fields
				key := strings.Split(part, "=")[0]
				redacted = append(redacted, key+"=\"[REDACTED]\"")
			} else {
				redacted = append(redacted, part)
			}
		}
		return strings.Join(redacted, ", ")
	}
	// For Basic auth or unknown schemes, redact entirely
	if idx := strings.Index(header, " "); idx > 0 {
		return header[:idx] + " [REDACTED]"
	}
	return "[REDACTED]"
}

// NewSIPHeadersAuditFromMap creates SIPHeadersAudit from a header map
func NewSIPHeadersAuditFromMap(headers map[string][]string, method, requestURI string) *SIPHeadersAudit {
	h := &SIPHeadersAudit{
		Method:        method,
		RequestURI:    requestURI,
		CustomHeaders: make(map[string]string),
	}

	getFirst := func(key string) string {
		if vals, ok := headers[key]; ok && len(vals) > 0 {
			return vals[0]
		}
		return ""
	}

	h.From = getFirst("From")
	h.To = getFirst("To")
	h.CallID = getFirst("Call-ID")
	h.CSeq = getFirst("CSeq")
	h.Via = getFirst("Via")
	h.Contact = getFirst("Contact")
	h.ContentType = getFirst("Content-Type")
	h.Authorization = getFirst("Authorization")
	h.ProxyAuthorization = getFirst("Proxy-Authorization")
	h.WWWAuthenticate = getFirst("WWW-Authenticate")
	h.Route = getFirst("Route")
	h.RecordRoute = getFirst("Record-Route")
	h.Allow = getFirst("Allow")
	h.Supported = getFirst("Supported")
	h.Require = getFirst("Require")
	h.UserAgent = getFirst("User-Agent")
	h.Server = getFirst("Server")
	h.Accept = getFirst("Accept")

	// Capture vendor-specific headers
	vendorHeaders := []string{
		"X-Session-ID", "Session-ID", "P-Asserted-Identity",
		"P-Preferred-Identity", "Remote-Party-ID", "Diversion",
		"X-UCID", "X-Call-Info", "P-Charging-Vector",
	}
	for _, hdr := range vendorHeaders {
		if val := getFirst(hdr); val != "" {
			h.CustomHeaders[hdr] = val
		}
	}

	return h
}
