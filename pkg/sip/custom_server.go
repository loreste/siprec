package sip

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	sipparser "github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"siprec-server/pkg/audio"
	"siprec-server/pkg/cdr"
	"siprec-server/pkg/correlation"
	"siprec-server/pkg/media"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/security"
	"siprec-server/pkg/security/audit"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/telemetry/tracing"
	"siprec-server/pkg/version"
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
	VendorType      string // "avaya", "cisco", "oracle", "genesys", "asterisk", "freeswitch", "opensips", "generic"
	VendorHeaders   map[string]string
	UCIDHeaders     []string // Universal Call ID variations (all vendors)
	SessionIDHeader string   // Cisco Session-ID header

	// UUI (User-to-User Information) per RFC 7433
	UUIHeader string // User-to-User header value

	// X-Headers - all custom headers starting with "X-"
	XHeaders map[string]string

	// Oracle SBC specific fields
	OracleUCID           string // Oracle Universal Call ID
	OracleConversationID string // Oracle Conversation ID for call correlation

	// Genesys specific fields
	GenesysInteractionID  string // Primary Genesys interaction identifier
	GenesysConversationID string // Genesys conversation ID for call correlation
	GenesysSessionID      string // Genesys session ID
	GenesysQueueName      string // Contact center queue name
	GenesysAgentID        string // Agent identifier
	GenesysCampaignID     string // Outbound campaign ID

	// NICE specific fields
	NICEInteractionID string // NICE interaction identifier
	NICESessionID     string // NICE session ID
	NICERecordingID   string // NICE recording ID
	NICECallID        string // NICE call ID
	NICEContactID     string // NICE CXone/inContact contact ID
	NICEAgentID       string // NICE agent identifier

	// Asterisk specific fields
	AsteriskUniqueID   string // Asterisk unique channel identifier
	AsteriskLinkedID   string // Asterisk linked channel ID (for bridged calls)
	AsteriskChannelID  string // Asterisk channel name
	AsteriskAccountCode string // Asterisk CDR account code
	AsteriskContext    string // Asterisk dialplan context

	// FreeSWITCH specific fields
	FreeSWITCHUUID         string // FreeSWITCH call UUID
	FreeSWITCHCoreUUID     string // FreeSWITCH core UUID
	FreeSWITCHChannelName  string // FreeSWITCH channel name
	FreeSWITCHProfileName  string // FreeSWITCH sofia profile
	FreeSWITCHAccountCode  string // FreeSWITCH account code

	// OpenSIPS specific fields
	OpenSIPSCallID        string // OpenSIPS Call-ID correlation
	OpenSIPSDialogID      string // OpenSIPS dialog identifier
	OpenSIPSTransactionID string // OpenSIPS transaction ID

	// Avaya specific fields
	AvayaUCID       string // Avaya Universal Call ID
	AvayaConfID     string // Avaya Conference ID
	AvayaStationID  string // Avaya station identifier
	AvayaAgentID    string // Avaya agent identifier
	AvayaVDN        string // Avaya Vector Directory Number
	AvayaSkillGroup string // Avaya skill/hunt group
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

	// Context cancellation for graceful goroutine cleanup
	rtpCtx    context.Context    // Context for all RTP forwarding goroutines
	cancelCtx context.CancelFunc // Cancel function to stop all RTP forwarding goroutines
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
	server.OnUpdate(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "UPDATE")
	})
	server.OnInfo(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "INFO")
	})
	server.OnRefer(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "REFER")
	})
	server.OnNotify(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "NOTIFY")
	})
	server.OnMessage(func(req *sipparser.Request, tx sipparser.ServerTransaction) {
		s.handleTransactionRequest(req, tx, "MESSAGE")
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

func (s *CustomSIPServer) resolveMediaIPAddress(message *SIPMessage) string {
	candidates := make([]string, 0, 8)

	if s.handler != nil {
		if nr := s.handler.NATRewriter; nr != nil {
			candidates = append(candidates, nr.GetExternalIP())
			if cfg := nr.config; cfg != nil {
				candidates = append(candidates, cfg.ExternalIP, cfg.InternalIP)
			}
		}
		if cfg := s.handler.Config; cfg != nil {
			if mediaCfg := cfg.MediaConfig; mediaCfg != nil {
				candidates = append(candidates, mediaCfg.ExternalIP, mediaCfg.InternalIP)
			}
			if natCfg := cfg.NATConfig; natCfg != nil {
				candidates = append(candidates, natCfg.ExternalIP, natCfg.InternalIP)
			}
		}
	}

	if message != nil && message.Connection != nil {
		if conn := message.Connection.conn; conn != nil {
			if addr := conn.LocalAddr(); addr != nil {
				candidates = append(candidates, addr.String())
			}
		}

		transport := strings.ToLower(message.Connection.transport)
		s.listenMu.RLock()
		if host := s.listenHosts[transport]; host != "" {
			candidates = append(candidates, host)
		}
		s.listenMu.RUnlock()
	}

	for _, candidate := range candidates {
		if ip := sanitizeMediaIPCandidate(candidate); isUsableMediaIP(ip) {
			return ip
		}
	}

	// Fall back to any known listen host before using loopback
	s.listenMu.RLock()
	for _, host := range s.listenHosts {
		if ip := sanitizeMediaIPCandidate(host); isUsableMediaIP(ip) {
			s.listenMu.RUnlock()
			return ip
		}
	}
	s.listenMu.RUnlock()

	return "127.0.0.1"
}

func sanitizeMediaIPCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}

	if strings.Contains(candidate, ",") {
		parts := strings.Split(candidate, ",")
		candidate = strings.TrimSpace(parts[0])
	}

	if strings.EqualFold(candidate, "auto") || strings.EqualFold(candidate, "unspecified") {
		return ""
	}

	// Remove IPv6 brackets
	if strings.HasPrefix(candidate, "[") && strings.Contains(candidate, "]") {
		candidate = strings.Trim(candidate, "[]")
	}

	// Remove optional port portion
	if host, _, err := net.SplitHostPort(candidate); err == nil {
		candidate = host
	}

	if strings.EqualFold(candidate, "localhost") {
		return ""
	}

	return candidate
}

func isUsableMediaIP(candidate string) bool {
	if candidate == "" {
		return false
	}

	switch candidate {
	case "0.0.0.0", "::", "0:0:0:0:0:0:0:0":
		return false
	}

	return true
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
	case "UPDATE":
		s.handleUpdateMessage(message)
	case "INFO":
		s.handleInfoMessage(message)
	case "REFER":
		s.handleReferMessage(message)
	case "NOTIFY":
		s.handleNotifyMessage(message)
	case "MESSAGE":
		s.handleMessageMessage(message)
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
		"Allow":     "INVITE, ACK, BYE, CANCEL, PRACK, OPTIONS, UPDATE, INFO, REFER, NOTIFY, MESSAGE, SUBSCRIBE",
		"Supported": "replaces, siprec, norefersub",
	}

	s.sendResponse(message, 200, "OK", headers, nil)
	logger.Info("Successfully responded to OPTIONS request")
}

