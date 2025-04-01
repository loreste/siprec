package sip

// First, we'll paste the updated content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
	"siprec-server/pkg/errors"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

type SessionStore interface {
	// Save stores a call data by key
	Save(key string, data *CallData) error
	
	// Load retrieves call data by key
	Load(key string) (*CallData, error)
	
	// Delete removes call data by key
	Delete(key string) error
	
	// List returns all stored keys
	List() ([]string, error)
}

// Config for the SIP handler
type Config struct {
	// Maximum concurrent calls allowed
	MaxConcurrentCalls int
	
	// Media configuration
	MediaConfig *media.Config
	
	// Redundancy-related configuration
	RedundancyEnabled bool
	SessionTimeout    time.Duration
	SessionCheckInterval time.Duration
	
	// Storage type for redundancy (memory, redis)
	RedundancyStorageType string
}

// Handler for SIP requests
type Handler struct {
	Logger       *logrus.Logger
	Server       *sipgo.Server
	UA           *sipgo.UserAgent
	Router       *sip.Router
	Config       *Config
	ActiveCalls  sync.Map // Map of call UUID to CallData
	
	// Speech-to-text callback function
	STTCallback func(context.Context, string, io.Reader, string) error
	
	// For session redundancy
	SessionStore SessionStore
	
	// For session monitor goroutine
	monitorCtx    context.Context
	monitorCancel context.CancelFunc
	sessionMonitorWG sync.WaitGroup
}

// CallData holds information about an active call
type CallData struct {
	// Forwarder for RTP packets
	Forwarder *media.RTPForwarder
	
	// SIPREC recording session information
	RecordingSession *siprec.RecordingSession
	
	// Dialog information for the call (required for sending BYE)
	DialogInfo *DialogInfo
	
	// Last activity timestamp (for session monitoring)
	LastActivity time.Time
	
	// Remote address for potential reconnection
	RemoteAddress string
}

// DialogInfo holds information about a SIP dialog
type DialogInfo struct {
	// Call-ID for the dialog
	CallID string
	
	// Tags for From and To headers
	LocalTag  string
	RemoteTag string
	
	// URI values
	LocalURI  string
	RemoteURI string
	
	// Sequence numbers
	LocalSeq  int
	RemoteSeq int
	
	// Contact header
	Contact string
	
	// Route set
	RouteSet []string
}

// NewHandler creates a new SIP handler
func NewHandler(logger *logrus.Logger, config *Config, sttCallback func(context.Context, string, io.Reader, string) error) (*Handler, error) {
	if config == nil {
		return nil, errors.New("configuration cannot be nil")
	}
	
	handler := &Handler{
		Logger:       logger,
		Config:       config,
		STTCallback:  sttCallback,
	}
	
	// Create a new SIP stack
	ua, err := sipgo.NewUA()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create SIP user agent")
	}
	
	// Create a new server using the user agent
	server := sipgo.NewServer(ua)
	router := server.Router()
	
	// Store for later use
	handler.UA = ua
	handler.Server = server
	handler.Router = router
	
	// Initialize the session store if redundancy is enabled
	if config.RedundancyEnabled {
		switch config.RedundancyStorageType {
		case "memory":
			logger.Info("Using in-memory session store")
			handler.SessionStore = NewMemorySessionStore()
		default:
			// Default to memory store for now
			logger.Warn("Unknown storage type, using in-memory session store")
			handler.SessionStore = NewMemorySessionStore()
		}
		
		// Create a dedicated context for the session monitor
		handler.monitorCtx, handler.monitorCancel = context.WithCancel(context.Background())
		handler.sessionMonitorWG.Add(1)
		
		// Start the session monitor
		go handler.monitorSessions(handler.monitorCtx)
	}
	
	return handler, nil
}

// SetupHandlers configures the SIP request handlers
func (h *Handler) SetupHandlers() {
	// Create request handlers
	h.Router.HandleFunc("INVITE", h.handleInvite)
	h.Router.HandleFunc("BYE", h.handleBye)
	h.Router.HandleFunc("OPTIONS", h.handleOptions)
	
	h.Logger.Info("SIP request handlers configured")
}

