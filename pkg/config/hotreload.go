package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

// HotReloadManager manages configuration hot-reloading
type HotReloadManager struct {
	configPath   string
	config       *Config
	validator    *ConfigValidator
	logger       *logrus.Logger
	watcher      *fsnotify.Watcher
	callbacks    []ReloadCallback
	mutex        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	reloadChan   chan bool
	enabled      bool
	debounceTime time.Duration
	lastReload   time.Time
}

// ReloadCallback defines a callback function for configuration reloads
type ReloadCallback func(oldConfig, newConfig *Config) error

// ReloadEvent represents a configuration reload event
type ReloadEvent struct {
	Timestamp   time.Time              `json:"timestamp"`
	ConfigPath  string                 `json:"config_path"`
	Success     bool                   `json:"success"`
	Changes     []ConfigChange         `json:"changes,omitempty"`
	Errors      []ValidationError      `json:"errors,omitempty"`
	Warnings    []ValidationWarning    `json:"warnings,omitempty"`
	ReloadTime  time.Duration          `json:"reload_time"`
	TriggerType string                 `json:"trigger_type"` // "file", "api", "signal"
}

// ConfigChange represents a change in configuration
type ConfigChange struct {
	Field    string      `json:"field"`
	OldValue interface{} `json:"old_value"`
	NewValue interface{} `json:"new_value"`
	Type     string      `json:"type"` // "added", "removed", "modified"
}


// NewHotReloadManager creates a new hot-reload manager
func NewHotReloadManager(configPath string, config *Config, logger *logrus.Logger) (*HotReloadManager, error) {
	validator := NewConfigValidator(logger)
	
	ctx, cancel := context.WithCancel(context.Background())

	manager := &HotReloadManager{
		configPath:   configPath,
		config:       config,
		validator:    validator,
		logger:       logger,
		callbacks:    make([]ReloadCallback, 0),
		ctx:          ctx,
		cancel:       cancel,
		reloadChan:   make(chan bool, 10),
		debounceTime: 2 * time.Second,
		lastReload:   time.Now(),
	}

	// Initialize file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}
	manager.watcher = watcher

	return manager, nil
}

// Start starts the hot-reload manager
func (h *HotReloadManager) Start() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if h.enabled {
		return fmt.Errorf("hot-reload manager already started")
	}

	// Add config file to watcher
	if err := h.watcher.Add(h.configPath); err != nil {
		return fmt.Errorf("failed to watch config file: %w", err)
	}

	// Also watch the directory for file creation/deletion
	configDir := filepath.Dir(h.configPath)
	if err := h.watcher.Add(configDir); err != nil {
		h.logger.WithError(err).Warning("Failed to watch config directory")
	}

	h.enabled = true

	// Start file watcher goroutine
	go h.watchFiles()

	// Start reload handler goroutine
	go h.handleReloads()

	h.logger.WithField("config_path", h.configPath).Info("Configuration hot-reload manager started")

	return nil
}

// Stop stops the hot-reload manager
func (h *HotReloadManager) Stop() error {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	if !h.enabled {
		return fmt.Errorf("hot-reload manager not started")
	}

	h.cancel()
	h.watcher.Close()
	close(h.reloadChan)
	h.enabled = false

	h.logger.Info("Configuration hot-reload manager stopped")

	return nil
}

// AddCallback adds a reload callback
func (h *HotReloadManager) AddCallback(callback ReloadCallback) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.callbacks = append(h.callbacks, callback)
}

// TriggerReload manually triggers a configuration reload
func (h *HotReloadManager) TriggerReload() (*ReloadEvent, error) {
	return h.performReload("api")
}

// GetCurrentConfig returns the current configuration (thread-safe)
func (h *HotReloadManager) GetCurrentConfig() *Config {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	// Return a deep copy to prevent external modifications
	return h.copyConfig(h.config)
}

// IsEnabled returns whether hot-reload is enabled
func (h *HotReloadManager) IsEnabled() bool {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return h.enabled
}

