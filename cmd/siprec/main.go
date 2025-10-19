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

	"siprec-server/pkg/backup"
	"siprec-server/pkg/config"
	"siprec-server/pkg/encryption"
	http_server "siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/messaging"
	"siprec-server/pkg/pii"
	"siprec-server/pkg/session"
	"siprec-server/pkg/sip"
	"siprec-server/pkg/stt"
	"siprec-server/pkg/telemetry/tracing"
)

var (
	logger        = logrus.New()
	appConfig     *config.Config
	legacyConfig  *config.Configuration
	amqpClient    messaging.AMQPClientInterface
	amqpEndpoints []amqpTranscriptionEndpoint
	sttManager    *stt.ProviderManager
	sipHandler    *sip.Handler
	httpServer    *http_server.Server

	// Context for graceful shutdown
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// WebSocket and transcription components
	transcriptionSvc *stt.TranscriptionService
	wsHub            *http_server.TranscriptionHub
	wsHandler        *http_server.WebSocketHandler

	// Encryption components
	encryptionManager  encryption.EncryptionManager
	keyRotationService *encryption.RotationService

	// PII detection components
	piiDetector *pii.PIIDetector
	piiFilter   *stt.PIITranscriptionFilter

	tracingShutdown = func(ctx context.Context) error { return nil }
)

type amqpTranscriptionEndpoint struct {
	name           string
	client         messaging.AMQPClientInterface
	publishPartial bool
	publishFinal   bool
}

