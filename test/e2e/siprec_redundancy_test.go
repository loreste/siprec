package e2e

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/stt"
)

// TestSiprecRedundancyFlow tests the complete session redundancy flow:
// 1. Creates an initial SIPREC session
// 2. Simulates a connection failure
// 3. Attempts to recover the session using the failover mechanism
// 4. Verifies that recording continues with the same session ID
func TestSiprecRedundancyFlow(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	// Create a mock STT provider for audio processing
	mockProvider := stt.NewMockProvider(logger)
	err := mockProvider.Initialize()
	require.NoError(t, err, "Failed to initialize mock provider")

	// Create a unique session ID for this test
	sessionID := fmt.Sprintf("test-session-%d", time.Now().UnixNano())
	logger.WithField("session_id", sessionID).Info("Starting redundancy test with session ID")

	// Step 1: Create the initial recording session
	recordingSession := &siprec.RecordingSession{
		ID:             sessionID,
		RecordingState: "active",
		AssociatedTime: time.Now(),
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
	}

	// Step 2: Generate failover metadata and store failover ID
	logger.Info("Step 2: Generating failover metadata")
	failoverMetadata := siprec.CreateFailoverMetadata(recordingSession)
	
	// Store failover ID in the original session
	originalFailoverID := failoverMetadata.SessionRecordingAssoc.FixedID
	recordingSession.FailoverID = originalFailoverID
	
	// Log the metadata for debugging
	metadataXML, err := siprec.SerializeMetadata(failoverMetadata)
	require.NoError(t, err, "Failed to serialize metadata")
	logger.WithField("metadata", metadataXML).Debug("Failover metadata generated")
	
	// Verify the failover metadata has the correct fields
	assert.Equal(t, sessionID, failoverMetadata.SessionID, "Session ID should match")
	assert.Equal(t, "active", failoverMetadata.State, "State should be active")
	assert.Equal(t, 2, failoverMetadata.Sequence, "Sequence should be incremented")
	assert.Equal(t, "failover", failoverMetadata.Reason, "Reason should be failover")
	assert.Equal(t, originalFailoverID, failoverMetadata.SessionRecordingAssoc.FixedID, "FixedID should match")
	
	// Step 3: Set up RTP endpoints for the test
	rtpPort1 := 17000 + rand.Intn(1000) // Random high port to avoid conflicts
	rtpPort2 := rtpPort1 + 2
	
	logger.WithFields(logrus.Fields{
		"rtp_port1": rtpPort1,
		"rtp_port2": rtpPort2,
	}).Info("Step 3: Setting up RTP endpoints")
	
	// Create UDP listeners for RTP
	conn1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort1})
	require.NoError(t, err, "Failed to create UDP listener for port 1")
	defer conn1.Close()
	
	conn2, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: rtpPort2})
	require.NoError(t, err, "Failed to create UDP listener for port 2")
	defer conn2.Close()
	
	// Create synchronization primitives
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Start RTP receivers
	startRTPReceiver := func(conn *net.UDPConn, streamName string, isPrimary bool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			buffer := make([]byte, 1500)
			packetCount := 0
			disconnected := false
			
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
					
					// If this is the primary stream and we've received 10 packets, simulate connection failure
					if isPrimary && packetCount == 10 && !disconnected {
						disconnected = true
						logger.WithField("stream", streamName).Info("Step 4: Simulating network failure")
					}
				}
			}
		}()
	}
	
	// Start RTP senders
	startRTPSender := func(port int, streamName string, simulateFailure bool) {
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
			connectionBroken := false
			
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
					
					// If we should simulate failure and we've sent enough packets, break the connection
					if simulateFailure && i == 20 && !connectionBroken {
						connectionBroken = true
						logger.WithField("stream", streamName).Info("Step 5: Creating new connection for recovery")
						
						// Close existing connection
						clientConn.Close()
						
						// Wait a moment to simulate disconnect
						time.Sleep(200 * time.Millisecond)
						
						// Create a new connection (simulating a reconnect with Replaces)
						newConn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
						if err != nil {
							logger.WithError(err).Error("Failed to create new client connection")
							return
						}
						clientConn = newConn
						defer newConn.Close()
						
						// Perform session recovery
						logger.Info("Step 6: Recovering session")
						recoveredSession, err := siprec.RecoverSession(failoverMetadata)
						if err != nil {
							logger.WithError(err).Error("Failed to recover session")
							return
						}
						
						// Verify recovery was successful
						if recoveredSession.ID != sessionID {
							logger.Error("Recovered session ID doesn't match original")
							return
						}
						
						if recoveredSession.FailoverID != originalFailoverID {
							logger.Error("Recovered session failover ID doesn't match original")
							return
						}
						
						logger.WithFields(logrus.Fields{
							"original_id": sessionID,
							"recovered_id": recoveredSession.ID,
							"failover_id": recoveredSession.FailoverID,
						}).Info("Session recovered successfully")
						
						// Process stream recovery
						siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
					}
					
					// Send the RTP packet if the connection is still valid
					_, err := clientConn.Write(packet)
					if err != nil {
						logger.WithError(err).Error("Failed to send RTP packet")
						continue
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
	
	// Start the RTP tests
	logger.Info("Starting RTP receivers...")
	startRTPReceiver(conn1, "stream1", true)
	startRTPReceiver(conn2, "stream2", false)
	
	logger.Info("Starting RTP senders...")
	startRTPSender(rtpPort1, "stream1", true)
	startRTPSender(rtpPort2, "stream2", false)
	
	// Wait for all goroutines to finish
	wg.Wait()
	
	// Step 7: Validate continuity
	logger.Info("Step 7: Validating session continuity")
	
	// Create a recovered session from failover metadata
	recoveredSession, err := siprec.RecoverSession(failoverMetadata)
	require.NoError(t, err, "Failed to recover session from metadata")
	
	// Process stream recovery
	siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
	
	// Validate session continuity
	err = siprec.ValidateSessionContinuity(recordingSession, recoveredSession)
	assert.NoError(t, err, "Session continuity validation failed")
	
	// Additional assertions to verify recovery worked correctly
	assert.Equal(t, recordingSession.ID, recoveredSession.ID, "Session ID should be preserved")
	assert.Equal(t, recordingSession.FailoverID, recoveredSession.FailoverID, "Failover ID should be preserved")
	assert.Equal(t, len(recordingSession.Participants), len(recoveredSession.Participants), "Participant count should match")
	
	logger.Info("Session redundancy test completed successfully")
}

// TestConcurrentSessionsRedundancy tests multiple concurrent sessions with redundancy
// Simplified to remove redundant result checks
func TestConcurrentSessionsRedundancy(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	// Define test parameters
	sessionCount := 3
	
	logger.WithField("session_count", sessionCount).Info("Starting concurrent sessions redundancy test")
	
	// Create wait group for test synchronization
	var wg sync.WaitGroup
	var mutex sync.Mutex
	var failureCount int
	
	// Create sessions and test them concurrently
	for i := 0; i < sessionCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			
			// Create a unique session ID
			sessionID := fmt.Sprintf("concurrent-session-%d-%d", index, time.Now().UnixNano())
			
			// Create the recording session
			recordingSession := &siprec.RecordingSession{
				ID:             sessionID,
				RecordingState: "active",
				AssociatedTime: time.Now(),
				SequenceNumber: 1,
				Participants: []siprec.Participant{
					{
						ID:          fmt.Sprintf("p%d-1", index),
						Name:        "Alice",
						DisplayName: "Alice Smith",
						Role:        "active",
					},
					{
						ID:          fmt.Sprintf("p%d-2", index),
						Name:        "Bob",
						DisplayName: "Bob Jones",
						Role:        "passive",
					},
				},
				MediaStreamTypes: []string{"audio"},
			}
			
			// Generate failover metadata
			failoverMetadata := siprec.CreateFailoverMetadata(recordingSession)
			recordingSession.FailoverID = failoverMetadata.SessionRecordingAssoc.FixedID
			
			// Simulate failure and recovery
			recoveredSession, err := siprec.RecoverSession(failoverMetadata)
			if err != nil {
				logger.WithError(err).Errorf("Session %d: Failed to recover session", index)
				mutex.Lock()
				failureCount++
				mutex.Unlock()
				return
			}
			
			// Process stream recovery and validate continuity
			siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
			if err = siprec.ValidateSessionContinuity(recordingSession, recoveredSession); err != nil {
				logger.WithError(err).Errorf("Session %d: Continuity validation failed", index)
				mutex.Lock()
				failureCount++
				mutex.Unlock()
				return
			}
			
			logger.WithField("session_id", sessionID).Infof("Session %d recovered successfully", index)
		}(i)
	}
	
	// Wait for all sessions to complete with a timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		logger.Info("All concurrent sessions completed")
	case <-time.After(5 * time.Second):
		t.Fatal("Test timed out waiting for concurrent sessions")
	}
	
	// Verify all sessions were recovered successfully
	assert.Equal(t, 0, failureCount, "All sessions should recover successfully")
}

