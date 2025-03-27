package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// SIPREC-specific data structures based on RFC 7865
type RecordingSession struct {
	ID               string
	Participants     []Participant
	AssociatedTime   time.Time
	SequenceNumber   int
	RecordingType    string // full, selective, etc.
	RecordingState   string // recording, paused, etc.
	MediaStreamTypes []string
}

type Participant struct {
	ID               string
	Name             string
	DisplayName      string
	CommunicationIDs []CommunicationID
	Role             string // passive, active, etc.
}

type CommunicationID struct {
	Type    string // tel, sip, etc.
	Value   string
	Purpose string // from, to, etc.
}

// RSMetadata represents the root element of the rs-metadata XML document
type RSMetadata struct {
	XMLName               xml.Name        `xml:"urn:ietf:params:xml:ns:recording:1 recording"`
	SessionID             string          `xml:"session,attr"`
	State                 string          `xml:"state,attr"`
	Group                 []Group         `xml:"group"`
	Participants          []RSParticipant `xml:"participant"`
	Streams               []Stream        `xml:"stream"`
	SessionRecordingAssoc RSAssociation   `xml:"sessionrecordingassoc"`
}

type Group struct {
	ID              string   `xml:"id,attr"`
	ParticipantRefs []string `xml:"participantsessionassoc"`
}

type RSParticipant struct {
	ID     string `xml:"id,attr"`
	NameID string `xml:"nameID,attr,omitempty"`
	Name   string `xml:"name,omitempty"`
	Aor    []Aor  `xml:"aor"`
}

type Aor struct {
	Value string `xml:",chardata"`
}

type Stream struct {
	Label    string `xml:"label,attr"`
	StreamID string `xml:"streamid,attr"`
}

type RSAssociation struct {
	SessionID string `xml:"sessionid,attr"`
}

// ParseSiprecInvite extracts SDP and rs-metadata from a SIPREC INVITE request
func ParseSiprecInvite(req *sip.Request) (sdp string, metadata *RSMetadata, err error) {
	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return "", nil, errors.New("missing Content-Type header")
	}

	// Check for multipart MIME content
	mediaType, params, err := mime.ParseMediaType(contentType.Value())
	if err != nil {
		return "", nil, fmt.Errorf("invalid Content-Type header: %w", err)
	}

	if !strings.HasPrefix(mediaType, "multipart/") {
		return "", nil, errors.New("not a multipart MIME message")
	}

	boundary, ok := params["boundary"]
	if !ok {
		return "", nil, errors.New("no boundary parameter in Content-Type")
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

		contentType := part.Header.Get("Content-Type")
		if contentType == "application/sdp" {
			// Read SDP content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return "", nil, fmt.Errorf("error reading SDP part: %w", err)
			}
			sdpContent = buf.String()
		} else if contentType == "application/rs-metadata+xml" {
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
		return "", nil, errors.New("no SDP content found in multipart message")
	}
	if metadataContent == "" {
		return "", nil, errors.New("no rs-metadata content found in multipart message")
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
	// Update the metadata to create a response
	// Here we'd typically keep the same sessionID but might need to update state or other attributes
	metadata.State = "active"

	// Marshal back to XML
	metadataBytes, err := xml.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshaling response metadata: %w", err)
	}

	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	return xmlHeader + string(metadataBytes), nil
}

// CreateMultipartResponse creates a multipart MIME response with SDP and rs-metadata
func CreateMultipartResponse(sdp string, metadata string) (string, string) {
	boundary := "boundary_" + uuid.New().String()

	multipartContent := fmt.Sprintf(
		`--%s
Content-Type: application/sdp
Content-Disposition: session;handling=required

%s
--%s
Content-Type: application/rs-metadata+xml
Content-Disposition: recording-session

%s
--%s--
`, boundary, sdp, boundary, metadata, boundary)

	contentType := `multipart/mixed;boundary="` + boundary + `"`

	return contentType, multipartContent
}

