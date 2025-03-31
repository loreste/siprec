package errors

import (
	"encoding/json"
	"errors"
	"net/http"
)

// HTTP status code mappings
var errorStatusCodes = map[error]int{
	ErrNotFound:           http.StatusNotFound,
	ErrInvalidInput:       http.StatusBadRequest,
	ErrInternalError:      http.StatusInternalServerError,
	ErrNotImplemented:     http.StatusNotImplemented,
	ErrTimeout:            http.StatusGatewayTimeout,
	ErrUnavailable:        http.StatusServiceUnavailable,
	ErrAlreadyExists:      http.StatusConflict,
	ErrPermissionDenied:   http.StatusForbidden,
	ErrUnauthenticated:    http.StatusUnauthorized,
	ErrResourceExhausted:  http.StatusTooManyRequests,
	ErrFailedPrecondition: http.StatusPreconditionFailed,
	ErrAborted:            http.StatusConflict,
	ErrCanceled:           http.StatusRequestTimeout,
	
	// Domain-specific error mappings
	ErrInvalidSIPMessage:   http.StatusBadRequest,
	ErrInvalidSDP:          http.StatusBadRequest,
	ErrSessionNotFound:     http.StatusNotFound,
	ErrSessionAlreadyExist: http.StatusConflict,
	ErrMediaFailure:        http.StatusInternalServerError,
	ErrTranscriptionFailed: http.StatusInternalServerError,
	ErrInvalidMetadata:     http.StatusBadRequest,
	ErrNetworkFailure:      http.StatusBadGateway,
	ErrRedundancyFailure:   http.StatusInternalServerError,
}

// WriteError writes a standardized error response to the HTTP response writer
func WriteError(w http.ResponseWriter, err error) {
	// Extract structured error if possible
	var statusCode int
	var response map[string]interface{}
	
	// Check if it's our custom error
	var serr *Error
	if err == nil {
		// Handle nil error case
		statusCode = http.StatusInternalServerError
		response = map[string]interface{}{
			"error": "Unknown error",
		}
	} else if errors.As(err, &serr) {
		// Use structured error details
		statusCode = HTTPStatusFromError(serr.original)
		response = serr.AsJSON()
	} else {
		// Simple error
		statusCode = HTTPStatusFromError(err)
		response = map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	// Set content type and status code
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	// Write the response
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(response)
}

// HTTPStatusFromError determines the appropriate HTTP status code for an error
func HTTPStatusFromError(err error) int {
	// Find the root error
	for err != nil {
		// Check if we have a direct mapping for this error
		if code, ok := errorStatusCodes[err]; ok {
			return code
		}
		
		// Try unwrapping
		unwrapped := errors.Unwrap(err)
		if unwrapped == err || unwrapped == nil {
			break
		}
		err = unwrapped
	}
	
	// Default to internal server error
	return http.StatusInternalServerError
}

// Error code to HTTP status mapping
var errorCodeStatusMap = map[string]int{
	"NOT_FOUND":           http.StatusNotFound,
	"INVALID_INPUT":       http.StatusBadRequest,
	"INTERNAL_ERROR":      http.StatusInternalServerError,
	"NOT_IMPLEMENTED":     http.StatusNotImplemented,
	"TIMEOUT":             http.StatusGatewayTimeout,
	"UNAVAILABLE":         http.StatusServiceUnavailable,
	"ALREADY_EXISTS":      http.StatusConflict,
	"PERMISSION_DENIED":   http.StatusForbidden,
	"UNAUTHENTICATED":     http.StatusUnauthorized,
	"RESOURCE_EXHAUSTED":  http.StatusTooManyRequests,
	"FAILED_PRECONDITION": http.StatusPreconditionFailed,
	"ABORTED":             http.StatusConflict,
	"CANCELED":            http.StatusRequestTimeout,
	"SESSION_NOT_FOUND":   http.StatusNotFound,
	"INVALID_SIP_MESSAGE": http.StatusBadRequest,
	"INVALID_METADATA":    http.StatusBadRequest,
}