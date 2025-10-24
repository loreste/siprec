package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"siprec-server/pkg/alerting"
	"siprec-server/pkg/auth"
	"siprec-server/pkg/backup"
	"siprec-server/pkg/cdr"
	"siprec-server/pkg/circuitbreaker"
	"siprec-server/pkg/compliance"
	"siprec-server/pkg/config"
	"siprec-server/pkg/database"
	"siprec-server/pkg/elasticsearch"
	"siprec-server/pkg/encryption"
	http_server "siprec-server/pkg/http"
	"siprec-server/pkg/media"
	"siprec-server/pkg/messaging"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/performance"
	"siprec-server/pkg/pii"
	"siprec-server/pkg/realtime/analytics"
	"siprec-server/pkg/security/audit"
	"siprec-server/pkg/session"
	"siprec-server/pkg/sip"
	"siprec-server/pkg/stt"
	"siprec-server/pkg/telemetry/tracing"
	"siprec-server/pkg/warnings"
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

	tracingShutdown     = func(ctx context.Context) error { return nil }
	analyticsDispatcher *analytics.Dispatcher
	dbConn              *database.MySQLDatabase
	dbRepo              *database.Repository
	cdrService          *cdr.CDRService
	gdprService         *compliance.GDPRService
	cbManager           *circuitbreaker.Manager
	perfMonitor         *performance.PerformanceMonitor
	authenticator       *auth.SimpleAuthenticator
	alertManager        *alerting.AlertManager
)

type analyticsAudioListener struct {
	dispatcher *analytics.Dispatcher
	ctx        context.Context
}

func (l *analyticsAudioListener) OnAudioMetrics(callUUID string, metrics media.AudioMetrics) {
	if l == nil || l.dispatcher == nil {
		return
	}
	analyticsMetrics := analytics.AudioMetrics{
		MOS:        metrics.MOS,
		VoiceRatio: metrics.VoiceRatio,
		NoiseFloor: metrics.NoiseFloor,
		PacketLoss: metrics.PacketLoss,
		JitterMs:   metrics.JitterMs,
		Timestamp:  metrics.Timestamp,
	}
	l.dispatcher.HandleAudioMetrics(l.ctx, callUUID, &analyticsMetrics, nil)
}

func (l *analyticsAudioListener) OnAcousticEvent(callUUID string, event media.AcousticEvent) {
	if l == nil || l.dispatcher == nil {
		return
	}
	analyticsEvent := analytics.AcousticEvent{
		Type:       event.Type,
		Confidence: event.Confidence,
		Timestamp:  event.Timestamp,
		Details:    event.Details,
	}
	l.dispatcher.HandleAudioMetrics(l.ctx, callUUID, nil, []analytics.AcousticEvent{analyticsEvent})
}

