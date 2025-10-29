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

	"github.com/emiago/sipgo"
	sipparser "github.com/emiago/sipgo/sip"
	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/cdr"
	"siprec-server/pkg/media"
	"siprec-server/pkg/security"
	"siprec-server/pkg/security/audit"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/telemetry/tracing"
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

	// SIP message parser
	sipParser *sipparser.Parser

	tlsConfig   *tls.Config
	tlsConfigMu sync.RWMutex

	// sipgo components for standards-compliant dialog/transaction handling
	ua        *sipgo.UserAgent
	sipServer *sipgo.Server

	// Listener address tracking for building accurate Contact headers
	listenMu    sync.RWMutex
	listenHosts map[string]string
	listenPorts map[string]int
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
	Method      string
	RequestURI  string
	Version     string
	Headers     map[string][]string
	Body        []byte
	RawMessage  []byte
	Connection  *SIPConnection
	Parsed      sipparser.Message
	Request     *sipparser.Request
	Transaction sipparser.ServerTransaction

	// Parsed SIP fields for easier access
	CallID      string
	FromTag     string
	ToTag       string
	CSeq        string
	Branch      string
	ContentType string
	StatusCode  int
	Reason      string

	HeaderOrder []headerEntry

	// Vendor-specific fields for enterprise compatibility
	UserAgent       string
	VendorType      string // "avaya", "cisco", "generic"
	VendorHeaders   map[string]string
	UCIDHeaders     []string // Avaya Universal Call ID variations
	SessionIDHeader string   // Cisco Session-ID header
}

// CallState tracks the state of SIP calls
type CallState struct {
	CallID            string
	State             string // "initial", "trying", "ringing", "connected", "terminated"
	LocalTag          string
	RemoteTag         string
	LocalCSeq         int
	RemoteCSeq        int
	PendingAckCSeq    int
	CreatedAt         time.Time
	LastActivity      time.Time
	SDP               []byte
	IsRecording       bool
	RecordingSession  *siprec.RecordingSession // SIPREC recording session with metadata
	RTPForwarder      *media.RTPForwarder      // RTP forwarder for this call
	RTPForwarders     []*media.RTPForwarder    // All RTP forwarders for multi-stream sessions
	StreamForwarders  map[string]*media.RTPForwarder
	AllocatedPortPair *media.PortPair    // Port pair reserved for non-SIPREC calls
	TraceScope        *tracing.CallScope // Per-call tracing scope
	OriginalInvite    *SIPMessage        // Stored original INVITE for cancellations
}

type headerEntry struct {
	Name  string
	Key   string
	Index int
}

// NewCustomSIPServer creates a new custom SIP server
func NewCustomSIPServer(logger *logrus.Logger, handler *Handler) *CustomSIPServer {
	ctx, cancel := context.WithCancel(context.Background())
	server := &CustomSIPServer{
		logger:       logger,
		handler:      handler,
		listeners:    make([]net.Listener, 0),
		tlsListeners: make([]net.Listener, 0),
		connections:  make(map[string]*SIPConnection),
		callStates:   make(map[string]*CallState),
		shutdownCtx:  ctx,
		shutdownFunc: cancel,
		PortManager:  media.GetPortManager(),
		sipParser:    sipparser.NewParser(),
		listenHosts:  make(map[string]string),
		listenPorts:  make(map[string]int),
	}
	server.initializeTransactionLayer()
	return server
}

func (s *CustomSIPServer) initializeTransactionLayer() {
	// Set UDP MTU to 4096 bytes to handle large SIPREC metadata
	// This prevents "size of packet larger than MTU" errors on UDP transport
	// See: https://github.com/loreste/siprec/issues/4
	sipparser.UDPMTUSize = 4096

	ua, err := sipgo.NewUA()
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to create SIP user agent for transaction layer")
	}

	s.ua = ua

	server, err := sipgo.NewServer(ua)
	if err != nil {
		s.logger.WithError(err).Fatal("Failed to create SIP server for transaction handling")
	}

	s.sipServer = server

	// Register request handlers
	server.OnInvite(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "INVITE")
	})
	server.OnPrack(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "PRACK")
	})
	server.OnAck(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "ACK")
	})
	server.OnBye(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "BYE")
	})
	server.OnCancel(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "CANCEL")
	})
	server.OnOptions(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "OPTIONS")
	})
	server.OnSubscribe(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "SUBSCRIBE")
	})
	// Default handler for unsupported methods
	server.OnNoRoute(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		msg := s.wrapRequest(req, tx)
		s.logger.WithField("method", msg.Method).Warn("Received unsupported SIP method")
		s.sendResponse(msg, 501, "Not Implemented", nil, nil)
	})
}

func (s *CustomSIPServer) setListenAddress(transport, address string) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		s.logger.WithError(err).Debugf("Failed to parse listen address %q", address)
		return
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		s.logger.WithError(err).Debugf("Failed to parse listen port from %q", address)
		return
	}

	if port <= 0 {
		return
	}

	transport = strings.ToLower(transport)
	s.listenMu.Lock()
	s.listenHosts[transport] = host
	s.listenPorts[transport] = port
	s.listenMu.Unlock()
}

func (s *CustomSIPServer) resolveContactAddress(transport string, message *SIPMessage) (string, int) {
	transport = strings.ToLower(transport)

	s.listenMu.RLock()
	host := s.listenHosts[transport]
	port := s.listenPorts[transport]
	s.listenMu.RUnlock()

	// Prefer dynamically detected external IP from NAT rewriter
	if s.handler != nil && s.handler.NATRewriter != nil {
		if extIP := s.handler.NATRewriter.GetExternalIP(); extIP != "" {
			host = extIP
		}
		if cfg := s.handler.NATRewriter.config; cfg != nil {
			if cfg.ExternalPort > 0 {
				port = cfg.ExternalPort
			}
		}
	}

	if s.handler != nil && s.handler.Config != nil {
		if nat := s.handler.Config.NATConfig; nat != nil {
			if nat.ExternalIP != "" && !strings.EqualFold(nat.ExternalIP, "auto") {
				host = nat.ExternalIP
			} else if host == "" && nat.InternalIP != "" && !strings.EqualFold(nat.InternalIP, "auto") {
				host = nat.InternalIP
			}

			if nat.ExternalPort > 0 {
				port = nat.ExternalPort
			} else if port == 0 && nat.InternalPort > 0 {
				port = nat.InternalPort
			}
		}

		if port == 0 && len(s.handler.Config.SIPPorts) > 0 {
			port = s.handler.Config.SIPPorts[0]
		}
	}

	if (host == "" || host == "0.0.0.0" || host == "::" || host == "[::]") && message != nil && message.Request != nil {
		uri := message.Request.Recipient
		if uri.Host != "" {
			host = uri.Host
		}
		if uri.Port > 0 {
			port = uri.Port
		}
	}

	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = s.detectLocalHost()
	}

	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}

	if port == 0 {
		port = 5060
	}

	return host, port
}

func (s *CustomSIPServer) detectLocalHost() string {
	if s.handler != nil && s.handler.Config != nil && s.handler.Config.NATConfig != nil {
		if ip := s.handler.Config.NATConfig.InternalIP; ip != "" && !strings.EqualFold(ip, "auto") {
			return ip
		}
	}

	interfaces, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range interfaces {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP != nil && !ipNet.IP.IsLoopback() {
				if ipv4 := ipNet.IP.To4(); ipv4 != nil {
					return ipv4.String()
				}
			}
		}
		for _, addr := range interfaces {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP != nil && !ipNet.IP.IsLoopback() {
				return ipNet.IP.String()
			}
		}
	}

	return "127.0.0.1"
}

func (s *CustomSIPServer) buildContactHeader(message *SIPMessage) string {
	transport := "udp"
	if message != nil {
		if message.Connection != nil && message.Connection.transport != "" {
			transport = message.Connection.transport
		} else if message.Request != nil {
			if reqTransport := message.Request.Transport(); reqTransport != "" {
				transport = strings.ToLower(reqTransport)
			}
		}
	}

	host, port := s.resolveContactAddress(transport, message)

	scheme := "sip"
	transportParam := ""
	switch transport {
	case "tls", "sips", "wss":
		scheme = "sips"
		transportParam = "transport=tls"
	case "tcp":
		transportParam = "transport=tcp"
	case "ws":
		transportParam = "transport=ws"
	}

	contact := fmt.Sprintf("<%s:%s", scheme, host)
	if port > 0 {
		contact = fmt.Sprintf("%s:%d", contact, port)
	}
	contact += ">"

	params := []string{}

	if message != nil {
		if suffix := extractContactParameters(s.getHeader(message, "contact")); suffix != "" {
			parts := strings.Split(suffix, ";")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				if strings.HasPrefix(strings.ToLower(part), "transport=") {
					// Skip existing transport parameters; we'll add the correct one later
					continue
				}
				params = append(params, part)
			}
		}
	}

	if transportParam != "" {
		params = append([]string{transportParam}, params...)
	}

	if len(params) > 0 {
		contact += ";" + strings.Join(params, ";")
	}

	return contact
}

func extractContactParameters(contact string) string {
	contact = strings.TrimSpace(contact)
	if contact == "" {
		return ""
	}

	if idx := strings.Index(contact, ">"); idx != -1 && idx+1 < len(contact) {
		return strings.TrimSpace(contact[idx+1:])
	}

	// Handle cases without angle brackets but with parameters
	if idx := strings.Index(contact, " "); idx != -1 && idx+1 < len(contact) {
		return strings.TrimSpace(contact[idx+1:])
	}

	return ""
}

func (s *CustomSIPServer) handleTransactionRequest(req *sipparser.Request, tx sipparser.ServerTransaction, method string) {
	message := s.wrapRequest(req, tx)

	switch strings.ToUpper(method) {
	case "INVITE":
		s.handleInviteMessage(message)
	case "PRACK":
		s.handlePrackMessage(message)
	case "ACK":
		s.handleAckMessage(message)
	case "BYE":
		s.handleByeMessage(message)
	case "CANCEL":
		s.handleCancelMessage(message)
	case "OPTIONS":
		s.handleOptionsMessage(message)
	case "SUBSCRIBE":
		s.handleSubscribeMessage(message)
	default:
		s.logger.WithField("method", method).Warn("Unhandled SIP method in transaction handler")
		s.sendResponse(message, 501, "Not Implemented", nil, nil)
	}
}

