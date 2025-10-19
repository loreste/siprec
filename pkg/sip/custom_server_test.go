package sip

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"

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
