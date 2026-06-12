package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

const (
	// maxInboundMessageSize is the maximum size of an incoming WebSocket message in bytes
	maxInboundMessageSize = 1024

	// maxProtocolViolations is the number of malformed messages tolerated before closing the connection
	maxProtocolViolations = 3

	// maxCallUUIDLength is the maximum accepted length of a call UUID in client messages
	maxCallUUIDLength = 128
)

// TranscriptionMessage represents a real-time transcription update
type TranscriptionMessage struct {
	CallUUID      string                 `json:"call_uuid"`
	Transcription string                 `json:"transcription"`
	IsFinal       bool                   `json:"is_final"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ClientMessage is the JSON protocol for messages sent by WebSocket clients.
// Supported types: "subscribe", "unsubscribe" (both require call_uuid) and "ping".
type ClientMessage struct {
	Type     string `json:"type"`
	CallUUID string `json:"call_uuid,omitempty"`
}

// ServerMessage is the JSON protocol for control responses sent to WebSocket clients.
type ServerMessage struct {
	Type     string `json:"type"`
	CallUUID string `json:"call_uuid,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Client represents a connected WebSocket client
type Client struct {
	hub      *TranscriptionHub
	conn     *websocket.Conn
	send     chan []byte
	logger   *logrus.Logger
	callUUID string // If client subscribes to a specific call
}

// TranscriptionHub manages WebSocket clients and broadcasts messages
type TranscriptionHub struct {
	logger          *logrus.Logger
	clients         map[*Client]bool
	callSubscribers map[string]map[*Client]bool
	broadcast       chan *TranscriptionMessage
	register        chan *Client
	unregister      chan *Client
	mutex           sync.RWMutex
	running         bool
}

// WebSocketUpgrader configures the WebSocket connection
var WebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return isSameOrigin(r)
	},
}

// NewTranscriptionHub creates a new transcription hub
func NewTranscriptionHub(logger *logrus.Logger) *TranscriptionHub {
	return &TranscriptionHub{
		logger:          logger,
		clients:         make(map[*Client]bool),
		callSubscribers: make(map[string]map[*Client]bool),
		broadcast:       make(chan *TranscriptionMessage, 256), // Buffered to prevent blocking
		register:        make(chan *Client),
		unregister:      make(chan *Client),
	}
}

// Run starts the transcription hub
func (h *TranscriptionHub) Run(ctx context.Context) {
	h.logger.Info("Starting WebSocket transcription hub")
	h.setRunning(true)

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Shutting down WebSocket transcription hub")
			h.setRunning(false)
			return

		case client := <-h.register:
			h.mutex.Lock()
			h.clients[client] = true

			// If client subscribes to a specific call
			if client.callUUID != "" {
				if _, exists := h.callSubscribers[client.callUUID]; !exists {
					h.callSubscribers[client.callUUID] = make(map[*Client]bool)
				}
				h.callSubscribers[client.callUUID][client] = true
				h.logger.WithFields(logrus.Fields{
					"call_uuid": client.callUUID,
				}).Info("Client subscribed to specific call")
			}

			h.mutex.Unlock()
			h.logger.Info("Client connected to WebSocket")

		case client := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)

				// Remove from call subscribers if needed
				h.removeSubscriptionLocked(client)

				h.logger.Info("Client disconnected from WebSocket")
			}
			h.mutex.Unlock()

		case message := <-h.broadcast:
			// Marshal message to JSON
			data, err := json.Marshal(message)
			if err != nil {
				h.logger.WithError(err).Error("Failed to marshal transcription message")
				continue
			}

			callSubscribers := h.getCallSubscribers(message.CallUUID)
			clients := h.getBroadcastClients()

			h.sendToClients(data, callSubscribers)
			h.sendToClients(data, clients)
		}
	}
}

