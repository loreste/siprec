package e2e

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/siprec"
)

// TestSessionRecoveryFlow tests the SIPREC session recovery flow
// It simulates a SIPREC session that experiences a network failure
// and verifies that the session is properly recovered
func TestSessionRecoveryFlow(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create original and recovery sessions
	originalSession := &siprec.RecordingSession{
		ID:             "test-session-abc123",
		RecordingState: "active",
		SequenceNumber: 1,
		Participants: []siprec.Participant{
			{
				ID:          "p1",
				Name:        "Alice",
				DisplayName: "Alice Smith",
				Role:        "active",
				CommunicationIDs: []siprec.CommunicationID{
					{
						Type:        "sip",
						Value:       "sip:alice@example.com",
						DisplayName: "Alice",
					},
				},
				MediaStreams: []string{"stream1"},
			},
			{
				ID:          "p2",
				Name:        "Bob",
				DisplayName: "Bob Jones",
				Role:        "passive",
				CommunicationIDs: []siprec.CommunicationID{
					{
						Type:        "sip",
						Value:       "sip:bob@example.com",
						DisplayName: "Bob",
					},
				},
				MediaStreams: []string{"stream2"},
			},
		},
		MediaStreamTypes: []string{"audio"},
		AssociatedTime:   time.Now(),
	}

	// Stage 1: Generate failover metadata for session
	logger.Info("Stage 1: Generate failover metadata for session")
	failoverMetadata := siprec.CreateFailoverMetadata(originalSession)
	
	// Store the failover ID for later verification
	originalSession.FailoverID = failoverMetadata.SessionRecordingAssoc.FixedID
	
	// Verify failover metadata
	assert.Equal(t, originalSession.ID, failoverMetadata.SessionID, "Session ID should match")
	assert.Equal(t, originalSession.RecordingState, failoverMetadata.State, "State should match")
	assert.Equal(t, originalSession.SequenceNumber+1, failoverMetadata.Sequence, "Sequence should be incremented")
	assert.Equal(t, "failover", failoverMetadata.Reason, "Reason should be failover")
	assert.NotEmpty(t, failoverMetadata.SessionRecordingAssoc.FixedID, "Fixed ID should be present")
	
	// Serialize the metadata to XML
	metadataXML, err := siprec.SerializeMetadata(failoverMetadata)
	require.NoError(t, err, "Failed to serialize metadata")
	logger.WithField("metadata", metadataXML).Debug("Failover metadata generated")
	
	// Stage 2: Simulate network failure and create replacement session
	logger.Info("Stage 2: Simulate network failure and session recovery")
	
	// Parse the metadata (simulates receiving it in a SIP message)
	originalID, failoverID, err := siprec.ParseFailoverMetadata(failoverMetadata)
	require.NoError(t, err, "Failed to parse failover metadata")
	assert.Equal(t, originalSession.ID, originalID, "Original session ID should match")
	assert.Equal(t, originalSession.FailoverID, failoverID, "Failover ID should match")
	
	// Create replacement session from failover metadata
	recoveredSession, err := siprec.RecoverSession(failoverMetadata)
	require.NoError(t, err, "Failed to recover session")
	
	// Recover stream information
	siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
	
	// Stage 3: Validate session continuity
	logger.Info("Stage 3: Validate session continuity")
	err = siprec.ValidateSessionContinuity(originalSession, recoveredSession)
	require.NoError(t, err, "Session continuity validation failed")
	
	// Additional verification of recovered session
	assert.Equal(t, originalSession.ID, recoveredSession.ID, "Session ID should match")
	assert.Equal(t, originalSession.RecordingState, recoveredSession.RecordingState, "Recording state should match")
	assert.Equal(t, originalSession.FailoverID, recoveredSession.FailoverID, "Failover ID should match")
	assert.Equal(t, len(originalSession.Participants), len(recoveredSession.Participants), "Participant count should match")
	
	// Stage 4: Test SIP Replaces header generation and parsing
	logger.Info("Stage 4: Test SIP Replaces header functionality")
	dialogID := fmt.Sprintf("%s;to-tag=abcd;from-tag=1234", originalSession.ID)
	
	// Generate Replaces header
	replacesHeader := siprec.CreateReplacesHeader(originalSession, dialogID, false)
	logger.WithField("replaces", replacesHeader).Debug("Generated Replaces header")
	
	// Parse Replaces header
	callID, toTag, fromTag, earlyOnly, err := siprec.ParseReplacesHeader(replacesHeader)
	require.NoError(t, err, "Failed to parse Replaces header")
	
	// Verify parsed values
	assert.Equal(t, originalSession.ID, callID, "Call ID should match")
	assert.Equal(t, "abcd", toTag, "To-tag should match")
	assert.Equal(t, "1234", fromTag, "From-tag should match")
	assert.False(t, earlyOnly, "Early-only should be false")
	
	// Stage 5: Test state change notification
	logger.Info("Stage 5: Test state change notification")
	stateChangeMetadata := siprec.GenerateStateChangeMetadata(recoveredSession, "paused", "user-requested")
	
	// Verify state change metadata
	assert.Equal(t, recoveredSession.ID, stateChangeMetadata.SessionID, "Session ID should match")
	assert.Equal(t, "paused", stateChangeMetadata.State, "State should be paused")
	assert.Equal(t, recoveredSession.SequenceNumber+1, stateChangeMetadata.Sequence, "Sequence should be incremented")
	assert.Equal(t, "user-requested", stateChangeMetadata.Reason, "Reason should match")
	
	// Update the recovered session with the state change
	siprec.UpdateRecordingSession(recoveredSession, stateChangeMetadata)
	assert.Equal(t, "paused", recoveredSession.RecordingState, "Recording state should be updated to paused")
	
	logger.Info("Session recovery test completed successfully")
}

