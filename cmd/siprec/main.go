package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	
	http_server "siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/messaging"
	"siprec-server/pkg/sip"
	"siprec-server/pkg/stt"
	"siprec-server/pkg/util"
)

var (
	logger = logrus.New()
	config *util.Configuration
	amqpClient *messaging.AMQPClient
	sttManager *stt.ProviderManager
	sipHandler *sip.Handler
	httpServer *http_server.Server
	startTime = time.Now() // For tracking uptime
)

func main() {
	// Set up logger with structured JSON logging for production
	logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})
	logger.SetOutput(os.Stdout)
	
	// Create context with cancellation for graceful shutdown
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize everything
	if err := initialize(); err != nil {
		logger.WithError(err).Fatal("Failed to initialize application")
	}

	// Start server
	var wg sync.WaitGroup
	wg.Add(1)
	
	// Start HTTP server for health checks and API
	httpServer.Start()
	logger.Info("HTTP server started")
	
	// Start SIP server
	go startSIPServer(&wg)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.WithField("signal", sig.String()).Info("Received shutdown signal, cleaning up...")
		
		// Create a context with timeout for graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		
		// Cancel the main context to signal shutdown to all goroutines
		cancel()
		
		// Shutdown HTTP server first
		if httpServer != nil {
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				logger.WithError(err).Error("Error shutting down HTTP server")
			}
		}
		
		// Clean up active calls
		if sipHandler != nil {
			sipHandler.CleanupActiveCalls()
		}
		
		// Disconnect from AMQP
		if amqpClient != nil {
			amqpClient.Disconnect()
		}
		
		// Allow some time for cleanup to complete
		select {
		case <-shutdownCtx.Done():
			logger.Warn("Shutdown timed out, forcing exit")
		case <-time.After(2 * time.Second):
			logger.Info("Cleanup complete")
		}
		
		logger.Info("Shutting down gracefully")
		os.Exit(0)
	}()

	// Wait for all server goroutines to complete
	wg.Wait()
}

