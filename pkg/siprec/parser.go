package siprec

import (
	"encoding/xml"
	stderrors "errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"

	"siprec-server/pkg/errors"
)

// ValidationResult captures schema validation issues for SIPREC metadata.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// Allowed recording states per RFC 6341/7866.
var allowedRecordingStates = map[string]struct{}{
	"pending":      {},
	"initializing": {},
	"active":       {},
	"paused":       {},
	"partial":      {},
	"inactive":     {},
	"resuming":     {},
	"recovering":   {},
	"completed":    {},
	"terminated":   {},
	"error":        {},
	"unknown":      {},
}

// Allowed reason codes when signalling state changes.
var allowedRecordingReasons = map[string]struct{}{
	"normal":             {},
	"manual":             {},
	"error":              {},
	"failure":            {},
	"system-failure":     {},
	"media-failure":      {},
	"resource-exhausted": {},
	"policy":             {},
	"timeout":            {},
	"emergency":          {},
	"cancelled":          {},
}

var stateReasonMatrix = map[string]map[string]struct{}{
	"pending": {
		"normal":             {},
		"manual":             {},
		"policy":             {},
		"resource-exhausted": {},
	},
	"initializing": {
		"normal": {},
		"manual": {},
		"policy": {},
	},
	"active": {
		"normal":    {},
		"manual":    {},
		"policy":    {},
		"emergency": {},
	},
	"paused": {
		"manual":             {},
		"policy":             {},
		"resource-exhausted": {},
		"system-failure":     {},
	},
	"partial": {
		"manual":             {},
		"policy":             {},
		"resource-exhausted": {},
		"system-failure":     {},
	},
	"inactive": {
		"normal": {},
		"manual": {},
		"policy": {},
	},
	"resuming": {
		"normal": {},
		"manual": {},
	},
	"recovering": {
		"manual":         {},
		"system-failure": {},
		"error":          {},
	},
	"completed": {
		"normal": {},
		"manual": {},
		"policy": {},
	},
	"terminated": {
		"normal":             {},
		"manual":             {},
		"error":              {},
		"failure":            {},
		"system-failure":     {},
		"media-failure":      {},
		"resource-exhausted": {},
		"policy":             {},
		"timeout":            {},
		"emergency":          {},
		"cancelled":          {},
	},
	"error": {
		"error":          {},
		"failure":        {},
		"system-failure": {},
		"media-failure":  {},
	},
}

var allowedPolicyStatuses = map[string]struct{}{
	"pending":      {},
	"applied":      {},
	"acknowledged": {},
	"rejected":     {},
	"accepted":     {},
	"denied":       {},
	"revoked":      {},
	"deferred":     {},
	"error":        {},
}

// Allowed participant roles per RFC 7866 §4.7 (case-insensitive).
var allowedParticipantRoles = map[string]struct{}{
	"active":   {},
	"passive":  {},
	"focus":    {},
	"mixer":    {},
	"observer": {},
}

func (vr *ValidationResult) addError(msg string) {
	vr.Errors = append(vr.Errors, msg)
}

func (vr *ValidationResult) addWarning(msg string) {
	vr.Warnings = append(vr.Warnings, msg)
}

