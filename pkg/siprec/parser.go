package siprec

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
)

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
// Enhanced for RFC compliance with proper Content-Disposition values
func CreateMultipartResponse(sdp string, metadata string) (string, string) {
	// Generate a boundary that is unlikely to appear in the content
	boundary := "boundary_" + uuid.New().String()

	// Format the multipart content with proper MIME headers
	// Note: The handling=required parameter ensures the receiver properly processes the content
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
`, 
		boundary, sdp, boundary, metadata, boundary)

	// Create the Content-Type header with proper boundary parameter
	contentType := `multipart/mixed;boundary="` + boundary + `"`

	return contentType, multipartContent
}

// ExtractRSMetadataFromRequest extracts SIPREC metadata from a SIP request
// Enhanced with better error handling and robustness
func ExtractRSMetadataFromRequest(req *sip.Request) (*RSMetadata, error) {
	if req == nil {
		return nil, pkg_errors.NewInvalidInput("request is nil").
			WithCode("EXTRACT_METADATA_FAILED")
	}
	
	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return nil, pkg_errors.NewInvalidInput("missing Content-Type header").
			WithCode("MISSING_HEADER").
			WithField("header", "Content-Type")
	}

	// Check for multipart MIME content which typically contains rs-metadata
	mediaType, params, err := mime.ParseMediaType(contentType.Value())
	if err != nil {
		return nil, pkg_errors.Wrap(err, "invalid Content-Type header").
			WithCode("INVALID_CONTENT_TYPE").
			WithField("content_type", contentType.Value())
	}

	// If not multipart, can't contain SIPREC metadata
	if !strings.HasPrefix(mediaType, "multipart/") {
		return nil, pkg_errors.NewInvalidInput("not a multipart MIME message").
			WithCode("NOT_MULTIPART").
			WithField("media_type", mediaType)
	}

	boundary, ok := params["boundary"]
	if !ok {
		return nil, pkg_errors.NewInvalidInput("no boundary parameter in Content-Type").
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
			return nil, pkg_errors.Wrap(err, "error reading multipart").
				WithCode("MULTIPART_PARSE_ERROR")
		}

		partContentType := part.Header.Get("Content-Type")
		partDisposition := part.Header.Get("Content-Disposition")
		
		// Check for SDP part - we need to track if it's present for a valid SIPREC request
		if partContentType == "application/sdp" {
			sdpFound = true
		} else if partContentType == "application/rs-metadata+xml" {
			// Read rs-metadata content
			buf := new(strings.Builder)
			_, err = io.Copy(buf, part)
			if err != nil {
				return nil, pkg_errors.Wrap(err, "error reading rs-metadata part").
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
		return nil, pkg_errors.NewInvalidInput("no rs-metadata content found in multipart message").
			WithCode("MISSING_METADATA")
	}
	
	if !sdpFound {
		// This is just a warning since we might still be able to process some SIPREC operations without SDP
		fmt.Println("Warning: SIPREC request missing SDP part")
	}

	// Parse rs-metadata XML
	var rsMetadata RSMetadata
	err = xml.Unmarshal([]byte(metadataContent), &rsMetadata)
	if err != nil {
		return nil, pkg_errors.Wrap(err, "error parsing rs-metadata XML").
			WithCode("XML_PARSE_ERROR").
			WithField("xml_content", metadataContent[:min(len(metadataContent), 100)]+"...") // Truncate to avoid huge errors
	}

	// Comprehensive validation
	deficiencies := ValidateSiprecMessage(&rsMetadata)
	if len(deficiencies) > 0 {
		// If there are critical deficiencies, return an error
		for _, deficiency := range deficiencies {
			if strings.Contains(deficiency, "missing session ID") || 
			   strings.Contains(deficiency, "missing recording state") ||
			   strings.Contains(deficiency, "invalid recording state") {
				return nil, pkg_errors.NewInvalidInput(fmt.Sprintf("critical metadata validation failure: %v", deficiencies)).
					WithCode("INVALID_METADATA").
					WithFields(map[string]interface{}{
						"deficiencies": deficiencies,
						"session_id": rsMetadata.SessionID,
					})
			}
		}
		
		// For non-critical issues, just return the metadata with a warning
		return &rsMetadata, fmt.Errorf("metadata validation warnings: %v", deficiencies)
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
		return nil, errors.New("no boundary parameter in Content-Type")
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

		contentType := part.Header.Get("Content-Type")
		if contentType == "application/rs-metadata+xml" {
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
		return nil, errors.New("no rs-metadata content found in multipart message")
	}

	// Parse rs-metadata XML
	var rsMetadata RSMetadata
	err = xml.Unmarshal([]byte(metadataContent), &rsMetadata)
	if err != nil {
		return nil, fmt.Errorf("error parsing rs-metadata XML: %w", err)
	}

	return &rsMetadata, nil
}

// ValidateSiprecMessage performs a comprehensive validation of a SIPREC message
// Returns a list of deficiencies found in the message
func ValidateSiprecMessage(rsMetadata *RSMetadata) []string {
	if rsMetadata == nil {
		return []string{"metadata is nil"}
	}
	
	deficiencies := []string{}

	// Check for required fields
	if rsMetadata.SessionID == "" {
		deficiencies = append(deficiencies, "missing recording session ID")
	} else if len(rsMetadata.SessionID) > 255 {
		// RFC 7866 compliance - check for overly long IDs
		deficiencies = append(deficiencies, "session ID exceeds maximum allowed length")
	}

	// Check for recording state
	if rsMetadata.State == "" {
		deficiencies = append(deficiencies, "missing recording state")
	} else {
		// Validate that state is one of the allowed values
		validStates := map[string]bool{
			"active":     true,
			"paused":     true,
			"inactive":   true,
			"terminated": true,
		}
		
		if !validStates[rsMetadata.State] {
			deficiencies = append(deficiencies, fmt.Sprintf("invalid recording state: %s", rsMetadata.State))
		}
		
		// If state is terminated, reason should be provided
		if rsMetadata.State == "terminated" && rsMetadata.Reason == "" {
			deficiencies = append(deficiencies, "termination reason not provided")
		}
	}

	// Validate sequence number for state changes
	if rsMetadata.Sequence < 0 {
		deficiencies = append(deficiencies, "invalid sequence number")
	}

	// Validate association information
	if (rsMetadata.SessionRecordingAssoc == RSAssociation{}) {
		deficiencies = append(deficiencies, "missing session recording association")
	} else if rsMetadata.SessionRecordingAssoc.SessionID == "" {
		deficiencies = append(deficiencies, "missing session ID in recording association")
	}
	
	// Validate additional association fields for compliance
	if rsMetadata.SessionRecordingAssoc.CallID == "" && rsMetadata.SessionRecordingAssoc.FixedID == "" {
		deficiencies = append(deficiencies, "session association missing both call-ID and fixed-ID")
	}

	// Check for participants
	if len(rsMetadata.Participants) == 0 {
		deficiencies = append(deficiencies, "no participants defined")
	} else {
		// Validate participant information
		for i, participant := range rsMetadata.Participants {
			if participant.ID == "" {
				deficiencies = append(deficiencies, fmt.Sprintf("participant %d missing ID", i))
			}
			
			// Check if participant has any identifiers
			if len(participant.Aor) == 0 {
				deficiencies = append(deficiencies, fmt.Sprintf("participant %s has no address of record", participant.ID))
			}
			
			// Validate AOR URIs
			for j, aor := range participant.Aor {
				if aor.Value == "" {
					deficiencies = append(deficiencies, fmt.Sprintf("participant %s has empty AOR at index %d", participant.ID, j))
				}
				
				// Basic URI validation if URI format is specified
				if aor.URI != "" && !strings.Contains(aor.URI, ":") {
					deficiencies = append(deficiencies, fmt.Sprintf("participant %s has invalid URI format for AOR", participant.ID))
				}
			}
			
			// Validate role if provided
			if participant.Role != "" {
				validRoles := map[string]bool{
					"active":  true,
					"passive": true,
					"focus":   true,
					"mixer":   true,
				}
				
				if !validRoles[participant.Role] {
					deficiencies = append(deficiencies, fmt.Sprintf("participant %s has invalid role: %s", participant.ID, participant.Role))
				}
			}
		}
	}
	
	// Validate stream information if present
	for i, stream := range rsMetadata.Streams {
		if stream.Label == "" {
			deficiencies = append(deficiencies, fmt.Sprintf("stream at index %d missing label", i))
		}
		
		if stream.StreamID == "" {
			deficiencies = append(deficiencies, fmt.Sprintf("stream with label %s missing stream ID", stream.Label))
		}
		
		// Validate stream type if provided
		if stream.Type != "" {
			validTypes := map[string]bool{
				"audio": true,
				"video": true,
				"text":  true,
				"message": true,
				"application": true,
			}
			
			if !validTypes[stream.Type] {
				deficiencies = append(deficiencies, fmt.Sprintf("stream %s has invalid type: %s", stream.Label, stream.Type))
			}
		}
		
		// For mixed streams, validate mixing information
		if stream.Mode == "mixed" && len(stream.Mixing.MixedStreams) == 0 {
			deficiencies = append(deficiencies, fmt.Sprintf("mixed stream %s has no source streams defined", stream.Label))
		}
	}

	return deficiencies
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
			},
		},
	}
	metadata.Participants = append(metadata.Participants, minimalParticipant)
	
	return metadata
}