func (s *CustomSIPServer) wrapRequest(req *sipparser.Request, tx sipparser.ServerTransaction) *SIPMessage {
	connection := &SIPConnection{
		conn:         nil,
		reader:       nil,
		writer:       nil,
		remoteAddr:   req.Source(),
		transport:    strings.ToLower(req.Transport()),
		lastActivity: time.Now(),
	}

	message := newSIPMessageFromSipgo(req, connection)
	message.Request = req
	message.Transaction = tx
	message.RawMessage = []byte(req.String())
	return message
}

// ListenAndServe starts the server on the specified protocol and address
// This method provides compatibility with the sipgo interface expected by main.go
func (s *CustomSIPServer) ListenAndServe(ctx context.Context, protocol, address string) error {
	switch protocol {
	case "udp":
		return s.ListenAndServeUDP(ctx, address)
	case "tcp":
		return s.ListenAndServeTCP(ctx, address)
	case "tls":
		cfg := s.getTLSConfig()
		if cfg == nil {
			return fmt.Errorf("tls config not set")
		}
		return s.ListenAndServeTLS(ctx, address, cfg)
	default:
		return fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// ListenAndServeUDP starts UDP listener
func (s *CustomSIPServer) ListenAndServeUDP(ctx context.Context, address string) error {
	if s.sipServer == nil {
		return fmt.Errorf("SIP server not initialized")
	}
	s.setListenAddress("udp", address)
	s.logger.WithField("address", address).Info("Custom SIP server listening on UDP via sipgo transaction layer")
	return s.sipServer.ListenAndServe(ctx, "udp", address)
}

// ListenAndServeTCP starts TCP listener
func (s *CustomSIPServer) ListenAndServeTCP(ctx context.Context, address string) error {
	if s.sipServer == nil {
		return fmt.Errorf("SIP server not initialized")
	}
	s.setListenAddress("tcp", address)
	s.logger.WithField("address", address).Info("Custom SIP server listening on TCP via sipgo transaction layer")
	return s.sipServer.ListenAndServe(ctx, "tcp", address)
}

// ListenAndServeTLS starts TLS listener
func (s *CustomSIPServer) ListenAndServeTLS(ctx context.Context, address string, tlsConfig *tls.Config) error {
	if s.sipServer == nil {
		return fmt.Errorf("SIP server not initialized")
	}
	if tlsConfig != nil {
		s.SetTLSConfig(tlsConfig)
	}
	cfg := tlsConfig
	if cfg == nil {
		cfg = s.getTLSConfig()
		if cfg == nil {
			return fmt.Errorf("TLS config required for TLS listener")
		}
	}
	s.setListenAddress("tls", address)
	s.logger.WithField("address", address).Info("Custom SIP server listening on TLS via sipgo transaction layer")
	return s.sipServer.ListenAndServeTLS(ctx, "tls", address, cfg)
}

// SetTLSConfig stores the TLS configuration for future TLS listeners.
func (s *CustomSIPServer) SetTLSConfig(cfg *tls.Config) {
	s.tlsConfigMu.Lock()
	s.tlsConfig = cfg
	s.tlsConfigMu.Unlock()
}

func (s *CustomSIPServer) getTLSConfig() *tls.Config {
	s.tlsConfigMu.RLock()
	defer s.tlsConfigMu.RUnlock()
	return s.tlsConfig
}

func newSIPMessageFromSipgo(msg sipparser.Message, conn *SIPConnection) *SIPMessage {
	out := &SIPMessage{
		Headers:     make(map[string][]string),
		HeaderOrder: make([]headerEntry, 0, 16),
		Connection:  conn,
		Body:        append([]byte(nil), msg.Body()...),
		Parsed:      msg,
	}
	out.Version = "SIP/2.0"

	switch m := msg.(type) {
	case *sipparser.Request:
		out.Method = string(m.Method)
		out.RequestURI = m.Recipient.String()
		out.Version = m.SipVersion
	case *sipparser.Response:
		out.Method = strconv.Itoa(m.StatusCode)
		out.StatusCode = m.StatusCode
		out.Reason = m.Reason
		out.Version = m.SipVersion
	}

	if headerHolder, ok := msg.(interface{ Headers() []sipparser.Header }); ok {
		for _, h := range headerHolder.Headers() {
			name := h.Name()
			key := strings.ToLower(name)
			value := h.Value()
			idx := len(out.Headers[key])
			out.Headers[key] = append(out.Headers[key], value)
			out.HeaderOrder = append(out.HeaderOrder, headerEntry{
				Name:  name,
				Key:   key,
				Index: idx,
			})

			// Capture Content-Type if present
			if name == "Content-Type" {
				out.ContentType = value
			}
		}
	}

	if callID := msg.CallID(); callID != nil {
		out.CallID = callID.Value()
	}

	if from := msg.From(); from != nil && from.Params != nil {
		if tag, ok := from.Params.Get("tag"); ok {
			out.FromTag = tag
		}
	}

	if to := msg.To(); to != nil && to.Params != nil {
		if tag, ok := to.Params.Get("tag"); ok {
			out.ToTag = tag
		}
	}

	if cseq := msg.CSeq(); cseq != nil {
		out.CSeq = cseq.Value()
	}

	if via := msg.Via(); via != nil && via.Params != nil {
		if branch, ok := via.Params.Get("branch"); ok {
			out.Branch = branch
		}
	}

	return out
}

// processSIPMessage processes a parsed SIP message
func (s *CustomSIPServer) processSIPMessage(message *SIPMessage) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.WithFields(logrus.Fields{
				"panic":   r,
				"method":  message.Method,
				"call_id": message.CallID,
			}).Error("Recovered from panic in SIP message processor")
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
	case "CANCEL":
		s.handleCancelMessage(message)
	case "PRACK":
		s.handlePrackMessage(message)
	case "SUBSCRIBE":
		s.handleSubscribeMessage(message)
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
		"Allow":     "INVITE, ACK, BYE, CANCEL, PRACK, OPTIONS",
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

	// Send 100 Trying for initial INVITEs to acknowledge receipt
	if !isReInvite {
		s.sendResponse(message, 100, "Trying", nil, nil)
	}

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
	transport := ""
	if message.Connection != nil {
		transport = message.Connection.transport
	}

	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"body_size": len(message.Body),
		"transport": transport,
		"from_tag":  message.FromTag,
		"branch":    message.Branch,
	}).Info("Processing SIPREC INVITE with large metadata")

	callScope := tracing.StartCallScope(
		s.shutdownCtx,
		message.CallID,
		attribute.String("sip.method", "INVITE"),
		attribute.String("sip.transport", transport),
		attribute.Bool("siprec.initial_invite", true),
	)
	callCtx := callScope.Context()

	// Create or update call state
	callState := &CallState{
		CallID:           message.CallID,
		State:            "trying",
		RemoteTag:        message.FromTag,
		LocalTag:         generateTag(),
		RemoteCSeq:       extractCSeqNumber(message.CSeq),
		CreatedAt:        time.Now(),
		LastActivity:     time.Now(),
		IsRecording:      true,
		TraceScope:       callScope,
		OriginalInvite:   message,
		RTPForwarders:    make([]*media.RTPForwarder, 0),
		StreamForwarders: make(map[string]*media.RTPForwarder),
	}

	// Store call state
	s.callMutex.Lock()
	s.callStates[message.CallID] = callState
	s.callMutex.Unlock()

	// Send 180 Ringing to establish dialog-state per RFC 3261
	contactHeader := s.buildContactHeader(message)
	provisionalHeaders := map[string]string{}
	if contactHeader != "" {
		provisionalHeaders["Contact"] = contactHeader
	}
	s.sendResponse(message, 180, "Ringing", provisionalHeaders, nil)
	callState.State = "early"
	callState.LastActivity = time.Now()

	success := false
	var inviteErr error
	defer func() {
		if success {
			return
		}
		if callState.TraceScope != nil {
			callState.TraceScope.End(inviteErr)
		}
		s.callMutex.Lock()
		delete(s.callStates, message.CallID)
		s.callMutex.Unlock()
	}()

	// Extract SDP from multipart body for SIPREC
	sdpData, rsMetadata := s.extractSiprecContent(message.Body, message.ContentType)
	if sdpData != nil {
		callState.SDP = sdpData
		logger.WithField("sdp_size", len(sdpData)).Debug("Extracted SDP from SIPREC multipart")
	}

	var recordingSession *siprec.RecordingSession
	if rsMetadata != nil {
		logger.WithField("metadata_size", len(rsMetadata)).Info("Extracted SIPREC metadata")

		metadataCtx, metadataSpan := tracing.StartSpan(callCtx, "siprec.metadata.parse", trace.WithAttributes(
			attribute.Int("siprec.metadata.bytes", len(rsMetadata)),
		))
		parsedMetadata, err := s.parseSiprecMetadata(rsMetadata, message.ContentType)
		if err != nil {
			metadataSpan.RecordError(err)
			metadataSpan.End()
			inviteErr = err
			callScope.RecordError(err)
			logger.WithError(err).Error("Failed to parse SIPREC metadata")
			s.notifyMetadataEvent(callCtx, nil, message.CallID, "metadata.error", map[string]interface{}{
				"stage": "parse_metadata",
				"error": err.Error(),
			})
			audit.Log(callCtx, s.logger, &audit.Event{
				Category: "sip",
				Action:   "invite",
				Outcome:  audit.OutcomeFailure,
				CallID:   message.CallID,
				Details: map[string]interface{}{
					"stage": "parse_metadata",
					"error": err.Error(),
				},
			})
			// Send error response for invalid metadata
			s.sendResponse(message, 400, "Bad Request - Invalid SIPREC metadata", nil, nil)
			return
		}
		metadataSpan.End()
		callCtx = metadataCtx

		// Create recording session from metadata
		recordingSession, err = s.createRecordingSession(message.CallID, parsedMetadata, logger)
		if err != nil {
			inviteErr = err
			callScope.RecordError(err)
			logger.WithError(err).Error("Failed to create recording session")
			s.notifyMetadataEvent(callCtx, nil, message.CallID, "metadata.error", map[string]interface{}{
				"stage": "create_session",
				"error": err.Error(),
			})
			audit.Log(callCtx, s.logger, &audit.Event{
				Category: "sip",
				Action:   "invite",
				Outcome:  audit.OutcomeFailure,
				CallID:   message.CallID,
				Details: map[string]interface{}{
					"stage": "create_session",
					"error": err.Error(),
				},
			})
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		// Store session information in call state
		callState.RecordingSession = recordingSession
		if callScope.Metadata() != nil {
			callScope.Metadata().SetSessionID(recordingSession.ID)
			if tenant := audit.TenantFromSession(recordingSession); tenant != "" {
				callScope.Metadata().SetTenant(tenant)
			}
			callScope.Metadata().SetUsers(audit.UsersFromSession(recordingSession))
		}
		callAttributes := []attribute.KeyValue{
			attribute.String("recording.session_id", recordingSession.ID),
			attribute.Int("recording.participants", len(recordingSession.Participants)),
			attribute.String("recording.state", recordingSession.RecordingState),
		}
		if recordingSession.StateReason != "" {
			callAttributes = append(callAttributes, attribute.String("recording.state_reason", recordingSession.StateReason))
		}
		if !recordingSession.StateExpires.IsZero() {
			callAttributes = append(callAttributes, attribute.String("recording.state_expires", recordingSession.StateExpires.Format(time.RFC3339)))
		}
		callScope.SetAttributes(callAttributes...)
		logger.WithFields(logrus.Fields{
			"session_id":        recordingSession.ID,
			"participant_count": len(recordingSession.Participants),
			"recording_state":   recordingSession.RecordingState,
		}).Info("Successfully created recording session from SIPREC metadata")

		if svc := s.handler.CDRService(); svc != nil {
			transport := ""
			if message.Connection != nil {
				transport = message.Connection.transport
			}
			remoteAddr := ""
			if message.Connection != nil {
				remoteAddr = message.Connection.remoteAddr
			}
			if err := svc.StartSession(recordingSession.ID, message.CallID, remoteAddr, transport); err != nil {
				logger.WithError(err).Warn("Failed to start CDR session")
			} else {
				update := cdr.CDRUpdate{}
				hasUpdates := false
				if pc := len(recordingSession.Participants); pc > 0 {
					update.ParticipantCount = &pc
					hasUpdates = true
				}
				if sc := len(recordingSession.MediaStreamTypes); sc > 0 {
					update.StreamCount = &sc
					hasUpdates = true
				}
				if hasUpdates {
					if err := svc.UpdateSession(recordingSession.ID, update); err != nil {
						logger.WithError(err).Warn("Failed to update CDR session metadata")
					}
				}
			}
		}

		if len(parsedMetadata.SessionGroupAssociations) > 0 {
			logger.WithField("session_groups", parsedMetadata.SessionGroupAssociations).Info("Session group associations received")
		}
		if len(parsedMetadata.PolicyUpdates) > 0 {
			logger.WithField("policy_updates", parsedMetadata.PolicyUpdates).Info("Policy updates acknowledged")
		}
	}

	// Parse the received SDP if available
	var receivedSDP *sdp.SessionDescription
	if len(sdpData) > 0 {
		parsed := &sdp.SessionDescription{}
		if err := parsed.Unmarshal(sdpData); err != nil {
			logger.WithError(err).Warn("Failed to parse received SDP, using default")
		} else {
			receivedSDP = parsed
		}
	}

	// Create RTP forwarders for this SIPREC call
	var responseSDP []byte

	if recordingSession != nil {
		mediaConfig := s.handler.Config.MediaConfig
		if mediaConfig == nil {
			inviteErr = fmt.Errorf("media configuration missing")
			callScope.RecordError(inviteErr)
			logger.Error("Media configuration missing; cannot start RTP forwarding")
			s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "metadata.error", map[string]interface{}{
				"stage": "media_config",
				"error": inviteErr.Error(),
			})
			audit.Log(callCtx, s.logger, &audit.Event{
				Category: "sip",
				Action:   "invite",
				Outcome:  audit.OutcomeFailure,
				CallID:   message.CallID,
				Details: map[string]interface{}{
					"stage": "media_config",
					"error": inviteErr.Error(),
				},
			})
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		sttProvider := s.handler.STTCallback
		if sttProvider == nil {
			sttProvider = func(ctx context.Context, vendor string, reader io.Reader, callUUID string) error {
				_, err := io.Copy(io.Discard, reader)
				return err
			}
		}

		type audioStreamInfo struct {
			index int
			label string
		}

		var audioStreams []audioStreamInfo
		if receivedSDP != nil {
			for idx, md := range receivedSDP.MediaDescriptions {
				if md.MediaName.Media != "audio" {
					continue
				}
				audioStreams = append(audioStreams, audioStreamInfo{
					index: idx,
					label: extractStreamLabel(md, len(audioStreams)),
				})
			}
		}

		if len(audioStreams) == 0 {
			audioStreams = append(audioStreams, audioStreamInfo{
				index: -1,
				label: "leg0",
			})
		}

		forwarders := make([]*media.RTPForwarder, 0, len(audioStreams))
		cleanupForwarders := func() {
			for _, fwd := range forwarders {
				if fwd == nil {
					continue
				}
				fwd.Stop()
				fwd.Cleanup()
			}
		}

		for range audioStreams {
			forwarder, err := media.NewRTPForwarder(30*time.Second, recordingSession, s.logger, mediaConfig.PIIAudioEnabled)
			if err != nil {
				cleanupForwarders()
				inviteErr = err
				callScope.RecordError(err)
				logger.WithError(err).Error("Failed to create RTP forwarder for SIPREC call")
				s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "metadata.error", map[string]interface{}{
					"stage": "rtp_forwarder",
					"error": err.Error(),
				})
				audit.Log(callCtx, s.logger, &audit.Event{
					Category: "sip",
					Action:   "invite",
					Outcome:  audit.OutcomeFailure,
					CallID:   message.CallID,
					Details: map[string]interface{}{
						"stage": "rtp_forwarder",
						"error": err.Error(),
					},
				})
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			forwarders = append(forwarders, forwarder)
		}

		callState.RTPForwarders = forwarders
		if len(forwarders) > 0 {
			callState.RTPForwarder = forwarders[0]
		}
		if callState.StreamForwarders == nil {
			callState.StreamForwarders = make(map[string]*media.RTPForwarder)
		}
		callState.SDP = message.Body // Store original SDP for reference

		if receivedSDP != nil {
			for idx, stream := range audioStreams {
				if stream.index < 0 || idx >= len(forwarders) {
					continue
				}
				forwarder := forwarders[idx]
				md := receivedSDP.MediaDescriptions[stream.index]
				media.ConfigureForwarderForMediaDescription(forwarder, receivedSDP, md, s.logger)

				if s.handler.Config.MediaConfig.RequireSRTP && (len(forwarder.SRTPMasterKey) == 0 || len(forwarder.SRTPMasterSalt) == 0) {
					cleanupForwarders()
					inviteErr = fmt.Errorf("srtp required but not negotiated")
					callScope.RecordError(inviteErr)
					logger.WithError(inviteErr).Error("Call rejected: SRTP required")
					s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "metadata.error", map[string]interface{}{
						"stage": "srtp_requirement",
						"error": inviteErr.Error(),
					})
					audit.Log(callCtx, s.logger, &audit.Event{
						Category: "sip",
						Action:   "invite",
						Outcome:  audit.OutcomeFailure,
						CallID:   message.CallID,
						Details: map[string]interface{}{
							"stage": "srtp_requirement",
						},
					})
					s.sendResponse(message, 488, "Not Acceptable Here - SRTP required", nil, nil)
					return
				}
			}
		}

		for idx, forwarder := range forwarders {
			streamID := audioStreams[idx].label
			if streamID == "" {
				streamID = fmt.Sprintf("leg%d", idx)
			}

			callState.StreamForwarders[streamID] = forwarder

			streamCallID := fmt.Sprintf("%s_%s", message.CallID, streamID)
			media.StartRTPForwarding(callCtx, forwarder, streamCallID, mediaConfig, sttProvider)
		}

		if receivedSDP != nil {
			responseSDPBytes, err := s.handler.generateSDPResponseForForwarders(receivedSDP, "127.0.0.1", forwarders).Marshal()
			if err != nil {
				cleanupForwarders()
				inviteErr = err
				callScope.RecordError(err)
				logger.WithError(err).Error("Failed to marshal SDP response")
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			responseSDP = responseSDPBytes
		} else {
			sdpResponse := s.handler.generateSDPResponseWithPort(nil, "127.0.0.1", forwarders[0].LocalPort, forwarders[0])
			responseSDP, _ = sdpResponse.Marshal()
		}
	} else {
		// For non-SIPREC calls, allocate ports and generate SDP
		// Allocate RTP/RTCP port pair following RFC 3550
		portPair, err := media.GetPortManager().AllocatePortPair()
		if err != nil {
			logger.WithError(err).Error("Failed to allocate RTP/RTCP ports")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		callState.AllocatedPortPair = portPair

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
		"Contact":   s.buildContactHeader(message),
		"Supported": "siprec",
		"Accept":    "application/sdp, application/rs-metadata+xml, multipart/mixed",
	}

	// If we have a recording session, generate proper SIPREC response with metadata
	if recordingSession != nil {
		contentType, multipartBody, err := s.generateSiprecResponse(responseSDP, recordingSession, logger)
		if err != nil {
			inviteErr = err
			callScope.RecordError(err)
			logger.WithError(err).Error("Failed to generate SIPREC response")
			s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "metadata.error", map[string]interface{}{
				"stage": "generate_response",
				"error": err.Error(),
			})
			audit.Log(callCtx, s.logger, &audit.Event{
				Category:  "sip",
				Action:    "invite",
				Outcome:   audit.OutcomeFailure,
				CallID:    message.CallID,
				SessionID: sessionIDFromState(callState),
				Details: map[string]interface{}{
					"stage": "generate_response",
					"error": err.Error(),
				},
			})
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		// Set content type for multipart response
		responseHeaders["Content-Type"] = contentType

		// Update call state to await ACK
		callState.State = "awaiting_ack"
		callState.PendingAckCSeq = callState.RemoteCSeq
		callState.LastActivity = time.Now()

		// Send multipart response with both SDP and rs-metadata
		s.sendResponse(message, 200, "OK", responseHeaders, []byte(multipartBody))
		s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "metadata.accepted", map[string]interface{}{
			"transport": transport,
		})
	} else {
		// Regular response without SIPREC metadata
		callState.State = "awaiting_ack"
		callState.PendingAckCSeq = callState.RemoteCSeq
		callState.LastActivity = time.Now()
		s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	}

	success = true
	if callState.TraceScope != nil {
		callState.TraceScope.SetAttributes(attribute.String("siprec.state", "awaiting_ack"))
		callState.TraceScope.Span().AddEvent("siprec.invite.accepted", trace.WithAttributes(
			attribute.Bool("siprec.has_metadata", recordingSession != nil),
		))
	}
	audit.Log(callCtx, s.logger, &audit.Event{
		Category:  "sip",
		Action:    "invite",
		Outcome:   audit.OutcomeSuccess,
		CallID:    message.CallID,
		SessionID: sessionIDFromState(callState),
		Details: map[string]interface{}{
			"transport":       transport,
			"siprec_metadata": recordingSession != nil,
		},
	})
	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"local_tag": callState.LocalTag,
	}).Info("Successfully responded to SIPREC INVITE")
}

