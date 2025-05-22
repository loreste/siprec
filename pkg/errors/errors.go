package errors

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
)

// Standard error types that can be used throughout the application
var (
	// Standard error sentinel values
	ErrNotFound           = errors.New("resource not found")
	ErrInvalidInput       = errors.New("invalid input")
	ErrInternalError      = errors.New("internal error")
	ErrNotImplemented     = errors.New("not implemented")
	ErrTimeout            = errors.New("operation timed out")
	ErrUnavailable        = errors.New("service unavailable")
	ErrAlreadyExists      = errors.New("resource already exists")
	ErrPermissionDenied   = errors.New("permission denied")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrResourceExhausted  = errors.New("resource exhausted")
	ErrFailedPrecondition = errors.New("failed precondition")
	ErrAborted            = errors.New("operation aborted")
	ErrCanceled           = errors.New("operation canceled")

	// Domain-specific error sentinel values
	ErrInvalidSIPMessage   = errors.New("invalid SIP message")
	ErrInvalidSDP          = errors.New("invalid SDP message")
	ErrSessionNotFound     = errors.New("recording session not found")
	ErrSessionAlreadyExist = errors.New("recording session already exists")
	ErrMediaFailure        = errors.New("media processing failure")
	ErrTranscriptionFailed = errors.New("transcription failed")
	ErrInvalidMetadata     = errors.New("invalid metadata")
	ErrNetworkFailure      = errors.New("network failure")
	ErrRedundancyFailure   = errors.New("redundancy operation failed")
)

// Error represents a structured error with stack trace and additional context
type Error struct {
	// original is the underlying error
	original error

	// message is the error message
	message string

	// fields contains contextual information
	fields map[string]interface{}

	// stackPC is the program counter for the error's creation
	stackPC uintptr

	// file and line record where the error was created
	file string
	line int

	// Code is an optional error code for categorization
	Code string
}

// New creates a new structured error with the given message
func New(message string, fields ...map[string]interface{}) *Error {
	pc, file, line, _ := runtime.Caller(1)

	var fieldMap map[string]interface{}
	if len(fields) > 0 && fields[0] != nil {
		fieldMap = fields[0]
	} else {
		fieldMap = make(map[string]interface{})
	}

	return &Error{
		original: errors.New(message),
		message:  message,
		fields:   fieldMap,
		stackPC:  pc,
		file:     file,
		line:     line,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(err error, message string, fields ...map[string]interface{}) *Error {
	if err == nil {
		return nil
	}

	pc, file, line, _ := runtime.Caller(1)

	var fieldMap map[string]interface{}
	if len(fields) > 0 && fields[0] != nil {
		fieldMap = fields[0]
	} else {
		fieldMap = make(map[string]interface{})
	}

	return &Error{
		original: err,
		message:  message,
		fields:   fieldMap,
		stackPC:  pc,
		file:     file,
		line:     line,
	}
}

// WithField adds a single field to the error context
func (e *Error) WithField(key string, value interface{}) *Error {
	if e == nil {
		return nil
	}

	// Create a copy to avoid modifying the original
	result := &Error{
		original: e.original,
		message:  e.message,
		fields:   make(map[string]interface{}, len(e.fields)+1),
		stackPC:  e.stackPC,
		file:     e.file,
		line:     e.line,
		Code:     e.Code,
	}

	// Copy existing fields
	for k, v := range e.fields {
		result.fields[k] = v
	}

	// Add new field
	result.fields[key] = value

	return result
}

// WithFields adds multiple fields to the error context
func (e *Error) WithFields(fields map[string]interface{}) *Error {
	if e == nil {
		return nil
	}

	// Create a copy to avoid modifying the original
	result := &Error{
		original: e.original,
		message:  e.message,
		fields:   make(map[string]interface{}, len(e.fields)+len(fields)),
		stackPC:  e.stackPC,
		file:     e.file,
		line:     e.line,
		Code:     e.Code,
	}

	// Copy existing fields
	for k, v := range e.fields {
		result.fields[k] = v
	}

	// Add new fields
	for k, v := range fields {
		result.fields[k] = v
	}

	return result
}

// WithCode adds an error code to the error
func (e *Error) WithCode(code string) *Error {
	if e == nil {
		return nil
	}

	result := &Error{
		original: e.original,
		message:  e.message,
		fields:   e.fields,
		stackPC:  e.stackPC,
		file:     e.file,
		line:     e.line,
		Code:     code,
	}

	return result
}

// Error implements the error interface
func (e *Error) Error() string {
	if e == nil || e.original == nil {
		return ""
	}

	if e.message == "" {
		return e.original.Error()
	}

	// Include both our message and the original error
	return fmt.Sprintf("%s: %v", e.message, e.original)
}

// Unwrap implements the errors.Unwrap interface
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.original
}

// Location returns the file:line where the error was created
func (e *Error) Location() string {
	if e == nil {
		return ""
	}

	// Extract just the filename without the full path
	parts := strings.Split(e.file, "/")
	filename := parts[len(parts)-1]

	return fmt.Sprintf("%s:%d", filename, e.line)
}

// GetFields returns the error's context fields
func (e *Error) GetFields() map[string]interface{} {
	if e == nil {
		return nil
	}
	return e.fields
}

// GetCode returns the error's code
func (e *Error) GetCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// Is reports whether any error in err's tree matches target.
// Implements the errors.Is interface.
func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}

	// Check if our original error matches the target
	if errors.Is(e.original, target) {
		return true
	}

	// Check if we ourselves match exactly
	return e == target
}

