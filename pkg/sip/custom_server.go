package sip

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// CustomSIPServer is our own SIP server implementation
type CustomSIPServer struct {
	logger        *logrus.Logger
	handler       *Handler
	listeners     []net.Listener
	tlsListeners  []net.Listener
	wg            sync.WaitGroup
	shutdownCtx   context.Context
	shutdownFunc  context.CancelFunc
	
	// Connection tracking
	connections map[string]*SIPConnection
	connMutex   sync.RWMutex
	
	// Call state tracking
	callStates  map[string]*CallState
	callMutex   sync.RWMutex
}

// SIPConnection represents an active TCP/TLS connection
type SIPConnection struct {
	conn        net.Conn
	reader      *bufio.Reader
	writer      *bufio.Writer
	remoteAddr  string
	transport   string
	lastActivity time.Time
	mutex       sync.Mutex
}

// SIPMessage represents a parsed SIP message
type SIPMessage struct {
	Method      string
	RequestURI  string
	Version     string
	Headers     map[string][]string
	Body        []byte
	RawMessage  []byte
	Connection  *SIPConnection
	
	// Parsed SIP fields for easier access
	CallID      string
	FromTag     string
	ToTag       string
	CSeq        string
	Branch      string
	ContentType string
}

// CallState tracks the state of SIP calls
type CallState struct {
	CallID       string
	State        string // "initial", "trying", "ringing", "connected", "terminated"
	LocalTag     string
	RemoteTag    string
	LocalCSeq    int
	RemoteCSeq   int
	CreatedAt    time.Time
	LastActivity time.Time
	SDP          []byte
	IsRecording  bool
}

// NewCustomSIPServer creates a new custom SIP server
func NewCustomSIPServer(logger *logrus.Logger, handler *Handler) *CustomSIPServer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &CustomSIPServer{
		logger:       logger,
		handler:      handler,
		listeners:    make([]net.Listener, 0),
		tlsListeners: make([]net.Listener, 0),
		connections:  make(map[string]*SIPConnection),
		callStates:   make(map[string]*CallState),
		shutdownCtx:  ctx,
		shutdownFunc: cancel,
	}
}

// ListenAndServeUDP starts UDP listener
func (s *CustomSIPServer) ListenAndServeUDP(ctx context.Context, address string) error {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address %s: %w", address, err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP %s: %w", address, err)
	}
	defer conn.Close()

	s.logger.WithField("address", address).Info("Custom SIP server listening on UDP")

	// Handle UDP packets
	buffer := make([]byte, 65536) // Large buffer for UDP
	
	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("UDP listener shutting down")
			return nil
		default:
			// Set read timeout
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			
			n, clientAddr, err := conn.ReadFromUDP(buffer)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, check context again
				}
				s.logger.WithError(err).Error("UDP read error")
				continue
			}

			// Process UDP message
			go s.handleUDPMessage(conn, clientAddr, buffer[:n])
		}
	}
}

// ListenAndServeTCP starts TCP listener
func (s *CustomSIPServer) ListenAndServeTCP(ctx context.Context, address string) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on TCP %s: %w", address, err)
	}
	defer listener.Close()

	s.listeners = append(s.listeners, listener)
	s.logger.WithField("address", address).Info("Custom SIP server listening on TCP")

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("TCP listener shutting down")
			return nil
		default:
			// Set accept timeout
			if tcpListener, ok := listener.(*net.TCPListener); ok {
				tcpListener.SetDeadline(time.Now().Add(1 * time.Second))
			}

			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, check context again
				}
				s.logger.WithError(err).Error("TCP accept error")
				continue
			}

			// Handle TCP connection
			go s.handleTCPConnection(ctx, conn, "tcp")
		}
	}
}

// ListenAndServeTLS starts TLS listener
func (s *CustomSIPServer) ListenAndServeTLS(ctx context.Context, address string, tlsConfig *tls.Config) error {
	listener, err := tls.Listen("tcp", address, tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on TLS %s: %w", address, err)
	}
	defer listener.Close()

	s.tlsListeners = append(s.tlsListeners, listener)
	s.logger.WithField("address", address).Info("Custom SIP server listening on TLS")

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("TLS listener shutting down")
			return nil
		default:
			// Set accept timeout
			if tcpListener, ok := listener.(*net.TCPListener); ok {
				tcpListener.SetDeadline(time.Now().Add(1 * time.Second))
			}

			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue // Timeout is expected, check context again
				}
				s.logger.WithError(err).Error("TLS accept error")
				continue
			}

			// Handle TLS connection
			go s.handleTCPConnection(ctx, conn, "tls")
		}
	}
}