// handleSiprecReInvite handles SIPREC re-INVITE requests for session updates
func (s *CustomSIPServer) handleSiprecReInvite(message *SIPMessage, callState *CallState) {
	logger := s.logger.WithField("siprec_reinvite", true)

	logger.WithFields(logrus.Fields{
		"call_id":        message.CallID,
		"existing_state": callState.State,
		"body_size":      len(message.Body),
	}).Info("Processing SIPREC re-INVITE for session update")

	callScope := callState.TraceScope
	if callScope == nil {
		callScope = tracing.StartCallScope(
			s.shutdownCtx,
			message.CallID,
			attribute.String("sip.method", "INVITE"),
			attribute.Bool("siprec.reinvite_promoted", true),
		)
		callState.TraceScope = callScope
	}

	reinviteCtx, reinviteSpan := tracing.StartSpan(callScope.Context(), "siprec.reinvite", trace.WithAttributes(
		attribute.Bool("siprec.has_existing_session", callState.RecordingSession != nil),
		attribute.Int("siprec.reinvite_body_bytes", len(message.Body)),
	))
	defer reinviteSpan.End()
	metadataRefreshed := false

	// Extract new metadata from re-INVITE
	sdpData, rsMetadata := s.extractSiprecContent(message.Body, message.ContentType)

	if rsMetadata != nil {
		// Parse the updated metadata
		parsedMetadata, err := s.parseSiprecMetadata(rsMetadata, message.ContentType)
		if err != nil {
			reinviteSpan.RecordError(err)
			callScope.RecordError(err)
			logger.WithError(err).Error("Failed to parse SIPREC metadata in re-INVITE")
			s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.error", map[string]interface{}{
				"stage":   "parse_metadata",
				"error":   err.Error(),
				"context": "reinvite",
			})
			audit.Log(reinviteCtx, s.logger, &audit.Event{
				Category: "sip",
				Action:   "reinvite",
				Outcome:  audit.OutcomeFailure,
				CallID:   message.CallID,
				Details: map[string]interface{}{
					"stage": "parse_metadata",
					"error": err.Error(),
				},
			})
			s.sendResponse(message, 400, "Bad Request - Invalid SIPREC metadata", nil, nil)
			return
		}

		// Update existing recording session
		if callState.RecordingSession != nil {
			err = s.updateRecordingSession(callState.RecordingSession, parsedMetadata, logger)
			if err != nil {
				reinviteSpan.RecordError(err)
				callScope.RecordError(err)
				logger.WithError(err).Error("Failed to update recording session")
				s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.error", map[string]interface{}{
					"stage":   "update_session",
					"error":   err.Error(),
					"context": "reinvite",
				})
				audit.Log(reinviteCtx, s.logger, &audit.Event{
					Category:  "sip",
					Action:    "reinvite",
					Outcome:   audit.OutcomeFailure,
					CallID:    message.CallID,
					SessionID: sessionIDFromState(callState),
					Details: map[string]interface{}{
						"stage": "update_session",
						"error": err.Error(),
					},
				})
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
		} else {
			// Create new session if none exists (shouldn't happen but handle gracefully)
			recordingSession, err := s.createRecordingSession(message.CallID, parsedMetadata, logger)
			if err != nil {
				reinviteSpan.RecordError(err)
				callScope.RecordError(err)
				logger.WithError(err).Error("Failed to create recording session from re-INVITE")
				s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.error", map[string]interface{}{
					"stage":   "create_session",
					"error":   err.Error(),
					"context": "reinvite",
				})
				audit.Log(reinviteCtx, s.logger, &audit.Event{
					Category: "sip",
					Action:   "reinvite",
					Outcome:  audit.OutcomeFailure,
					CallID:   message.CallID,
					Details: map[string]interface{}{
						"stage": "create_session",
						"error": err.Error(),
					},
				})
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			callState.RecordingSession = recordingSession
			if callScope.Metadata() != nil {
				callScope.Metadata().SetSessionID(recordingSession.ID)
			}
			reinviteAttributes := []attribute.KeyValue{
				attribute.String("recording.session_id", recordingSession.ID),
				attribute.Int("recording.participants", len(recordingSession.Participants)),
				attribute.String("recording.state", recordingSession.RecordingState),
			}
			if recordingSession.StateReason != "" {
				reinviteAttributes = append(reinviteAttributes, attribute.String("recording.state_reason", recordingSession.StateReason))
			}
			if !recordingSession.StateExpires.IsZero() {
				reinviteAttributes = append(reinviteAttributes, attribute.String("recording.state_expires", recordingSession.StateExpires.Format(time.RFC3339)))
			}
			callScope.SetAttributes(reinviteAttributes...)
		}

		logger.WithFields(logrus.Fields{
			"session_id": callState.RecordingSession.ID,
			"new_state":  callState.RecordingSession.RecordingState,
			"sequence":   callState.RecordingSession.SequenceNumber,
		}).Info("Updated recording session from SIPREC re-INVITE")

		if len(parsedMetadata.SessionGroupAssociations) > 0 {
			logger.WithField("session_groups", parsedMetadata.SessionGroupAssociations).Info("Session group associations updated")
		}
		if len(parsedMetadata.PolicyUpdates) > 0 {
			logger.WithField("policy_updates", parsedMetadata.PolicyUpdates).Info("Policy updates refreshed")
		}

		metadataRefreshed = true

		if callScope.Metadata() != nil && callState.RecordingSession != nil {
			if tenant := audit.TenantFromSession(callState.RecordingSession); tenant != "" {
				callScope.Metadata().SetTenant(tenant)
			}
			callScope.Metadata().SetSessionID(callState.RecordingSession.ID)
			callScope.Metadata().SetUsers(audit.UsersFromSession(callState.RecordingSession))
		}
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
		"Contact":   s.buildContactHeader(message),
		"Supported": "siprec",
		"Accept":    "application/sdp, application/rs-metadata+xml, multipart/mixed",
	}

	var responseSDP []byte
	if callState.RTPForwarder != nil {
		var parsedSDP *sdp.SessionDescription
		if sdpData != nil && len(sdpData) > 0 {
			parsed := &sdp.SessionDescription{}
			if err := parsed.Unmarshal(sdpData); err != nil {
				logger.WithError(err).Warn("Failed to parse received SDP in re-INVITE, using default")
			} else {
				parsedSDP = parsed
			}
		}

		type audioStreamInfo struct {
			index int
			label string
		}

		var audioStreams []audioStreamInfo
		if parsedSDP != nil {
			for idx, md := range parsedSDP.MediaDescriptions {
				if md.MediaName.Media != "audio" {
					continue
				}
				audioStreams = append(audioStreams, audioStreamInfo{
					index: idx,
					label: extractStreamLabel(md, len(audioStreams)),
				})
			}
		}

		if len(audioStreams) == 0 {
			audioStreams = append(audioStreams, audioStreamInfo{index: -1, label: "leg0"})
		}

		forwarders := callState.RTPForwarders
		if len(forwarders) == 0 && callState.RTPForwarder != nil {
			forwarders = []*media.RTPForwarder{callState.RTPForwarder}
		}

		mediaConfig := s.handler.Config.MediaConfig
		if mediaConfig == nil {
			reinviteSpan.RecordError(fmt.Errorf("media configuration missing"))
			logger.Error("Media configuration missing during re-INVITE; cannot continue")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		sttProvider := s.handler.STTCallback
		if sttProvider == nil {
			sttProvider = func(ctx context.Context, vendor string, reader io.Reader, callUUID string) error {
				_, err := io.Copy(io.Discard, reader)
				return err
			}
		}

		// Allocate additional forwarders if the SRC added new streams
		existingCount := len(forwarders)
		for len(forwarders) < len(audioStreams) {
			forwarder, err := media.NewRTPForwarder(30*time.Second, callState.RecordingSession, s.logger, mediaConfig.PIIAudioEnabled)
			if err != nil {
				logger.WithError(err).Error("Failed to create additional RTP forwarder for re-INVITE")
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			forwarders = append(forwarders, forwarder)
		}

		// Tear down surplus forwarders if streams were removed
		if len(forwarders) > len(audioStreams) {
			for _, extra := range forwarders[len(audioStreams):] {
				if extra == nil {
					continue
				}
				extra.Stop()
				extra.Cleanup()
			}
			forwarders = forwarders[:len(audioStreams)]
		}

		if callState.StreamForwarders == nil {
			callState.StreamForwarders = make(map[string]*media.RTPForwarder)
		}

		// Start any new forwarders
		for idx := existingCount; idx < len(forwarders); idx++ {
			streamID := audioStreams[idx].label
			if streamID == "" {
				streamID = fmt.Sprintf("leg%d", idx)
			}
			callState.StreamForwarders[streamID] = forwarders[idx]
			media.StartRTPForwarding(reinviteCtx, forwarders[idx], fmt.Sprintf("%s_%s", message.CallID, streamID), mediaConfig, sttProvider)
		}

		callState.RTPForwarders = forwarders
		if len(forwarders) > 0 {
			callState.RTPForwarder = forwarders[0]
		}

		if callState.StreamForwarders == nil {
			callState.StreamForwarders = make(map[string]*media.RTPForwarder)
		} else {
			for k := range callState.StreamForwarders {
				delete(callState.StreamForwarders, k)
			}
		}

		if parsedSDP != nil {
			for idx, stream := range audioStreams {
				if stream.index < 0 || idx >= len(forwarders) {
					continue
				}
				md := parsedSDP.MediaDescriptions[stream.index]
				forwarder := forwarders[idx]
				media.ConfigureForwarderForMediaDescription(forwarder, parsedSDP, md, s.logger)
				callState.StreamForwarders[stream.label] = forwarder

				if s.handler.Config.MediaConfig.RequireSRTP && (len(forwarder.SRTPMasterKey) == 0 || len(forwarder.SRTPMasterSalt) == 0) {
					err := fmt.Errorf("srtp required but not negotiated")
					reinviteSpan.RecordError(err)
					callScope.RecordError(err)
					logger.WithError(err).Error("Rejecting re-INVITE: SRTP required")
					s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.error", map[string]interface{}{
						"stage":   "srtp_requirement",
						"error":   err.Error(),
						"context": "reinvite",
					})
					audit.Log(reinviteCtx, s.logger, &audit.Event{
						Category:  "sip",
						Action:    "reinvite",
						Outcome:   audit.OutcomeFailure,
						CallID:    message.CallID,
						SessionID: sessionIDFromState(callState),
						Details: map[string]interface{}{
							"stage": "srtp_requirement",
						},
					})
					s.sendResponse(message, 488, "Not Acceptable Here - SRTP required", nil, nil)
					return
				}
			}
		} else {
			for idx, forwarder := range forwarders {
				label := fmt.Sprintf("leg%d", idx)
				callState.StreamForwarders[label] = forwarder
			}
		}

		if parsedSDP != nil {
			sdpResponse := s.handler.generateSDPResponseForForwarders(parsedSDP, "127.0.0.1", forwarders)
			responseSDP, _ = sdpResponse.Marshal()
		} else {
			responseSDP = s.generateSiprecSDP(forwarders[0].LocalPort)
		}
	} else {
		// Fallback to basic SDP generation if no forwarder exists
		responseSDP = s.generateSiprecSDP(0) // 0 means allocate port dynamically
	}

	// Generate SIPREC response if we have a recording session
	if callState.RecordingSession != nil {
		contentType, multipartBody, err := s.generateSiprecResponse(responseSDP, callState.RecordingSession, logger)
		if err != nil {
			reinviteSpan.RecordError(err)
			callScope.RecordError(err)
			logger.WithError(err).Error("Failed to generate SIPREC response for re-INVITE")
			s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.error", map[string]interface{}{
				"stage":   "generate_response",
				"error":   err.Error(),
				"context": "reinvite",
			})
			audit.Log(reinviteCtx, s.logger, &audit.Event{
				Category:  "sip",
				Action:    "reinvite",
				Outcome:   audit.OutcomeFailure,
				CallID:    message.CallID,
				SessionID: sessionIDFromState(callState),
				Details: map[string]interface{}{
					"stage": "generate_response",
					"error": err.Error(),
				},
			})
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		responseHeaders["Content-Type"] = contentType
		callState.State = "awaiting_ack"
		callState.PendingAckCSeq = callState.RemoteCSeq
		callState.LastActivity = time.Now()
		s.sendResponse(message, 200, "OK", responseHeaders, []byte(multipartBody))
	} else {
		callState.State = "awaiting_ack"
		callState.PendingAckCSeq = callState.RemoteCSeq
		callState.LastActivity = time.Now()
		s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	}

	callScope.Span().AddEvent("siprec.reinvite.processed", trace.WithAttributes(
		attribute.Bool("siprec.metadata_updated", metadataRefreshed),
	))
	audit.Log(reinviteCtx, s.logger, &audit.Event{
		Category:  "sip",
		Action:    "reinvite",
		Outcome:   audit.OutcomeSuccess,
		CallID:    message.CallID,
		SessionID: sessionIDFromState(callState),
		Details: map[string]interface{}{
			"metadata_updated": metadataRefreshed,
			"rtp_forwarder":    callState.RTPForwarder != nil,
		},
	})

	if metadataRefreshed {
		s.notifyMetadataEvent(reinviteCtx, callState.RecordingSession, message.CallID, "metadata.updated", nil)
	}

	logger.WithField("call_id", message.CallID).Info("Successfully responded to SIPREC re-INVITE")
}

