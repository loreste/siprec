package http

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"siprec-server/pkg/realtime/analytics"
)

// AnalyticsWebSocketHandler handles WebSocket connections for real-time analytics streaming
type AnalyticsWebSocketHandler struct {
	logger       *logrus.Logger
	upgrader     websocket.Upgrader
	clients      map[*AnalyticsClient]bool
	clientsMu    sync.RWMutex
	register     chan *AnalyticsClient
	unregister   chan *AnalyticsClient
	broadcast    chan *AnalyticsMessage
	pingInterval time.Duration // Configurable ping interval for testing
}

// AnalyticsClient represents a connected WebSocket client
type AnalyticsClient struct {
	conn      *websocket.Conn
	send      chan []byte
	handler   *AnalyticsWebSocketHandler
	callID    string // Optional: filter by specific call
	sessionID string
	mu        sync.RWMutex
}

// AnalyticsMessage represents a message to broadcast
type AnalyticsMessage struct {
	Type      string                       `json:"type"`
	CallID    string                       `json:"call_id"`
	Timestamp time.Time                    `json:"timestamp"`
	Data      *analytics.AnalyticsSnapshot `json:"data,omitempty"`
	Event     interface{}                  `json:"event,omitempty"`
}

// NewAnalyticsWebSocketHandler creates a new analytics WebSocket handler
func NewAnalyticsWebSocketHandler(logger *logrus.Logger) *AnalyticsWebSocketHandler {
	return &AnalyticsWebSocketHandler{
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return isSameOrigin(r)
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		clients:      make(map[*AnalyticsClient]bool),
		register:     make(chan *AnalyticsClient),
		unregister:   make(chan *AnalyticsClient),
		broadcast:    make(chan *AnalyticsMessage, 256),
		pingInterval: 54 * time.Second, // Default ping interval
	}
}

// Start begins the WebSocket handler's event loop
func (h *AnalyticsWebSocketHandler) Start() {
	go h.run()
}

// run manages client connections and message broadcasting
func (h *AnalyticsWebSocketHandler) run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			h.clients[client] = true
			h.clientsMu.Unlock()
			client.mu.RLock()
			callID := client.callID
			client.mu.RUnlock()
			h.logger.WithFields(logrus.Fields{
				"session_id": client.sessionID,
				"call_id":    callID,
			}).Debug("Analytics WebSocket client registered")

		case client := <-h.unregister:
			h.cleanupClients([]*AnalyticsClient{client})

		case message := <-h.broadcast:
			stale := h.broadcastMessage(message)
			if len(stale) > 0 {
				h.cleanupClients(stale)
			}

		case <-ticker.C:
			// Send ping to all clients
			stale := h.sendPingToAll()
			if len(stale) > 0 {
				h.cleanupClients(stale)
			}
		}
	}
}

// broadcastMessage sends a message to all appropriate clients
func (h *AnalyticsWebSocketHandler) broadcastMessage(message *AnalyticsMessage) []*AnalyticsClient {
	if message == nil {
		return nil
	}
	
	data, err := json.Marshal(message)
	if err != nil {
		h.logger.WithError(err).Error("Failed to marshal analytics message")
		return nil
	}

	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	var stale []*AnalyticsClient
	for client := range h.clients {
		client.mu.RLock()
		callID := client.callID
		client.mu.RUnlock()

		// Filter by call ID if client has specified one
		if callID != "" && callID != message.CallID {
			continue
		}

		select {
		case client.send <- data:
		default:
			stale = append(stale, client)
		}
	}

	return stale
}

