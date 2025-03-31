package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	
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
	
	// Start health check server explicitly
	startHealthServer()
	logger.Info("Health check server started from main")
	
	// Start SIP server
	go startServer(&wg)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.WithField("signal", sig.String()).Info("Received shutdown signal, cleaning up...")
		
		// Cancel context to signal shutdown to all goroutines
		cancel()
		
		// Clean up active calls
		if sipHandler != nil {
			sipHandler.CleanupActiveCalls()
		}
		
		// Disconnect from AMQP
		if amqpClient != nil {
			amqpClient.Disconnect()
		}
		
		// Allow some time for cleanup to complete
		time.Sleep(3 * time.Second)
		
		logger.Info("Cleanup complete. Shutting down gracefully.")
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

	// Log configuration on startup
	logStartupConfig()

	return nil
}

// startServer initializes and starts the SIP server
func startServer(wg *sync.WaitGroup) {
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
	
	// Start the health check server
	go startHealthServer()
	
	// Keep server running
	select {}
}

// Start a simple health check HTTP server
func startHealthServer() {
	healthPort := 8080
	mux := http.NewServeMux()
	
	logger.Info("Setting up health check endpoints")
	
	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		logger.WithField("endpoint", "/health").Debug("Health endpoint accessed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		response := fmt.Sprintf(`{"status":"ok","uptime":"%s"}`, time.Since(startTime).String())
		_, _ = w.Write([]byte(response))
	})
	
	// Metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		logger.WithField("endpoint", "/metrics").Debug("Metrics endpoint accessed")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		
		// Simple metrics reporting
		activeCalls := 0
		if sipHandler != nil {
			activeCalls = sipHandler.GetActiveCallCount()
		}
		
		metrics := fmt.Sprintf(`# HELP siprec_active_calls Number of active calls
# TYPE siprec_active_calls gauge
siprec_active_calls %d
`, activeCalls)
		
		_, _ = w.Write([]byte(metrics))
	})
	
	// Start the server in a goroutine
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", healthPort),
		Handler: mux,
	}
	
	logger.WithField("port", healthPort).Info("Starting health check server")
	
	// Start serving in a goroutine
	go func() {
		logger.Infof("Health check server listening on port %d", healthPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.WithError(err).Error("Health check server failed")
		}
	}()
	
	// Also verify that we can actually bind to the port
	go func() {
		time.Sleep(1 * time.Second)
		logger.Info("Verifying health check server is running...")
		
		// Try to open a connection to the health port
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", healthPort), 3*time.Second)
		if err != nil {
			logger.WithError(err).Error("Could not connect to health check server")
		} else {
			logger.Info("Health check server is running correctly")
			conn.Close()
		}
	}()
}

// logStartupConfig logs the current configuration
func logStartupConfig() {
	logger.Info("SIP Server is starting with the following configuration:")
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