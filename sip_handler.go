package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/sdp/v3"
	"github.com/sirupsen/logrus"
)

var (
	activeCalls sync.Map
)

func handleSiprecInvite(req *sip.Request, tx sip.ServerTransaction, server *sipgo.Server) {
	callUUID := req.CallID().String()

	// Add basic request validation
	if err := validateSIPRequest(req); err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Request validation failed")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - "+err.Error(), nil)
		tx.Respond(resp)
		return
	}

	// Check if the call already exists
	if _, exists := activeCalls.Load(callUUID); exists {
		logger.WithField("call_uuid", callUUID).Warn("Call already exists, ignoring duplicate INVITE")
		return
	}

	// Create a new context for the call
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		// If we don't store this call in activeCalls, we need to cancel the context
		if _, exists := activeCalls.Load(callUUID); !exists {
			cancel()
		}
	}()

	// Try to parse as SIPREC INVITE
	sdpContent, rsMetadata, err := ParseSiprecInvite(req)
	if err != nil {
		// If not a SIPREC INVITE, try handling as a regular INVITE with SDP
		logger.WithError(err).WithField("call_uuid", callUUID).Info("Not a SIPREC INVITE, trying as regular INVITE")

		// Check Content-Type for SDP
		contentType := req.GetHeader("Content-Type")
		if contentType == nil || contentType.Value() != "application/sdp" {
			logger.WithField("call_uuid", callUUID).Error("Unsupported Content-Type for INVITE")
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
	activeCalls.Range(func(_, _ interface{}) bool {
		activeCallCount++
		return true
	})

	if config.MaxConcurrentCalls > 0 && activeCallCount >= config.MaxConcurrentCalls {
		logger.WithField("call_uuid", callUUID).Warn("Maximum concurrent call limit reached, rejecting new call")
		resp := sip.NewResponseFromRequest(req, 503, "Service Unavailable - Maximum concurrent calls reached", nil)
		tx.Respond(resp)
		return
	}

	// Allocate RTP port
	rtpPort := allocateRTPPort()

	// Create forwarder with or without recording session
	var forwarder *RTPForwarder
	if rsMetadata != nil {
		// This is a SIPREC call
		logger.WithFields(logrus.Fields{
			"call_uuid":         callUUID,
			"recording_session": rsMetadata.SessionID,
			"state":             rsMetadata.State,
			"participants":      len(rsMetadata.Participants),
		}).Info("Received SIPREC INVITE")

		// Create forwarder with recording session
		forwarder = &RTPForwarder{
			localPort:      rtpPort,
			stopChan:       make(chan struct{}),
			transcriptChan: make(chan string),
			timeout:        60 * time.Second,
			recordingSession: &RecordingSession{
				ID:             rsMetadata.SessionID,
				RecordingState: rsMetadata.State,
				AssociatedTime: time.Now(),
			},
			recordingPaused: rsMetadata.State == "paused",
		}

		// Initialize participants if available
		if rsMetadata.Participants != nil {
			forwarder.recordingSession.Participants = make([]Participant, 0, len(rsMetadata.Participants))
			for _, p := range rsMetadata.Participants {
				forwarder.recordingSession.Participants = append(
					forwarder.recordingSession.Participants,
					ConvertRSParticipantToParticipant(p),
				)
			}
		}
	} else {
		// Regular call, not SIPREC
		logger.WithField("call_uuid", callUUID).Info("Received regular INVITE with SDP")

		// Create forwarder without recording session
		forwarder = &RTPForwarder{
			localPort:      rtpPort,
			stopChan:       make(chan struct{}),
			transcriptChan: make(chan string),
			timeout:        60 * time.Second,
		}
	}

	// Store the call state
	activeCalls.Store(callUUID, forwarder)

	// Parse SDP
	sdpParsed := &sdp.SessionDescription{}
	err = sdpParsed.Unmarshal([]byte(sdpContent))
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to parse SDP")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
		tx.Respond(resp)
		activeCalls.Delete(callUUID)
		releaseRTPPort(rtpPort)
		return
	}

	// Generate SDP response
	newSDP := generateSDPResponse(sdpParsed, config.ExternalIP)
	sdpResponseBytes, err := newSDP.Marshal()
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal SDP for response")
		resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(resp)
		activeCalls.Delete(callUUID)
		releaseRTPPort(rtpPort)
		return
	}

	// Create response based on whether this is SIPREC or regular call
	var resp *sip.Response
	if rsMetadata != nil {
		// Create rs-metadata response for SIPREC
		metadataResponse, err := CreateMetadataResponse(rsMetadata)
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create metadata response")
			resp = sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(resp)
			activeCalls.Delete(callUUID)
			releaseRTPPort(rtpPort)
			return
		}

		// Create multipart response
		contentType, multipartBody := CreateMultipartResponse(string(sdpResponseBytes), metadataResponse)

		// Send 200 OK response with multipart body
		resp = sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody([]byte(multipartBody))
		resp.RemoveHeader("Content-Type") // Remove default to add our own
		resp.AppendHeader(sip.NewHeader("Content-Type", contentType))
	} else {
		// Regular SDP response for non-SIPREC
		resp = sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody(sdpResponseBytes)
		// Content-Type will be set to application/sdp by default
	}

	// Send response
	tx.Respond(resp)

	// Start RTP forwarding, recording, and transcription
	go startRTPForwarding(ctx, forwarder, callUUID)

	logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"is_siprec": rsMetadata != nil,
		"state":     "In Progress",
		"rtp_port":  rtpPort,
	}).Info("Call session established")

	// Set call cleanup timeout based on max duration
	if config.RecordingMaxDuration > 0 {
		go func() {
			select {
			case <-time.After(config.RecordingMaxDuration):
				logger.WithField("call_uuid", callUUID).Info("Maximum recording duration reached, terminating call")
				handleBye(nil, nil, callUUID)
				cancel()
			case <-ctx.Done():
				// Context was canceled, no need to do anything
				return
			}
		}()
	}

	// Wait for ACK to confirm call setup
	server.OnRequest(sip.ACK, func(req *sip.Request, tx sip.ServerTransaction) {
		if req.CallID().String() == callUUID {
			logger.WithField("call_uuid", callUUID).Info("Received ACK, session confirmed")
		}
	})

	// Set up call cleanup on BYE
	server.OnRequest(sip.BYE, func(req *sip.Request, tx sip.ServerTransaction) {
		if req.CallID().String() == callUUID {
			handleBye(req, tx, callUUID)
			cancel() // Cancel the context on call termination
		}
	})
}