// updateRecordingSession updates an existing recording session with new metadata
func (s *CustomSIPServer) updateRecordingSession(session *siprec.RecordingSession, metadata *siprec.RSMetadata, logger *logrus.Entry) error {
	// Update sequence number if explicitly provided; otherwise leave untouched
	if metadata.Sequence > 0 {
		session.SequenceNumber = metadata.Sequence
	}
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
				ID:              rsParticipant.ID,
				Name:            rsParticipant.Name,
				DisplayName:     rsParticipant.DisplayName,
				Role:            rsParticipant.Role,
				JoinTime:        time.Now(), // Set join time for new/updated participants
				RecordingAware:  true,
				ConsentObtained: true,
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
		"session_id":        session.ID,
		"recording_state":   session.RecordingState,
		"sequence":          session.SequenceNumber,
		"participant_count": len(session.Participants),
	}).Info("Recording session updated successfully")

	if svc := s.handler.CDRService(); svc != nil {
		update := cdr.CDRUpdate{}
		hasUpdates := false
		if pc := len(session.Participants); pc > 0 {
			update.ParticipantCount = &pc
			hasUpdates = true
		}
		if sc := len(session.MediaStreamTypes); sc > 0 {
			update.StreamCount = &sc
			hasUpdates = true
		}
		if hasUpdates {
			if err := svc.UpdateSession(session.ID, update); err != nil {
				logger.WithError(err).Warn("Failed to update CDR session after metadata refresh")
			}
		}
	}

	return nil
}

