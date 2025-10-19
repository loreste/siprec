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
	Category  string
	Action    string
	Outcome   string
	CallID    string
	SessionID string
	Tenant    string
	Users     []string
	Details   map[string]interface{}
	Timestamp time.Time
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
