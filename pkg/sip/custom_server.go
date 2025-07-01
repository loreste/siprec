package sip

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/pion/sdp/v3"
	
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

// CustomSIPServer is our own SIP server implementation
type CustomSIPServer struct {
	logger       *logrus.Logger
	handler      *Handler
	listeners    []net.Listener
	tlsListeners []net.Listener
	wg           sync.WaitGroup
	shutdownCtx  context.Context
	shutdownFunc context.CancelFunc

	// Connection tracking
	connections map[string]*SIPConnection
	connMutex   sync.RWMutex

	// Call state tracking
	callStates map[string]*CallState
	callMutex  sync.RWMutex
	
	// Port manager for dynamic port allocation
	PortManager *media.PortManager
}

// SIPConnection represents an active TCP/TLS connection
type SIPConnection struct {
	conn         net.Conn
	reader       *bufio.Reader
	writer       *bufio.Writer
	remoteAddr   string
	transport    string
	lastActivity time.Time
	mutex        sync.Mutex
}

// SIPMessage represents a parsed SIP message
type SIPMessage struct {
	Method     string
	RequestURI string
	Version    string
	Headers    map[string][]string
	Body       []byte
	RawMessage []byte
	Connection *SIPConnection

	// Parsed SIP fields for easier access
	CallID      string
	FromTag     string
	ToTag       string
	CSeq        string
	Branch      string
	ContentType string

	// Vendor-specific fields for enterprise compatibility
	UserAgent       string
	VendorType      string // "avaya", "cisco", "generic"
	VendorHeaders   map[string]string
	UCIDHeaders     []string // Avaya Universal Call ID variations
	SessionIDHeader string   // Cisco Session-ID header
}

// CallState tracks the state of SIP calls
type CallState struct {
	CallID           string
	State            string // "initial", "trying", "ringing", "connected", "terminated"
	LocalTag         string
	RemoteTag        string
	LocalCSeq        int
	RemoteCSeq       int
	CreatedAt        time.Time
	LastActivity     time.Time
	SDP              []byte
	IsRecording      bool
	RecordingSession *siprec.RecordingSession // SIPREC recording session with metadata
	RTPForwarder     *media.RTPForwarder      // RTP forwarder for this call
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
		PortManager:  media.GetPortManager(),
	}
}

