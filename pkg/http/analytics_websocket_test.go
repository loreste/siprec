package http

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/realtime/analytics"
)

func TestAnalyticsWebSocketHandler_Connection(t *testing.T) {
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	t.Run("successful connection", func(t *testing.T) {
		ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer ws.Close()

		// Read welcome message
		var msg AnalyticsMessage
		err = ws.ReadJSON(&msg)
		assert.NoError(t, err)
		assert.Equal(t, "connected", msg.Type)
		assert.NotEmpty(t, msg.Event)
	})

	t.Run("multiple clients", func(t *testing.T) {
		clients := make([]*websocket.Conn, 3)
		
		// Connect multiple clients
		for i := range clients {
			ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			require.NoError(t, err)
			clients[i] = ws
			
			// Read welcome message
			var msg AnalyticsMessage
			err = ws.ReadJSON(&msg)
			assert.NoError(t, err)
		}
		
		// Verify all clients connected
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 3, handler.GetConnectedClients())
		
		// Close all clients
		for _, ws := range clients {
			ws.Close()
		}
		
		// Verify all disconnected
		time.Sleep(200 * time.Millisecond)
		assert.Equal(t, 0, handler.GetConnectedClients())
	})
}

func TestAnalyticsWebSocketHandler_BroadcastAnalytics(t *testing.T) {
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()

	server := httptest.NewServer(handler)
	defer server.Close()
	
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()

	// Skip welcome message
	var welcome AnalyticsMessage
	ws.ReadJSON(&welcome)

	// Broadcast analytics snapshot
	snapshot := &analytics.AnalyticsSnapshot{
		CallID:       "test-call-123",
		QualityScore: 0.85,
		Keywords:     []string{"test", "analytics"},
		SentimentTrend: []analytics.SentimentResult{
			{Score: 0.5, Label: "positive"},
		},
	}
	
	handler.BroadcastAnalytics("test-call-123", snapshot)
	
	// Read the broadcast message
	var msg AnalyticsMessage
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = ws.ReadJSON(&msg)
	assert.NoError(t, err)
	assert.Equal(t, "analytics_update", msg.Type)
	assert.Equal(t, "test-call-123", msg.CallID)
	assert.NotNil(t, msg.Data)
	assert.Equal(t, 0.85, msg.Data.QualityScore)
}

func TestAnalyticsWebSocketHandler_FilterByCallID(t *testing.T) {
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()

	server := httptest.NewServer(handler)
	defer server.Close()
	
	baseURL := "ws" + strings.TrimPrefix(server.URL, "http")
	
	// Connect client with call ID filter
	filteredURL := baseURL + "?call_id=specific-call"
	filteredWS, _, err := websocket.DefaultDialer.Dial(filteredURL, nil)
	require.NoError(t, err)
	defer filteredWS.Close()
	
	// Connect client without filter
	unfilteredWS, _, err := websocket.DefaultDialer.Dial(baseURL, nil)
	require.NoError(t, err)
	defer unfilteredWS.Close()
	
	// Skip welcome messages
	var welcome AnalyticsMessage
	filteredWS.ReadJSON(&welcome)
	unfilteredWS.ReadJSON(&welcome)
	
	// Broadcast for specific call
	snapshot := &analytics.AnalyticsSnapshot{
		CallID: "specific-call",
	}
	handler.BroadcastAnalytics("specific-call", snapshot)
	
	// Filtered client should receive
	var msg AnalyticsMessage
	filteredWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = filteredWS.ReadJSON(&msg)
	assert.NoError(t, err)
	assert.Equal(t, "specific-call", msg.CallID)
	
	// Unfiltered client should also receive
	unfilteredWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = unfilteredWS.ReadJSON(&msg)
	assert.NoError(t, err)
	
	// Broadcast for different call
	otherSnapshot := &analytics.AnalyticsSnapshot{
		CallID: "other-call",
	}
	handler.BroadcastAnalytics("other-call", otherSnapshot)
	
	// Filtered client should NOT receive
	filteredWS.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	err = filteredWS.ReadJSON(&msg)
	assert.Error(t, err) // Should timeout
	
	// Unfiltered client should receive
	unfilteredWS.SetReadDeadline(time.Now().Add(2 * time.Second))
	err = unfilteredWS.ReadJSON(&msg)
	assert.NoError(t, err)
	assert.Equal(t, "other-call", msg.CallID)
}