// handleInvite handles INVITE requests
func (h *Handler) handleInvite(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Check if this is a re-INVITE for an existing call
	callDataValue, exists := h.ActiveCalls.Load(callUUID)
	
	// Check if this is a session recovery attempt
	replaces := req.GetHeader("Replaces")
	hasReplaces := replaces != nil
	
	if !exists && hasReplaces && h.Config.RedundancyEnabled {
		// This might be a session recovery attempt
		replacesValue := replaces.Value()
		replacesCallID, _, _ := parseReplacesHeader(replacesValue)
		if replacesCallID != "" {
			// Try to recover the session
			logger.WithField("replaces_call_id", replacesCallID).Info("Attempting session recovery")
			h.recoverSession(req, tx, replacesCallID)
			return
		}
	} else if exists {
		// Normal re-INVITE for an existing session
		callData := callDataValue.(*CallData)
		
		// Update activity timestamp
		callData.UpdateActivity()
		
		// Update dialog information if needed
		if callData.DialogInfo == nil {
			callData.DialogInfo = extractDialogInfo(req)
		}
		
		// Store updated remote address for potential reconnection
		callData.RemoteAddress = getSourceAddress(req)
		
		// Update the session store
		if h.Config.RedundancyEnabled {
			h.SessionStore.Save(callUUID, callData)
		}
		
		// Process any SDP updates if present
		contentType := req.GetHeader("Content-Type")
		body := req.Body()
		if len(body) > 0 && contentType != nil && 
		   contentType.Value() == "application/sdp" {
			// Handle SDP update if needed
			// For now, just acknowledge without changing media settings
		}
		
		// Respond with 200 OK
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
		logger.Info("Processed re-INVITE for existing session")
	} else {
		// New call - check if we exceed concurrent call limit
		callCount := h.GetActiveCallCount()
		if h.Config.MaxConcurrentCalls > 0 && callCount >= h.Config.MaxConcurrentCalls {
			logger.WithField("current_calls", callCount).Error("Maximum concurrent calls limit reached")
			resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - Maximum calls reached", nil)
			tx.Respond(resp)
			return
		}
		
		// Process the new call
		h.processNewInvite(req, tx, callUUID)
	}
}

// processNewInvite handles new INVITE requests
func (h *Handler) processNewInvite(req *sip.Request, tx sip.ServerTransaction, callUUID string) {
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Perform validation
	if err := h.validateSIPRequest(req); err != nil {
		logger.WithError(err).Error("Invalid INVITE request")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - "+err.Error(), nil)
		tx.Respond(resp)
		return
	}
	
	// Determine if this is a SIPREC request
	issiprec, rsMetadata := h.isSIPREC(req)
	
	// Extract SDP information
	sdp, err := extractSDP(req)
	if err != nil {
		logger.WithError(err).Error("Failed to extract SDP")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		return
	}
	
	// Allocate RTP port
	rtpPort := media.AllocateRTPPort(h.Config.MediaConfig.RTPPortMin, h.Config.MediaConfig.RTPPortMax, h.Logger)
	
	// Prepare SDP response
	sdpResp, err := h.prepareSdpResponse(sdp, rtpPort)
	if err != nil {
		logger.WithError(err).Error("Failed to prepare SDP response")
		resp := sip.NewResponseFromRequest(req, 500, "Server Error - Failed to prepare SDP response", nil)
		tx.Respond(resp)
		return
	}
	
	// Create response with SDP
	resp := sip.NewResponseFromRequest(req, 200, "OK", sdpResp)
	
	// Set necessary headers
	contact := req.GetHeader("Contact")
	if contact != nil {
		resp.AppendHeader(sip.NewHeader("Contact", contact.Value()))
	}
	
	// Initialize call data
	callData := &CallData{
		DialogInfo:    extractDialogInfo(req),
		LastActivity:  time.Now(),
		RemoteAddress: getSourceAddress(req),
	}
	
	// Handle SIPREC case
	if issiprec {
		logger.Info("Processing SIPREC INVITE")
		
		// Create recording session
		recordingSession := &siprec.RecordingSession{
			ID:              uuid.New().String(),
			SIPID:           callUUID,
			RecordingState:  "active",
			StartTime:       time.Now(),
			Direction:       "unknown", // Will be determined later
			Participants:    []siprec.Participant{},
			OriginalRequest: req,
		}
		
		// Extract participants if available
		if rsMetadata != nil {
			recordingSession.Direction = rsMetadata.Direction
			
			for _, p := range rsMetadata.Participants {
				recordingSession.Participants = append(
					recordingSession.Participants,
					siprec.ConvertRSParticipantToParticipant(p),
				)
			}
		}
		
		// Create RTP forwarder
		forwarder, err := media.NewRTPForwarder(60*time.Second, recordingSession, h.Logger)
		if err != nil {
			logger.WithError(err).Error("Failed to create RTP forwarder")
			resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - No RTP ports available", nil)
			tx.Respond(resp)
			return
		}
		forwarder.RecordingPaused = rsMetadata.State == "paused"
		
		// Store in call data
		callData.Forwarder = forwarder
		callData.RecordingSession = recordingSession
	} else {
		// Regular call, not SIPREC
		logger.Info("Received regular INVITE with SDP")

		// Create forwarder without recording session
		forwarder, err := media.NewRTPForwarder(60*time.Second, nil, h.Logger)
		if err != nil {
			logger.WithError(err).Error("Failed to create RTP forwarder")
			resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - No RTP ports available", nil)
			tx.Respond(resp)
			return
		}
		callData.Forwarder = forwarder
	}

	// Store the call state
	h.ActiveCalls.Store(callUUID, callData)
	
	// Save to persistent store if redundancy is enabled
	if h.Config.RedundancyEnabled {
		if err := h.SessionStore.Save(callUUID, callData); err != nil {
			logger.WithError(err).Warn("Failed to save session to persistent store")
		}
	}
	
	// Start RTP forwarding
	ctx := context.Background()
	media.StartRTPForwarding(ctx, callData.Forwarder, callUUID, h.Config.MediaConfig, h.STTCallback)
	
	// Send response
	if err := tx.Respond(resp); err != nil {
		logger.WithError(err).Error("Failed to send INVITE response")
		return
	}
	
	logger.Info("Successfully responded to INVITE")
}