// ListenAndServe starts the server on the specified protocol and address
// This method provides compatibility with the sipgo interface expected by main.go
func (s *CustomSIPServer) ListenAndServe(ctx context.Context, protocol, address string) error {
	switch protocol {
	case "udp":
		return s.ListenAndServeUDP(ctx, address)
	case "tcp":
		return s.ListenAndServeTCP(ctx, address)
	default:
		return fmt.Errorf("unsupported protocol: %s", protocol)
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
		conn:         &udpConn{conn: conn, addr: clientAddr},
		remoteAddr:   clientAddr.String(),
		transport:    "udp",
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
		conn:         conn,
		reader:       bufio.NewReader(conn),
		writer:       bufio.NewWriter(conn),
		remoteAddr:   conn.RemoteAddr().String(),
		transport:    transport,
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
				"headers_size":   len(buffer),
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
				"read":     bytesRead,
			}).Error("Error reading message body")
			return nil, err
		}

		buffer = append(buffer, body...)
		s.logger.WithFields(logrus.Fields{
			"body_size":          len(body),
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
		"method":         message.Method,
		"message_size":   len(buffer),
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

	// Extract vendor-specific information for enterprise compatibility
	s.extractVendorInformation(message)

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
		"method":       message.Method,
		"request_uri":  message.RequestURI,
		"transport":    message.Connection.transport,
		"remote_addr":  message.Connection.remoteAddr,
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

	// Check if this is a re-INVITE (session update)
	existingCallState := s.getCallState(message.CallID)
	isReInvite := existingCallState != nil

	// Check if this is a SIPREC request
	contentType := s.getHeader(message, "content-type")
	if contentType != "" && strings.Contains(strings.ToLower(contentType), "multipart/mixed") {
		if isReInvite {
			logger.Info("Processing SIPREC re-INVITE (session update)")
			s.handleSiprecReInvite(message, existingCallState)
		} else {
			logger.Info("Processing initial SIPREC INVITE")
			s.handleSiprecInvite(message)
		}
	} else {
		if isReInvite {
			logger.Info("Processing regular re-INVITE")
			s.handleRegularReInvite(message, existingCallState)
		} else {
			logger.Info("Processing initial regular INVITE")
			s.handleRegularInvite(message)
		}
	}
}

// handleSiprecInvite handles SIPREC INVITE requests
func (s *CustomSIPServer) handleSiprecInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", true)

	// Log message details
	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"body_size": len(message.Body),
		"transport": message.Connection.transport,
		"from_tag":  message.FromTag,
		"branch":    message.Branch,
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

	var recordingSession *siprec.RecordingSession
	if rsMetadata != nil {
		logger.WithField("metadata_size", len(rsMetadata)).Info("Extracted SIPREC metadata")
		
		// Parse the SIPREC metadata XML
		parsedMetadata, err := s.parseSiprecMetadata(rsMetadata, message.ContentType)
		if err != nil {
			logger.WithError(err).Error("Failed to parse SIPREC metadata")
			// Send error response for invalid metadata
			s.sendResponse(message, 400, "Bad Request - Invalid SIPREC metadata", nil, nil)
			return
		}
		
		// Create recording session from metadata
		recordingSession, err = s.createRecordingSession(message.CallID, parsedMetadata, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to create recording session")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}
		
		// Store session information in call state
		callState.RecordingSession = recordingSession
		logger.WithFields(logrus.Fields{
			"session_id":      recordingSession.ID,
			"participant_count": len(recordingSession.Participants),
			"recording_state": recordingSession.RecordingState,
		}).Info("Successfully created recording session from SIPREC metadata")
	}

	// Create RTP forwarder for this SIPREC call
	var rtpForwarder *media.RTPForwarder
	var responseSDP []byte
	
	if recordingSession != nil {
		// Create RTP forwarder for recording session
		forwarder, err := media.NewRTPForwarder(30*time.Second, recordingSession, s.logger)
		if err != nil {
			logger.WithError(err).Error("Failed to create RTP forwarder for SIPREC call")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}
		rtpForwarder = forwarder
		
		// Store the forwarder in call state for cleanup
		callState.RTPForwarder = rtpForwarder
		callState.SDP = message.Body // Store original SDP for reference
		
		// Generate SDP response using the proper handler methods with allocated port
		// Parse the received SDP if available
		var receivedSDP *sdp.SessionDescription
		if len(sdpData) > 0 {
			receivedSDP = &sdp.SessionDescription{}
			if err := receivedSDP.Unmarshal(sdpData); err != nil {
				logger.WithError(err).Warn("Failed to parse received SDP, using default")
				receivedSDP = nil
			}
		}
		
		// Use the handler's SDP generation with the allocated port
		sdpResponse := s.handler.generateSDPResponseWithPort(receivedSDP, "127.0.0.1", rtpForwarder.LocalPort, rtpForwarder)
		responseSDP, _ = sdpResponse.Marshal()
	} else {
		// For non-SIPREC calls, allocate ports and generate SDP
		// Allocate RTP/RTCP port pair following RFC 3550
		portPair, err := media.GetPortManager().AllocatePortPair()
		if err != nil {
			logger.WithError(err).Error("Failed to allocate RTP/RTCP ports")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}
		
		// Generate SDP with the allocated port
		responseSDP = s.generateSiprecSDP(portPair.RTPPort)
		
		// Note: In a real implementation, you'd want to create an RTP listener
		// on these ports and store them for cleanup later
		logger.WithFields(logrus.Fields{
			"rtp_port":  portPair.RTPPort,
			"rtcp_port": portPair.RTCPPort,
		}).Debug("Allocated ports for non-SIPREC call")
	}

	// Create clean SDP response using existing media config
	responseHeaders := map[string]string{
		"Contact": s.getHeader(message, "contact"),
	}

	// If we have a recording session, generate proper SIPREC response with metadata
	if recordingSession != nil {
		contentType, multipartBody, err := s.generateSiprecResponse(responseSDP, recordingSession, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to generate SIPREC response")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}
		
		// Set content type for multipart response
		responseHeaders["Content-Type"] = contentType
		
		// Update call state to connected
		callState.State = "connected"
		callState.LastActivity = time.Now()
		
		// Send multipart response with both SDP and rs-metadata
		s.sendResponse(message, 200, "OK", responseHeaders, []byte(multipartBody))
	} else {
		// Regular response without SIPREC metadata
		callState.State = "connected"
		callState.LastActivity = time.Now()
		s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	}
	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"local_tag": callState.LocalTag,
	}).Info("Successfully responded to SIPREC INVITE")
}

