package sip

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sipparser "github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

func TestHandleSubscribeRegistersCallback(t *testing.T) {
	eventCh := make(chan NotificationEvent, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var event NotificationEvent
		require.NoError(t, json.NewDecoder(r.Body).Decode(&event))
		eventCh <- event
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	notifier := NewMetadataNotifier(logger, nil, time.Second)
	handler := &Handler{
		Logger:   logger,
		Config:   &Config{},
		Notifier: notifier,
	}

	sipServer := NewCustomSIPServer(logger, handler)

	callID := "call-subscribe-001"
	session := &siprec.RecordingSession{
		ID:                "session-subscribe-001",
		RecordingState:    "active",
		ExtendedMetadata:  make(map[string]string),
		SessionGroupRoles: make(map[string]string),
		PolicyStates:      make(map[string]siprec.PolicyAckStatus),
	}

	sipServer.callMutex.Lock()
	sipServer.callStates[callID] = &CallState{
		CallID:            callID,
		State:             "connected",
		RecordingSession:  session,
		LocalTag:          "local-tag",
		RemoteTag:         "remote-tag",
		LastActivity:      time.Now(),
		AllocatedPortPair: nil,
	}
	sipServer.callMutex.Unlock()

	pipeA, pipeB := net.Pipe()
	defer pipeA.Close()
	defer pipeB.Close()

	message := &SIPMessage{
		Method:  "SUBSCRIBE",
		CallID:  callID,
		Version: "SIP/2.0",
		Headers: map[string][]string{
			"x-callback-url": {server.URL},
			"to":             {"<sip:server@example.com>;tag=server"},
			"from":           {"<sip:client@example.com>;tag=client"},
			"call-id":        {callID},
			"cseq":           {"2 SUBSCRIBE"},
			"via":            {"SIP/2.0/TCP localhost;branch=z9hG4bK"},
		},
		Connection: &SIPConnection{
			conn:      pipeA,
			writer:    bufio.NewWriter(io.Discard),
			transport: "tcp",
		},
	}

	sipServer.handleSubscribeMessage(message)
	require.Contains(t, session.Callbacks, server.URL, "Callback URL should be stored on session")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go notifier.Notify(ctx, session, callID, "metadata.accepted", nil)

	select {
	case event := <-eventCh:
		require.Equal(t, callID, event.CallID)
		require.Equal(t, "metadata.accepted", event.Event)
	case <-ctx.Done():
		t.Fatal("expected notification delivery via registered callback")
	}
}

func TestExtractSiprecContentMultipart(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := &Handler{Logger: logger, Config: &Config{}}
	sipServer := NewCustomSIPServer(logger, handler)

	boundary := "OSS-unique-boundary-42"
	sdp := "v=0\r\no=test 1 1 IN IP4 192.168.1.10\r\nm=audio 4000 RTP/AVP 8 101\r\na=sendonly\r\n"
	metadata := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="sess-1" state="active" sequence="1"/>`
	body := fmt.Sprintf("--%s\r\nContent-Type: application/sdp; charset=UTF-8\r\n\r\n%s\r\n--%s\r\nContent-Type: application/rs-metadata+xml; charset=UTF-8\r\n\r\n%s\r\n--%s--\r\n",
		boundary, sdp, boundary, metadata, boundary)

	sdpPart, metadataPart := sipServer.extractSiprecContent([]byte(body), fmt.Sprintf("multipart/mixed; boundary=%s", boundary))
	require.NotNil(t, sdpPart)
	require.NotNil(t, metadataPart)
	require.Contains(t, string(sdpPart), "m=audio 4000")
	require.Contains(t, string(metadataPart), "<recording")
}

func TestHandleByeAllowsPendingAck(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := &Handler{Logger: logger, Config: &Config{}}
	sipServer := NewCustomSIPServer(logger, handler)

	callID := "call-bye-pending"
	session := &siprec.RecordingSession{
		ID:               "session-123",
		RecordingState:   "active",
		ExtendedMetadata: map[string]string{},
	}

	sipServer.callMutex.Lock()
	sipServer.callStates[callID] = &CallState{
		CallID:           callID,
		State:            "awaiting_ack",
		PendingAckCSeq:   2,
		RemoteCSeq:       2,
		LocalTag:         "local-tag",
		RecordingSession: session,
		StreamForwarders: make(map[string]*media.RTPForwarder),
	}
	sipServer.callMutex.Unlock()

	req := sipparser.NewRequest(sipparser.BYE, sipparser.Uri{Host: "example.com"})
	req.AppendHeader(sipparser.NewHeader("Via", "SIP/2.0/UDP 192.0.2.100;branch=z9hG4bK-test"))
	req.AppendHeader(sipparser.NewHeader("From", "<sip:src@example.com>;tag=src-tag"))
	req.AppendHeader(sipparser.NewHeader("To", "<sip:dst@example.com>"))
	req.AppendHeader(sipparser.NewHeader("Call-ID", callID))
	req.AppendHeader(sipparser.NewHeader("CSeq", "3 BYE"))
	req.AppendHeader(sipparser.NewHeader("Contact", "<sip:src@example.com>"))

	tx := newTestServerTransaction(req)
	message := &SIPMessage{
		Method:      "BYE",
		CallID:      callID,
		CSeq:        "3 BYE",
		Request:     req,
		Parsed:      req,
		Transaction: tx,
	}

	sipServer.handleByeMessage(message)
	require.NotNil(t, tx.resp)
	require.Equal(t, 200, tx.resp.StatusCode)
	sipServer.callMutex.RLock()
	_, exists := sipServer.callStates[callID]
	sipServer.callMutex.RUnlock()
	require.False(t, exists, "call state should be cleaned up after BYE")
}

func TestHandleSiprecInviteRejectsMissingSDP(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := &Handler{
		Logger: logger,
		Config: &Config{MediaConfig: &media.Config{}},
	}
	sipServer := NewCustomSIPServer(logger, handler)

	callID := "call-invite-invalid-sdp"
	boundary := "OSS-unique-boundary-42"
	metadata := &siprec.RSMetadata{
		SessionID: "sess-1",
		State:     "active",
		Sequence:  1,
		Participants: []siprec.RSParticipant{
			{
				ID:  "participant-1",
				Aor: []siprec.Aor{{Value: "sip:alice@example.com"}},
			},
		},
		Streams: []siprec.Stream{
			{Label: "0", StreamID: "stream-0", Type: "audio"},
		},
	}
	metadataXML, err := siprec.CreateMetadataResponse(metadata)
	require.NoError(t, err)
	body := fmt.Sprintf("--%s\r\nContent-Type: application/rs-metadata+xml\r\n\r\n%s\r\n--%s--\r\n", boundary, metadataXML, boundary)

	req := sipparser.NewRequest(sipparser.INVITE, sipparser.Uri{Host: "recorder"})
	req.AppendHeader(sipparser.NewHeader("Via", "SIP/2.0/UDP 192.0.2.1;branch=z9hG4bK-test"))
	req.AppendHeader(sipparser.NewHeader("From", "<sip:src@example.com>;tag=src"))
	req.AppendHeader(sipparser.NewHeader("To", "<sip:dst@example.com>"))
	req.AppendHeader(sipparser.NewHeader("Call-ID", callID))
	req.AppendHeader(sipparser.NewHeader("CSeq", "2 INVITE"))
	req.AppendHeader(sipparser.NewHeader("Contact", "<sip:src@example.com>"))

	tx := newTestServerTransaction(req)
	message := &SIPMessage{
		Method:      "INVITE",
		CallID:      callID,
		CSeq:        "2 INVITE",
		ContentType: fmt.Sprintf("multipart/mixed; boundary=%s", boundary),
		Body:        []byte(body),
		Request:     req,
		Parsed:      req,
		Transaction: tx,
	}

	sipServer.handleSiprecInvite(message)
	require.NotEmpty(t, tx.responses)
	var statuses []int
	for _, resp := range tx.responses {
		statuses = append(statuses, resp.StatusCode)
	}
	t.Logf("SIP responses: %v", statuses)
	last := tx.responses[len(tx.responses)-1]
	t.Logf("Final response: %d %s", last.StatusCode, last.Reason)
	require.Equal(t, 488, last.StatusCode)
}

type testServerTransaction struct {
	req       *sipparser.Request
	resp      *sipparser.Response
	responses []*sipparser.Response
	done      chan struct{}
	acks      chan *sipparser.Request
}

func newTestServerTransaction(req *sipparser.Request) *testServerTransaction {
	done := make(chan struct{})
	close(done)
	acks := make(chan *sipparser.Request)
	close(acks)
	return &testServerTransaction{req: req, done: done, acks: acks}
}

func (t *testServerTransaction) Key() string { return "test" }

func (t *testServerTransaction) Origin() *sipparser.Request { return t.req }

func (t *testServerTransaction) Done() <-chan struct{} { return t.done }

func (t *testServerTransaction) Err() error { return nil }

func (t *testServerTransaction) Respond(res *sipparser.Response) error {
	t.resp = res
	t.responses = append(t.responses, res)
	return nil
}

func (t *testServerTransaction) Acks() <-chan *sipparser.Request { return t.acks }

func (t *testServerTransaction) OnTerminate(sipparser.FnTxTerminate) bool { return true }

func (t *testServerTransaction) Terminate() {}