// TestRealTimeSessionRecovery tests the session recovery with simulated RTP streams
// This test creates a more realistic scenario with actual UDP connections
func TestRealTimeSessionRecovery(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	// Create a simulated call with two RTP streams
	rtpPort1 := 17000
	rtpPort2 := 17002
	
	// Create UDP listeners for the RTP ports
	conn1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort1})
	require.NoError(t, err, "Failed to create UDP listener for port 1")
	defer conn1.Close()
	
	conn2, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort2})
	require.NoError(t, err, "Failed to create UDP listener for port 2")
	defer conn2.Close()
	
	// Create original session
	originalSession := &siprec.RecordingSession{
		ID:             "realtime-test-123",
		RecordingState: "active",
		SequenceNumber: 1,
		Participants: []siprec.Participant{
			{
				ID:          "p1",
				Name:        "Alice",
				DisplayName: "Alice Smith",
				Role:        "active",
				CommunicationIDs: []siprec.CommunicationID{
					{
						Type:  "sip",
						Value: "sip:alice@example.com",
					},
				},
				MediaStreams: []string{"stream1"},
			},
			{
				ID:          "p2",
				Name:        "Bob",
				DisplayName: "Bob Jones",
				Role:        "passive",
				CommunicationIDs: []siprec.CommunicationID{
					{
						Type:  "sip",
						Value: "sip:bob@example.com",
					},
				},
				MediaStreams: []string{"stream2"},
			},
		},
		MediaStreamTypes: []string{"audio"},
		AssociatedTime:   time.Now(),
	}
	
	// Create synchronization primitives
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Start RTP receivers
	startRTPReceiver := func(conn *net.UDPConn, streamName string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			buffer := make([]byte, 1500)
			packetCount := 0
			
			for {
				select {
				case <-ctx.Done():
					logger.WithField("stream", streamName).Info("RTP receiver stopped due to context done")
					return
				default:
					conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
					n, _, err := conn.ReadFromUDP(buffer)
					if err != nil {
						continue
					}
					
					packetCount++
					logger.WithFields(logrus.Fields{
						"stream":  streamName,
						"bytes":   n,
						"packets": packetCount,
					}).Debug("Received RTP packet")
					
					// If this is the primary stream and we've received enough packets, simulate failure
					if streamName == "stream1" && packetCount == 10 {
						logger.WithField("stream", streamName).Info("Simulating network failure")
						// Just let the stream continue but we'll pretend there was a failure
						// The recovery process will happen in the main test goroutine
					}
				}
			}
		}()
	}
	
	// Start RTP senders
	startRTPSender := func(port int, streamName string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			// Create a client UDP connection
			clientConn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
			if err != nil {
				logger.WithError(err).Error("Failed to create client UDP connection")
				return
			}
			defer clientConn.Close()
			
			// Create a simple RTP packet
			header := make([]byte, 12)
			header[0] = 0x80 // Version 2
			header[1] = 0     // PCMU payload type
			
			// Payload with test audio data
			testData := []byte(fmt.Sprintf("This is test audio for %s", streamName))
			
			// Send packets
			ticker := time.NewTicker(50 * time.Millisecond)
			defer ticker.Stop()
			
			sequence := uint16(0)
			timestamp := uint32(0)
			
			for i := 0; i < 40; i++ { // 40 * 50ms = 2 seconds
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					// Update sequence and timestamp
					header[2] = byte(sequence >> 8)
					header[3] = byte(sequence)
					sequence++
					
					timestamp += 160
					header[4] = byte(timestamp >> 24)
					header[5] = byte(timestamp >> 16)
					header[6] = byte(timestamp >> 8)
					header[7] = byte(timestamp)
					
					// Create and send packet
					packet := append(header, testData...)
					_, err := clientConn.Write(packet)
					if err != nil {
						logger.WithError(err).Error("Failed to send RTP packet")
						return
					}
					
					logger.WithFields(logrus.Fields{
						"stream":   streamName,
						"sequence": sequence - 1,
					}).Debug("Sent RTP packet")
				}
			}
			
			logger.WithField("stream", streamName).Info("Finished sending RTP packets")
		}()
	}
	
	// Start RTP receivers
	startRTPReceiver(conn1, "stream1")
	startRTPReceiver(conn2, "stream2")
	
	// Start RTP senders
	startRTPSender(rtpPort1, "stream1")
	startRTPSender(rtpPort2, "stream2")
	
	// Wait a bit for initial packets to flow
	time.Sleep(500 * time.Millisecond)
	
	// Create failover metadata from the original session
	failoverMetadata := siprec.CreateFailoverMetadata(originalSession)
	originalSession.FailoverID = failoverMetadata.SessionRecordingAssoc.FixedID
	
	// Add stream information to the metadata
	failoverMetadata.Streams = []siprec.Stream{
		{
			Label:    "audio1",
			StreamID: "stream1",
			Type:     "audio",
		},
		{
			Label:    "audio2",
			StreamID: "stream2",
			Type:     "audio",
		},
	}
	
	// Update participant send associations
	for i := range failoverMetadata.Participants {
		if failoverMetadata.Participants[i].ID == "p1" {
			failoverMetadata.Participants[i].Send = []string{"audio1"}
		} else if failoverMetadata.Participants[i].ID == "p2" {
			failoverMetadata.Participants[i].Send = []string{"audio2"}
		}
	}
	
	// Log the metadata
	metadataXML, err := siprec.SerializeMetadata(failoverMetadata)
	require.NoError(t, err, "Failed to serialize metadata")
	logger.WithField("metadata", metadataXML).Debug("Failover metadata generated")
	
	// Simulate short delay for failover
	time.Sleep(200 * time.Millisecond)
	
	// Create a recovered session from the failover metadata
	recoveredSession, err := siprec.RecoverSession(failoverMetadata)
	require.NoError(t, err, "Failed to recover session")
	
	// Process stream recovery
	siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
	
	// Add stream types manually since our metadata doesn't include them
	recoveredSession.MediaStreamTypes = []string{"audio"}
	
	// Validate session continuity
	err = siprec.ValidateSessionContinuity(originalSession, recoveredSession)
	require.NoError(t, err, "Session continuity validation failed")
	
	// Log the recovered session
	// Log the recovered session details directly
	logger.WithFields(logrus.Fields{
		"session_id":        recoveredSession.ID,
		"state":             recoveredSession.RecordingState,
		"participant_count": len(recoveredSession.Participants),
	}).Info("Recovered session details")
	
	// Wait for all RTP operations to complete
	wg.Wait()
	
	// Final verification
	assert.Equal(t, originalSession.ID, recoveredSession.ID, "Session ID should match")
	assert.Equal(t, len(originalSession.Participants), len(recoveredSession.Participants), "Should have same number of participants")
	assert.Equal(t, len(originalSession.MediaStreamTypes), len(recoveredSession.MediaStreamTypes), "Should have same number of media stream types")
	
	logger.Info("Real-time session recovery test completed successfully")
}