// handleUDPMessage processes UDP SIP messages
func (s *CustomSIPServer) handleUDPMessage(conn *net.UDPConn, clientAddr *net.UDPAddr, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.WithField("recover", r).Error("Recovered from panic in UDP message handler")
		}
	}()

	// Parse SIP message
	message, err := s.parseSIPMessage(data, nil)
	if err != nil {
		s.logger.WithError(err).WithField("client", clientAddr.String()).Error("Failed to parse UDP SIP message")
		return
	}

	message.Connection = &SIPConnection{
		conn:        &udpConn{conn: conn, addr: clientAddr},
		remoteAddr:  clientAddr.String(),
		transport:   "udp",
		lastActivity: time.Now(),
	}

	// Process the message
	s.processSIPMessage(message)
}

// handleTCPConnection handles TCP/TLS connections
func (s *CustomSIPServer) handleTCPConnection(ctx context.Context, conn net.Conn, transport string) {
	defer conn.Close()
	
	connID := fmt.Sprintf("%s-%s", transport, conn.RemoteAddr().String())
	
	sipConn := &SIPConnection{
		conn:        conn,
		reader:      bufio.NewReader(conn),
		writer:      bufio.NewWriter(conn),
		remoteAddr:  conn.RemoteAddr().String(),
		transport:   transport,
		lastActivity: time.Now(),
	}

	// Register connection
	s.connMutex.Lock()
	s.connections[connID] = sipConn
	s.connMutex.Unlock()

	defer func() {
		// Unregister connection
		s.connMutex.Lock()
		delete(s.connections, connID)
		s.connMutex.Unlock()
		
		if r := recover(); r != nil {
			s.logger.WithField("recover", r).Error("Recovered from panic in TCP connection handler")
		}
	}()

	s.logger.WithFields(logrus.Fields{
		"connection_id": connID,
		"transport":     transport,
		"remote_addr":   conn.RemoteAddr().String(),
	}).Debug("New TCP connection established")

	// Read SIP messages from connection
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Set read timeout
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))

			// Read SIP message
			message, err := s.readSIPMessageFromTCP(sipConn)
			if err != nil {
				if err == io.EOF {
					s.logger.WithField("connection_id", connID).Debug("TCP connection closed by client")
					return
				}
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Check for connection timeout
					if time.Since(sipConn.lastActivity) > 5*time.Minute {
						s.logger.WithField("connection_id", connID).Debug("TCP connection timed out")
						return
					}
					continue
				}
				s.logger.WithError(err).WithField("connection_id", connID).Error("Failed to read TCP SIP message")
				return
			}

			message.Connection = sipConn
			sipConn.lastActivity = time.Now()

			// Process the message
			go s.processSIPMessage(message)
		}
	}
}

