package siprec

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	pkg_errors "siprec-server/pkg/errors"
)

// UpdateRecordingSession updates an existing recording session with new metadata
func UpdateRecordingSession(existing *RecordingSession, rsMetadata *RSMetadata) {
	if existing == nil || rsMetadata == nil {
		return
	}

	if existing.ExtendedMetadata == nil {
		existing.ExtendedMetadata = make(map[string]string)
	}

	// Update recording state if changed
	if rsMetadata.State != "" {
		existing.RecordingState = rsMetadata.State
	}

	if reason := strings.TrimSpace(rsMetadata.Reason); reason != "" {
		existing.StateReason = reason
		existing.ExtendedMetadata["state_reason"] = reason
		existing.ExtendedMetadata["reason"] = reason
	}

	if reasonRef := strings.TrimSpace(rsMetadata.ReasonRef); reasonRef != "" {
		existing.StateReasonRef = reasonRef
		existing.ExtendedMetadata["state_reason_ref"] = reasonRef
		existing.ExtendedMetadata["reason_ref"] = reasonRef
	}

	if expires := strings.TrimSpace(rsMetadata.Expires); expires != "" {
		existing.ExtendedMetadata["state_expires"] = expires
		existing.ExtendedMetadata["expires"] = expires
		if parsed, err := time.Parse(time.RFC3339, expires); err == nil {
			existing.StateExpires = parsed
		}
	}

	// Update participant information
	existing.Participants = updateParticipants(existing.Participants, rsMetadata.Participants)

	if len(rsMetadata.SessionGroupAssociations) > 0 {
		existing.SessionGroups = rsMetadata.SessionGroupAssociations
		if existing.SessionGroupRoles == nil {
			existing.SessionGroupRoles = make(map[string]string, len(rsMetadata.SessionGroupAssociations))
		} else {
			for key := range existing.SessionGroupRoles {
				delete(existing.SessionGroupRoles, key)
			}
		}
		for _, assoc := range rsMetadata.SessionGroupAssociations {
			existing.SessionGroupRoles[assoc.SessionGroupID] = assoc.Role
			key := fmt.Sprintf("session_group_%s", assoc.SessionGroupID)
			existing.ExtendedMetadata[key] = assoc.Role
		}
	}

	if len(rsMetadata.PolicyUpdates) > 0 {
		existing.PolicyUpdates = rsMetadata.PolicyUpdates
		if existing.PolicyStates == nil {
			existing.PolicyStates = make(map[string]PolicyAckStatus, len(rsMetadata.PolicyUpdates))
		} else {
			for key := range existing.PolicyStates {
				delete(existing.PolicyStates, key)
			}
		}
		for _, policy := range rsMetadata.PolicyUpdates {
			rawTimestamp := strings.TrimSpace(policy.Timestamp)
			reportedAt := time.Now()
			if rawTimestamp != "" {
				if parsed, err := time.Parse(time.RFC3339, rawTimestamp); err == nil {
					reportedAt = parsed
				}
			}
			statusValue := strings.ToLower(strings.TrimSpace(policy.Status))
			existing.PolicyStates[policy.PolicyID] = PolicyAckStatus{
				Status:       statusValue,
				Acknowledged: policy.Acknowledged,
				ReportedAt:   reportedAt,
				RawTimestamp: rawTimestamp,
			}

			statusKey := fmt.Sprintf("policy_%s_status", policy.PolicyID)
			existing.ExtendedMetadata[statusKey] = statusValue
			existing.ExtendedMetadata[statusKey+"_ack"] = strconv.FormatBool(policy.Acknowledged)
			if rawTimestamp != "" {
				existing.ExtendedMetadata[statusKey+"_timestamp"] = rawTimestamp
			} else {
				delete(existing.ExtendedMetadata, statusKey+"_timestamp")
			}
		}
	}

	// Update sequence number respecting explicit metadata when provided
	if rsMetadata.Sequence > 0 {
		if rsMetadata.Sequence >= existing.SequenceNumber {
			existing.SequenceNumber = rsMetadata.Sequence
		}
	} else if existing.SequenceNumber == 0 {
		// Bootstrap sequence when metadata omitted it entirely
		existing.SequenceNumber = 1
	}

	// Update associated time
	existing.AssociatedTime = time.Now()
	existing.UpdatedAt = time.Now()
}