// watchFiles watches for file system events
func (h *HotReloadManager) watchFiles() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.WithField("panic", r).Error("File watcher panic recovered")
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			return

		case event, ok := <-h.watcher.Events:
			if !ok {
				return
			}

			h.logger.WithFields(logrus.Fields{
				"event": event.Op.String(),
				"file":  event.Name,
			}).Debug("File system event received")

			// Check if this is our config file
			if event.Name == h.configPath || filepath.Base(event.Name) == filepath.Base(h.configPath) {
				if event.Op&fsnotify.Write == fsnotify.Write || 
				   event.Op&fsnotify.Create == fsnotify.Create ||
				   event.Op&fsnotify.Rename == fsnotify.Rename {
					
					// Debounce rapid file changes
					select {
					case h.reloadChan <- true:
						h.logger.Debug("Configuration reload triggered by file change")
					default:
						h.logger.Debug("Configuration reload already pending")
					}
				}
			}

		case err, ok := <-h.watcher.Errors:
			if !ok {
				return
			}
			h.logger.WithError(err).Error("File watcher error")
		}
	}
}

// handleReloads handles configuration reload events
func (h *HotReloadManager) handleReloads() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.WithField("panic", r).Error("Reload handler panic recovered")
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			return

		case <-h.reloadChan:
			// Debounce rapid changes
			time.Sleep(h.debounceTime)

			// Drain any additional reload requests that came in during debounce
		drainLoop:
			for {
				select {
				case <-h.reloadChan:
					// Keep draining
				default:
					break drainLoop
				}
			}

			// Check if enough time has passed since last reload
			if time.Since(h.lastReload) < h.debounceTime {
				h.logger.Debug("Skipping reload due to debounce")
				continue
			}

			// Perform the reload
			event, err := h.performReload("file")
			if err != nil {
				h.logger.WithError(err).Error("Configuration reload failed")
			} else if event.Success {
				h.logger.WithFields(logrus.Fields{
					"changes":     len(event.Changes),
					"reload_time": event.ReloadTime,
				}).Info("Configuration reloaded successfully")
			}
		}
	}
}