// BroadcastTranscription sends a transcription to all relevant clients
func (h *TranscriptionHub) BroadcastTranscription(message interface{}) {
	// Convert to TranscriptionMessage if it's not already
	var typedMessage *TranscriptionMessage

	switch msg := message.(type) {
	case *TranscriptionMessage:
		typedMessage = msg
	case TranscriptionMessage:
		typedMessage = &msg
	default:
		// Try to convert from generic structure
		if msg, ok := message.(map[string]interface{}); ok {
			typedMessage = mapToTranscriptionMessage(msg)
		} else {
			// Use reflection to extract fields
			val := reflect.ValueOf(message)
			if val.Kind() == reflect.Ptr {
				val = val.Elem()
			}

			if val.Kind() == reflect.Struct {
				typedMessage = &TranscriptionMessage{
					Timestamp: time.Now(),
				}

				// Try to extract common fields
				if callUUIDField := val.FieldByName("CallUUID"); callUUIDField.IsValid() {
					typedMessage.CallUUID = callUUIDField.String()
				}

				if transcriptionField := val.FieldByName("Transcription"); transcriptionField.IsValid() {
					typedMessage.Transcription = transcriptionField.String()
				}

				if isFinalField := val.FieldByName("IsFinal"); isFinalField.IsValid() {
					typedMessage.IsFinal = isFinalField.Bool()
				}

				if timestampField := val.FieldByName("Timestamp"); timestampField.IsValid() {
					if timestampField.Type().String() == "time.Time" {
						typedMessage.Timestamp = timestampField.Interface().(time.Time)
					}
				}

				if metadataField := val.FieldByName("Metadata"); metadataField.IsValid() {
					if metadataField.Type().String() == "map[string]interface {}" {
						typedMessage.Metadata = metadataField.Interface().(map[string]interface{})
					}
				}
			}
		}
	}

	if typedMessage == nil {
		return // Could not convert message
	}

	h.broadcast <- typedMessage
}

// ServeWs handles WebSocket requests from clients
func (h *TranscriptionHub) ServeWs(w http.ResponseWriter, r *http.Request) {
	responseHeaders := http.Header{}
	if protocol := getWebSocketToken(r); protocol != "" {
		responseHeaders.Set("Sec-WebSocket-Protocol", protocol)
	}

	// Upgrade HTTP connection to WebSocket
	conn, err := WebSocketUpgrader.Upgrade(w, r, responseHeaders)
	if err != nil {
		h.logger.WithError(err).Error("Failed to upgrade connection to WebSocket")
		return
	}

	// Get call UUID from query parameter (optional)
	callUUID := r.URL.Query().Get("call_uuid")

	// Create new client
	client := &Client{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 256),
		logger:   h.logger,
		callUUID: callUUID,
	}

	// Register client with hub
	client.hub.register <- client

	// Start both read and write pumps to handle connection properly
	go client.writePump()
	go client.readPump()
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(60 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// The hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			// Send ping to keep connection alive
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump handles incoming messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	// Set read limits and timeouts
	c.conn.SetReadLimit(maxInboundMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	violations := 0
	for {
		// Read message from client
		messageType, payload, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.WithError(err).Error("WebSocket unexpected close error")
			}
			break
		}

		if err := c.handleInboundMessage(messageType, payload); err != nil {
			violations++
			c.logger.WithError(err).WithFields(logrus.Fields{
				"violations":     violations,
				"max_violations": maxProtocolViolations,
			}).Warning("Malformed WebSocket message received")

			if violations >= maxProtocolViolations {
				c.logger.Warning("Closing WebSocket connection after repeated malformed messages")
				c.conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "too many malformed messages"),
					time.Now().Add(time.Second),
				)
				break
			}
		}
	}
}

// handleInboundMessage validates and dispatches a single message received from a client.
// It returns an error when the message is malformed or uses an unknown type so the
// caller can count protocol violations.
func (c *Client) handleInboundMessage(messageType int, payload []byte) error {
	if messageType != websocket.TextMessage {
		return fmt.Errorf("unsupported WebSocket message type: %d", messageType)
	}

	var msg ClientMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return fmt.Errorf("invalid JSON message: %w", err)
	}

	switch msg.Type {
	case "ping":
		c.enqueueControlMessage(&ServerMessage{Type: "pong"})
		return nil

	case "subscribe":
		if err := validateCallUUID(msg.CallUUID); err != nil {
			c.enqueueControlMessage(&ServerMessage{Type: "error", Error: err.Error()})
			return fmt.Errorf("invalid subscribe request: %w", err)
		}
		c.hub.SubscribeToCall(c, msg.CallUUID)
		c.enqueueControlMessage(&ServerMessage{Type: "subscribed", CallUUID: msg.CallUUID})
		return nil

	case "unsubscribe":
		if err := validateCallUUID(msg.CallUUID); err != nil {
			c.enqueueControlMessage(&ServerMessage{Type: "error", Error: err.Error()})
			return fmt.Errorf("invalid unsubscribe request: %w", err)
		}
		c.hub.UnsubscribeFromCall(c, msg.CallUUID)
		c.enqueueControlMessage(&ServerMessage{Type: "unsubscribed", CallUUID: msg.CallUUID})
		return nil

	case "":
		return fmt.Errorf("message missing required \"type\" field")

	default:
		return fmt.Errorf("unknown message type: %q", msg.Type)
	}
}

// enqueueControlMessage queues a control response for delivery to the client.
// Messages are dropped (with a log entry) if the client send buffer is full.
func (c *Client) enqueueControlMessage(msg *ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		c.logger.WithError(err).Error("Failed to marshal WebSocket control message")
		return
	}

	select {
	case c.send <- data:
	default:
		c.logger.WithField("type", msg.Type).Warning("Client send buffer full, dropping control message")
	}
}