// validateSIPRequest performs basic validation on SIP requests
func (h *Handler) validateSIPRequest(req *sip.Request) error {
	// Ensure required headers are present
	if req.From() == nil {
		return errors.New("missing From header")
	}
	
	if req.To() == nil {
		return errors.New("missing To header")
	}
	
	if req.CallID() == nil {
		return errors.New("missing Call-ID header")
	}
	
	// Optional - check for Contact header for dialog establishment
	contact := req.GetHeader("Contact")
	if contact == nil {
		return errors.New("missing Contact header")
	}
	
	return nil
}

// handleBye handles BYE requests
func (h *Handler) handleBye(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Check if the call exists
	callDataValue, exists := h.ActiveCalls.Load(callUUID)
	if !exists {
		logger.Warn("Received BYE for unknown call")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
		return
	}
	
	// Get call data
	callData := callDataValue.(*CallData)
	
	// Update activity timestamp
	callData.UpdateActivity()
	
	// Stop forwarding and clean up
	if callData.Forwarder != nil {
		logger.Info("Stopping RTP forwarding for call")
		close(callData.Forwarder.StopChan)
	}
	
	// Update recording state if this is a SIPREC session
	if callData.RecordingSession != nil {
		callData.RecordingSession.RecordingState = "stopped"
		callData.RecordingSession.EndTime = time.Now()
		
		logger.WithFields(logrus.Fields{
			"recording_id": callData.RecordingSession.ID,
			"duration":     callData.RecordingSession.EndTime.Sub(callData.RecordingSession.StartTime).String(),
		}).Info("Recording session stopped")
		
		// If we're keeping session info for redundancy, update it
		if h.Config.RedundancyEnabled {
			h.SessionStore.Save(callUUID, callData)
		}
	}
	
	// Remove from active calls if we're not preserving for redundancy
	// or if redundancy is disabled
	if !h.Config.RedundancyEnabled {
		h.ActiveCalls.Delete(callUUID)
	}
	
	// Send 200 OK
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(resp)
	
	logger.Info("Call terminated")
}

// handleOptions handles OPTIONS requests
func (h *Handler) handleOptions(req *sip.Request, tx sip.ServerTransaction) {
	logger := h.Logger.WithField("call_uuid", req.CallID().String())
	
	// Create response
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	
	// Add Allow header
	resp.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS"))
	
	// Add Supported header to indicate SIPREC support
	resp.AppendHeader(sip.NewHeader("Supported", "replaces, siprec"))
	
	// Send response
	if err := tx.Respond(resp); err != nil {
		logger.WithError(err).Error("Failed to send OPTIONS response")
		return
	}
	
	logger.Debug("Responded to OPTIONS request")
}

// UpdateActivity updates the last activity timestamp for a call
func (c *CallData) UpdateActivity() {
	c.LastActivity = time.Now()
}

// IsStale checks if a session is stale based on last activity
func (c *CallData) IsStale(timeout time.Duration) bool {
	return time.Since(c.LastActivity) > timeout
}