// DetectParticipantChanges analyzes metadata to identify participant changes
// Simplified implementation with more efficient comparison
func DetectParticipantChanges(existing *RecordingSession, rsMetadata *RSMetadata) (added []Participant, removed []Participant, modified []Participant) {
	// Create maps for easy lookups
	existingMap := make(map[string]Participant)
	for _, p := range existing.Participants {
		existingMap[p.ID] = p
	}

	newMap := make(map[string]RSParticipant)
	for _, p := range rsMetadata.Participants {
		newMap[p.ID] = p
	}

	// Find added participants
	for id, newP := range newMap {
		if _, exists := existingMap[id]; !exists {
			// Convert to participant and add
			added = append(added, ConvertRSParticipantToParticipant(newP))
		}
	}

	// Find removed participants
	for id, existingP := range existingMap {
		if _, exists := newMap[id]; !exists {
			removed = append(removed, existingP)
		}
	}

	// Find modified participants
	for id, existingP := range existingMap {
		if newP, exists := newMap[id]; exists {
			// Check if anything changed
			changed := false

			// Name changes
			if newP.Name != "" && newP.Name != existingP.Name {
				changed = true
			}

			// Display name changes
			if newP.NameID != "" && newP.NameID != existingP.DisplayName {
				changed = true
			}

			// AOR changes - simplified comparison
			if len(newP.Aor) > 0 {
				// Simple length check first
				if len(newP.Aor) != len(existingP.CommunicationIDs) {
					changed = true
				} else {
					// Create a map of new AOR values
					newAors := make(map[string]bool)
					for _, aor := range newP.Aor {
						newAors[aor.Value] = true
					}

					// Check if all existing AORs are in new set
					for _, comm := range existingP.CommunicationIDs {
						if !newAors[comm.Value] {
							changed = true
							break
						}
					}
				}
			}

			if changed {
				// Create updated participant
				participant := existingP // Start with existing

				if newP.Name != "" {
					participant.Name = newP.Name
				}

				if newP.NameID != "" {
					participant.DisplayName = newP.NameID
				}

				// Update communication IDs if provided
				if len(newP.Aor) > 0 {
					newCommunicationIDs := make([]CommunicationID, 0, len(newP.Aor))
					for _, aor := range newP.Aor {
						newCommunicationIDs = append(newCommunicationIDs, CommunicationID{
							Type:  "sip",
							Value: aor.Value,
						})
					}
					participant.CommunicationIDs = newCommunicationIDs
				}

				modified = append(modified, participant)
			}
		}
	}

	return added, removed, modified
}

// Helper function to extract participant IDs for logging
func GetParticipantIDs(participants []Participant) []string {
	ids := make([]string, 0, len(participants))
	for _, p := range participants {
		ids = append(ids, p.ID)
	}
	return ids
}

// updateParticipants merges existing and new participant information
func updateParticipants(existingParticipants []Participant, newParticipants []RSParticipant) []Participant {
	// Create map of existing participants by ID for easy lookup
	existingMap := make(map[string]Participant)
	for _, p := range existingParticipants {
		existingMap[p.ID] = p
	}

	// Process new/updated participants
	result := []Participant{}
	for _, np := range newParticipants {
		// Check if this participant already exists
		if existing, ok := existingMap[np.ID]; ok {
			// Update existing participant
			updated := existing
			if np.Name != "" {
				updated.Name = np.Name
			}
			if np.NameID != "" {
				updated.DisplayName = np.NameID
			}

			// Update communication IDs if provided
			if len(np.Aor) > 0 {
				newCommunicationIDs := []CommunicationID{}
				for _, aor := range np.Aor {
					newCommunicationIDs = append(newCommunicationIDs, CommunicationID{
						Type:  "sip", // Assuming SIP by default
						Value: aor.Value,
					})
				}
				updated.CommunicationIDs = newCommunicationIDs
			}

			result = append(result, updated)
			delete(existingMap, np.ID) // Remove from map to track processed participants
		} else {
			// New participant
			newParticipant := Participant{
				ID:          np.ID,
				Name:        np.Name,
				DisplayName: np.DisplayName,
				Role:        np.Role,
			}

			// Add communication IDs
			for _, aor := range np.Aor {
				newParticipant.CommunicationIDs = append(newParticipant.CommunicationIDs, CommunicationID{
					Type:  "sip",
					Value: aor.Value,
				})
			}

			result = append(result, newParticipant)
		}
	}

	// Add any remaining existing participants that weren't updated
	for _, p := range existingMap {
		result = append(result, p)
	}

	return result
}