// handleBye handles BYE requests
func handleBye(req *sip.Request, tx sip.ServerTransaction, callUUID string) {
	// If callUUID is not provided but request is, extract it
	if callUUID == "" && req != nil {
		callUUID = req.CallID().String()
	}

	// Retrieve the call state
	forwarderValue, exists := activeCalls.Load(callUUID)
	if !exists {
		logger.WithField("call_uuid", callUUID).Warn("Received BYE for non-existent call")
		if tx != nil {
			resp := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
			tx.Respond(resp)
		}
		return
	}

	forwarder := forwarderValue.(*RTPForwarder)

	// Clean up the call resources
	forwarder.stopChan <- struct{}{}
	releaseRTPPort(forwarder.localPort)
	activeCalls.Delete(callUUID)

	// Respond with 200 OK
	if tx != nil && req != nil {
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
	}

	logger.WithField("call_uuid", callUUID).Info("Call terminated and resources cleaned up")
}

// handleCancel handles CANCEL requests
func handleCancel(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	if _, exists := activeCalls.Load(callUUID); exists {
		handleBye(req, tx, callUUID) // Treat CANCEL like a BYE for cleanup
		logger.WithField("call_uuid", callUUID).Info("Call cancelled")
	} else {
		logger.WithField("call_uuid", callUUID).Warn("Received CANCEL for non-existent call")
	}
}

// handleOptions handles OPTIONS requests
func handleOptions(req *sip.Request, tx sip.ServerTransaction) {
	// Create a 200 OK response with supported capabilities
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)

	// Add headers to indicate supported capabilities
	resp.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS"))
	resp.AppendHeader(sip.NewHeader("Accept", "application/sdp, application/rs-metadata+xml"))
	resp.AppendHeader(sip.NewHeader("Supported", "siprec, replaces, timer"))

	// Send the response
	tx.Respond(resp)

	logger.WithField("method", "OPTIONS").Info("Responded to OPTIONS request")
}

