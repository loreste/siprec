package sip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	
	"github.com/emiago/sipgo/sip"
	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
)

// SetupHandlers registers all the SIP handlers
func (h *Handler) SetupHandlers() {
	// Register SIP request handlers with recovery middleware
	h.Server.OnRequest(sip.INVITE, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		// Check if this is an initial INVITE or re-INVITE
		toTag, _ := req.To().Params.Get("tag")
		if toTag != "" {
			// This is a re-INVITE (in-dialog)
			h.handleReInvite(req, tx)
		} else {
			// Initial INVITE
			h.handleSiprecInvite(req, tx)
		}
	}))

	// Handle CANCEL requests
	h.Server.OnRequest(sip.CANCEL, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		h.handleCancel(req, tx)
	}))

	// Handle OPTIONS requests
	h.Server.OnRequest(sip.OPTIONS, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		h.handleOptions(req, tx)
	}))

	// Handle BYE requests
	h.Server.OnRequest(sip.BYE, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		callUUID := req.CallID().String()
		h.handleBye(req, tx, callUUID)
	}))

	// Handle UPDATE requests (for in-dialog updates)
	h.Server.OnRequest(sip.UPDATE, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		h.handleUpdate(req, tx)
	}))
	
	// Handle SUBSCRIBE requests for session state
	h.Server.OnRequest(sip.SUBSCRIBE, h.recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		h.handleSubscribe(req, tx)
	}))
	
	// Note: NOTIFY handler removed as it was a placeholder function
}

// handleReInvite processes re-INVITE requests for existing sessions
func (h *Handler) handleReInvite(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Get current call data
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
		// Unknown session
		logger.Warn("Received re-INVITE for unknown session")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
	}
}

// handleUpdate processes SIP UPDATE requests
func (h *Handler) handleUpdate(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Get current call data
	callDataValue, exists := h.ActiveCalls.Load(callUUID)
	
	if exists {
		callData := callDataValue.(*CallData)
		
		// Update activity timestamp
		callData.UpdateActivity()
		
		// Update session store if redundancy is enabled
		if h.Config.RedundancyEnabled {
			h.SessionStore.Save(callUUID, callData)
		}
		
		// Respond with 200 OK
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
		logger.Info("Processed UPDATE request")
	} else {
		// Unknown session
		logger.Warn("Received UPDATE for unknown session")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
	}
}

// handleSubscribe processes SIP SUBSCRIBE requests for session state
func (h *Handler) handleSubscribe(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	logger := h.Logger.WithField("call_uuid", callUUID)
	
	// Only process if redundancy is enabled
	if !h.Config.RedundancyEnabled {
		resp := sip.NewResponseFromRequest(req, 489, "Bad Event", nil)
		tx.Respond(resp)
		return
	}
	
	// Check Event header
	eventHeader := req.GetHeader("Event")
	if eventHeader == nil || eventHeader.Value() != "siprec-session" {
		resp := sip.NewResponseFromRequest(req, 489, "Bad Event", nil)
		tx.Respond(resp)
		return
	}
	
	// Extract the target session ID
	sessionID := ""
	body := req.Body()
	if len(body) > 0 {
		// Parse body to extract session ID
		// This is simplified - in a real implementation you'd parse XML/JSON
		sessionID = string(body)
	}
	
	if sessionID == "" {
		// If no session ID provided, we can't process the subscription
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Missing Session ID", nil)
		tx.Respond(resp)
		return
	}
	
	// Generate subscription ID (not used in this implementation)
	_ = "sub-" + sessionID
	
	// Respond with 202 Accepted
	resp := sip.NewResponseFromRequest(req, 202, "Accepted", nil)
	resp.AppendHeader(sip.NewHeader("Expires", "3600")) // 1 hour
	resp.AppendHeader(sip.NewHeader("Event", "siprec-session"))
	tx.Respond(resp)
	
	// Note: Sending of NOTIFY removed as it was just a placeholder
	
	logger.WithField("session_id", sessionID).Info("Accepted subscription for session state")
}

// Note: handleNotify and sendSessionStateNotification removed as they were placeholder functions

// monitorSessions periodically checks session health and handles cleanup
func (h *Handler) monitorSessions() {
	ticker := time.NewTicker(h.Config.StateCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			h.checkSessions()
		}
	}
}

