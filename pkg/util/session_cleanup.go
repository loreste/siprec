package util

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SessionCleanupService handles cleanup of stale sessions and resources
type SessionCleanupService struct {
	logger           *logrus.Logger
	resourceManager  *ResourceManager
	cleanupInterval  time.Duration
	sessionTimeout   time.Duration
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	cleanupCallbacks []CleanupCallback
	mutex            sync.RWMutex
}

// CleanupCallback represents a cleanup function
type CleanupCallback func() error

// SessionCleanupConfig holds configuration for the cleanup service
type SessionCleanupConfig struct {
	CleanupInterval time.Duration
	SessionTimeout  time.Duration
}

// NewSessionCleanupService creates a new session cleanup service
func NewSessionCleanupService(logger *logrus.Logger, rm *ResourceManager, config SessionCleanupConfig) *SessionCleanupService {
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 30 * time.Second
	}
	if config.SessionTimeout == 0 {
		config.SessionTimeout = 5 * time.Minute
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &SessionCleanupService{
		logger:          logger,
		resourceManager: rm,
		cleanupInterval: config.CleanupInterval,
		sessionTimeout:  config.SessionTimeout,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start begins the cleanup service
func (scs *SessionCleanupService) Start() {
	scs.wg.Add(1)
	go scs.cleanupLoop()
	scs.logger.WithFields(logrus.Fields{
		"cleanup_interval": scs.cleanupInterval,
		"session_timeout":  scs.sessionTimeout,
	}).Info("Session cleanup service started")
}

// Stop gracefully stops the cleanup service
func (scs *SessionCleanupService) Stop(timeout time.Duration) {
	scs.logger.Info("Stopping session cleanup service")
	scs.cancel()

	done := make(chan struct{})
	go func() {
		scs.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		scs.logger.Info("Session cleanup service stopped")
	case <-time.After(timeout):
		scs.logger.Warning("Session cleanup service stop timed out")
	}
}

// RegisterCleanupCallback registers a callback for custom cleanup
func (scs *SessionCleanupService) RegisterCleanupCallback(callback CleanupCallback) {
	scs.mutex.Lock()
	defer scs.mutex.Unlock()
	scs.cleanupCallbacks = append(scs.cleanupCallbacks, callback)
}

// cleanupLoop runs the main cleanup loop
func (scs *SessionCleanupService) cleanupLoop() {
	defer scs.wg.Done()

	ticker := time.NewTicker(scs.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-scs.ctx.Done():
			return
		case <-ticker.C:
			scs.performCleanup()
		}
	}
}

// performCleanup performs a cleanup cycle
func (scs *SessionCleanupService) performCleanup() {
	start := time.Now()
	totalCleaned := 0

	// Clean up stale resources
	if scs.resourceManager != nil {
		cleaned := scs.resourceManager.CleanupStale(scs.sessionTimeout)
		totalCleaned += cleaned
	}

	// Run custom cleanup callbacks
	scs.mutex.RLock()
	callbacks := make([]CleanupCallback, len(scs.cleanupCallbacks))
	copy(callbacks, scs.cleanupCallbacks)
	scs.mutex.RUnlock()

	for _, callback := range callbacks {
		if err := callback(); err != nil {
			scs.logger.WithError(err).Warning("Cleanup callback failed")
		}
	}

	if totalCleaned > 0 {
		scs.logger.WithFields(logrus.Fields{
			"cleaned_resources": totalCleaned,
			"duration":         time.Since(start),
		}).Info("Cleanup cycle completed")
	}
}

// ForceCleanup immediately runs a cleanup cycle
func (scs *SessionCleanupService) ForceCleanup() {
	scs.performCleanup()
}

// GetStats returns cleanup service statistics
func (scs *SessionCleanupService) GetStats() CleanupStats {
	stats := CleanupStats{
		CleanupInterval: scs.cleanupInterval,
		SessionTimeout:  scs.sessionTimeout,
		IsRunning:      scs.isRunning(),
	}

	scs.mutex.RLock()
	stats.CallbackCount = len(scs.cleanupCallbacks)
	scs.mutex.RUnlock()

	if scs.resourceManager != nil {
		stats.ResourceStats = scs.resourceManager.Stats()
	}

	return stats
}

// isRunning checks if the cleanup service is running
func (scs *SessionCleanupService) isRunning() bool {
	select {
	case <-scs.ctx.Done():
		return false
	default:
		return true
	}
}

// CleanupStats contains cleanup service statistics
type CleanupStats struct {
	CleanupInterval time.Duration `json:"cleanup_interval"`
	SessionTimeout  time.Duration `json:"session_timeout"`
	IsRunning       bool          `json:"is_running"`
	CallbackCount   int           `json:"callback_count"`
	ResourceStats   ResourceStats `json:"resource_stats"`
}

// PanicRecoveryWrapper wraps functions with panic recovery
func PanicRecoveryWrapper(logger *logrus.Logger, fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.WithField("panic", r).Error("Recovered from panic in goroutine")
		}
	}()
	fn()
}