// AsJSON returns the error in JSON-friendly map format
func (e *Error) AsJSON() map[string]interface{} {
	if e == nil {
		return nil
	}

	result := map[string]interface{}{
		"message":  e.Error(),
		"location": e.Location(),
	}

	if e.Code != "" {
		result["code"] = e.Code
	}

	if len(e.fields) > 0 {
		result["context"] = e.fields
	}

	return result
}

// NewNotFound creates a new ErrNotFound error with additional context
func NewNotFound(message string, fields ...map[string]interface{}) *Error {
	err := New(message, fields...)
	return &Error{
		original: ErrNotFound,
		message:  message,
		fields:   err.fields,
		stackPC:  err.stackPC,
		file:     err.file,
		line:     err.line,
		Code:     "NOT_FOUND",
	}
}

// NewInvalidInput creates a new ErrInvalidInput error with additional context
func NewInvalidInput(message string, fields ...map[string]interface{}) *Error {
	err := New(message, fields...)
	return &Error{
		original: ErrInvalidInput,
		message:  message,
		fields:   err.fields,
		stackPC:  err.stackPC,
		file:     err.file,
		line:     err.line,
		Code:     "INVALID_INPUT",
	}
}

// NewInternalError creates a new ErrInternalError with additional context
func NewInternalError(message string, fields ...map[string]interface{}) *Error {
	err := New(message, fields...)
	return &Error{
		original: ErrInternalError,
		message:  message,
		fields:   err.fields,
		stackPC:  err.stackPC,
		file:     err.file,
		line:     err.line,
		Code:     "INTERNAL_ERROR",
	}
}

// NewSessionNotFound creates a new ErrSessionNotFound with additional context
func NewSessionNotFound(sessionID string, fields ...map[string]interface{}) *Error {
	fieldMap := make(map[string]interface{})
	if len(fields) > 0 && fields[0] != nil {
		fieldMap = fields[0]
	}
	fieldMap["session_id"] = sessionID

	pc, file, line, _ := runtime.Caller(1)

	return &Error{
		original: ErrSessionNotFound,
		message:  fmt.Sprintf("recording session not found: %s", sessionID),
		fields:   fieldMap,
		stackPC:  pc,
		file:     file,
		line:     line,
		Code:     "SESSION_NOT_FOUND",
	}
}

// NewInvalidSIP creates a new ErrInvalidSIPMessage with additional context
func NewInvalidSIP(details string, fields ...map[string]interface{}) *Error {
	fieldMap := make(map[string]interface{})
	if len(fields) > 0 && fields[0] != nil {
		fieldMap = fields[0]
	}

	pc, file, line, _ := runtime.Caller(1)

	return &Error{
		original: ErrInvalidSIPMessage,
		message:  fmt.Sprintf("invalid SIP message: %s", details),
		fields:   fieldMap,
		stackPC:  pc,
		file:     file,
		line:     line,
		Code:     "INVALID_SIP_MESSAGE",
	}
}

// NewInvalidMetadata creates a new ErrInvalidMetadata with additional context
func NewInvalidMetadata(details string, fields ...map[string]interface{}) *Error {
	fieldMap := make(map[string]interface{})
	if len(fields) > 0 && fields[0] != nil {
		fieldMap = fields[0]
	}

	pc, file, line, _ := runtime.Caller(1)

	return &Error{
		original: ErrInvalidMetadata,
		message:  fmt.Sprintf("invalid metadata: %s", details),
		fields:   fieldMap,
		stackPC:  pc,
		file:     file,
		line:     line,
		Code:     "INVALID_METADATA",
	}
}

// IsErrorType checks if an error is of a specific error type
func IsErrorType(err, target error) bool {
	return errors.Is(err, target)
}

// GetErrorCode extracts the error code from an error if it's a structured error
func GetErrorCode(err error) string {
	var serr *Error
	if errors.As(err, &serr) {
		return serr.GetCode()
	}
	return ""
}

// GetErrorFields extracts fields from an error if it's a structured error
func GetErrorFields(err error) map[string]interface{} {
	var serr *Error
	if errors.As(err, &serr) {
		return serr.GetFields()
	}
	return nil
}

// GetErrorLocation extracts location from an error if it's a structured error
func GetErrorLocation(err error) string {
	var serr *Error
	if errors.As(err, &serr) {
		return serr.Location()
	}
	return ""
}

// NewNotImplemented creates a new ErrNotImplemented error with additional context
func NewNotImplemented(message string, fields ...map[string]interface{}) *Error {
	err := New(message, fields...)
	return &Error{
		original: ErrNotImplemented,
		message:  message,
		fields:   err.fields,
		stackPC:  err.stackPC,
		file:     err.file,
		line:     err.line,
		Code:     "NOT_IMPLEMENTED",
	}
}
