# SIPREC Error Handling Package

This package provides a structured error handling system for the SIPREC server with context-rich error data, stack traces, error codes, and consistent HTTP response handling.

## Key Features

- **Structured errors** with contextual data
- **Error codes** for categorization
- **Location information** (file:line) for easier debugging
- **Error wrapping** with the standard Go errors package
- **HTTP status code mapping** for RESTful APIs
- **Field annotations** for structured logging

## Usage Examples

### Creating New Errors

```go
// Simple error
err := errors.New("resource not found")

// With fields
err := errors.New("session expired").WithField("session_id", sessionID)

// With multiple fields
err := errors.New("authentication failed").WithFields(map[string]interface{}{
    "user_id": userID,
    "source_ip": ipAddress,
})

// With error code
err := errors.New("network timeout").WithCode("NETWORK_ERROR")
```

### Wrapping Errors

```go
// Wrap an existing error with additional context
originalErr := doSomething()
if originalErr != nil {
    return errors.Wrap(originalErr, "failed during processing").
        WithField("resource_id", id)
}
```

### Using Domain-Specific Error Types

```go
// For not found errors
if session == nil {
    return errors.NewSessionNotFound(sessionID)
}

// For invalid input
if !validFormat(input) {
    return errors.NewInvalidInput("input format is invalid").
        WithField("provided", input)
}

// For metadata validation
if metadata.State == "" {
    return errors.NewInvalidMetadata("missing state field").
        WithFields(map[string]interface{}{
            "session_id": metadata.SessionID, 
            "fields_present": []string{"id", "sequence"},
        })
}
```

### Error Handling and Type Checking

```go
err := doSomething()

// Check error type
if errors.IsErrorType(err, errors.ErrNotFound) {
    // Handle not found case
}

// Extract error code
if errors.GetErrorCode(err) == "NETWORK_ERROR" {
    // Special handling for network errors
}

// Extract context fields for logging
fields := errors.GetErrorFields(err)
logger.WithFields(fields).Error("Operation failed")

// Get the source location
loc := errors.GetErrorLocation(err)
fmt.Printf("Error occurred at %s\n", loc)
```

### HTTP Response Handling

```go
func HandleRequest(w http.ResponseWriter, r *http.Request) {
    result, err := processRequest(r)
    if err != nil {
        // Automatically maps error to appropriate HTTP status code
        // and writes a JSON response with error details
        errors.WriteError(w, err)
        return
    }
    
    // Success case
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}
```

## Error Categories

The package provides standard error types that can be used throughout the application:

### General Error Types

- `ErrNotFound` - Resource not found
- `ErrInvalidInput` - Invalid input provided
- `ErrInternalError` - Internal server error
- `ErrNotImplemented` - Feature not implemented
- `ErrTimeout` - Operation timed out
- `ErrUnavailable` - Service unavailable
- `ErrAlreadyExists` - Resource already exists
- `ErrPermissionDenied` - Permission denied
- `ErrUnauthenticated` - Unauthenticated request

### Domain-Specific Error Types

- `ErrInvalidSIPMessage` - Invalid SIP message
- `ErrInvalidSDP` - Invalid SDP message
- `ErrSessionNotFound` - Recording session not found
- `ErrSessionAlreadyExist` - Recording session already exists
- `ErrMediaFailure` - Media processing failure
- `ErrTranscriptionFailed` - Transcription failed
- `ErrInvalidMetadata` - Invalid metadata
- `ErrNetworkFailure` - Network failure
- `ErrRedundancyFailure` - Redundancy operation failed

## Error Codes

Error codes provide a way to categorize errors for easier handling:

- `NOT_FOUND` - Resource not found
- `INVALID_INPUT` - Invalid input provided
- `INTERNAL_ERROR` - Internal server error
- `SESSION_NOT_FOUND` - Recording session not found
- `INVALID_SIP_MESSAGE` - Invalid SIP message
- `INVALID_METADATA` - Invalid metadata
- `RECOVERY_FAILED` - Session recovery failed
- `CONTINUITY_VALIDATION_FAILED` - Session continuity validation failed

## Best Practices

1. **Be Descriptive**: Always include meaningful error messages
2. **Add Context**: Include relevant data with WithField/WithFields
3. **Use Codes**: Add error codes for programmatic handling
4. **Wrap When Needed**: Maintain error chains for debugging
5. **Check Types**: Use IsErrorType() for checking error types
6. **Log Structure**: Use GetErrorFields() to enhance log entries

## Integration with Logging

The error package is designed to work well with structured logging:

```go
if err := processSession(session); err != nil {
    // Get all error fields for structured logging
    fields := errors.GetErrorFields(err)
    
    // Add error location
    fields["error_location"] = errors.GetErrorLocation(err)
    
    // Add error code
    fields["error_code"] = errors.GetErrorCode(err)
    
    // Log with all context
    logger.WithFields(fields).Error(err.Error())
    
    // Return appropriate error
    return err
}
```