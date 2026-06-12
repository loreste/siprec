package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func newProtocolTestClient(t *testing.T) (*TranscriptionHub, *Client) {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	hub := NewTranscriptionHub(logger)
	client := &Client{
		hub:    hub,
		send:   make(chan []byte, 8),
		logger: logger,
	}
	hub.clients[client] = true
	return hub, client
}

// readControlMessage reads a queued control message from the client send buffer
func readControlMessage(t *testing.T, client *Client) ServerMessage {
	t.Helper()

	select {
	case data := <-client.send:
		var msg ServerMessage
		assert.NoError(t, json.Unmarshal(data, &msg))
		return msg
	default:
		t.Fatal("expected control message in client send buffer")
		return ServerMessage{}
	}
}

func TestHandleInboundMessage_Ping(t *testing.T) {
	_, client := newProtocolTestClient(t)

	err := client.handleInboundMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	assert.NoError(t, err)

	msg := readControlMessage(t, client)
	assert.Equal(t, "pong", msg.Type)
}

func TestHandleInboundMessage_SubscribeUnsubscribe(t *testing.T) {
	hub, client := newProtocolTestClient(t)

	// Subscribe
	err := client.handleInboundMessage(websocket.TextMessage, []byte(`{"type":"subscribe","call_uuid":"call-1"}`))
	assert.NoError(t, err)
	assert.Equal(t, "call-1", client.callUUID)
	assert.Len(t, hub.getCallSubscribers("call-1"), 1)

	msg := readControlMessage(t, client)
	assert.Equal(t, "subscribed", msg.Type)
	assert.Equal(t, "call-1", msg.CallUUID)

	// Subscribing to another call replaces the previous subscription
	err = client.handleInboundMessage(websocket.TextMessage, []byte(`{"type":"subscribe","call_uuid":"call-2"}`))
	assert.NoError(t, err)
	assert.Equal(t, "call-2", client.callUUID)
	assert.Empty(t, hub.getCallSubscribers("call-1"))
	assert.Len(t, hub.getCallSubscribers("call-2"), 1)
	readControlMessage(t, client)

	// Unsubscribing from a call the client is not subscribed to is a no-op
	err = client.handleInboundMessage(websocket.TextMessage, []byte(`{"type":"unsubscribe","call_uuid":"call-1"}`))
	assert.NoError(t, err)
	assert.Equal(t, "call-2", client.callUUID)
	readControlMessage(t, client)

	// Unsubscribe from the active call
	err = client.handleInboundMessage(websocket.TextMessage, []byte(`{"type":"unsubscribe","call_uuid":"call-2"}`))
	assert.NoError(t, err)
	assert.Empty(t, client.callUUID)
	assert.Empty(t, hub.getCallSubscribers("call-2"))

	msg = readControlMessage(t, client)
	assert.Equal(t, "unsubscribed", msg.Type)
	assert.Equal(t, "call-2", msg.CallUUID)
}

func TestHandleInboundMessage_Malformed(t *testing.T) {
	_, client := newProtocolTestClient(t)

	tests := []struct {
		name        string
		messageType int
		payload     string
	}{
		{"invalid JSON", websocket.TextMessage, "not json"},
		{"missing type", websocket.TextMessage, `{"call_uuid":"call-1"}`},
		{"unknown type", websocket.TextMessage, `{"type":"bogus"}`},
		{"binary message", websocket.BinaryMessage, `{"type":"ping"}`},
		{"subscribe without call_uuid", websocket.TextMessage, `{"type":"subscribe"}`},
		{"unsubscribe without call_uuid", websocket.TextMessage, `{"type":"unsubscribe"}`},
		{"call_uuid with control characters", websocket.TextMessage, `{"type":"subscribe","call_uuid":"a\nb"}`},
		{"call_uuid too long", websocket.TextMessage, `{"type":"subscribe","call_uuid":"` + strings.Repeat("a", maxCallUUIDLength+1) + `"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := client.handleInboundMessage(tc.messageType, []byte(tc.payload))
			assert.Error(t, err)
		})
	}

	// No subscription side effects should have occurred
	assert.Empty(t, client.callUUID)
}

// startWebSocketTestServer starts a hub and an HTTP test server serving WebSocket connections
func startWebSocketTestServer(t *testing.T) *websocket.Conn {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	hub := NewTranscriptionHub(logger)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go hub.Run(ctx)

	server := httptest.NewServer(http.HandlerFunc(hub.ServeWs))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	assert.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return conn
}

// readServerMessages reads one WebSocket frame and splits batched messages
func readServerMessages(t *testing.T, conn *websocket.Conn) []ServerMessage {
	t.Helper()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	assert.NoError(t, err)

	var messages []ServerMessage
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var msg ServerMessage
		assert.NoError(t, json.Unmarshal([]byte(line), &msg))
		messages = append(messages, msg)
	}
	return messages
}

func TestWebSocketProtocol_EndToEnd(t *testing.T) {
	conn := startWebSocketTestServer(t)

	// Ping should produce a pong
	err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`))
	assert.NoError(t, err)

	messages := readServerMessages(t, conn)
	assert.NotEmpty(t, messages)
	assert.Equal(t, "pong", messages[0].Type)

	// Subscribe should be acknowledged
	err = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"subscribe","call_uuid":"call-e2e"}`))
	assert.NoError(t, err)

	messages = readServerMessages(t, conn)
	assert.NotEmpty(t, messages)
	assert.Equal(t, "subscribed", messages[0].Type)
	assert.Equal(t, "call-e2e", messages[0].CallUUID)
}

func TestWebSocketProtocol_MalformedMessagesCloseConnection(t *testing.T) {
	conn := startWebSocketTestServer(t)

	for i := 0; i < maxProtocolViolations; i++ {
		err := conn.WriteMessage(websocket.TextMessage, []byte("not json"))
		assert.NoError(t, err)
	}

	// The server should close the connection with a policy violation
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			assert.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation),
				"expected policy violation close, got: %v", err)
			return
		}
	}
}