// handleInviteMessage handles INVITE requests
func (s *CustomSIPServer) handleInviteMessage(message *SIPMessage) {
	// Extract vendor-specific headers early for use throughout the call
	s.extractVendorInformation(message)

	// Generate correlation ID for request tracking
	// Try to extract from SIP header first, otherwise generate new
	correlationID := correlation.FromString(s.getHeaderValue(message, correlation.SIPHeader))
	if correlationID.IsEmpty() {
		correlationID = correlation.New()
	}

	// Build logger fields including vendor-specific identifiers
	logFields := logrus.Fields{
		"method":         "INVITE",
		"correlation_id": correlationID.String(),
		"call_id":        message.CallID,
	}
	// Add vendor type if detected
	if message.VendorType != "" && message.VendorType != "generic" {
		logFields["vendor_type"] = message.VendorType
	}
	// Add Oracle UCID if present
	if message.OracleUCID != "" {
		logFields["oracle_ucid"] = message.OracleUCID
	}
	// Add Oracle Conversation ID if present
	if message.OracleConversationID != "" {
		logFields["oracle_conversation_id"] = message.OracleConversationID
	}
	// Add Cisco Session-ID if present
	if message.SessionIDHeader != "" {
		logFields["cisco_session_id"] = message.SessionIDHeader
	}
	// Add Avaya UCID if present
	if len(message.UCIDHeaders) > 0 {
		logFields["ucid"] = strings.Join(message.UCIDHeaders, ";")
	}
	// Add Genesys Interaction ID if present
	if message.GenesysInteractionID != "" {
		logFields["genesys_interaction_id"] = message.GenesysInteractionID
	}
	// Add Genesys Conversation ID if present
	if message.GenesysConversationID != "" {
		logFields["genesys_conversation_id"] = message.GenesysConversationID
	}

	logger := s.logger.WithFields(logFields)
	logger.Info("Received INVITE request")

	// Create context with correlation ID for downstream operations
	ctx := correlation.WithCorrelationID(context.Background(), correlationID)

	// Extract client IP for authentication and rate limiting
	clientIP := ""
	if message.Connection != nil && message.Connection.remoteAddr != "" {
		clientIP = message.Connection.remoteAddr
		// Extract just the IP without port
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}
	}
	ctx = correlation.WithClientIP(ctx, clientIP)
	ctx = correlation.WithMethod(ctx, "INVITE")

	// Check SIP rate limiting first (before authentication)
	if s.handler != nil && s.handler.IsSIPRateLimitEnabled() {
		if !s.handler.CheckSIPRateLimit(clientIP, "INVITE") {
			// Rate limited - send 503 Service Unavailable
			logger.WithField("client_ip", clientIP).Warn("INVITE rate limited")
			headers := map[string]string{
				"Retry-After":            "60",
				correlation.SIPHeader:    correlationID.String(),
			}
			s.sendResponse(message, 503, "Service Unavailable - Rate Limit Exceeded", headers, nil)
			// Audit log for rate limiting
			audit.Log(ctx, s.logger, &audit.Event{
				Category:   "security",
				Action:     "rate_limited",
				Outcome:    audit.OutcomeFailure,
				CallID:     message.CallID,
				SIPHeaders: s.extractSIPHeadersForAudit(message),
				Details: map[string]interface{}{
					"client_ip":      clientIP,
					"method":         "INVITE",
					"reason":         "rate_limit_exceeded",
					"correlation_id": correlationID.String(),
				},
			})
			return
		}
	}

	if s.handler != nil && s.handler.IsAuthenticationEnabled() {
		authHeader := s.getHeaderValue(message, "Authorization")
		requestURI := message.RequestURI
		if requestURI == "" && message.Request != nil {
			requestURI = message.Request.Recipient.String()
		}

		authenticated, challenge := s.handler.AuthenticateRequest(authHeader, "INVITE", requestURI, clientIP)
		if !authenticated {
			if challenge != "" {
				// Send 401 Unauthorized with WWW-Authenticate challenge
				logger.WithField("client_ip", clientIP).Info("Sending authentication challenge")
				headers := map[string]string{
					"WWW-Authenticate":       challenge,
					correlation.SIPHeader:    correlationID.String(),
				}
				s.sendResponse(message, 401, "Unauthorized", headers, nil)
				// Audit log for auth challenge
				audit.Log(ctx, s.logger, &audit.Event{
					Category:   "security",
					Action:     "auth_challenge",
					Outcome:    audit.OutcomeFailure,
					CallID:     message.CallID,
					SIPHeaders: s.extractSIPHeadersForAudit(message),
					Details: map[string]interface{}{
						"client_ip":      clientIP,
						"reason":         "authentication_required",
						"request_uri":    requestURI,
						"correlation_id": correlationID.String(),
					},
				})
			} else {
				// IP blocked - send 403 Forbidden
				logger.WithField("client_ip", clientIP).Warn("Request blocked by IP access control")
				headers := map[string]string{
					correlation.SIPHeader: correlationID.String(),
				}
				s.sendResponse(message, 403, "Forbidden", headers, nil)
				// Audit log for IP block
				audit.Log(ctx, s.logger, &audit.Event{
					Category:   "security",
					Action:     "ip_blocked",
					Outcome:    audit.OutcomeFailure,
					CallID:     message.CallID,
					SIPHeaders: s.extractSIPHeadersForAudit(message),
					Details: map[string]interface{}{
						"client_ip":      clientIP,
						"reason":         "ip_access_denied",
						"request_uri":    requestURI,
						"correlation_id": correlationID.String(),
					},
				})
			}
			return
		}
	}

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
	// Create cancellable context for RTP forwarding goroutines
	callCtx, cancelFunc := context.WithCancel(callScope.Context())

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
		rtpCtx:           callCtx,     // Store context for RTP goroutines
		cancelCtx:        cancelFunc,  // Store cancel function for cleanup
	}
	mediaIP := s.resolveMediaIPAddress(message)

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

	if len(sdpData) == 0 {
		inviteErr = fmt.Errorf("SIPREC INVITE missing mandatory SDP part")
		callScope.RecordError(inviteErr)
		logger.WithError(inviteErr).Warn("Rejecting SIPREC INVITE without SDP part")
		s.sendResponse(message, 400, "Bad Request - Missing SDP", nil, nil)
		return
	}

	if len(rsMetadata) == 0 {
		inviteErr = fmt.Errorf("SIPREC INVITE missing mandatory rs-metadata part")
		callScope.RecordError(inviteErr)
		logger.WithError(inviteErr).Warn("Rejecting SIPREC INVITE without rs-metadata part")
		s.sendResponse(message, 400, "Bad Request - Missing SIPREC metadata", nil, nil)
		return
	}

	if sdpData != nil {
		callState.SDP = sdpData
		logger.WithField("sdp_size", len(sdpData)).Debug("Extracted SDP from SIPREC multipart")
	}

	var recordingSession *siprec.RecordingSession
	var parsedMetadata *siprec.RSMetadata
	if rsMetadata != nil {
		logger.WithField("metadata_size", len(rsMetadata)).Info("Extracted SIPREC metadata")

		metadataCtx, metadataSpan := tracing.StartSpan(callCtx, "siprec.metadata.parse", trace.WithAttributes(
			attribute.Int("siprec.metadata.bytes", len(rsMetadata)),
		))
		var err error
		parsedMetadata, err = s.parseSiprecMetadata(rsMetadata, message.ContentType)
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

		// Store UUI and X-headers in recording session metadata
		s.storeUUIAndXHeadersInSession(recordingSession, message)

		// Propagate session metadata to conversation tracking for AMQP publishing
		if s.handler != nil && s.handler.SessionMetadataCallback != nil && recordingSession.ExtendedMetadata != nil {
			s.handler.SessionMetadataCallback(recordingSession.ID, recordingSession.ExtendedMetadata)
		}

		if callScope.Metadata() != nil {
			callScope.Metadata().SetSessionID(recordingSession.ID)
			if tenant := audit.TenantFromSession(recordingSession); tenant != "" {
				callScope.Metadata().SetTenant(tenant)
			}
			callScope.Metadata().SetUsers(audit.UsersFromSession(recordingSession))
			// Store vendor metadata (Oracle UCID, Conversation ID, etc.) for audit logging
			if recordingSession.ExtendedMetadata != nil {
				callScope.Metadata().SetVendorMetadata(recordingSession.ExtendedMetadata)
			}
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

		// Track vendor-specific metrics
		vendorType := message.VendorType
		if vendorType == "" {
			vendorType = "generic"
		}
		if metrics.VendorSessionsActive != nil {
			metrics.VendorSessionsActive.WithLabelValues(vendorType).Inc()
		}
		if metrics.VendorSessionsTotal != nil {
			metrics.VendorSessionsTotal.WithLabelValues(vendorType).Inc()
		}
		// Track metadata extractions
		if metrics.VendorMetadataExtractions != nil {
			if message.OracleUCID != "" {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "oracle_ucid").Inc()
			}
			if message.OracleConversationID != "" {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "oracle_conversation_id").Inc()
			}
			if len(message.UCIDHeaders) > 0 {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "ucid").Inc()
			}
			if message.SessionIDHeader != "" {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "cisco_session_id").Inc()
			}
			if message.GenesysInteractionID != "" {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "genesys_interaction_id").Inc()
			}
			if message.GenesysConversationID != "" {
				metrics.VendorMetadataExtractions.WithLabelValues(vendorType, "genesys_conversation_id").Inc()
			}
		}

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
				// Add vendor-specific identifiers to CDR
				if message.VendorType != "" && message.VendorType != "generic" {
					update.VendorType = &message.VendorType
					hasUpdates = true
				}
				if message.OracleUCID != "" {
					update.OracleUCID = &message.OracleUCID
					hasUpdates = true
				}
				if message.OracleConversationID != "" {
					update.ConversationID = &message.OracleConversationID
					hasUpdates = true
				}
				if message.SessionIDHeader != "" {
					update.CiscoSessionID = &message.SessionIDHeader
					hasUpdates = true
				}
				if len(message.UCIDHeaders) > 0 {
					ucid := strings.Join(message.UCIDHeaders, ";")
					update.UCID = &ucid
					hasUpdates = true
				}
				// Genesys-specific CDR fields
				if message.GenesysInteractionID != "" {
					update.GenesysInteractionID = &message.GenesysInteractionID
					hasUpdates = true
				}
				if message.GenesysConversationID != "" {
					update.GenesysConversationID = &message.GenesysConversationID
					hasUpdates = true
				}
				if message.GenesysQueueName != "" {
					update.GenesysQueueName = &message.GenesysQueueName
					hasUpdates = true
				}
				if message.GenesysAgentID != "" {
					update.GenesysAgentID = &message.GenesysAgentID
					hasUpdates = true
				}
				if message.GenesysCampaignID != "" {
					update.GenesysCampaignID = &message.GenesysCampaignID
					hasUpdates = true
				}
				// Asterisk-specific CDR fields
				if message.AsteriskUniqueID != "" {
					update.AsteriskUniqueID = &message.AsteriskUniqueID
					hasUpdates = true
				}
				if message.AsteriskLinkedID != "" {
					update.AsteriskLinkedID = &message.AsteriskLinkedID
					hasUpdates = true
				}
				if message.AsteriskChannelID != "" {
					update.AsteriskChannelID = &message.AsteriskChannelID
					hasUpdates = true
				}
				if message.AsteriskAccountCode != "" {
					update.AsteriskAccountCode = &message.AsteriskAccountCode
					hasUpdates = true
				}
				if message.AsteriskContext != "" {
					update.AsteriskContext = &message.AsteriskContext
					hasUpdates = true
				}
				// FreeSWITCH-specific CDR fields
				if message.FreeSWITCHUUID != "" {
					update.FreeSWITCHUUID = &message.FreeSWITCHUUID
					hasUpdates = true
				}
				if message.FreeSWITCHCoreUUID != "" {
					update.FreeSWITCHCoreUUID = &message.FreeSWITCHCoreUUID
					hasUpdates = true
				}
				if message.FreeSWITCHChannelName != "" {
					update.FreeSWITCHChannelName = &message.FreeSWITCHChannelName
					hasUpdates = true
				}
				if message.FreeSWITCHProfileName != "" {
					update.FreeSWITCHProfileName = &message.FreeSWITCHProfileName
					hasUpdates = true
				}
				if message.FreeSWITCHAccountCode != "" {
					update.FreeSWITCHAccountCode = &message.FreeSWITCHAccountCode
					hasUpdates = true
				}
				// OpenSIPS-specific CDR fields
				if message.OpenSIPSDialogID != "" {
					update.OpenSIPSDialogID = &message.OpenSIPSDialogID
					hasUpdates = true
				}
				if message.OpenSIPSTransactionID != "" {
					update.OpenSIPSTransactionID = &message.OpenSIPSTransactionID
					hasUpdates = true
				}
				if message.OpenSIPSCallID != "" {
					update.OpenSIPSCallID = &message.OpenSIPSCallID
					hasUpdates = true
				}
				// NICE-specific CDR fields
				if message.NICEInteractionID != "" {
					update.NICEInteractionID = &message.NICEInteractionID
					hasUpdates = true
				}
				if message.NICESessionID != "" {
					update.NICESessionID = &message.NICESessionID
					hasUpdates = true
				}
				if message.NICERecordingID != "" {
					update.NICERecordingID = &message.NICERecordingID
					hasUpdates = true
				}
				if message.NICEContactID != "" {
					update.NICEContactID = &message.NICEContactID
					hasUpdates = true
				}
				if message.NICEAgentID != "" {
					update.NICEAgentID = &message.NICEAgentID
					hasUpdates = true
				}
				if message.NICECallID != "" {
					update.NICECallID = &message.NICECallID
					hasUpdates = true
				}
				// Avaya-specific CDR fields
				if message.AvayaUCID != "" {
					update.AvayaUCID = &message.AvayaUCID
					hasUpdates = true
				}
				if message.AvayaConfID != "" {
					update.AvayaConfID = &message.AvayaConfID
					hasUpdates = true
				}
				if message.AvayaStationID != "" {
					update.AvayaStationID = &message.AvayaStationID
					hasUpdates = true
				}
				if message.AvayaAgentID != "" {
					update.AvayaAgentID = &message.AvayaAgentID
					hasUpdates = true
				}
				if message.AvayaVDN != "" {
					update.AvayaVDN = &message.AvayaVDN
					hasUpdates = true
				}
				if message.AvayaSkillGroup != "" {
					update.AvayaSkillGroup = &message.AvayaSkillGroup
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
			// Process policy updates through policy manager
			s.handler.ProcessPolicyUpdates(recordingSession.ID, parsedMetadata)
		}

		// RFC 7866 Policy enforcement check
		policyDecision := s.handler.EvaluateRecordingPolicy(recordingSession, parsedMetadata)
		if policyDecision.IsBlocked() {
			inviteErr = fmt.Errorf("recording blocked by policy: %s", policyDecision.Reason)
			callScope.RecordError(inviteErr)
			logger.WithFields(logrus.Fields{
				"policy_id": policyDecision.PolicyID,
				"reason":    policyDecision.Reason,
			}).Warn("Recording blocked by policy")
			s.notifyMetadataEvent(callCtx, recordingSession, message.CallID, "policy.blocked", map[string]interface{}{
				"policy_id": policyDecision.PolicyID,
				"reason":    policyDecision.Reason,
			})
			audit.Log(callCtx, s.logger, &audit.Event{
				Category: "policy",
				Action:   "recording_blocked",
				Outcome:  audit.OutcomeFailure,
				CallID:   message.CallID,
				Details: map[string]interface{}{
					"policy_id": policyDecision.PolicyID,
					"reason":    policyDecision.Reason,
				},
			})
			s.sendResponse(message, 403, "Forbidden - Recording not permitted by policy", nil, nil)
			return
		}

		// Store policy decision on recording session
		recordingSession.PolicyID = policyDecision.PolicyID
		if policyDecision.RetentionDays > 0 {
			recordingSession.RetentionPeriod = time.Duration(policyDecision.RetentionDays) * 24 * time.Hour
		}

		logger.WithFields(logrus.Fields{
			"policy_action":     policyDecision.Action,
			"policy_id":         policyDecision.PolicyID,
			"allow_audio":       policyDecision.AllowAudio,
			"allow_video":       policyDecision.AllowVideo,
			"retention_days":    policyDecision.RetentionDays,
		}).Debug("Policy evaluation completed")
	}

	// Parse the received SDP if available
	var receivedSDP *sdp.SessionDescription
	if len(sdpData) > 0 {
		parsed, err := ParseSDPTolerant(sdpData, s.logger)
		if err != nil {
			logger.WithError(err).Warn("Failed to parse received SDP offer")
		} else {
			receivedSDP = parsed
		}
	}

	if receivedSDP == nil || len(receivedSDP.MediaDescriptions) == 0 {
		inviteErr = fmt.Errorf("invalid SIPREC offer: missing SDP media")
		callScope.RecordError(inviteErr)
		logger.WithError(inviteErr).Warn("Rejecting SIPREC INVITE without valid SDP")
		s.sendResponse(message, 488, "Not Acceptable Here", nil, nil)
		return
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
			logger.WithField("audio_stream_count", len(audioStreams)).Info("Detected audio streams in received SDP")
		} else {
			logger.Warn("No received SDP available, will use default")
		}

		if len(audioStreams) == 0 {
			inviteErr = fmt.Errorf("invalid SIPREC offer: no audio streams detected")
			callScope.RecordError(inviteErr)
			logger.WithError(inviteErr).Warn("Rejecting SIPREC INVITE without audio streams")
			s.sendResponse(message, 488, "Not Acceptable Here", nil, nil)
			return
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
			// Use per-call timeout if configured, otherwise fall back to global
			rtpTimeout := siprec.GetEffectiveRTPTimeout(recordingSession, mediaConfig.RTPTimeout)
			if rtpTimeout == 0 {
				rtpTimeout = 30 * time.Second
			}
			forwarder, err := media.NewRTPForwarder(rtpTimeout, recordingSession, s.logger, mediaConfig.PIIAudioEnabled, mediaConfig.EncryptedRecorder)
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

		// Configure audio encoder for format conversion if not using WAV
		recordingFormat := "wav"
		if s.handler.Config.Recording != nil && s.handler.Config.Recording.Format != "" {
			recordingFormat = s.handler.Config.Recording.Format
		}
		if recordingFormat != "wav" {
			encoderConfig := &audio.EncoderConfig{
				Format:     audio.ParseFormat(recordingFormat),
				SampleRate: 8000,
				Channels:   1,
				BitRate:    128,
				Quality:    5,
			}
			if s.handler.Config.Recording != nil {
				if s.handler.Config.Recording.MP3Bitrate > 0 {
					encoderConfig.BitRate = s.handler.Config.Recording.MP3Bitrate
				}
				if s.handler.Config.Recording.OpusBitrate > 0 && (recordingFormat == "opus" || recordingFormat == "ogg") {
					encoderConfig.BitRate = s.handler.Config.Recording.OpusBitrate
				}
				if s.handler.Config.Recording.Quality > 0 {
					encoderConfig.Quality = s.handler.Config.Recording.Quality
				}
			}
			encoder := audio.NewAudioEncoder(encoderConfig, s.logger)
			for _, f := range forwarders {
				f.AudioEncoder = encoder
				f.TargetFormat = recordingFormat
			}
			logger.WithField("format", recordingFormat).Debug("Audio encoder configured for format conversion")
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

			// Inject per-stream metadata so AMQP consumers can identify participants
			if s.handler != nil && s.handler.SessionMetadataCallback != nil {
				streamMeta := buildStreamMetadata(streamID, parsedMetadata, recordingSession)
				s.handler.SessionMetadataCallback(streamCallID, streamMeta)
			}
		}

		if receivedSDP != nil {
			logger.WithFields(logrus.Fields{
				"forwarder_count":  len(forwarders),
				"media_desc_count": len(receivedSDP.MediaDescriptions),
			}).Info("Generating SDP response for multiple forwarders")
			responseSDPBytes, err := s.handler.generateSDPResponseForForwarders(receivedSDP, mediaIP, forwarders).Marshal()
			if err != nil {
				cleanupForwarders()
				inviteErr = err
				callScope.RecordError(err)
				logger.WithError(err).Error("Failed to marshal SDP response")
				s.sendResponse(message, 500, "Internal Server Error", nil, nil)
				return
			}
			responseSDP = responseSDPBytes
			logger.WithField("response_sdp_size", len(responseSDP)).Debug("Generated multi-stream SDP response")
		} else {
			logger.Warn("Using single-stream SDP response (receivedSDP is nil)")
			sdpResponse := s.handler.generateSDPResponseWithPort(nil, mediaIP, forwarders[0].LocalPort, forwarders[0])
			responseSDP, _ = sdpResponse.Marshal()
		}
	} else {
		// For non-SIPREC calls, create an RTP forwarder to receive and record audio
		mediaConfig := s.handler.Config.MediaConfig
		if mediaConfig == nil {
			logger.Error("Media configuration missing for non-SIPREC call")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		// Use default RTP timeout
		rtpTimeout := mediaConfig.RTPTimeout
		if rtpTimeout == 0 {
			rtpTimeout = 30 * time.Second
		}

		// Create RTP forwarder (allocates ports internally)
		forwarder, err := media.NewRTPForwarder(rtpTimeout, nil, s.logger, false, nil)
		if err != nil {
			logger.WithError(err).Error("Failed to create RTP forwarder for non-SIPREC call")
			s.sendResponse(message, 500, "Internal Server Error", nil, nil)
			return
		}

		// Store forwarder in call state for cleanup
		callState.RTPForwarder = forwarder

		// Generate SDP with the allocated port
		responseSDP = s.generateSiprecSDP(mediaIP, forwarder.LocalPort)

		// Create a no-op STT provider for non-SIPREC calls
		sttProvider := func(ctx context.Context, vendor string, reader io.Reader, callUUID string) error {
			_, err := io.Copy(io.Discard, reader)
			return err
		}

		// Start RTP forwarding to create the UDP listener
		media.StartRTPForwarding(callCtx, forwarder, message.CallID, mediaConfig, sttProvider)

		logger.WithFields(logrus.Fields{
			"rtp_port":  forwarder.LocalPort,
			"rtcp_port": forwarder.RTCPPort,
		}).Info("Started RTP listener for non-SIPREC call")
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

	// Sync to handler.ActiveCalls for pause/resume API support
	s.syncCallToActiveList(message.CallID, callState)

	if callState.TraceScope != nil {
		callState.TraceScope.SetAttributes(attribute.String("siprec.state", "awaiting_ack"))
		callState.TraceScope.Span().AddEvent("siprec.invite.accepted", trace.WithAttributes(
			attribute.Bool("siprec.has_metadata", recordingSession != nil),
		))
	}
	audit.Log(callCtx, s.logger, &audit.Event{
		Category:   "sip",
		Action:     "invite",
		Outcome:    audit.OutcomeSuccess,
		CallID:     message.CallID,
		SessionID:  sessionIDFromState(callState),
		SIPHeaders: s.extractSIPHeadersForAudit(message),
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
	var reinviteParsedMetadata *siprec.RSMetadata

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

			// Store UUI and X-headers in recording session metadata
			s.storeUUIAndXHeadersInSession(recordingSession, message)

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
		reinviteParsedMetadata = parsedMetadata

		if callScope.Metadata() != nil && callState.RecordingSession != nil {
			if tenant := audit.TenantFromSession(callState.RecordingSession); tenant != "" {
				callScope.Metadata().SetTenant(tenant)
			}
			callScope.Metadata().SetSessionID(callState.RecordingSession.ID)
			callScope.Metadata().SetUsers(audit.UsersFromSession(callState.RecordingSession))
			// Store vendor metadata (Oracle UCID, Conversation ID, etc.) for audit logging
			if callState.RecordingSession.ExtendedMetadata != nil {
				callScope.Metadata().SetVendorMetadata(callState.RecordingSession.ExtendedMetadata)
			}
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
	mediaIP := s.resolveMediaIPAddress(message)

	// Fix for Resume after Long Hold:
	// Check for dead/timed-out forwarders and remove them so new ones are allocated.
	if callState.RTPForwarder != nil {
		select {
		case <-callState.RTPForwarder.StopChan:
			logger.Warn("Cleanup: Removing dead RTP forwarder (legacy primary) before re-INVITE")
			callState.RTPForwarder = nil
		default:
			// Forwarder is still active
		}
	}

	if len(callState.RTPForwarders) > 0 {
		var activeForwarders []*media.RTPForwarder
		for _, f := range callState.RTPForwarders {
			isDead := false
			select {
			case <-f.StopChan:
				isDead = true
			default:
			}
			if !isDead {
				activeForwarders = append(activeForwarders, f)
			} else {
				logger.Warn("Cleanup: Removing dead RTP forwarder from list")
			}
		}
		callState.RTPForwarders = activeForwarders
	}

	if len(callState.StreamForwarders) > 0 {
		for id, f := range callState.StreamForwarders {
			select {
			case <-f.StopChan:
				logger.WithField("stream_id", id).Warn("Cleanup: Removing dead stream forwarder")
				delete(callState.StreamForwarders, id)
			default:
			}
		}
	}

	// Generate response using existing RTP forwarder
	responseHeaders := map[string]string{
		"Contact":   s.buildContactHeader(message),
		"Supported": "siprec",
		"Accept":    "application/sdp, application/rs-metadata+xml, multipart/mixed",
	}

	var responseSDP []byte
	if callState.RTPForwarder != nil || (sdpData != nil && len(sdpData) > 0) {
		var parsedSDP *sdp.SessionDescription
		if sdpData != nil && len(sdpData) > 0 {
			parsed, err := ParseSDPTolerant(sdpData, s.logger)
			if err != nil {
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
			// Use per-call timeout if configured, otherwise fall back to global
			rtpTimeout := siprec.GetEffectiveRTPTimeout(callState.RecordingSession, mediaConfig.RTPTimeout)
			if rtpTimeout == 0 {
				rtpTimeout = 30 * time.Second
			}
			forwarder, err := media.NewRTPForwarder(rtpTimeout, callState.RecordingSession, s.logger, mediaConfig.PIIAudioEnabled, mediaConfig.EncryptedRecorder)
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
			// Use the call's RTP context so all forwarders share the same cancellation
			forwarderCtx := callState.rtpCtx
			if forwarderCtx == nil {
				forwarderCtx = reinviteCtx // Fallback if rtpCtx not set
			}
			streamCallID := fmt.Sprintf("%s_%s", message.CallID, streamID)
			media.StartRTPForwarding(forwarderCtx, forwarders[idx], streamCallID, mediaConfig, sttProvider)

			// Inject per-stream metadata so AMQP consumers can identify participants
			if s.handler != nil && s.handler.SessionMetadataCallback != nil {
				streamMeta := buildStreamMetadata(streamID, reinviteParsedMetadata, callState.RecordingSession)
				s.handler.SessionMetadataCallback(streamCallID, streamMeta)
			}
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
			sdpResponse := s.handler.generateSDPResponseForForwarders(parsedSDP, mediaIP, forwarders)
			responseSDP, _ = sdpResponse.Marshal()
		} else {
			responseSDP = s.generateSiprecSDP(mediaIP, forwarders[0].LocalPort)
		}
	} else {
		// Fallback to basic SDP generation if no forwarder exists
		responseSDP = s.generateSiprecSDP(mediaIP, 0) // 0 means allocate port dynamically
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

// handleRegularInvite rejects regular (non-SIPREC) INVITE requests
// This server is a SIPREC Recording Server (SRS) and only accepts SIPREC sessions
func (s *CustomSIPServer) handleRegularInvite(message *SIPMessage) {
	logger := s.logger.WithField("siprec", false)

	logger.WithFields(logrus.Fields{
		"call_id":    message.CallID,
		"from":       s.getHeaderValue(message, "From"),
		"to":         s.getHeaderValue(message, "To"),
		"user_agent": s.getHeaderValue(message, "User-Agent"),
	}).Warn("Rejecting non-SIPREC INVITE - this server only accepts SIPREC sessions")

	// Reject with 403 Forbidden - this is a SIPREC-only server
	// Include a reason phrase that clearly indicates why the call was rejected
	s.sendResponse(message, 403, "Forbidden - SIPREC sessions only", nil, nil)
}

// handleRegularReInvite rejects regular (non-SIPREC) re-INVITE requests
// Since initial regular INVITEs are rejected, this should rarely be called
func (s *CustomSIPServer) handleRegularReInvite(message *SIPMessage, callState *CallState) {
	logger := s.logger.WithField("regular_reinvite", true)

	logger.WithFields(logrus.Fields{
		"call_id":        message.CallID,
		"existing_state": callState.State,
	}).Warn("Rejecting non-SIPREC re-INVITE - this server only accepts SIPREC sessions")

	// Reject with 403 Forbidden
	s.sendResponse(message, 403, "Forbidden - SIPREC sessions only", nil, nil)
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

	logger.WithFields(logrus.Fields{
		"call_state":  callState.State,
		"pending_ack": callState.PendingAckCSeq,
		"cseq":        seq,
	}).Debug("Processing BYE request")

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
		logger.WithField("pending_ack_cseq", callState.PendingAckCSeq).Warn("BYE received before ACK completed; proceeding for interoperability")
	}

	if callState.State != "connected" && callState.State != "terminating" && callState.State != "awaiting_ack" {
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

	if callState.State == "awaiting_ack" {
		logger.Warn("BYE received while awaiting ACK; treating as connected call")
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
	callState.PendingAckCSeq = 0
	callState.State = "terminating"
	callState.LastActivity = time.Now()
	s.callMutex.Unlock()

	// Respond immediately before running cleanup to avoid retransmitted BYEs
	s.sendResponse(message, 200, "OK", nil, nil)
	if callState.TraceScope != nil {
		callState.TraceScope.Span().AddEvent("siprec.bye.acknowledged", trace.WithAttributes(
			attribute.Int("sip.cseq", seq),
		))
	}
	logger.Info("Successfully responded to BYE request")

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
		Category:   "sip",
		Action:     "bye",
		Outcome:    auditOutcome,
		CallID:     message.CallID,
		SessionID:  sessionID,
		SIPHeaders: s.extractSIPHeadersForAudit(message),
		Details: map[string]interface{}{
			"call_state_present": callState != nil,
		},
	})
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
		Category:   "sip",
		Action:     "cancel",
		Outcome:    audit.OutcomeSuccess,
		CallID:     message.CallID,
		SessionID:  sessionIDFromState(callState),
		SIPHeaders: s.extractSIPHeadersForAudit(message),
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
	logger.Info("Received ACK request")

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

	logger.WithFields(logrus.Fields{
		"call_id": message.CallID,
		"state":   "connected",
		"cseq":    seq,
	}).Info("Call state transitioned to connected after ACK")

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

// handleUpdateMessage handles UPDATE requests for mid-call SDP renegotiation
// UPDATE allows changing session parameters without affecting dialog state (RFC 3311)
func (s *CustomSIPServer) handleUpdateMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "UPDATE")
	logger.Info("Received UPDATE request")

	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("UPDATE received for unknown Call-ID")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	// UPDATE is used for mid-call SDP renegotiation (hold/resume, codec changes)
	// For a SIPREC server, we primarily care about tracking session state
	callState.LastActivity = time.Now()

	// Check if there's SDP in the UPDATE
	contentType := s.getHeaderValue(message, "Content-Type")
	if len(message.Body) > 0 && strings.Contains(strings.ToLower(contentType), "application/sdp") {
		// Parse the new SDP
		parsedSDP, err := ParseSDPTolerant(message.Body, s.logger)
		if err != nil {
			logger.WithError(err).Warn("Failed to parse SDP in UPDATE")
			s.sendResponse(message, 400, "Bad Request - Invalid SDP", nil, nil)
			return
		}

		// Check for hold indication (c=0.0.0.0 or a=sendonly/inactive)
		isHold := false
		for _, md := range parsedSDP.MediaDescriptions {
			if md.ConnectionInformation != nil && md.ConnectionInformation.Address != nil {
				if md.ConnectionInformation.Address.Address == "0.0.0.0" {
					isHold = true
					break
				}
			}
			for _, attr := range md.Attributes {
				if attr.Key == "sendonly" || attr.Key == "inactive" {
					isHold = true
					break
				}
			}
		}

		if isHold {
			logger.WithField("call_id", message.CallID).Info("Call placed on hold via UPDATE")
			// Optionally pause recording during hold
			if callState.RTPForwarder != nil {
				callState.RTPForwarder.Pause(false, true) // Pause transcription only during hold
			}
		} else {
			logger.WithField("call_id", message.CallID).Info("Call resumed from hold via UPDATE")
			if callState.RTPForwarder != nil {
				callState.RTPForwarder.Resume()
			}
		}

		// Update stored SDP
		callState.SDP = message.Body
	}

	// Send 200 OK response
	// For UPDATE, we should echo back the SDP if we received one
	var responseSDP []byte
	if len(message.Body) > 0 && strings.Contains(strings.ToLower(contentType), "application/sdp") {
		// Generate response SDP based on current state
		mediaIP := s.resolveMediaIPAddress(message)
		if callState.RTPForwarder != nil {
			responseSDP = s.generateSiprecSDP(mediaIP, callState.RTPForwarder.LocalPort)
		}
	}

	responseHeaders := map[string]string{
		"Contact": s.buildContactHeader(message),
	}
	s.sendResponse(message, 200, "OK", responseHeaders, responseSDP)
	logger.WithField("call_id", message.CallID).Info("Successfully responded to UPDATE")
}

// handleInfoMessage handles INFO requests for mid-call signaling
// INFO is used for DTMF, call progress, and other in-dialog information (RFC 6086)
func (s *CustomSIPServer) handleInfoMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "INFO")
	logger.Info("Received INFO request")

	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("INFO received for unknown Call-ID")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	callState.LastActivity = time.Now()

	// Parse INFO content type to determine what kind of info this is
	contentType := s.getHeaderValue(message, "Content-Type")

	switch {
	case strings.Contains(strings.ToLower(contentType), "application/dtmf-relay"):
		// DTMF via INFO (RFC 2833 alternative)
		s.handleDTMFInfo(message, callState, logger)

	case strings.Contains(strings.ToLower(contentType), "application/dtmf"):
		// Simple DTMF
		s.handleDTMFInfo(message, callState, logger)

	case strings.Contains(strings.ToLower(contentType), "application/media_control+xml"):
		// Media control (e.g., picture fast update for video)
		logger.WithField("call_id", message.CallID).Debug("Received media control INFO")

	case strings.Contains(strings.ToLower(contentType), "application/broadsoft"):
		// Broadsoft proprietary info
		logger.WithField("call_id", message.CallID).Debug("Received Broadsoft INFO")

	default:
		logger.WithFields(logrus.Fields{
			"call_id":      message.CallID,
			"content_type": contentType,
		}).Debug("Received INFO with unhandled content type")
	}

	// Always acknowledge INFO requests
	s.sendResponse(message, 200, "OK", nil, nil)
	logger.WithField("call_id", message.CallID).Debug("Acknowledged INFO request")
}

// handleDTMFInfo processes DTMF digits received via INFO
func (s *CustomSIPServer) handleDTMFInfo(message *SIPMessage, callState *CallState, logger *logrus.Entry) {
	if len(message.Body) == 0 {
		return
	}

	body := string(message.Body)
	var digit string

	// Parse DTMF-Relay format: Signal=5\r\nDuration=160
	if strings.Contains(body, "Signal=") {
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Signal=") {
				digit = strings.TrimPrefix(line, "Signal=")
				digit = strings.TrimSpace(digit)
				break
			}
		}
	} else {
		// Simple digit format
		digit = strings.TrimSpace(body)
	}

	if digit != "" {
		logger.WithFields(logrus.Fields{
			"call_id": message.CallID,
			"digit":   digit,
		}).Info("Received DTMF digit via INFO")

		// Could store DTMF history in recording session metadata
		if callState.RecordingSession != nil {
			// Add to metadata if needed
		}
	}
}

// handleReferMessage handles REFER requests for call transfers
// REFER is used for blind and attended transfers (RFC 3515)
func (s *CustomSIPServer) handleReferMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "REFER")
	logger.Info("Received REFER request")

	callState := s.getCallState(message.CallID)
	if callState == nil {
		logger.WithField("call_id", message.CallID).Warn("REFER received for unknown Call-ID")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	callState.LastActivity = time.Now()

	// Get the Refer-To header which contains the transfer target
	referTo := s.getHeaderValue(message, "Refer-To")
	if referTo == "" {
		logger.Warn("REFER request missing Refer-To header")
		s.sendResponse(message, 400, "Bad Request - Missing Refer-To", nil, nil)
		return
	}

	// For a SIPREC server, we don't actually perform the transfer
	// We just acknowledge that we're aware of it and continue recording
	// The actual transfer happens between the SBC and the endpoints
	logger.WithFields(logrus.Fields{
		"call_id":  message.CallID,
		"refer_to": referTo,
	}).Info("Call transfer initiated via REFER - continuing recording")

	// Accept the REFER with 202 Accepted
	// This tells the referrer we'll attempt the transfer (even though we won't)
	responseHeaders := map[string]string{
		"Contact": s.buildContactHeader(message),
	}
	s.sendResponse(message, 202, "Accepted", responseHeaders, nil)

	// Send NOTIFY to indicate transfer progress
	// For SIPREC, we can send a 200 OK NOTIFY to indicate success
	// (since we're just continuing to record)
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.sendReferNotify(message, callState, "SIP/2.0 200 OK")
	}()
}

