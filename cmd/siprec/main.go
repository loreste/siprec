// SIPREC Server - SIP Recording Server
//
// Copyright (C) 2024 SIPREC Server Project
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/config"
	"siprec-server/pkg/encryption"
	http_server "siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/messaging"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/sip"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/stt"
)

var (
	logger       = logrus.New()
	appConfig    *config.Config
	legacyConfig *config.Configuration
	amqpClient   *messaging.AMQPClient
	sttManager   *stt.ProviderManager
	sipHandler   *sip.Handler
	httpServer   *http_server.Server
	startTime    = time.Now() // For tracking uptime

	// Context for graceful shutdown
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// WebSocket and transcription components
	transcriptionSvc *stt.TranscriptionService
	wsHub            *http_server.TranscriptionHub
	wsHandler        *http_server.WebSocketHandler

	// Encryption components
	encryptionManager   encryption.EncryptionManager
	keyRotationService  *encryption.RotationService
)

func main() {
	// Check for special commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "envcheck", "env-check", "--env-check":
			runEnvironmentCheck()
			return
		case "version", "--version", "-v":
			fmt.Printf("SIPREC Server %s\n", getVersion())
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}

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

	// Initialize the root context for graceful shutdown
	rootCtx, rootCancel = context.WithCancel(context.Background())
	defer rootCancel()

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

	// Start custom SIP server
	go startCustomSIPServer(&wg)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigChan
		logger.WithField("signal", sig.String()).Info("Received shutdown signal, cleaning up...")

		// Create a context with timeout for graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		// Cancel the root context to signal shutdown to all goroutines
		rootCancel()

		// Shutdown HTTP server first
		if httpServer != nil {
			logger.Debug("Shutting down HTTP server...")
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				logger.WithError(err).Error("Error shutting down HTTP server")
			} else {
				logger.Info("HTTP server shut down successfully")
			}
		}

		// Shutdown SIP server next (with its own dedicated timeout)
		if sipHandler != nil {
			logger.Debug("Shutting down SIP server...")
			sipShutdownCtx, sipShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer sipShutdownCancel()

			if err := sipHandler.Shutdown(sipShutdownCtx); err != nil {
				logger.WithError(err).Error("Error shutting down SIP server")
			} else {
				logger.Info("SIP server shut down successfully")
			}
		}

		// Disconnect from AMQP
		if amqpClient != nil {
			logger.Debug("Disconnecting from AMQP...")
			amqpClient.Disconnect()
			logger.Info("AMQP disconnected")
		}

		// Shut down WebSocket hub if active
		if wsHub != nil {
			logger.Debug("Shutting down WebSocket hub...")
			// The hub will be shut down through context cancellation
			// Wait a moment for connections to close gracefully
			time.Sleep(500 * time.Millisecond)
			logger.Info("WebSocket hub shut down")
		}

		// Shut down STT providers
		if sttManager != nil {
			logger.Debug("Shutting down STT providers...")
			// STT providers are cleaned up through context cancellation
			logger.Info("STT providers shut down")
		}

		// Stop encryption services
		if keyRotationService != nil {
			logger.Debug("Stopping key rotation service...")
			if err := keyRotationService.Stop(); err != nil {
				logger.WithError(err).Error("Error stopping key rotation service")
			} else {
				logger.Info("Key rotation service stopped")
			}
		}

		// Allow some time for final cleanup to complete
		select {
		case <-shutdownCtx.Done():
			logger.Warn("Global shutdown timed out, forcing exit")
		case <-time.After(500 * time.Millisecond):
			logger.Info("All components shut down successfully")
		}

		logger.Info("Application shut down gracefully")
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

	// Initialize metrics
	logger.Info("Initializing metrics...")
	metrics.Init(logger)
	metrics.SetMetricsEnabled(appConfig.HTTP.EnableMetrics)
	logger.Info("Metrics initialized")

	// Initialize encryption manager
	logger.Info("About to initialize encryption...")
	if err := initializeEncryption(); err != nil {
		logger.WithError(err).Error("Failed to initialize encryption")
		return fmt.Errorf("failed to initialize encryption: %w", err)
	}
	logger.Info("Encryption initialization completed")

	// Initialize AMQP client with robust error handling
	if appConfig.Messaging.AMQPUrl != "" && appConfig.Messaging.AMQPQueueName != "" {
		// Create AMQP client in a separate goroutine with timeout
		// This ensures that AMQP issues don't block server startup
		logger.Info("Initializing AMQP client")

		amqpConnectChan := make(chan struct {
			client *messaging.AMQPClient
			err    error
		}, 1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("recover", r).Error("Recovered from panic in AMQP initialization")
					amqpConnectChan <- struct {
						client *messaging.AMQPClient
						err    error
					}{nil, fmt.Errorf("panic during AMQP initialization: %v", r)}
				}
			}()

			amqpConfig := messaging.AMQPConfig{
				URL:          appConfig.Messaging.AMQPUrl,
				QueueName:    appConfig.Messaging.AMQPQueueName,
				ExchangeName: "",
				RoutingKey:   appConfig.Messaging.AMQPQueueName,
				Durable:      true,
				AutoDelete:   false,
			}
			client := messaging.NewAMQPClient(logger, amqpConfig)
			err := client.Connect()
			amqpConnectChan <- struct {
				client *messaging.AMQPClient
				err    error
			}{client, err}
		}()

		// Wait for AMQP connection with timeout
		select {
		case result := <-amqpConnectChan:
			if result.err != nil {
				logger.WithError(result.err).Warn("Failed to connect to AMQP server, continuing without AMQP")
			} else {
				amqpClient = result.client
				logger.Info("AMQP client initialized successfully")
			}
		case <-time.After(5 * time.Second):
			logger.Warn("AMQP initialization timed out after 5 seconds, continuing without AMQP")
		}
	} else {
		logger.Warn("AMQP not configured, transcriptions will not be sent to message queue")
	}

	// Initialize speech-to-text providers
	sttManager = stt.NewProviderManager(logger, appConfig.STT.DefaultVendor)

	// Register Mock provider for local testing
	mockProvider := stt.NewMockProvider(logger)

	// Create transcription service before STT providers
	transcriptionSvc = stt.NewTranscriptionService(logger)

	// Set transcription service for mock provider
	mockProvider.SetTranscriptionService(transcriptionSvc)

	if err := sttManager.RegisterProvider(mockProvider); err != nil {
		logger.WithError(err).Warn("Failed to register Mock Speech-to-Text provider")
	}

	// Register providers based on configuration
	for _, vendor := range appConfig.STT.SupportedVendors {
		switch vendor {
		case "google":
			googleProvider := stt.NewGoogleProvider(logger, transcriptionSvc)
			if err := sttManager.RegisterProvider(googleProvider); err != nil {
				logger.WithError(err).Warn("Failed to register Google Speech-to-Text provider")
			}
		case "deepgram":
			// For now, Deepgram doesn't support transcription service
			deepgramProvider := stt.NewDeepgramProvider(logger)
			if err := sttManager.RegisterProvider(deepgramProvider); err != nil {
				logger.WithError(err).Warn("Failed to register Deepgram provider")
			}
		case "openai":
			// For now, OpenAI doesn't support transcription service
			openaiProvider := stt.NewOpenAIProvider(logger)
			if err := sttManager.RegisterProvider(openaiProvider); err != nil {
				logger.WithError(err).Warn("Failed to register OpenAI provider")
			}
		default:
			logger.WithField("vendor", vendor).Warn("Unknown STT vendor in configuration")
		}
	}

	// Initialize the RTP port manager
	media.InitPortManager(appConfig.Network.RTPPortMin, appConfig.Network.RTPPortMax)
	logger.WithFields(logrus.Fields{
		"min_port": appConfig.Network.RTPPortMin,
		"max_port": appConfig.Network.RTPPortMax,
	}).Info("Initialized RTP port manager")

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
		// Initialize audio processing configuration with sensible defaults
		AudioProcessing: media.AudioProcessingConfig{
			Enabled:              true, // Force enable for testing
			EnableVAD:            true,
			VADThreshold:         0.02,
			VADHoldTimeMs:        400,
			EnableNoiseReduction: true,
			NoiseReductionLevel:  0.01,
			ChannelCount:         1,
			MixChannels:          true,
		},
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

	// Configure HTTP server with component references for health checks
	httpServer.SetSIPHandler(sipHandler)
	httpServer.SetAMQPClient(amqpClient)

	// Create session handler and register HTTP handlers
	sessionHandler := http_server.NewSessionHandler(logger, sipAdapter)
	sessionHandler.RegisterHandlers(httpServer)

	// Initialize WebSocket components

	// Create the WebSocket hub and start it in a goroutine
	wsHub = http_server.NewTranscriptionHub(logger)
	go wsHub.Run(rootCtx)

	// Configure WebSocket hub in HTTP server
	httpServer.SetWebSocketHub(wsHub)

	// Create a bridge between transcription service and WebSocket hub
	wsBridge := stt.NewWebSocketTranscriptionBridge(logger, wsHub)
	transcriptionSvc.AddListener(wsBridge)

	// Create and register WebSocket handler
	wsHandler = http_server.NewWebSocketHandler(logger, wsHub)
	wsHandler.RegisterHandlers(httpServer)

	logger.Info("WebSocket real-time transcription streaming initialized")

	// Register AMQP transcription listener if AMQP is configured
	if amqpClient != nil && amqpClient.IsConnected() {
		amqpListener := messaging.NewAMQPTranscriptionListener(logger, amqpClient)
		transcriptionSvc.AddListener(amqpListener)
		logger.Info("AMQP transcription listener registered - transcriptions will be sent to message queue")
	} else {
		logger.Warn("AMQP not connected, transcriptions will not be sent to message queue")
	}

	// Log configuration on startup
	logStartupConfig()

	return nil
}