// ConvertRSParticipantToParticipant converts RSParticipant to Participant
func ConvertRSParticipantToParticipant(p RSParticipant) Participant {
	participant := Participant{
		ID:          p.ID,
		Name:        p.Name,
		DisplayName: p.DisplayName,
		Role:        p.Role,
	}

	// Add communication IDs
	for _, aor := range p.Aor {
		participant.CommunicationIDs = append(participant.CommunicationIDs, CommunicationID{
			Type:  "sip",
			Value: aor.Value,
		})
	}

	if len(participant.CommunicationIDs) == 0 {
		for _, ni := range p.NameInfos {
			value := ni.AOR
			if value == "" {
				value = ni.URI
			}
			if strings.TrimSpace(value) == "" {
				continue
			}
			participant.CommunicationIDs = append(participant.CommunicationIDs, CommunicationID{
				Type:  "sip",
				Value: value,
			})
		}
	}

	if participant.DisplayName == "" {
		if p.NameID != "" {
			participant.DisplayName = p.NameID
		} else if p.Name != "" {
			participant.DisplayName = p.Name
		} else if len(p.NameInfos) > 0 && strings.TrimSpace(p.NameInfos[0].Display) != "" {
			participant.DisplayName = strings.TrimSpace(p.NameInfos[0].Display)
		}
	}

	return participant
}

// LogRecordingSession removed as it was only used for debugging

