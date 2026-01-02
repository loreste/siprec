package sip

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sipparser "github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/media"
	"siprec-server/pkg/security"
	"siprec-server/pkg/siprec"
)

func TestHandleSubscribeRegistersCallback(t *testing.T) {
	eventCh := make(chan NotificationEvent, 1)
	callbackURL := "http://callback.local/notify"

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	notifier := NewMetadataNotifier(logger, nil, time.Second)
	notifier.client = &http.Client{
		Timeout: time.Second,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			defer req.Body.Close()
			var event NotificationEvent
			require.NoError(t, json.NewDecoder(req.Body).Decode(&event))
			eventCh <- event
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}),
	}
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
			"x-callback-url": {callbackURL},
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
	require.Contains(t, session.Callbacks, callbackURL, "Callback URL should be stored on session")

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

func writeTempWAV(t *testing.T, path string, samples []int16) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create wav: %v", err)
	}
	defer f.Close()

	writer, err := media.NewWAVWriter(f, 8000, 1)
	if err != nil {
		t.Fatalf("wav writer: %v", err)
	}

	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}

	if _, err := writer.Write(buf); err != nil {
		t.Fatalf("write samples: %v", err)
	}
	if err := writer.Finalize(); err != nil {
		t.Fatalf("finalize wav: %v", err)
	}
}

func TestCombineRecordingLegsCreatesMergedWav(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dir := t.TempDir()
	leg0 := filepath.Join(dir, "leg0.wav")
	leg1 := filepath.Join(dir, "leg1.wav")
	writeTempWAV(t, leg0, []int16{100, 200})
	writeTempWAV(t, leg1, []int16{1000})

	handler := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{
				RecordingDir: dir,
				CombineLegs:  true,
			},
		},
	}
	server := NewCustomSIPServer(logger, handler)

	callState := &CallState{
		RecordingSession: &siprec.RecordingSession{
			ExtendedMetadata: make(map[string]string),
		},
	}
	callID := "B2B.160.1111.2222"
	server.combineRecordingLegs(callID, callState, []string{leg0, leg1})

	expectedName := security.SanitizeCallUUID(callID)
	expectedPath := filepath.Join(dir, fmt.Sprintf("%s.wav", expectedName))

	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("combined wav not created: %v", err)
	}

	reader, err := media.NewWAVReader(expectedPath)
	if err != nil {
		t.Fatalf("open combined: %v", err)
	}
	defer reader.Close()

	if reader.Channels != 2 {
		t.Fatalf("expected 2 channels, got %d", reader.Channels)
	}

	samples, err := reader.ReadSamples(10)
	if err != nil && err != io.EOF {
		t.Fatalf("read samples: %v", err)
	}
	expectedSamples := []int16{
		100, 1000,
		200, 0,
	}
	if len(samples) != len(expectedSamples) {
		t.Fatalf("unexpected sample count %d", len(samples))
	}
	for i := range samples {
		if samples[i] != expectedSamples[i] {
			t.Fatalf("sample[%d]=%d expected %d", i, samples[i], expectedSamples[i])
		}
	}

	pathMeta := callState.RecordingSession.ExtendedMetadata["combined_recording_path"]
	if pathMeta != expectedPath {
		t.Fatalf("metadata path mismatch: %s vs %s", pathMeta, expectedPath)
	}
}