// sendReferNotify sends a NOTIFY for REFER progress
func (s *CustomSIPServer) sendReferNotify(originalRefer *SIPMessage, callState *CallState, sipfrag string) {
	logger := s.logger.WithFields(logrus.Fields{
		"call_id": originalRefer.CallID,
		"sipfrag": sipfrag,
	})

	// Construct NOTIFY body with SIP fragment
	notifyBody := fmt.Sprintf("SIP/2.0 %s\r\n", sipfrag)

	// Get the CSeq from the original REFER
	referCSeq, _ := parseCSeq(originalRefer.CSeq)

	// Build NOTIFY request headers
	notifyHeaders := map[string]string{
		"Event":              "refer",
		"Subscription-State": "terminated;reason=noresource",
		"Content-Type":       "message/sipfrag;version=2.0",
	}

	// Try to send NOTIFY using sipgo's client transaction
	if s.ua != nil {
		// Get From, To, and Contact from original REFER
		from := s.getHeaderValue(originalRefer, "To")   // Our To becomes From
		to := s.getHeaderValue(originalRefer, "From")     // Their From becomes To
		contact := s.buildContactHeader(originalRefer)

		// Create request using sipgo
		notifyReq := sipparser.NewRequest(
			"NOTIFY",
			sipparser.Uri{
				Scheme: "sip",
				User:   "",
				Host:   originalRefer.Connection.remoteAddr,
			},
		)

		// Add required headers
		notifyReq.AppendHeader(sipparser.NewHeader("From", from))
		notifyReq.AppendHeader(sipparser.NewHeader("To", to))
		notifyReq.AppendHeader(sipparser.NewHeader("Call-ID", originalRefer.CallID))
		notifyReq.AppendHeader(sipparser.NewHeader("CSeq", fmt.Sprintf("%d NOTIFY", referCSeq+1)))
		notifyReq.AppendHeader(sipparser.NewHeader("Contact", contact))
		notifyReq.AppendHeader(sipparser.NewHeader("Event", notifyHeaders["Event"]))
		notifyReq.AppendHeader(sipparser.NewHeader("Subscription-State", notifyHeaders["Subscription-State"]))
		notifyReq.AppendHeader(sipparser.NewHeader("Content-Type", notifyHeaders["Content-Type"]))
		notifyReq.AppendHeader(sipparser.NewHeader("Content-Length", fmt.Sprintf("%d", len(notifyBody))))
		notifyReq.SetBody([]byte(notifyBody))

		// Send the NOTIFY request
		tx, err := s.ua.TransactionLayer().Request(context.Background(), notifyReq)
		if err != nil {
			logger.WithError(err).Warn("Failed to send REFER NOTIFY request")
			return
		}

		// Wait for response in a goroutine to avoid blocking
		go func() {
			select {
			case res := <-tx.Responses():
				if res != nil {
					logger.WithField("status", res.StatusCode).Debug("Received response to REFER NOTIFY")
				}
			case <-time.After(5 * time.Second):
				logger.Debug("Timeout waiting for REFER NOTIFY response")
			}
		}()

		logger.Info("Sent REFER NOTIFY successfully")
	} else {
		logger.Warn("Cannot send REFER NOTIFY: user agent not initialized")
	}
}