// readSIPMessageFromTCP reads a complete SIP message from TCP connection
func (s *CustomSIPServer) readSIPMessageFromTCP(conn *SIPConnection) (*SIPMessage, error) {
	var buffer []byte
	var contentLength int = -1
	headerComplete := false

	// Starting to read SIP message from TCP - debug log removed for cleaner output

	// Read headers line by line
	for {
		line, err := conn.reader.ReadBytes('\n')
		if err != nil {
			s.logger.WithError(err).Debug("Error reading line from TCP connection")
			return nil, err
		}

		buffer = append(buffer, line...)

		// Convert to string for processing - handle both \r\n and \n endings
		lineStr := string(line)
		lineStr = strings.TrimRight(lineStr, "\r\n")
		
		// Debug log for message reading - only log short lines and important headers
		if len(lineStr) <= 50 || strings.Contains(strings.ToLower(lineStr), "content-length") {
			s.logger.WithField("line", lineStr).Debug("Read SIP header line")
		}

		// Check for end of headers
		if lineStr == "" {
			headerComplete = true
			s.logger.WithFields(logrus.Fields{
				"headers_size": len(buffer),
				"content_length": contentLength,
			}).Debug("Headers complete, reading body")
			break
		}

		// Parse Content-Length header
		if strings.HasPrefix(strings.ToLower(lineStr), "content-length:") {
			parts := strings.SplitN(lineStr, ":", 2)
			if len(parts) == 2 {
				if cl, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
					contentLength = cl
					s.logger.WithField("content_length", contentLength).Debug("Parsed Content-Length header")
				}
			}
		}
	}

	// Read body if Content-Length is specified
	if headerComplete && contentLength > 0 {
		s.logger.WithField("content_length", contentLength).Debug("Reading message body")
		
		body := make([]byte, contentLength)
		bytesRead, err := io.ReadFull(conn.reader, body)
		if err != nil {
			s.logger.WithError(err).WithFields(logrus.Fields{
				"expected": contentLength,
				"read": bytesRead,
			}).Error("Error reading message body")
			return nil, err
		}
		
		buffer = append(buffer, body...)
		s.logger.WithFields(logrus.Fields{
			"body_size": len(body),
			"total_message_size": len(buffer),
		}).Debug("Successfully read complete SIP message")
	}

	// Parse the complete message
	message, err := s.parseSIPMessage(buffer, conn)
	if err != nil {
		s.logger.WithError(err).WithField("message_size", len(buffer)).Error("Failed to parse SIP message")
		return nil, err
	}

	s.logger.WithFields(logrus.Fields{
		"method": message.Method,
		"message_size": len(buffer),
		"content_length": contentLength,
	}).Debug("Successfully parsed SIP message")

	return message, nil
}

// parseSIPMessage parses raw SIP message bytes
func (s *CustomSIPServer) parseSIPMessage(data []byte, conn *SIPConnection) (*SIPMessage, error) {
	message := &SIPMessage{
		Headers:    make(map[string][]string),
		RawMessage: data,
		Connection: conn,
	}

	lines := strings.Split(string(data), "\r\n")
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty SIP message")
	}

	// Parse request line
	requestLine := strings.TrimSpace(lines[0])
	parts := strings.Split(requestLine, " ")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid SIP request line: %s", requestLine)
	}

	message.Method = parts[0]
	message.RequestURI = parts[1]
	message.Version = parts[2]

	// Parse headers
	headerEnd := 1
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			headerEnd = i + 1
			break
		}

		if !strings.Contains(line, ":") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		headerName := strings.TrimSpace(strings.ToLower(parts[0]))
		headerValue := strings.TrimSpace(parts[1])

		if _, exists := message.Headers[headerName]; !exists {
			message.Headers[headerName] = make([]string, 0)
		}
		message.Headers[headerName] = append(message.Headers[headerName], headerValue)
	}

	// Extract body
	if headerEnd < len(lines) {
		bodyLines := lines[headerEnd:]
		bodyStr := strings.Join(bodyLines, "\r\n")
		message.Body = []byte(bodyStr)
	}

	// Parse common SIP fields for easier access
	message.CallID = s.getHeaderValue(message, "call-id")
	message.CSeq = s.getHeaderValue(message, "cseq")
	message.ContentType = s.getHeaderValue(message, "content-type")
	
	// Extract tags from From and To headers
	fromHeader := s.getHeaderValue(message, "from")
	if fromHeader != "" {
		message.FromTag = extractTag(fromHeader)
	}
	
	toHeader := s.getHeaderValue(message, "to")
	if toHeader != "" {
		message.ToTag = extractTag(toHeader)
	}
	
	// Extract branch from Via header
	viaHeader := s.getHeaderValue(message, "via")
	if viaHeader != "" {
		message.Branch = extractBranch(viaHeader)
	}

	return message, nil
}