// startCustomSIPServer initializes and starts the custom SIP server
func startCustomSIPServer(wg *sync.WaitGroup) {
	defer wg.Done()

	ip := "0.0.0.0" // Listen on all interfaces
	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	// Create custom SIP server
	customServer := sip.NewCustomSIPServer(logger, sipHandler)

	// Create error channel to communicate errors from listener goroutines
	errChan := make(chan error, 10)

	// Keep track of active listeners
	var wgListeners sync.WaitGroup

	// Start UDP listeners
	for _, port := range appConfig.Network.GetUDPPorts() {
		address := fmt.Sprintf("%s:%d", ip, port)
		wgListeners.Add(1)

		go func(address string, port int) {
			defer wgListeners.Done()

			logger.WithField("address", address).Info("Starting custom SIP server on UDP")
			if err := customServer.ListenAndServeUDP(ctx, address); err != nil {
				logger.WithError(err).WithField("port", port).Error("Failed to start custom SIP server on UDP")
				errChan <- fmt.Errorf("UDP listener error: %w", err)
				return
			}
		}(address, port)
	}

	// Start TCP listeners
	for _, port := range appConfig.Network.GetTCPPorts() {
		address := fmt.Sprintf("%s:%d", ip, port)
		wgListeners.Add(1)

		go func(address string, port int) {
			defer wgListeners.Done()

			logger.WithField("address", address).Info("Starting custom SIP server on TCP")
			if err := customServer.ListenAndServeTCP(ctx, address); err != nil {
				logger.WithError(err).WithField("port", port).Error("Failed to start custom SIP server on TCP")
				errChan <- fmt.Errorf("TCP listener error: %w", err)
				return
			}
		}(address, port)
	}

	// Check if TLS is enabled
	if appConfig.Network.EnableTLS && appConfig.Network.TLSPort != 0 {
		tlsAddress := fmt.Sprintf("%s:%d", ip, appConfig.Network.TLSPort)

		// Verify TLS certificate and key files exist
		if appConfig.Network.TLSCertFile == "" || appConfig.Network.TLSKeyFile == "" {
			logger.Warn("TLS is enabled but certificate or key file is not specified, TLS will not be started")
			return
		}

		// Get absolute paths for certificate files
		certPath, _ := filepath.Abs(appConfig.Network.TLSCertFile)
		keyPath, _ := filepath.Abs(appConfig.Network.TLSKeyFile)

		logger.WithFields(logrus.Fields{
			"cert_path": certPath,
			"key_path":  keyPath,
		}).Debug("TLS certificate file paths")

		// Check if certificate and key files exist
		if _, err := os.Stat(appConfig.Network.TLSCertFile); os.IsNotExist(err) {
			logger.WithField("cert_file", appConfig.Network.TLSCertFile).Error("TLS certificate file does not exist, TLS will not be started")
			return
		}

		if _, err := os.Stat(appConfig.Network.TLSKeyFile); os.IsNotExist(err) {
			logger.WithField("key_file", appConfig.Network.TLSKeyFile).Error("TLS key file does not exist, TLS will not be started")
			return
		}

		// Load and validate TLS certificate
		cert, err := tls.LoadX509KeyPair(appConfig.Network.TLSCertFile, appConfig.Network.TLSKeyFile)
		if err != nil {
			logger.WithError(err).Error("Failed to load TLS certificate and key, TLS will not be started")
			return
		}

		// Set up TLS configuration using sipgo's utility function or manual config
		tlsConfig := &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}

		logger.WithFields(logrus.Fields{
			"address": tlsAddress,
			"port":    appConfig.Network.TLSPort,
		}).Info("Starting SIP server on TLS")

		// Start TLS server in a separate goroutine
		wgListeners.Add(1)
		go func() {
			defer wgListeners.Done()

			// Start custom TLS server
			if err := customServer.ListenAndServeTLS(
				ctx,
				tlsAddress,
				tlsConfig,
			); err != nil {
				logger.WithError(err).WithField("port", appConfig.Network.TLSPort).Error("Failed to start custom SIP server on TLS")
				errChan <- fmt.Errorf("TLS listener error: %w", err)
				return
			}

			logger.WithField("port", appConfig.Network.TLSPort).Info("Custom SIP server started on TLS successfully")
		}()

		// Verify TLS server started successfully by checking if the port is listening
		// Allow time for the server to start
		go func() {
			time.Sleep(1 * time.Second)

			dialer := &net.Dialer{
				Timeout: 2 * time.Second,
			}

			conn, err := dialer.DialContext(ctx, "tcp", tlsAddress)
			if err != nil {
				logger.WithError(err).Warn("TLS port check failed - port does not appear to be listening")
			} else {
				conn.Close()
				logger.WithField("port", appConfig.Network.TLSPort).Info("TLS port verified to be listening")
			}
		}()
	}

	// Keep server running until an error occurs or context is cancelled
	select {
	case err := <-errChan:
		logger.WithError(err).Error("SIP server error, shutting down")
		cancel()
	case <-ctx.Done():
		logger.Info("SIP server context cancelled, shutting down")
	}

	// Shutdown custom server
	if err := customServer.Shutdown(ctx); err != nil {
		logger.WithError(err).Error("Error shutting down custom SIP server")
	}

	// Wait for all listener goroutines to exit
	wgListeners.Wait()
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
			"tls_port": appConfig.Network.TLSPort,
			"tls_cert": appConfig.Network.TLSCertFile,
			"tls_key":  appConfig.Network.TLSKeyFile,
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
		"srtp_enabled":           appConfig.Network.EnableSRTP,
		"rtp_port_range":         fmt.Sprintf("%d-%d", appConfig.Network.RTPPortMin, appConfig.Network.RTPPortMax),
		"recording_dir":          appConfig.Recording.Directory,
		"recording_max_duration": appConfig.Recording.MaxDuration,
		"recording_cleanup_days": appConfig.Recording.CleanupDays,
	}).Info("Media configuration")

	// Audio processing configuration
	logger.WithFields(logrus.Fields{
		"audio_processing_enabled": true, // Forced on for testing
		"vad_enabled":              true,
		"noise_reduction_enabled":  true,
		"channel_count":            1,
		"mix_channels":             true,
	}).Info("Audio processing configuration")

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