// handleNotifyMessage handles NOTIFY requests for event notifications
// NOTIFY is used to deliver event state (RFC 3265)
func (s *CustomSIPServer) handleNotifyMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "NOTIFY")
	logger.Info("Received NOTIFY request")

	callState := s.getCallState(message.CallID)
	if callState == nil {
		// NOTIFY might come for subscriptions we don't track
		logger.WithField("call_id", message.CallID).Debug("NOTIFY received for unknown Call-ID")
		s.sendResponse(message, 481, "Call/Transaction Does Not Exist", nil, nil)
		return
	}

	callState.LastActivity = time.Now()

	// Get the Event header
	event := s.getHeaderValue(message, "Event")
	subscriptionState := s.getHeaderValue(message, "Subscription-State")

	logger.WithFields(logrus.Fields{
		"call_id":            message.CallID,
		"event":              event,
		"subscription_state": subscriptionState,
	}).Debug("Processing NOTIFY")

	// Check for refer event (transfer progress)
	if strings.HasPrefix(strings.ToLower(event), "refer") {
		// This is a NOTIFY for a REFER we initiated (which we don't do as SRS)
		// Just acknowledge it
		logger.WithField("call_id", message.CallID).Debug("Received REFER progress NOTIFY")
	}

	// Acknowledge the NOTIFY
	s.sendResponse(message, 200, "OK", nil, nil)
}

