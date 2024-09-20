package main

import (
	"context"
	"sync"

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

	// Check if the call already exists
	if _, exists := activeCalls.Load(callUUID); exists {
		logger.WithField("call_uuid", callUUID).Warn("Call already exists, ignoring duplicate INVITE")
		return
	}

	// Create a new context for the call
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Ensure cancellation to avoid context leaks

	rtpPort := allocateRTPPort() // Allocate RTP port for the call
	forwarder := &RTPForwarder{
		localPort:      rtpPort,
		stopChan:       make(chan struct{}),
		transcriptChan: make(chan string),
	}

	// Store the call state
	activeCalls.Store(callUUID, forwarder)

	// Process the SDP from the INVITE
	sdpBody := req.Body()
	sdpParsed := &sdp.SessionDescription{}
	err := sdpParsed.Unmarshal([]byte(sdpBody))
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to parse SDP from SIPREC INVITE")
		resp := sip.NewResponseFromRequest(req, 400, "Bad Request", nil)
		tx.Respond(resp)
		return
	}

	// Generate SDP response
	newSDP := generateSDPResponse(sdpParsed, config.ExternalIP)

	// Send 200 OK response with the new SDP
	sdpResponseBytes, err := newSDP.Marshal()
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to marshal SDP for 200 OK")
		resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
		tx.Respond(resp)
		return
	}
	resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
	resp.SetBody(sdpResponseBytes)
	tx.Respond(resp)

	// Start RTP forwarding, recording, and transcription
	go startRTPForwarding(ctx, forwarder, callUUID)

	logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"state":     "In Progress",
		"rtp_port":  rtpPort,
	}).Info("SIPREC INVITE handled, call is in progress")

	// Wait for ACK to confirm call setup
	server.OnRequest(sip.ACK, func(req *sip.Request, tx sip.ServerTransaction) {
		if req.CallID().String() == callUUID {
			logger.WithField("call_uuid", callUUID).Info("Received ACK, call confirmed")
		}
	})

	// Set up call cleanup on BYE
	server.OnRequest(sip.BYE, func(req *sip.Request, tx sip.ServerTransaction) {
		if req.CallID().String() == callUUID {
			handleBye(req, tx)
			cancel() // Cancel the context on call termination
		}
	})
}

func handleBye(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()

	// Retrieve the call state
	forwarder, exists := activeCalls.Load(callUUID)
	if !exists {
		logger.WithField("call_uuid", callUUID).Warn("Received BYE for non-existent call")
		if tx != nil {
			resp := sip.NewResponseFromRequest(req, 404, "Not Found", nil)
			tx.Respond(resp)
		}
		return
	}

	// Clean up the call resources
	forwarder.(*RTPForwarder).stopChan <- struct{}{}
	releaseRTPPort(forwarder.(*RTPForwarder).localPort)
	activeCalls.Delete(callUUID)

	// Respond with 200 OK
	if tx != nil {
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
	}

	logger.WithField("call_uuid", callUUID).Info("Call terminated and resources cleaned up")
}

func handleCancel(req *sip.Request, tx sip.ServerTransaction) {
	callUUID := req.CallID().String()
	if _, exists := activeCalls.Load(callUUID); exists {
		handleBye(req, tx) // Treat CANCEL like a BYE for cleanup
		logger.WithField("call_uuid", callUUID).Info("Call cancelled")
	} else {
		logger.WithField("call_uuid", callUUID).Warn("Received CANCEL for non-existent call")
	}
}

func cleanupActiveCalls() {
	activeCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		logger.Println("callUUID: ", callUUID)
		handleBye(nil, nil) // Clean up all active calls
		return true
	})
}

func generateSDPResponse(receivedSDP *sdp.SessionDescription, ipToUse string) *sdp.SessionDescription {
	mediaStreams := make([]*sdp.MediaDescription, len(receivedSDP.MediaDescriptions))

	for i, media := range receivedSDP.MediaDescriptions {
		// Allocate a dynamic RTP port
		rtpPort := allocateRTPPort()

		newMedia := &sdp.MediaDescription{
			MediaName: sdp.MediaName{
				Media:   media.MediaName.Media,
				Port:    sdp.RangedPort{Value: int(rtpPort)},
				Protos:  media.MediaName.Protos,
				Formats: media.MediaName.Formats,
			},
			ConnectionInformation: &sdp.ConnectionInformation{
				NetworkType: "IN",
				AddressType: "IP4",
				Address:     &sdp.Address{Address: ipToUse},
			},
			Attributes: media.Attributes,
		}
		mediaStreams[i] = newMedia
	}

	return &sdp.SessionDescription{
		Origin:                receivedSDP.Origin,
		SessionName:           receivedSDP.SessionName,
		ConnectionInformation: &sdp.ConnectionInformation{NetworkType: "IN", AddressType: "IP4", Address: &sdp.Address{Address: ipToUse}},
		TimeDescriptions:      receivedSDP.TimeDescriptions,
		MediaDescriptions:     mediaStreams,
	}
}
