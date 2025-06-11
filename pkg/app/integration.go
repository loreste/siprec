package app

import (
	"context"
	"fmt"
	"sync"

	"siprec-server/pkg/config"
	"siprec-server/pkg/core"
	"siprec-server/pkg/http"
	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
)

// IntegrationManager manages the integration of async STT and config hot-reload
type IntegrationManager struct {
	logger           *logrus.Logger
	asyncSTTProcessor *stt.AsyncSTTProcessor
	hotReloadManager  *config.HotReloadManager
	httpServer        *http.Server
	sttHandlers       *http.STTHandlers
	configHandlers    *http.ConfigHandlers
	mutex             sync.RWMutex
}

// NewIntegrationManager creates a new integration manager
func NewIntegrationManager(logger *logrus.Logger) *IntegrationManager {
	return &IntegrationManager{
		logger: logger,
	}
}

// InitializeAsyncSTT initializes the async STT processing system
func (m *IntegrationManager) InitializeAsyncSTT(providerManager *stt.ProviderManager, sttConfig *stt.AsyncSTTConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Create async STT processor
	processor := stt.NewAsyncSTTProcessor(providerManager, m.logger, sttConfig)
	
	// Start the processor
	if err := processor.Start(); err != nil {
		return fmt.Errorf("failed to start async STT processor: %w", err)
	}
	
	// Register in global registry
	registry := core.GetServiceRegistry()
	registry.SetAsyncSTTProcessor(processor)
	
	// Store reference
	m.asyncSTTProcessor = processor
	
	// Add callbacks for job completion
	processor.AddCallback(m.onSTTJobComplete)
	
	m.logger.Info("Async STT processing system initialized")
	return nil
}

// InitializeConfigHotReload initializes the configuration hot-reload system
func (m *IntegrationManager) InitializeConfigHotReload(configPath string, currentConfig *config.Config) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Create hot-reload manager
	manager, err := config.NewHotReloadManager(configPath, currentConfig, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create hot-reload manager: %w", err)
	}
	
	// Add reload callbacks
	manager.AddCallback(m.onConfigReload)
	
	// Start the manager
	if err := manager.Start(); err != nil {
		return fmt.Errorf("failed to start hot-reload manager: %w", err)
	}
	
	// Register in global registry
	registry := core.GetServiceRegistry()
	registry.SetHotReloadManager(manager)
	
	// Store reference
	m.hotReloadManager = manager
	
	m.logger.Info("Configuration hot-reload system initialized")
	return nil
}

// RegisterHTTPEndpoints registers HTTP endpoints for STT and config management
func (m *IntegrationManager) RegisterHTTPEndpoints(httpServer *http.Server) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.httpServer = httpServer
	
	// Register STT endpoints
	if m.asyncSTTProcessor != nil {
		m.sttHandlers = http.NewSTTHandlers(m.asyncSTTProcessor, m.logger)
		httpServer.RegisterSTTEndpoints(m.sttHandlers)
		m.logger.Info("STT HTTP endpoints registered")
	}
	
	// Register config endpoints
	if m.hotReloadManager != nil {
		m.configHandlers = http.NewConfigHandlers(m.hotReloadManager, m.logger)
		httpServer.RegisterConfigEndpoints(m.configHandlers)
		m.logger.Info("Configuration HTTP endpoints registered")
	}
}

// onSTTJobComplete handles STT job completion events
func (m *IntegrationManager) onSTTJobComplete(job *stt.STTJob) {
	m.logger.WithFields(logrus.Fields{
		"job_id":    job.ID,
		"call_uuid": job.CallUUID,
		"status":    job.Status,
		"duration":  job.ProcessingTime,
	}).Info("STT job completed")
	
	// Here you could add additional processing:
	// - Send notifications
	// - Update database
	// - Trigger webhooks
	// - Update metrics
	
	if job.Status == stt.StatusCompleted && job.Result != nil {
		// Process successful transcription
		m.logger.WithFields(logrus.Fields{
			"job_id":     job.ID,
			"word_count": job.Result.WordCount,
			"confidence": job.Result.Confidence,
			"cost":       job.ActualCost,
		}).Debug("Transcription result received")
		
		// You could emit this to WebSocket clients, AMQP, etc.
	}
}

// onConfigReload handles configuration reload events
func (m *IntegrationManager) onConfigReload(oldConfig, newConfig *config.Config) error {
	m.logger.Info("Processing configuration reload")
	
	// Check for STT-related changes
	if m.asyncSTTProcessor != nil {
		if err := m.handleSTTConfigChanges(oldConfig, newConfig); err != nil {
			return fmt.Errorf("failed to apply STT config changes: %w", err)
		}
	}
	
	// Update logging configuration
	if oldConfig.Logging.Level != newConfig.Logging.Level {
		level, err := logrus.ParseLevel(newConfig.Logging.Level)
		if err == nil {
			m.logger.SetLevel(level)
			m.logger.WithField("new_level", newConfig.Logging.Level).Info("Log level updated")
		}
	}
	
	// Handle other configuration changes...
	
	return nil
}

// handleSTTConfigChanges handles STT-specific configuration changes
func (m *IntegrationManager) handleSTTConfigChanges(oldConfig, newConfig *config.Config) error {
	// This is where you'd handle dynamic STT configuration changes
	// For example: updating worker count, queue size, providers, etc.
	
	m.logger.Debug("Checking for STT configuration changes")
	
	// Note: Some changes might require restarting the STT processor
	// while others can be applied dynamically
	
	return nil
}

// Shutdown gracefully shuts down all integrated systems
func (m *IntegrationManager) Shutdown(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var wg sync.WaitGroup
	errChan := make(chan error, 2)
	
	// Shutdown async STT processor
	if m.asyncSTTProcessor != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.logger.Info("Shutting down async STT processor")
			if err := m.asyncSTTProcessor.Stop(); err != nil {
				errChan <- fmt.Errorf("failed to stop STT processor: %w", err)
			}
		}()
	}
	
	// Shutdown hot-reload manager
	if m.hotReloadManager != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.logger.Info("Shutting down hot-reload manager")
			if err := m.hotReloadManager.Stop(); err != nil {
				errChan <- fmt.Errorf("failed to stop hot-reload manager: %w", err)
			}
		}()
	}
	
	// Wait for shutdown or context timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		close(errChan)
		// Check for any errors
		for err := range errChan {
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// GetHealthStatus returns health status for the integration components
func (m *IntegrationManager) GetHealthStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	status := map[string]interface{}{
		"async_stt_initialized":    m.asyncSTTProcessor != nil,
		"hot_reload_initialized":   m.hotReloadManager != nil,
		"http_endpoints_registered": m.sttHandlers != nil && m.configHandlers != nil,
	}
	
	if m.asyncSTTProcessor != nil {
		metrics := m.asyncSTTProcessor.GetMetrics()
		status["stt_metrics"] = map[string]interface{}{
			"jobs_processed": metrics.JobsProcessed,
			"jobs_failed":    metrics.JobsFailed,
			"queue_size":     metrics.QueueSize,
			"active_workers": metrics.ActiveWorkers,
		}
	}
	
	if m.hotReloadManager != nil {
		status["hot_reload_enabled"] = m.hotReloadManager.IsEnabled()
	}
	
	return status
}