// checkSessions verifies all active sessions and updates state store
// Simplified to focus on core functionality
func (h *Handler) checkSessions() {
	now := time.Now()
	logger := h.Logger.WithField("component", "session_monitor")
	
	// Iterate through active calls
	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)
		
		// Check if session is stale
		if callData.IsStale(h.Config.SessionTimeout) {
			logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"last_activity": callData.LastActivity,
				"elapsed": now.Sub(callData.LastActivity).String(),
			}).Warn("Session appears stale, marking for cleanup")
			
			// Clean up stale session
			h.handleBye(nil, nil, callUUID)
		} else if h.Config.RedundancyEnabled {
			// Session is still active, update store
			h.SessionStore.Save(callUUID, callData)
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

	// Check if the call already exists
	if _, exists := h.ActiveCalls.Load(callUUID); exists {
		logger.Warn("Call already exists, ignoring duplicate INVITE")
		return
	}

	// Create a new context for the call
	ctx, cancel := context.WithCancel(context.Background())
	callData := NewCallData(ctx, cancel, callUUID)
	
	// Extract dialog information for potential reconnection
	callData.DialogInfo = extractDialogInfo(req)
	callData.RemoteAddress = getSourceAddress(req)
	callData.State = "establishing"
	
	defer func() {
		// If we don't store this call in activeCalls, we need to cancel the context
		if _, exists := h.ActiveCalls.Load(callUUID); !exists {
			cancel()
		}
	}()

	// Try to parse as SIPREC INVITE
	sdpContent, rsMetadata, err := siprec.ParseSiprecInvite(req)
	if err != nil {
		// If not a SIPREC INVITE, try handling as a regular INVITE with SDP
		logger.WithError(err).Info("Not a SIPREC INVITE, trying as regular INVITE")

		// Check Content-Type for SDP
		contentType := req.GetHeader("Content-Type")
		if contentType == nil || contentType.Value() != "application/sdp" {
			logger.Error("Unsupported Content-Type for INVITE")
			resp := sip.NewResponseFromRequest(req, 415, "Unsupported Media Type", nil)
			tx.Respond(resp)
			return
		}

		// Use body directly as SDP
		sdpContent = string(req.Body())
		rsMetadata = nil
	}

	// Check resource limits
	activeCallCount := 0
	h.ActiveCalls.Range(func(_, _ interface{}) bool {
		activeCallCount++
		return true
	})

	if h.Config.MaxConcurrentCalls > 0 && activeCallCount >= h.Config.MaxConcurrentCalls {
		logger.Warn("Maximum concurrent call limit reached, rejecting new call")
		resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - Maximum concurrent calls reached", nil)
		tx.Respond(resp)
		return
	}

	// Allocate RTP port
	rtpPort := media.AllocateRTPPort(h.Config.MediaConfig.RTPPortMin, h.Config.MediaConfig.RTPPortMax, h.Logger)

	// Create forwarder with or without recording session
	if rsMetadata != nil {
		// This is a SIPREC call
		logger.WithFields(logrus.Fields{
			"recording_session": rsMetadata.SessionID,
			"state":             rsMetadata.State,
			"participants":      len(rsMetadata.Participants),
		}).Info("Received SIPREC INVITE")

		// Create recording session
		recordingSession := &siprec.RecordingSession{
			ID:             rsMetadata.SessionID,
			RecordingState: rsMetadata.State,
			AssociatedTime: time.Now(),
		}

		// Initialize participants if available
		if rsMetadata.Participants != nil {
			recordingSession.Participants = make([]siprec.Participant, 0, len(rsMetadata.Participants))
			for _, p := range rsMetadata.Participants {
				recordingSession.Participants = append(
					recordingSession.Participants,
					siprec.ConvertRSParticipantToParticipant(p),
				)
			}
		}
		
		// Create RTP forwarder
		forwarder := media.NewRTPForwarder(rtpPort, 60*time.Second, recordingSession, h.Logger)
		forwarder.RecordingPaused = rsMetadata.State == "paused"
		
		// Store in call data
		callData.Forwarder = forwarder
		callData.RecordingSession = recordingSession
	} else {
		// Regular call, not SIPREC
		logger.Info("Received regular INVITE with SDP")

		// Create forwarder without recording session
		forwarder := media.NewRTPForwarder(rtpPort, 60*time.Second, nil, h.Logger)
		callData.Forwarder = forwarder
	}

	// Store the call state
	h.ActiveCalls.Store(callUUID, callData)
	
	// If redundancy is enabled, store session in persistent store
	if h.Config.RedundancyEnabled {
		if err := h.SessionStore.Save(callUUID, callData); err != nil {
			logger.WithError(err).Warn("Failed to save session to persistent store")
		}
	}

	// Parse SDP
	sdpParsed := &sdp.SessionDescription{}
	err = sdpParsed.Unmarshal([]byte(sdpContent))
	if err != nil {
		logger.WithError(err).Error("Failed to parse SDP")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		h.ActiveCalls.Delete(callUUID)
		media.ReleaseRTPPort(rtpPort)
		return
	}

	// Generate SDP response
	newSDP := h.generateSDPResponse(sdpParsed, h.Config.MediaConfig.ExternalIP)
	sdpResponseBytes, err := newSDP.Marshal()
	if err != nil {
		logger.WithError(err).Error("Failed to marshal SDP for response")
		resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(resp)
		h.ActiveCalls.Delete(callUUID)
		media.ReleaseRTPPort(rtpPort)
		return
	}

	// Create response based on whether this is SIPREC or regular call
	var resp *sip.Response
	if rsMetadata != nil {
		// Create rs-metadata response for SIPREC
		metadataResponse, err := siprec.CreateMetadataResponse(rsMetadata)
		if err != nil {
			logger.WithError(err).Error("Failed to create metadata response")
			resp = sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(resp)
			h.ActiveCalls.Delete(callUUID)
			media.ReleaseRTPPort(rtpPort)
			return
		}

		// Create multipart response
		contentType, multipartBody := siprec.CreateMultipartResponse(string(sdpResponseBytes), metadataResponse)

		// Send 200 OK response with multipart body
		resp = sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody([]byte(multipartBody))
		resp.RemoveHeader("Content-Type") // Remove default to add our own
		resp.AppendHeader(sip.NewHeader("Content-Type", contentType))
		
		// Add supported header with replaces for redundancy
		if h.Config.RedundancyEnabled {
			resp.AppendHeader(sip.NewHeader("Supported", "siprec, replaces"))
		}
	} else {
		// Regular SDP response for non-SIPREC
		resp = sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody(sdpResponseBytes)
		// Content-Type will be set to application/sdp by default
		
		// Add supported header with replaces for redundancy
		if h.Config.RedundancyEnabled {
			resp.AppendHeader(sip.NewHeader("Supported", "replaces"))
		}
	}

	// Send response
	tx.Respond(resp)

	// Start RTP forwarding, recording, and transcription
	media.StartRTPForwarding(ctx, callData.Forwarder, callUUID, h.Config.MediaConfig, h.SttProvider)

	// Update call state
	callData.State = "active"
	
	// If redundancy is enabled, update the session in persistent store
	if h.Config.RedundancyEnabled {
		if err := h.SessionStore.Save(callUUID, callData); err != nil {
			logger.WithError(err).Warn("Failed to update session in persistent store")
		}
	}

	logger.WithFields(logrus.Fields{
		"is_siprec": rsMetadata != nil,
		"state":     "active",
		"rtp_port":  rtpPort,
	}).Info("Call session established")

	// Set up call cleanup on BYE for this specific dialog
	h.Server.OnRequest(sip.BYE, func(byeReq *sip.Request, byeTx sip.ServerTransaction) {
		if byeReq.CallID().String() == callUUID {
			h.handleBye(byeReq, byeTx, callUUID)
			cancel() // Cancel the context on call termination
		}
	})
}