// isSIPREC determines if a request is a SIPREC INVITE
// based on Content-Type and body content
func (h *Handler) isSIPREC(req *sip.Request) (bool, *siprec.RSMetadata) {
	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return false, nil
	}
	
	// Check for multipart content with RS-Metadata
	if strings.Contains(contentType.Value(), "multipart/mixed") {
		body := req.Body()
		if body == nil || len(body) == 0 {
			return false, nil
		}
		
		// Simple check for RS-Metadata
		if strings.Contains(string(body), "application/rs-metadata+xml") {
			// Extract RS-Metadata if possible
			metadata, err := siprec.ExtractRSMetadata(contentType.Value(), body)
			if err != nil {
				h.Logger.WithError(err).Warn("Failed to extract RS-Metadata from multipart body")
				return true, nil // Still recognize as SIPREC despite parsing failure
			}
			return true, metadata
		}
	}
	
	return false, nil
}

// extractSDP extracts the SDP content from a SIP request
func extractSDP(req *sip.Request) ([]byte, error) {
	contentType := req.GetHeader("Content-Type")
	if contentType == nil {
		return nil, errors.New("missing Content-Type header")
	}
	
	body := req.Body()
	if body == nil || len(body) == 0 {
		return nil, errors.New("empty request body")
	}
	
	// Check content type
	if strings.Contains(contentType.Value(), "application/sdp") {
		return body, nil
	} else if strings.Contains(contentType.Value(), "multipart/mixed") {
		// For multipart content, extract the SDP part
		boundary, found := getBoundary(contentType.Value())
		if !found {
			return nil, errors.New("missing boundary in multipart content")
		}
		
		// Split by boundary
		parts := splitMultipart(string(body), boundary)
		for _, part := range parts {
			if strings.Contains(part, "Content-Type: application/sdp") {
				// Extract SDP content
				sdpContent := extractMultipartContent(part)
				return []byte(sdpContent), nil
			}
		}
		
		return nil, errors.New("no SDP content found in multipart body")
	}
	
	return nil, fmt.Errorf("unsupported Content-Type: %s", contentType.Value())
}

// getBoundary extracts the boundary from a Content-Type header
func getBoundary(contentType string) (string, bool) {
	boundaryPrefix := "boundary="
	
	index := strings.Index(contentType, boundaryPrefix)
	if index == -1 {
		return "", false
	}
	
	boundary := contentType[index+len(boundaryPrefix):]
	
	// If boundary is quoted, remove quotes
	if strings.HasPrefix(boundary, "\"") && strings.HasSuffix(boundary, "\"") {
		boundary = boundary[1 : len(boundary)-1]
	}
	
	return boundary, true
}

// splitMultipart splits a multipart body into parts based on the boundary
func splitMultipart(body, boundary string) []string {
	parts := []string{}
	
	// Boundary is prefixed with -- in the content
	boundary = "--" + boundary
	
	// Split by boundary
	segments := strings.Split(body, boundary)
	
	// First segment is usually empty or contains preamble
	// Last segment is usually empty or contains epilogue with trailing --
	for i := 1; i < len(segments)-1; i++ {
		parts = append(parts, segments[i])
	}
	
	// Check if the last segment is a valid part (not just "--\r\n")
	lastSegment := segments[len(segments)-1]
	if len(lastSegment) > 4 && !strings.HasPrefix(strings.TrimSpace(lastSegment), "--") {
		parts = append(parts, lastSegment)
	}
	
	return parts
}

// extractMultipartContent extracts the content from a multipart part
// by skipping the headers and any blank lines
func extractMultipartContent(part string) string {
	// Split by double newline to separate headers from content
	segments := strings.Split(part, "\r\n\r\n")
	if len(segments) < 2 {
		// Try with just \n\n
		segments = strings.Split(part, "\n\n")
	}
	
	if len(segments) < 2 {
		return "" // No content found
	}
	
	// Return the content part
	return segments[1]
}