func TestAnalyticsWebSocketHandler_Events(t *testing.T) {
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()

	server := httptest.NewServer(handler)
	defer server.Close()
	
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	
	// Skip welcome message
	var welcome AnalyticsMessage
	ws.ReadJSON(&welcome)
	
	// Test different event types
	testCases := []struct {
		name      string
		eventType string
		event     interface{}
	}{
		{
			name:      "sentiment alert",
			eventType: "sentiment_alert",
			event: map[string]interface{}{
				"severity": "high",
				"score":    -0.8,
			},
		},
		{
			name:      "compliance violation",
			eventType: "compliance_violation",
			event: map[string]interface{}{
				"rule_id":  "rule1",
				"severity": "critical",
			},
		},
		{
			name:      "quality alert",
			eventType: "quality_alert",
			event: map[string]interface{}{
				"score":   0.3,
				"message": "Low quality",
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handler.BroadcastEvent("test-call", tc.eventType, tc.event)
			
			var msg AnalyticsMessage
			ws.SetReadDeadline(time.Now().Add(2 * time.Second))
			err := ws.ReadJSON(&msg)
			assert.NoError(t, err)
			assert.Equal(t, tc.eventType, msg.Type)
			assert.Equal(t, "test-call", msg.CallID)
			assert.NotNil(t, msg.Event)
		})
	}
}

func TestAnalyticsWebSocketHandler_ClientMessages(t *testing.T) {
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()

	server := httptest.NewServer(handler)
	defer server.Close()
	
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	
	// Skip welcome message
	var welcome AnalyticsMessage
	ws.ReadJSON(&welcome)
	
	t.Run("subscribe to call", func(t *testing.T) {
		msg := map[string]interface{}{
			"type":    "subscribe",
			"call_id": "subscribed-call",
		}
		err := ws.WriteJSON(msg)
		assert.NoError(t, err)
		
		// Verify subscription works by sending targeted message
		time.Sleep(100 * time.Millisecond)
		handler.BroadcastEvent("subscribed-call", "test", nil)
		
		var received AnalyticsMessage
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		err = ws.ReadJSON(&received)
		assert.NoError(t, err)
		assert.Equal(t, "subscribed-call", received.CallID)
	})
	
	t.Run("ping pong", func(t *testing.T) {
		ping := map[string]interface{}{
			"type": "ping",
		}
		err := ws.WriteJSON(ping)
		assert.NoError(t, err)
		
		var pong map[string]interface{}
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		err = ws.ReadJSON(&pong)
		assert.NoError(t, err)
		assert.Equal(t, "pong", pong["type"])
	})
}

func TestAnalyticsWebSocketHandler_Heartbeat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping heartbeat test in short mode")
	}
	
	logger := logrus.New()
	handler := NewAnalyticsWebSocketHandler(logger)
	
	// Set shorter ping interval for testing
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()
	
	server := httptest.NewServer(handler)
	defer server.Close()
	
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer ws.Close()
	
	// Set pong handler to track pings
	pingReceived := make(chan bool, 1)
	ws.SetPingHandler(func(appData string) error {
		pingReceived <- true
		return ws.WriteMessage(websocket.PongMessage, []byte{})
	})
	
	// Start read loop to handle pings
	go func() {
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}()
	
	// Wait for ping (WebSocket sends pings every 30 seconds)
	select {
	case <-pingReceived:
		// Success - received ping
		assert.True(t, true)
	case <-time.After(35 * time.Second):
		t.Fatal("No ping received within expected time")
	}
}

func BenchmarkAnalyticsWebSocketBroadcast(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	handler := NewAnalyticsWebSocketHandler(logger)
	handler.Start()
	defer func() {
		if handler.broadcast != nil {
			close(handler.broadcast)
		}
	}()
	
	// Connect multiple clients
	numClients := 100
	for i := 0; i < numClients; i++ {
		client := &AnalyticsClient{
			conn:      nil, // Mock connection
			send:      make(chan []byte, 256),
			handler:   handler,
			sessionID: fmt.Sprintf("client-%d", i),
		}
		handler.clients[client] = true
	}
	
	snapshot := &analytics.AnalyticsSnapshot{
		CallID:       "bench-call",
		QualityScore: 0.75,
		Keywords:     []string{"test", "benchmark"},
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		handler.BroadcastAnalytics("bench-call", snapshot)
	}
}