// handleSiprecReInvite handles SIPREC re-INVITE requests for session updates
func (s *CustomSIPServer) handleSiprecReInvite(message *SIPMessage, callState *CallState) {
	logger := s.logger.WithField("siprec_reinvite", true)
	
	logger.WithFields(logrus.Fields{
		"call_id":       message.CallID,
		"existing_state": callState.State,
		"body_size":     len(message.Body),
	}).Info("Processing SIPREC re-INVITE for session update")
	
	// Extract new metadata from re-INVITE
	sdpData, rsMetadata := s.extractSiprecContent(message.Body, message.ContentType)
	
	if rsMetadata != nil {
		// Parse the updated metadata
		parsedMetadata, err := s.parseSiprecMetadata(rsMetadata, message.ContentType)
		if err != nil {
			logger.WithError(err).Error("Failed to parse SIPREC metadata in re-INVITE")
			s.sendResponse(message, 400, "Bad Request - Invalid SIPREC metadata", nil, nil)
			return
		}
		
		// Update existing recording session
		if callState.RecordingSession != nil {
			err = s.updateRecordingSession(callState.RecordingSession, parsedMetadata, logger)
			if err != nil {
				logger.WithError(err).Error("Failed to update recording session")
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
		} else {
			// Create new session if none exists (shouldn't happen but handle gracefully)
			recordingSession, err := s.createRecordingSession(message.CallID, parsedMetadata, logger)
			if err != nil {
				logger.WithError(err).Error("Failed to create recording session from re-INVITE")
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			callState.RecordingSession = recordingSession
		}
		
		logger.WithFields(logrus.Fields{
			"session_id":      callState.RecordingSession.ID,
			"new_state":       callState.RecordingSession.RecordingState,
			"sequence":        callState.RecordingSession.SequenceNumber,
		}).Info("Updated recording session from SIPREC re-INVITE")
	}
	
	// Update SDP if provided
	if sdpData != nil {
		callState.SDP = sdpData
		logger.WithField("sdp_size", len(sdpData)).Debug("Updated SDP from SIPREC re-INVITE")
	}
	
	// Update call state
	callState.RemoteCSeq = extractCSeqNumber(message.CSeq)
	callState.LastActivity = time.Now()
	
	// Generate response using existing RTP forwarder
	responseHeaders := map[string]string{
		"Contact": s.getHeader(message, "contact"),
	}
	
	var responseSDP []byte
	if callState.RTPForwarder != nil {
		// Use existing RTP forwarder for re-INVITE
		var receivedSDP *sdp.SessionDescription
		if sdpData != nil && len(sdpData) > 0 {
			receivedSDP = &sdp.SessionDescription{}
			if err := receivedSDP.Unmarshal(sdpData); err != nil {
				logger.WithError(err).Warn("Failed to parse received SDP in re-INVITE, using default")
				receivedSDP = nil
			}
		}
		
		// Use the handler's SDP generation with the existing forwarder's port
		sdpResponse := s.handler.generateSDPResponseWithPort(receivedSDP, "127.0.0.1", callState.RTPForwarder.LocalPort, callState.RTPForwarder)
		responseSDP, _ = sdpResponse.Marshal()
	} else {
		// Fallback to basic SDP generation if no forwarder exists
		responseSDP = s.generateSiprecSDP(0) // 0 means allocate port dynamically
	}
	
	// Generate SIPREC response if we have a recording session
	if callState.RecordingSession != nil {
		contentType, multipartBody, err := s.generateSiprecResponse(responseSDP, callState.RecordingSession, logger)
		if err != nil {
			logger.WithError(err).Error("Failed to generate SIPREC response for re-INVITE")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}
		
		responseHeaders["Content-Type"] = contentType
		s.sendResponse(message, 200, "OK", responseHeaders, []byte(multipartBody))
	} else {
		s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	}
	
	logger.WithField("call_id", message.CallID).Info("Successfully responded to SIPREC re-INVITE")
}

// updateRecordingSession updates an existing recording session with new metadata
func (s *CustomSIPServer) updateRecordingSession(session *siprec.RecordingSession, metadata *siprec.RSMetadata, logger *logrus.Entry) error {
	// Update sequence number
	session.SequenceNumber = metadata.Sequence
	session.UpdatedAt = time.Now()
	
	// Update recording state if provided
	if metadata.State != "" {
		oldState := session.RecordingState
		session.RecordingState = metadata.State
		
		logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"old_state":  oldState,
			"new_state":  metadata.State,
		}).Info("Recording session state changed")
		
		// Handle state transitions
		if metadata.State == "terminated" {
			session.EndTime = time.Now()
			if metadata.Reason != "" {
				session.ExtendedMetadata["termination_reason"] = metadata.Reason
			}
		} else if metadata.State == "paused" {
			session.ExtendedMetadata["pause_time"] = time.Now().Format(time.RFC3339)
		} else if metadata.State == "active" && oldState == "paused" {
			session.ExtendedMetadata["resume_time"] = time.Now().Format(time.RFC3339)
		}
	}
	
	// Update direction if provided
	if metadata.Direction != "" {
		session.Direction = metadata.Direction
	}
	
	// Update participant information if provided
	if len(metadata.Participants) > 0 {
		// Clear existing participants and rebuild from metadata
		session.Participants = session.Participants[:0]
		
		for _, rsParticipant := range metadata.Participants {
			participant := siprec.Participant{
				ID:               rsParticipant.ID,
				Name:             rsParticipant.Name,
				DisplayName:      rsParticipant.DisplayName,
				Role:             rsParticipant.Role,
				JoinTime:         time.Now(), // Set join time for new/updated participants
				RecordingAware:   true,
				ConsentObtained:  true,
			}
			
			// Update communication IDs
			for _, aor := range rsParticipant.Aor {
				commID := siprec.CommunicationID{
					Type:        "sip",
					Value:       aor.Value,
					DisplayName: aor.Display,
					Priority:    aor.Priority,
					ValidFrom:   time.Now(),
				}
				
				if strings.HasPrefix(aor.Value, "tel:") {
					commID.Type = "tel"
				}
				
				participant.CommunicationIDs = append(participant.CommunicationIDs, commID)
			}
			
			session.Participants = append(session.Participants, participant)
		}
	}
	
	// Update stream information if provided
	if len(metadata.Streams) > 0 {
		session.MediaStreamTypes = session.MediaStreamTypes[:0]
		for _, stream := range metadata.Streams {
			session.MediaStreamTypes = append(session.MediaStreamTypes, stream.Type)
		}
	}
	
	// Update extended metadata
	if metadata.Reason != "" {
		session.ExtendedMetadata["reason"] = metadata.Reason
	}
	if metadata.ReasonRef != "" {
		session.ExtendedMetadata["reason_ref"] = metadata.ReasonRef
	}
	
	logger.WithFields(logrus.Fields{
		"session_id":      session.ID,
		"recording_state": session.RecordingState,
		"sequence":        session.SequenceNumber,
		"participant_count": len(session.Participants),
	}).Info("Recording session updated successfully")
	
	return nil
}