// sendPingToAll sends a ping message to all connected clients
func (h *AnalyticsWebSocketHandler) sendPingToAll() []*AnalyticsClient {
	ping := &AnalyticsMessage{
		Type:      "ping",
		Timestamp: time.Now(),
	}

	data, _ := json.Marshal(ping)

	h.clientsMu.RLock()
	clients := make([]*AnalyticsClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.clientsMu.RUnlock()

	var stale []*AnalyticsClient
	for _, client := range clients {
		select {
		case client.send <- data:
		default:
			stale = append(stale, client)
		}
	}

	return stale
}

// cleanupClients removes clients and closes their channels
func (h *AnalyticsWebSocketHandler) cleanupClients(clients []*AnalyticsClient) {
	if len(clients) == 0 {
		return
	}

	h.clientsMu.Lock()
	for _, client := range clients {
		if _, ok := h.clients[client]; ok {
			delete(h.clients, client)
			close(client.send)
			h.logger.WithField("session_id", client.sessionID).Debug("Analytics WebSocket client unregistered")
		}
	}
	h.clientsMu.Unlock()
}

// ServeHTTP handles WebSocket upgrade requests
func (h *AnalyticsWebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	responseHeaders := http.Header{}
	if protocol := getWebSocketToken(r); protocol != "" {
		responseHeaders.Set("Sec-WebSocket-Protocol", protocol)
	}

	conn, err := h.upgrader.Upgrade(w, r, responseHeaders)
	if err != nil {
		h.logger.WithError(err).Error("Failed to upgrade to WebSocket")
		return
	}

	// Extract call ID filter from query params
	callID := r.URL.Query().Get("call_id")
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	client := &AnalyticsClient{
		conn:      conn,
		send:      make(chan []byte, 256),
		handler:   h,
		callID:    callID,
		sessionID: sessionID,
	}

	h.register <- client

	// Send welcome message
	welcome := &AnalyticsMessage{
		Type:      "connected",
		Timestamp: time.Now(),
		Event: map[string]interface{}{
			"session_id": sessionID,
			"version":    "1.0",
			"features":   []string{"sentiment", "keywords", "compliance", "metrics"},
		},
	}
	if data, err := json.Marshal(welcome); err == nil {
		client.send <- data
	}

	// Start client goroutines
	go client.writePump()
	go client.readPump()
}

// BroadcastAnalytics sends analytics snapshot to all connected clients
func (h *AnalyticsWebSocketHandler) BroadcastAnalytics(callID string, snapshot *analytics.AnalyticsSnapshot) {
	if snapshot == nil {
		return
	}

	message := &AnalyticsMessage{
		Type:      "analytics_update",
		CallID:    callID,
		Timestamp: time.Now(),
		Data:      snapshot,
	}

	select {
	case h.broadcast <- message:
	default:
		// Broadcast channel full, log and drop
		h.logger.Warn("Analytics broadcast channel full, dropping message")
	}
}

// BroadcastEvent sends a custom event to all connected clients
func (h *AnalyticsWebSocketHandler) BroadcastEvent(callID string, eventType string, event interface{}) {
	message := &AnalyticsMessage{
		Type:      eventType,
		CallID:    callID,
		Timestamp: time.Now(),
		Event:     event,
	}

	select {
	case h.broadcast <- message:
	default:
		h.logger.Warn("Analytics broadcast channel full, dropping event")
	}
}

// GetConnectedClients returns the number of connected clients
func (h *AnalyticsWebSocketHandler) GetConnectedClients() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

// Client methods

// readPump handles incoming messages from the client
func (c *AnalyticsClient) readPump() {
	defer func() {
		c.handler.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		// Read message from client
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.handler.logger.WithError(err).Debug("WebSocket read error")
			}
			break
		}

		// Handle client messages (subscriptions, filters, etc.)
		c.handleMessage(message)
	}
}

// writePump handles sending messages to the client
func (c *AnalyticsClient) writePump() {
	ticker := time.NewTicker(c.handler.pingInterval)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Channel closed
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current websocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage processes incoming client messages
func (c *AnalyticsClient) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		c.handler.logger.WithError(err).Debug("Failed to parse client message")
		return
	}

	msgType, _ := msg["type"].(string)

	switch msgType {
	case "subscribe":
		// Handle subscription to specific call
		if callID, ok := msg["call_id"].(string); ok {
			c.mu.Lock()
			c.callID = callID
			c.mu.Unlock()
			c.handler.logger.WithFields(logrus.Fields{
				"session_id": c.sessionID,
				"call_id":    callID,
			}).Debug("Client subscribed to call")
		}

	case "unsubscribe":
		// Clear call filter
		c.mu.Lock()
		c.callID = ""
		c.mu.Unlock()
		c.handler.logger.WithField("session_id", c.sessionID).Debug("Client unsubscribed from call")

	case "ping":
		// Respond with pong
		pong := map[string]interface{}{
			"type":      "pong",
			"timestamp": time.Now(),
		}
		if data, err := json.Marshal(pong); err == nil {
			select {
			case c.send <- data:
			default:
			}
		}

	default:
		c.handler.logger.WithField("type", msgType).Debug("Unknown message type from client")
	}
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	return time.Now().Format("20060102-150405") + "-" + randomString(8)
}

// randomString generates a random string of specified length
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