// CreateFailoverMetadata generates RFC 7245 compliant metadata for session failover
// This function creates metadata specifically for session recovery/failover operations
func CreateFailoverMetadata(originalSession *RecordingSession) *RSMetadata {
	// Generate a new failover ID if not present
	failoverID := originalSession.FailoverID
	if failoverID == "" {
		failoverID = uuid.New().String()
	}

	// Create base metadata
	metadata := &RSMetadata{
		SessionID: originalSession.ID,
		State:     originalSession.RecordingState,
		Sequence:  originalSession.SequenceNumber + 1,
		Reason:    "failover",
		ReasonRef: "urn:ietf:params:xml:ns:recording:1:failover",
	}

	// Add RFC 7245 specific Session Recording Association information
	metadata.SessionRecordingAssoc = RSAssociation{
		SessionID: originalSession.ID,
		FixedID:   failoverID,         // Use the failover ID as the fixed ID for recovery
		CallID:    originalSession.ID, // Original session ID
	}

	// Add participant information
	for _, participant := range originalSession.Participants {
		display := participant.DisplayName
		if display == "" {
			display = participant.Name
		}

		rsParticipant := RSParticipant{
			ID:          participant.ID,
			Name:        participant.Name,
			DisplayName: display,
			Role:        participant.Role,
		}

		// Add communication identifiers
		for _, commID := range participant.CommunicationIDs {
			aorValue := NormalizeCommunicationURI(commID)
			rsParticipant.Aor = append(rsParticipant.Aor, Aor{
				Value:    aorValue,
				URI:      aorValue,
				Display:  commID.DisplayName,
				Priority: commID.Priority,
			})

			nameEntry := RSNameID{
				AOR:     aorValue,
				URI:     aorValue,
				Display: display,
			}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		if len(rsParticipant.NameInfos) == 0 && display != "" {
			nameEntry := RSNameID{
				Display: display,
			}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		metadata.Participants = append(metadata.Participants, rsParticipant)
	}

	metadata.Normalize()

	return metadata
}

// ParseFailoverMetadata extracts failover information from rs-metadata
// Used to reconstruct recording sessions during recovery
// Simplified to reduce redundant checks
func ParseFailoverMetadata(metadata *RSMetadata) (string, string, error) {
	if metadata == nil {
		return "", "", pkg_errors.NewInvalidMetadata("cannot parse nil metadata")
	}

	// Extract the original session ID and failover ID
	originalSessionID := metadata.SessionID
	failoverID := metadata.SessionRecordingAssoc.FixedID

	// Validate both values in a single check
	if originalSessionID == "" || failoverID == "" {
		fields := map[string]interface{}{
			"has_session_id": originalSessionID != "",
			"has_fixed_id":   failoverID != "",
		}
		return "", "", pkg_errors.NewInvalidMetadata("missing required fields").WithFields(fields)
	}

	return originalSessionID, failoverID, nil
}

// GenerateStateChangeMetadata creates a metadata update for recording state changes
// Complies with RFC 7866 requirements for state change notifications
func GenerateStateChangeMetadata(session *RecordingSession, newState string, reason string) *RSMetadata {
	metadata := &RSMetadata{
		SessionID: session.ID,
		State:     newState,
		Sequence:  session.SequenceNumber + 1,
		Reason:    reason,
	}

	// Add the session recording association
	metadata.SessionRecordingAssoc = RSAssociation{
		SessionID: session.ID,
		FixedID:   session.FailoverID,
	}

	// Include minimal participant information (required by RFC 7866)
	for _, participant := range session.Participants {
		display := participant.DisplayName
		if display == "" {
			display = participant.Name
		}

		rsParticipant := RSParticipant{
			ID:          participant.ID,
			Name:        participant.Name,
			DisplayName: display,
			Role:        participant.Role,
		}

		// Add at least one communication identifier
		if len(participant.CommunicationIDs) > 0 {
			first := participant.CommunicationIDs[0]
			aorValue := NormalizeCommunicationURI(first)
			rsParticipant.Aor = append(rsParticipant.Aor, Aor{
				Value: aorValue,
				URI:   aorValue,
			})
			nameEntry := RSNameID{
				AOR:     aorValue,
				URI:     aorValue,
				Display: display,
			}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		metadata.Participants = append(metadata.Participants, rsParticipant)
	}

	metadata.Normalize()

	return metadata
}

// CreateReplacesHeader generates a SIP Replaces header for session recovery
// As specified in RFC 7245 for SIP-based Communications Session Continuity
func CreateReplacesHeader(session *RecordingSession, dialogID string, earlyFlag bool) string {
	// Format: Replaces: call-id;to-tag=to-tag-value;from-tag=from-tag-value
	replacesHeader := session.ID

	// Add dialog tags if available
	if dialogID != "" {
		// Extract to-tag and from-tag from dialogID
		// Dialog ID format is typically: call-id;to-tag=xxx;from-tag=yyy
		parts := strings.Split(dialogID, ";")
		for _, part := range parts[1:] { // Skip call-id part
			replacesHeader += ";" + part
		}
	}

	// Add early-only parameter if this is an early dialog
	if earlyFlag {
		replacesHeader += ";early-only"
	}

	return replacesHeader
}

// ParseReplacesHeader parses a SIP Replaces header
// Used during session recovery to extract original session information
func ParseReplacesHeader(replacesHeader string) (callID string, toTag string, fromTag string, earlyOnly bool, err error) {
	if replacesHeader == "" {
		return "", "", "", false, fmt.Errorf("empty Replaces header")
	}

	parts := strings.Split(replacesHeader, ";")
	if len(parts) < 3 {
		return "", "", "", false, fmt.Errorf("invalid Replaces header format: missing tags")
	}

	// First part is the Call-ID
	callID = parts[0]

	// Extract tags and flags
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "to-tag=") {
			toTag = strings.TrimPrefix(part, "to-tag=")
		} else if strings.HasPrefix(part, "from-tag=") {
			fromTag = strings.TrimPrefix(part, "from-tag=")
		} else if part == "early-only" {
			earlyOnly = true
		}
	}

	// Validate required components
	if callID == "" || toTag == "" || fromTag == "" {
		return callID, toTag, fromTag, earlyOnly, fmt.Errorf("invalid Replaces header: missing required components")
	}

	return callID, toTag, fromTag, earlyOnly, nil
}

// SerializeMetadata converts an RSMetadata object to XML string
// Useful for sending metadata in SIP messages during session recovery
func SerializeMetadata(metadata *RSMetadata) (string, error) {
	if metadata == nil {
		return "", fmt.Errorf("cannot serialize nil metadata")
	}

	xmlBytes, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata to XML: %v", err)
	}

	// Add XML declaration
	xmlString := `<?xml version="1.0" encoding="UTF-8"?>` + "\n" + string(xmlBytes)
	return xmlString, nil
}