// handleMessageMessage handles MESSAGE requests for instant messaging
// MESSAGE is used for SIP-based instant messaging (RFC 3428)
func (s *CustomSIPServer) handleMessageMessage(message *SIPMessage) {
	logger := s.logger.WithField("method", "MESSAGE")
	logger.Info("Received MESSAGE request")

	// MESSAGE can be in-dialog or out-of-dialog
	// For SIPREC, we generally don't need to handle instant messages
	// but we should acknowledge them gracefully

	callState := s.getCallState(message.CallID)
	if callState != nil {
		callState.LastActivity = time.Now()
	}

	contentType := s.getHeaderValue(message, "Content-Type")
	logger.WithFields(logrus.Fields{
		"call_id":      message.CallID,
		"content_type": contentType,
		"body_size":    len(message.Body),
	}).Debug("MESSAGE content received")

	// Acknowledge the message
	s.sendResponse(message, 200, "OK", nil, nil)
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

	// Add Server header to all responses
	resp.AppendHeader(sipparser.NewHeader("Server", version.ServerHeader()))

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

	// Cancel RTP forwarding context to stop all goroutines
	if callState.cancelCtx != nil {
		callState.cancelCtx()
		callState.cancelCtx = nil
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

		// Decrement vendor-specific session counter
		vendorType := "generic"
		if v, ok := callState.RecordingSession.ExtendedMetadata["sip_vendor_type"]; ok && v != "" {
			vendorType = v
		}
		if metrics.VendorSessionsActive != nil {
			metrics.VendorSessionsActive.WithLabelValues(vendorType).Dec()
		}
	}

	s.logger.WithFields(logrus.Fields{
		"call_id":            callID,
		"rtp_forwarders":     len(callState.RTPForwarders),
		"stream_forwarders":  len(callState.StreamForwarders),
		"single_forwarder":   callState.RTPForwarder != nil,
	}).Debug("Starting forwarder cleanup in finalizeCall")

	recordingPaths := make([]string, 0, len(callState.RTPForwarders))
	if len(callState.RTPForwarders) > 0 {
		s.logger.WithField("call_id", callID).Debug("Cleaning up RTPForwarders array")
		for _, forwarder := range callState.RTPForwarders {
			if forwarder == nil {
				continue
			}
			forwarder.Stop()
			forwarder.Cleanup()
			if forwarder.RecordingPath != "" {
				recordingPaths = append(recordingPaths, forwarder.RecordingPath)
			}
		}
		callState.RTPForwarders = nil
	} else if callState.RTPForwarder != nil {
		s.logger.WithField("call_id", callID).Debug("Cleaning up single RTPForwarder")
		callState.RTPForwarder.Stop()
		callState.RTPForwarder.Cleanup()
		if callState.RTPForwarder.RecordingPath != "" {
			recordingPaths = append(recordingPaths, callState.RTPForwarder.RecordingPath)
		}
	}

	callState.RTPForwarder = nil

	// Clean up StreamForwarders (used for multi-stream SIPREC calls)
	if len(callState.StreamForwarders) > 0 {
		// Clear per-stream metadata entries before releasing forwarders
		if s.handler != nil && s.handler.ClearSessionMetadataCallback != nil {
			for streamID := range callState.StreamForwarders {
				streamCallID := fmt.Sprintf("%s_%s", callID, streamID)
				s.handler.ClearSessionMetadataCallback(streamCallID)
			}
		}
		for streamID, forwarder := range callState.StreamForwarders {
			if forwarder == nil {
				continue
			}
			forwarder.Stop()
			forwarder.Cleanup()
			if forwarder.RecordingPath != "" {
				// Check if not already in recordingPaths before adding
				found := false
				for _, path := range recordingPaths {
					if path == forwarder.RecordingPath {
						found = true
						break
					}
				}
				if !found {
					recordingPaths = append(recordingPaths, forwarder.RecordingPath)
				}
			}
			s.logger.WithFields(logrus.Fields{
				"call_id":   callID,
				"stream_id": streamID,
			}).Debug("Cleaned up stream forwarder")
		}
	}
	callState.StreamForwarders = nil

	callState.PendingAckCSeq = 0

	s.combineRecordingLegs(callID, callState, recordingPaths)

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

	// Remove from handler.ActiveCalls for pause/resume API support
	s.removeCallFromActiveList(callID)

	if s.handler != nil {
		s.handler.ClearSTTRouting(callID)
		// Clear session metadata from transcription service to prevent memory leaks
		if s.handler.ClearSessionMetadataCallback != nil && callState.RecordingSession != nil {
			s.handler.ClearSessionMetadataCallback(callState.RecordingSession.ID)
		}
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

func (s *CustomSIPServer) combineRecordingLegs(callID string, callState *CallState, recordingPaths []string) {
	if len(recordingPaths) < 2 {
		return
	}
	if s.handler == nil || s.handler.Config == nil || s.handler.Config.MediaConfig == nil {
		return
	}
	mediaCfg := s.handler.Config.MediaConfig
	if !mediaCfg.CombineLegs {
		return
	}

	outputDir := ""
	if mediaCfg.RecordingDir != "" {
		outputDir = mediaCfg.RecordingDir
	}
	if outputDir == "" {
		outputDir = filepath.Dir(recordingPaths[0])
	}

	baseName := security.SanitizeCallUUID(callID)
	if baseName == "" {
		baseName = fmt.Sprintf("call-%d", time.Now().Unix())
	}
	outputPath := filepath.Join(outputDir, fmt.Sprintf("%s.wav", baseName))

	if err := media.CombineWAVRecordings(outputPath, recordingPaths); err != nil {
		s.logger.WithError(err).WithField("call_id", callID).Warn("Failed to combine SIPREC legs into single recording")
		return
	}

	if callState != nil && callState.RecordingSession != nil {
		if callState.RecordingSession.ExtendedMetadata == nil {
			callState.RecordingSession.ExtendedMetadata = make(map[string]string)
		}
		callState.RecordingSession.ExtendedMetadata["combined_recording_path"] = outputPath
	}

	s.logger.WithFields(logrus.Fields{
		"call_id": callID,
		"path":    outputPath,
		"legs":    len(recordingPaths),
	}).Info("Combined SIPREC legs into single recording")
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

// generateUUID generates a new UUID string for session failover tracking
func generateUUID() string {
	return uuid.New().String()
}

// extractSIPHeadersForAudit extracts SIP headers from a message for audit logging
func (s *CustomSIPServer) extractSIPHeadersForAudit(message *SIPMessage) *audit.SIPHeadersAudit {
	if message == nil {
		return nil
	}

	h := &audit.SIPHeadersAudit{
		Method:        message.Method,
		RequestURI:    message.RequestURI,
		CallID:        message.CallID,
		CSeq:          message.CSeq,
		ContentType:   message.ContentType,
		CustomHeaders: make(map[string]string),
	}

	// Extract headers from message.Headers map
	if message.Headers != nil {
		getFirst := func(key string) string {
			if vals, ok := message.Headers[key]; ok && len(vals) > 0 {
				return vals[0]
			}
			return ""
		}

		h.From = getFirst("From")
		h.To = getFirst("To")
		h.Via = getFirst("Via")
		h.Contact = getFirst("Contact")
		h.Authorization = getFirst("Authorization")
		h.ProxyAuthorization = getFirst("Proxy-Authorization")
		h.Route = getFirst("Route")
		h.RecordRoute = getFirst("Record-Route")
		h.Allow = getFirst("Allow")
		h.Supported = getFirst("Supported")
		h.Require = getFirst("Require")
		h.UserAgent = getFirst("User-Agent")
		h.Server = getFirst("Server")
		h.Accept = getFirst("Accept")

		// Capture vendor-specific headers
		vendorHeaders := []string{
			"X-Session-ID", "Session-ID", "P-Asserted-Identity",
			"P-Preferred-Identity", "Remote-Party-ID", "Diversion",
			"X-UCID", "X-Call-Info", "P-Charging-Vector",
			"X-Recording-Timeout", "X-Recording-Max-Duration",
		}
		for _, hdr := range vendorHeaders {
			if val := getFirst(hdr); val != "" {
				h.CustomHeaders[hdr] = val
			}
		}
	}

	// Add response info if present
	if message.StatusCode > 0 {
		h.StatusCode = message.StatusCode
		h.ReasonPhrase = message.Reason
	}

	// Add transport info
	if message.Connection != nil {
		h.Transport = message.Connection.transport
		h.RemoteAddr = message.Connection.remoteAddr
	}

	return h
}

// extractSiprecContent extracts SDP and rs-metadata from multipart SIPREC body
func (s *CustomSIPServer) extractSiprecContent(body []byte, contentType string) ([]byte, []byte) {
	// Validate multipart body size
	if err := security.ValidateSize(body, security.MaxMultipartSize, "multipart body"); err != nil {
		s.logger.WithError(err).Warn("Multipart body exceeds size limit")
		return nil, nil
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to parse Content-Type for SIPREC body")
		return nil, nil
	}

	if !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		s.logger.WithField("media_type", mediaType).Debug("SIPREC body missing multipart content type")
		return nil, nil
	}

	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		s.logger.Warn("Multipart SIPREC body missing boundary parameter")
		return nil, nil
	}

	mr := multipart.NewReader(bytes.NewReader(body), boundary)

	var sdpData []byte
	var rsMetadata []byte

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.logger.WithError(err).Warn("Failed to iterate multipart SIPREC body")
			break
		}

		ct := part.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		partType, _, _ := mime.ParseMediaType(ct)
		partType = strings.ToLower(partType)

		buf := bytes.NewBuffer(nil)
		if _, err := io.Copy(buf, part); err != nil {
			s.logger.WithError(err).Warn("Failed to read multipart section")
			continue
		}
		data := buf.Bytes()

		switch partType {
		case "application/sdp":
			if err := security.ValidateSize(data, security.MaxSDPSize, "SDP"); err != nil {
				s.logger.WithError(err).Warn("SDP exceeds size limit")
				continue
			}
			sdpData = append([]byte(nil), data...)
			s.logger.WithFields(logrus.Fields{
				"sdp_size": len(sdpData),
			}).Debug("Extracted SDP part from SIPREC multipart")
		case "application/rs-metadata+xml":
			if err := security.ValidateSize(data, security.MaxMetadataSize, "SIPREC metadata"); err != nil {
				s.logger.WithError(err).Warn("SIPREC metadata exceeds size limit")
				continue
			}
			rsMetadata = append([]byte(nil), data...)
			s.logger.WithField("metadata_size", len(rsMetadata)).Debug("Extracted rs-metadata part from SIPREC multipart")
		default:
			s.logger.WithField("content_type", partType).Debug("Ignoring non-SIPREC multipart section")
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

// buildStreamMetadata constructs per-stream metadata for AMQP publishing.
// It resolves the participant associated with a stream label from the SIPREC
// rs-metadata and returns a metadata map suitable for SessionMetadataCallback.
func buildStreamMetadata(streamLabel string, rsMeta *siprec.RSMetadata, session *siprec.RecordingSession) map[string]string {
	meta := map[string]string{
		"stream_label": streamLabel,
	}

	if rsMeta == nil {
		return meta
	}

	participant := rsMeta.ResolveStreamParticipant(streamLabel)
	if participant == nil {
		return meta
	}

	name := participant.DisplayName
	if name == "" {
		name = participant.Name
	}
	if name != "" {
		meta["participant_name"] = name
	}

	if participant.Role != "" {
		meta["participant_role"] = participant.Role
	}

	if len(participant.Aor) > 0 {
		aor := participant.Aor[0].Value
		if aor == "" {
			aor = participant.Aor[0].URI
		}
		if aor != "" {
			meta["participant_aor"] = aor
		}
	}

	return meta
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

	// RFC 7245/7866 Session Recovery Markers
	// Handle FailoverID for session recovery support
	if metadata.SessionRecordingAssoc.FixedID != "" {
		session.FailoverID = metadata.SessionRecordingAssoc.FixedID
		session.ExtendedMetadata["fixed_id"] = metadata.SessionRecordingAssoc.FixedID
		session.ExtendedMetadata["failover_id"] = metadata.SessionRecordingAssoc.FixedID
	} else {
		// Generate a new FailoverID for future recovery support
		session.FailoverID = generateUUID()
		session.ExtendedMetadata["failover_id"] = session.FailoverID
	}

	// Check if this is a session recovery scenario
	isRecovery := strings.EqualFold(session.RecordingState, "recovering") ||
		strings.EqualFold(session.StateReason, "failover") ||
		strings.Contains(strings.ToLower(session.StateReasonRef), "failover")

	if isRecovery {
		logger.WithFields(logrus.Fields{
			"session_id":   session.ID,
			"failover_id":  session.FailoverID,
			"state":        session.RecordingState,
			"state_reason": session.StateReason,
		}).Info("Detected session recovery scenario")

		// Process stream recovery information
		siprec.ProcessStreamRecovery(session, metadata)

		// Mark this session as recovering
		session.ExtendedMetadata["recovery_mode"] = "true"
		session.ExtendedMetadata["recovery_timestamp"] = time.Now().UTC().Format(time.RFC3339)

		// If we have an original session ID reference, mark it
		if metadata.SessionRecordingAssoc.SessionID != "" && metadata.SessionRecordingAssoc.SessionID != session.ID {
			session.ReplacesSessionID = metadata.SessionRecordingAssoc.SessionID
			session.ExtendedMetadata["replaces_session_id"] = session.ReplacesSessionID
			logger.WithFields(logrus.Fields{
				"new_session_id":      session.ID,
				"replaces_session_id": session.ReplacesSessionID,
			}).Info("Session recovery: new session replaces previous session")
		}
	}

	// Process XML extensions from metadata (captures vendor-specific elements like NICE, Cisco, etc.)
	processXMLExtensions(session, metadata, logger)

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

// processXMLExtensions extracts vendor-specific XML extensions from SIPREC metadata
// and stores them in session.ExtendedMetadata. This captures extensions from NICE,
// Cisco, Oracle, Avaya and other vendors that embed custom data in the SIPREC XML.
func processXMLExtensions(session *siprec.RecordingSession, metadata *siprec.RSMetadata, logger *logrus.Entry) {
	if session == nil || metadata == nil {
		return
	}

	if session.ExtendedMetadata == nil {
		session.ExtendedMetadata = make(map[string]string)
	}

	extensionCount := 0

	// Process session element extensions (RSSession)
	for i, sess := range metadata.Sessions {
		for _, ext := range sess.Extensions {
			key := formatExtensionKey(fmt.Sprintf("session_%d", i), ext.XMLName)
			if key != "" && ext.InnerXML != "" {
				session.ExtendedMetadata[key] = strings.TrimSpace(ext.InnerXML)
				extensionCount++

				// Check for NICE-specific extensions
				extractNICEExtensionData(session, ext, logger)
			}
		}
	}

	// Process recording session extensions (RSRecordingSession)
	for i, recSess := range metadata.RecordingSessions {
		for _, ext := range recSess.Extensions {
			key := formatExtensionKey(fmt.Sprintf("recsession_%d", i), ext.XMLName)
			if key != "" && ext.InnerXML != "" {
				session.ExtendedMetadata[key] = strings.TrimSpace(ext.InnerXML)
				extensionCount++

				// Check for NICE-specific extensions
				extractNICEExtensionData(session, ext, logger)
			}
		}
	}

	// Process participant extensions (RSParticipant)
	for i, participant := range metadata.Participants {
		for _, ext := range participant.Extensions {
			key := formatExtensionKey(fmt.Sprintf("participant_%d", i), ext.XMLName)
			if key != "" && ext.InnerXML != "" {
				session.ExtendedMetadata[key] = strings.TrimSpace(ext.InnerXML)
				extensionCount++

				// Check for NICE-specific extensions
				extractNICEExtensionData(session, ext, logger)
			}
		}
	}

	// Process stream extensions
	for i, stream := range metadata.Streams {
		for _, ext := range stream.Extensions {
			key := formatExtensionKey(fmt.Sprintf("stream_%d", i), ext.XMLName)
			if key != "" && ext.InnerXML != "" {
				session.ExtendedMetadata[key] = strings.TrimSpace(ext.InnerXML)
				extensionCount++

				// Check for NICE-specific extensions
				extractNICEExtensionData(session, ext, logger)
			}
		}
	}

	// Process group extensions
	for i, group := range metadata.Group {
		for _, ext := range group.Extensions {
			key := formatExtensionKey(fmt.Sprintf("group_%d", i), ext.XMLName)
			if key != "" && ext.InnerXML != "" {
				session.ExtendedMetadata[key] = strings.TrimSpace(ext.InnerXML)
				extensionCount++

				// Check for NICE-specific extensions
				extractNICEExtensionData(session, ext, logger)
			}
		}
	}

	if extensionCount > 0 {
		logger.WithFields(logrus.Fields{
			"session_id":      session.ID,
			"extension_count": extensionCount,
		}).Debug("Processed XML extensions from SIPREC metadata")
	}
}

// formatExtensionKey creates a normalized key name for an XML extension
func formatExtensionKey(prefix string, xmlName xml.Name) string {
	localName := strings.TrimSpace(xmlName.Local)
	if localName == "" {
		return ""
	}

	// Normalize namespace to a shorter prefix
	nsPrefix := ""
	if xmlName.Space != "" {
		// Extract short name from namespace URI
		ns := strings.ToLower(xmlName.Space)
		switch {
		case strings.Contains(ns, "nice"):
			nsPrefix = "nice_"
		case strings.Contains(ns, "cisco"):
			nsPrefix = "cisco_"
		case strings.Contains(ns, "oracle"):
			nsPrefix = "oracle_"
		case strings.Contains(ns, "avaya"):
			nsPrefix = "avaya_"
		case strings.Contains(ns, "genesys"):
			nsPrefix = "genesys_"
		default:
			// Use last part of namespace as prefix
			parts := strings.Split(ns, "/")
			if len(parts) > 0 {
				lastPart := parts[len(parts)-1]
				if len(lastPart) > 0 && len(lastPart) <= 20 {
					nsPrefix = strings.ToLower(lastPart) + "_"
				}
			}
		}
	}

	return fmt.Sprintf("ext_%s_%s%s", prefix, nsPrefix, strings.ToLower(localName))
}

// extractNICEExtensionData extracts NICE-specific data from XML extensions
func extractNICEExtensionData(session *siprec.RecordingSession, ext siprec.XMLExtension, logger *logrus.Entry) {
	localName := strings.ToLower(ext.XMLName.Local)
	ns := strings.ToLower(ext.XMLName.Space)

	// Check if this is a NICE extension
	isNICE := strings.Contains(ns, "nice") ||
		strings.HasPrefix(localName, "nice") ||
		strings.HasPrefix(localName, "ntr") ||
		strings.HasPrefix(localName, "incontact") ||
		strings.HasPrefix(localName, "cxone")

	if !isNICE {
		return
	}

	innerContent := strings.TrimSpace(ext.InnerXML)
	if innerContent == "" {
		return
	}

	// Store recognized NICE fields directly
	switch {
	case strings.Contains(localName, "interaction") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_interaction_id"] = innerContent
		logger.WithField("nice_interaction_id", innerContent).Debug("Extracted NICE interaction ID from XML extension")

	case strings.Contains(localName, "session") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_session_id"] = innerContent
		logger.WithField("nice_session_id", innerContent).Debug("Extracted NICE session ID from XML extension")

	case strings.Contains(localName, "recording") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_recording_id"] = innerContent
		logger.WithField("nice_recording_id", innerContent).Debug("Extracted NICE recording ID from XML extension")

	case strings.Contains(localName, "contact") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_contact_id"] = innerContent
		logger.WithField("nice_contact_id", innerContent).Debug("Extracted NICE contact ID from XML extension")

	case strings.Contains(localName, "agent") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_agent_id"] = innerContent
		logger.WithField("nice_agent_id", innerContent).Debug("Extracted NICE agent ID from XML extension")

	case strings.Contains(localName, "call") && strings.Contains(localName, "id"):
		session.ExtendedMetadata["nice_call_id"] = innerContent
		logger.WithField("nice_call_id", innerContent).Debug("Extracted NICE call ID from XML extension")

	case strings.Contains(localName, "ucid"):
		session.ExtendedMetadata["nice_ucid"] = innerContent
		logger.WithField("nice_ucid", innerContent).Debug("Extracted NICE UCID from XML extension")
	}
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
func (s *CustomSIPServer) generateSiprecSDP(ip string, rtpPort int) []byte {
	// Generate SDP with proper session info
	timestamp := time.Now().Unix()

	if ip == "" {
		ip = "127.0.0.1"
	}

	// Validate that we have a valid port
	if rtpPort <= 0 {
		s.logger.Error("generateSiprecSDP called without a valid RTP port")
		// Use a fallback port to avoid complete failure, but this is an error condition
		rtpPort = 10000
	}

	sdp := fmt.Sprintf(`v=0
o=- %d %d IN IP4 %s
s=SIPREC Recording Session
c=IN IP4 %s
t=0 0
m=audio %d RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=recvonly
`, timestamp, timestamp, ip, ip, rtpPort)

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
	case "oracle":
		s.extractOracleHeaders(message)
	case "genesys":
		s.extractGenesysHeaders(message)
	case "nice":
		s.extractNICEHeaders(message)
	case "asterisk":
		s.extractAsteriskHeaders(message)
	case "freeswitch":
		s.extractFreeSWITCHHeaders(message)
	case "opensips":
		s.extractOpenSIPSHeaders(message)
	default:
		s.extractGenericHeaders(message)
	}

	// Always extract UUI and X-headers regardless of vendor
	s.extractUUIAndXHeaders(message)
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

	// Detect Oracle SBC systems
	if strings.Contains(userAgent, "oracle") ||
		strings.Contains(userAgent, "acme packet") ||
		strings.Contains(userAgent, "ocsbc") ||
		strings.Contains(userAgent, "esbc") {
		return "oracle"
	}

	// Detect Genesys systems (Cloud, PureConnect, Engage, GVP)
	if strings.Contains(userAgent, "genesys") ||
		strings.Contains(userAgent, "pureconnect") ||
		strings.Contains(userAgent, "purecloud") ||
		strings.Contains(userAgent, "pureengage") ||
		strings.Contains(userAgent, "gvp") ||
		strings.Contains(userAgent, "interaction") ||
		strings.Contains(userAgent, "inin") {
		return "genesys"
	}

	// Detect NICE systems (NICE Engage, NICE inContact, NICE CXone, NTR)
	if strings.Contains(userAgent, "nice") ||
		strings.Contains(userAgent, "ntr") ||
		strings.Contains(userAgent, "incontact") ||
		strings.Contains(userAgent, "cxone") ||
		strings.Contains(userAgent, "engage recording") ||
		strings.Contains(userAgent, "nexidia") ||
		strings.Contains(userAgent, "actimize") {
		return "nice"
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

	// Oracle SBC header detection fallback
	if s.getHeaderValue(message, "x-ocsbc-ucid") != "" ||
		s.getHeaderValue(message, "x-ocsbc-conversation-id") != "" ||
		s.getHeaderValue(message, "x-oracle-ucid") != "" ||
		s.getHeaderValue(message, "x-oracle-conversation-id") != "" ||
		s.getHeaderValue(message, "p-ocsbc-ucid") != "" {
		return "oracle"
	}

	// Genesys header detection fallback
	if s.getHeaderValue(message, "x-genesys-interaction-id") != "" ||
		s.getHeaderValue(message, "x-genesys-conversation-id") != "" ||
		s.getHeaderValue(message, "x-genesys-session-id") != "" ||
		s.getHeaderValue(message, "x-interaction-id") != "" ||
		s.getHeaderValue(message, "x-inin-interaction-id") != "" ||
		s.getHeaderValue(message, "x-inin-ic-userid") != "" {
		return "genesys"
	}

	// NICE header detection fallback
	if s.getHeaderValue(message, "x-nice-interaction-id") != "" ||
		s.getHeaderValue(message, "x-nice-session-id") != "" ||
		s.getHeaderValue(message, "x-nice-call-id") != "" ||
		s.getHeaderValue(message, "x-nice-recording-id") != "" ||
		s.getHeaderValue(message, "x-ntr-session-id") != "" ||
		s.getHeaderValue(message, "x-incontact-contact-id") != "" ||
		s.getHeaderValue(message, "x-cxone-contact-id") != "" {
		return "nice"
	}

	// Detect Asterisk PBX
	if strings.Contains(userAgent, "asterisk") ||
		strings.Contains(userAgent, "ast_") ||
		strings.Contains(userAgent, "chan_sip") ||
		strings.Contains(userAgent, "chan_pjsip") ||
		strings.Contains(userAgent, "pjsip/asterisk") ||
		strings.Contains(userAgent, "fpbx") {
		return "asterisk"
	}

	// Detect FreeSWITCH
	if strings.Contains(userAgent, "freeswitch") ||
		strings.Contains(userAgent, "freeswich") ||
		strings.Contains(userAgent, "sofia-sip") ||
		strings.Contains(userAgent, "mod_sofia") {
		return "freeswitch"
	}

	// Detect OpenSIPS
	if strings.Contains(userAgent, "opensips") ||
		strings.Contains(userAgent, "openser") ||
		strings.Contains(userAgent, "kamailio") { // Kamailio is a fork, often similar behavior
		return "opensips"
	}

	// Asterisk header detection fallback
	if s.getHeaderValue(message, "x-asterisk-hangupcause") != "" ||
		s.getHeaderValue(message, "x-asterisk-hangupcausecode") != "" ||
		s.getHeaderValue(message, "x-asterisk-unique-id") != "" ||
		s.getHeaderValue(message, "x-asterisk-linkedid") != "" {
		return "asterisk"
	}

	// FreeSWITCH header detection fallback
	if s.getHeaderValue(message, "x-fs-unique-id") != "" ||
		s.getHeaderValue(message, "x-fs-uuid") != "" ||
		s.getHeaderValue(message, "x-freeswitch-uuid") != "" ||
		s.getHeaderValue(message, "x-freeswitch-core-uuid") != "" ||
		s.getHeaderValue(message, "x-fs-hostname") != "" {
		return "freeswitch"
	}

	// OpenSIPS header detection fallback
	if s.getHeaderValue(message, "x-opensips-dialog-id") != "" ||
		s.getHeaderValue(message, "x-opensips-transaction-id") != "" ||
		s.getHeaderValue(message, "x-opensips-did") != "" {
		return "opensips"
	}

	return "generic"
}

// extractAvayaHeaders extracts Avaya-specific SIP headers
// Supports Avaya Aura, Communication Manager, Session Manager, AES
func (s *CustomSIPServer) extractAvayaHeaders(message *SIPMessage) {
	// Avaya-specific headers to extract
	avayaHeaders := []string{
		// Primary Avaya identifiers
		"x-avaya-conf-id",
		"x-avaya-station-id",
		"x-avaya-ucid",
		"x-avaya-trunk-group",
		"x-avaya-user-id",
		"x-avaya-agent-id",
		"x-avaya-skill-group",
		// Communication Manager (CM) headers
		"x-avaya-cm-call-id",
		"x-avaya-cm-originator",
		"x-avaya-cm-destination",
		"x-avaya-cm-location",
		// Session Manager (SM) headers
		"x-avaya-sm-session-id",
		"x-avaya-sm-entity",
		"x-avaya-sm-domain",
		// Contact center headers
		"x-avaya-vdn",
		"x-avaya-workgroup",
		"x-avaya-interaction-id",
		"x-avaya-call-priority",
		"x-avaya-queue-name",
		"x-avaya-queue-time",
		"x-avaya-hold-time",
		"x-avaya-talk-time",
		// ACD/Elite headers
		"x-avaya-acd-number",
		"x-avaya-split",
		"x-avaya-skill",
		"x-avaya-vector",
		// B2BUA and call flow
		"x-avaya-b2bua-call-id",
		"x-avaya-originating-trunk",
		"x-avaya-terminating-trunk",
		"x-avaya-translated-number",
		"x-avaya-original-called",
		"x-avaya-redirecting-number",
		// Standard SIP headers used by Avaya
		"p-asserted-identity",
		"diversion",
		"remote-party-id",
		"p-called-party-id",
		"p-calling-party-id",
		"history-info",
	}

	for _, header := range avayaHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Handle Universal Call ID (UCID) variations
	ucidHeaders := []string{
		"x-avaya-ucid",
		"x-ucid",
		"x-avaya-conf-id",
		"x-avaya-cm-call-id",
		"x-avaya-interaction-id",
	}
	for _, header := range ucidHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.UCIDHeaders = append(message.UCIDHeaders, value)
		}
	}

	// Populate dedicated Avaya fields for structured access
	if value := s.getHeaderValue(message, "x-avaya-ucid"); value != "" {
		message.AvayaUCID = value
	} else if value := s.getHeaderValue(message, "x-ucid"); value != "" {
		message.AvayaUCID = value
	}

	if value := s.getHeaderValue(message, "x-avaya-conf-id"); value != "" {
		message.AvayaConfID = value
	}

	if value := s.getHeaderValue(message, "x-avaya-station-id"); value != "" {
		message.AvayaStationID = value
	}

	if value := s.getHeaderValue(message, "x-avaya-agent-id"); value != "" {
		message.AvayaAgentID = value
	}

	if value := s.getHeaderValue(message, "x-avaya-vdn"); value != "" {
		message.AvayaVDN = value
	}

	if value := s.getHeaderValue(message, "x-avaya-skill-group"); value != "" {
		message.AvayaSkillGroup = value
	} else if value := s.getHeaderValue(message, "x-avaya-skill"); value != "" {
		message.AvayaSkillGroup = value
	}
}

// extractCiscoHeaders extracts Cisco-specific SIP headers
// Supports CUCM (Unified Communications Manager), CUBE, Webex Calling, SRST
func (s *CustomSIPServer) extractCiscoHeaders(message *SIPMessage) {
	// Cisco-specific headers to extract
	ciscoHeaders := []string{
		// Primary identifiers
		"session-id",
		"cisco-guid",
		"x-cisco-call-id",
		"x-cisco-gcid",
		// CUCM cluster/node info
		"x-cisco-cluster-id",
		"x-cisco-node-id",
		"x-cisco-cluster-fqdn",
		// Device and location
		"x-cisco-device-name",
		"x-cisco-device-type",
		"x-cisco-device-mac",
		"x-cisco-device-id",
		"x-cisco-location",
		"x-cisco-location-id",
		"x-cisco-region",
		"x-cisco-region-tag",
		"x-cisco-site-id",
		// Trunk and routing
		"x-cisco-trunk-license",
		"x-cisco-trunk-id",
		"x-cisco-dial-peer",
		"x-cisco-route-pattern",
		"x-cisco-route-list",
		"x-cisco-translation-pattern",
		// Media and codec
		"x-cisco-media-profile",
		"x-cisco-codec",
		"x-cisco-media-region",
		// SRST (Survivable Remote Site Telephony)
		"x-cisco-srst-mode",
		"x-cisco-srst-id",
		// Webex/Cloud calling
		"x-cisco-webex-tracking-id",
		"x-cisco-spark-tracking-id",
		"x-cisco-tenant-id",
		"x-cisco-org-id",
		// Contact center (UCCX/UCCE)
		"x-cisco-queue-id",
		"x-cisco-skill-group",
		"x-cisco-agent-id",
		"x-cisco-agent-extension",
		// Call recording/compliance
		"x-cisco-recording-mode",
		"x-cisco-recording-server",
		// Standard SIP headers used by Cisco
		"remote-party-id",
		"p-called-party-id",
		"p-calling-party-id",
		"p-asserted-identity",
		"diversion",
		"history-info",
	}

	for _, header := range ciscoHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Handle Cisco Session-ID header specifically (primary correlation ID)
	if sessionID := s.getHeaderValue(message, "session-id"); sessionID != "" {
		message.SessionIDHeader = sessionID
		message.UCIDHeaders = append(message.UCIDHeaders, sessionID)
	}

	// Also check for GUID as correlation ID
	if guid := s.getHeaderValue(message, "cisco-guid"); guid != "" {
		message.UCIDHeaders = append(message.UCIDHeaders, guid)
	}
}

// extractOracleHeaders extracts Oracle SBC-specific SIP headers
// Supports Oracle Communications SBC (OCSBC), Enterprise SBC (ESBC), WebRTC Session Controller
func (s *CustomSIPServer) extractOracleHeaders(message *SIPMessage) {
	// Oracle SBC-specific headers to extract
	// OCSBC = Oracle Communications Session Border Controller
	oracleHeaders := []string{
		// Primary identifiers
		"x-ocsbc-ucid",
		"x-ocsbc-conversation-id",
		"x-oracle-ucid",
		"x-oracle-conversation-id",
		"p-ocsbc-ucid",
		"p-ocsbc-conversation-id",
		"x-ocsbc-session-id",
		"x-ocsbc-call-id",
		// Realm/routing info
		"x-ocsbc-egress-realm",
		"x-ocsbc-ingress-realm",
		"x-ocsbc-egress-network-interface",
		"x-ocsbc-ingress-network-interface",
		// SBC instance and HA
		"x-ocsbc-sbc-instance-id",
		"x-ocsbc-geo-redundancy-id",
		"x-ocsbc-primary-sbc",
		"x-ocsbc-secondary-sbc",
		// Media handling
		"x-ocsbc-media-ip",
		"x-ocsbc-media-encryption",
		"x-ocsbc-signaling-encryption",
		"x-ocsbc-srtp-profile",
		// IMS/VoLTE headers
		"p-charging-vector",
		"p-charging-function-addresses",
		"p-access-network-info",
		"p-visited-network-id",
		"p-served-user",
		// Standard SIP identity headers
		"p-asserted-identity",
		"p-preferred-identity",
		"remote-party-id",
		"p-called-party-id",
		"p-calling-party-id",
		"diversion",
		"history-info",
		// Oracle-specific routing
		"x-ocsbc-route",
		"x-ocsbc-local-policy",
		"x-ocsbc-session-agent",
		"x-ocsbc-translation-id",
		// Recording/compliance
		"x-ocsbc-recording-mode",
		"x-ocsbc-recording-session",
		// Billing
		"x-ocsbc-billing-id",
		"x-ocsbc-account-code",
	}

	for _, header := range oracleHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Handle Oracle UCID - check multiple header variations
	ucidHeaders := []string{
		"x-ocsbc-ucid",
		"x-oracle-ucid",
		"p-ocsbc-ucid",
		"x-ucid",
	}
	for _, header := range ucidHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.OracleUCID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id": message.CallID,
				"ucid":    value,
				"header":  header,
			}).Debug("Extracted Oracle UCID")
			break
		}
	}

	// Handle Oracle Conversation ID - for call correlation
	conversationHeaders := []string{
		"x-ocsbc-conversation-id",
		"x-oracle-conversation-id",
		"p-ocsbc-conversation-id",
		"x-conversation-id",
	}
	for _, header := range conversationHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.OracleConversationID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":         message.CallID,
				"conversation_id": value,
				"header":          header,
			}).Debug("Extracted Oracle Conversation ID")
			break
		}
	}

	// Extract fields from P-Charging-Vector if present (IMS Charging ID)
	// Format: icid-value=xxx;icid-generated-at=yyy;orig-ioi=zzz;term-ioi=aaa
	if pChargingVector := s.getHeaderValue(message, "p-charging-vector"); pChargingVector != "" {
		pcvFields := parsePChargingVector(pChargingVector)
		for key, value := range pcvFields {
			message.VendorHeaders["pcv_"+key] = value
		}
		// Use ICID as fallback UCID if not already set
		if icid, ok := pcvFields["icid-value"]; ok && icid != "" {
			message.VendorHeaders["icid-value"] = icid
			if message.OracleUCID == "" {
				message.OracleUCID = icid
				message.UCIDHeaders = append(message.UCIDHeaders, icid)
			}
		}
	}
}

