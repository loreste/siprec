package sip

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/textproto"
	"os"
	"testing"
	"time"

	sipparser "github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

func TestResumeAfterLongHoldRestartsListener(t *testing.T) {
	// 1. Setup Server with short RTP timeout
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.DebugLevel)

	mediaConfig := &media.Config{
		RTPTimeout: 100 * time.Millisecond,
		RTPPortMin: 20000,
		RTPPortMax: 20010,
	}
	handler := &Handler{
		Logger: logger,
		Config: &Config{MediaConfig: mediaConfig},
	}
	media.InitPortManager(20000, 20010)

	sipServer := NewCustomSIPServer(logger, handler)

	// 2. Simulate existing Active Call
	callID := "call-long-hold-bug"

	// Create a forwarder and start it
	session := &siprec.RecordingSession{ID: "session-1", RecordingState: "active"}
	forwarder, err := media.NewRTPForwarder(mediaConfig.RTPTimeout, session, logger, false, nil)
	require.NoError(t, err)

	// Start forwarding (opens UDP port)
	ctx := context.Background()
	media.StartRTPForwarding(ctx, forwarder, callID, mediaConfig, nil)

	// Verify it's active (use CleanupMutex to safely read Conn)
	require.Eventually(t, func() bool {
		forwarder.CleanupMutex.Lock()
		conn := forwarder.Conn
		forwarder.CleanupMutex.Unlock()
		return conn != nil && forwarder.LocalPort != 0
	}, time.Second, 10*time.Millisecond, "RTP forwarder failed to start listener")

	callState := &CallState{
		CallID:           callID,
		State:            "connected",
		RecordingSession: session,
		RTPForwarder:     forwarder,
		RTPForwarders:    []*media.RTPForwarder{forwarder},
		StreamForwarders: map[string]*media.RTPForwarder{"leg0": forwarder},
	}

	sipServer.callMutex.Lock()
	sipServer.callStates[callID] = callState
	sipServer.callMutex.Unlock()

	// 3. Wait for Timeout (Simulate Long Hold)
	// MonitorRTPTimeout has a hardcoded 1s ticker, so we must wait > 1s
	time.Sleep(1500 * time.Millisecond)

	// Verify forwarder is dead (Conn closed/nil or StopChan closed)
	// Note: In the real code, Stop() closes the StopChan.
	select {
	case <-forwarder.StopChan:
		// Expected
	default:
		t.Fatal("Forwarder did not timeout as expected")
	}

	// 4. Send Re-INVITE (Resume)
	var bodyBuf bytes.Buffer
	writer := multipart.NewWriter(&bodyBuf)

	// Add SDP part
	sdpHeader := make(textproto.MIMEHeader)
	sdpHeader.Set("Content-Type", "application/sdp")
	sdpPart, err := writer.CreatePart(sdpHeader)
	require.NoError(t, err)
	sdp := "v=0\r\no=test 1 2 IN IP4 127.0.0.1\r\ns=Test\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 1234 RTP/AVP 0\r\na=sendrecv\r\na=label:0\r\n"
	_, err = sdpPart.Write([]byte(sdp))
	require.NoError(t, err)

	// Add Metadata part
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/rs-metadata+xml")
	metaPart, err := writer.CreatePart(metaHeader)
	require.NoError(t, err)
	metadata := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="session-1" state="active">
  <session session_id="session-1"/>
  <sessionrecordingassoc session_id="session-1"/>
  <participant participant_id="p1"><aor>sip:alice@example.com</aor></participant>
  <stream stream_id="str-1" label="0"><label>0</label></stream>
</recording>`
	_, err = metaPart.Write([]byte(metadata))
	require.NoError(t, err)

	writer.Close()

	boundary := writer.Boundary()
	contentType := fmt.Sprintf("multipart/mixed; boundary=%s", boundary)

	req := sipparser.NewRequest(sipparser.INVITE, sipparser.Uri{Host: "recorder"})
	req.AppendHeader(sipparser.NewHeader("Call-ID", callID))
	req.AppendHeader(sipparser.NewHeader("CSeq", "2 INVITE"))
	req.AppendHeader(sipparser.NewHeader("Content-Type", contentType))
	req.AppendHeader(sipparser.NewHeader("Via", "SIP/2.0/UDP 192.168.1.1:5060;branch=z9hG4bK-1234"))
	req.AppendHeader(sipparser.NewHeader("From", "<sip:alice@example.com>;tag=123"))
	req.AppendHeader(sipparser.NewHeader("To", "<sip:recorder@example.com>"))
	req.AppendHeader(sipparser.NewHeader("Contact", "<sip:alice@192.168.1.1:5060>"))

	// Ensure parsed components are populated to avoid panic in NewResponseFromRequest
	req.SetBody(bodyBuf.Bytes())

	tx := newTestServerTransaction(req)
	message := &SIPMessage{
		Method:      "INVITE",
		CallID:      callID,
		CSeq:        "2 INVITE",
		ContentType: contentType,
		Body:        bodyBuf.Bytes(),
		Request:     req,
		Transaction: tx,
	}

	// 5. Assert: The handler should detect the dead forwarder and start a new one
	sipServer.handleSiprecReInvite(message, callState)

	require.Equal(t, 200, tx.responses[len(tx.responses)-1].StatusCode)

	// CRITICAL CHECK: The current forwarder in callState should be ACTIVE
	currentForwarder := callState.RTPForwarder

	// If the bug exists, this might be the OLD dead forwarder, or a new one that isn't started?
	// The bug is that it reuses the dead forwarder.

	select {
	case <-currentForwarder.StopChan:
		t.Fatal("Call state still has a dead/stopped RTP forwarder after Resume re-INVITE")
	default:
		// Good, it's running
	}

	currentForwarder.CleanupMutex.Lock()
	connOpen := currentForwarder.Conn != nil
	currentForwarder.CleanupMutex.Unlock()
	require.True(t, connOpen, "RTP Conn should be open")
	// Verify it's listening on a valid port (might be different or same, but must be active)
	require.NotZero(t, currentForwarder.LocalPort)

	// If it allocated a new one, the pointer should be different
	verifyNewForwarder := true // strict mode
	if verifyNewForwarder && currentForwarder == forwarder {
		// If pointers are same, check if it was somehow resurrected (unlikely for struct)
		// or if we failed to replace it.
		// Since NewRTPForwarder returns a new struct, we expect a replacement.
		t.Log("Warning: Forwarder struct pointer is identical. Unless we have logic to restart in-place, this is suspicious.")
	}
}