// initialize loads configuration and initializes all components
func initialize() error {
	var err error
	
	// Load configuration
	config, err = util.LoadConfig(logger)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Set log level from config
	logger.SetLevel(config.LogLevel)
	logger.WithField("level", logger.GetLevel().String()).Info("Log level set")

	// Initialize AMQP client
	if config.AMQPUrl != "" && config.AMQPQueueName != "" {
		amqpClient = messaging.NewAMQPClient(logger, config.AMQPUrl, config.AMQPQueueName)
		if err := amqpClient.Connect(); err != nil {
			logger.WithError(err).Warn("Failed to connect to AMQP server, continuing without AMQP")
		}
	} else {
		logger.Warn("AMQP not configured, transcriptions will not be sent to message queue")
	}

	// Initialize speech-to-text providers
	sttManager = stt.NewProviderManager(logger, config.DefaultVendor)
	
	// Register Mock provider for local testing
	mockProvider := stt.NewMockProvider(logger)
	if err := sttManager.RegisterProvider(mockProvider); err != nil {
		logger.WithError(err).Warn("Failed to register Mock Speech-to-Text provider")
	}
	
	// Register Google provider
	if contains(config.SupportedVendors, "google") {
		googleProvider := stt.NewGoogleProvider(logger)
		if err := sttManager.RegisterProvider(googleProvider); err != nil {
			logger.WithError(err).Warn("Failed to register Google Speech-to-Text provider")
		}
	}
	
	// Register Deepgram provider
	if contains(config.SupportedVendors, "deepgram") {
		deepgramProvider := stt.NewDeepgramProvider(logger)
		if err := sttManager.RegisterProvider(deepgramProvider); err != nil {
			logger.WithError(err).Warn("Failed to register Deepgram provider")
		}
	}
	
	// Register OpenAI provider
	if contains(config.SupportedVendors, "openai") {
		openaiProvider := stt.NewOpenAIProvider(logger)
		if err := sttManager.RegisterProvider(openaiProvider); err != nil {
			logger.WithError(err).Warn("Failed to register OpenAI provider")
		}
	}
	
	// Create the media config
	mediaConfig := &media.Config{
		RTPPortMin:    config.RTPPortMin,
		RTPPortMax:    config.RTPPortMax,
		EnableSRTP:    config.EnableSRTP,
		RecordingDir:  config.RecordingDir,
		BehindNAT:     config.BehindNAT,
		InternalIP:    config.InternalIP,
		ExternalIP:    config.ExternalIP,
		DefaultVendor: config.DefaultVendor,
	}
	
	// Create SIP handler config
	sipConfig := &sip.Config{
		MaxConcurrentCalls: config.MaxConcurrentCalls,
		MediaConfig:        mediaConfig,
	}
	
	// Initialize SIP handler
	sipHandler, err = sip.NewHandler(logger, sipConfig, sttManager.StreamToProvider)
	if err != nil {
		return fmt.Errorf("failed to create SIP handler: %w", err)
	}
	
	// Register SIP handlers
	sipHandler.SetupHandlers()
	
	// Initialize HTTP server
	httpServerConfig := &http_server.Config{
		Port:            config.HTTPPort,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    30 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
	
	// Create HTTP server with SIP handler adapter for metrics
	sipAdapter := http_server.NewSIPHandlerAdapter(logger, sipHandler)
	httpServer = http_server.NewServer(logger, httpServerConfig, sipAdapter)
	
	// Create session handler and register HTTP handlers
	sessionHandler := http_server.NewSessionHandler(logger, sipAdapter)
	sessionHandler.RegisterHandlers(httpServer)

	// Log configuration on startup
	logStartupConfig()

	return nil
}

// startSIPServer initializes and starts the SIP server
func startSIPServer(wg *sync.WaitGroup) {
	defer wg.Done()
	
	ip := "0.0.0.0" // Listen on all interfaces
	
	// Add ports to waitgroup
	for _, port := range config.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		logger.WithField("address", address).Info("Starting SIP server on UDP")
		if err := sipHandler.Server.ListenAndServe(context.Background(), "udp", address); err != nil {
			logger.WithError(err).WithField("port", port).Fatal("Failed to start SIP server on UDP")
		}
		
		logger.WithField("address", address).Info("Starting SIP server on TCP")
		if err := sipHandler.Server.ListenAndServe(context.Background(), "tcp", address); err != nil {
			logger.WithError(err).WithField("port", port).Fatal("Failed to start SIP server on TCP")
		}
	}
	
	// Check if TLS is enabled
	if config.EnableTLS && config.TLSPort != 0 {
		tlsAddress := fmt.Sprintf("%s:%d", ip, config.TLSPort)
		logger.WithField("address", tlsAddress).Info("Starting SIP server on TLS")
		// TODO: Implement TLS support
		logger.Warn("TLS support is not yet implemented")
	}
	
	// Keep server running
	select {}
}

// logStartupConfig logs the current configuration
func logStartupConfig() {
	logger.Info("SIPREC Server is starting with the following configuration:")
	logger.WithFields(logrus.Fields{
		"external_ip": config.ExternalIP,
		"internal_ip": config.InternalIP,
		"sip_ports":   config.Ports,
		"tls_enabled": config.EnableTLS,
	}).Info("Network configuration")
	
	if config.EnableTLS {
		logger.WithFields(logrus.Fields{
			"tls_port":     config.TLSPort,
			"tls_cert":     config.TLSCertFile,
			"tls_key":      config.TLSKeyFile,
		}).Info("TLS configuration")
	}
	
	// Log HTTP configuration
	logger.WithFields(logrus.Fields{
		"http_enabled":       config.HTTPEnabled,
		"http_port":          config.HTTPPort,
		"http_metrics":       config.HTTPEnableMetrics,
		"http_api":           config.HTTPEnableAPI,
	}).Info("HTTP server configuration")
	
	logger.WithFields(logrus.Fields{
		"srtp_enabled":      config.EnableSRTP,
		"rtp_port_range":    fmt.Sprintf("%d-%d", config.RTPPortMin, config.RTPPortMax),
		"recording_dir":     config.RecordingDir,
		"stt_vendor":        config.DefaultVendor,
		"nat_traversal":     config.BehindNAT,
		"supported_codecs":  config.SupportedCodecs,
		"max_calls":         config.MaxConcurrentCalls,
	}).Info("Media and processing configuration")
	
	if config.BehindNAT {
		logger.WithField("stun_servers", config.STUNServers).Info("STUN configuration")
	}
}

// contains checks if a string is in a slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}