// handleRegularInvite handles regular INVITE requests
func (s *CustomSIPServer) handleRegularInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", false)
	logger.Info("Processing regular INVITE")

	// Create or update call state
	callState := &CallState{
		CallID:       message.CallID,
		State:        "trying",
		RemoteTag:    message.FromTag,
		LocalTag:     generateTag(),
		RemoteCSeq:   extractCSeqNumber(message.CSeq),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
		IsRecording:  false,
	}

	// Store call state
	s.callMutex.Lock()
	s.callStates[message.CallID] = callState
	s.callMutex.Unlock()

	// For regular calls, we should generate proper SDP response
	// Parse received SDP if available
	var receivedSDP *sdp.SessionDescription
	var responseSDP []byte
	
	if len(message.Body) > 0 && strings.Contains(s.getHeaderValue(message, "content-type"), "application/sdp") {
		receivedSDP = &sdp.SessionDescription{}
		if err := receivedSDP.Unmarshal(message.Body); err != nil {
			logger.WithError(err).Warn("Failed to parse received SDP in regular INVITE")
			receivedSDP = nil
		}
	}
	
	// Allocate RTP/RTCP port pair for the call
	portPair, err := media.GetPortManager().AllocatePortPair()
	if err != nil {
		logger.WithError(err).Error("Failed to allocate RTP/RTCP ports for regular call")
		s.sendResponse(message, 503, "Service Unavailable", nil, nil)
		return
	}
	
	// Generate SDP response
	if s.handler != nil && receivedSDP != nil {
		// Use handler's SDP generation for better compatibility
		sdpResponse := s.handler.generateSDPResponseWithPort(receivedSDP, "127.0.0.1", portPair.RTPPort, nil)
		responseSDP, _ = sdpResponse.Marshal()
	} else {
		// Fallback to simple SDP generation
		responseSDP = s.generateSiprecSDP(portPair.RTPPort)
	}
	
	// Update call state
	callState.State = "connected"
	callState.SDP = responseSDP
	callState.LastActivity = time.Now()
	
	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"rtp_port":  portPair.RTPPort,
		"rtcp_port": portPair.RTCPPort,
	}).Info("Allocated ports for regular call")

	// Send response with SDP
	responseHeaders := map[string]string{
		"Contact": s.getHeader(message, "contact"),
	}
	s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	logger.Info("Successfully responded to regular INVITE with SDP")
}

// handleRegularReInvite handles regular re-INVITE requests
func (s *CustomSIPServer) handleRegularReInvite(message *SIPMessage, callState *CallState) {
	logger := s.logger.WithField("regular_reinvite", true)
	
	logger.WithFields(logrus.Fields{
		"call_id":       message.CallID,
		"existing_state": callState.State,
		"body_size":     len(message.Body),
	}).Info("Processing regular re-INVITE")
	
	// Update SDP if provided
	if len(message.Body) > 0 {
		callState.SDP = message.Body
		logger.WithField("sdp_size", len(message.Body)).Debug("Updated SDP from regular re-INVITE")
	}
	
	// Update call state
	callState.RemoteCSeq = extractCSeqNumber(message.CSeq)
	callState.LastActivity = time.Now()
	
	// Simple response for regular re-INVITE
	s.sendResponse(message, 200, "OK", nil, nil)
	logger.WithField("call_id", message.CallID).Info("Successfully responded to regular re-INVITE")
}