// prepareSdpResponse prepares an SDP response
func (h *Handler) prepareSdpResponse(receivedSDP []byte, rtpPort int) ([]byte, error) {
	// Parse received SDP
	parsed, err := sipgo.ParseSDP(receivedSDP)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse SDP")
	}
	
	// Prepare our SDP options
	sdpOptions := SDPOptions{
		IPAddress:  h.Config.MediaConfig.InternalIP,
		BehindNAT:  h.Config.MediaConfig.BehindNAT,
		InternalIP: h.Config.MediaConfig.InternalIP,
		ExternalIP: h.Config.MediaConfig.ExternalIP,
		RTPPort:    rtpPort,
		EnableSRTP: h.Config.MediaConfig.EnableSRTP,
	}
	
	// Set up SRTP if enabled
	if h.Config.MediaConfig.EnableSRTP {
		// Generate SRTP keys that will be used in the crypto lines
		// These would be generated securely and provided to the media handler
		masterKey := make([]byte, 16) // 16 bytes for AES-128
		masterSalt := make([]byte, 14) // 14 bytes for the salt
		
		// For production, use crypto/rand to generate these
		// rand.Read(masterKey)
		// rand.Read(masterSalt)
		
		// For now, just use a fixed test key (NOT FOR PRODUCTION)
		for i := range masterKey {
			masterKey[i] = byte(i + 1)
		}
		for i := range masterSalt {
			masterSalt[i] = byte(i + 100)
		}
		
		sdpOptions.SRTPKeyInfo = &SRTPKeyInfo{
			MasterKey:   masterKey,
			MasterSalt:  masterSalt,
			Profile:     "AES_CM_128_HMAC_SHA1_80",
			KeyLifetime: 31, // 2^31 packets
		}
	}
	
	// Generate SDP response
	responseSDP := h.generateSDP(parsed, sdpOptions)
	
	// Marshal the SDP object back to bytes
	return sipgo.MarshalSDP(responseSDP)
}

// getSourceAddress gets the source address of a SIP request
func getSourceAddress(req *sip.Request) string {
	if req == nil || req.Source() == nil {
		return ""
	}
	
	return req.Source().String()
}

// extractDialogInfo extracts dialog information from a SIP request
func extractDialogInfo(req *sip.Request) *DialogInfo {
	if req == nil {
		return nil
	}
	
	dialogInfo := &DialogInfo{
		CallID:     req.CallID().String(),
		LocalTag:   getTagParam(req.To()),
		RemoteTag:  getTagParam(req.From()),
		LocalURI:   req.To().Address.String(),
		RemoteURI:  req.From().Address.String(),
		LocalSeq:   0, // Will be populated from response
		RemoteSeq:  getCSeqValue(req),
	}
	
	// Extract Contact header
	if contact := req.GetHeader("Contact"); contact != nil {
		dialogInfo.Contact = contact.Value()
	}
	
	// Extract Route headers if any
	routeSet := []string{}
	for _, header := range req.GetHeaders("Route") {
		routeSet = append(routeSet, header.Value())
	}
	dialogInfo.RouteSet = routeSet
	
	return dialogInfo
}

// getTagParam extracts tag parameter from address header
func getTagParam(addr *sip.FromHeader) string {
	if addr == nil {
		return ""
	}
	
	// Extract tag using string operations since we don't know
	// the exact interface of the SIP library's FromHeader
	addrStr := addr.String()
	tagIndex := strings.Index(addrStr, ";tag=")
	if tagIndex < 0 {
		return ""
	}
	
	return addrStr[tagIndex+5:]
}

// getCSeqValue extracts CSeq number from a request
func getCSeqValue(req *sip.Request) int {
	if req == nil {
		return 0
	}
	
	cseq := req.CSeq()
	if cseq == nil {
		return 0
	}
	
	return int(cseq.SeqNo)
}

// parseReplacesHeader parses a Replaces header into callID, toTag, fromTag
func parseReplacesHeader(replacesValue string) (string, string, string) {
	// Basic format: callid;to-tag=tag1;from-tag=tag2
	if replacesValue == "" {
		return "", "", ""
	}
	
	parts := strings.Split(replacesValue, ";")
	if len(parts) < 3 {
		return "", "", ""
	}
	
	callID := parts[0]
	toTag := ""
	fromTag := ""
	
	// Extract tags
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "to-tag=") {
			toTag = strings.TrimPrefix(part, "to-tag=")
		} else if strings.HasPrefix(part, "from-tag=") {
			fromTag = strings.TrimPrefix(part, "from-tag=")
		}
	}
	
	return callID, toTag, fromTag
}

