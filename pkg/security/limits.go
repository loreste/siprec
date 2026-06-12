package security

import (
	"fmt"
	"strings"
	"unicode"
)

// Size limits for various inputs to prevent DoS attacks
const (
	// SIP message size limits
	MaxSIPMessageSize = 64 * 1024 // 64KB for SIP messages
	MaxSIPHeaderSize  = 8 * 1024  // 8KB for headers
	MaxSIPBodySize    = 56 * 1024 // 56KB for body

	// SDP size limits
	MaxSDPSize = 16 * 1024 // 16KB for SDP

	// SIPREC metadata limits
	MaxMetadataSize  = 1024 * 1024     // 1MB for XML metadata
	MaxMultipartSize = 2 * 1024 * 1024 // 2MB for multipart bodies

	// Recording limits
	MaxRecordingFileSize = 2 * 1024 * 1024 * 1024 // 2GB max recording size

	// Network limits
	MaxUDPPacketSize = 65536      // Max UDP packet size
	MaxTCPBufferSize = 128 * 1024 // 128KB TCP buffer

	// Timeout limits
	DefaultRequestTimeout = 30  // 30 seconds
	MaxRequestTimeout     = 300 // 5 minutes
)

// ValidateSize checks if data size is within allowed limits
func ValidateSize(data []byte, maxSize int, description string) error {
	if len(data) > maxSize {
		return fmt.Errorf("%s size %d exceeds maximum allowed %d", description, len(data), maxSize)
	}
	return nil
}

// SanitizeCallUUID ensures a call UUID is safe for use in filenames
func SanitizeCallUUID(uuid string) string {
	// Keep only alphanumeric and dash characters
	var sanitized strings.Builder
	for _, r := range uuid {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			sanitized.WriteRune(r)
		}
	}

	result := sanitized.String()

	// Limit length
	if len(result) > 64 {
		result = result[:64]
	}

	// Ensure not empty
	if result == "" {
		result = "unknown"
	}

	return result
}