// ParseSiprecInvite extracts SDP and rs-metadata from a SIPREC INVITE request
func ParseSiprecInvite(req *sip.Request) (sdp string, metadata *RSMetadata, err error) {
	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return "", nil, stderrors.New("missing Content-Type header")
	}

	// Check for multipart MIME content
	mediaType, params, err := mime.ParseMediaType(contentType.Value())
	if err != nil {
		return "", nil, fmt.Errorf("invalid Content-Type header: %w", err)
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		return "", nil, stderrors.New("not a multipart MIME message")
	}

	boundary, ok := params["boundary"]
	if !ok {
		return "", nil, stderrors.New("no boundary parameter in Content-Type")
	}

	// Parse multipart MIME body
	reader := multipart.NewReader(strings.NewReader(string(req.Body())), boundary)
	var sdpContent string
	var metadataContent string

	for {
		part, err := reader.NextPart()
		if err != nil {
			// End of multipart message
			break
		}

		contentTypeHeader := strings.TrimSpace(part.Header.Get("Content-Type"))
		if contentTypeHeader == "" {
			continue
		}

		mediaType, _, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			// Skip parts with malformed content types but continue parsing remaining parts
			continue
		}

		switch strings.ToLower(mediaType) {
		case "application/sdp":
			// Read SDP content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return "", nil, fmt.Errorf("error reading SDP part: %w", err)
			}
			sdpContent = buf.String()
		case "application/rs-metadata+xml":
			// Read rs-metadata content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return "", nil, fmt.Errorf("error reading rs-metadata part: %w", err)
			}
			metadataContent = buf.String()
		}
	}

	if sdpContent == "" {
		return "", nil, stderrors.New("no SDP content found in multipart message")
	}
	if metadataContent == "" {
		return "", nil, stderrors.New("no rs-metadata content found in multipart message")
	}

	// Parse rs-metadata XML
	var rsMetadata RSMetadata
	err = xml.Unmarshal([]byte(metadataContent), &rsMetadata)
	if err != nil {
		return "", nil, fmt.Errorf("error parsing rs-metadata XML: %w", err)
	}

	return sdpContent, &rsMetadata, nil
}

// CreateMetadataResponse creates a response rs-metadata with proper session ID
func CreateMetadataResponse(metadata *RSMetadata) (string, error) {
	if metadata == nil {
		return "", fmt.Errorf("metadata cannot be nil")
	}

	response := *metadata

	state := strings.TrimSpace(response.State)
	if state == "" {
		state = "active"
	}
	response.State = state

	if response.Sequence <= 0 {
		response.Sequence = 1
	}

	response.Normalize()

	metadataBytes, err := xml.Marshal(response)
	if err != nil {
		return "", fmt.Errorf("error marshaling response metadata: %w", err)
	}

	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	return xmlHeader + string(metadataBytes), nil
}

// CreateMultipartResponse creates a multipart MIME response with SDP and rs-metadata
// Enhanced for RFC compliance with proper Content-Disposition values
func CreateMultipartResponse(sdp string, metadata string) (string, string) {
	// Generate a boundary that is unlikely to appear in the content
	boundary := "boundary_" + uuid.New().String()

	// Format the multipart content with CRLF separators per RFC 3261/2046
	// Note: handling=required is set on both parts to signal mandatory processing
	multipartContent := fmt.Sprintf(
		"--%s\r\n"+
			"Content-Type: application/sdp\r\n"+
			"Content-Disposition: session;handling=required\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s\r\n"+
			"Content-Type: application/rs-metadata+xml\r\n"+
			"Content-Disposition: recording-session;handling=required\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s--\r\n",
		boundary, sdp, boundary, metadata, boundary)

	// Create the Content-Type header with proper boundary parameter
	contentType := `multipart/mixed;boundary="` + boundary + `"`

	return contentType, multipartContent
}