// recoverSession attempts to recover a lost session from persistent storage
func (h *Handler) recoverSession(req *sip.Request, tx sip.ServerTransaction, oldCallID string) {
	logger := h.Logger.WithFields(logrus.Fields{
		"old_call_id": oldCallID,
		"new_call_id": req.CallID().String(),
	})
	
	// Only proceed if redundancy is enabled
	if !h.Config.RedundancyEnabled {
		logger.Warn("Session recovery attempted but redundancy is not enabled")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
		return
	}
	
	// Try to load the previous session from storage
	oldSession, err := h.SessionStore.Load(oldCallID)
	if err != nil {
		logger.WithError(err).Error("Failed to load previous session")
		resp := sip.NewResponseFromRequest(req, 404, "Session Not Found", nil)
		tx.Respond(resp)
		return
	}
	
	// Check if the session is too old to recover
	if oldSession.IsStale(h.Config.SessionTimeout * 2) {
		logger.Warn("Previous session is too old to recover")
		resp := sip.NewResponseFromRequest(req, 410, "Session Gone", nil)
		tx.Respond(resp)
		return
	}
	
	// Create a new session based on the old one
	newCallUUID := req.CallID().String()
	ctx, cancel := context.WithCancel(context.Background())
	
	// Create new call data based on the recovered session
	newCallData := &CallData{
		CallUUID:         newCallUUID,
		Context:          ctx,
		CancelFunc:       cancel,
		LastActivity:     time.Now(),
		State:            "recovering",
		Sequence:         oldSession.Sequence + 1,
		RemoteAddress:    getSourceAddress(req),
		DialogInfo:       extractDialogInfo(req),
		RecordingSession: oldSession.RecordingSession, // Preserve recording session
	}
	
	// Set up media for the recovered session
	rtpPort := media.AllocateRTPPort(h.Config.MediaConfig.RTPPortMin, h.Config.MediaConfig.RTPPortMax, h.Logger)
	
	// Create a new forwarder with the same recording session
	forwarder := media.NewRTPForwarder(rtpPort, 60*time.Second, oldSession.RecordingSession, h.Logger)
	if oldSession.RecordingSession != nil {
		forwarder.RecordingPaused = oldSession.RecordingSession.RecordingState == "paused"
	}
	
	newCallData.Forwarder = forwarder
	
	// Store the recovered call
	h.ActiveCalls.Store(newCallUUID, newCallData)
	
	// Delete the old session from the store
	h.SessionStore.Delete(oldCallID)
	
	// Save the new session to the store
	h.SessionStore.Save(newCallUUID, newCallData)
	
	// Parse SDP from the request
	contentType := req.GetHeader("Content-Type")
	if contentType == nil || contentType.Value() != "application/sdp" {
		logger.Error("Unsupported Content-Type for recovery INVITE")
		resp := sip.NewResponseFromRequest(req, 415, "Unsupported Media Type", nil)
		tx.Respond(resp)
		h.ActiveCalls.Delete(newCallUUID)
		media.ReleaseRTPPort(rtpPort)
		return
	}
	
	// Parse the SDP
	sdpParsed := &sdp.SessionDescription{}
	err = sdpParsed.Unmarshal(req.Body())
	if err != nil {
		logger.WithError(err).Error("Failed to parse SDP in recovery request")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		h.ActiveCalls.Delete(newCallUUID)
		media.ReleaseRTPPort(rtpPort)
		return
	}
	
	// Generate SDP response
	newSDP := h.generateSDPResponse(sdpParsed, h.Config.MediaConfig.ExternalIP)
	sdpResponseBytes, err := newSDP.Marshal()
	if err != nil {
		logger.WithError(err).Error("Failed to marshal SDP for recovery response")
		resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(resp)
		h.ActiveCalls.Delete(newCallUUID)
		media.ReleaseRTPPort(rtpPort)
		return
	}
	
	// Create the response
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	resp.SetBody(sdpResponseBytes)
	
	// If we have a SIPREC session, include rs-metadata
	if oldSession.RecordingSession != nil {
		// Create a failover metadata using our new library
		failoverMetadata := siprec.CreateFailoverMetadata(oldSession.RecordingSession)
		
		// Create rs-metadata response
		metadataXML, err := siprec.SerializeMetadata(failoverMetadata)
		if err != nil {
			logger.WithError(err).Error("Failed to create metadata for recovery response")
			resp.SetBody(sdpResponseBytes) // Fallback to SDP-only response
		} else {
			// Create multipart response
			contentType, multipartBody := siprec.CreateMultipartResponse(string(sdpResponseBytes), metadataXML)
			resp.SetBody([]byte(multipartBody))
			resp.RemoveHeader("Content-Type")
			resp.AppendHeader(sip.NewHeader("Content-Type", contentType))
		}
	}
	
	// Send the response
	tx.Respond(resp)
	
	// Start RTP forwarding
	media.StartRTPForwarding(ctx, newCallData.Forwarder, newCallUUID, h.Config.MediaConfig, h.SttProvider)
	
	// Update call state
	newCallData.State = "active"
	h.SessionStore.Save(newCallUUID, newCallData)
	
	logger.WithFields(logrus.Fields{
		"new_state":       "active",
		"recovery_status": "successful",
		"rtp_port":        rtpPort,
	}).Info("Session recovered successfully")
	
	// Set up call cleanup on BYE for this specific dialog
	h.Server.OnRequest(sip.BYE, func(byeReq *sip.Request, byeTx sip.ServerTransaction) {
		if byeReq.CallID().String() == newCallUUID {
			h.handleBye(byeReq, byeTx, newCallUUID)
			cancel() // Cancel the context on call termination
		}
	})
}