// UpdateRecordingSession updates an existing recording session with new metadata
func UpdateRecordingSession(existing *RecordingSession, rsMetadata *RSMetadata) {
	// Update recording state if changed
	if rsMetadata.State != "" {
		existing.RecordingState = rsMetadata.State
	}

	// Update participant information
	existing.Participants = updateParticipants(existing.Participants, rsMetadata.Participants)

	// Update sequence number
	existing.SequenceNumber++

	// Update associated time
	existing.AssociatedTime = time.Now()
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
				DisplayName: np.NameID,
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

// DetectParticipantChanges analyzes metadata to identify participant changes
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
			// This is a new participant
			participant := Participant{
				ID:          newP.ID,
				Name:        newP.Name,
				DisplayName: newP.NameID,
			}

			// Add communication IDs
			for _, aor := range newP.Aor {
				participant.CommunicationIDs = append(participant.CommunicationIDs, CommunicationID{
					Type:  "sip",
					Value: aor.Value,
				})
			}

			added = append(added, participant)
		}
	}

	// Find removed participants
	for id, existingP := range existingMap {
		if _, exists := newMap[id]; !exists {
			// This participant was removed
			removed = append(removed, existingP)
		}
	}

	// Find modified participants
	for id, existingP := range existingMap {
		if newP, exists := newMap[id]; exists {
			// Check if anything changed
			changed := false

			if newP.Name != "" && newP.Name != existingP.Name {
				changed = true
			}

			if newP.NameID != "" && newP.NameID != existingP.DisplayName {
				changed = true
			}

			// Check for AOR changes
			if len(newP.Aor) > 0 {
				// Create maps of AOR values for comparison
				existingAors := make(map[string]bool)
				for _, comm := range existingP.CommunicationIDs {
					existingAors[comm.Value] = true
				}

				newAors := make(map[string]bool)
				for _, aor := range newP.Aor {
					newAors[aor.Value] = true
				}

				// Check if AORs are different
				aorChanged := false

				// Check if any new AORs are missing from existing
				for aorVal := range newAors {
					if !existingAors[aorVal] {
						aorChanged = true
						break
					}
				}

				// Check if any existing AORs are missing from new
				if !aorChanged {
					for aorVal := range existingAors {
						if !newAors[aorVal] {
							aorChanged = true
							break
						}
					}
				}

				if aorChanged {
					changed = true
				}
			}

			if changed {
				// Create the updated participant
				participant := existingP // Start with existing

				if newP.Name != "" {
					participant.Name = newP.Name
				}

				if newP.NameID != "" {
					participant.DisplayName = newP.NameID
				}

				// Update communication IDs if provided
				if len(newP.Aor) > 0 {
					newCommunicationIDs := []CommunicationID{}
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

// Helper function to validate SIPREC message
func ValidateSiprecMessage(rsMetadata *RSMetadata) []string {
	deficiencies := []string{}

	// Check for required fields
	if rsMetadata.SessionID == "" {
		deficiencies = append(deficiencies, "missing recording session ID")
	}

	// Check for recording state
	if rsMetadata.State == "" {
		deficiencies = append(deficiencies, "missing recording state")
	}

	return deficiencies
}

// ConvertRSParticipantToParticipant converts RSParticipant to Participant
func ConvertRSParticipantToParticipant(p RSParticipant) Participant {
	participant := Participant{
		ID:          p.ID,
		Name:        p.Name,
		DisplayName: p.NameID,
	}

	// Add communication IDs
	for _, aor := range p.Aor {
		participant.CommunicationIDs = append(participant.CommunicationIDs, CommunicationID{
			Type:  "sip",
			Value: aor.Value,
		})
	}

	return participant
}

// Log recording session details
func LogRecordingSession(session *RecordingSession, logger *logrus.Logger) {
	logger.WithFields(logrus.Fields{
		"session_id":        session.ID,
		"state":             session.RecordingState,
		"sequence":          session.SequenceNumber,
		"participant_count": len(session.Participants),
	}).Info("Recording session details")

	// Log participant details at debug level
	for i, p := range session.Participants {
		logger.WithFields(logrus.Fields{
			"index":        i,
			"id":           p.ID,
			"name":         p.Name,
			"display_name": p.DisplayName,
			"comms_count":  len(p.CommunicationIDs),
		}).Debug("Participant details")
	}
}