// performReload performs the actual configuration reload
func (h *HotReloadManager) performReload(triggerType string) (*ReloadEvent, error) {
	startTime := time.Now()

	event := &ReloadEvent{
		Timestamp:   startTime,
		ConfigPath:  h.configPath,
		TriggerType: triggerType,
	}

	h.logger.WithField("trigger", triggerType).Info("Starting configuration reload")

	// Load new configuration
	newConfig, err := Load(h.logger)
	if err != nil {
		event.Success = false
		event.ReloadTime = time.Since(startTime)
		h.logger.WithError(err).Error("Failed to load new configuration")
		return event, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate new configuration
	validationResult := h.validator.ValidateConfig(newConfig)
	event.Errors = validationResult.Errors
	event.Warnings = validationResult.Warnings

	if !validationResult.Valid {
		event.Success = false
		event.ReloadTime = time.Since(startTime)
		h.logger.WithField("errors", len(validationResult.Errors)).Error("New configuration validation failed")
		return event, fmt.Errorf("configuration validation failed: %s", validationResult.Summary)
	}

	// Create backup of current config if enabled
	if err := h.backupCurrentConfig(); err != nil {
		h.logger.WithError(err).Warning("Failed to backup current configuration")
	}

	// Detect changes
	changes := h.detectChanges(h.config, newConfig)
	event.Changes = changes

	// Update configuration atomically
	h.mutex.Lock()
	oldConfig := h.config
	h.config = newConfig
	h.lastReload = time.Now()
	h.mutex.Unlock()

	// Execute callbacks
	var callbackErrors []error
	for _, callback := range h.callbacks {
		if err := callback(oldConfig, newConfig); err != nil {
			callbackErrors = append(callbackErrors, err)
			h.logger.WithError(err).Error("Configuration reload callback failed")
		}
	}

	event.Success = len(callbackErrors) == 0
	event.ReloadTime = time.Since(startTime)

	if len(callbackErrors) > 0 {
		return event, fmt.Errorf("some reload callbacks failed")
	}

	h.logger.WithFields(logrus.Fields{
		"changes":     len(changes),
		"reload_time": event.ReloadTime,
		"warnings":    len(validationResult.Warnings),
	}).Info("Configuration reload completed successfully")

	return event, nil
}

// detectChanges detects changes between old and new configuration
func (h *HotReloadManager) detectChanges(oldConfig, newConfig *Config) []ConfigChange {
	changes := make([]ConfigChange, 0)

	// This is a simplified implementation
	// In a real system, you'd use reflection or a more sophisticated diff algorithm

	// Check SIP ports
	if !h.intSlicesEqual(oldConfig.Network.Ports, newConfig.Network.Ports) {
		changes = append(changes, ConfigChange{
			Field:    "sip_ports",
			OldValue: oldConfig.Network.Ports,
			NewValue: newConfig.Network.Ports,
			Type:     "modified",
		})
	}

	// Check HTTP port
	if oldConfig.HTTP.Port != newConfig.HTTP.Port {
		changes = append(changes, ConfigChange{
			Field:    "http_port",
			OldValue: oldConfig.HTTP.Port,
			NewValue: newConfig.HTTP.Port,
			Type:     "modified",
		})
	}

	// Check log level
	if oldConfig.Logging.Level != newConfig.Logging.Level {
		changes = append(changes, ConfigChange{
			Field:    "log_level",
			OldValue: oldConfig.Logging.Level,
			NewValue: newConfig.Logging.Level,
			Type:     "modified",
		})
	}

	// Check STT vendors
	if !h.stringSlicesEqual(oldConfig.STT.SupportedVendors, newConfig.STT.SupportedVendors) {
		changes = append(changes, ConfigChange{
			Field:    "stt_vendors",
			OldValue: oldConfig.STT.SupportedVendors,
			NewValue: newConfig.STT.SupportedVendors,
			Type:     "modified",
		})
	}

	// Check recording directory
	if oldConfig.Recording.Directory != newConfig.Recording.Directory {
		changes = append(changes, ConfigChange{
			Field:    "recording_dir",
			OldValue: oldConfig.Recording.Directory,
			NewValue: newConfig.Recording.Directory,
			Type:     "modified",
		})
	}

	// Check TLS settings
	if oldConfig.Network.EnableTLS != newConfig.Network.EnableTLS {
		changes = append(changes, ConfigChange{
			Field:    "tls_enabled",
			OldValue: oldConfig.Network.EnableTLS,
			NewValue: newConfig.Network.EnableTLS,
			Type:     "modified",
		})
	}

	// Add more field comparisons as needed...

	return changes
}

// backupCurrentConfig creates a backup of the current configuration
func (h *HotReloadManager) backupCurrentConfig() error {
	backupDir := "./config_backups"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("config_backup_%s.yaml", timestamp))

	// Read current config file
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return fmt.Errorf("failed to read current config: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	h.logger.WithField("backup_path", backupPath).Debug("Configuration backup created")

	// Cleanup old backups (keep last 10)
	h.cleanupOldBackups(backupDir, 10)

	return nil
}

// cleanupOldBackups removes old backup files
func (h *HotReloadManager) cleanupOldBackups(backupDir string, keepCount int) {
	files, err := filepath.Glob(filepath.Join(backupDir, "config_backup_*.yaml"))
	if err != nil {
		h.logger.WithError(err).Warning("Failed to list backup files")
		return
	}

	if len(files) <= keepCount {
		return
	}

	// Sort files by name (which includes timestamp)
	// Remove oldest files
	for i := 0; i < len(files)-keepCount; i++ {
		if err := os.Remove(files[i]); err != nil {
			h.logger.WithError(err).WithField("file", files[i]).Warning("Failed to remove old backup")
		}
	}
}

// copyConfig creates a deep copy of configuration
func (h *HotReloadManager) copyConfig(config *Config) *Config {
	// This is a simplified copy - in a real implementation you'd use 
	// reflection or a proper deep copy library
	newConfig := *config

	// Copy slices
	if config.Network.Ports != nil {
		newConfig.Network.Ports = make([]int, len(config.Network.Ports))
		copy(newConfig.Network.Ports, config.Network.Ports)
	}

	if config.STT.SupportedVendors != nil {
		newConfig.STT.SupportedVendors = make([]string, len(config.STT.SupportedVendors))
		copy(newConfig.STT.SupportedVendors, config.STT.SupportedVendors)
	}

	if config.STT.SupportedCodecs != nil {
		newConfig.STT.SupportedCodecs = make([]string, len(config.STT.SupportedCodecs))
		copy(newConfig.STT.SupportedCodecs, config.STT.SupportedCodecs)
	}

	if config.Network.STUNServers != nil {
		newConfig.Network.STUNServers = make([]string, len(config.Network.STUNServers))
		copy(newConfig.Network.STUNServers, config.Network.STUNServers)
	}

	return &newConfig
}

// Helper functions for change detection
func (h *HotReloadManager) intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func (h *HotReloadManager) stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}