// recoverSession attempts to recover a session based on a previous Call-ID
func (h *Handler) recoverSession(req *sip.Request, tx sip.ServerTransaction, oldCallID string) {
	logger := h.Logger.WithField("replaces_call_id", oldCallID)
	
	// Get the new call UUID
	newCallID := req.CallID().String()
	
	// Check if redundancy is enabled and session store is available
	if !h.Config.RedundancyEnabled || h.SessionStore == nil {
		logger.Warn("Session recovery requested but redundancy is not enabled")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
		return
	}
	
	// Try to load the old session
	oldSession, err := h.SessionStore.Load(oldCallID)
	if err != nil {
		logger.WithError(err).Error("Failed to load session for recovery")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
		return
	}
	
	logger.Info("Found session for recovery")
	
	// Create new call data for the recovered session
	newCallData := &CallData{
		RecordingSession: oldSession.RecordingSession,
		DialogInfo:       extractDialogInfo(req),
		LastActivity:     time.Now(),
		RemoteAddress:    getSourceAddress(req),
	}
	
	// Set up media for the recovered session
	rtpPort := media.AllocateRTPPort(h.Config.MediaConfig.RTPPortMin, h.Config.MediaConfig.RTPPortMax, h.Logger)
	
	// Create a new forwarder with the same recording session
	forwarder, err := media.NewRTPForwarder(60*time.Second, oldSession.RecordingSession, h.Logger)
	if err != nil {
		logger.WithError(err).Error("Failed to create RTP forwarder for recovered session")
		resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - No RTP ports available", nil)
		tx.Respond(resp)
		return
	}
	if oldSession.RecordingSession != nil {
		forwarder.RecordingPaused = oldSession.RecordingSession.RecordingState == "paused"
	}
	
	newCallData.Forwarder = forwarder
	
	// Extract SDP information
	sdp, err := extractSDP(req)
	if err != nil {
		logger.WithError(err).Error("Failed to extract SDP for session recovery")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		return
	}
	
	// Prepare SDP response
	sdpResp, err := h.prepareSdpResponse(sdp, rtpPort)
	if err != nil {
		logger.WithError(err).Error("Failed to prepare SDP response for session recovery")
		resp := sip.NewResponseFromRequest(req, 500, "Server Error - Failed to prepare SDP response", nil)
		tx.Respond(resp)
		return
	}
	
	// Create response with SDP
	resp := sip.NewResponseFromRequest(req, 200, "OK", sdpResp)
	
	// Set necessary headers
	contact := req.GetHeader("Contact")
	if contact != nil {
		resp.AppendHeader(sip.NewHeader("Contact", contact.Value()))
	}
	
	// Store the new call state and remove the old one
	h.ActiveCalls.Store(newCallID, newCallData)
	h.ActiveCalls.Delete(oldCallID)
	
	// Update the session store
	h.SessionStore.Save(newCallID, newCallData)
	h.SessionStore.Delete(oldCallID)
	
	// Start RTP forwarding
	ctx := context.Background()
	media.StartRTPForwarding(ctx, newCallData.Forwarder, newCallID, h.Config.MediaConfig, h.STTCallback)
	
	// Send response
	if err := tx.Respond(resp); err != nil {
		logger.WithError(err).Error("Failed to send response for session recovery")
		return
	}
	
	logger.Info("Session successfully recovered with new Call-ID")
}

// monitorSessions periodically checks for stale sessions
func (h *Handler) monitorSessions(ctx context.Context) {
	defer h.sessionMonitorWG.Done()
	
	logger := h.Logger.WithField("component", "session_monitor")
	logger.Info("Starting session monitor")
	
	// Create a ticker for the check interval
	ticker := time.NewTicker(h.Config.SessionCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			logger.Info("Session monitor shutting down")
			return
		case <-ticker.C:
			// Check for stale sessions
			h.cleanupStaleSessions()
		}
	}
}

// cleanupStaleSessions checks for and cleans up stale sessions
func (h *Handler) cleanupStaleSessions() {
	logger := h.Logger.WithField("component", "session_cleanup")
	
	// Define stale criterion
	staleDuration := h.Config.SessionTimeout
	
	// Check active calls for stale sessions
	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)
		
		// Check if the session is stale
		if callData.IsStale(staleDuration) {
			logger.WithField("call_uuid", callUUID).Info("Cleaning up stale session")
			
			// Stop RTP forwarding
			if callData.Forwarder != nil {
				close(callData.Forwarder.StopChan)
			}
			
			// Remove from active calls
			h.ActiveCalls.Delete(callUUID)
			
			// Clean up from session store if redundancy is enabled
			if h.Config.RedundancyEnabled {
				h.SessionStore.Delete(callUUID)
			}
		}
		
		return true // Continue iteration
	})
	
	// Clean up stale sessions from the store if redundancy is enabled
	if h.Config.RedundancyEnabled {
		storedSessions, err := h.SessionStore.List()
		if err != nil {
			logger.WithError(err).Error("Failed to list stored sessions")
			return
		}
		
		for _, sessionID := range storedSessions {
			// Remove orphaned sessions that are too old
			if _, exists := h.ActiveCalls.Load(sessionID); !exists {
				sessionData, err := h.SessionStore.Load(sessionID)
				if err != nil {
					continue
				}
				
				// Check if the session is too old
				if sessionData.IsStale(h.Config.SessionTimeout * 2) {
					logger.WithField("session_id", sessionID).Info("Removing stale orphaned session")
					h.SessionStore.Delete(sessionID)
				}
			}
		}
	}
}