// handleRegularInvite handles regular INVITE requests
func (s *CustomSIPServer) handleRegularInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", false)
	logger.Info("Processing regular INVITE")

	// Create or update call state
	callState := &CallState{
		CallID:           message.CallID,
		State:            "trying",
		RemoteTag:        message.FromTag,
		LocalTag:         generateTag(),
		RemoteCSeq:       extractCSeqNumber(message.CSeq),
		CreatedAt:        time.Now(),
		LastActivity:     time.Now(),
		IsRecording:      false,
		OriginalInvite:   message,
		RTPForwarders:    make([]*media.RTPForwarder, 0),
		StreamForwarders: make(map[string]*media.RTPForwarder),
	}

	// Store call state
	s.callMutex.Lock()
	s.callStates[message.CallID] = callState
	s.callMutex.Unlock()

	// Establish early dialog with 180 Ringing
	var provisionalHeaders map[string]string
	contactHeader := s.buildContactHeader(message)
	provisionalHeaders = map[string]string{}
	if contactHeader != "" {
		provisionalHeaders["Contact"] = contactHeader
	}
	s.sendResponse(message, 180, "Ringing", provisionalHeaders, nil)
	callState.State = "early"
	callState.LastActivity = time.Now()

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

	callState.AllocatedPortPair = portPair

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
	callState.State = "awaiting_ack"
	callState.PendingAckCSeq = callState.RemoteCSeq
	callState.SDP = responseSDP
	callState.LastActivity = time.Now()

	logger.WithFields(logrus.Fields{
		"call_id":   message.CallID,
		"rtp_port":  portPair.RTPPort,
		"rtcp_port": portPair.RTCPPort,
	}).Info("Allocated ports for regular call")

	// Send response with SDP
	responseHeaders := map[string]string{
		"Contact": s.buildContactHeader(message),
	}
	s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	logger.Info("Successfully responded to regular INVITE with SDP")
}