// processSIPMessage processes a parsed SIP message
func (s *CustomSIPServer) processSIPMessage(message *SIPMessage) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.WithField("recover", r).Error("Recovered from panic in SIP message processor")
		}
	}()

	logger := s.logger.WithFields(logrus.Fields{
		"method":      message.Method,
		"request_uri": message.RequestURI,
		"transport":   message.Connection.transport,
		"remote_addr": message.Connection.remoteAddr,
		"message_size": len(message.RawMessage),
	})

	logger.Debug("Processing SIP message")

	// Handle different SIP methods
	switch strings.ToUpper(message.Method) {
	case "OPTIONS":
		s.handleOptionsMessage(message)
	case "INVITE":
		s.handleInviteMessage(message)
	case "BYE":
		s.handleByeMessage(message)
	case "ACK":
		s.handleAckMessage(message)
	default:
		logger.WithField("method", message.Method).Warn("Unsupported SIP method")
		s.sendResponse(message, 501, "Not Implemented", nil, nil)
	}
}

// handleOptionsMessage handles OPTIONS requests
func (s *CustomSIPServer) handleOptionsMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "OPTIONS")
	logger.Info("Received OPTIONS request")

	headers := map[string]string{
		"Allow":     "INVITE, ACK, BYE, CANCEL, OPTIONS",
		"Supported": "replaces, siprec",
	}

	s.sendResponse(message, 200, "OK", headers, nil)
	logger.Info("Successfully responded to OPTIONS request")
}

// handleInviteMessage handles INVITE requests
func (s *CustomSIPServer) handleInviteMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "INVITE")
	logger.Info("Received INVITE request")

	// Check if this is a SIPREC request
	contentType := s.getHeader(message, "content-type")
	if contentType != "" && strings.Contains(strings.ToLower(contentType), "multipart/mixed") {
		logger.Info("Processing SIPREC INVITE")
		s.handleSiprecInvite(message)
	} else {
		logger.Info("Processing regular INVITE")
		s.handleRegularInvite(message)
	}
}

// handleSiprecInvite handles SIPREC INVITE requests
func (s *CustomSIPServer) handleSiprecInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", true)
	
	// Log message details
	logger.WithFields(logrus.Fields{
		"call_id": message.CallID,
		"body_size": len(message.Body),
		"transport": message.Connection.transport,
		"from_tag": message.FromTag,
		"branch": message.Branch,
	}).Info("Processing SIPREC INVITE with large metadata")

	// Create or update call state
	callState := &CallState{
		CallID:       message.CallID,
		State:        "trying",
		RemoteTag:    message.FromTag,
		LocalTag:     generateTag(),
		RemoteCSeq:   extractCSeqNumber(message.CSeq),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		IsRecording:  true,
	}

	// Store call state
	s.callMutex.Lock()
	s.callStates[message.CallID] = callState
	s.callMutex.Unlock()

	// Extract SDP from multipart body for SIPREC
	sdpData, rsMetadata := s.extractSiprecContent(message.Body, message.ContentType)
	if sdpData != nil {
		callState.SDP = sdpData
		logger.WithField("sdp_size", len(sdpData)).Debug("Extracted SDP from SIPREC multipart")
	}

	if rsMetadata != nil {
		logger.WithField("metadata_size", len(rsMetadata)).Info("Extracted SIPREC metadata")
	}

	// Create clean SDP response using existing media config
	responseHeaders := map[string]string{
		"Contact": s.getHeader(message, "contact"),
	}

	// Generate proper SDP response for SIPREC
	responseSDP := s.generateSiprecSDP()
	
	// Update call state to connected
	callState.State = "connected"
	callState.LastActivity = time.Now()

	s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	logger.WithFields(logrus.Fields{
		"call_id": message.CallID,
		"local_tag": callState.LocalTag,
	}).Info("Successfully responded to SIPREC INVITE")
}

// handleRegularInvite handles regular INVITE requests
func (s *CustomSIPServer) handleRegularInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", false)
	logger.Info("Processing regular INVITE")

	// Simple response for regular INVITE
	s.sendResponse(message, 200, "OK", nil, nil)
	logger.Info("Successfully responded to regular INVITE")
}

// handleByeMessage handles BYE requests
func (s *CustomSIPServer) handleByeMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "BYE")
	logger.Info("Received BYE request")

	s.sendResponse(message, 200, "OK", nil, nil)
	logger.Info("Successfully responded to BYE request")
}

// handleAckMessage handles ACK requests
func (s *CustomSIPServer) handleAckMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "ACK")
	logger.Debug("Received ACK request")
	// ACK doesn't require a response
}

