// Package test_siprec provides a test for the SIPREC implementation
// To use this test, rename the package to main and run with go run test_siprec.go
package test_siprec

import (
	"encoding/xml"
	"fmt"
	"time"
	
	"siprec-server/pkg/siprec"
)

// TestSiprec tests the SIPREC implementation
func TestSiprec() {
	fmt.Println("Testing enhanced SIPREC implementation")
	
	// Create a test RSMetadata
	metadata := &siprec.RSMetadata{
		XMLName:   xml.Name{Local: "recording", Space: "urn:ietf:params:xml:ns:recording:1"},
		SessionID: "test-session-123",
		State:     "active",
		Reason:    "Test recording",
		Sequence:  1,
		Direction: "inbound",
		Participants: []siprec.RSParticipant{
			{
				ID:         "p1",
				NameID:     "Alice",
				Role:       "active",
				DisplayName: "Alice Smith",
				Aor: []siprec.Aor{
					{
						Value:   "sip:alice@example.com",
						Display: "Alice",
					},
				},
			},
			{
				ID:         "p2",
				NameID:     "Bob",
				Role:       "passive",
				DisplayName: "Bob Jones",
				Aor: []siprec.Aor{
					{
						Value:   "sip:bob@example.com",
						Display: "Bob",
					},
				},
			},
		},
		SessionRecordingAssoc: siprec.RSAssociation{
			SessionID: "test-session-123",
			CallID:    "call-123",
		},
	}
	
	// Create a test RecordingSession
	recordingSession := &siprec.RecordingSession{
		ID:              "test-session-123",
		SIPID:           "call-123",
		RecordingState:  "active",
		Direction:       "inbound",
		SequenceNumber:  1,
		Reason:          "Test recording",
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		StartTime:       time.Now(),
		IsValid:         true,
	}
	
	// Test validator
	deficiencies := siprec.ValidateSiprecMessage(metadata)
	if len(deficiencies) > 0 {
		fmt.Println("Validation deficiencies:", deficiencies)
	} else {
		fmt.Println("Metadata validation passed")
	}
	
	// Test state change handling
	err := siprec.HandleSiprecStateChange(recordingSession, "paused", "User paused recording")
	if err != nil {
		fmt.Println("State change error:", err)
	} else {
		fmt.Println("State changed to:", recordingSession.RecordingState)
		fmt.Println("Reason:", recordingSession.Reason)
	}
	
	// Test error response generation
	errorResponse := siprec.GenerateErrorResponse(400, "Invalid request format", "test-session-456")
	if errorResponse != nil {
		fmt.Printf("Generated error response with session ID: %s, state: %s, reason: %s\n", 
			errorResponse.SessionID, errorResponse.State, errorResponse.Reason)
	}
	
	// Test non-SIPREC error response
	nonSiprecError := siprec.GenerateNonSiprecErrorResponse()
	if nonSiprecError != nil {
		fmt.Printf("Generated non-SIPREC error with reason: %s\n", nonSiprecError.Reason)
	}
	
	// Test is session expired
	if siprec.IsSessionExpired(recordingSession) {
		fmt.Println("Session is expired")
	} else {
		fmt.Println("Session is not expired")
	}
	
	fmt.Println("SIPREC tests completed successfully")
}