// ExtractRSMetadataFromRequest extracts SIPREC metadata from a SIP request
// Enhanced with better error handling and robustness
func ExtractRSMetadataFromRequest(req *sip.Request) (*RSMetadata, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("request is nil").
			WithCode("EXTRACT_METADATA_FAILED")
	}

	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return nil, errors.NewInvalidInput("missing Content-Type header").
			WithCode("MISSING_HEADER").
			WithField("header", "Content-Type")
	}

	// Check for multipart MIME content which typically contains rs-metadata
	mediaType, params, err := mime.ParseMediaType(contentType.Value())
	if err != nil {
		return nil, errors.Wrap(err, "invalid Content-Type header").
			WithCode("INVALID_CONTENT_TYPE").
			WithField("content_type", contentType.Value())
	}

	// If not multipart, can't contain SIPREC metadata
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, errors.NewInvalidInput("not a multipart MIME message").
			WithCode("NOT_MULTIPART").
			WithField("media_type", mediaType)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, errors.NewInvalidInput("no boundary parameter in Content-Type").
			WithCode("MISSING_BOUNDARY").
			WithField("content_type", contentType.Value())
	}

	// Parse multipart MIME body
	reader := multipart.NewReader(strings.NewReader(string(req.Body())), boundary)
	var metadataContent string
	var sdpFound bool

	for {
		part, err := reader.NextPart()
		if err != nil {
			// End of multipart message or error
			if err == io.EOF {
				break
			}
			return nil, errors.Wrap(err, "error reading multipart").
				WithCode("MULTIPART_PARSE_ERROR")
		}

		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		partDisposition := part.Header.Get("Content-Disposition")

		partMediaType := strings.ToLower(partContentType)
		if partContentType != "" {
			if mediaType, _, err := mime.ParseMediaType(partContentType); err == nil {
				partMediaType = strings.ToLower(mediaType)
			}
		}

		// Check for SDP part - we need to track if it's present for a valid SIPREC request
		if partMediaType == "application/sdp" {
			sdpFound = true
		} else if partMediaType == "application/rs-metadata+xml" {
			// Read rs-metadata content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return nil, errors.Wrap(err, "error reading rs-metadata part").
					WithCode("METADATA_READ_ERROR")
			}
			metadataContent = buf.String()

			// Verify proper Content-Disposition for SIPREC compliance
			if !strings.Contains(partDisposition, "recording-session") {
				// This is just a warning, not a hard error
				fmt.Printf("Warning: rs-metadata part has non-standard Content-Disposition: %s\n", partDisposition)
			}
		}
	}

	// Check for missing parts
	if metadataContent == "" {
		return nil, errors.NewInvalidInput("no rs-metadata content found in multipart message").
			WithCode("MISSING_METADATA")
	}

	if !sdpFound {
		return nil, errors.NewInvalidInput("no SDP content found in multipart message").
			WithCode("MISSING_SDP").
			WithField("content_type", contentType.Value())
	}

	// Parse rs-metadata XML
	var rsMetadata RSMetadata
	err = xml.Unmarshal([]byte(metadataContent), &rsMetadata)
	if err != nil {
		return nil, errors.Wrap(err, "error parsing rs-metadata XML").
			WithCode("XML_PARSE_ERROR").
			WithField("xml_content", metadataContent[:min(len(metadataContent), 100)]+"...") // Truncate to avoid huge errors
	}

	validation := ValidateSiprecMessage(&rsMetadata)
	if len(validation.Errors) > 0 {
		return nil, errors.NewInvalidInput(fmt.Sprintf("critical metadata validation failure: %v", validation.Errors)).
			WithCode("INVALID_METADATA").
			WithFields(map[string]interface{}{
				"deficiencies": validation.Errors,
				"session_id":   rsMetadata.SessionID,
			})
	}

	if len(validation.Warnings) > 0 {
		fmt.Printf("Warning: SIPREC metadata validation warnings: %v\n", validation.Warnings)
	}

	return &rsMetadata, nil
}

// Helper function to find minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ExtractRSMetadata extracts metadata from a multipart MIME message
func ExtractRSMetadata(contentType string, body []byte) (*RSMetadata, error) {
	// Parse the content type to get the boundary
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Type header: %w", err)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, stderrors.New("no boundary parameter in Content-Type")
	}

	// Parse multipart MIME body
	reader := multipart.NewReader(strings.NewReader(string(body)), boundary)
	var metadataContent string

	for {
		part, err := reader.NextPart()
		if err != nil {
			// End of multipart message or error
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("error reading multipart: %w", err)
		}

		contentTypeHeader := strings.TrimSpace(part.Header.Get("Content-Type"))
		if contentTypeHeader == "" {
			continue
		}

		mediaType, _, err := mime.ParseMediaType(contentTypeHeader)
		if err != nil {
			continue
		}

		if strings.EqualFold(mediaType, "application/rs-metadata+xml") {
			// Read rs-metadata content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return nil, fmt.Errorf("error reading rs-metadata part: %w", err)
			}
			metadataContent = buf.String()
			break // Found what we need
		}
	}

	if metadataContent == "" {
		return nil, stderrors.New("no rs-metadata content found in multipart message")
	}

	// Parse rs-metadata XML
	var rsMetadata RSMetadata
	err = xml.Unmarshal([]byte(metadataContent), &rsMetadata)
	if err != nil {
		return nil, fmt.Errorf("error parsing rs-metadata XML: %w", err)
	}

	return &rsMetadata, nil
}

func normalizedParticipantID(participant RSParticipant) string {
	candidates := []string{
		participant.ID,
		participant.LegacyID,
		participant.NameID,
	}
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate); id != "" {
			return id
		}
	}
	return ""
}