// handleRegularReInvite handles regular re-INVITE requests
func (s *CustomSIPServer) handleRegularReInvite(message *SIPMessage, callState *CallState) {
	logger := s.logger.WithField("regular_reinvite", true)

	logger.WithFields(logrus.Fields{
		"call_id":        message.CallID,
		"existing_state": callState.State,
		"body_size":      len(message.Body),
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
	callState.State = "awaiting_ack"
	callState.PendingAckCSeq = callState.RemoteCSeq
	s.sendResponse(message, 200, "OK", nil, nil)
	logger.WithField("call_id", message.CallID).Info("Successfully responded to regular re-INVITE")
}

// handleByeMessage handles BYE requests
func (s *CustomSIPServer) handleByeMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "BYE")
	logger.Info("Received BYE request")

	seq, method := parseCSeq(message.CSeq)
	if method != "" && method != "BYE" {
		logger.WithField("cseq_method", method).Warn("BYE request with mismatched CSeq method")
	}

	var callScope *tracing.CallScope
	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("BYE received without established dialog")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	if callState != nil {
		callScope = callState.TraceScope
	} else if scope, ok := tracing.GetCallScope(message.CallID); ok {
		callScope = scope
	}

	byeCtx := tracing.ContextForCall(message.CallID)
	var byeSpan trace.Span
	if callScope != nil {
		byeCtx, byeSpan = tracing.StartSpan(callScope.Context(), "siprec.bye", trace.WithAttributes(
			attribute.String("sip.method", "BYE"),
		))
		callScope.SetAttributes(attribute.String("siprec.state", "terminating"))
	} else {
		byeCtx, byeSpan = tracing.StartSpan(byeCtx, "siprec.bye", trace.WithAttributes(
			attribute.String("sip.method", "BYE"),
		))
	}
	defer byeSpan.End()

	sessionID := sessionIDFromState(callState)

	if callState.PendingAckCSeq != 0 {
		logger.WithField("pending_ack_cseq", callState.PendingAckCSeq).Warn("BYE received before ACK completed")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		audit.Log(byeCtx, s.logger, &audit.Event{
			Category:  "sip",
			Action:    "bye",
			Outcome:   audit.OutcomeFailure,
			CallID:    message.CallID,
			SessionID: sessionID,
			Details: map[string]interface{}{
				"reason": "ack_pending",
			},
		})
		return
	}

	if callState.State != "connected" && callState.State != "terminating" {
		logger.WithField("state", callState.State).Warn("BYE received in invalid dialog state")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		audit.Log(byeCtx, s.logger, &audit.Event{
			Category:  "sip",
			Action:    "bye",
			Outcome:   audit.OutcomeFailure,
			CallID:    message.CallID,
			SessionID: sessionID,
			Details: map[string]interface{}{
				"reason": "invalid_state",
				"state":  callState.State,
			},
		})
		return
	}

	if seq != 0 && seq <= callState.RemoteCSeq {
		logger.WithFields(logrus.Fields{
			"received_cseq": seq,
			"last_cseq":     callState.RemoteCSeq,
		}).Warn("BYE received with non-incrementing CSeq")
		s.sendResponse(message, 400, "Bad Request", nil, nil)
		audit.Log(byeCtx, s.logger, &audit.Event{
			Category:  "sip",
			Action:    "bye",
			Outcome:   audit.OutcomeFailure,
			CallID:    message.CallID,
			SessionID: sessionID,
			Details: map[string]interface{}{
				"reason": "cseq_regression",
				"cseq":   seq,
			},
		})
		return
	}

	s.callMutex.Lock()
	if seq != 0 {
		callState.RemoteCSeq = seq
	}
	callState.State = "terminating"
	callState.LastActivity = time.Now()
	s.callMutex.Unlock()

	// Clean up call state and recording session if exists
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
		s.finalizeCall(message.CallID, callState, "terminated")
		logger.WithField("call_id", message.CallID).Info("Call state cleaned up after BYE")
	} else if callScope != nil {
		callScope.Span().AddEvent("siprec.call.terminated", trace.WithAttributes(
			attribute.String("termination.reason", "bye"),
		))
		callScope.End(nil)
	}

	auditOutcome := audit.OutcomeSuccess
	if callState == nil {
		auditOutcome = audit.OutcomeFailure
	}

	audit.Log(byeCtx, s.logger, &audit.Event{
		Category:  "sip",
		Action:    "bye",
		Outcome:   auditOutcome,
		CallID:    message.CallID,
		SessionID: sessionID,
		Details: map[string]interface{}{
			"call_state_present": callState != nil,
		},
	})

	s.sendResponse(message, 200, "OK", nil, nil)
	if callState.TraceScope != nil {
		callState.TraceScope.Span().AddEvent("siprec.bye.acknowledged", trace.WithAttributes(
			attribute.Int("sip.cseq", seq),
		))
	}
	logger.Info("Successfully responded to BYE request")
}

// handleCancelMessage handles CANCEL requests
func (s *CustomSIPServer) handleCancelMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "CANCEL")
	logger.Info("Received CANCEL request")

	callState := s.getCallState(message.CallID)
	cancelCtx := tracing.ContextForCall(message.CallID)
	_, cancelSpan := tracing.StartSpan(cancelCtx, "siprec.cancel", trace.WithAttributes(
		attribute.String("sip.method", "CANCEL"),
	))
	defer cancelSpan.End()

	s.sendResponse(message, 200, "OK", nil, nil)

	if callState == nil {
		logger.Warn("No call state found for CANCEL request")
		return
	}

	if callState.State == "connected" || callState.State == "terminated" {
		logger.WithField("call_state", callState.State).Debug("Call already finalized; ignoring CANCEL")
		return
	}

	if callState.TraceScope != nil {
		callState.TraceScope.SetAttributes(attribute.String("siprec.state", "cancelled"))
	}

	if callState.OriginalInvite != nil {
		s.sendResponse(callState.OriginalInvite, 487, "Request Terminated", nil, nil)
	}

	if callState.RecordingSession != nil {
		callState.RecordingSession.RecordingState = "cancelled"
		callState.RecordingSession.UpdatedAt = time.Now()
		if callState.RecordingSession.ExtendedMetadata == nil {
			callState.RecordingSession.ExtendedMetadata = make(map[string]string)
		}
		callState.RecordingSession.ExtendedMetadata["termination_reason"] = "cancelled"
	}

	s.finalizeCall(message.CallID, callState, "cancelled")
	logger.WithField("call_id", message.CallID).Info("Call state cleaned up after CANCEL")

	audit.Log(cancelCtx, s.logger, &audit.Event{
		Category:  "sip",
		Action:    "cancel",
		Outcome:   audit.OutcomeSuccess,
		CallID:    message.CallID,
		SessionID: sessionIDFromState(callState),
	})
}

// handlePrackMessage handles PRACK requests
func (s *CustomSIPServer) handlePrackMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "PRACK")
	logger.Info("Received PRACK request")
	if callState := s.getCallState(message.CallID); callState != nil {
		callState.LastActivity = time.Now()
		if callState.State == "early" {
			callState.State = "proceeding"
		}
	}
	s.sendResponse(message, 200, "OK", nil, nil)
}

// handleAckMessage handles ACK requests
func (s *CustomSIPServer) handleAckMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "ACK")
	logger.Debug("Received ACK request")

	seq, method := parseCSeq(message.CSeq)
	if method != "" && method != "ACK" {
		logger.WithField("cseq_method", method).Warn("ACK request with mismatched CSeq method")
	}

	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("ACK received without existing dialog state")
		return
	}

	s.callMutex.Lock()
	defer s.callMutex.Unlock()

	currentState, exists := s.callStates[message.CallID]
	if !exists {
		logger.WithField("call_id", message.CallID).Warn("ACK received but call state removed")
		return
	}

	if currentState.PendingAckCSeq != 0 && seq != 0 && seq != currentState.PendingAckCSeq {
		logger.WithFields(logrus.Fields{
			"expected_cseq": currentState.PendingAckCSeq,
			"received_cseq": seq,
		}).Warn("ACK CSeq does not match pending INVITE transaction")
	}

	currentState.PendingAckCSeq = 0
	if seq != 0 {
		currentState.RemoteCSeq = seq
	}
	currentState.State = "connected"
	currentState.LastActivity = time.Now()

	if currentState.TraceScope != nil {
		currentState.TraceScope.SetAttributes(attribute.String("siprec.state", "connected"))
		currentState.TraceScope.Span().AddEvent("siprec.ack.received", trace.WithAttributes(
			attribute.Int("sip.cseq", seq),
		))
	}
}

// handleSubscribeMessage processes SUBSCRIBE requests used for metadata notifications
func (s *CustomSIPServer) handleSubscribeMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "SUBSCRIBE")
	logger.Info("Received SUBSCRIBE request")

	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("SUBSCRIBE received for unknown Call-ID")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	callbackURL := strings.TrimSpace(s.getHeader(message, "x-callback-url"))
	if callbackURL == "" {
		callbackURL = strings.TrimSpace(s.getHeader(message, "callback-url"))
	}
	if callbackURL == "" && len(message.Body) > 0 {
		body := strings.TrimSpace(string(message.Body))
		lowerBody := strings.ToLower(body)
		if strings.HasPrefix(lowerBody, "http://") || strings.HasPrefix(lowerBody, "https://") {
			callbackURL = body
		}
	}

	if callbackURL == "" {
		logger.Warn("SUBSCRIBE request missing callback URL")
		s.sendResponse(message, 400, "Bad Request - Missing Callback-URL", nil, nil)
		return
	}

	if s.handler != nil && s.handler.Notifier != nil {
		s.handler.Notifier.RegisterCallEndpoint(message.CallID, callbackURL)
	}

	if callState.RecordingSession != nil {
		callState.RecordingSession.Callbacks = append(callState.RecordingSession.Callbacks, callbackURL)
	}

	callState.LastActivity = time.Now()
	logger.WithFields(logrus.Fields{
		"call_id":      message.CallID,
		"callback_url": callbackURL,
	}).Info("Registered metadata callback via SUBSCRIBE")

	responseHeaders := map[string]string{
		"Expires": "300",
	}
	s.sendResponse(message, 202, "Accepted", responseHeaders, nil)
}

// sendResponse sends a SIP response
func (s *CustomSIPServer) sendResponse(message *SIPMessage, statusCode int, reasonPhrase string, headers map[string]string, body []byte) {
	if message == nil {
		s.logger.Warn("Attempted to send response for nil message")
		return
	}

	if message.Transaction == nil {
		s.logger.WithFields(logrus.Fields{
			"call_id": message.CallID,
			"method":  message.Method,
		}).Warn("No server transaction available to send response")
		return
	}

	req := message.Request
	if req == nil {
		if parsedReq, ok := message.Parsed.(*sipparser.Request); ok {
			req = parsedReq
		}
	}

	if req == nil {
		s.logger.WithFields(logrus.Fields{
			"call_id": message.CallID,
			"method":  message.Method,
		}).Warn("Unable to build SIP response without original request context")
		return
	}

	resp := sipparser.NewResponseFromRequest(req, statusCode, reasonPhrase, body)

	// Ensure To-tag aligns with dialog state
	if message.CallID != "" {
		if callState := s.getCallState(message.CallID); callState != nil && callState.LocalTag != "" {
			if to := resp.To(); to != nil {
				if to.Params == nil {
					to.Params = sipparser.HeaderParams{}
				}
				to.Params.Add("tag", callState.LocalTag)
			}
		}
	}

	if len(body) > 0 {
		resp.SetBody(body)
	}

	if len(body) > 0 {
		// Check if Content-Type header exists
		if len(resp.GetHeaders("Content-Type")) == 0 {
			resp.ReplaceHeader(sipparser.NewHeader("Content-Type", "application/sdp"))
		}
	}

	for name, value := range headers {
		if value == "" {
			continue
		}
		if len(resp.GetHeaders(name)) > 0 {
			resp.ReplaceHeader(sipparser.NewHeader(name, value))
		} else {
			resp.AppendHeader(sipparser.NewHeader(name, value))
		}
	}

	if s.handler != nil && s.handler.NATRewriter != nil {
		if err := s.handler.NATRewriter.RewriteOutgoingResponse(resp); err != nil {
			s.logger.WithError(err).Debug("Failed to apply NAT rewriting to outgoing response")
		}
	}

	if err := message.Transaction.Respond(resp); err != nil {
		s.logger.WithError(err).WithFields(logrus.Fields{
			"call_id": message.CallID,
			"method":  message.Method,
			"status":  statusCode,
		}).Error("Failed to send SIP response over transaction layer")
	}
}