// Helper functions for session redundancy

// extractDialogInfo extracts dialog information from a SIP request
func extractDialogInfo(req *sip.Request) *DialogInfo {
	if req == nil {
		return nil
	}
	
	dialogInfo := &DialogInfo{
		CallID:     req.CallID().String(),
		LocalURI:   req.To().Address.String(),
		RemoteURI:  req.From().Address.String(),
		RemoteTag:  getTagParam(req.From()),
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
	if req == nil || req.CSeq() == nil {
		return 0
	}
	
	return int(req.CSeq().SeqNo)
}

// getSourceAddress extracts source address from request
func getSourceAddress(req *sip.Request) string {
	if req == nil {
		return ""
	}
	
	// In newer versions of the SIP library, Source might be a string
	// This is a safe way to handle it
	source := ""
	srcField := req.Source
	if srcField != nil {
		source = fmt.Sprintf("%v", srcField)
	}
	
	return source
}

// parseReplacesHeader parses the Replaces header to extract dialog identifiers
func parseReplacesHeader(replacesValue string) (callID string, toTag string, fromTag string) {
	// Format: call-id;to-tag=xxx;from-tag=yyy
	if replacesValue == "" {
		return "", "", ""
	}
	
	// Simple naive parsing, in a real implementation use proper SIP parsing
	params := make(map[string]string)
	
	// Split by semicolons
	parts := strings.Split(replacesValue, ";")
	if len(parts) < 3 {
		return "", "", ""
	}
	
	callID = parts[0]
	
	// Parse remaining parts as params
	for i := 1; i < len(parts); i++ {
		param := strings.Split(parts[i], "=")
		if len(param) != 2 {
			continue
		}
		
		params[param[0]] = param[1]
	}
	
	toTag = params["to-tag"]
	fromTag = params["from-tag"]
	
	return callID, toTag, fromTag
}

// recoverMiddleware wraps a SIP handler with panic recovery
func (h *Handler) recoverMiddleware(handler func(*sip.Request, sip.ServerTransaction)) func(*sip.Request, sip.ServerTransaction) {
	return func(req *sip.Request, tx sip.ServerTransaction) {
		defer func() {
			if r := recover(); r != nil {
				h.Logger.WithFields(logrus.Fields{
					"call_uuid": req.CallID().String(),
					"method":    req.Method,
					"panic":     r,
				}).Error("Recovered from panic in SIP handler")

				// Try to send a 500 response if possible
				resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
				tx.Respond(resp)
			}
		}()

		// Call the original handler
		handler(req, tx)
	}
}

// validateSIPRequest performs basic validation of SIP requests
func (h *Handler) validateSIPRequest(req *sip.Request) error {
	// Verify Call-ID
	callID := req.CallID().String()
	if callID == "" {
		return errors.New("missing Call-ID")
	}

	// Verify From header
	from := req.From()
	if from == nil || from.Address.String() == "" {
		return errors.New("invalid From header")
	}

	// Verify To header
	to := req.To()
	if to == nil || to.Address.String() == "" {
		return errors.New("invalid To header")
	}

	// Verify body size (10MB limit)
	if len(req.Body()) > 10*1024*1024 {
		return errors.New("request body too large")
	}

	return nil
}

// handleOptions handles OPTIONS requests
func (h *Handler) handleOptions(req *sip.Request, tx sip.ServerTransaction) {
	// Create a 200 OK response with supported capabilities
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)

	// Add headers to indicate supported capabilities
	resp.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS"))
	resp.AppendHeader(sip.NewHeader("Accept", "application/sdp, application/rs-metadata+xml"))
	resp.AppendHeader(sip.NewHeader("Supported", "siprec, replaces, timer"))

	// Send the response
	tx.Respond(resp)

	h.Logger.WithField("method", "OPTIONS").Info("Responded to OPTIONS request")
}

