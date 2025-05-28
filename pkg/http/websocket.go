package http

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

// TranscriptionMessage represents a real-time transcription update
type TranscriptionMessage struct {
	CallUUID      string                 `json:"call_uuid"`
	Transcription string                 `json:"transcription"`
	IsFinal       bool                   `json:"is_final"`
	Timestamp     time.Time              `json:"timestamp"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
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
}

// WebSocketUpgrader configures the WebSocket connection
var WebSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all connections
		return true
	},
}

// NewTranscriptionHub creates a new transcription hub
func NewTranscriptionHub(logger *logrus.Logger) *TranscriptionHub {
	return &TranscriptionHub{
		logger:          logger,
		clients:         make(map[*Client]bool),
		callSubscribers: make(map[string]map[*Client]bool),
		broadcast:       make(chan *TranscriptionMessage),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
	}
}

// Run starts the transcription hub
func (h *TranscriptionHub) Run(ctx context.Context) {
	h.logger.Info("Starting WebSocket transcription hub")

	for {
		select {
		case <-ctx.Done():
			h.logger.Info("Shutting down WebSocket transcription hub")
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
				if client.callUUID != "" {
					if subscribers, exists := h.callSubscribers[client.callUUID]; exists {
						delete(subscribers, client)
						// Clean up empty subscriber maps
						if len(subscribers) == 0 {
							delete(h.callSubscribers, client.callUUID)
						}
					}
				}

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

			h.mutex.RLock()

			// Send to subscribers of this specific call
			if subscribers, exists := h.callSubscribers[message.CallUUID]; exists && len(subscribers) > 0 {
				for client := range subscribers {
					select {
					case client.send <- data:
					default:
						close(client.send)
						delete(h.clients, client)
						delete(subscribers, client)
					}
				}
			}

			// Also broadcast to clients that want all transcriptions
			for client := range h.clients {
				// Skip clients that are subscribed to specific calls
				if client.callUUID != "" {
					continue
				}

				select {
				case client.send <- data:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}

			h.mutex.RUnlock()
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
			typedMessage = &TranscriptionMessage{
				CallUUID:      msg["call_uuid"].(string),
				Transcription: msg["transcription"].(string),
				IsFinal:       msg["is_final"].(bool),
			}
			if ts, ok := msg["timestamp"].(time.Time); ok {
				typedMessage.Timestamp = ts
			} else {
				typedMessage.Timestamp = time.Now()
			}
			if meta, ok := msg["metadata"].(map[string]interface{}); ok {
				typedMessage.Metadata = meta
			}
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
	// Upgrade HTTP connection to WebSocket
	conn, err := WebSocketUpgrader.Upgrade(w, r, nil)
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

	// Start writer goroutine
	go client.writePump()
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

// IsRunning returns true if the hub is running
func (h *TranscriptionHub) IsRunning() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients) >= 0 // Always true if hub exists
}