// SafeGoroutine starts a goroutine with panic recovery and resource tracking
func SafeGoroutine(logger *logrus.Logger, rm *ResourceManager, id string, fn func(context.Context)) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.WithFields(logrus.Fields{
					"goroutine_id": id,
					"panic":        r,
				}).Error("Goroutine panicked")
			}
		}()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Register goroutine resource if resource manager is available
		if rm != nil {
			resource := NewGoroutineResource(id, cancel)
			rm.Register(resource)
			defer func() {
				resource.Done()
				rm.Unregister(id)
			}()
		}

		fn(ctx)
	}()
}

// FileCleanupHelper helps with file cleanup
type FileCleanupHelper struct {
	logger *logrus.Logger
	files  map[string]string // id -> path
	mutex  sync.RWMutex
}

// NewFileCleanupHelper creates a new file cleanup helper
func NewFileCleanupHelper(logger *logrus.Logger) *FileCleanupHelper {
	return &FileCleanupHelper{
		logger: logger,
		files:  make(map[string]string),
	}
}

// TrackFile adds a file for cleanup tracking
func (fch *FileCleanupHelper) TrackFile(id, path string) {
	fch.mutex.Lock()
	defer fch.mutex.Unlock()
	fch.files[id] = path
}

// UntrackFile removes a file from cleanup tracking
func (fch *FileCleanupHelper) UntrackFile(id string) {
	fch.mutex.Lock()
	defer fch.mutex.Unlock()
	delete(fch.files, id)
}

// CleanupFile removes a file and untracks it
func (fch *FileCleanupHelper) CleanupFile(id string) error {
	fch.mutex.Lock()
	path, exists := fch.files[id]
	if exists {
		delete(fch.files, id)
	}
	fch.mutex.Unlock()

	if !exists {
		return nil
	}

	if err := RemoveFileWithRetry(path, 3); err != nil {
		fch.logger.WithError(err).WithField("path", path).Warning("Failed to clean up file")
		return err
	}

	return nil
}

// CleanupAllFiles removes all tracked files
func (fch *FileCleanupHelper) CleanupAllFiles() {
	fch.mutex.Lock()
	files := make(map[string]string)
	for id, path := range fch.files {
		files[id] = path
	}
	fch.files = make(map[string]string)
	fch.mutex.Unlock()

	for id, path := range files {
		if err := RemoveFileWithRetry(path, 3); err != nil {
			fch.logger.WithError(err).WithFields(logrus.Fields{
				"id":   id,
				"path": path,
			}).Warning("Failed to clean up file")
		}
	}
}

// GetTrackedFiles returns a copy of tracked files
func (fch *FileCleanupHelper) GetTrackedFiles() map[string]string {
	fch.mutex.RLock()
	defer fch.mutex.RUnlock()
	
	files := make(map[string]string)
	for id, path := range fch.files {
		files[id] = path
	}
	return files
}

// RemoveFileWithRetry removes a file with retry logic
func RemoveFileWithRetry(path string, maxRetries int) error {
	var lastErr error
	
	for i := 0; i < maxRetries; i++ {
		if err := RemoveFile(path); err != nil {
			lastErr = err
			if i < maxRetries-1 {
				time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
				continue
			}
		} else {
			return nil
		}
	}
	
	return lastErr
}

// RemoveFile safely removes a file
func RemoveFile(path string) error {
	return RemoveFileOrDir(path)
}

// RemoveFileOrDir safely removes a file or directory
func RemoveFileOrDir(path string) error {
	return os.RemoveAll(path)
}