func (s *CustomSIPServer) finalizeCall(callID string, callState *CallState, reason string) {
	if callState == nil {
		return
	}

	notifyCtx := context.Background()
	if callState.TraceScope != nil {
		notifyCtx = callState.TraceScope.Context()
	}

	if callState.RecordingSession != nil {
		now := time.Now()
		if reason != "" {
			switch reason {
			case "terminated":
				callState.RecordingSession.RecordingState = "terminated"
			case "cancelled":
				if callState.RecordingSession.RecordingState == "" || callState.RecordingSession.RecordingState == "active" {
					callState.RecordingSession.RecordingState = "cancelled"
				}
			default:
				if callState.RecordingSession.RecordingState == "" {
					callState.RecordingSession.RecordingState = reason
				}
			}
		}
		callState.RecordingSession.EndTime = now
		callState.RecordingSession.UpdatedAt = now
		if callState.RecordingSession.ExtendedMetadata == nil {
			callState.RecordingSession.ExtendedMetadata = make(map[string]string)
		}
		callState.RecordingSession.ExtendedMetadata["termination_reason"] = reason
	}

	if len(callState.RTPForwarders) > 0 {
		for _, forwarder := range callState.RTPForwarders {
			if forwarder == nil {
				continue
			}
			forwarder.Stop()
			forwarder.Cleanup()
		}
		callState.RTPForwarders = nil
	} else if callState.RTPForwarder != nil {
		callState.RTPForwarder.Stop()
		callState.RTPForwarder.Cleanup()
	}

	callState.RTPForwarder = nil

	callState.StreamForwarders = nil

	callState.PendingAckCSeq = 0

	// Release allocated port pairs
	pm := media.GetPortManager()
	if callState.AllocatedPortPair != nil {
		pm.ReleasePortPair(callState.AllocatedPortPair)
		callState.AllocatedPortPair = nil
	}

	callState.State = "terminated"
	callState.LastActivity = time.Now()

	s.callMutex.Lock()
	delete(s.callStates, callID)
	s.callMutex.Unlock()

	if s.handler != nil {
		s.handler.ClearSTTRouting(callID)
	}

	if svc := s.handler.CDRService(); svc != nil && callState.RecordingSession != nil {
		status := "completed"
		recordingState := strings.ToLower(callState.RecordingSession.RecordingState)
		switch recordingState {
		case "cancelled":
			status = "partial"
		case "failed", "error":
			status = "failed"
		}
		reasonLower := strings.ToLower(reason)
		switch reasonLower {
		case "cancelled":
			status = "partial"
		case "failed", "error":
			status = "failed"
		}
		var errMsg *string
		if status == "failed" && reason != "" {
			errMsg = &reason
		}
		if err := svc.EndSession(callState.RecordingSession.ID, status, errMsg); err != nil {
			s.logger.WithError(err).WithField("session_id", callState.RecordingSession.ID).Warn("Failed to record CDR for terminated session")
		}
	}

	if callState.TraceScope != nil {
		callState.TraceScope.Span().AddEvent("siprec.call.terminated", trace.WithAttributes(
			attribute.String("termination.reason", reason),
		))
		callState.TraceScope.End(nil)
		callState.TraceScope = nil
	}

	if s.handler != nil && s.handler.analyticsDispatcher != nil {
		s.handler.analyticsDispatcher.CompleteCall(context.Background(), callID)
	}

	if s.handler != nil && s.handler.Notifier != nil {
		s.handler.Notifier.ClearCallEndpoints(callID)
		s.notifyMetadataEvent(notifyCtx, callState.RecordingSession, callID, "metadata.terminated", map[string]interface{}{
			"termination_reason": reason,
		})
	}
}

func (s *CustomSIPServer) notifyMetadataEvent(ctx context.Context, session *siprec.RecordingSession, callID, event string, extra map[string]interface{}) {
	if s.handler == nil || s.handler.Notifier == nil {
		return
	}

	metadata := s.buildNotificationMetadata(session)
	if len(extra) > 0 {
		if metadata == nil {
			metadata = make(map[string]interface{}, len(extra))
		}
		for k, v := range extra {
			metadata[k] = v
		}
	}

	s.handler.Notifier.Notify(ctx, session, callID, event, metadata)
}

func (s *CustomSIPServer) buildNotificationMetadata(session *siprec.RecordingSession) map[string]interface{} {
	if session == nil {
		return nil
	}

	snapshot := map[string]interface{}{
		"sequence":     session.SequenceNumber,
		"direction":    session.Direction,
		"participants": len(session.Participants),
	}

	if session.StateReason != "" {
		snapshot["state_reason"] = session.StateReason
	}
	if session.StateReasonRef != "" {
		snapshot["state_reason_ref"] = session.StateReasonRef
	}
	if !session.StateExpires.IsZero() {
		snapshot["state_expires"] = session.StateExpires.Format(time.RFC3339)
	}
	if len(session.MediaStreamTypes) > 0 {
		snapshot["media_streams"] = session.MediaStreamTypes
	}
	if len(session.SessionGroupRoles) > 0 {
		snapshot["session_groups"] = session.SessionGroupRoles
	}
	if len(session.PolicyStates) > 0 {
		policyStates := make(map[string]map[string]interface{}, len(session.PolicyStates))
		for policyID, state := range session.PolicyStates {
			entry := map[string]interface{}{
				"status":       state.Status,
				"acknowledged": state.Acknowledged,
			}
			if !state.ReportedAt.IsZero() {
				entry["timestamp"] = state.ReportedAt.Format(time.RFC3339)
			}
			if state.RawTimestamp != "" {
				entry["raw_timestamp"] = state.RawTimestamp
			}
			policyStates[policyID] = entry
		}
		snapshot["policies"] = policyStates
	}

	return snapshot
}

func defaultReasonPhrase(status int) string {
	switch status {
	case 100:
		return "Trying"
	case 180:
		return "Ringing"
	case 183:
		return "Session Progress"
	case 200:
		return "OK"
	case 202:
		return "Accepted"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 405:
		return "Method Not Allowed"
	case 415:
		return "Unsupported Media Type"
	case 480:
		return "Temporarily Unavailable"
	case 486:
		return "Busy Here"
	case 500:
		return "Server Internal Error"
	case 501:
		return "Not Implemented"
	case 503:
		return "Service Unavailable"
	case 504:
		return "Server Time-out"
	case 603:
		return "Decline"
	default:
		return "SIP Response"
	}
}

