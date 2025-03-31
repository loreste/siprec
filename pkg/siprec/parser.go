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

// ValidateSiprecMessage checks if a SIPREC message has all required fields
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