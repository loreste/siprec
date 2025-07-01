package security

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// Size limits for various inputs to prevent DoS attacks
const (
	// SIP message size limits
	MaxSIPMessageSize = 64 * 1024  // 64KB for SIP messages
	MaxSIPHeaderSize  = 8 * 1024   // 8KB for headers
	MaxSIPBodySize    = 56 * 1024  // 56KB for body
	
	// SDP size limits
	MaxSDPSize = 16 * 1024 // 16KB for SDP
	
	// SIPREC metadata limits
	MaxMetadataSize    = 1024 * 1024 // 1MB for XML metadata
	MaxMultipartSize   = 2 * 1024 * 1024 // 2MB for multipart bodies
	
	// Recording limits
	MaxRecordingFileSize = 2 * 1024 * 1024 * 1024 // 2GB max recording size
	MaxFileNameLength    = 255 // Max filename length
	
	// Network limits
	MaxUDPPacketSize = 65536 // Max UDP packet size
	MaxTCPBufferSize = 128 * 1024 // 128KB TCP buffer
	
	// Timeout limits
	DefaultRequestTimeout = 30 // 30 seconds
	MaxRequestTimeout     = 300 // 5 minutes
)

// ValidateSize checks if data size is within allowed limits
func ValidateSize(data []byte, maxSize int, description string) error {
	if len(data) > maxSize {
		return fmt.Errorf("%s size %d exceeds maximum allowed %d", description, len(data), maxSize)
	}
	return nil
}

// SanitizeFilePath sanitizes a file path to prevent directory traversal attacks
func SanitizeFilePath(path string) (string, error) {
	// Remove any null bytes
	path = strings.ReplaceAll(path, "\x00", "")
	
	// Clean the path
	cleaned := filepath.Clean(path)
	
	// Ensure the path doesn't contain any parent directory references
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("invalid path: contains parent directory reference")
	}
	
	// Remove any leading slashes or drive letters
	cleaned = strings.TrimLeft(cleaned, "/\\")
	if len(cleaned) > 1 && cleaned[1] == ':' {
		cleaned = cleaned[2:] // Remove Windows drive letter
	}
	
	// Validate filename length
	if len(filepath.Base(cleaned)) > MaxFileNameLength {
		return "", fmt.Errorf("filename too long: %d characters (max %d)", len(filepath.Base(cleaned)), MaxFileNameLength)
	}
	
	// Ensure only safe characters in filename
	base := filepath.Base(cleaned)
	for _, r := range base {
		if !isValidFileNameChar(r) {
			return "", fmt.Errorf("invalid character in filename: %c", r)
		}
	}
	
	return cleaned, nil
}

// isValidFileNameChar checks if a rune is valid for a filename
func isValidFileNameChar(r rune) bool {
	// Allow alphanumeric, dash, underscore, and dot
	return unicode.IsLetter(r) || unicode.IsDigit(r) || 
		r == '-' || r == '_' || r == '.' || r == ' '
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