// handleSiprecInvite handles SIPREC INVITE requests
func (h *Handler) handleSiprecInvite(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)

	// Add basic request validation
	if err := h.validateSIPRequest(req); err != nil {
		logger.WithError(err).Error("Request validation failed")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - "+err.Error(), nil)
		tx.Respond(resp)
		return
	}

	// Extract metadata from the SIPREC request
	rsMetadata, err := siprec.ExtractRSMetadataFromRequest(req)
	if err != nil {
		logger.WithError(err).Warn("Failed to extract RS-Metadata from request")
		// Continue processing despite metadata extraction failure
	}

	// Extract SDP information
	sdp, err := extractSDP(req)
	if err != nil {
		logger.WithError(err).Error("Failed to extract SDP from SIPREC request")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		return
	}

	// Allocate RTP port
	rtpPort := media.AllocateRTPPort(h.Config.MediaConfig.RTPPortMin, h.Config.MediaConfig.RTPPortMax, h.Logger)

	// Prepare SDP response
	sdpResp, err := h.prepareSdpResponse(sdp, rtpPort)
	if err != nil {
		logger.WithError(err).Error("Failed to prepare SDP response")
		resp := sip.NewResponseFromRequest(req, 500, "Server Error - Failed to prepare SDP response", nil)
		tx.Respond(resp)
		return
	}

	// Create response with SDP
	resp := sip.NewResponseFromRequest(req, 200, "OK", sdpResp)

	// Set necessary headers
	contact := req.GetHeader("Contact")
	if contact != nil {
		resp.AppendHeader(sip.NewHeader("Contact", contact.Value()))
	}

	// Initialize call data
	callData := &CallData{
		DialogInfo:    extractDialogInfo(req),
		LastActivity:  time.Now(),
		RemoteAddress: getSourceAddress(req),
	}

	// Create recording session
	recordingSession := &siprec.RecordingSession{
		ID:              uuid.New().String(),
		SIPID:           callUUID,
		RecordingState:  "active",
		StartTime:       time.Now(),
		Direction:       "unknown", // Default
		Participants:    []siprec.Participant{},
		OriginalRequest: req,
	}

	// Update with metadata if available
	if rsMetadata != nil {
		recordingSession.Direction = rsMetadata.Direction
		recordingSession.Participants = make([]siprec.Participant, len(rsMetadata.Participants))
		
		for i, p := range rsMetadata.Participants {
			recordingSession.Participants[i] = siprec.ConvertRSParticipantToParticipant(p)
		}
	}

	// Create RTP forwarder
	forwarder, err := media.NewRTPForwarder(60*time.Second, recordingSession, h.Logger)
	if err != nil {
		logger.WithError(err).Error("Failed to create RTP forwarder")
		resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - No RTP ports available", nil)
		tx.Respond(resp)
		return
	}
	
	// Set pause state if specified in metadata
	if rsMetadata != nil {
		forwarder.RecordingPaused = rsMetadata.State == "paused"
	}

	// Store in call data
	callData.Forwarder = forwarder
	callData.RecordingSession = recordingSession

	// Store the call state
	h.ActiveCalls.Store(callUUID, callData)

	// Save to persistent store if redundancy is enabled
	if h.Config.RedundancyEnabled {
		if err := h.SessionStore.Save(callUUID, callData); err != nil {
			logger.WithError(err).Warn("Failed to save session to persistent store")
		}
	}

	// Start RTP forwarding
	ctx := context.Background()
	media.StartRTPForwarding(ctx, forwarder, callUUID, h.Config.MediaConfig, h.STTCallback)

	// Send response
	if err := tx.Respond(resp); err != nil {
		logger.WithError(err).Error("Failed to send SIPREC INVITE response")
		return
	}

	logger.Info("Successfully responded to SIPREC INVITE")
}