func createRecordingStorage(logger *logrus.Logger, recCfg *config.RecordingConfig, encCfg *config.EncryptionConfig) media.RecordingStorage {
	if recCfg == nil || !recCfg.Storage.Enabled {
		return nil
	}

	storageCfg := backup.StorageConfig{}
	storageCfg.Local = recCfg.Storage.KeepLocal

	backends := 0
	if recCfg.Storage.S3.Enabled {
		storageCfg.S3 = backup.S3Config{
			Enabled:   true,
			Bucket:    recCfg.Storage.S3.Bucket,
			Region:    recCfg.Storage.S3.Region,
			AccessKey: recCfg.Storage.S3.AccessKey,
			SecretKey: recCfg.Storage.S3.SecretKey,
			Prefix:    recCfg.Storage.S3.Prefix,
		}
		backends++
	}

	if recCfg.Storage.GCS.Enabled {
		storageCfg.GCS = backup.GCSConfig{
			Enabled:           true,
			Bucket:            recCfg.Storage.GCS.Bucket,
			ServiceAccountKey: recCfg.Storage.GCS.ServiceAccountKey,
			Prefix:            recCfg.Storage.GCS.Prefix,
		}
		backends++
	}

	if recCfg.Storage.Azure.Enabled {
		storageCfg.Azure = backup.AzureConfig{
			Enabled:   true,
			Account:   recCfg.Storage.Azure.Account,
			Container: recCfg.Storage.Azure.Container,
			AccessKey: recCfg.Storage.Azure.AccessKey,
			Prefix:    recCfg.Storage.Azure.Prefix,
		}
		backends++
	}

	if !storageCfg.Local && backends == 0 {
		logger.Warn("Recording storage enabled but no remote backend configured; disabling upload")
		return nil
	}

	store, err := backup.NewBackupStorage(storageCfg, logger)
	if err != nil {
		logger.WithError(err).Error("Failed to initialize recording storage backend")
		return nil
	}

	if encCfg != nil && !encCfg.EnableRecordingEncryption {
		logger.Warn("Recording storage is enabled without ENABLE_RECORDING_ENCRYPTION; enable encryption to meet compliance requirements")
	}

	return media.NewRecordingStorage(logger, store, recCfg.Storage.KeepLocal)
}

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

	// Start SIP server
	go startSIPServer(&wg)

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

		// Disconnect from AMQP endpoints
		if len(amqpEndpoints) > 0 {
			for _, endpoint := range amqpEndpoints {
				if endpoint.client == nil {
					continue
				}
				logger.WithField("amqp_endpoint", endpoint.name).Debug("Disconnecting from AMQP endpoint...")
				endpoint.client.Disconnect()
				logger.WithField("amqp_endpoint", endpoint.name).Info("AMQP endpoint disconnected")
			}
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := sttManager.Shutdown(shutdownCtx); err != nil {
				logger.WithError(err).Error("Error shutting down STT providers")
			} else {
				logger.Info("STT providers shut down")
			}
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

		shutdownTraceCtx, shutdownTraceCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := tracingShutdown(shutdownTraceCtx); err != nil {
			logger.WithError(err).Warn("Failed to flush tracing spans during shutdown")
		}
		shutdownTraceCancel()

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

	shutdownTracing, err := tracing.Init(rootCtx, appConfig.Tracing, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize tracing: %w", err)
	}
	tracingShutdown = shutdownTracing

	// Initialize encryption manager
	logger.Info("About to initialize encryption...")
	if err := initializeEncryption(); err != nil {
		logger.WithError(err).Error("Failed to initialize encryption")
		return fmt.Errorf("failed to initialize encryption: %w", err)
	}
	logger.Info("Encryption initialization completed")

	amqpEndpoints = nil

	// Initialize AMQP client with robust error handling
	if appConfig.Messaging.AMQPUrl != "" && appConfig.Messaging.AMQPQueueName != "" {
		// Create AMQP client in a separate goroutine with timeout
		// This ensures that AMQP issues don't block server startup
		logger.Info("Initializing AMQP client")

		amqpConnectChan := make(chan struct {
			client messaging.AMQPClientInterface
			err    error
		}, 1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.WithField("recover", r).Error("Recovered from panic in AMQP initialization")
					amqpConnectChan <- struct {
						client messaging.AMQPClientInterface
						err    error
					}{nil, fmt.Errorf("panic during AMQP initialization: %v", r)}
				}
			}()

			// Use enhanced AMQP client with connection pooling and advanced features
			enhancedClient := messaging.NewEnhancedAMQPClient(logger, &appConfig.Messaging.AMQP)
			err := enhancedClient.Connect()

			// If enhanced client fails, fallback to basic client
			if err != nil {
				logger.WithError(err).Warn("Enhanced AMQP client failed, trying basic client")
				amqpConfig := messaging.AMQPConfig{
					URL:          appConfig.Messaging.AMQPUrl,
					QueueName:    appConfig.Messaging.AMQPQueueName,
					ExchangeName: "",
					RoutingKey:   appConfig.Messaging.AMQPQueueName,
					Durable:      true,
					AutoDelete:   false,
					TLSConfig:    convertToMessagingTLS(appConfig.Messaging.AMQP.TLS),
				}
				basicClient := messaging.NewAMQPClient(logger, amqpConfig)
				err = basicClient.Connect()
				if err == nil {
					amqpClient = basicClient
				}
			} else {
				// Wrap enhanced client to be compatible with AMQPClient interface
				amqpClient = &messaging.EnhancedAMQPClientWrapper{EnhancedClient: enhancedClient}
			}
			amqpConnectChan <- struct {
				client messaging.AMQPClientInterface
				err    error
			}{amqpClient, err}
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

	if amqpClient != nil && amqpClient.IsConnected() {
		amqpEndpoints = append(amqpEndpoints, amqpTranscriptionEndpoint{
			name:           "primary",
			client:         amqpClient,
			publishPartial: appConfig.Messaging.PublishPartialTranscripts,
			publishFinal:   appConfig.Messaging.PublishFinalTranscripts,
		})
	}

	if appConfig.Messaging.EnableRealtimeAMQP && len(appConfig.Messaging.RealtimeEndpoints) > 0 {
		logger.WithField("endpoint_count", len(appConfig.Messaging.RealtimeEndpoints)).Info("Initializing additional realtime AMQP endpoints")
		for _, endpointCfg := range appConfig.Messaging.RealtimeEndpoints {
			if !endpointCfg.Enabled {
				continue
			}

			endpointLogger := logger.WithField("amqp_endpoint", endpointCfg.Name)

			var endpointClient messaging.AMQPClientInterface
			if endpointCfg.UseEnhanced {
				cfgCopy := endpointCfg.AMQP
				if endpointCfg.ExchangeName != "" {
					cfgCopy.DefaultExchange = endpointCfg.ExchangeName
				}
				if endpointCfg.RoutingKey != "" {
					cfgCopy.DefaultRoutingKey = endpointCfg.RoutingKey
				}

				enhancedClient := messaging.NewEnhancedAMQPClient(logger, &cfgCopy)
				if err := enhancedClient.Connect(); err != nil {
					endpointLogger.WithError(err).Warn("Failed to connect enhanced AMQP endpoint")
					continue
				}
				endpointClient = &messaging.EnhancedAMQPClientWrapper{EnhancedClient: enhancedClient}
			} else {
				if endpointCfg.URL == "" {
					endpointLogger.Warn("Realtime AMQP endpoint missing URL, skipping")
					continue
				}

				simpleConfig := messaging.AMQPConfig{
					URL:          endpointCfg.URL,
					QueueName:    endpointCfg.QueueName,
					ExchangeName: endpointCfg.ExchangeName,
					RoutingKey:   endpointCfg.RoutingKey,
					Durable:      true,
					AutoDelete:   false,
					TLSConfig:    convertToMessagingTLS(endpointCfg.TLS),
				}

				simpleClient := messaging.NewAMQPClient(logger, simpleConfig)
				if err := simpleClient.Connect(); err != nil {
					endpointLogger.WithError(err).Warn("Failed to connect AMQP endpoint")
					continue
				}
				endpointClient = simpleClient
			}

			if endpointClient == nil || !endpointClient.IsConnected() {
				endpointLogger.Warn("AMQP endpoint is not connected after initialization")
				continue
			}

			amqpEndpoints = append(amqpEndpoints, amqpTranscriptionEndpoint{
				name:           endpointCfg.Name,
				client:         endpointClient,
				publishPartial: boolWithDefault(endpointCfg.PublishPartial, appConfig.Messaging.PublishPartialTranscripts),
				publishFinal:   boolWithDefault(endpointCfg.PublishFinal, appConfig.Messaging.PublishFinalTranscripts),
			})

			endpointLogger.Info("Realtime AMQP endpoint initialized")
		}
	}

	// Initialize speech-to-text providers
	sttManager = stt.NewProviderManager(logger, appConfig.STT.DefaultVendor, appConfig.STT.SupportedVendors)

	// Create transcription service before STT providers
	transcriptionSvc = stt.NewTranscriptionService(logger)

	// Initialize PII detection if enabled
	if appConfig.PII.Enabled {
		logger.Info("Initializing PII detection")
		piiConfig := &pii.Config{
			EnabledTypes:   convertPIITypes(appConfig.PII.EnabledTypes),
			RedactionChar:  appConfig.PII.RedactionChar,
			PreserveFormat: appConfig.PII.PreserveFormat,
			ContextLength:  appConfig.PII.ContextLength,
		}

		var err error
		piiDetector, err = pii.NewPIIDetector(logger, piiConfig)
		if err != nil {
			logger.WithError(err).Error("Failed to initialize PII detector")
			return fmt.Errorf("failed to initialize PII detector: %w", err)
		}

		// Create PII filter for transcriptions
		if appConfig.PII.ApplyToTranscriptions {
			piiFilter = stt.NewPIITranscriptionFilter(logger, piiDetector, true)
			logger.Info("PII transcription filter initialized")
		}
	} else {
		logger.Info("PII detection disabled")
	}

	// Log the configured STT vendors
	logger.WithFields(logrus.Fields{
		"vendors": appConfig.STT.SupportedVendors,
		"default": appConfig.STT.DefaultVendor,
	}).Info("Initializing STT providers")

	// Register providers based on configuration
	for _, vendor := range appConfig.STT.SupportedVendors {
		switch vendor {
		case "google":
			if appConfig.STT.Google.Enabled {
				googleProvider := stt.NewGoogleProvider(logger, transcriptionSvc, &appConfig.STT.Google)
				if err := sttManager.RegisterProvider(googleProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Google Speech-to-Text provider")
				}
			}
		case "deepgram":
			if appConfig.STT.Deepgram.Enabled {
				deepgramProvider := stt.NewDeepgramProvider(logger, transcriptionSvc, &appConfig.STT.Deepgram)
				if err := sttManager.RegisterProvider(deepgramProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Deepgram provider")
				}
			}
		case "azure":
			if appConfig.STT.Azure.Enabled {
				azureProvider := stt.NewAzureSpeechProvider(logger, transcriptionSvc, &appConfig.STT.Azure)
				if err := sttManager.RegisterProvider(azureProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Azure Speech provider")
				}
			}
		case "amazon":
			if appConfig.STT.Amazon.Enabled {
				amazonProvider := stt.NewAmazonTranscribeProvider(logger, transcriptionSvc, &appConfig.STT.Amazon)
				if err := sttManager.RegisterProvider(amazonProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Amazon Transcribe provider")
				}
			}
		case "openai":
			if appConfig.STT.OpenAI.Enabled {
				openaiProvider := stt.NewOpenAIProvider(logger, transcriptionSvc, &appConfig.STT.OpenAI)
				if err := sttManager.RegisterProvider(openaiProvider); err != nil {
					logger.WithError(err).Warn("Failed to register OpenAI provider")
				}
			}
		case "speechmatics":
			if appConfig.STT.Speechmatics.Enabled {
				speechmaticsProvider := stt.NewSpeechmaticsProvider(logger, transcriptionSvc, &appConfig.STT.Speechmatics)
				if err := sttManager.RegisterProvider(speechmaticsProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Speechmatics provider")
				}
			}
		case "elevenlabs":
			if appConfig.STT.ElevenLabs.Enabled {
				elevenProvider := stt.NewElevenLabsProvider(logger, transcriptionSvc, &appConfig.STT.ElevenLabs)
				if err := sttManager.RegisterProvider(elevenProvider); err != nil {
					logger.WithError(err).Warn("Failed to register ElevenLabs provider")
				}
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

	recordingStorage := createRecordingStorage(logger, &appConfig.Recording, &appConfig.Encryption)

	// Create the media config
	mediaConfig := &media.Config{
		RTPPortMin:       appConfig.Network.RTPPortMin,
		RTPPortMax:       appConfig.Network.RTPPortMax,
		EnableSRTP:       appConfig.Network.EnableSRTP,
		RecordingDir:     appConfig.Recording.Directory,
		RecordingStorage: recordingStorage,
		BehindNAT:        appConfig.Network.BehindNAT,
		InternalIP:       appConfig.Network.InternalIP,
		ExternalIP:       appConfig.Network.ExternalIP,
		DefaultVendor:    appConfig.STT.DefaultVendor,
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
		// PII detection configuration
		PIIAudioEnabled: appConfig.PII.Enabled && appConfig.PII.ApplyToRecordings,
	}

	// Create SIP handler config
	sipConfig := &sip.Config{
		MaxConcurrentCalls: appConfig.Resources.MaxConcurrentCalls,
		MediaConfig:        mediaConfig,
		SIPPorts:           appConfig.Network.Ports, // Pass SIP ports for dynamic NAT configuration
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

	// Set the SIP handler reference for health checks
	httpServer.SetSIPHandler(sipHandler)

	// Create session handler and register HTTP handlers
	sessionHandler := http_server.NewSessionHandler(logger, sipAdapter)
	sessionHandler.RegisterHandlers(httpServer)

	// Initialize WebSocket components

	// Create the WebSocket hub and start it in a goroutine
	wsHub = http_server.NewTranscriptionHub(logger)
	go wsHub.Run(rootCtx)

	// Set the WebSocket hub reference for health checks
	httpServer.SetWebSocketHub(wsHub)

	// Create a bridge between transcription service and WebSocket hub
	wsBridge := stt.NewWebSocketTranscriptionBridge(logger, wsHub)

	// Create PII audio transcription bridge if PII is enabled for recordings
	var piiAudioBridge *stt.PIIAudioTranscriptionBridge
	if appConfig.PII.Enabled && appConfig.PII.ApplyToRecordings {
		// Create a function to retrieve RTP forwarders by call UUID
		getRTPForwarder := func(callUUID string) *media.RTPForwarder {
			if sipHandler != nil && sipHandler.ActiveCalls != nil {
				if value, exists := sipHandler.ActiveCalls.Load(callUUID); exists {
					if callData, ok := value.(*sip.CallData); ok && callData != nil {
						return callData.Forwarder
					}
				}
			}
			return nil
		}

		piiAudioBridge = stt.NewPIIAudioTranscriptionBridge(logger, getRTPForwarder, true)
		logger.Info("PII audio transcription bridge initialized")
	}

	// If PII filtering is enabled for transcriptions, route through the filter
	if piiFilter != nil {
		piiFilter.AddListener(wsBridge)

		// Add PII audio bridge if enabled
		if piiAudioBridge != nil {
			piiFilter.AddListener(piiAudioBridge)
			logger.Info("PII audio transcription bridge registered with PII filter")
		}

		transcriptionSvc.AddListener(piiFilter)
		logger.Info("WebSocket transcription bridge registered with PII filter")
	} else {
		transcriptionSvc.AddListener(wsBridge)

		// Add PII audio bridge directly if no PII filter but audio PII is enabled
		if piiAudioBridge != nil {
			transcriptionSvc.AddListener(piiAudioBridge)
			logger.Info("PII audio transcription bridge registered directly")
		}

		logger.Info("WebSocket transcription bridge registered directly")
	}

	// Create and register WebSocket handler
	wsHandler = http_server.NewWebSocketHandler(logger, wsHub)
	wsHandler.RegisterHandlers(httpServer)

	logger.Info("WebSocket real-time transcription streaming initialized")

	// Register pause/resume handlers if enabled
	if appConfig.PauseResume.Enabled {
		pauseResumeService := sip.NewPauseResumeService(sipHandler, logger)
		pauseResumeHandler := http_server.NewPauseResumeHandler(logger, &appConfig.PauseResume, pauseResumeService)
		pauseResumeHandler.RegisterHandlers(httpServer)
		logger.Info("Pause/Resume API handlers registered")
	}

	// Register AMQP transcription listeners for each configured endpoint
	if len(amqpEndpoints) > 0 {
		for _, endpoint := range amqpEndpoints {
			if endpoint.client == nil || !endpoint.client.IsConnected() {
				logger.WithField("amqp_endpoint", endpoint.name).Warn("AMQP endpoint not connected, skipping transcription listener registration")
				continue
			}

			endpointLogger := logger.WithField("amqp_endpoint", endpoint.name)
			baseListener := messaging.NewAMQPTranscriptionListener(endpointLogger, endpoint.client)

			var listener interface {
				OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{})
			} = baseListener

			if !endpoint.publishPartial || !endpoint.publishFinal {
				listener = messaging.NewFilteredTranscriptionListener(baseListener, endpoint.publishPartial, endpoint.publishFinal)
			}

			if piiFilter != nil {
				piiFilter.AddListener(listener)
				endpointLogger.Info("AMQP transcription listener registered with PII filter")
			} else {
				transcriptionSvc.AddListener(listener)
				endpointLogger.Info("AMQP transcription listener registered directly")
			}
		}
	} else {
		logger.Warn("No AMQP endpoints connected; transcriptions will not be delivered via AMQP")
	}

	// Log configuration on startup
	logStartupConfig()

	return nil
}

// startSIPServer initializes and starts the SIP server
func startSIPServer(wg *sync.WaitGroup) {
	defer wg.Done()

	ip := "0.0.0.0" // Listen on all interfaces
	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	// Create error channel to communicate errors from listener goroutines
	errChan := make(chan error, 10)

	// Keep track of active listeners
	var wgListeners sync.WaitGroup

	// Start UDP listeners
	for _, port := range appConfig.Network.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		wgListeners.Add(1)

		go func(address string, port int) {
			defer wgListeners.Done()

			logger.WithField("address", address).Info("Starting SIP server on UDP")
			if err := sipHandler.Server.ListenAndServe(ctx, "udp", address); err != nil {
				logger.WithError(err).WithField("port", port).Error("Failed to start SIP server on UDP")
				errChan <- fmt.Errorf("UDP listener error: %w", err)
				return
			}
		}(address, port)
	}

	// Start TCP listeners
	for _, port := range appConfig.Network.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		wgListeners.Add(1)

		go func(address string, port int) {
			defer wgListeners.Done()

			logger.WithField("address", address).Info("Starting SIP server on TCP")
			if err := sipHandler.Server.ListenAndServe(ctx, "tcp", address); err != nil {
				logger.WithError(err).WithField("port", port).Error("Failed to start SIP server on TCP")
				errChan <- fmt.Errorf("TCP listener error: %w", err)
				return
			}
		}(address, port)
	}

	// Check if TLS can be started
	startTLS := appConfig.Network.EnableTLS && appConfig.Network.TLSPort != 0
	if startTLS {
		tlsAddress := fmt.Sprintf("%s:%d", ip, appConfig.Network.TLSPort)

		// Verify TLS certificate and key files exist
		if appConfig.Network.TLSCertFile == "" || appConfig.Network.TLSKeyFile == "" {
			logger.Warn("TLS is enabled but certificate or key file is not specified, skipping TLS listener")
			startTLS = false
		}

		var cert tls.Certificate

		if startTLS {
			// Get absolute paths for certificate files to make debugging easier
			certPath, _ := filepath.Abs(appConfig.Network.TLSCertFile)
			keyPath, _ := filepath.Abs(appConfig.Network.TLSKeyFile)

			logger.WithFields(logrus.Fields{
				"cert_path": certPath,
				"key_path":  keyPath,
			}).Debug("TLS certificate file paths")

			// Check if certificate and key files exist
			if _, err := os.Stat(appConfig.Network.TLSCertFile); os.IsNotExist(err) {
				logger.WithField("cert_file", appConfig.Network.TLSCertFile).Error("TLS certificate file does not exist, skipping TLS listener")
				startTLS = false
			}

			if startTLS {
				if _, err := os.Stat(appConfig.Network.TLSKeyFile); os.IsNotExist(err) {
					logger.WithField("key_file", appConfig.Network.TLSKeyFile).Error("TLS key file does not exist, skipping TLS listener")
					startTLS = false
				}
			}

			// Load and validate TLS certificate
			if startTLS {
				var err error
				cert, err = tls.LoadX509KeyPair(appConfig.Network.TLSCertFile, appConfig.Network.TLSKeyFile)
				if err != nil {
					logger.WithError(err).Error("Failed to load TLS certificate and key, skipping TLS listener")
					startTLS = false
				}
			}
		}
		if startTLS {
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

				// Use CustomSIPServer TLS method
				if err := sipHandler.Server.ListenAndServeTLS(
					ctx,
					tlsAddress,
					tlsConfig,
				); err != nil {
					logger.WithError(err).WithField("port", appConfig.Network.TLSPort).Error("Failed to start SIP server on TLS")
					errChan <- fmt.Errorf("TLS listener error: %w", err)
					return
				}

				logger.WithField("port", appConfig.Network.TLSPort).Info("SIP server started on TLS successfully")
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
	}

	// Keep server running until an error occurs or context is cancelled
	select {
	case err := <-errChan:
		logger.WithError(err).Error("SIP server error, shutting down")
		cancel()
	case <-ctx.Done():
		logger.Info("SIP server context cancelled, shutting down")
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

func convertToMessagingTLS(cfg config.AMQPTLSConfig) messaging.AMQPTLSConfig {
	return messaging.AMQPTLSConfig{
		Enabled:    cfg.Enabled,
		CertFile:   cfg.CertFile,
		KeyFile:    cfg.KeyFile,
		CAFile:     cfg.CAFile,
		SkipVerify: cfg.SkipVerify,
	}
}

func boolWithDefault(flag *bool, fallback bool) bool {
	if flag == nil {
		return fallback
	}
	return *flag
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
		MasterKeyPath:             appConfig.Encryption.MasterKeyPath,
		KeyRotationInterval:       appConfig.Encryption.KeyRotationInterval,
		KeyBackupEnabled:          appConfig.Encryption.KeyBackupEnabled,
		KeySize:                   appConfig.Encryption.KeySize,
		NonceSize:                 appConfig.Encryption.NonceSize,
		SaltSize:                  appConfig.Encryption.SaltSize,
		PBKDF2Iterations:          appConfig.Encryption.PBKDF2Iterations,
		EncryptionKeyStore:        appConfig.Encryption.EncryptionKeyStore,
	}

	// Create key store
	var keyStore encryption.KeyStore
	var err error

	switch encConfig.EncryptionKeyStore {
	case "file":
		// Create KMS provider first
		kmsProvider, err := encryption.NewLocalKMSProvider(encConfig.MasterKeyPath, logger)
		if err != nil {
			return fmt.Errorf("failed to create KMS provider: %w", err)
		}
		keyStore, err = encryption.NewFileKeyStore(encConfig.MasterKeyPath, kmsProvider, logger)
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

	// Initialize Redis session manager
	_, err = session.InitializeSessionManager(logger)
	if err != nil {
		logger.WithError(err).Warn("Failed to initialize Redis session manager, continuing without Redis")
	}

	logger.WithFields(logrus.Fields{
		"recording_encryption": encConfig.EnableRecordingEncryption,
		"metadata_encryption":  encConfig.EnableMetadataEncryption,
		"algorithm":            encConfig.Algorithm,
		"key_store":            encConfig.EncryptionKeyStore,
		"rotation_enabled":     keyRotationService != nil,
	}).Info("Encryption subsystem initialized")

	return nil
}

// convertPIITypes converts string slice to PIIType slice
func convertPIITypes(types []string) []pii.PIIType {
	var piiTypes []pii.PIIType
	for _, t := range types {
		switch t {
		case "ssn":
			piiTypes = append(piiTypes, pii.PIITypeSSN)
		case "credit_card":
			piiTypes = append(piiTypes, pii.PIITypeCreditCard)
		case "phone":
			piiTypes = append(piiTypes, pii.PIITypePhone)
		case "email":
			piiTypes = append(piiTypes, pii.PIITypeEmail)
		}
	}
	return piiTypes
}
