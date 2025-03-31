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
	
	"siprec-server/pkg/config"
	http_server "siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/messaging"
	"siprec-server/pkg/sip"
	"siprec-server/pkg/stt"
)

var (
	logger = logrus.New()
	appConfig *config.Config
	legacyConfig *config.Configuration
	amqpClient *messaging.AMQPClient
	sttManager *stt.ProviderManager
	sipHandler *sip.Handler
	httpServer *http_server.Server
	startTime = time.Now() // For tracking uptime
)

func main() {
	// Set up logger with basic configuration (will be updated after config is loaded)
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
	if legacyConfig.HTTPEnabled {
		httpServer.Start()
		logger.Info("HTTP server started")
	} else {
		logger.Info("HTTP server is disabled by configuration")
	}
	
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
	
	// Load new configuration
	appConfig, err = config.Load(logger)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	
	// Convert to legacy config for backward compatibility
	legacyConfig = appConfig.ToLegacyConfig(logger)

	// Apply logging configuration
	if err := appConfig.ApplyLogging(logger); err != nil {
		return fmt.Errorf("failed to apply logging configuration: %w", err)
	}
	logger.WithField("level", logger.GetLevel().String()).Info("Log level set")

	// Initialize AMQP client
	if appConfig.Messaging.AMQPUrl != "" && appConfig.Messaging.AMQPQueueName != "" {
		amqpClient = messaging.NewAMQPClient(logger, appConfig.Messaging.AMQPUrl, appConfig.Messaging.AMQPQueueName)
		if err := amqpClient.Connect(); err != nil {
			logger.WithError(err).Warn("Failed to connect to AMQP server, continuing without AMQP")
		}
	} else {
		logger.Warn("AMQP not configured, transcriptions will not be sent to message queue")
	}

	// Initialize speech-to-text providers
	sttManager = stt.NewProviderManager(logger, appConfig.STT.DefaultVendor)
	
	// Register Mock provider for local testing
	mockProvider := stt.NewMockProvider(logger)
	if err := sttManager.RegisterProvider(mockProvider); err != nil {
		logger.WithError(err).Warn("Failed to register Mock Speech-to-Text provider")
	}
	
	// Register providers based on configuration
	for _, vendor := range appConfig.STT.SupportedVendors {
		switch vendor {
		case "google":
			googleProvider := stt.NewGoogleProvider(logger)
			if err := sttManager.RegisterProvider(googleProvider); err != nil {
				logger.WithError(err).Warn("Failed to register Google Speech-to-Text provider")
			}
		case "deepgram":
			deepgramProvider := stt.NewDeepgramProvider(logger)
			if err := sttManager.RegisterProvider(deepgramProvider); err != nil {
				logger.WithError(err).Warn("Failed to register Deepgram provider")
			}
		case "openai":
			openaiProvider := stt.NewOpenAIProvider(logger)
			if err := sttManager.RegisterProvider(openaiProvider); err != nil {
				logger.WithError(err).Warn("Failed to register OpenAI provider")
			}
		default:
			logger.WithField("vendor", vendor).Warn("Unknown STT vendor in configuration")
		}
	}
	
	// Create the media config
	mediaConfig := &media.Config{
		RTPPortMin:    appConfig.Network.RTPPortMin,
		RTPPortMax:    appConfig.Network.RTPPortMax,
		EnableSRTP:    appConfig.Network.EnableSRTP,
		RecordingDir:  appConfig.Recording.Directory,
		BehindNAT:     appConfig.Network.BehindNAT,
		InternalIP:    appConfig.Network.InternalIP,
		ExternalIP:    appConfig.Network.ExternalIP,
		DefaultVendor: appConfig.STT.DefaultVendor,
	}
	
	// Create SIP handler config
	sipConfig := &sip.Config{
		MaxConcurrentCalls: appConfig.Resources.MaxConcurrentCalls,
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
		Port:            appConfig.HTTP.Port,
		ReadTimeout:     appConfig.HTTP.ReadTimeout,
		WriteTimeout:    appConfig.HTTP.WriteTimeout,
		ShutdownTimeout: 5 * time.Second,
		Enabled:         appConfig.HTTP.Enabled,
		EnableMetrics:   appConfig.HTTP.EnableMetrics,
		EnableAPI:       appConfig.HTTP.EnableAPI,
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
	for _, port := range appConfig.Network.Ports {
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
	if appConfig.Network.EnableTLS && appConfig.Network.TLSPort != 0 {
		tlsAddress := fmt.Sprintf("%s:%d", ip, appConfig.Network.TLSPort)
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
	
	// Network configuration
	logger.WithFields(logrus.Fields{
		"external_ip": appConfig.Network.ExternalIP,
		"internal_ip": appConfig.Network.InternalIP,
		"sip_ports":   appConfig.Network.Ports,
		"tls_enabled": appConfig.Network.EnableTLS,
	}).Info("Network configuration")
	
	if appConfig.Network.EnableTLS {
		logger.WithFields(logrus.Fields{
			"tls_port":     appConfig.Network.TLSPort,
			"tls_cert":     appConfig.Network.TLSCertFile,
			"tls_key":      appConfig.Network.TLSKeyFile,
		}).Info("TLS configuration")
	}
	
	// HTTP configuration
	logger.WithFields(logrus.Fields{
		"http_enabled":       appConfig.HTTP.Enabled,
		"http_port":          appConfig.HTTP.Port,
		"http_metrics":       appConfig.HTTP.EnableMetrics,
		"http_api":           appConfig.HTTP.EnableAPI,
		"http_read_timeout":  appConfig.HTTP.ReadTimeout,
		"http_write_timeout": appConfig.HTTP.WriteTimeout,
	}).Info("HTTP server configuration")
	
	// Media configuration
	logger.WithFields(logrus.Fields{
		"srtp_enabled":      appConfig.Network.EnableSRTP,
		"rtp_port_range":    fmt.Sprintf("%d-%d", appConfig.Network.RTPPortMin, appConfig.Network.RTPPortMax),
		"recording_dir":     appConfig.Recording.Directory,
		"recording_max_duration": appConfig.Recording.MaxDuration,
		"recording_cleanup_days": appConfig.Recording.CleanupDays,
	}).Info("Media configuration")
	
	// STT configuration
	logger.WithFields(logrus.Fields{
		"stt_vendor":        appConfig.STT.DefaultVendor,
		"supported_vendors": appConfig.STT.SupportedVendors,
		"supported_codecs":  appConfig.STT.SupportedCodecs,
	}).Info("Speech-to-text configuration")
	
	// Resource configuration
	logger.WithFields(logrus.Fields{
		"max_calls": appConfig.Resources.MaxConcurrentCalls,
	}).Info("Resource configuration")
	
	// NAT configuration
	if appConfig.Network.BehindNAT {
		logger.WithField("stun_servers", appConfig.Network.STUNServers).Info("STUN configuration")
	}
	
	// Redundancy configuration
	logger.WithFields(logrus.Fields{
		"redundancy_enabled": appConfig.Redundancy.Enabled,
		"session_timeout":    appConfig.Redundancy.SessionTimeout,
		"check_interval":     appConfig.Redundancy.SessionCheckInterval,
		"storage_type":       appConfig.Redundancy.StorageType,
	}).Info("Redundancy configuration")
}