// sendResponse sends a SIP response
func (s *CustomSIPServer) sendResponse(message *SIPMessage, statusCode int, reasonPhrase string, headers map[string]string, body []byte) {
	var response strings.Builder

	// Status line
	response.WriteString(fmt.Sprintf("%s %d %s\r\n", message.Version, statusCode, reasonPhrase))

	// Copy Via headers
	if viaHeaders, exists := message.Headers["via"]; exists {
		for _, via := range viaHeaders {
			response.WriteString(fmt.Sprintf("Via: %s\r\n", via))
		}
	}

	// Copy other essential headers
	essentialHeaders := []string{"from", "to", "call-id", "cseq"}
	for _, headerName := range essentialHeaders {
		if headerValues, exists := message.Headers[headerName]; exists {
			for _, value := range headerValues {
				// Add tag to To header if it's a 200 OK response and no tag exists
				if headerName == "to" && statusCode == 200 && !strings.Contains(value, "tag=") {
					// Check if we have a call state with a local tag
					if callState := s.getCallState(message.CallID); callState != nil && callState.LocalTag != "" {
						value += fmt.Sprintf(";tag=%s", callState.LocalTag)
					} else {
						value += fmt.Sprintf(";tag=%s", generateTag())
					}
				}
				response.WriteString(fmt.Sprintf("%s: %s\r\n", strings.Title(headerName), value))
			}
		}
	}

	// Add custom headers
	for name, value := range headers {
		if value != "" {
			response.WriteString(fmt.Sprintf("%s: %s\r\n", name, value))
		}
	}

	// Content headers
	if body != nil {
		response.WriteString(fmt.Sprintf("Content-Type: application/sdp\r\n"))
		response.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	} else {
		response.WriteString("Content-Length: 0\r\n")
	}

	// End of headers
	response.WriteString("\r\n")

	// Body
	if body != nil {
		response.Write(body)
	}

	// Send response
	responseBytes := []byte(response.String())
	
	if err := s.writeToConnection(message.Connection, responseBytes); err != nil {
		s.logger.WithError(err).Error("Failed to send SIP response")
	}
}

// writeToConnection writes data to a SIP connection
func (s *CustomSIPServer) writeToConnection(conn *SIPConnection, data []byte) error {
	conn.mutex.Lock()
	defer conn.mutex.Unlock()

	switch conn.transport {
	case "udp":
		if udpConn, ok := conn.conn.(*udpConn); ok {
			_, err := udpConn.conn.WriteToUDP(data, udpConn.addr)
			return err
		}
		return fmt.Errorf("invalid UDP connection type")
	case "tcp", "tls":
		conn.writer.Write(data)
		return conn.writer.Flush()
	default:
		return fmt.Errorf("unsupported transport: %s", conn.transport)
	}
}

// getHeaderValue gets a header value from the message
func (s *CustomSIPServer) getHeaderValue(message *SIPMessage, name string) string {
	if values, exists := message.Headers[strings.ToLower(name)]; exists && len(values) > 0 {
		return values[0]
	}
	return ""
}

// getHeader gets a header value from the message (backwards compatibility)
func (s *CustomSIPServer) getHeader(message *SIPMessage, name string) string {
	return s.getHeaderValue(message, name)
}

// extractTag extracts tag parameter from SIP header
func extractTag(header string) string {
	// Look for ;tag=value
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "tag=") {
			return part[4:] // Remove "tag="
		}
	}
	return ""
}

// extractBranch extracts branch parameter from Via header
func extractBranch(header string) string {
	// Look for ;branch=value
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "branch=") {
			return part[7:] // Remove "branch="
		}
	}
	return ""
}

// extractCSeqNumber extracts sequence number from CSeq header
func extractCSeqNumber(cseq string) int {
	parts := strings.Fields(cseq)
	if len(parts) > 0 {
		if num, err := strconv.Atoi(parts[0]); err == nil {
			return num
		}
	}
	return 0
}

// generateTag generates a random tag for SIP headers
func generateTag() string {
	return fmt.Sprintf("tag-%d", time.Now().UnixNano())
}