// parsePChargingVector parses all fields from P-Charging-Vector header
// Returns a map of field name to value
// Format: icid-value=xxx;icid-generated-at=yyy;orig-ioi=zzz;term-ioi=aaa
func parsePChargingVector(pcv string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(pcv, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx > 0 {
			key := strings.ToLower(strings.TrimSpace(part[:idx]))
			value := strings.TrimSpace(part[idx+1:])
			// Remove surrounding quotes if present
			value = strings.Trim(value, "\"")
			result[key] = value
		}
	}
	return result
}

// extractGenesysHeaders extracts Genesys-specific SIP headers
// Supports Genesys Cloud, PureConnect, PureEngage, and GVP platforms
func (s *CustomSIPServer) extractGenesysHeaders(message *SIPMessage) {
	// Genesys-specific headers to extract
	genesysHeaders := []string{
		// Primary identifiers
		"x-genesys-interaction-id",
		"x-genesys-conversation-id",
		"x-genesys-session-id",
		"x-genesys-call-uuid",
		"x-interaction-id",
		// ININ (Interactive Intelligence) legacy headers
		"x-inin-interaction-id",
		"x-inin-ic-userid",
		"x-inin-ic-target",
		"x-inin-ic-workgroup",
		"x-inin-ic-station",
		// Contact center metadata
		"x-genesys-queue",
		"x-genesys-queue-name",
		"x-genesys-agent-id",
		"x-genesys-agent-name",
		"x-genesys-tenant-id",
		"x-genesys-org-id",
		// Campaign and routing
		"x-genesys-campaign-id",
		"x-genesys-campaign-name",
		"x-genesys-contact-id",
		"x-genesys-contact-list-id",
		"x-genesys-skill-group",
		"x-genesys-routing-target",
		// Call metadata
		"x-genesys-call-type",
		"x-genesys-call-direction",
		"x-genesys-ani",
		"x-genesys-dnis",
		"x-genesys-customer-id",
		// GVP (Genesys Voice Platform) headers
		"x-gvp-session-id",
		"x-gvp-tenant-id",
		"x-gvp-application",
		// Billing and business data
		"x-genesys-billing-code",
		"x-genesys-cost-center",
		"x-genesys-business-unit",
		"x-genesys-account-code",
		// Standard SIP headers also used by Genesys
		"p-asserted-identity",
		"remote-party-id",
		"p-called-party-id",
		"p-calling-party-id",
		"diversion",
	}

	for _, header := range genesysHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Extract primary Genesys Interaction ID (main correlation identifier)
	interactionHeaders := []string{
		"x-genesys-interaction-id",
		"x-interaction-id",
		"x-inin-interaction-id",
		"x-genesys-call-uuid",
	}
	for _, header := range interactionHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.GenesysInteractionID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id":        message.CallID,
				"interaction_id": value,
				"header":         header,
			}).Debug("Extracted Genesys Interaction ID")
			break
		}
	}

	// Extract Genesys Conversation ID
	conversationHeaders := []string{
		"x-genesys-conversation-id",
		"x-conversation-id",
	}
	for _, header := range conversationHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.GenesysConversationID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":         message.CallID,
				"conversation_id": value,
				"header":          header,
			}).Debug("Extracted Genesys Conversation ID")
			break
		}
	}

	// Extract Genesys Session ID
	sessionHeaders := []string{
		"x-genesys-session-id",
		"x-gvp-session-id",
	}
	for _, header := range sessionHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.GenesysSessionID = value
			break
		}
	}

	// Extract Queue Name
	queueHeaders := []string{
		"x-genesys-queue-name",
		"x-genesys-queue",
		"x-inin-ic-workgroup",
	}
	for _, header := range queueHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.GenesysQueueName = value
			break
		}
	}

	// Extract Agent ID
	agentHeaders := []string{
		"x-genesys-agent-id",
		"x-inin-ic-userid",
	}
	for _, header := range agentHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.GenesysAgentID = value
			break
		}
	}

	// Extract Campaign ID (for outbound)
	if campaignID := s.getHeaderValue(message, "x-genesys-campaign-id"); campaignID != "" {
		message.GenesysCampaignID = campaignID
	}
}