// RecoverSession creates a new recording session based on failover metadata
// Implements core functionality required by RFC 7245
// Simplified to remove redundant checks
func RecoverSession(failoverMetadata *RSMetadata) (*RecordingSession, error) {
	if failoverMetadata == nil {
		return nil, pkg_errors.NewInvalidInput("cannot recover session from nil metadata").
			WithCode("RECOVERY_FAILED")
	}

	// Extract original session ID and failover ID
	originalSessionID, failoverID, err := ParseFailoverMetadata(failoverMetadata)
	if err != nil {
		return nil, pkg_errors.Wrap(err, "failed to parse failover metadata").
			WithCode("RECOVERY_FAILED").
			WithField("reason", "metadata_parse_error")
	}

	// Create a new recording session with the same ID
	session := &RecordingSession{
		ID:                originalSessionID,
		FailoverID:        failoverID,
		RecordingState:    failoverMetadata.State,
		SequenceNumber:    failoverMetadata.Sequence,
		AssociatedTime:    time.Now(),
		ReplacesSessionID: originalSessionID, // Mark that this session replaces the original
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
		IsValid:           true,
	}

	// Add participants from metadata
	if len(failoverMetadata.Participants) > 0 {
		session.Participants = make([]Participant, 0, len(failoverMetadata.Participants))
		for _, rsParticipant := range failoverMetadata.Participants {
			participant := ConvertRSParticipantToParticipant(rsParticipant)
			session.Participants = append(session.Participants, participant)
		}
	} else {
		// Log a warning about no participants
		return session, pkg_errors.New("session recovered without participants").
			WithCode("RECOVERY_WARNING").
			WithFields(map[string]interface{}{
				"session_id":  originalSessionID,
				"failover_id": failoverID,
				"severity":    "warning",
			})
	}

	return session, nil
}

// ProcessStreamRecovery restores stream information during session recovery
// Required by RFC 7245 for media continuity
func ProcessStreamRecovery(session *RecordingSession, metadata *RSMetadata) {
	if session == nil || metadata == nil {
		return
	}

	// Clear existing media stream types to rebuild from metadata
	session.MediaStreamTypes = []string{}

	// Process stream information from metadata
	for _, stream := range metadata.Streams {
		// Add stream type to recording session if not already present
		streamType := stream.Type
		if streamType != "" {
			found := false
			for _, existingType := range session.MediaStreamTypes {
				if existingType == streamType {
					found = true
					break
				}
			}

			if !found {
				session.MediaStreamTypes = append(session.MediaStreamTypes, streamType)
			}
		}

		// Update participant stream associations
		for _, participant := range metadata.Participants {
			participantID := participant.ID

			// Check if participant sends to this stream
			for _, sendLabel := range participant.Send {
				if sendLabel == stream.Label {
					// Find participant in session and update
					for i, sessionParticipant := range session.Participants {
						if sessionParticipant.ID == participantID {
							// Check if stream is already in the participant's streams
							foundStream := false
							for _, streamID := range sessionParticipant.MediaStreams {
								if streamID == stream.StreamID {
									foundStream = true
									break
								}
							}

							if !foundStream {
								session.Participants[i].MediaStreams = append(
									session.Participants[i].MediaStreams,
									stream.StreamID,
								)
							}
							break
						}
					}
				}
			}
		}
	}
}

// ValidateSessionContinuity validates that a recovered session maintains continuity
// as required by RFC 7245
func ValidateSessionContinuity(originalSession, recoveredSession *RecordingSession) error {
	if originalSession == nil || recoveredSession == nil {
		return pkg_errors.NewInvalidInput("cannot validate nil sessions").
			WithCode("CONTINUITY_VALIDATION_FAILED")
	}

	// Collect context information for potential errors
	context := map[string]interface{}{
		"original_session_id":         originalSession.ID,
		"recovered_session_id":        recoveredSession.ID,
		"original_failover_id":        originalSession.FailoverID,
		"recovered_failover_id":       recoveredSession.FailoverID,
		"original_participant_count":  len(originalSession.Participants),
		"recovered_participant_count": len(recoveredSession.Participants),
	}

	// Verify session IDs match
	if originalSession.ID != recoveredSession.ID {
		return pkg_errors.NewInvalidInput(
			fmt.Sprintf("session ID mismatch: original=%s, recovered=%s",
				originalSession.ID, recoveredSession.ID)).
			WithCode("CONTINUITY_VALIDATION_FAILED").
			WithFields(context).
			WithField("error_type", "session_id_mismatch")
	}

	// Verify failover IDs match if both are present
	if originalSession.FailoverID != "" && recoveredSession.FailoverID != "" &&
		originalSession.FailoverID != recoveredSession.FailoverID {
		return pkg_errors.NewInvalidInput(
			fmt.Sprintf("failover ID mismatch: original=%s, recovered=%s",
				originalSession.FailoverID, recoveredSession.FailoverID)).
			WithCode("CONTINUITY_VALIDATION_FAILED").
			WithFields(context).
			WithField("error_type", "failover_id_mismatch")
	}

	// Verify essential participants are preserved
	originalParticipants := make(map[string]struct{})
	for _, p := range originalSession.Participants {
		originalParticipants[p.ID] = struct{}{}
	}

	recoveredParticipants := make(map[string]struct{})
	for _, p := range recoveredSession.Participants {
		recoveredParticipants[p.ID] = struct{}{}
	}

	// Check for missing participants (all essential participants must be preserved)
	var missingParticipants []string
	for id := range originalParticipants {
		if _, exists := recoveredParticipants[id]; !exists {
			missingParticipants = append(missingParticipants, id)
		}
	}

	if len(missingParticipants) > 0 {
		return pkg_errors.NewInvalidInput("essential participants missing in recovered session").
			WithCode("CONTINUITY_VALIDATION_FAILED").
			WithFields(context).
			WithField("error_type", "missing_participants").
			WithField("missing_participants", missingParticipants)
	}

	// Session continuity is valid
	return nil
}