// handleReInvite handles in-dialog INVITE requests
func handleReInvite(req *sip.Request, tx sip.ServerTransaction, server *sipgo.Server) {
	callUUID := req.CallID().String()

	// Check if this call exists
	forwarderValue, exists := activeCalls.Load(callUUID)
	if !exists {
		logger.WithField("call_uuid", callUUID).Warn("Received re-INVITE for non-existent call")
		resp := sip.NewResponseFromRequest(req, 481, "Call/Transaction Does Not Exist", nil)
		tx.Respond(resp)
		return
	}

	forwarder := forwarderValue.(*RTPForwarder)

	// Check for SIPREC content (multipart message)
	contentType := req.GetHeader("Content-Type")
	if contentType != nil && strings.HasPrefix(contentType.Value(), "multipart/") {
		// Parse SIPREC re-INVITE
		sdpContent, rsMetadata, err := ParseSiprecInvite(req)
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to parse SIPREC re-INVITE")
			resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SIPREC format", nil)
			tx.Respond(resp)
			return
		}

		// Detect and log participant changes
		if forwarder.recordingSession != nil {
			added, removed, modified := DetectParticipantChanges(forwarder.recordingSession, rsMetadata)

			// Log participant changes
			if len(added) > 0 {
				logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"count":     len(added),
					"ids":       GetParticipantIDs(added),
				}).Info("Participants added to recording session")
			}

			if len(removed) > 0 {
				logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"count":     len(removed),
					"ids":       GetParticipantIDs(removed),
				}).Info("Participants removed from recording session")
			}

			if len(modified) > 0 {
				logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"count":     len(modified),
					"ids":       GetParticipantIDs(modified),
				}).Info("Participants modified in recording session")
			}

			// Update recording session metadata
			stateChanged := forwarder.recordingSession.RecordingState != rsMetadata.State && rsMetadata.State != ""
			UpdateRecordingSession(forwarder.recordingSession, rsMetadata)

			if stateChanged {
				logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"old_state": forwarder.recordingSession.RecordingState,
					"new_state": rsMetadata.State,
				}).Info("Recording state changed")

				// If state changed to "paused" or "stopped", we might want to take special action
				if rsMetadata.State == "paused" {
					// Handle recording pause
					forwarder.recordingPaused = true
					logger.WithField("call_uuid", callUUID).Info("Recording paused")
				} else if rsMetadata.State == "stopped" {
					// Handle recording stop
					logger.WithField("call_uuid", callUUID).Info("Recording stopped")
				} else if rsMetadata.State == "active" && forwarder.recordingSession.RecordingState != "active" {
					// Recording resumed
					forwarder.recordingPaused = false
					logger.WithField("call_uuid", callUUID).Info("Recording resumed")
				}
			}

			// Send a metadata update event via AMQP
			sendMetadataUpdateToAMQP(callUUID, forwarder.recordingSession, added, removed, modified)
		}

		// Process SDP
		sdpParsed := &sdp.SessionDescription{}
		err = sdpParsed.Unmarshal([]byte(sdpContent))
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to parse SDP in re-INVITE")
			resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
			tx.Respond(resp)
			return
		}

		// Generate SDP response - maintaining the same RTP port
		newSDP := generateSDPResponseWithPort(sdpParsed, config.ExternalIP, forwarder.localPort)
		sdpResponseBytes, err := newSDP.Marshal()
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal SDP for re-INVITE response")
			resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(resp)
			return
		}

		// Create rs-metadata response
		metadataResponse, err := CreateMetadataResponse(rsMetadata)
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create metadata response for re-INVITE")
			resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(resp)
			return
		}

		// Create multipart response
		contentTypeValue, multipartBody := CreateMultipartResponse(string(sdpResponseBytes), metadataResponse)

		// Send 200 OK response with multipart body
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody([]byte(multipartBody))
		resp.RemoveHeader("Content-Type")
		resp.AppendHeader(sip.NewHeader("Content-Type", contentTypeValue))
		tx.Respond(resp)

		logger.WithField("call_uuid", callUUID).Info("Processed SIPREC re-INVITE with updated metadata")
		return
	} else if contentType != nil && contentType.Value() == "application/sdp" {
		// Process regular SDP re-INVITE (not full SIPREC)
		sdpBody := string(req.Body())
		sdpParsed := &sdp.SessionDescription{}
		err := sdpParsed.Unmarshal([]byte(sdpBody))
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to parse SDP in re-INVITE")
			resp := sip.NewResponseFromRequest(req, 400, "Bad Request - Invalid SDP", nil)
			tx.Respond(resp)
			return
		}

		// Generate SDP response - use the existing port
		newSDP := generateSDPResponseWithPort(sdpParsed, config.ExternalIP, forwarder.localPort)
		sdpResponseBytes, err := newSDP.Marshal()
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal SDP for re-INVITE response")
			resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
			tx.Respond(resp)
			return
		}

		// Send 200 OK with updated SDP
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		resp.SetBody(sdpResponseBytes)
		tx.Respond(resp)

		logger.WithField("call_uuid", callUUID).Info("Processed re-INVITE with SDP changes")
		return
	}

	// Fallback for other types of re-INVITE
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	tx.Respond(resp)

	logger.WithField("call_uuid", callUUID).Info("Processed re-INVITE without changes")
}

