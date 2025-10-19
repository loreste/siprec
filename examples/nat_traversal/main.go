package main

import (
	"context"
	"io"
	"log"
	"time"

	"siprec-server/pkg/media"
	"siprec-server/pkg/sip"

	"github.com/sirupsen/logrus"
)

// Example of how to configure and use NAT traversal in the SIPREC server
func main() {
	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Example 1: Configure NAT traversal using explicit NAT configuration
	natConfig := &sip.NATConfig{
		BehindNAT:            true,
		InternalIP:           "192.168.1.100",
		ExternalIP:           "203.0.113.10", // Your public IP
		InternalPort:         5060,
		ExternalPort:         5060,
		RewriteVia:           true,
		RewriteContact:       true,
		RewriteRecordRoute:   true,
		AutoDetectExternalIP: false, // Set to true if you want auto-detection
		STUNServer:           "stun.l.google.com:19302",
		ForceRewrite:         false,
	}

	// Validate NAT configuration
	if err := sip.ValidateNATConfig(natConfig); err != nil {
		log.Fatalf("Invalid NAT configuration: %v", err)
	}

	// Example 2: Configure using media configuration (alternative approach)
	mediaConfig := &media.Config{
		RTPPortMin:      10000,
		RTPPortMax:      20000,
		EnableSRTP:      true,
		RecordingDir:    "/var/lib/siprec/recordings",
		BehindNAT:       true,
		InternalIP:      "192.168.1.100",
		ExternalIP:      "203.0.113.10",
		SIPInternalPort: 5060,
		SIPExternalPort: 5060,
		DefaultVendor:   "google",
	}

	// Create SIP handler configuration
	sipConfig := &sip.Config{
		MaxConcurrentCalls:    1000,
		MediaConfig:           mediaConfig,
		NATConfig:             natConfig, // Explicit NAT config takes precedence
		RedundancyEnabled:     true,
		SessionTimeout:        5 * time.Minute,
		SessionCheckInterval:  30 * time.Second,
		RedundancyStorageType: "memory",
		ShardCount:            64,
	}

	// Create SIP handler with NAT support
	handler, err := sip.NewHandler(logger, sipConfig, nil)
	if err != nil {
		log.Fatalf("Failed to create SIP handler: %v", err)
	}
	handler.STTCallback = func(ctx context.Context, vendor string, reader io.Reader, callUUID string) error {
		return nil
	}

	// Verify NAT rewriter is initialized
	if handler.NATRewriter != nil {
		logger.Info("NAT traversal is enabled and configured")
		logger.WithField("external_ip", handler.NATRewriter.GetExternalIP()).Info("Current external IP")
	} else {
		logger.Info("NAT traversal is disabled")
	}

	// Create custom SIP server
	sipServer := sip.NewCustomSIPServer(logger, handler)

	// Start SIP server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Example of running the server with NAT traversal
	logger.Info("Starting SIPREC server with NAT traversal support...")

	// Start UDP listener
	go func() {
		if err := sipServer.ListenAndServeUDP(ctx, "0.0.0.0:5060"); err != nil {
			logger.WithError(err).Error("UDP server failed")
		}
	}()

	// Start TCP listener
	go func() {
		if err := sipServer.ListenAndServeTCP(ctx, "0.0.0.0:5060"); err != nil {
			logger.WithError(err).Error("TCP server failed")
		}
	}()

	// Example of updating external IP at runtime (useful for dynamic IP scenarios)
	go func() {
		time.Sleep(30 * time.Second)
		if handler.NATRewriter != nil {
			newExternalIP := "203.0.113.20" // Simulate IP change
			handler.NATRewriter.SetExternalIP(newExternalIP)
			logger.WithField("new_external_ip", newExternalIP).Info("Updated external IP")
		}
	}()

	// Run for demonstration
	time.Sleep(2 * time.Minute)

	// Shutdown gracefully
	logger.Info("Shutting down SIPREC server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := handler.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during handler shutdown")
	}

	if err := sipServer.Shutdown(shutdownCtx); err != nil {
		logger.WithError(err).Error("Error during server shutdown")
	}

	logger.Info("SIPREC server shutdown complete")
}

// Example configuration scenarios:

// Scenario 1: Auto-detect external IP using STUN
func autoDetectNATConfig() *sip.NATConfig {
	return &sip.NATConfig{
		BehindNAT:            true,
		InternalIP:           "192.168.1.100",
		ExternalIP:           "", // Empty - will be auto-detected
		InternalPort:         5060,
		ExternalPort:         5060,
		RewriteVia:           true,
		RewriteContact:       true,
		RewriteRecordRoute:   true,
		AutoDetectExternalIP: true,
		STUNServer:           "stun.l.google.com:19302",
		ForceRewrite:         false,
	}
}

// Scenario 2: NAT with port forwarding
func portForwardNATConfig() *sip.NATConfig {
	return &sip.NATConfig{
		BehindNAT:            true,
		InternalIP:           "192.168.1.100",
		ExternalIP:           "203.0.113.10",
		InternalPort:         5060,
		ExternalPort:         15060, // Different external port
		RewriteVia:           true,
		RewriteContact:       true,
		RewriteRecordRoute:   true,
		AutoDetectExternalIP: false,
		STUNServer:           "stun.l.google.com:19302",
		ForceRewrite:         false,
	}
}

// Scenario 3: Force rewrite all private IPs
func forceRewriteNATConfig() *sip.NATConfig {
	return &sip.NATConfig{
		BehindNAT:            true,
		InternalIP:           "192.168.1.100",
		ExternalIP:           "203.0.113.10",
		InternalPort:         5060,
		ExternalPort:         5060,
		RewriteVia:           true,
		RewriteContact:       true,
		RewriteRecordRoute:   true,
		AutoDetectExternalIP: false,
		STUNServer:           "stun.l.google.com:19302",
		ForceRewrite:         true, // Rewrite any RFC1918 IP found
	}
}

// Scenario 4: Disable NAT traversal
func noNATConfig() *sip.NATConfig {
	return &sip.NATConfig{
		BehindNAT:            false, // Disable NAT traversal
		RewriteVia:           false,
		RewriteContact:       false,
		RewriteRecordRoute:   false,
		AutoDetectExternalIP: false,
		ForceRewrite:         false,
	}
}