// extractNICEHeaders extracts NICE-specific SIP headers
// Supports NICE Engage, NICE inContact, NICE CXone, and NTR systems
func (s *CustomSIPServer) extractNICEHeaders(message *SIPMessage) {
	// NICE-specific headers to extract
	niceHeaders := []string{
		"x-nice-interaction-id",
		"x-nice-session-id",
		"x-nice-recording-id",
		"x-nice-call-id",
		"x-ntr-session-id",
		"x-ntr-call-id",
		"x-incontact-contact-id",
		"x-incontact-master-contact-id",
		"x-cxone-contact-id",
		"x-cxone-master-contact-id",
		"x-nice-agent-id",
		"x-nice-agent-station",
		"x-engage-call-id",
		"x-engage-recording-id",
	}

	for _, header := range niceHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			if message.VendorHeaders == nil {
				message.VendorHeaders = make(map[string]string)
			}
			message.VendorHeaders[header] = value
		}
	}

	// Extract primary NICE Interaction ID
	interactionHeaders := []string{
		"x-nice-interaction-id",
		"x-incontact-contact-id",
		"x-cxone-contact-id",
		"x-engage-call-id",
	}
	for _, header := range interactionHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICEInteractionID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id":        message.CallID,
				"interaction_id": value,
				"header":         header,
			}).Debug("Extracted NICE Interaction ID")
			break
		}
	}

	// Extract NICE Session ID
	sessionHeaders := []string{
		"x-nice-session-id",
		"x-ntr-session-id",
	}
	for _, header := range sessionHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICESessionID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":    message.CallID,
				"session_id": value,
				"header":     header,
			}).Debug("Extracted NICE Session ID")
			break
		}
	}

	// Extract NICE Recording ID
	recordingHeaders := []string{
		"x-nice-recording-id",
		"x-engage-recording-id",
	}
	for _, header := range recordingHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICERecordingID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":      message.CallID,
				"recording_id": value,
				"header":       header,
			}).Debug("Extracted NICE Recording ID")
			break
		}
	}

	// Extract NICE Call ID
	callIDHeaders := []string{
		"x-nice-call-id",
		"x-ntr-call-id",
	}
	for _, header := range callIDHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICECallID = value
			break
		}
	}

	// Extract Contact ID (CXone/inContact)
	contactHeaders := []string{
		"x-incontact-contact-id",
		"x-cxone-contact-id",
		"x-incontact-master-contact-id",
		"x-cxone-master-contact-id",
	}
	for _, header := range contactHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICEContactID = value
			break
		}
	}

	// Extract Agent ID
	agentHeaders := []string{
		"x-nice-agent-id",
		"x-incontact-agent-id",
		"x-cxone-agent-id",
	}
	for _, header := range agentHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.NICEAgentID = value
			break
		}
	}
}

// extractAsteriskHeaders extracts Asterisk-specific SIP headers
// Supports Asterisk PBX, FreePBX, and chan_sip/chan_pjsip
func (s *CustomSIPServer) extractAsteriskHeaders(message *SIPMessage) {
	// Asterisk-specific headers to extract
	asteriskHeaders := []string{
		// Primary Asterisk identifiers
		"x-asterisk-unique-id",
		"x-asterisk-uniqueid",
		"x-asterisk-linkedid",
		"x-asterisk-linked-id",
		"x-asterisk-channel",
		"x-asterisk-channel-name",
		// Call context and routing
		"x-asterisk-context",
		"x-asterisk-extension",
		"x-asterisk-priority",
		"x-asterisk-application",
		// Account and billing
		"x-asterisk-accountcode",
		"x-asterisk-account-code",
		"x-asterisk-cdr-accountcode",
		// Hangup and disposition
		"x-asterisk-hangupcause",
		"x-asterisk-hangupcausecode",
		"x-asterisk-hangup-cause",
		"x-asterisk-disposition",
		// Queue and agent
		"x-asterisk-queue",
		"x-asterisk-queuename",
		"x-asterisk-agent",
		"x-asterisk-agentname",
		"x-asterisk-member",
		// Caller information
		"x-asterisk-callerid",
		"x-asterisk-callerid-num",
		"x-asterisk-callerid-name",
		"x-asterisk-callingpres",
		"x-asterisk-callingani2",
		// DNID and original number
		"x-asterisk-dnid",
		"x-asterisk-rdnis",
		"x-asterisk-exten",
		// Bridge and transfer info
		"x-asterisk-bridge-id",
		"x-asterisk-bridgeid",
		"x-asterisk-transfer-context",
		"x-asterisk-transfer-exten",
		// AMI/ARI correlation
		"x-asterisk-event-id",
		"x-asterisk-ami-action-id",
		"x-ari-session-id",
		// chan_pjsip specific
		"x-pjsip-endpoint",
		"x-pjsip-transport",
		"x-pjsip-contact",
		// FreePBX specific
		"x-fpbx-did",
		"x-fpbx-extension",
		"x-fpbx-ringgroup",
		"x-fpbx-ivr",
		"x-fpbx-queue",
		"x-fpbx-callid",
	}

	for _, header := range asteriskHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Extract Asterisk Unique ID (primary correlation identifier)
	uniqueIDHeaders := []string{
		"x-asterisk-unique-id",
		"x-asterisk-uniqueid",
	}
	for _, header := range uniqueIDHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.AsteriskUniqueID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id":   message.CallID,
				"unique_id": value,
				"header":    header,
			}).Debug("Extracted Asterisk Unique ID")
			break
		}
	}

	// Extract Asterisk Linked ID (for bridged/transferred calls)
	linkedIDHeaders := []string{
		"x-asterisk-linkedid",
		"x-asterisk-linked-id",
	}
	for _, header := range linkedIDHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.AsteriskLinkedID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":   message.CallID,
				"linked_id": value,
			}).Debug("Extracted Asterisk Linked ID")
			break
		}
	}

	// Extract Channel Name
	channelHeaders := []string{
		"x-asterisk-channel",
		"x-asterisk-channel-name",
	}
	for _, header := range channelHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.AsteriskChannelID = value
			break
		}
	}

	// Extract Account Code
	accountHeaders := []string{
		"x-asterisk-accountcode",
		"x-asterisk-account-code",
		"x-asterisk-cdr-accountcode",
	}
	for _, header := range accountHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.AsteriskAccountCode = value
			break
		}
	}

	// Extract Context
	if context := s.getHeaderValue(message, "x-asterisk-context"); context != "" {
		message.AsteriskContext = context
	}
}

// extractFreeSWITCHHeaders extracts FreeSWITCH-specific SIP headers
// Supports FreeSWITCH, mod_sofia, and related systems
func (s *CustomSIPServer) extractFreeSWITCHHeaders(message *SIPMessage) {
	// FreeSWITCH-specific headers to extract
	freeswwitchHeaders := []string{
		// Primary FreeSWITCH identifiers
		"x-fs-uuid",
		"x-fs-unique-id",
		"x-freeswitch-uuid",
		"x-fs-call-uuid",
		"x-fs-core-uuid",
		"x-freeswitch-core-uuid",
		// Channel information
		"x-fs-channel-name",
		"x-freeswitch-channel-name",
		"x-fs-channel-uuid",
		// Profile and context
		"x-fs-profile-name",
		"x-freeswitch-profile-name",
		"x-fs-sofia-profile",
		"x-fs-context",
		"x-freeswitch-context",
		"x-fs-dialplan",
		// Caller/callee information
		"x-fs-caller-id-number",
		"x-fs-caller-id-name",
		"x-fs-destination-number",
		"x-fs-called-party-number",
		"x-fs-calling-party-number",
		// Network information
		"x-fs-network-ip",
		"x-fs-network-port",
		"x-fs-hostname",
		"x-freeswitch-hostname",
		// Account and billing
		"x-fs-accountcode",
		"x-fs-account-code",
		"x-freeswitch-accountcode",
		"x-fs-billing-code",
		// Hangup and disposition
		"x-fs-hangup-cause",
		"x-freeswitch-hangup-cause",
		"x-fs-hangup-cause-q850",
		"x-fs-disposition",
		// Bridge and transfer
		"x-fs-bridge-uuid",
		"x-fs-other-leg-uuid",
		"x-fs-other-leg-channel-uuid",
		"x-fs-transfer-source",
		"x-fs-transfer-destination",
		// Recording info
		"x-fs-record-file-path",
		"x-fs-record-uuid",
		"x-fs-recording-uuid",
		// Call center / callcenter
		"x-fs-cc-queue",
		"x-fs-cc-agent",
		"x-fs-cc-member-uuid",
		"x-fs-cc-action",
		// Originate info
		"x-fs-originate-uuid",
		"x-fs-originated-from-uuid",
		// Custom variables
		"x-fs-sip-auth-username",
		"x-fs-sip-from-user",
		"x-fs-sip-to-user",
		// mod_sofia specific
		"x-sofia-sip-url",
		"x-sofia-profile",
		// Direction and state
		"x-fs-call-direction",
		"x-fs-call-state",
		"x-fs-session-state",
	}

	for _, header := range freeswwitchHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Extract FreeSWITCH UUID (primary correlation identifier)
	uuidHeaders := []string{
		"x-fs-uuid",
		"x-fs-unique-id",
		"x-freeswitch-uuid",
		"x-fs-call-uuid",
	}
	for _, header := range uuidHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.FreeSWITCHUUID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id": message.CallID,
				"fs_uuid": value,
				"header":  header,
			}).Debug("Extracted FreeSWITCH UUID")
			break
		}
	}

	// Extract FreeSWITCH Core UUID
	coreUUIDHeaders := []string{
		"x-fs-core-uuid",
		"x-freeswitch-core-uuid",
	}
	for _, header := range coreUUIDHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.FreeSWITCHCoreUUID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":   message.CallID,
				"core_uuid": value,
			}).Debug("Extracted FreeSWITCH Core UUID")
			break
		}
	}

	// Extract Channel Name
	channelHeaders := []string{
		"x-fs-channel-name",
		"x-freeswitch-channel-name",
	}
	for _, header := range channelHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.FreeSWITCHChannelName = value
			break
		}
	}

	// Extract Profile Name
	profileHeaders := []string{
		"x-fs-profile-name",
		"x-freeswitch-profile-name",
		"x-fs-sofia-profile",
	}
	for _, header := range profileHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.FreeSWITCHProfileName = value
			break
		}
	}

	// Extract Account Code
	accountHeaders := []string{
		"x-fs-accountcode",
		"x-fs-account-code",
		"x-freeswitch-accountcode",
	}
	for _, header := range accountHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.FreeSWITCHAccountCode = value
			break
		}
	}
}