func normalizedStreamID(stream Stream) string {
	candidates := []string{
		stream.StreamID,
		stream.StreamIDAlt,
		stream.ID,
	}
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate); id != "" {
			return id
		}
	}
	return ""
}

func normalizedStreamLabel(stream Stream) string {
	candidates := []string{
		stream.Label,
		stream.LabelElement,
		normalizedStreamID(stream),
	}
	for _, candidate := range candidates {
		if label := strings.TrimSpace(candidate); label != "" {
			return label
		}
	}
	return ""
}

func normalizedAssocSessionID(assoc RSAssociation) string {
	candidates := []string{
		assoc.SessionID,
		assoc.SessionIDAlt,
	}
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate); id != "" {
			return id
		}
	}
	return ""
}

// ValidateSiprecMessage performs a comprehensive validation of a SIPREC message
// Returns a list of deficiencies found in the message
func ValidateSiprecMessage(rsMetadata *RSMetadata) ValidationResult {
	result := ValidationResult{}

	if rsMetadata == nil {
		result.addError("metadata is nil")
		return result
	}

	if ns := strings.TrimSpace(rsMetadata.XMLName.Space); ns != "" && ns != "urn:ietf:params:xml:ns:recording:1" {
		result.addWarning(fmt.Sprintf("unexpected metadata namespace: %s", ns))
	} else if ns == "" {
		result.addWarning("metadata XML missing namespace declaration")
	}

	if local := strings.TrimSpace(rsMetadata.XMLName.Local); local != "" && local != "recording" {
		result.addError(fmt.Sprintf("unexpected root element: %s", local))
	}

	sessionID := strings.TrimSpace(rsMetadata.SessionID)
	if sessionID == "" {
		result.addError("missing recording session ID")
	} else if len(sessionID) > 255 {
		result.addError("session ID exceeds maximum allowed length")
	}

	// RFC 7866 §4.2 requires a recording session state attribute.
	state := strings.ToLower(strings.TrimSpace(rsMetadata.State))
	if state == "" {
		result.addWarning("missing recording state attribute; will default to 'active' in responses")
	} else if _, ok := allowedRecordingStates[state]; !ok {
		result.addError(fmt.Sprintf("invalid recording state: %s", rsMetadata.State))
	}

	reason := strings.ToLower(strings.TrimSpace(rsMetadata.Reason))
	if state == "terminated" && reason == "" {
		result.addError("termination reason not provided")
	}
	if reason != "" {
		if _, ok := allowedRecordingReasons[reason]; !ok {
			result.addError(fmt.Sprintf("unsupported recording reason: %s", rsMetadata.Reason))
		}
		if allowedSet, ok := stateReasonMatrix[state]; ok && len(allowedSet) > 0 {
			if _, allowed := allowedSet[reason]; !allowed {
				result.addError(fmt.Sprintf("reason %q is not valid for state %q", reason, state))
			}
		}
	} else if state == "error" {
		result.addError("error state must include a reason")
	}

	if reasonRef := strings.TrimSpace(rsMetadata.ReasonRef); reasonRef != "" {
		if !strings.HasPrefix(reasonRef, "urn:ietf:params:xml:ns:recording:1:") {
			result.addWarning(fmt.Sprintf("reasonref uses non-standard namespace: %s", rsMetadata.ReasonRef))
		}
		if reason == "" {
			result.addWarning("reasonref provided without reason attribute")
		}
	}

	if rsMetadata.Sequence < 0 {
		result.addError("invalid sequence number")
	}

	if expires := strings.TrimSpace(rsMetadata.Expires); expires != "" {
		if _, err := time.Parse(time.RFC3339, expires); err != nil {
			result.addWarning(fmt.Sprintf("expires attribute is not RFC3339 timestamp: %v", err))
		}
	}

	participantIDs := make(map[string]struct{}, len(rsMetadata.Participants))
	if len(rsMetadata.Participants) == 0 {
		result.addError("no participants provided in metadata")
	}
	for _, participant := range rsMetadata.Participants {
		id := normalizedParticipantID(participant)
		if id == "" {
			result.addError("participant missing id attribute")
			continue
		}
		if _, exists := participantIDs[id]; exists {
			result.addError(fmt.Sprintf("duplicate participant id detected: %s", id))
		}
		participantIDs[id] = struct{}{}

		hasContact := false
		for _, aor := range participant.Aor {
			value := strings.TrimSpace(aor.Value)
			if value != "" {
				hasContact = true
			}
			if value == "" {
				result.addError(fmt.Sprintf("participant %s includes empty AOR value", id))
			}
			if uri := strings.TrimSpace(aor.URI); uri != "" && !strings.Contains(uri, ":") {
				result.addWarning(fmt.Sprintf("participant %s has invalid URI format for AOR %s", id, aor.URI))
			}
		}
		if !hasContact {
			for _, ni := range participant.NameInfos {
				if strings.TrimSpace(ni.AOR) != "" || strings.TrimSpace(ni.URI) != "" {
					hasContact = true
					break
				}
			}
		}
		if !hasContact {
			result.addError(fmt.Sprintf("participant %s missing address-of-record or nameID contact info", id))
		}

		if role := strings.ToLower(strings.TrimSpace(participant.Role)); role != "" {
			if _, ok := allowedParticipantRoles[role]; !ok {
				result.addError(fmt.Sprintf("participant %s has invalid role: %s", id, participant.Role))
			}
		}
	}

	for _, group := range rsMetadata.Group {
		groupID := strings.TrimSpace(group.ID)
		if groupID == "" {
			result.addError("group missing id attribute")
			continue
		}
		for _, ref := range group.ParticipantRefs {
			if _, exists := participantIDs[strings.TrimSpace(ref)]; !exists {
				result.addWarning(fmt.Sprintf("group %s references unknown participant %s", groupID, ref))
			}
		}
	}

	streamIDs := make(map[string]struct{}, len(rsMetadata.Streams))
	for _, stream := range rsMetadata.Streams {
		label := normalizedStreamLabel(stream)
		if label == "" {
			result.addError("stream missing label attribute")
		}
		streamID := normalizedStreamID(stream)
		if streamID == "" {
			result.addError("stream missing streamid attribute")
		} else {
			if _, exists := streamIDs[streamID]; exists {
				result.addError(fmt.Sprintf("duplicate streamid detected: %s", streamID))
			}
			streamIDs[streamID] = struct{}{}
		}
		if stream.Type == "" {
			result.addWarning(fmt.Sprintf("stream %s missing type attribute", streamID))
		} else {
			switch stream.Type {
			case "audio", "video", "text", "message", "application":
			default:
				result.addError(fmt.Sprintf("stream %s has invalid type: %s", stream.Label, stream.Type))
			}
		}

		if stream.Mode == "mixed" && len(stream.Mixing.MixedStreams) == 0 {
			result.addWarning(fmt.Sprintf("mixed stream %s has no source streams defined", stream.Label))
		}
	}

	if len(rsMetadata.SessionGroupAssociations) > 0 {
		assocSeen := make(map[string]struct{}, len(rsMetadata.SessionGroupAssociations))
		for _, assoc := range rsMetadata.SessionGroupAssociations {
			groupID := strings.TrimSpace(assoc.SessionGroupID)
			if groupID == "" {
				result.addError("sessiongroupassoc missing sessiongroupid attribute")
			}
			sAssocID := strings.TrimSpace(assoc.SessionID)
			if sAssocID == "" {
				result.addError("sessiongroupassoc missing sessionid attribute")
			} else if sessionID != "" && sAssocID != sessionID {
				result.addWarning(fmt.Sprintf("sessiongroupassoc references mismatched sessionid %s (expected %s)", sAssocID, sessionID))
			}
			key := fmt.Sprintf("%s::%s", groupID, sAssocID)
			if _, exists := assocSeen[key]; exists {
				result.addWarning(fmt.Sprintf("duplicate sessiongroup association detected for %s", key))
			}
			assocSeen[key] = struct{}{}
		}
	}

	if len(rsMetadata.PolicyUpdates) > 0 {
		policyIDs := make(map[string]struct{}, len(rsMetadata.PolicyUpdates))
		for _, update := range rsMetadata.PolicyUpdates {
			policyID := strings.TrimSpace(update.PolicyID)
			if policyID == "" {
				result.addError("policy update missing policyid attribute")
			}
			if policyID != "" {
				if _, exists := policyIDs[policyID]; exists {
					result.addWarning(fmt.Sprintf("duplicate policy update entry for %s", policyID))
				}
				policyIDs[policyID] = struct{}{}
			}

			status := strings.ToLower(strings.TrimSpace(update.Status))
			if status == "" {
				result.addError(fmt.Sprintf("policy %s missing status attribute", policyID))
			} else if _, ok := allowedPolicyStatuses[status]; !ok {
				result.addError(fmt.Sprintf("policy %s uses unsupported status %q", policyID, status))
			}

			if ts := strings.TrimSpace(update.Timestamp); ts != "" {
				if _, err := time.Parse(time.RFC3339, ts); err != nil {
					result.addError(fmt.Sprintf("policy %s timestamp not RFC3339: %v", policyID, err))
				}
			}

			if update.Acknowledged {
				if status == "pending" {
					result.addWarning(fmt.Sprintf("policy %s acknowledged while still pending", policyID))
				}
				if strings.TrimSpace(update.Timestamp) == "" {
					result.addWarning(fmt.Sprintf("policy %s acknowledged without timestamp", policyID))
				}
			} else if status == "acknowledged" || status == "applied" {
				result.addWarning(fmt.Sprintf("policy %s status %q reported without acknowledgement flag", policyID, status))
			}
		}
	}

	assoc := rsMetadata.SessionRecordingAssoc
	if (assoc == RSAssociation{}) {
		result.addError("missing session recording association element")
	} else {
		assocSessionID := normalizedAssocSessionID(assoc)
		if assocSessionID == "" {
			result.addError("missing session ID in recording association")
		} else if sessionID != "" && assocSessionID != sessionID {
			result.addWarning(fmt.Sprintf("recording association sessionid (%s) does not match metadata session (%s)", assocSessionID, sessionID))
		}
		if strings.TrimSpace(assoc.CallID) == "" && strings.TrimSpace(assoc.CallIDAlt) == "" && strings.TrimSpace(assoc.FixedID) == "" {
			result.addWarning("session association missing both call-ID and fixed-ID")
		}
	}

	if len(rsMetadata.Sessions) == 0 {
		result.addError("no <session> elements provided")
	} else if sessionID != "" {
		matchFound := false
		for _, sess := range rsMetadata.Sessions {
			sID := strings.TrimSpace(sess.ID)
			if sID == "" {
				sID = strings.TrimSpace(sess.LegacyID)
			}
			if sID == "" {
				result.addError("session element missing session_id attribute")
				continue
			}
			if sID == sessionID {
				matchFound = true
			}
			for _, pref := range sess.ParticipantRefs {
				if _, ok := participantIDs[strings.TrimSpace(pref)]; !ok && len(participantIDs) > 0 {
					result.addError(fmt.Sprintf("session %s references unknown participant %s", sID, pref))
				}
			}
			for _, sref := range sess.StreamRefs {
				if _, ok := streamIDs[strings.TrimSpace(sref)]; !ok && len(streamIDs) > 0 {
					result.addError(fmt.Sprintf("session %s references unknown stream %s", sID, sref))
				}
			}
		}
		if !matchFound {
			result.addError("no session element matches recording session ID")
		}
	}

	for _, groupAssoc := range rsMetadata.SessionGroupAssociations {
		if strings.TrimSpace(groupAssoc.SessionGroupID) == "" {
			result.addError("session group association missing sessiongroupid")
		}
		if strings.TrimSpace(groupAssoc.SessionID) == "" {
			result.addError("session group association missing sessionid")
		}
	}

	for _, policy := range rsMetadata.PolicyUpdates {
		if strings.TrimSpace(policy.PolicyID) == "" {
			result.addError("policy update missing policyid")
		}
		if strings.TrimSpace(policy.Status) == "" {
			result.addError(fmt.Sprintf("policy %s missing status", policy.PolicyID))
		}
	}

	participantRefs := make(map[string]struct{}, len(rsMetadata.Participants))
	for _, p := range rsMetadata.Participants {
		if id := normalizedParticipantID(p); id != "" {
			participantRefs[id] = struct{}{}
		}
	}

	for _, psa := range rsMetadata.ParticipantSessionAssoc {
		pid := strings.TrimSpace(psa.ParticipantID)
		if pid == "" {
			result.addError("participantsessionassoc missing participant_id")
		} else if _, ok := participantRefs[pid]; !ok {
			result.addError(fmt.Sprintf("participantsessionassoc references unknown participant %s", pid))
		}
		sid := strings.TrimSpace(psa.SessionID)
		if sid == "" {
			result.addError("participantsessionassoc missing session_id")
		} else if sessionID != "" && sid != sessionID {
			result.addWarning(fmt.Sprintf("participantsessionassoc references mismatched sessionid %s (expected %s)", sid, sessionID))
		}
	}

	for _, psa := range rsMetadata.ParticipantStreamAssoc {
		pid := strings.TrimSpace(psa.Participant)
		if pid == "" {
			pid = strings.TrimSpace(psa.ParticipantID)
		}
		if pid == "" {
			result.addError("participantstreamassoc missing participant reference")
		} else if _, ok := participantRefs[pid]; !ok {
			result.addError(fmt.Sprintf("participantstreamassoc references unknown participant %s", pid))
		}

		streamRefs := make([]string, 0, 2+len(psa.Send)+len(psa.Receive))
		if ref := strings.TrimSpace(psa.Stream); ref != "" {
			streamRefs = append(streamRefs, ref)
		}
		if ref := strings.TrimSpace(psa.StreamID); ref != "" {
			streamRefs = append(streamRefs, ref)
		}
		for _, send := range psa.Send {
			if ref := strings.TrimSpace(send); ref != "" {
				streamRefs = append(streamRefs, ref)
			}
		}
		for _, recv := range psa.Receive {
			if ref := strings.TrimSpace(recv); ref != "" {
				streamRefs = append(streamRefs, ref)
			}
		}

		if len(streamRefs) == 0 {
			result.addError("participantstreamassoc missing stream reference")
			continue
		}
		for _, ref := range streamRefs {
			if _, ok := streamIDs[ref]; !ok && len(streamIDs) > 0 {
				result.addError(fmt.Sprintf("participantstreamassoc references unknown stream %s", ref))
			}
		}
	}

	return result
}

