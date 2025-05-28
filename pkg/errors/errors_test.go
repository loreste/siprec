package errors

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	err := New("test error")
	if err == nil {
		t.Fatal("New() returned nil")
	}

	if !strings.Contains(err.Error(), "test error") {
		t.Errorf("Expected error message to contain 'test error', got: %s", err.Error())
	}

	if err.Location() == "" {
		t.Error("Location should not be empty")
	}
}

func TestWrap(t *testing.T) {
	baseErr := errors.New("base error")
	err := Wrap(baseErr, "wrapped")

	if err == nil {
		t.Fatal("Wrap() returned nil")
	}

	if !strings.Contains(err.Error(), "wrapped") {
		t.Errorf("Expected error message to contain 'wrapped', got: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "base error") {
		t.Errorf("Expected error message to contain 'base error', got: %s", err.Error())
	}

	// Test unwrapping
	unwrapped := errors.Unwrap(err)
	if unwrapped != baseErr {
		t.Errorf("Unwrap() returned wrong error: %v", unwrapped)
	}
}

func TestWithField(t *testing.T) {
	err := New("test error").WithField("key", "value")

	fields := err.GetFields()
	if len(fields) != 1 {
		t.Fatalf("Expected 1 field, got %d", len(fields))
	}

	if fields["key"] != "value" {
		t.Errorf("Expected field['key'] = 'value', got: %v", fields["key"])
	}
}

func TestWithFields(t *testing.T) {
	fields := map[string]interface{}{
		"key1": "value1",
		"key2": 123,
	}

	err := New("test error").WithFields(fields)

	errFields := err.GetFields()
	if len(errFields) != 2 {
		t.Fatalf("Expected 2 fields, got %d", len(errFields))
	}

	if errFields["key1"] != "value1" {
		t.Errorf("Expected field['key1'] = 'value1', got: %v", errFields["key1"])
	}

	if errFields["key2"] != 123 {
		t.Errorf("Expected field['key2'] = 123, got: %v", errFields["key2"])
	}
}

func TestWithCode(t *testing.T) {
	err := New("test error").WithCode("TEST_CODE")

	if err.GetCode() != "TEST_CODE" {
		t.Errorf("Expected code 'TEST_CODE', got: %s", err.GetCode())
	}
}

func TestErrorIs(t *testing.T) {
	// Test with standard errors
	notFoundErr := NewNotFound("resource not found")
	if !errors.Is(notFoundErr, ErrNotFound) {
		t.Error("errors.Is() should return true for ErrNotFound")
	}

	// Test with wrapped errors
	wrapped := Wrap(ErrInvalidInput, "wrapped invalid input")
	if !errors.Is(wrapped, ErrInvalidInput) {
		t.Error("errors.Is() should return true for wrapped ErrInvalidInput")
	}
}

func TestErrorAs(t *testing.T) {
	err := New("test error").WithCode("TEST_CODE")

	var structErr *Error
	if !errors.As(err, &structErr) {
		t.Error("errors.As() should successfully cast to *Error")
	}

	if structErr.GetCode() != "TEST_CODE" {
		t.Errorf("Expected code 'TEST_CODE', got: %s", structErr.GetCode())
	}
}

func TestHelperFunctions(t *testing.T) {
	// Test IsErrorType
	notFoundErr := NewNotFound("resource not found")
	if !IsErrorType(notFoundErr, ErrNotFound) {
		t.Error("IsErrorType() should return true for ErrNotFound")
	}

	// Test GetErrorCode
	codeErr := New("test error").WithCode("TEST_CODE")
	if GetErrorCode(codeErr) != "TEST_CODE" {
		t.Errorf("GetErrorCode() should return 'TEST_CODE', got: %s", GetErrorCode(codeErr))
	}

	// Test GetErrorFields
	fieldsErr := New("test error").WithField("key", "value")
	fields := GetErrorFields(fieldsErr)
	if fields == nil || fields["key"] != "value" {
		t.Error("GetErrorFields() should return the error fields")
	}

	// Test GetErrorLocation
	locErr := New("test error")
	if GetErrorLocation(locErr) == "" {
		t.Error("GetErrorLocation() should return a non-empty string")
	}
}

func TestHTTPStatusFromError(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{"NotFound", ErrNotFound, http.StatusNotFound},
		{"InvalidInput", ErrInvalidInput, http.StatusBadRequest},
		{"Wrapped", Wrap(ErrNotFound, "wrapped"), http.StatusNotFound},
		{"Unknown", errors.New("unknown"), http.StatusInternalServerError},
		{"SessionNotFound", NewSessionNotFound("123"), http.StatusNotFound},
		{"InvalidSIP", NewInvalidSIP("bad format"), http.StatusBadRequest},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			status := HTTPStatusFromError(tc.err)
			if status != tc.expectedStatus {
				t.Errorf("Expected status %d, got: %d", tc.expectedStatus, status)
			}
		})
	}
}

func TestWriteError(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "StructuredError",
			err:            New("test error").WithField("key", "value").WithCode("TEST_CODE"),
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   `"message"`,
		},
		{
			name:           "StandardError",
			err:            ErrNotFound,
			expectedStatus: http.StatusNotFound,
			expectedBody:   `"error": "resource not found"`,
		},
		{
			name:           "SessionNotFound",
			err:            NewSessionNotFound("123"),
			expectedStatus: http.StatusNotFound,
			expectedBody:   `"session_id": "123"`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, tc.err)

			// Check status code
			if rec.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got: %d", tc.expectedStatus, rec.Code)
			}

			// Check content type
			contentType := rec.Header().Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("Expected Content-Type 'application/json', got: %s", contentType)
			}

			// Check response body contains expected strings
			body := rec.Body.String()
			if !strings.Contains(body, tc.expectedBody) {
				t.Errorf("Expected body to contain '%s', got: %s", tc.expectedBody, body)
			}
		})
	}
}