// initializeEncryption initializes the encryption subsystem
func initializeEncryption() error {
	logger.Info("Initializing encryption subsystem")

	// Convert config to encryption config
	encConfig := &encryption.EncryptionConfig{
		EnableRecordingEncryption: appConfig.Encryption.EnableRecordingEncryption,
		EnableMetadataEncryption:  appConfig.Encryption.EnableMetadataEncryption,
		Algorithm:                 appConfig.Encryption.Algorithm,
		KeyDerivationMethod:       appConfig.Encryption.KeyDerivationMethod,
		MasterKeyPath:            appConfig.Encryption.MasterKeyPath,
		KeyRotationInterval:      appConfig.Encryption.KeyRotationInterval,
		KeyBackupEnabled:         appConfig.Encryption.KeyBackupEnabled,
		KeySize:                  appConfig.Encryption.KeySize,
		NonceSize:                appConfig.Encryption.NonceSize,
		SaltSize:                 appConfig.Encryption.SaltSize,
		PBKDF2Iterations:         appConfig.Encryption.PBKDF2Iterations,
		EncryptionKeyStore:       appConfig.Encryption.EncryptionKeyStore,
	}

	// Create key store
	var keyStore encryption.KeyStore
	var err error

	switch encConfig.EncryptionKeyStore {
	case "file":
		keyStore, err = encryption.NewFileKeyStore(encConfig.MasterKeyPath, logger)
		if err != nil {
			return fmt.Errorf("failed to create file key store: %w", err)
		}
	case "memory":
		keyStore = encryption.NewMemoryKeyStore()
	default:
		return fmt.Errorf("unsupported key store type: %s", encConfig.EncryptionKeyStore)
	}

	// Create encryption manager
	encryptionManager, err = encryption.NewManager(encConfig, keyStore, logger)
	if err != nil {
		return fmt.Errorf("failed to create encryption manager: %w", err)
	}

	// Create key rotation service if encryption is enabled
	if encConfig.EnableRecordingEncryption || encConfig.EnableMetadataEncryption {
		keyRotationService = encryption.NewRotationService(encryptionManager, encConfig, logger)
		
		// Start key rotation service
		if err := keyRotationService.Start(); err != nil {
			return fmt.Errorf("failed to start key rotation service: %w", err)
		}
	}

	// Note: EncryptedSessionManager is not implemented yet
	// For now, we'll use the regular session manager
	_ = siprec.GetGlobalSessionManager()

	logger.WithFields(logrus.Fields{
		"recording_encryption": encConfig.EnableRecordingEncryption,
		"metadata_encryption":  encConfig.EnableMetadataEncryption,
		"algorithm":           encConfig.Algorithm,
		"key_store":           encConfig.EncryptionKeyStore,
		"rotation_enabled":    keyRotationService != nil,
	}).Info("Encryption subsystem initialized")

	return nil
}

// runEnvironmentCheck performs environment validation
func runEnvironmentCheck() {
	fmt.Println("SIPREC Server Environment Check")
	fmt.Println("==============================")
	
	// Check if config can be loaded
	cfg, err := config.Load(logger)
	if err != nil {
		fmt.Printf("❌ Configuration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Configuration: Valid")
	
	// Check network ports
	for _, port := range cfg.Network.Ports {
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Printf("❌ Port %d: %v\n", port, err)
		} else {
			ln.Close()
			fmt.Printf("✅ Port %d: Available\n", port)
		}
	}
	
	fmt.Println("Environment check completed")
}

// getVersion returns the current version
func getVersion() string {
	// Try to read VERSION file
	if data, err := os.ReadFile("VERSION"); err == nil {
		return string(data)
	}
	return "development"
}

// printUsage prints usage information
func printUsage() {
	fmt.Printf(`SIPREC Server - SIP Recording Server

Usage:
  %s [command]

Commands:
  envcheck    Check environment and configuration
  version     Show version information
  help        Show this help message

If no command is provided, the server will start normally.

Environment Variables:
  SIPREC_CONFIG_FILE    Path to configuration file
  
For more information, see the documentation.
`, os.Args[0])
}