func TestCombineRecordingLegsRespectsDisableFlag(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	dir := t.TempDir()
	leg0 := filepath.Join(dir, "leg0.wav")
	leg1 := filepath.Join(dir, "leg1.wav")
	writeTempWAV(t, leg0, []int16{10})
	writeTempWAV(t, leg1, []int16{20})

	handler := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{
				RecordingDir: dir,
				CombineLegs:  false,
			},
		},
	}
	server := NewCustomSIPServer(logger, handler)
	session := &siprec.RecordingSession{ExtendedMetadata: map[string]string{}}
	callState := &CallState{RecordingSession: session}

	callID := "B2B.160.3333.4444"
	server.combineRecordingLegs(callID, callState, []string{leg0, leg1})

	expectedName := security.SanitizeCallUUID(callID)
	expectedPath := filepath.Join(dir, fmt.Sprintf("%s.wav", expectedName))
	if _, err := os.Stat(expectedPath); err == nil {
		t.Fatalf("combined file should not exist when disabled")
	}
	if _, ok := session.ExtendedMetadata["combined_recording_path"]; ok {
		t.Fatalf("metadata should not contain combined path when disabled")
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

func TestHandleSiprecReInviteUpdatesSession(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := &Handler{Logger: logger, Config: &Config{MediaConfig: &media.Config{}}}
	sipServer := NewCustomSIPServer(logger, handler)

	callID := "call-reinvite"
	forwarderA := &media.RTPForwarder{LocalPort: 20000, RTCPPort: 20001}
	forwarderB := &media.RTPForwarder{LocalPort: 21000, RTCPPort: 21001}
	callState := &CallState{
		CallID:           callID,
		State:            "connected",
		RecordingSession: &siprec.RecordingSession{ID: "session-1", RecordingState: "active", ExtendedMetadata: map[string]string{}},
		RTPForwarders:    []*media.RTPForwarder{forwarderA, forwarderB},
		StreamForwarders: map[string]*media.RTPForwarder{"leg0": forwarderA, "leg1": forwarderB},
	}

	sipServer.callMutex.Lock()
	sipServer.callStates[callID] = callState
	sipServer.callMutex.Unlock()

	sdp := "v=0\r\no=ATS99 399418590 399418590 IN IP4 192.168.22.133\r\ns=SipCall\r\nt=0 0\r\nm=audio 11584 RTP/AVP 8 108\r\nc=IN IP4 192.168.82.21\r\na=label:0\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:108 telephone-event/8000\r\na=sendonly\r\na=rtcp:11585\r\na=ptime:20\r\nm=audio 15682 RTP/AVP 8 108\r\nc=IN IP4 192.168.82.21\r\na=label:1\r\na=rtpmap:8 PCMA/8000\r\na=rtpmap:108 telephone-event/8000\r\na=sendonly\r\na=rtcp:15683\r\na=ptime:20\r\n"
	metadata := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="session-1" state="active" sequence="3">
  <session session_id="session-1">
    <sipSessionID>session-1@test</sipSessionID>
  </session>
  <sessionrecordingassoc sessionid="session-1"/>
  <participant participant_id="p1">
    <aor>sip:alice@example.com</aor>
  </participant>
  <stream label="0" streamid="stream-0" type="audio"/>
  <stream label="1" streamid="stream-1" type="audio"/>
</recording>`
	boundary := "OSS-unique-boundary-42"
	body := fmt.Sprintf("--%s\r\nContent-Type: application/sdp\r\n\r\n%s\r\n--%s\r\nContent-Type: application/rs-metadata+xml\r\n\r\n%s\r\n--%s--\r\n", boundary, sdp, boundary, metadata, boundary)

	req := sipparser.NewRequest(sipparser.INVITE, sipparser.Uri{Host: "recorder"})
	req.AppendHeader(sipparser.NewHeader("Via", "SIP/2.0/UDP 192.0.2.1;branch=z9hG4bK-reinvite"))
	req.AppendHeader(sipparser.NewHeader("From", "<sip:src@example.com>;tag=src"))
	req.AppendHeader(sipparser.NewHeader("To", "<sip:dst@example.com>;tag=dst"))
	req.AppendHeader(sipparser.NewHeader("Call-ID", callID))
	req.AppendHeader(sipparser.NewHeader("CSeq", "4 INVITE"))
	req.AppendHeader(sipparser.NewHeader("Contact", "<sip:src@example.com>"))

	tx := newTestServerTransaction(req)
	message := &SIPMessage{
		Method:      "INVITE",
		CallID:      callID,
		CSeq:        "4 INVITE",
		ContentType: fmt.Sprintf("multipart/mixed; boundary=%s", boundary),
		Body:        []byte(body),
		Request:     req,
		Parsed:      req,
		Transaction: tx,
	}

	sipServer.handleSiprecReInvite(message, callState)
	require.NotEmpty(t, tx.responses)
	require.Equal(t, 200, tx.responses[len(tx.responses)-1].StatusCode)
	require.Equal(t, []string{"audio", "audio"}, callState.RecordingSession.MediaStreamTypes)
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
		Sessions: []siprec.RSSession{
			{ID: "sess-1"},
		},
		Participants: []siprec.RSParticipant{
			{
				ID:  "participant-1",
				Aor: []siprec.Aor{{Value: "sip:alice@example.com"}},
			},
		},
		Streams: []siprec.Stream{
			{Label: "0", StreamID: "stream-0", Type: "audio"},
		},
		SessionRecordingAssoc: siprec.RSAssociation{
			SessionID: "sess-1",
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
	require.Equal(t, 400, last.StatusCode)
}

func TestHandleSiprecInitialInviteSuccess(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := &Handler{
		Logger: logger,
		Config: &Config{
			MediaConfig: &media.Config{
				RTPPortMin: 10000,
				RTPPortMax: 20000,
			},
		},
	}
	sipServer := NewCustomSIPServer(logger, handler)

	callID := "call-invite-initial-success"
	boundary := "OSS-unique-boundary-42"

	sdp := "v=0\r\no=test 1 1 IN IP4 127.0.0.1\r\ns=Test\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 1234 RTP/AVP 0\r\na=sendonly\r\na=label:0\r\n"

	metadata := `<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1" session="sess-1" state="active">
  <session session_id="sess-1">
    <sipSessionID>session-1@test</sipSessionID>
  </session>
  <participant participant_id="p1">
    <aor>sip:alice@example.com</aor>
    <name>Alice</name>
  </participant>
  <stream stream_id="str-1" label="0">
    <label>0</label>
  </stream>
  <sessionrecordingassoc session_id="sess-1"/>
</recording>`

	body := fmt.Sprintf("--%s\r\nContent-Type: application/sdp\r\n\r\n%s\r\n--%s\r\nContent-Type: application/rs-metadata+xml\r\n\r\n%s\r\n--%s--\r\n", boundary, sdp, boundary, metadata, boundary)

	req := sipparser.NewRequest(sipparser.INVITE, sipparser.Uri{Host: "recorder"})
	req.AppendHeader(sipparser.NewHeader("Via", "SIP/2.0/UDP 127.0.0.1;branch=z9hG4bK-test"))
	req.AppendHeader(sipparser.NewHeader("From", "<sip:src@example.com>;tag=src"))
	req.AppendHeader(sipparser.NewHeader("To", "<sip:dst@example.com>"))
	req.AppendHeader(sipparser.NewHeader("Call-ID", callID))
	req.AppendHeader(sipparser.NewHeader("CSeq", "1 INVITE"))
	req.AppendHeader(sipparser.NewHeader("Contact", "<sip:src@example.com>"))

	tx := newTestServerTransaction(req)
	message := &SIPMessage{
		Method:      "INVITE",
		CallID:      callID,
		CSeq:        "1 INVITE",
		ContentType: fmt.Sprintf("multipart/mixed; boundary=%s", boundary),
		Body:        []byte(body),
		Request:     req,
		Parsed:      req,
		Transaction: tx,
	}

	sipServer.handleSiprecInvite(message)

	// Check response
	require.NotEmpty(t, tx.responses)
	last := tx.responses[len(tx.responses)-1]
	require.Equal(t, 200, last.StatusCode)

	// Verify session state was created
	sipServer.callMutex.RLock()
	state, exists := sipServer.callStates[callID]
	sipServer.callMutex.RUnlock()
	require.True(t, exists, "Call state should exist")
	require.NotNil(t, state.RecordingSession, "Recording session should be populated")
	require.Equal(t, "sess-1", state.RecordingSession.ID)
	require.Equal(t, "active", state.RecordingSession.RecordingState)
	require.Len(t, state.RecordingSession.Participants, 1)
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