func sessionIDFromState(callState *CallState) string {
	if callState != nil && callState.RecordingSession != nil {
		return callState.RecordingSession.ID
	}
	return ""
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

// parseCSeq extracts both the sequence number and method from a CSeq header
func parseCSeq(cseq string) (int, string) {
	parts := strings.Fields(cseq)
	if len(parts) == 0 {
		return 0, ""
	}

	number := 0
	if num, err := strconv.Atoi(parts[0]); err == nil {
		number = num
	}

	method := ""
	if len(parts) > 1 {
		method = strings.ToUpper(parts[1])
	}

	return number, method
}

// ensureHeaderHasTag guarantees that the SIP header contains a local tag parameter
func ensureHeaderHasTag(headerValue, tag string) string {
	if tag == "" {
		return headerValue
	}

	if strings.Contains(strings.ToLower(headerValue), "tag=") {
		return headerValue
	}

	if strings.Contains(headerValue, ">") {
		parts := strings.SplitN(headerValue, ">", 2)
		suffix := ""
		if len(parts) > 1 {
			suffix = strings.TrimLeft(parts[1], " ")
			if strings.HasPrefix(suffix, ";") {
				suffix = suffix[1:]
			}
			if suffix != "" {
				suffix = ";" + suffix
			}
		}
		return fmt.Sprintf("%s>;tag=%s%s", parts[0], tag, suffix)
	}

	trimmed := strings.TrimSpace(headerValue)
	if trimmed == "" {
		return fmt.Sprintf(";tag=%s", tag)
	}

	if strings.HasSuffix(trimmed, ";") {
		return fmt.Sprintf("%s tag=%s", headerValue, tag)
	}

	if strings.Contains(trimmed, ";") {
		return fmt.Sprintf("%s;tag=%s", trimmed, tag)
	}

	return fmt.Sprintf("%s;tag=%s", headerValue, tag)
}

// generateTag generates a random tag for SIP headers
func generateTag() string {
	return fmt.Sprintf("tag-%d", time.Now().UnixNano())
}

// extractSiprecContent extracts SDP and rs-metadata from multipart SIPREC body
func (s *CustomSIPServer) extractSiprecContent(body []byte, contentType string) ([]byte, []byte) {
	// Validate multipart body size
	if err := security.ValidateSize(body, security.MaxMultipartSize, "multipart body"); err != nil {
		s.logger.WithError(err).Warn("Multipart body exceeds size limit")
		return nil, nil
	}

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
				// Validate SDP size
				if err := security.ValidateSize(sdpData, security.MaxSDPSize, "SDP"); err != nil {
					s.logger.WithError(err).Warn("SDP exceeds size limit")
					sdpData = nil
				} else {
					s.logger.WithField("sdp_size", len(sdpData)).Debug("Found SDP part")
				}
			} else if strings.Contains(headers, "application/rs-metadata+xml") {
				rsMetadata = []byte(content)
				// Validate metadata size
				if err := security.ValidateSize(rsMetadata, security.MaxMetadataSize, "SIPREC metadata"); err != nil {
					s.logger.WithError(err).Warn("SIPREC metadata exceeds size limit")
					rsMetadata = nil
				} else {
					s.logger.WithField("metadata_size", len(rsMetadata)).Debug("Found rs-metadata part")
				}
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

	validation := siprec.ValidateSiprecMessage(&metadata)
	if len(validation.Errors) > 0 {
		return nil, fmt.Errorf("critical SIPREC metadata validation failure: %v", validation.Errors)
	}

	if len(validation.Warnings) > 0 {
		s.logger.WithField("warnings", validation.Warnings).Warn("SIPREC metadata validation warnings")
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
		SessionGroupRoles: make(map[string]string),
		PolicyStates:      make(map[string]siprec.PolicyAckStatus),
	}

	if s.handler != nil && len(s.handler.Config.MetadataCallbackURLs) > 0 {
		session.Callbacks = append(session.Callbacks, s.handler.Config.MetadataCallbackURLs...)
	}

	session.StateReason = strings.TrimSpace(metadata.Reason)
	session.StateReasonRef = strings.TrimSpace(metadata.ReasonRef)

	if session.StateReason != "" {
		session.ExtendedMetadata["state_reason"] = session.StateReason
		session.ExtendedMetadata["reason"] = session.StateReason
	}
	if session.StateReasonRef != "" {
		session.ExtendedMetadata["state_reason_ref"] = session.StateReasonRef
		session.ExtendedMetadata["reason_ref"] = session.StateReasonRef
	}
	if expires := strings.TrimSpace(metadata.Expires); expires != "" {
		session.ExtendedMetadata["state_expires"] = expires
		session.ExtendedMetadata["expires"] = expires
		if parsed, err := time.Parse(time.RFC3339, expires); err == nil {
			session.StateExpires = parsed
		} else {
			logger.WithError(err).Debug("Failed to parse metadata expires timestamp")
		}
	}

	// Convert participants from metadata
	for _, rsParticipant := range metadata.Participants {
		participantID := strings.TrimSpace(rsParticipant.ID)
		if participantID == "" {
			participantID = strings.TrimSpace(rsParticipant.LegacyID)
		}

		participant := siprec.Participant{
			ID:              participantID,
			Name:            rsParticipant.Name,
			DisplayName:     rsParticipant.DisplayName,
			Role:            rsParticipant.Role,
			JoinTime:        time.Now(),
			RecordingAware:  true, // Assume recording aware for SIPREC
			ConsentObtained: true, // Assume consent for SIPREC
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

	// Handle session group associations
	if len(metadata.SessionGroupAssociations) > 0 {
		session.SessionGroups = append(session.SessionGroups, metadata.SessionGroupAssociations...)
		for _, assoc := range metadata.SessionGroupAssociations {
			session.SessionGroupRoles[assoc.SessionGroupID] = assoc.Role
			key := fmt.Sprintf("session_group_%s", assoc.SessionGroupID)
			session.ExtendedMetadata[key] = assoc.Role
		}
	}

	// Handle policy updates/acknowledgements
	if len(metadata.PolicyUpdates) > 0 {
		session.PolicyUpdates = append(session.PolicyUpdates, metadata.PolicyUpdates...)
		for _, policy := range metadata.PolicyUpdates {
			rawTimestamp := strings.TrimSpace(policy.Timestamp)
			reportedAt := time.Now()
			if rawTimestamp != "" {
				if parsed, err := time.Parse(time.RFC3339, rawTimestamp); err == nil {
					reportedAt = parsed
				} else {
					logger.WithError(err).Debugf("Failed to parse policy timestamp for %s", policy.PolicyID)
				}
			}
			statusValue := strings.ToLower(strings.TrimSpace(policy.Status))
			session.PolicyStates[policy.PolicyID] = siprec.PolicyAckStatus{
				Status:       statusValue,
				Acknowledged: policy.Acknowledged,
				ReportedAt:   reportedAt,
				RawTimestamp: rawTimestamp,
			}

			statusKey := fmt.Sprintf("policy_%s_status", policy.PolicyID)
			session.ExtendedMetadata[statusKey] = statusValue
			session.ExtendedMetadata[statusKey+"_ack"] = strconv.FormatBool(policy.Acknowledged)
			if rawTimestamp != "" {
				session.ExtendedMetadata[statusKey+"_timestamp"] = rawTimestamp
			}
		}
	}

	// Set default values if not provided
	if session.RecordingState == "" {
		session.RecordingState = "active"
	}

	if session.Direction == "" {
		session.Direction = "unknown"
	}

	// Store additional metadata
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
		"session_id":        session.ID,
		"participant_count": len(session.Participants),
		"stream_count":      len(session.MediaStreamTypes),
		"recording_state":   session.RecordingState,
		"direction":         session.Direction,
		"session_groups":    len(session.SessionGroups),
		"policy_updates":    len(session.PolicyUpdates),
	}).Info("Created recording session from SIPREC metadata")

	return session, nil
}

// generateSiprecResponse creates a proper SIPREC multipart response with SDP and rs-metadata
func (s *CustomSIPServer) generateSiprecResponse(sdp []byte, session *siprec.RecordingSession, logger *logrus.Entry) (string, string, error) {
	// Create response metadata from the recording session
	state := session.RecordingState
	if state == "" {
		state = "active"
	}
	responseMetadata := &siprec.RSMetadata{
		SessionID: session.ID,
		State:     state,
		Sequence:  session.SequenceNumber + 1,
		Direction: session.Direction,
	}

	if session.StateReason != "" {
		responseMetadata.Reason = session.StateReason
	}
	if session.StateReasonRef != "" {
		responseMetadata.ReasonRef = session.StateReasonRef
	}
	if !session.StateExpires.IsZero() {
		responseMetadata.Expires = session.StateExpires.Format(time.RFC3339)
	}

	// Copy participants from session
	for _, participant := range session.Participants {
		display := participant.DisplayName
		if display == "" {
			display = participant.Name
		}

		rsParticipant := siprec.RSParticipant{
			ID:          participant.ID,
			Name:        participant.Name,
			DisplayName: display,
			Role:        participant.Role,
		}

		// Copy communication IDs
		for _, commID := range participant.CommunicationIDs {
			aorValue := siprec.NormalizeCommunicationURI(commID)
			rsParticipant.Aor = append(rsParticipant.Aor, siprec.Aor{
				Value:    aorValue,
				URI:      aorValue,
				Display:  commID.DisplayName,
				Priority: commID.Priority,
			})

			nameEntry := siprec.RSNameID{
				AOR:     aorValue,
				URI:     aorValue,
				Display: display,
			}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, siprec.LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		if len(rsParticipant.NameInfos) == 0 && display != "" {
			nameEntry := siprec.RSNameID{Display: display}
			if participant.Name != "" {
				nameEntry.Names = append(nameEntry.Names, siprec.LocalizedName{Value: participant.Name})
			}
			rsParticipant.NameInfos = append(rsParticipant.NameInfos, nameEntry)
		}

		responseMetadata.Participants = append(responseMetadata.Participants, rsParticipant)
	}

	if len(session.SessionGroups) > 0 {
		responseMetadata.SessionGroupAssociations = append(responseMetadata.SessionGroupAssociations, session.SessionGroups...)
	}

	if len(session.PolicyUpdates) > 0 {
		responseMetadata.PolicyUpdates = append(responseMetadata.PolicyUpdates, session.PolicyUpdates...)
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

	// Update session sequencing and state to reflect response sent
	if responseMetadata.Sequence > 0 {
		session.SequenceNumber = responseMetadata.Sequence
	}
	if responseMetadata.State != "" {
		session.RecordingState = responseMetadata.State
	}
	session.UpdatedAt = time.Now()

	// Create multipart response
	contentType, multipartBody := siprec.CreateMultipartResponse(string(sdp), metadataXML)

	logger.WithFields(logrus.Fields{
		"session_id":        responseMetadata.SessionID,
		"state":             responseMetadata.State,
		"sequence":          responseMetadata.Sequence,
		"participant_count": len(responseMetadata.Participants),
		"stream_count":      len(responseMetadata.Streams),
	}).Info("Generated SIPREC multipart response")

	return contentType, multipartBody, nil
}

func extractStreamLabel(md *sdp.MediaDescription, idx int) string {
	if md == nil {
		return fmt.Sprintf("leg%d", idx)
	}
	for _, attr := range md.Attributes {
		if attr.Key == "label" {
			value := strings.TrimSpace(attr.Value)
			if value != "" {
				return value
			}
		}
	}
	return fmt.Sprintf("leg%d", idx)
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
		forwarders := callState.RTPForwarders
		if len(forwarders) == 0 && callState.RTPForwarder != nil {
			forwarders = []*media.RTPForwarder{callState.RTPForwarder}
		}

		for _, forwarder := range forwarders {
			if forwarder == nil {
				continue
			}
			s.logger.WithFields(logrus.Fields{
				"call_id":   callID,
				"rtp_port":  forwarder.LocalPort,
				"rtcp_port": forwarder.RTCPPort,
			}).Debug("Cleaning up RTP forwarder during shutdown")

			forwarder.Stop()
			forwarder.Cleanup()
		}

		if callState.AllocatedPortPair != nil {
			media.GetPortManager().ReleasePortPair(callState.AllocatedPortPair)
			callState.AllocatedPortPair = nil
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

	// Close sipgo user agent and transport
	if s.ua != nil {
		if err := s.ua.Close(); err != nil {
			s.logger.WithError(err).Warn("Failed to close SIP user agent cleanly")
		}
	}

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