// cleanupActiveCalls performs cleanup of all active calls
func cleanupActiveCalls() {
	activeCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		forwarder := value.(*RTPForwarder)

		logger.WithField("call_uuid", callUUID).Info("Cleaning up call during shutdown")

		// Signal the forwarder to stop
		forwarder.stopChan <- struct{}{}

		// Release the port
		releaseRTPPort(forwarder.localPort)

		// Remove from the active calls map
		activeCalls.Delete(callUUID)

		return true // Continue iteration
	})
}

// validateSIPRequest performs basic validation of SIP requests
func validateSIPRequest(req *sip.Request) error {
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

// Generate SDP response with specific port (for re-INVITEs)
func generateSDPResponseWithPort(receivedSDP *sdp.SessionDescription, ipToUse string, rtpPort int) *sdp.SessionDescription {
	mediaStreams := make([]*sdp.MediaDescription, len(receivedSDP.MediaDescriptions))

	for i, media := range receivedSDP.MediaDescriptions {
		// Create new attributes, transforming direction attributes as needed
		newAttributes := []sdp.Attribute{}
		foundDirectionAttr := false

		for _, attr := range media.Attributes {
			switch attr.Key {
			case "sendonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
				foundDirectionAttr = true
			case "sendrecv":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "inactive":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "recvonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "sendonly"})
				foundDirectionAttr = true
			default:
				newAttributes = append(newAttributes, attr)
			}
		}

		// If no direction attribute found, default to recvonly
		if !foundDirectionAttr {
			newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
		}

		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   media.MediaName.Media,
				Port:    sdp.RangedPort{Value: rtpPort}, // Use provided port
				Protos:  media.MediaName.Protos,
				Formats: prioritizeCodecs(media.MediaName.Formats),
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: ipToUse},
			},
			Attributes: appendCodecAttributes(newAttributes, prioritizeCodecs(media.MediaName.Formats)),
		}
		mediaStreams[i] = newMedia
	}

	return &sdp.SessionDescription{
		Origin:                receivedSDP.Origin,
		SessionName:           receivedSDP.SessionName,
		ConnectionInformation: &sdp.ConnectionInformation{NetworkType: "IN", AddressType: "IP4", Address: &sdp.Address{Address: ipToUse}},
		TimeDescriptions:      receivedSDP.TimeDescriptions,
		MediaDescriptions:     mediaStreams,
		Attributes:            []sdp.Attribute{{Key: "a", Value: "recording-session"}}, // Mark as recording session
	}
}