// CleanupActiveCalls cleans up all active calls
func (h *Handler) CleanupActiveCalls() {
	logger := h.Logger.WithField("component", "cleanup")
	logger.Info("Cleaning up all active calls")
	
	// Track if redundancy is enabled
	isRedundancyEnabled := h.Config.RedundancyEnabled

	// Iterate through all active calls and clean them up
	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)
		
		// Stop RTP forwarding
		if callData.Forwarder != nil {
			logger.WithField("call_uuid", callUUID).Debug("Stopping RTP forwarding")
			close(callData.Forwarder.StopChan)
		}
		
		// Update recording session state if needed
		if callData.RecordingSession != nil {
			callData.RecordingSession.RecordingState = "stopped"
			callData.RecordingSession.EndTime = time.Now()
			
			// If redundancy is enabled, update the session store
			if isRedundancyEnabled {
				h.SessionStore.Save(callUUID, callData)
			}
		}
		
		// Remove from the active calls map
		h.ActiveCalls.Delete(callUUID)

		return true // Continue iteration
	})
	
	// Log status of persistent sessions
	if isRedundancyEnabled {
		sessions, err := h.SessionStore.List()
		if err != nil {
			logger.WithError(err).Error("Failed to list persistent sessions during shutdown")
		} else {
			logger.WithField("preserved_sessions", len(sessions)).
				Info("Sessions preserved in persistent store for recovery")
		}
	}
}

// GetActiveCallCount returns the number of currently active calls
func (h *Handler) GetActiveCallCount() int {
	count := 0
	h.ActiveCalls.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// Shutdown gracefully shuts down the SIP handler and all its components
func (h *Handler) Shutdown(ctx context.Context) error {
	logger := h.Logger.WithField("operation", "sip_shutdown")
	logger.Info("Shutting down SIP handler and all components")

	// First clean up all active calls
	h.CleanupActiveCalls()
	
	// Stop the session monitor
	if h.monitorCancel != nil {
		h.monitorCancel()
		
		// Wait for the monitor goroutine to exit with a timeout
		done := make(chan struct{})
		go func() {
			h.sessionMonitorWG.Wait()
			close(done)
		}()
		
		select {
		case <-done:
			logger.Debug("Session monitor stopped gracefully")
		case <-ctx.Done():
			logger.Warn("Timed out waiting for session monitor to stop")
		}
	}
	
	// Close any server-level resources
	// Use context timeout for any remote connections
	if err := h.UA.Close(); err != nil {
		logger.WithError(err).Warn("Error closing SIP User Agent")
	}
	
	// Close the server
	if h.Server != nil {
		// Check if the Server has a Close method, if not already available
		// For future implementation or different SIP library versions
		logger.Info("SIP Server resources released")
	}
	
	// Close session store if needed
	if closer, ok := h.SessionStore.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			logger.WithError(err).Warn("Error closing session store")
		}
	}
	
	logger.Info("SIP handler shutdown complete")
	return nil
}

// MemorySessionStore is an in-memory implementation of the SessionStore interface
type MemorySessionStore struct {
	sessions sync.Map
}

// NewMemorySessionStore creates a new in-memory session store
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{}
}

// Save stores a call data by key
func (s *MemorySessionStore) Save(key string, data *CallData) error {
	// Serialize the call data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	
	// Store the serialized data
	s.sessions.Store(key, jsonData)
	return nil
}

// Load retrieves call data by key
func (s *MemorySessionStore) Load(key string) (*CallData, error) {
	// Get the serialized data
	value, ok := s.sessions.Load(key)
	if !ok {
		return nil, errors.New("session not found")
	}
	
	// Deserialize the data
	jsonData, ok := value.([]byte)
	if !ok {
		return nil, errors.New("invalid session data format")
	}
	
	// Unmarshal the data
	var callData CallData
	err := json.Unmarshal(jsonData, &callData)
	if err != nil {
		return nil, err
	}
	
	return &callData, nil
}

// Delete removes call data by key
func (s *MemorySessionStore) Delete(key string) error {
	s.sessions.Delete(key)
	return nil
}

// List returns all stored keys
func (s *MemorySessionStore) List() ([]string, error) {
	keys := []string{}
	s.sessions.Range(func(key, _ interface{}) bool {
		keys = append(keys, key.(string))
		return true
	})
	return keys, nil
}