// validateCallUUID validates a call UUID supplied by a client
func validateCallUUID(callUUID string) error {
	if callUUID == "" {
		return fmt.Errorf("call_uuid is required")
	}
	if len(callUUID) > maxCallUUIDLength {
		return fmt.Errorf("call_uuid exceeds maximum length of %d", maxCallUUIDLength)
	}
	if strings.IndexFunc(callUUID, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return fmt.Errorf("call_uuid contains invalid characters")
	}
	return nil
}

// IsRunning returns true if the hub is running
func (h *TranscriptionHub) IsRunning() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.running
}

func (h *TranscriptionHub) setRunning(running bool) {
	h.mutex.Lock()
	h.running = running
	h.mutex.Unlock()
}

func (h *TranscriptionHub) getCallSubscribers(callUUID string) []*Client {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	subscribers := h.callSubscribers[callUUID]
	if len(subscribers) == 0 {
		return nil
	}

	clients := make([]*Client, 0, len(subscribers))
	for client := range subscribers {
		clients = append(clients, client)
	}
	return clients
}

func (h *TranscriptionHub) getBroadcastClients() []*Client {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.callUUID != "" {
			continue
		}
		clients = append(clients, client)
	}
	return clients
}

func (h *TranscriptionHub) sendToClients(payload []byte, clients []*Client) {
	if len(clients) == 0 {
		return
	}

	for _, client := range clients {
		select {
		case client.send <- payload:
		default:
			h.removeClient(client)
		}
	}
}

func (h *TranscriptionHub) removeClient(client *Client) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if _, ok := h.clients[client]; !ok {
		return
	}

	delete(h.clients, client)
	close(client.send)

	h.removeSubscriptionLocked(client)
}

// SubscribeToCall subscribes a client to transcription updates for a specific call.
// A client can be subscribed to one call at a time; subscribing replaces any
// previous subscription.
func (h *TranscriptionHub) SubscribeToCall(client *Client, callUUID string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if client.callUUID == callUUID {
		return
	}

	// Replace any existing subscription
	h.removeSubscriptionLocked(client)
	client.callUUID = callUUID

	if _, exists := h.callSubscribers[callUUID]; !exists {
		h.callSubscribers[callUUID] = make(map[*Client]bool)
	}
	h.callSubscribers[callUUID][client] = true

	h.logger.WithField("call_uuid", callUUID).Info("Client subscribed to specific call")
}

// UnsubscribeFromCall removes a client's subscription to a specific call. The client
// reverts to receiving broadcast transcriptions for all calls.
func (h *TranscriptionHub) UnsubscribeFromCall(client *Client, callUUID string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if client.callUUID != callUUID {
		return
	}

	h.removeSubscriptionLocked(client)
	client.callUUID = ""

	h.logger.WithField("call_uuid", callUUID).Info("Client unsubscribed from specific call")
}

// removeSubscriptionLocked removes the client from the call subscriber index.
// The hub mutex must be held by the caller.
func (h *TranscriptionHub) removeSubscriptionLocked(client *Client) {
	if client.callUUID == "" {
		return
	}

	if subscribers, exists := h.callSubscribers[client.callUUID]; exists {
		delete(subscribers, client)
		if len(subscribers) == 0 {
			delete(h.callSubscribers, client.callUUID)
		}
	}
}

func mapToTranscriptionMessage(msg map[string]interface{}) *TranscriptionMessage {
	callUUID, ok := msg["call_uuid"].(string)
	if !ok || callUUID == "" {
		return nil
	}

	transcription, ok := msg["transcription"].(string)
	if !ok {
		transcription = ""
	}

	isFinal, ok := msg["is_final"].(bool)
	if !ok {
		isFinal = false
	}

	typedMessage := &TranscriptionMessage{
		CallUUID:      callUUID,
		Transcription: transcription,
		IsFinal:       isFinal,
		Timestamp:     time.Now(),
	}

	if ts, ok := msg["timestamp"].(time.Time); ok {
		typedMessage.Timestamp = ts
	}

	if meta, ok := msg["metadata"].(map[string]interface{}); ok {
		typedMessage.Metadata = meta
	}

	return typedMessage
}

func isSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	host := requestHost(r)
	return strings.EqualFold(parsed.Host, host)
}

func requestHost(r *http.Request) string {
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		if idx := strings.Index(forwardedHost, ","); idx > 0 {
			return strings.TrimSpace(forwardedHost[:idx])
		}
		return strings.TrimSpace(forwardedHost)
	}
	return r.Host
}