// generateSDPResponse generates an SDP response for the initial INVITE
func generateSDPResponse(receivedSDP *sdp.SessionDescription, ipToUse string) *sdp.SessionDescription {
	mediaStreams := make([]*sdp.MediaDescription, len(receivedSDP.MediaDescriptions))

	// Handle NAT traversal for SDP
	connectionAddr := ipToUse
	if config.BehindNAT {
		// Use external IP for connection address
		connectionAddr = config.ExternalIP

		// Log NAT traversal
		logger.WithFields(logrus.Fields{
			"internal_ip": config.InternalIP,
			"external_ip": config.ExternalIP,
		}).Debug("Using external IP for SDP due to NAT")
	}

	for i, media := range receivedSDP.MediaDescriptions {
		// Allocate a dynamic RTP port
		rtpPort := allocateRTPPort()

		// Create new attributes, handling direction and NAT
		newAttributes := []sdp.Attribute{}
		foundDirectionAttr := false

		for _, attr := range media.Attributes {
			// Process direction attributes
			switch attr.Key {
			case "sendonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
				foundDirectionAttr = true
			case "sendrecv":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "inactive":
				newAttributes = append(newAttributes, attr)
				foundDirectionAttr = true
			case "recvonly":
				newAttributes = append(newAttributes, sdp.Attribute{Key: "sendonly"})
				foundDirectionAttr = true
			default:
				// Don't forward local network attributes in NAT scenarios
				if config.BehindNAT && (attr.Key == "candidate" && strings.Contains(attr.Value, config.InternalIP)) {
					continue
				}

				newAttributes = append(newAttributes, attr)
			}
		}

		// If no direction attribute found, default to recvonly
		if !foundDirectionAttr {
			newAttributes = append(newAttributes, sdp.Attribute{Key: "recvonly"})
		}

		// Add NAT-specific attributes if needed
		if config.BehindNAT {
			// Add ICE attributes for NAT traversal
			newAttributes = append(newAttributes, sdp.Attribute{Key: "rtcp-mux", Value: ""})
		}

		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   media.MediaName.Media,
				Port:    sdp.RangedPort{Value: rtpPort},
				Protos:  media.MediaName.Protos,
				Formats: prioritizeCodecs(media.MediaName.Formats),
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: connectionAddr}, // Use NAT-aware address
			},
			Attributes: appendCodecAttributes(newAttributes, prioritizeCodecs(media.MediaName.Formats)),
		}
		mediaStreams[i] = newMedia
	}

	// Update this part in generateSDPResponse function
	return &sdp.SessionDescription{
		// Change from pointer to value
		Origin: sdp.Origin{
			Username:       receivedSDP.Origin.Username,
			SessionID:      receivedSDP.Origin.SessionID,
			SessionVersion: receivedSDP.Origin.SessionVersion,
			NetworkType:    "IN",
			AddressType:    "IP4",
			UnicastAddress: connectionAddr, // Use NAT-aware address
		},
		SessionName: receivedSDP.SessionName,
		ConnectionInformation: &sdp.ConnectionInformation{
			NetworkType: "IN",
			AddressType: "IP4",
			Address:     &sdp.Address{Address: connectionAddr}, // Use NAT-aware address
		},
		TimeDescriptions:  receivedSDP.TimeDescriptions,
		MediaDescriptions: mediaStreams,
		Attributes:        []sdp.Attribute{{Key: "a", Value: "recording-session"}},
	}
}

// Helper function to prioritize G.711 and G.722 codecs
func prioritizeCodecs(formats []string) []string {
	// G.711 μ-law (PCMU) payload type is 0
	// G.711 a-law (PCMA) payload type is 8
	// G.722 payload type is 9

	// Create a map for easy lookup
	formatMap := make(map[string]bool)
	for _, format := range formats {
		formatMap[format] = true
	}

	prioritized := []string{}

	// Add G.711 codecs first as highest priority
	preferredG711 := []string{"0", "8"} // PCMU, PCMA (G.711 variants)
	for _, codec := range preferredG711 {
		if formatMap[codec] {
			prioritized = append(prioritized, codec)
			delete(formatMap, codec) // Remove to avoid duplicates
		}
	}

	// Then add G.722 if available
	if formatMap["9"] { // G.722
		prioritized = append(prioritized, "9")
		delete(formatMap, "9")
	}

	// Add any remaining formats
	for _, format := range formats {
		if formatMap[format] {
			prioritized = append(prioritized, format)
		}
	}

	// Log the codec prioritization
	logger.WithFields(logrus.Fields{
		"original":    formats,
		"prioritized": prioritized,
	}).Debug("Prioritized codecs for SDP")

	return prioritized
}

// Helper function to add codec-specific SDP attributes
func appendCodecAttributes(attributes []sdp.Attribute, formats []string) []sdp.Attribute {
	// Keep existing attributes that are not related to codecs
	filteredAttributes := []sdp.Attribute{}
	for _, attr := range attributes {
		if !strings.HasPrefix(attr.Key, "rtpmap") && !strings.HasPrefix(attr.Key, "fmtp") {
			filteredAttributes = append(filteredAttributes, attr)
		}
	}

	// Add attributes for prioritized codecs
	for _, format := range formats {
		switch format {
		case "0": // G.711 PCMU
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "0 PCMU/8000",
			})
		case "8": // G.711 PCMA
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "8 PCMA/8000",
			})
		case "9": // G.722
			filteredAttributes = append(filteredAttributes, sdp.Attribute{
				Key:   "rtpmap",
				Value: "9 G722/8000",
			})
		}
	}

	return filteredAttributes
}