type amqpTranscriptionEndpoint struct {
	name              string
	client            messaging.AMQPClientInterface
	publishPartial    bool
	publishFinal      bool
	realtimePublisher *messaging.AMQPRealtimePublisher
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

				if endpoint.realtimePublisher != nil && endpoint.realtimePublisher.IsStarted() {
					if err := endpoint.realtimePublisher.Stop(); err != nil {
						logger.WithField("amqp_endpoint", endpoint.name).WithError(err).Warn("Failed to stop realtime AMQP publisher")
					} else {
						logger.WithField("amqp_endpoint", endpoint.name).Info("Realtime AMQP publisher stopped")
					}
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

		if dbConn != nil {
			logger.Debug("Closing database connection...")
			if err := dbConn.Close(); err != nil {
				logger.WithError(err).Error("Error closing database connection")
			} else {
				logger.Info("Database connection closed")
			}
		}

		// Allow some time for final cleanup to complete
		select {
		case <-shutdownCtx.Done():
			logger.Warn("Global shutdown timed out, forcing exit")
		case <-time.After(500 * time.Millisecond):
			logger.Info("All components shut down successfully")
		}

		// Stop performance monitor
		if perfMonitor != nil {
			logger.Debug("Stopping performance monitor...")
			perfMonitor.Stop()
			logger.Info("Performance monitor stopped")
		}

		// Stop alert manager
		if alertManager != nil {
			logger.Debug("Stopping alert manager...")
			alertManager.Stop()
			logger.Info("Alert manager stopped")
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

	applyComplianceModes(appConfig, logger)

	// Apply logging configuration
	if err := appConfig.ApplyLogging(logger); err != nil {
		return fmt.Errorf("failed to apply logging configuration: %w", err)
	}
	logger.WithField("level", logger.GetLevel().String()).Info("Log level set")

	// Initialize metrics system
	metrics.Init(logger)
	logger.Info("Metrics system initialized")
	if appConfig.HTTP.EnableMetrics {
		metrics.InitEnhancedMetrics(logger)
		logger.Info("Enhanced metrics initialized")
	}

	// Initialize circuit breaker manager for STT provider resilience
	cbManager = circuitbreaker.NewManager(logger, circuitbreaker.STTConfig())
	logger.Info("Circuit breaker manager initialized")

	// Initialize performance monitor
	perfConfig := performance.DefaultConfig()
	if appConfig.Performance.MonitorInterval > 0 {
		perfConfig.MonitorInterval = appConfig.Performance.MonitorInterval
	}
	if appConfig.Performance.MemoryLimitMB > 0 {
		perfConfig.MemoryLimitMB = int64(appConfig.Performance.MemoryLimitMB)
	}
	if appConfig.Performance.CPULimit > 0 {
		perfConfig.CPULimit = appConfig.Performance.CPULimit
	}
	perfMonitor = performance.NewPerformanceMonitor(logger, perfConfig)
	perfMonitor.Start()
	logger.Info("Performance monitor started")

	// Initialize authentication system if enabled
	if appConfig.Auth.Enabled {
		authenticator = auth.NewSimpleAuthenticator(
			appConfig.Auth.JWTSecret,
			appConfig.Auth.JWTIssuer,
			appConfig.Auth.TokenExpiry,
			logger,
		)

		// Override default admin password if configured
		if appConfig.Auth.AdminPassword != "" {
			authenticator.AddUser(appConfig.Auth.AdminUsername, appConfig.Auth.AdminPassword, "admin")
			logger.WithField("username", appConfig.Auth.AdminUsername).Info("Admin user configured")
		}

		logger.WithFields(logrus.Fields{
			"jwt_issuer":      appConfig.Auth.JWTIssuer,
			"token_expiry":    appConfig.Auth.TokenExpiry,
			"enable_api_keys": appConfig.Auth.EnableAPIKeys,
		}).Info("Authentication system initialized")
	} else {
		logger.Debug("Authentication disabled")
	}

	// Initialize global warning collector
	warnings.InitGlobalCollector(logger)
	logger.Info("Warning collector initialized")

	// Initialize alert manager if enabled
	if appConfig.Alerting.Enabled {
		alertConfig := alerting.AlertConfig{
			Enabled:            true,
			EvaluationInterval: appConfig.Alerting.EvaluationInterval,
			Rules:              []alerting.AlertRule{},     // No rules configured yet
			Channels:           []alerting.ChannelConfig{}, // No channels configured yet
		}
		alertManager = alerting.NewAlertManager(alertConfig, logger)
		logger.WithField("evaluation_interval", appConfig.Alerting.EvaluationInterval).Info("Alert manager initialized")
	} else {
		logger.Debug("Alert manager disabled")
	}

	shutdownTracing, err := tracing.Init(rootCtx, appConfig.Tracing, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize tracing: %w", err)
	}
	tracingShutdown = shutdownTracing

	if appConfig.Database.Enabled {
		dbConn, dbRepo, err = database.InitializeDatabase(logger)
		if err != nil {
			if errors.Is(err, database.ErrMySQLDisabled) {
				logger.Warn("Database enabled but binary built without MySQL support; continuing without database persistence")
			} else {
				return fmt.Errorf("failed to initialize database: %w", err)
			}
		} else {
			logger.Info("Database connection established")
			cdrService = cdr.NewCDRService(dbRepo, cdr.CDRConfig{}, logger)
			logger.Info("CDR service initialized")
		}
	} else {
		logger.Debug("Database persistence disabled by configuration")
	}

	gdprService = nil
	if appConfig.Compliance.GDPR.Enabled {
		if dbRepo == nil {
			logger.Warn("GDPR tools enabled but database repository unavailable; export/erase APIs disabled")
		} else {
			gdprService = compliance.NewGDPRService(dbRepo, appConfig.Compliance.GDPR.ExportDir, logger)
			logger.WithField("export_dir", appConfig.Compliance.GDPR.ExportDir).Info("GDPR service initialized")
		}
	}

	// Initialize encryption manager
	logger.Info("About to initialize encryption...")
	if err := initializeEncryption(); err != nil {
		logger.WithError(err).Error("Failed to initialize encryption")
		return fmt.Errorf("failed to initialize encryption: %w", err)
	}
	logger.Info("Encryption initialization completed")

	amqpEndpoints = nil

	createRealtimePublisher := func(endpointName string, client messaging.AMQPClientInterface, publishPartial, publishFinal bool) *messaging.AMQPRealtimePublisher {
		if !appConfig.Messaging.EnableRealtimeAMQP || client == nil || !client.IsConnected() {
			return nil
		}

		cfg := &messaging.AMQPRealtimeConfig{
			BatchSize:            appConfig.Messaging.RealtimeBatchSize,
			BatchTimeout:         appConfig.Messaging.RealtimeBatchTimeout,
			QueueSize:            appConfig.Messaging.RealtimeQueueSize,
			EnableBatching:       appConfig.Messaging.RealtimeBatchSize > 1,
			EnableRetries:        appConfig.Messaging.AMQP.MaxRetries > 1,
			MaxRetries:           appConfig.Messaging.AMQP.MaxRetries,
			RetryDelay:           appConfig.Messaging.AMQP.RetryDelay,
			PublishPartial:       publishPartial,
			PublishFinal:         publishFinal,
			PublishSentiment:     appConfig.Messaging.PublishSentimentUpdates,
			PublishKeywords:      appConfig.Messaging.PublishKeywordDetections,
			PublishSpeakerChange: appConfig.Messaging.PublishSpeakerChanges,
			MessageTTL:           appConfig.Messaging.AMQP.MessageTTL,
			EnableCompression:    false,
			IncludeAudioData:     false,
		}

		if cfg.BatchSize <= 0 {
			cfg.BatchSize = 1
		}
		if cfg.QueueSize <= 0 {
			cfg.QueueSize = 1000
		}
		if cfg.BatchTimeout <= 0 {
			cfg.BatchTimeout = time.Second
		}

		if cfg.MaxRetries <= 0 {
			cfg.MaxRetries = 1
		}
		cfg.EnableRetries = cfg.MaxRetries > 1

		if cfg.RetryDelay <= 0 {
			cfg.RetryDelay = 2 * time.Second
		}

		publisher := messaging.NewAMQPRealtimePublisher(logger, client, cfg)
		if err := publisher.Start(); err != nil {
			logger.WithField("amqp_endpoint", endpointName).WithError(err).Warn("Failed to start realtime AMQP publisher")
			return nil
		}

		logger.WithField("amqp_endpoint", endpointName).Info("Realtime AMQP publisher started")
		return publisher
	}

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
		primaryEndpoint := amqpTranscriptionEndpoint{
			name:           "primary",
			client:         amqpClient,
			publishPartial: appConfig.Messaging.PublishPartialTranscripts,
			publishFinal:   appConfig.Messaging.PublishFinalTranscripts,
		}
		primaryEndpoint.realtimePublisher = createRealtimePublisher(primaryEndpoint.name, primaryEndpoint.client, primaryEndpoint.publishPartial, primaryEndpoint.publishFinal)
		amqpEndpoints = append(amqpEndpoints, primaryEndpoint)
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

			endpointConfigPartial := boolWithDefault(endpointCfg.PublishPartial, appConfig.Messaging.PublishPartialTranscripts)
			endpointConfigFinal := boolWithDefault(endpointCfg.PublishFinal, appConfig.Messaging.PublishFinalTranscripts)

			endpoint := amqpTranscriptionEndpoint{
				name:           endpointCfg.Name,
				client:         endpointClient,
				publishPartial: endpointConfigPartial,
				publishFinal:   endpointConfigFinal,
			}

			endpoint.realtimePublisher = createRealtimePublisher(endpoint.name, endpoint.client, endpoint.publishPartial, endpoint.publishFinal)

			amqpEndpoints = append(amqpEndpoints, endpoint)

			endpointLogger.Info("Realtime AMQP endpoint initialized")
		}
	}

	// Initialize speech-to-text providers
	sttManager = stt.NewProviderManager(logger, appConfig.STT.DefaultVendor, appConfig.STT.SupportedVendors)
	if len(appConfig.STT.LanguageRouting) > 0 {
		sttManager.SetLanguageRouting(appConfig.STT.LanguageRouting)
	}

	// Create transcription service before STT providers
	transcriptionSvc = stt.NewTranscriptionService(logger)
	if sttManager != nil {
		languageListener := stt.NewLanguageRoutingListener(logger, sttManager)
		transcriptionSvc.AddListener(languageListener)
	}

	// Initialize real-time analytics pipeline
	analyticsPipeline := analytics.NewPipeline(logger, nil,
		analytics.NewSentimentProcessor(),
		analytics.NewKeywordProcessor([]string{"the", "and", "you", "uh", "um"}),
		analytics.NewComplianceProcessor([]analytics.ComplianceRule{
			{
				ID:          "recording_disclosure",
				Description: "Agent must disclose call recording",
				Severity:    "high",
				Contains:    []string{"recorded"},
			},
		}),
		analytics.NewAgentMetricsProcessor([]string{"agent"}),
	)
	analyticsDispatcher = analytics.NewDispatcher(logger, analyticsPipeline)
	analyticsDispatcher.AddSubscriber(&analytics.SnapshotLogger{})

	if appConfig.Analytics.Enabled {
		esClient, err := elasticsearch.NewClient(elasticsearch.Config{
			Addresses: appConfig.Analytics.Elasticsearch.Addresses,
			Username:  appConfig.Analytics.Elasticsearch.Username,
			Password:  appConfig.Analytics.Elasticsearch.Password,
			Timeout:   appConfig.Analytics.Elasticsearch.Timeout,
		})
		if err != nil {
			logger.WithError(err).Warn("Failed to initialize Elasticsearch analytics client")
		} else {
			writer := analytics.NewElasticsearchSnapshotWriter(esClient, appConfig.Analytics.Elasticsearch.Index)
			analyticsDispatcher.SetSnapshotWriter(writer)
			logger.WithFields(logrus.Fields{
				"addresses": appConfig.Analytics.Elasticsearch.Addresses,
				"index":     appConfig.Analytics.Elasticsearch.Index,
			}).Info("Analytics snapshots will be persisted to Elasticsearch")
		}
	}

	analyticsListener := stt.NewAnalyticsListener(logger, analyticsDispatcher)

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
			piiFilter.AddListener(analyticsListener)
		}
	} else {
		logger.Info("PII detection disabled")
	}

	if piiFilter == nil {
		transcriptionSvc.AddListener(analyticsListener)
	}

	// Log the configured STT vendors
	logger.WithFields(logrus.Fields{
		"vendors": appConfig.STT.SupportedVendors,
		"default": appConfig.STT.DefaultVendor,
	}).Info("Initializing STT providers")

	// Register providers based on configuration with circuit breaker protection
	for _, vendor := range appConfig.STT.SupportedVendors {
		switch vendor {
		case "google":
			if appConfig.STT.Google.Enabled {
				googleProvider := stt.NewGoogleProvider(logger, transcriptionSvc, &appConfig.STT.Google)
				// Wrap with circuit breaker for resilience
				wrappedProvider := stt.NewCircuitBreakerWrapper(googleProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Google Speech-to-Text provider")
				} else {
					logger.WithField("provider", "google").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "deepgram":
			if appConfig.STT.Deepgram.Enabled {
				deepgramProvider := stt.NewDeepgramProvider(logger, transcriptionSvc, &appConfig.STT.Deepgram, sttManager)
				wrappedProvider := stt.NewCircuitBreakerWrapper(deepgramProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Deepgram provider")
				} else {
					logger.WithField("provider", "deepgram").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "azure":
			if appConfig.STT.Azure.Enabled {
				azureProvider := stt.NewAzureSpeechProvider(logger, transcriptionSvc, &appConfig.STT.Azure)
				wrappedProvider := stt.NewCircuitBreakerWrapper(azureProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Azure Speech provider")
				} else {
					logger.WithField("provider", "azure").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "amazon":
			if appConfig.STT.Amazon.Enabled {
				amazonProvider := stt.NewAmazonTranscribeProvider(logger, transcriptionSvc, &appConfig.STT.Amazon)
				wrappedProvider := stt.NewCircuitBreakerWrapper(amazonProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Amazon Transcribe provider")
				} else {
					logger.WithField("provider", "amazon").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "openai":
			if appConfig.STT.OpenAI.Enabled {
				openaiProvider := stt.NewOpenAIProvider(logger, transcriptionSvc, &appConfig.STT.OpenAI)
				wrappedProvider := stt.NewCircuitBreakerWrapper(openaiProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register OpenAI provider")
				} else {
					logger.WithField("provider", "openai").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "speechmatics":
			if appConfig.STT.Speechmatics.Enabled {
				speechmaticsProvider := stt.NewSpeechmaticsProvider(logger, transcriptionSvc, &appConfig.STT.Speechmatics)
				wrappedProvider := stt.NewCircuitBreakerWrapper(speechmaticsProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register Speechmatics provider")
				} else {
					logger.WithField("provider", "speechmatics").Info("Registered STT provider with circuit breaker protection")
				}
			}
		case "elevenlabs":
			if appConfig.STT.ElevenLabs.Enabled {
				elevenProvider := stt.NewElevenLabsProvider(logger, transcriptionSvc, &appConfig.STT.ElevenLabs)
				wrappedProvider := stt.NewCircuitBreakerWrapper(elevenProvider, cbManager, logger, nil)
				if err := sttManager.RegisterProvider(wrappedProvider); err != nil {
					logger.WithError(err).Warn("Failed to register ElevenLabs provider")
				} else {
					logger.WithField("provider", "elevenlabs").Info("Registered STT provider with circuit breaker protection")
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
		RequireSRTP:      appConfig.Network.RequireSRTP,
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
		AudioMetricsListener: &analyticsAudioListener{
			dispatcher: analyticsDispatcher,
			ctx:        rootCtx,
		},
		AudioMetricsInterval: 5 * time.Second,
	}

	// Create SIP handler config
	sipConfig := &sip.Config{
		MaxConcurrentCalls: appConfig.Resources.MaxConcurrentCalls,
		MediaConfig:        mediaConfig,
		SIPPorts:           appConfig.Network.Ports, // Pass SIP ports for dynamic NAT configuration
	}

	// Initialize SIP handler
	sipHandler, err = sip.NewHandler(logger, sipConfig, sttManager)
	if err != nil {
		return fmt.Errorf("failed to create SIP handler: %w", err)
	}

	if cdrService != nil {
		sipHandler.SetCDRService(cdrService)
	}

	if analyticsDispatcher != nil {
		sipHandler.SetAnalyticsDispatcher(analyticsDispatcher)
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

	if appConfig != nil {
		complianceHandler := http_server.NewComplianceHandler(logger, gdprService, appConfig)
		complianceHandler.RegisterHandlers(httpServer)
	}

	// Initialize WebSocket components

	// Create the WebSocket hub and start it in a goroutine
	wsHub = http_server.NewTranscriptionHub(logger)
	go wsHub.Run(rootCtx)

	// Set the WebSocket hub reference for health checks
	httpServer.SetWebSocketHub(wsHub)

	// Setup Analytics WebSocket if analytics is enabled
	if appConfig.Analytics.Enabled && analyticsDispatcher != nil {
		analyticsWSHandler := http_server.NewAnalyticsWebSocketHandler(logger)
		analyticsWSHandler.Start()
		httpServer.SetAnalyticsWebSocketHandler(analyticsWSHandler)

		// Add WebSocket subscriber to analytics dispatcher
		wsSubscriber := analytics.NewWebSocketSubscriber(logger, analyticsWSHandler)
		analyticsDispatcher.AddSubscriber(wsSubscriber)

		logger.Info("Analytics WebSocket endpoint enabled at /ws/analytics")
	}

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

			if endpoint.realtimePublisher != nil && endpoint.realtimePublisher.IsStarted() {
				realtimeListener := messaging.NewRealtimeTranscriptionListener(endpointLogger, endpoint.realtimePublisher)
				if piiFilter != nil {
					piiFilter.AddListener(realtimeListener)
					endpointLogger.Info("Realtime AMQP listener registered with PII filter")
				} else {
					transcriptionSvc.AddListener(realtimeListener)
					endpointLogger.Info("Realtime AMQP listener registered directly")
				}
			} else if appConfig.Messaging.EnableRealtimeAMQP {
				endpointLogger.Debug("Realtime AMQP publisher unavailable; realtime listener not registered")
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

	ip := appConfig.Network.Host // Use configured SIP host or default 0.0.0.0
	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()

	// Create error channel to communicate errors from listener goroutines
	errChan := make(chan error, 10)

	// Keep track of active listeners
	var wgListeners sync.WaitGroup

	requireTLS := appConfig.Network.RequireTLSOnly
	if !requireTLS {
		udpPorts := appConfig.Network.GetUDPPorts()
		tcpPorts := appConfig.Network.GetTCPPorts()

		// Start UDP listeners
		for _, port := range udpPorts {
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
		for _, port := range tcpPorts {
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
	} else {
		logger.Info("TLS-only SIP mode enabled; skipping UDP/TCP listeners")
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
	} else if requireTLS {
		errChan <- fmt.Errorf("TLS-only mode enabled but TLS listener could not be started")
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
		"sip_host":    appConfig.Network.Host,
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

func applyComplianceModes(cfg *config.Config, logger *logrus.Logger) {
	if cfg == nil {
		return
	}

	if cfg.Compliance.PCI.Enabled {
		logger.Info("PCI compliance mode enabled; applying required safeguards")

		if !cfg.PII.Enabled {
			cfg.PII.Enabled = true
			logger.Info("PII detection enabled for PCI compliance")
		}

		requiredTypes := []string{"credit_card", "ssn"}
		typeSet := make(map[string]struct{}, len(cfg.PII.EnabledTypes)+len(requiredTypes))
		normalizedTypes := make([]string, 0, len(cfg.PII.EnabledTypes))

		for _, t := range cfg.PII.EnabledTypes {
			trimmed := strings.ToLower(strings.TrimSpace(t))
			if trimmed == "" {
				continue
			}
			if _, exists := typeSet[trimmed]; exists {
				continue
			}
			typeSet[trimmed] = struct{}{}
			normalizedTypes = append(normalizedTypes, trimmed)
		}

		for _, required := range requiredTypes {
			if _, exists := typeSet[required]; !exists {
				normalizedTypes = append(normalizedTypes, required)
				typeSet[required] = struct{}{}
				logger.WithField("pii_type", required).Info("Added required PII detection type for PCI compliance")
			}
		}

		cfg.PII.EnabledTypes = normalizedTypes

		if !cfg.PII.ApplyToTranscriptions {
			cfg.PII.ApplyToTranscriptions = true
			logger.Info("Enabled transcription redaction for PCI compliance")
		}

		if !cfg.PII.ApplyToRecordings {
			cfg.PII.ApplyToRecordings = true
			logger.Info("Enabled recording redaction markers for PCI compliance")
		}

		if strings.TrimSpace(cfg.PII.RedactionChar) == "" {
			cfg.PII.RedactionChar = "*"
			logger.Info("Set default PII redaction character")
		}

		if !cfg.Encryption.EnableRecordingEncryption {
			cfg.Encryption.EnableRecordingEncryption = true
			logger.Info("Enabled recording encryption for PCI compliance")
		}

		if !cfg.Network.EnableTLS {
			logger.Warn("PCI compliance mode enabled but TLS is disabled; enable TLS to avoid compliance violations")
		} else if !cfg.Network.RequireTLSOnly {
			logger.Warn("PCI compliance mode enabled; consider enabling SIP_REQUIRE_TLS to restrict transport to TLS")
		}
	}

	if cfg.Compliance.Audit.TamperProof {
		writer := compliance.NewAuditChainWriter(cfg.Compliance.Audit.LogPath)
		audit.SetChainWriter(writer)
		logger.WithField("log_path", cfg.Compliance.Audit.LogPath).Info("Tamper-proof audit chain writer enabled")
	}
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