// handleByeMessage handles BYE requests
func (s *CustomSIPServer) handleByeMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "BYE")
	logger.Info("Received BYE request")

	// Clean up call state and recording session if exists
	callState := s.getCallState(message.CallID)
	if callState != nil {
		if callState.RecordingSession != nil {
			// Update recording session to terminated state
			callState.RecordingSession.RecordingState = "terminated"
			callState.RecordingSession.EndTime = time.Now()
			callState.RecordingSession.UpdatedAt = time.Now()
			callState.RecordingSession.ExtendedMetadata["termination_reason"] = "bye_received"
			
			logger.WithFields(logrus.Fields{
				"session_id":      callState.RecordingSession.ID,
				"recording_state": "terminated",
				"duration":        time.Since(callState.RecordingSession.StartTime),
			}).Info("Recording session terminated due to BYE")
		}
		
		// Clean up RTP forwarder if exists
		if callState.RTPForwarder != nil {
			logger.WithFields(logrus.Fields{
				"call_id":   message.CallID,
				"rtp_port":  callState.RTPForwarder.LocalPort,
				"rtcp_port": callState.RTPForwarder.RTCPPort,
			}).Debug("Cleaning up RTP forwarder")
			
			// Signal forwarding to stop
			close(callState.RTPForwarder.StopChan)
			
			// Perform thorough cleanup
			callState.RTPForwarder.Cleanup()
			callState.RTPForwarder = nil
		}
		
		// Update call state
		callState.State = "terminated"
		callState.LastActivity = time.Now()
		
		// Remove from active call states
		s.callMutex.Lock()
		delete(s.callStates, message.CallID)
		s.callMutex.Unlock()
		
		logger.WithField("call_id", message.CallID).Info("Call state cleaned up")
	}

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
	// Create a response message for NAT rewriting
	responseMessage := &SIPMessage{
		Method:     "RESPONSE",
		RequestURI: "",
		Version:    message.Version,
		Headers:    make(map[string][]string),
		Body:       body,
		Connection: message.Connection,
		CallID:     message.CallID,
	}

	// Copy Via headers
	if viaHeaders, exists := message.Headers["via"]; exists {
		responseMessage.Headers["via"] = make([]string, len(viaHeaders))
		copy(responseMessage.Headers["via"], viaHeaders)
	}

	// Copy other essential headers
	essentialHeaders := []string{"from", "to", "call-id", "cseq"}
	for _, headerName := range essentialHeaders {
		if headerValues, exists := message.Headers[headerName]; exists {
			responseMessage.Headers[headerName] = make([]string, len(headerValues))
			copy(responseMessage.Headers[headerName], headerValues)

			// Add tag to To header if it's a 200 OK response and no tag exists
			if headerName == "to" && statusCode == 200 {
				for i, value := range responseMessage.Headers[headerName] {
					if !strings.Contains(value, "tag=") {
						// Check if we have a call state with a local tag
						if callState := s.getCallState(message.CallID); callState != nil && callState.LocalTag != "" {
							responseMessage.Headers[headerName][i] = value + fmt.Sprintf(";tag=%s", callState.LocalTag)
						} else {
							responseMessage.Headers[headerName][i] = value + fmt.Sprintf(";tag=%s", generateTag())
						}
					}
				}
			}
		}
	}

	// Add custom headers
	for name, value := range headers {
		if value != "" {
			headerName := strings.ToLower(name)
			if _, exists := responseMessage.Headers[headerName]; !exists {
				responseMessage.Headers[headerName] = make([]string, 0)
			}
			responseMessage.Headers[headerName] = append(responseMessage.Headers[headerName], value)
		}
	}

	// Set Content-Type if body exists
	if body != nil {
		responseMessage.Headers["content-type"] = []string{"application/sdp"}
	}

	// Apply NAT rewriting if handler has NAT rewriter configured
	if s.handler != nil && s.handler.NATRewriter != nil {
		if err := s.handler.NATRewriter.RewriteOutgoingMessage(responseMessage); err != nil {
			s.logger.WithError(err).Debug("Failed to apply NAT rewriting to outgoing response")
		}
	}

	// Build the response string from the (possibly rewritten) response message
	var response strings.Builder

	// Status line
	response.WriteString(fmt.Sprintf("%s %d %s\r\n", responseMessage.Version, statusCode, reasonPhrase))

	// Write Via headers
	if viaHeaders, exists := responseMessage.Headers["via"]; exists {
		for _, via := range viaHeaders {
			response.WriteString(fmt.Sprintf("Via: %s\r\n", via))
		}
	}

	// Write other essential headers
	for _, headerName := range essentialHeaders {
		if headerValues, exists := responseMessage.Headers[headerName]; exists {
			for _, value := range headerValues {
				response.WriteString(fmt.Sprintf("%s: %s\r\n", strings.Title(headerName), value))
			}
		}
	}

	// Write custom headers
	for headerName, headerValues := range responseMessage.Headers {
		// Skip headers we've already written
		if headerName == "via" || contains(essentialHeaders, headerName) || headerName == "content-type" || headerName == "content-length" {
			continue
		}
		for _, value := range headerValues {
			response.WriteString(fmt.Sprintf("%s: %s\r\n", strings.Title(headerName), value))
		}
	}

	// Content headers
	if responseMessage.Body != nil {
		if contentType, exists := responseMessage.Headers["content-type"]; exists && len(contentType) > 0 {
			response.WriteString(fmt.Sprintf("Content-Type: %s\r\n", contentType[0]))
		} else {
			response.WriteString("Content-Type: application/sdp\r\n")
		}
		response.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(responseMessage.Body)))
	} else {
		response.WriteString("Content-Length: 0\r\n")
	}

	// End of headers
	response.WriteString("\r\n")

	// Body
	if responseMessage.Body != nil {
		response.Write(responseMessage.Body)
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

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
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

// parseSiprecMetadata parses raw SIPREC metadata bytes into RSMetadata structure
func (s *CustomSIPServer) parseSiprecMetadata(rsMetadata []byte, contentType string) (*siprec.RSMetadata, error) {
	// Parse the XML metadata
	var metadata siprec.RSMetadata
	if err := xml.Unmarshal(rsMetadata, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal SIPREC metadata XML: %w", err)
	}
	
	// Validate the metadata using the existing validation function
	deficiencies := siprec.ValidateSiprecMessage(&metadata)
	if len(deficiencies) > 0 {
		// Check for critical deficiencies
		for _, deficiency := range deficiencies {
			if strings.Contains(deficiency, "missing session ID") ||
				strings.Contains(deficiency, "missing recording state") ||
				strings.Contains(deficiency, "invalid recording state") {
				return nil, fmt.Errorf("critical SIPREC metadata validation failure: %v", deficiencies)
			}
		}
		
		// Log warnings for non-critical issues
		s.logger.WithField("deficiencies", deficiencies).Warning("SIPREC metadata validation warnings")
	}
	
	return &metadata, nil
}

// createRecordingSession creates a RecordingSession from parsed SIPREC metadata
func (s *CustomSIPServer) createRecordingSession(sipCallID string, metadata *siprec.RSMetadata, logger *logrus.Entry) (*siprec.RecordingSession, error) {
	// Create the recording session object
	session := &siprec.RecordingSession{
		ID:                metadata.SessionID,
		SIPID:             sipCallID,
		AssociatedTime:    time.Now(),
		SequenceNumber:    metadata.Sequence,
		RecordingState:    metadata.State,
		Direction:         metadata.Direction,
		StartTime:         time.Now(),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
		IsValid:           true,
		ExtendedMetadata:  make(map[string]string),
	}
	
	// Convert participants from metadata
	for _, rsParticipant := range metadata.Participants {
		participant := siprec.Participant{
			ID:               rsParticipant.ID,
			Name:             rsParticipant.Name,
			DisplayName:      rsParticipant.DisplayName,
			Role:             rsParticipant.Role,
			JoinTime:         time.Now(),
			RecordingAware:   true, // Assume recording aware for SIPREC
			ConsentObtained:  true, // Assume consent for SIPREC
		}
		
		// Convert communication IDs
		for _, aor := range rsParticipant.Aor {
			commID := siprec.CommunicationID{
				Type:        "sip", // Default to SIP
				Value:       aor.Value,
				DisplayName: aor.Display,
				Priority:    aor.Priority,
				ValidFrom:   time.Now(),
			}
			
			// Determine type from URI format
			if strings.HasPrefix(aor.Value, "tel:") {
				commID.Type = "tel"
			} else if strings.HasPrefix(aor.Value, "sip:") {
				commID.Type = "sip"
			}
			
			participant.CommunicationIDs = append(participant.CommunicationIDs, commID)
		}
		
		session.Participants = append(session.Participants, participant)
	}
	
	// Handle stream information
	for _, stream := range metadata.Streams {
		session.MediaStreamTypes = append(session.MediaStreamTypes, stream.Type)
	}
	
	// Set default values if not provided
	if session.RecordingState == "" {
		session.RecordingState = "active"
	}
	
	if session.Direction == "" {
		session.Direction = "unknown"
	}
	
	// Store additional metadata
	if metadata.Reason != "" {
		session.ExtendedMetadata["reason"] = metadata.Reason
	}
	if metadata.ReasonRef != "" {
		session.ExtendedMetadata["reason_ref"] = metadata.ReasonRef
	}
	if metadata.Expires != "" {
		session.ExtendedMetadata["expires"] = metadata.Expires
	}
	if metadata.MediaLabel != "" {
		session.ExtendedMetadata["media_label"] = metadata.MediaLabel
	}
	
	// Store association information
	if metadata.SessionRecordingAssoc.CallID != "" {
		session.ExtendedMetadata["associated_call_id"] = metadata.SessionRecordingAssoc.CallID
	}
	if metadata.SessionRecordingAssoc.Group != "" {
		session.ExtendedMetadata["group"] = metadata.SessionRecordingAssoc.Group
	}
	if metadata.SessionRecordingAssoc.FixedID != "" {
		session.ExtendedMetadata["fixed_id"] = metadata.SessionRecordingAssoc.FixedID
	}
	
	logger.WithFields(logrus.Fields{
		"session_id":      session.ID,
		"participant_count": len(session.Participants),
		"stream_count":    len(session.MediaStreamTypes),
		"recording_state": session.RecordingState,
		"direction":       session.Direction,
	}).Info("Created recording session from SIPREC metadata")
	
	return session, nil
}

// generateSiprecResponse creates a proper SIPREC multipart response with SDP and rs-metadata
func (s *CustomSIPServer) generateSiprecResponse(sdp []byte, session *siprec.RecordingSession, logger *logrus.Entry) (string, string, error) {
	// Create response metadata from the recording session
	responseMetadata := &siprec.RSMetadata{
		SessionID: session.ID,
		State:     "active", // Set to active since we're accepting the session
		Sequence:  session.SequenceNumber + 1, // Increment sequence for response
		Direction: session.Direction,
	}
	
	// Copy participants from session
	for _, participant := range session.Participants {
		rsParticipant := siprec.RSParticipant{
			ID:          participant.ID,
			Name:        participant.Name,
			DisplayName: participant.DisplayName,
			Role:        participant.Role,
		}
		
		// Copy communication IDs
		for _, commID := range participant.CommunicationIDs {
			aor := siprec.Aor{
				Value:    commID.Value,
				Display:  commID.DisplayName,
				Priority: commID.Priority,
			}
			
			// Set URI format if not already set
			if commID.Type == "sip" && !strings.HasPrefix(commID.Value, "sip:") {
				aor.URI = "sip:" + commID.Value
			} else if commID.Type == "tel" && !strings.HasPrefix(commID.Value, "tel:") {
				aor.URI = "tel:" + commID.Value
			} else {
				aor.URI = commID.Value
			}
			
			rsParticipant.Aor = append(rsParticipant.Aor, aor)
		}
		
		responseMetadata.Participants = append(responseMetadata.Participants, rsParticipant)
	}
	
	// Add session recording association
	responseMetadata.SessionRecordingAssoc = siprec.RSAssociation{
		SessionID: session.ID,
		CallID:    session.SIPID,
	}
	
	// Add stream information if available
	for i, streamType := range session.MediaStreamTypes {
		stream := siprec.Stream{
			Label:    fmt.Sprintf("stream_%d", i),
			StreamID: fmt.Sprintf("stream_%d_%s", i, streamType),
			Type:     streamType,
			Mode:     "separate", // Default to separate streams
		}
		responseMetadata.Streams = append(responseMetadata.Streams, stream)
	}
	
	// Convert metadata to XML
	metadataXML, err := siprec.CreateMetadataResponse(responseMetadata)
	if err != nil {
		return "", "", fmt.Errorf("failed to create metadata response: %w", err)
	}
	
	// Create multipart response
	contentType, multipartBody := siprec.CreateMultipartResponse(string(sdp), metadataXML)
	
	logger.WithFields(logrus.Fields{
		"session_id":      responseMetadata.SessionID,
		"state":           responseMetadata.State,
		"sequence":        responseMetadata.Sequence,
		"participant_count": len(responseMetadata.Participants),
		"stream_count":    len(responseMetadata.Streams),
	}).Info("Generated SIPREC multipart response")
	
	return contentType, multipartBody, nil
}

// generateSiprecSDP generates appropriate SDP response for SIPREC
func (s *CustomSIPServer) generateSiprecSDP(rtpPort int) []byte {
	// Generate SDP with proper session info
	timestamp := time.Now().Unix()

	// Validate that we have a valid port
	if rtpPort <= 0 {
		s.logger.Error("generateSiprecSDP called without a valid RTP port")
		// Use a fallback port to avoid complete failure, but this is an error condition
		rtpPort = 10000
	}

	sdp := fmt.Sprintf(`v=0
o=- %d %d IN IP4 127.0.0.1
s=SIPREC Recording Session
c=IN IP4 127.0.0.1
t=0 0
m=audio %d RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=recvonly
`, timestamp, timestamp, rtpPort)

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

	// Clean up all active calls and release their ports
	s.callMutex.Lock()
	for callID, callState := range s.callStates {
		if callState.RTPForwarder != nil {
			s.logger.WithFields(logrus.Fields{
				"call_id":   callID,
				"rtp_port":  callState.RTPForwarder.LocalPort,
				"rtcp_port": callState.RTPForwarder.RTCPPort,
			}).Debug("Cleaning up RTP forwarder during shutdown")
			
			// Close the stop channel if it's not already closed
			select {
			case <-callState.RTPForwarder.StopChan:
				// Already closed
			default:
				close(callState.RTPForwarder.StopChan)
			}
			
			// Perform cleanup
			callState.RTPForwarder.Cleanup()
		}
	}
	// Clear all call states
	s.callStates = make(map[string]*CallState)
	s.callMutex.Unlock()

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

	// Log port manager statistics
	if s.PortManager != nil {
		stats := s.PortManager.GetStats()
		s.logger.WithFields(logrus.Fields{
			"total_ports":     stats.TotalPorts,
			"used_ports":      stats.UsedPorts,
			"available_ports": stats.AvailablePorts,
		}).Info("Port manager statistics at shutdown")
	}

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

// extractVendorInformation detects and extracts vendor-specific headers for enterprise compatibility
func (s *CustomSIPServer) extractVendorInformation(message *SIPMessage) {
	// Initialize vendor fields
	message.VendorHeaders = make(map[string]string)
	message.UCIDHeaders = []string{}
	
	// Extract User-Agent for vendor detection
	message.UserAgent = s.getHeaderValue(message, "user-agent")
	message.VendorType = s.detectVendor(message)
	
	// Extract vendor-specific headers based on detected vendor
	switch message.VendorType {
	case "avaya":
		s.extractAvayaHeaders(message)
	case "cisco":
		s.extractCiscoHeaders(message)
	default:
		s.extractGenericHeaders(message)
	}
}

// detectVendor identifies the vendor type based on headers
func (s *CustomSIPServer) detectVendor(message *SIPMessage) string {
	userAgent := strings.ToLower(message.UserAgent)
	
	// Detect Avaya systems
	if strings.Contains(userAgent, "avaya") ||
		strings.Contains(userAgent, "aura") ||
		strings.Contains(userAgent, "session manager") {
		return "avaya"
	}
	
	// Detect Cisco systems  
	if strings.Contains(userAgent, "cisco") ||
		strings.Contains(userAgent, "cube") ||
		strings.Contains(userAgent, "ccm") ||
		strings.Contains(userAgent, "cucm") {
		return "cisco"
	}
	
	// Check for vendor-specific headers as fallback
	if s.getHeaderValue(message, "x-avaya-conf-id") != "" ||
		s.getHeaderValue(message, "x-avaya-ucid") != "" ||
		s.getHeaderValue(message, "x-avaya-station-id") != "" {
		return "avaya"
	}
	
	if s.getHeaderValue(message, "session-id") != "" ||
		s.getHeaderValue(message, "cisco-guid") != "" ||
		s.getHeaderValue(message, "x-cisco-call-id") != "" {
		return "cisco"
	}
	
	return "generic"
}

// extractAvayaHeaders extracts Avaya-specific SIP headers
func (s *CustomSIPServer) extractAvayaHeaders(message *SIPMessage) {
	// Avaya-specific headers to extract
	avayaHeaders := []string{
		"x-avaya-conf-id",
		"x-avaya-station-id", 
		"x-avaya-ucid",
		"x-avaya-trunk-group",
		"x-avaya-user-id",
		"x-avaya-agent-id",
		"x-avaya-skill-group",
		"p-asserted-identity",
		"diversion",
		"remote-party-id",
	}
	
	for _, header := range avayaHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}
	
	// Handle Universal Call ID (UCID) variations
	ucidHeaders := []string{"x-avaya-ucid", "call-id", "x-ucid", "x-avaya-conf-id"}
	for _, header := range ucidHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.UCIDHeaders = append(message.UCIDHeaders, value)
		}
	}
}

// extractCiscoHeaders extracts Cisco-specific SIP headers
func (s *CustomSIPServer) extractCiscoHeaders(message *SIPMessage) {
	// Cisco-specific headers to extract
	ciscoHeaders := []string{
		"session-id",
		"cisco-guid",
		"x-cisco-call-id",
		"x-cisco-trunk-license",
		"x-cisco-media-profile",
		"x-cisco-dial-peer",
		"remote-party-id",
		"p-called-party-id",
		"p-calling-party-id",
	}
	
	for _, header := range ciscoHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}
	
	// Handle Cisco Session-ID header specifically
	if sessionID := s.getHeaderValue(message, "session-id"); sessionID != "" {
		message.SessionIDHeader = sessionID
	}
}

// extractGenericHeaders extracts common headers for non-vendor-specific systems
func (s *CustomSIPServer) extractGenericHeaders(message *SIPMessage) {
	// Common headers that might be useful for any vendor
	commonHeaders := []string{
		"p-asserted-identity",
		"remote-party-id",
		"p-preferred-identity",
		"privacy",
		"supported",
		"require",
	}
	
	for _, header := range commonHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}
}
