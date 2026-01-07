package http

import (
	"net/http"

	"github.com/sirupsen/logrus"
)

// WebSocketHandler handles WebSocket connections for real-time transcription streaming
type WebSocketHandler struct {
	logger         *logrus.Logger
	hub            *TranscriptionHub
	authMiddleware *AuthMiddleware
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(logger *logrus.Logger, hub *TranscriptionHub) *WebSocketHandler {
	return &WebSocketHandler{
		logger: logger,
		hub:    hub,
	}
}

// SetAuthMiddleware sets the authentication middleware
func (h *WebSocketHandler) SetAuthMiddleware(auth *AuthMiddleware) {
	h.authMiddleware = auth
}

// RegisterHandlers registers WebSocket handlers with the HTTP server
func (h *WebSocketHandler) RegisterHandlers(server *Server) {
	server.RegisterHandler("/ws/transcriptions", h.handleTranscriptionWS)

	// API endpoint that serves the WebSocket client HTML page
	server.RegisterHandler("/websocket-client", h.handleWebSocketClientPage)

	h.logger.Info("Registered WebSocket handlers")
}

// handleTranscriptionWS handles WebSocket connections for real-time transcription streaming
func (h *WebSocketHandler) handleTranscriptionWS(w http.ResponseWriter, r *http.Request) {
	h.logger.WithField("remote_addr", r.RemoteAddr).Info("WebSocket connection request received")

	// Authenticate if auth middleware is configured
	if h.authMiddleware != nil {
		userInfo, err := h.authMiddleware.WebSocketAuth(r)
		if err != nil {
			h.logger.WithError(err).WithField("remote_addr", r.RemoteAddr).Warning("WebSocket authentication failed")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Log authenticated connection
		h.logger.WithFields(logrus.Fields{
			"remote_addr": r.RemoteAddr,
			"user_id":     userInfo.UserID,
			"username":    userInfo.Username,
			"role":        userInfo.Role,
		}).Info("Authenticated WebSocket connection")
	}

	// Let the hub serve the WebSocket connection
	h.hub.ServeWs(w, r)
}

// handleWebSocketClientPage serves a simple HTML page with a WebSocket client
func (h *WebSocketHandler) handleWebSocketClientPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)

	html := `
<!DOCTYPE html>
<html>
<head>
    <title>SIPREC Transcription WebSocket Client</title>
    <style>
        body {
            font-family: sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
        }
        h1 {
            color: #333;
        }
        .controls {
            margin: 20px 0;
            padding: 15px;
            background: #f5f5f5;
            border-radius: 5px;
        }
        #callUUID {
            padding: 8px;
            width: 250px;
        }
        button {
            padding: 8px 15px;
            background: #4CAF50;
            color: white;
            border: none;
            border-radius: 4px;
            cursor: pointer;
        }
        button:hover {
            background: #45a049;
        }
        #status {
            margin: 10px 0;
            padding: 10px;
            border-radius: 4px;
        }
        .connected {
            background: #d4edda;
            color: #155724;
        }
        .disconnected {
            background: #f8d7da;
            color: #721c24;
        }
        .connecting {
            background: #fff3cd;
            color: #856404;
        }
        .transcription {
            margin: 10px 0;
            padding: 15px;
            border-radius: 4px;
            border-left: 4px solid #ccc;
            background: #f9f9f9;
        }
        .interim {
            border-left-color: #ffc107;
            background: #fffbf0;
        }
        .final {
            border-left-color: #28a745;
            background: #f0fff4;
        }
        .metadata {
            font-size: 0.8em;
            color: #666;
            margin-top: 5px;
        }
        #transcriptions {
            max-height: 600px;
            overflow-y: auto;
            border: 1px solid #ddd;
            border-radius: 4px;
            padding: 10px;
        }
    </style>
</head>
<body>
    <h1>SIPREC Transcription WebSocket Client</h1>
    
    <div class="controls">
        <p>Connect to receive real-time transcriptions. Optionally specify a Call UUID to get transcriptions for a specific call only.</p>
        <input type="text" id="callUUID" placeholder="Call UUID (optional)">
        <input type="text" id="authToken" placeholder="Auth Token (required - sent via WebSocket subprotocol)">
        <button id="connect">Connect</button>
        <button id="disconnect" disabled>Disconnect</button>
        <div id="status" class="disconnected">Disconnected</div>
    </div>
    
    <h2>Transcriptions</h2>
    <button id="clear">Clear Transcriptions</button>
    <div id="transcriptions"></div>
    
    <script>
        let socket;
        const statusEl = document.getElementById('status');
        const transcriptionsEl = document.getElementById('transcriptions');
        const connectButton = document.getElementById('connect');
        const disconnectButton = document.getElementById('disconnect');
        const callUUIDInput = document.getElementById('callUUID');
        const authTokenInput = document.getElementById('authToken');
        const clearButton = document.getElementById('clear');
        
        function updateStatus(message, type) {
            statusEl.textContent = message;
            statusEl.className = type;
        }
        
        function connect() {
            // Get the WebSocket URL
            const callUUID = callUUIDInput.value.trim();
            const authToken = authTokenInput.value.trim();
            let wsUrl = window.location.origin.replace('http', 'ws') + '/ws/transcriptions';

            const params = new URLSearchParams();
            if (callUUID) {
                params.append('call_uuid', callUUID);
            }

            if (params.toString()) {
                wsUrl += '?' + params.toString();
            }

            // Send the auth token via WebSocket subprotocol to avoid leaking it in URLs
            const protocols = authToken ? [authToken] : [];

            // Create WebSocket connection
            updateStatus('Connecting...', 'connecting');
            socket = new WebSocket(wsUrl, protocols);
            
            // Connection opened
            socket.addEventListener('open', function(event) {
                updateStatus('Connected', 'connected');
                connectButton.disabled = true;
                disconnectButton.disabled = false;
            });
            
            // Listen for messages
            socket.addEventListener('message', function(event) {
                const data = JSON.parse(event.data);
                displayTranscription(data);
            });
            
            // Connection closed
            socket.addEventListener('close', function(event) {
                updateStatus('Disconnected', 'disconnected');
                connectButton.disabled = false;
                disconnectButton.disabled = true;
            });
            
            // Connection error
            socket.addEventListener('error', function(event) {
                console.error('WebSocket error:', event);
                updateStatus('Error: ' + event, 'disconnected');
                connectButton.disabled = false;
                disconnectButton.disabled = true;
            });
        }
        
        function disconnect() {
            if (socket) {
                socket.close();
            }
        }
        
        function displayTranscription(data) {
            const transcriptionDiv = document.createElement('div');
            transcriptionDiv.className = 'transcription ' + (data.is_final ? 'final' : 'interim');
            
            const textEl = document.createElement('div');
            textEl.textContent = data.transcription;
            transcriptionDiv.appendChild(textEl);
            
            const metadataEl = document.createElement('div');
            metadataEl.className = 'metadata';
            metadataEl.textContent = 'Call UUID: ' + data.call_uuid + 
                ' | Type: ' + (data.is_final ? 'Final' : 'Interim') + 
                ' | Timestamp: ' + new Date(data.timestamp).toLocaleTimeString();
            
            if (data.metadata) {
                if (data.metadata.provider) {
                    metadataEl.textContent += ' | Provider: ' + data.metadata.provider;
                }
                if (data.metadata.confidence) {
                    metadataEl.textContent += ' | Confidence: ' + data.metadata.confidence.toFixed(2);
                }
            }
            
            transcriptionDiv.appendChild(metadataEl);
            
            transcriptionsEl.insertBefore(transcriptionDiv, transcriptionsEl.firstChild);
        }
        
        function clearTranscriptions() {
            transcriptionsEl.innerHTML = '';
        }
        
        // Attach event listeners
        connectButton.addEventListener('click', connect);
        disconnectButton.addEventListener('click', disconnect);
        clearButton.addEventListener('click', clearTranscriptions);
    </script>
</body>
</html>
`
	w.Write([]byte(html))
}
