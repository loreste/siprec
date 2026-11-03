package siprec

import (
	"net"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	sip "github.com/emiago/sipgo/sip"
)

// CallingParticipantHeader is the SIP header an SRC (or an SBC manipulation
// rule in front of it) can set to supply a fallback participant when the
// rs-metadata carries none.
const CallingParticipantHeader = "X-Calling-Participant"

// CallingParticipantID is the participant_id given to the participant built
// from CallingParticipantHeader.
const CallingParticipantID = "calling-participant"

// Values recorded in RSMetadata.FilledFields.
const (
	FilledSessionID   = "session_id"
	FilledParticipant = "participant"
)

// FillContext carries the SIP signalling values used to complete rs-metadata
// that a non-compliant SRC left incomplete.
type FillContext struct {
	// CallID is the SIP Call-ID of the dialog. It stays the same across digest
	// challenges and re-INVITEs, so it is a stable recording session ID.
	CallID string

	// SessionID preserves the established recording identity for in-dialog updates.
	SessionID string

	// CallingParticipant is the raw X-Calling-Participant header value
	// (name-addr or bare URI).
	CallingParticipant string
}

// FillMissing completes metadata that lacks a recording session ID or
// participants, using values from the SIP signalling. Values already present
// in the metadata are never overwritten. What was filled is recorded in
// m.FilledFields.
func FillMissing(m *RSMetadata, ctx FillContext) {
	if m == nil {
		return
	}

	sessionID := strings.TrimSpace(ctx.SessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(ctx.CallID)
	}
	// Multiple sessions are ambiguous; leave validation to reject missing IDs.
	if callID := sessionID; strings.TrimSpace(m.SessionID) == "" && callID != "" && len(m.Sessions) <= 1 {
		m.SessionID = callID

		// A lone <session> without an ID can only be this session; with
		// several we cannot tell which one is meant, so leave them alone.
		if len(m.Sessions) == 0 {
			m.Sessions = append(m.Sessions, RSSession{ID: callID})
		} else if len(m.Sessions) == 1 && strings.TrimSpace(m.Sessions[0].ID) == "" {
			m.Sessions[0].ID = callID
		}

		if strings.TrimSpace(m.SessionRecordingAssoc.SessionID) == "" {
			m.SessionRecordingAssoc.SessionID = callID
			if strings.TrimSpace(m.SessionRecordingAssoc.CallID) == "" {
				m.SessionRecordingAssoc.CallID = strings.TrimSpace(ctx.CallID)
			}
		}

		for i := range m.ParticipantSessionAssoc {
			if strings.TrimSpace(m.ParticipantSessionAssoc[i].SessionID) == "" {
				m.ParticipantSessionAssoc[i].SessionID = callID
			}
		}

		m.FilledFields = append(m.FilledFields, FilledSessionID)
	}

	if len(m.Participants) == 0 {
		if display, uri := parseNameAddr(ctx.CallingParticipant); uri != "" {
			m.Participants = append(m.Participants, RSParticipant{
				ID:          CallingParticipantID,
				Name:        display,
				DisplayName: display,
				Aor:         []Aor{{Value: uri}},
			})
			m.FilledFields = append(m.FilledFields, FilledParticipant)
		}
	}
}

// parseNameAddr accepts one SIP/SIPS address, not an arbitrary identity string.
// This feature was adapted from hopefulGiupplo/siprec commit da9748aae33d9f936b9a840cd107be2e2149b0b1.
func parseNameAddr(value string) (display, address string) {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", ""
	}
	value = strings.TrimSpace(value)
	// Require a single address: sipgo permits arbitrary text after a closing bracket.
	if end := strings.LastIndex(value, ">"); end >= 0 {
		tail := strings.TrimSpace(value[end+1:])
		if tail != "" && !strings.HasPrefix(tail, ";") {
			return "", ""
		}
		if strings.ContainsAny(tail, ",<>") {
			return "", ""
		}
	}
	if strings.Count(value, "<") > 1 || strings.Count(value, ">") > 1 {
		return "", ""
	}
	if value == "" {
		return "", ""
	}
	var uri sip.Uri
	params := sip.NewParams()
	display, err := sip.ParseAddressValue(value, &uri, &params)
	if err != nil || (uri.Scheme != "sip" && uri.Scheme != "sips" && uri.Scheme != "tel") || uri.Wildcard || uri.Host == "" || uri.Password != "" || uri.HierarhicalSlashes {
		return "", ""
	}
	// Accept global telephone numbers; local numbers need phone-context semantics
	// and must be supplied explicitly in the SRC metadata instead.
	if uri.Scheme == "tel" {
		// Bare address parameters are parsed as header parameters by sipgo.
		// Reject them rather than silently discarding telephone extensions/context.
		if !strings.Contains(value, "<") && len(params) > 0 {
			return "", ""
		}
		if !strings.HasPrefix(uri.Host, "+") || uri.User != "" || uri.Port != 0 || len(uri.UriParams) > 0 || len(uri.Headers) > 0 {
			return "", ""
		}
		digits := 0
		for _, ch := range uri.Host[1:] {
			if ch >= '0' && ch <= '9' {
				digits++
				continue
			}
			if !strings.ContainsRune("-.()", ch) {
				return "", ""
			}
		}
		if digits == 0 {
			return "", ""
		}
		return display, uri.String()
	}
	// The parser intentionally tolerates some non-RFC host syntax; constrain it here.
	host := strings.Trim(uri.Host, "[]")
	if strings.ContainsAny(uri.Host, "[]") && (uri.Host != "["+host+"]" || net.ParseIP(host) == nil) {
		return "", ""
	}
	if net.ParseIP(host) == nil {
		// A trailing dot denotes a fully qualified DNS name.
		host = strings.TrimSuffix(host, ".")
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return "", ""
			}
			for _, ch := range label {
				if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-') {
					return "", ""
				}
			}
		}
	}
	decodedUser, decodeErr := url.PathUnescape(uri.User)
	if decodeErr != nil || strings.IndexFunc(decodedUser, unicode.IsControl) >= 0 {
		return "", ""
	}
	if strings.ContainsAny(uri.User, " <>\"\t\r\n") || uri.Port < 0 || uri.Port > 65535 {
		return "", ""
	}
	return display, uri.String()
}

// ParticipantSynthesized reports whether this metadata inferred the participant.
func (m *RSMetadata) ParticipantSynthesized(id string) bool {
	if id != CallingParticipantID {
		return false
	}
	for _, field := range m.FilledFields {
		if field == FilledParticipant {
			return true
		}
	}
	return false
}