// GenerateErrorResponse creates an error response for a SIPREC request
// Used when there are issues with the SIPREC metadata
func GenerateErrorResponse(errorCode int, errorReason string, requestSessionID string) *RSMetadata {
	// If no session ID was provided, generate one to ensure the response is valid
	if requestSessionID == "" {
		requestSessionID = uuid.New().String()
	}

	// Create a minimal valid metadata response indicating an error
	metadata := &RSMetadata{
		SessionID: requestSessionID,
		State:     "terminated", // Immediately terminate the session
		Reason:    errorReason,
		Sequence:  1,
	}

	// Map standard error codes to standardized reason references
	reasonRef := ""
	switch errorCode {
	case 400: // Bad Request
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:invalid-request"
	case 403: // Forbidden
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:not-authorized"
	case 404: // Not Found
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:not-found"
	case 413: // Request Entity Too Large
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:request-too-large"
	case 500: // Server Error
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:server-error"
	case 503: // Service Unavailable
		reasonRef = "urn:ietf:params:xml:ns:recording:1:error:service-unavailable"
	}

	if reasonRef != "" {
		metadata.ReasonRef = reasonRef
	}

	// Add minimal session recording association
	metadata.SessionRecordingAssoc = RSAssociation{
		SessionID: requestSessionID,
	}

	// Add minimal required participant information (RFC 7866 requires at least one participant)
	minimalParticipant := RSParticipant{
		ID:   "server",
		Role: "passive",
		Aor: []Aor{
			{
				Value: "sip:recording-server@unknown",
				URI:   "sip:recording-server@unknown",
			},
		},
		NameInfos: []RSNameID{
			{
				AOR: "sip:recording-server@unknown",
				URI: "sip:recording-server@unknown",
				Names: []LocalizedName{
					{Value: "Recording Server"},
				},
				Display: "Recording Server",
			},
		},
	}
	metadata.Participants = append(metadata.Participants, minimalParticipant)

	metadata.Normalize()

	return metadata
}