// SetSessionExpiration sets the expiration time for a recording session
// Implements the expiration functionality from RFC 7866
func SetSessionExpiration(session *RecordingSession, duration time.Duration) {
	if session == nil {
		return
	}

	// Set the expiration time based on the current time plus the duration
	session.RetentionPeriod = duration

	// Calculate the actual expiration time
	if !session.StartTime.IsZero() {
		session.EndTime = session.StartTime.Add(duration)
	} else {
		// If no start time recorded, use current time
		session.StartTime = time.Now()
		session.EndTime = session.StartTime.Add(duration)
	}
}

func NormalizeCommunicationURI(commID CommunicationID) string {
	value := strings.TrimSpace(commID.Value)
	if value == "" {
		return ""
	}

	switch strings.ToLower(strings.TrimSpace(commID.Type)) {
	case "sip":
		if !strings.HasPrefix(strings.ToLower(value), "sip:") {
			value = "sip:" + value
		}
	case "tel":
		if !strings.HasPrefix(strings.ToLower(value), "tel:") {
			value = "tel:" + value
		}
	}

	return value
}

// GenerateSessionUpdateNotification creates metadata for session updates
// Used to notify recording participants of changes per RFC 7866
func GenerateSessionUpdateNotification(session *RecordingSession, updateReason string) *RSMetadata {
	// Create the base metadata
	metadata := &RSMetadata{
		SessionID: session.ID,
		State:     session.RecordingState,
		Sequence:  session.SequenceNumber + 1,
		Reason:    updateReason,
	}

	// Set expiration time if applicable
	if !session.EndTime.IsZero() {
		metadata.Expires = session.EndTime.Format(time.RFC3339)
	}

	// Add session recording association with failover ID if present
	metadata.SessionRecordingAssoc = RSAssociation{
		SessionID: session.ID,
	}

	if session.FailoverID != "" {
		metadata.SessionRecordingAssoc.FixedID = session.FailoverID
	}

	// Add essential participant information
	for _, participant := range session.Participants {
		display := participant.DisplayName
		if display == "" {
			display = participant.Name
		}

		rsParticipant := RSParticipant{
			ID:          participant.ID,
			Name:        participant.Name,
			DisplayName: display,
			Role:        participant.Role,
		}

		// Add at least one communication identifier for each participant
		if len(participant.CommunicationIDs) > 0 {
			commID := participant.CommunicationIDs[0]
			aorValue := NormalizeCommunicationURI(commID)
			rsParticipant.Aor = append(rsParticipant.Aor, Aor{
				Value:    aorValue,
				URI:      aorValue,
				Display:  commID.DisplayName,
				Priority: commID.Priority,
			})
			nameEntry := RSNameID{
				AOR:     aorValue,
				URI:     aorValue,
				Display: display,
			}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		metadata.Participants = append(metadata.Participants, rsParticipant)
	}

	metadata.Normalize()

	return metadata
}

// GetEffectiveRTPTimeout returns the RTP timeout to use for a session
// considering per-call overrides
func GetEffectiveRTPTimeout(session *RecordingSession, globalTimeout time.Duration) time.Duration {
	if session != nil && session.CustomRTPTimeout > 0 {
		return session.CustomRTPTimeout
	}
	if session != nil && session.Timeout > 0 {
		return session.Timeout
	}
	return globalTimeout
}