// handleCancel handles CANCEL requests
func (h *Handler) handleCancel(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	if _, exists := h.ActiveCalls.Load(callUUID); exists {
		h.handleBye(req, tx, callUUID) // Treat CANCEL like a BYE for cleanup
		h.Logger.WithField("call_uuid", callUUID).Info("Call cancelled")
	} else {
		h.Logger.WithField("call_uuid", callUUID).Warn("Received CANCEL for non-existent call")
	}
}

// handleBye handles BYE requests
func (h *Handler) handleBye(req *sip.Request, tx sip.ServerTransaction, callUUID string) {
	// If callUUID is not provided but request is, extract it
	if callUUID == "" && req != nil {
		callUUID = req.CallID().String()
	}

	logger := h.Logger.WithField("call_uuid", callUUID)

	// Retrieve the call state
	callDataValue, exists := h.ActiveCalls.Load(callUUID)
	if !exists {
		logger.Warn("Received BYE for non-existent call")
		
		// Check if we have this session in persistent storage if redundancy is enabled
		if h.Config.RedundancyEnabled && req != nil {
			if storedSession, err := h.SessionStore.Load(callUUID); err == nil {
				logger.Info("Found stale session in persistent store, cleaning up")
				h.SessionStore.Delete(callUUID)
				
				// If the session had a forwarder, free the RTP port
				if storedSession.Forwarder != nil {
					media.ReleaseRTPPort(storedSession.Forwarder.LocalPort)
				}
			}
		}
		
		// Send 404 response if this is a direct request
		if tx != nil && req != nil {
			resp := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
			tx.Respond(resp)
		}
		return
	}

	callData := callDataValue.(*CallData)
	
	// Update session state
	callData.State = "terminating"
	
	// If redundancy is enabled, update session store before deletion
	if h.Config.RedundancyEnabled {
		// Final update to the session before removal
		if err := h.SessionStore.Save(callUUID, callData); err != nil {
			logger.WithError(err).Warn("Failed to update session in store during termination")
		}
		
		// Schedule deletion from persistent store with a small delay to allow
		// any in-flight recovery attempts to complete
		if req != nil { // Only for externally triggered terminations
			go func() {
				time.Sleep(5 * time.Second)
				if err := h.SessionStore.Delete(callUUID); err != nil {
					logger.WithError(err).Warn("Failed to delete session from persistent store")
				}
			}()
		} else {
			// Immediate deletion for internal cleanup
			h.SessionStore.Delete(callUUID)
		}
	}

	// Clean up the call resources
	if callData.Forwarder != nil {
		callData.Forwarder.StopChan <- struct{}{}
		media.ReleaseRTPPort(callData.Forwarder.LocalPort)
	}
	
	// Cancel the context
	if callData.CancelFunc != nil {
		callData.CancelFunc()
	}
	
	// Remove from active calls
	h.ActiveCalls.Delete(callUUID)

	// Respond with 200 OK
	if tx != nil && req != nil {
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
	}

	logger.Info("Call terminated and resources cleaned up")
}

// CleanupActiveCalls performs cleanup of all active calls
func (h *Handler) CleanupActiveCalls() {
	logger := h.Logger.WithField("operation", "shutdown")
	
	// If redundancy is enabled, mark sessions as shutting down but preserve them
	// in the store for potential recovery
	isRedundancyEnabled := h.Config.RedundancyEnabled
	
	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)

		callLogger := logger.WithField("call_uuid", callUUID)
		callLogger.Info("Cleaning up call during shutdown")
		
		// Update session state for redundancy
		if isRedundancyEnabled {
			// Mark as inactive but potentially recoverable
			callData.State = "suspended"
			callData.LastActivity = time.Now()
			
			// Save final state to store
			if err := h.SessionStore.Save(callUUID, callData); err != nil {
				callLogger.WithError(err).Warn("Failed to save session state during shutdown")
			} else {
				callLogger.Info("Session state preserved for potential recovery")
			}
		}

		// Signal the forwarder to stop
		if callData.Forwarder != nil {
			callData.Forwarder.StopChan <- struct{}{}
			media.ReleaseRTPPort(callData.Forwarder.LocalPort)
		}
		
		// Cancel context
		if callData.CancelFunc != nil {
			callData.CancelFunc()
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