// TestStreamContinuityAfterFailover specifically tests that media stream continuity
// is maintained after a failover event
func TestStreamContinuityAfterFailover(t *testing.T) {
	// Create a logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	// Create a recording session with streams
	sessionID := fmt.Sprintf("stream-session-%d", time.Now().UnixNano())
	recordingSession := &siprec.RecordingSession{
		ID:             sessionID,
		RecordingState: "active",
		AssociatedTime: time.Now(),
		SequenceNumber: 1,
		Participants: []siprec.Participant{
			{
				ID:          "p1",
				Name:        "Alice",
				DisplayName: "Alice Smith",
				Role:        "active",
				MediaStreams: []string{"audio1", "video1"},
			},
			{
				ID:          "p2",
				Name:        "Bob",
				DisplayName: "Bob Jones",
				Role:        "passive",
				MediaStreams: []string{"audio2"},
			},
		},
		MediaStreamTypes: []string{"audio", "video"},
	}
	
	// Create failover metadata with stream information
	failoverMetadata := siprec.CreateFailoverMetadata(recordingSession)
	recordingSession.FailoverID = failoverMetadata.SessionRecordingAssoc.FixedID
	
	// Add stream information to metadata
	failoverMetadata.Streams = []siprec.Stream{
		{
			Label:    "audio1",
			StreamID: "audio1",
			Type:     "audio",
		},
		{
			Label:    "video1",
			StreamID: "video1",
			Type:     "video",
		},
		{
			Label:    "audio2",
			StreamID: "audio2",
			Type:     "audio",
		},
	}
	
	// Add stream send relationships to participants
	for i := range failoverMetadata.Participants {
		if failoverMetadata.Participants[i].ID == "p1" {
			failoverMetadata.Participants[i].Send = []string{"audio1", "video1"}
		} else if failoverMetadata.Participants[i].ID == "p2" {
			failoverMetadata.Participants[i].Send = []string{"audio2"}
		}
	}
	
	// Simulate audio streams
	audioStreams := make(map[string][]byte)
	audioPipes := make(map[string]*io.PipeWriter)
	
	for _, stream := range []string{"audio1", "audio2"} {
		pr, pw := io.Pipe()
		audioPipes[stream] = pw
		
		// Collect audio in a separate goroutine
		go func(stream string, reader io.Reader) {
			buf := make([]byte, 1024)
			for {
				n, err := reader.Read(buf)
				if err != nil {
					if err != io.EOF {
						logger.WithError(err).Errorf("Error reading from %s", stream)
					}
					break
				}
				
				// Append to collected audio
				audioStreams[stream] = append(audioStreams[stream], buf[:n]...)
			}
		}(stream, pr)
	}
	
	// Write some test audio
	audioPipes["audio1"].Write([]byte("Audio stream 1 before failover"))
	audioPipes["audio2"].Write([]byte("Audio stream 2 before failover"))
	
	// Now simulate failover
	logger.Info("Simulating failover event")
	
	// Recover the session
	recoveredSession, err := siprec.RecoverSession(failoverMetadata)
	require.NoError(t, err, "Failed to recover session")
	
	// Process stream recovery to restore stream information
	siprec.ProcessStreamRecovery(recoveredSession, failoverMetadata)
	
	// Validate that the media stream types are recovered
	assert.ElementsMatch(t, recordingSession.MediaStreamTypes, recoveredSession.MediaStreamTypes,
		"Media stream types should be recovered")
	
	// Write more test audio after failover
	audioPipes["audio1"].Write([]byte("Audio stream 1 after failover"))
	audioPipes["audio2"].Write([]byte("Audio stream 2 after failover"))
	
	// Close pipes to end collection
	for _, pw := range audioPipes {
		pw.Close()
	}
	
	// Verify stream continuity by checking participants' media streams
	for i, p := range recoveredSession.Participants {
		if p.ID == "p1" {
			assert.Len(t, p.MediaStreams, 2, "Participant p1 should have 2 media streams")
			assert.Contains(t, p.MediaStreams, "audio1", "Participant p1 should have audio1 stream")
			assert.Contains(t, p.MediaStreams, "video1", "Participant p1 should have video1 stream")
		} else if p.ID == "p2" {
			assert.Len(t, p.MediaStreams, 1, "Participant p2 should have 1 media stream")
			assert.Contains(t, p.MediaStreams, "audio2", "Participant p2 should have audio2 stream")
		} else {
			t.Errorf("Unexpected participant ID: %s at index %d", p.ID, i)
		}
	}
	
	// Verify audio continuity by checking the content
	for stream, audio := range audioStreams {
		assert.Contains(t, string(audio), "before failover", 
			"Audio stream %s should contain pre-failover data", stream)
		assert.Contains(t, string(audio), "after failover", 
			"Audio stream %s should contain post-failover data", stream)
	}
	
	logger.Info("Stream continuity test completed successfully")
}