// extractOpenSIPSHeaders extracts OpenSIPS-specific SIP headers
// Supports OpenSIPS, Kamailio, and similar SIP proxies
func (s *CustomSIPServer) extractOpenSIPSHeaders(message *SIPMessage) {
	// OpenSIPS-specific headers to extract
	opensipsHeaders := []string{
		// Primary OpenSIPS identifiers
		"x-opensips-dialog-id",
		"x-opensips-did",
		"x-opensips-transaction-id",
		"x-opensips-tid",
		"x-opensips-call-id",
		// Load balancer info
		"x-opensips-lb-dst",
		"x-opensips-lb-group",
		"x-opensips-lb-resource",
		// Dispatcher info
		"x-opensips-dispatcher-dst",
		"x-opensips-dispatcher-setid",
		"x-opensips-dispatcher-group",
		// Routing information
		"x-opensips-route",
		"x-opensips-request-uri",
		"x-opensips-next-hop",
		"x-opensips-domain",
		// User agent info
		"x-opensips-registered-aor",
		"x-opensips-contact",
		"x-opensips-socket",
		// Dialog info
		"x-opensips-dlg-callid",
		"x-opensips-dlg-from-tag",
		"x-opensips-dlg-to-tag",
		"x-opensips-dlg-state",
		"x-opensips-dlg-hash",
		// Accounting
		"x-opensips-acc-timestamp",
		"x-opensips-acc-method",
		"x-opensips-acc-duration",
		// Topology hiding
		"x-opensips-th-callid",
		"x-opensips-th-from-tag",
		// Media proxy / RTPproxy / RTPengine
		"x-opensips-rtpproxy-id",
		"x-opensips-rtpengine-id",
		"x-opensips-media-relay",
		// Fraud detection
		"x-opensips-fraud-profile",
		"x-opensips-fraud-score",
		// Kamailio specific (often compatible)
		"x-kamailio-dialog-id",
		"x-kamailio-transaction-id",
		"x-kamailio-route",
		"x-kamailio-server",
		// Custom OpenSIPS AVPs often passed as headers
		"x-opensips-custom-avp1",
		"x-opensips-custom-avp2",
		"x-opensips-account",
		"x-opensips-src-ip",
		"x-opensips-src-port",
		// Branch info
		"x-opensips-branch",
		"x-opensips-branch-id",
		// Cluster info
		"x-opensips-cluster-id",
		"x-opensips-node-id",
	}

	for _, header := range opensipsHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.VendorHeaders[header] = value
		}
	}

	// Extract OpenSIPS Dialog ID (primary correlation identifier)
	dialogHeaders := []string{
		"x-opensips-dialog-id",
		"x-opensips-did",
		"x-kamailio-dialog-id",
	}
	for _, header := range dialogHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.OpenSIPSDialogID = value
			message.UCIDHeaders = append(message.UCIDHeaders, value)
			s.logger.WithFields(logrus.Fields{
				"call_id":   message.CallID,
				"dialog_id": value,
				"header":    header,
			}).Debug("Extracted OpenSIPS Dialog ID")
			break
		}
	}

	// Extract Transaction ID
	transactionHeaders := []string{
		"x-opensips-transaction-id",
		"x-opensips-tid",
		"x-kamailio-transaction-id",
	}
	for _, header := range transactionHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.OpenSIPSTransactionID = value
			s.logger.WithFields(logrus.Fields{
				"call_id":        message.CallID,
				"transaction_id": value,
			}).Debug("Extracted OpenSIPS Transaction ID")
			break
		}
	}

	// Extract OpenSIPS Call-ID correlation (may differ from SIP Call-ID after topology hiding)
	callIDHeaders := []string{
		"x-opensips-call-id",
		"x-opensips-th-callid",
	}
	for _, header := range callIDHeaders {
		if value := s.getHeaderValue(message, header); value != "" {
			message.OpenSIPSCallID = value
			break
		}
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

// extractUUIAndXHeaders extracts User-to-User Information (RFC 7433) and all X-headers
func (s *CustomSIPServer) extractUUIAndXHeaders(message *SIPMessage) {
	if message.XHeaders == nil {
		message.XHeaders = make(map[string]string)
	}

	// Extract User-to-User Information header (RFC 7433)
	// The UUI header can appear in multiple forms
	uuiVariants := []string{
		"User-to-User",
		"user-to-user",
		"X-User-to-User",
		"x-user-to-user",
	}
	for _, header := range uuiVariants {
		if value := s.getHeaderValue(message, header); value != "" {
			message.UUIHeader = value
			message.XHeaders["User-to-User"] = value
			s.logger.WithFields(logrus.Fields{
				"call_id": message.CallID,
				"uui":     value,
			}).Debug("Extracted UUI header")
			break
		}
	}

	// Extract all X-headers dynamically from the Headers map
	if message.Headers != nil {
		for headerName, values := range message.Headers {
			// Check if this is an X-header (case-insensitive)
			lowerName := strings.ToLower(headerName)
			if strings.HasPrefix(lowerName, "x-") && len(values) > 0 {
				// Normalize header name for storage (preserve original case from first character)
				normalizedName := headerName
				if strings.HasPrefix(headerName, "x-") {
					normalizedName = "X-" + headerName[2:]
				}

				// Store the first value (or join multiple values)
				if len(values) == 1 {
					message.XHeaders[normalizedName] = values[0]
				} else {
					message.XHeaders[normalizedName] = strings.Join(values, ", ")
				}
			}
		}
	}

	// Also check for X-headers using the raw request if available
	if message.Request != nil {
		// Common X-headers used in enterprise telephony
		enterpriseXHeaders := []string{
			"X-Call-ID",
			"X-Original-Call-ID",
			"X-Correlation-ID",
			"X-Transaction-ID",
			"X-Customer-ID",
			"X-Account-ID",
			"X-Agent-ID",
			"X-Queue-ID",
			"X-Campaign-ID",
			"X-Recording-ID",
			"X-Session-ID",
			"X-Tenant-ID",
			"X-Application-ID",
			"X-Custom-Data",
			"X-Metadata",
			"X-Call-Type",
			"X-Call-Direction",
			"X-ANI",
			"X-DNIS",
			"X-Called-Number",
			"X-Calling-Number",
			"X-Redirect-Number",
			"X-Original-Called-Number",
			"X-Diversion-Reason",
		}

		for _, header := range enterpriseXHeaders {
			if _, exists := message.XHeaders[header]; !exists {
				if value := s.getHeaderValue(message, header); value != "" {
					message.XHeaders[header] = value
				}
			}
		}
	}

	// Log extracted X-headers count for debugging
	if len(message.XHeaders) > 0 {
		s.logger.WithFields(logrus.Fields{
			"call_id":        message.CallID,
			"x_header_count": len(message.XHeaders),
		}).Debug("Extracted X-headers from SIP message")
	}
}

// storeUUIAndXHeadersInSession stores UUI and X-headers in the recording session's ExtendedMetadata
func (s *CustomSIPServer) storeUUIAndXHeadersInSession(session *siprec.RecordingSession, message *SIPMessage) {
	if session == nil || message == nil {
		return
	}

	// Ensure ExtendedMetadata map exists
	if session.ExtendedMetadata == nil {
		session.ExtendedMetadata = make(map[string]string)
	}

	// Store UUI header if present
	if message.UUIHeader != "" {
		session.ExtendedMetadata["sip_uui"] = message.UUIHeader
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"uui":        message.UUIHeader,
		}).Debug("Stored UUI in recording session metadata")
	}

	// Store all X-headers with "sip_" prefix to avoid collisions
	if len(message.XHeaders) > 0 {
		for name, value := range message.XHeaders {
			// Normalize the key: convert "X-Custom-Header" to "sip_x_custom_header"
			normalizedKey := "sip_" + strings.ToLower(strings.ReplaceAll(name, "-", "_"))
			session.ExtendedMetadata[normalizedKey] = value
		}
		s.logger.WithFields(logrus.Fields{
			"session_id":     session.ID,
			"x_header_count": len(message.XHeaders),
		}).Debug("Stored X-headers in recording session metadata")
	}

	// Also store vendor headers in metadata
	if len(message.VendorHeaders) > 0 {
		for name, value := range message.VendorHeaders {
			normalizedKey := "sip_" + strings.ToLower(strings.ReplaceAll(name, "-", "_"))
			// Don't overwrite if already set by X-headers
			if _, exists := session.ExtendedMetadata[normalizedKey]; !exists {
				session.ExtendedMetadata[normalizedKey] = value
			}
		}
	}

	// Store vendor type for reference
	if message.VendorType != "" {
		session.ExtendedMetadata["sip_vendor_type"] = message.VendorType
	}

	// Store UCID headers if present (Avaya)
	if len(message.UCIDHeaders) > 0 {
		session.ExtendedMetadata["sip_ucid"] = strings.Join(message.UCIDHeaders, ";")
	}

	// Store Cisco Session-ID if present
	if message.SessionIDHeader != "" {
		session.ExtendedMetadata["sip_cisco_session_id"] = message.SessionIDHeader
	}

	// Store Oracle SBC UCID if present
	if message.OracleUCID != "" {
		session.ExtendedMetadata["sip_oracle_ucid"] = message.OracleUCID
		s.logger.WithFields(logrus.Fields{
			"session_id":  session.ID,
			"oracle_ucid": message.OracleUCID,
		}).Debug("Stored Oracle UCID in recording session metadata")
	}

	// Store Oracle SBC Conversation ID if present
	if message.OracleConversationID != "" {
		session.ExtendedMetadata["sip_oracle_conversation_id"] = message.OracleConversationID
		s.logger.WithFields(logrus.Fields{
			"session_id":      session.ID,
			"conversation_id": message.OracleConversationID,
		}).Debug("Stored Oracle Conversation ID in recording session metadata")
	}

	// Store Genesys-specific metadata if present
	if message.GenesysInteractionID != "" {
		session.ExtendedMetadata["sip_genesys_interaction_id"] = message.GenesysInteractionID
		s.logger.WithFields(logrus.Fields{
			"session_id":     session.ID,
			"interaction_id": message.GenesysInteractionID,
		}).Debug("Stored Genesys Interaction ID in recording session metadata")
	}

	if message.GenesysConversationID != "" {
		session.ExtendedMetadata["sip_genesys_conversation_id"] = message.GenesysConversationID
		s.logger.WithFields(logrus.Fields{
			"session_id":      session.ID,
			"conversation_id": message.GenesysConversationID,
		}).Debug("Stored Genesys Conversation ID in recording session metadata")
	}

	if message.GenesysSessionID != "" {
		session.ExtendedMetadata["sip_genesys_session_id"] = message.GenesysSessionID
	}

	if message.GenesysQueueName != "" {
		session.ExtendedMetadata["sip_genesys_queue_name"] = message.GenesysQueueName
	}

	if message.GenesysAgentID != "" {
		session.ExtendedMetadata["sip_genesys_agent_id"] = message.GenesysAgentID
	}

	if message.GenesysCampaignID != "" {
		session.ExtendedMetadata["sip_genesys_campaign_id"] = message.GenesysCampaignID
	}

	// Store NICE-specific metadata if present
	if message.NICEInteractionID != "" {
		session.ExtendedMetadata["sip_nice_interaction_id"] = message.NICEInteractionID
		s.logger.WithFields(logrus.Fields{
			"session_id":     session.ID,
			"interaction_id": message.NICEInteractionID,
		}).Debug("Stored NICE Interaction ID in recording session metadata")
	}

	if message.NICESessionID != "" {
		session.ExtendedMetadata["sip_nice_session_id"] = message.NICESessionID
		s.logger.WithFields(logrus.Fields{
			"session_id":      session.ID,
			"nice_session_id": message.NICESessionID,
		}).Debug("Stored NICE Session ID in recording session metadata")
	}

	if message.NICERecordingID != "" {
		session.ExtendedMetadata["sip_nice_recording_id"] = message.NICERecordingID
		s.logger.WithFields(logrus.Fields{
			"session_id":   session.ID,
			"recording_id": message.NICERecordingID,
		}).Debug("Stored NICE Recording ID in recording session metadata")
	}

	if message.NICECallID != "" {
		session.ExtendedMetadata["sip_nice_call_id"] = message.NICECallID
	}

	if message.NICEContactID != "" {
		session.ExtendedMetadata["sip_nice_contact_id"] = message.NICEContactID
	}

	if message.NICEAgentID != "" {
		session.ExtendedMetadata["sip_nice_agent_id"] = message.NICEAgentID
	}

	// Store Asterisk-specific metadata if present
	if message.AsteriskUniqueID != "" {
		session.ExtendedMetadata["sip_asterisk_unique_id"] = message.AsteriskUniqueID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"unique_id":  message.AsteriskUniqueID,
		}).Debug("Stored Asterisk Unique ID in recording session metadata")
	}

	if message.AsteriskLinkedID != "" {
		session.ExtendedMetadata["sip_asterisk_linked_id"] = message.AsteriskLinkedID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"linked_id":  message.AsteriskLinkedID,
		}).Debug("Stored Asterisk Linked ID in recording session metadata")
	}

	if message.AsteriskChannelID != "" {
		session.ExtendedMetadata["sip_asterisk_channel_id"] = message.AsteriskChannelID
	}

	if message.AsteriskAccountCode != "" {
		session.ExtendedMetadata["sip_asterisk_account_code"] = message.AsteriskAccountCode
	}

	if message.AsteriskContext != "" {
		session.ExtendedMetadata["sip_asterisk_context"] = message.AsteriskContext
	}

	// Store FreeSWITCH-specific metadata if present
	if message.FreeSWITCHUUID != "" {
		session.ExtendedMetadata["sip_freeswitch_uuid"] = message.FreeSWITCHUUID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"fs_uuid":    message.FreeSWITCHUUID,
		}).Debug("Stored FreeSWITCH UUID in recording session metadata")
	}

	if message.FreeSWITCHCoreUUID != "" {
		session.ExtendedMetadata["sip_freeswitch_core_uuid"] = message.FreeSWITCHCoreUUID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"core_uuid":  message.FreeSWITCHCoreUUID,
		}).Debug("Stored FreeSWITCH Core UUID in recording session metadata")
	}

	if message.FreeSWITCHChannelName != "" {
		session.ExtendedMetadata["sip_freeswitch_channel_name"] = message.FreeSWITCHChannelName
	}

	if message.FreeSWITCHProfileName != "" {
		session.ExtendedMetadata["sip_freeswitch_profile_name"] = message.FreeSWITCHProfileName
	}

	if message.FreeSWITCHAccountCode != "" {
		session.ExtendedMetadata["sip_freeswitch_account_code"] = message.FreeSWITCHAccountCode
	}

	// Store OpenSIPS-specific metadata if present
	if message.OpenSIPSDialogID != "" {
		session.ExtendedMetadata["sip_opensips_dialog_id"] = message.OpenSIPSDialogID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"dialog_id":  message.OpenSIPSDialogID,
		}).Debug("Stored OpenSIPS Dialog ID in recording session metadata")
	}

	if message.OpenSIPSTransactionID != "" {
		session.ExtendedMetadata["sip_opensips_transaction_id"] = message.OpenSIPSTransactionID
		s.logger.WithFields(logrus.Fields{
			"session_id":     session.ID,
			"transaction_id": message.OpenSIPSTransactionID,
		}).Debug("Stored OpenSIPS Transaction ID in recording session metadata")
	}

	if message.OpenSIPSCallID != "" {
		session.ExtendedMetadata["sip_opensips_call_id"] = message.OpenSIPSCallID
	}

	// Store Avaya-specific metadata if present
	if message.AvayaUCID != "" {
		session.ExtendedMetadata["sip_avaya_ucid"] = message.AvayaUCID
		s.logger.WithFields(logrus.Fields{
			"session_id": session.ID,
			"avaya_ucid": message.AvayaUCID,
		}).Debug("Stored Avaya UCID in recording session metadata")
	}

	if message.AvayaConfID != "" {
		session.ExtendedMetadata["sip_avaya_conf_id"] = message.AvayaConfID
	}

	if message.AvayaStationID != "" {
		session.ExtendedMetadata["sip_avaya_station_id"] = message.AvayaStationID
	}

	if message.AvayaAgentID != "" {
		session.ExtendedMetadata["sip_avaya_agent_id"] = message.AvayaAgentID
	}

	if message.AvayaVDN != "" {
		session.ExtendedMetadata["sip_avaya_vdn"] = message.AvayaVDN
	}

	if message.AvayaSkillGroup != "" {
		session.ExtendedMetadata["sip_avaya_skill_group"] = message.AvayaSkillGroup
	}
}

// syncCallToActiveList syncs a CallState to handler.ActiveCalls for pause/resume API support
func (s *CustomSIPServer) syncCallToActiveList(callID string, callState *CallState) {
	if s.handler == nil || s.handler.ActiveCalls == nil {
		return
	}

	// Create CallData from CallState
	callData := &CallData{
		Forwarder:        callState.RTPForwarder,
		RecordingSession: callState.RecordingSession,
		LastActivity:     callState.LastActivity,
		TraceScope:       callState.TraceScope,
	}

	// If there's dialog info in CallState, create DialogInfo
	if callState.CallID != "" {
		callData.DialogInfo = &DialogInfo{
			CallID:    callState.CallID,
			LocalTag:  callState.LocalTag,
			RemoteTag: callState.RemoteTag,
		}
	}

	// Use the session ID (recording session ID) or call ID as the key
	sessionID := callID
	if callState.RecordingSession != nil && callState.RecordingSession.ID != "" {
		sessionID = callState.RecordingSession.ID
	}

	s.handler.ActiveCalls.Store(sessionID, callData)
	s.logger.WithFields(logrus.Fields{
		"call_id":    callID,
		"session_id": sessionID,
	}).Debug("Synced call to ActiveCalls for pause/resume API")
}

// removeCallFromActiveList removes a call from handler.ActiveCalls
func (s *CustomSIPServer) removeCallFromActiveList(callID string) {
	if s.handler == nil || s.handler.ActiveCalls == nil {
		return
	}

	// Try to remove by call ID
	s.handler.ActiveCalls.Delete(callID)

	s.logger.WithField("call_id", callID).Debug("Removed call from ActiveCalls")
}