// extractSiprecContent extracts SDP and rs-metadata from multipart SIPREC body
func (s *CustomSIPServer) extractSiprecContent(body []byte, contentType string) ([]byte, []byte) {
	bodyStr := string(body)
	
	// Extract boundary from Content-Type
	boundary := ""
	if strings.Contains(contentType, "boundary=") {
		parts := strings.Split(contentType, "boundary=")
		if len(parts) > 1 {
			boundary = strings.TrimSpace(parts[1])
			boundary = strings.Trim(boundary, "\"")
		}
	}
	
	if boundary == "" {
		s.logger.Debug("No boundary found in Content-Type")
		return nil, nil
	}
	
	// Split by boundary
	parts := strings.Split(bodyStr, "--"+boundary)
	
	var sdpData []byte
	var rsMetadata []byte
	
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "--" {
			continue
		}
		
		// Split headers and content
		sections := strings.Split(part, "\r\n\r\n")
		if len(sections) < 2 {
			sections = strings.Split(part, "\n\n")
		}
		
		if len(sections) >= 2 {
			headers := sections[0]
			content := strings.Join(sections[1:], "\r\n\r\n")
			
			if strings.Contains(headers, "application/sdp") {
				sdpData = []byte(content)
				s.logger.WithField("sdp_size", len(sdpData)).Debug("Found SDP part")
			} else if strings.Contains(headers, "application/rs-metadata+xml") {
				rsMetadata = []byte(content)
				s.logger.WithField("metadata_size", len(rsMetadata)).Debug("Found rs-metadata part")
			}
		}
	}
	
	return sdpData, rsMetadata
}

// generateSiprecSDP generates appropriate SDP response for SIPREC
func (s *CustomSIPServer) generateSiprecSDP() []byte {
	// Generate SDP with proper session info
	timestamp := time.Now().Unix()
	
	sdp := fmt.Sprintf(`v=0
o=- %d %d IN IP4 127.0.0.1
s=SIPREC Recording Session
c=IN IP4 127.0.0.1
t=0 0
m=audio 16384 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=recvonly
`, timestamp, timestamp)

	return []byte(sdp)
}

// updateCallState updates an existing call state
func (s *CustomSIPServer) updateCallState(callID string, state string) {
	s.callMutex.Lock()
	defer s.callMutex.Unlock()
	
	if callState, exists := s.callStates[callID]; exists {
		callState.State = state
		callState.LastActivity = time.Now()
	}
}

// getCallState retrieves call state for a call ID
func (s *CustomSIPServer) getCallState(callID string) *CallState {
	s.callMutex.RLock()
	defer s.callMutex.RUnlock()
	
	return s.callStates[callID]
}

// Shutdown gracefully shuts down the server
func (s *CustomSIPServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down custom SIP server")
	
	// Cancel server context
	s.shutdownFunc()

	// Close all listeners
	for _, listener := range s.listeners {
		listener.Close()
	}
	for _, listener := range s.tlsListeners {
		listener.Close()
	}

	// Close all active connections
	s.connMutex.Lock()
	for connID, conn := range s.connections {
		conn.conn.Close()
		s.logger.WithField("connection_id", connID).Debug("Closed connection during shutdown")
	}
	s.connMutex.Unlock()

	s.logger.Info("Custom SIP server shutdown completed")
	return nil
}

// udpConn wraps UDP connection for interface compatibility
type udpConn struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func (u *udpConn) Read(b []byte) (n int, err error) {
	return u.conn.Read(b)
}

func (u *udpConn) Write(b []byte) (n int, err error) {
	return u.conn.WriteToUDP(b, u.addr)
}

func (u *udpConn) Close() error {
	return nil // Don't close the main UDP socket
}

func (u *udpConn) LocalAddr() net.Addr {
	return u.conn.LocalAddr()
}

func (u *udpConn) RemoteAddr() net.Addr {
	return u.addr
}

func (u *udpConn) SetDeadline(t time.Time) error {
	return u.conn.SetDeadline(t)
}

func (u *udpConn) SetReadDeadline(t time.Time) error {
	return u.conn.SetReadDeadline(t)
}

func (u *udpConn) SetWriteDeadline(t time.Time) error {
	return u.conn.SetWriteDeadline(t)
}