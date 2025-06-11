package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SessionStore interface defines the contract for session storage backends
type SessionStore interface {
	Store(sessionID string, data *SessionData) error
	Get(sessionID string) (*SessionData, error)
	Update(sessionID string, updates map[string]interface{}) error
	Delete(sessionID string) error
	List(nodeID string) ([]*SessionData, error)
	Exists(sessionID string) (bool, error)
	Extend(sessionID string) error
	ListByCallID(callID string) ([]*SessionData, error)
	GetStats() (*SessionStoreStats, error)
	Cleanup() error
	Health() error
	Close() error
}

// SessionManager manages sessions with failover support
type SessionManager struct {
	store           SessionStore
	backupStore     SessionStore
	logger          *logrus.Logger
	nodeID          string
	config          *ManagerConfig
	activeSessions  map[string]*SessionInfo
	mutex           sync.RWMutex
	heartbeatTicker *time.Ticker
	cleanupTicker   *time.Ticker
	stopChan        chan struct{}
}

// ManagerConfig holds session manager configuration
type ManagerConfig struct {
	NodeID           string
	HeartbeatInterval time.Duration
	CleanupInterval   time.Duration
	SessionTimeout    time.Duration
	EnableFailover    bool
	EnableBackup      bool
}

// SessionInfo represents a session with additional management data
type SessionInfo struct {
	Data         *SessionData
	LastAccessed time.Time
	AccessCount  int64
	CreatedAt    time.Time
}

// NewSessionManager creates a new session manager
func NewSessionManager(store SessionStore, config *ManagerConfig, logger *logrus.Logger) *SessionManager {
	if config == nil {
		config = &ManagerConfig{
			HeartbeatInterval: 30 * time.Second,
			CleanupInterval:   5 * time.Minute,
			SessionTimeout:    1 * time.Hour,
			EnableFailover:    true,
			EnableBackup:      false,
		}
	}

	manager := &SessionManager{
		store:          store,
		logger:         logger,
		nodeID:         config.NodeID,
		config:         config,
		activeSessions: make(map[string]*SessionInfo),
		stopChan:       make(chan struct{}),
	}

	// Start background tasks
	manager.startBackgroundTasks()

	logger.WithFields(logrus.Fields{
		"node_id":            config.NodeID,
		"heartbeat_interval": config.HeartbeatInterval,
		"cleanup_interval":   config.CleanupInterval,
		"session_timeout":    config.SessionTimeout,
	}).Info("Session manager initialized")

	return manager
}

// SetBackupStore sets a backup session store for redundancy
func (sm *SessionManager) SetBackupStore(backupStore SessionStore) {
	sm.backupStore = backupStore
	sm.config.EnableBackup = true
	sm.logger.Info("Backup session store configured")
}

// CreateSession creates a new session
func (sm *SessionManager) CreateSession(sessionID, callID string, metadata map[string]interface{}) error {
	now := time.Now()
	
	sessionData := &SessionData{
		SessionID:     sessionID,
		CallID:        callID,
		Status:        "active",
		StartTime:     now,
		LastUpdate:    now,
		RecordingPath: "",
		Metadata:      metadata,
		NodeID:        sm.nodeID,
	}

	// Store in primary store
	if err := sm.store.Store(sessionID, sessionData); err != nil {
		return fmt.Errorf("failed to store session in primary store: %w", err)
	}

	// Store in backup store if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		if err := sm.backupStore.Store(sessionID, sessionData); err != nil {
			sm.logger.WithError(err).Warning("Failed to store session in backup store")
		}
	}

	// Track locally
	sm.mutex.Lock()
	sm.activeSessions[sessionID] = &SessionInfo{
		Data:         sessionData,
		LastAccessed: now,
		AccessCount:  1,
		CreatedAt:    now,
	}
	sm.mutex.Unlock()

	sm.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"call_id":    callID,
		"node_id":    sm.nodeID,
	}).Info("Session created")

	return nil
}

// GetSession retrieves a session
func (sm *SessionManager) GetSession(sessionID string) (*SessionData, error) {
	// Check local cache first
	sm.mutex.RLock()
	if sessionInfo, exists := sm.activeSessions[sessionID]; exists {
		sessionInfo.LastAccessed = time.Now()
		sessionInfo.AccessCount++
		data := sessionInfo.Data
		sm.mutex.RUnlock()
		return data, nil
	}
	sm.mutex.RUnlock()

	// Try primary store
	data, err := sm.store.Get(sessionID)
	if err == nil {
		// Update local cache
		sm.mutex.Lock()
		sm.activeSessions[sessionID] = &SessionInfo{
			Data:         data,
			LastAccessed: time.Now(),
			AccessCount:  1,
			CreatedAt:    data.StartTime,
		}
		sm.mutex.Unlock()
		return data, nil
	}

	// Try backup store if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		data, backupErr := sm.backupStore.Get(sessionID)
		if backupErr == nil {
			sm.logger.WithField("session_id", sessionID).Info("Session recovered from backup store")
			
			// Restore to primary store
			if storeErr := sm.store.Store(sessionID, data); storeErr != nil {
				sm.logger.WithError(storeErr).Warning("Failed to restore session to primary store")
			}

			// Update local cache
			sm.mutex.Lock()
			sm.activeSessions[sessionID] = &SessionInfo{
				Data:         data,
				LastAccessed: time.Now(),
				AccessCount:  1,
				CreatedAt:    data.StartTime,
			}
			sm.mutex.Unlock()
			
			return data, nil
		}
	}

	return nil, err
}

// UpdateSession updates a session
func (sm *SessionManager) UpdateSession(sessionID string, updates map[string]interface{}) error {
	// Update in primary store
	if err := sm.store.Update(sessionID, updates); err != nil {
		return fmt.Errorf("failed to update session in primary store: %w", err)
	}

	// Update in backup store if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		if err := sm.backupStore.Update(sessionID, updates); err != nil {
			sm.logger.WithError(err).Warning("Failed to update session in backup store")
		}
	}

	// Update local cache
	sm.mutex.Lock()
	if sessionInfo, exists := sm.activeSessions[sessionID]; exists {
		sessionInfo.LastAccessed = time.Now()
		sessionInfo.AccessCount++
		
		// Apply updates to cached data
		for key, value := range updates {
			switch key {
			case "status":
				if status, ok := value.(string); ok {
					sessionInfo.Data.Status = status
				}
			case "recording_path":
				if path, ok := value.(string); ok {
					sessionInfo.Data.RecordingPath = path
				}
			default:
				if sessionInfo.Data.Metadata == nil {
					sessionInfo.Data.Metadata = make(map[string]interface{})
				}
				sessionInfo.Data.Metadata[key] = value
			}
		}
		sessionInfo.Data.LastUpdate = time.Now()
	}
	sm.mutex.Unlock()

	sm.logger.WithField("session_id", sessionID).Debug("Session updated")
	return nil
}

// DeleteSession removes a session
func (sm *SessionManager) DeleteSession(sessionID string) error {
	// Delete from primary store
	if err := sm.store.Delete(sessionID); err != nil {
		return fmt.Errorf("failed to delete session from primary store: %w", err)
	}

	// Delete from backup store if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		if err := sm.backupStore.Delete(sessionID); err != nil {
			sm.logger.WithError(err).Warning("Failed to delete session from backup store")
		}
	}

	// Remove from local cache
	sm.mutex.Lock()
	delete(sm.activeSessions, sessionID)
	sm.mutex.Unlock()

	sm.logger.WithField("session_id", sessionID).Info("Session deleted")
	return nil
}

// ListSessions returns all sessions
func (sm *SessionManager) ListSessions(nodeID string) ([]*SessionData, error) {
	return sm.store.List(nodeID)
}

// GetActiveSessionCount returns the number of active sessions on this node
func (sm *SessionManager) GetActiveSessionCount() int {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	return len(sm.activeSessions)
}

// ExtendSession extends the TTL of a session
func (sm *SessionManager) ExtendSession(sessionID string) error {
	// Extend in primary store
	if err := sm.store.Extend(sessionID); err != nil {
		return err
	}

	// Extend in backup store if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		if err := sm.backupStore.Extend(sessionID); err != nil {
			sm.logger.WithError(err).Warning("Failed to extend session in backup store")
		}
	}

	// Update local cache
	sm.mutex.Lock()
	if sessionInfo, exists := sm.activeSessions[sessionID]; exists {
		sessionInfo.LastAccessed = time.Now()
	}
	sm.mutex.Unlock()

	return nil
}

// GetStats returns session manager statistics
func (sm *SessionManager) GetStats() (*ManagerStats, error) {
	storeStats, err := sm.store.GetStats()
	if err != nil {
		return nil, err
	}

	sm.mutex.RLock()
	localSessions := len(sm.activeSessions)
	sm.mutex.RUnlock()

	stats := &ManagerStats{
		NodeID:               sm.nodeID,
		LocalCachedSessions:  localSessions,
		StoreStats:           storeStats,
		HeartbeatInterval:    sm.config.HeartbeatInterval,
		CleanupInterval:      sm.config.CleanupInterval,
		SessionTimeout:       sm.config.SessionTimeout,
		FailoverEnabled:      sm.config.EnableFailover,
		BackupEnabled:        sm.config.EnableBackup,
		LastChecked:          time.Now(),
	}

	return stats, nil
}

// Health returns the health status of the session manager
func (sm *SessionManager) Health() error {
	// Check primary store health
	if err := sm.store.Health(); err != nil {
		return fmt.Errorf("primary store unhealthy: %w", err)
	}

	// Check backup store health if enabled
	if sm.config.EnableBackup && sm.backupStore != nil {
		if err := sm.backupStore.Health(); err != nil {
			sm.logger.WithError(err).Warning("Backup store unhealthy")
			// Don't fail health check for backup store issues
		}
	}

	return nil
}

// Shutdown gracefully shuts down the session manager
func (sm *SessionManager) Shutdown() error {
	sm.logger.Info("Shutting down session manager")

	// Stop background tasks
	close(sm.stopChan)

	if sm.heartbeatTicker != nil {
		sm.heartbeatTicker.Stop()
	}
	if sm.cleanupTicker != nil {
		sm.cleanupTicker.Stop()
	}

	// Close stores
	var lastErr error
	if err := sm.store.Close(); err != nil {
		sm.logger.WithError(err).Error("Failed to close primary store")
		lastErr = err
	}

	if sm.backupStore != nil {
		if err := sm.backupStore.Close(); err != nil {
			sm.logger.WithError(err).Error("Failed to close backup store")
			lastErr = err
		}
	}

	sm.logger.Info("Session manager shutdown complete")
	return lastErr
}

// Private methods

func (sm *SessionManager) startBackgroundTasks() {
	// Start heartbeat
	sm.heartbeatTicker = time.NewTicker(sm.config.HeartbeatInterval)
	go sm.heartbeatLoop()

	// Start cleanup
	sm.cleanupTicker = time.NewTicker(sm.config.CleanupInterval)
	go sm.cleanupLoop()
}

func (sm *SessionManager) heartbeatLoop() {
	for {
		select {
		case <-sm.heartbeatTicker.C:
			sm.performHeartbeat()
		case <-sm.stopChan:
			return
		}
	}
}

func (sm *SessionManager) cleanupLoop() {
	for {
		select {
		case <-sm.cleanupTicker.C:
			sm.performCleanup()
		case <-sm.stopChan:
			return
		}
	}
}

func (sm *SessionManager) performHeartbeat() {
	// Extend TTL for active sessions
	sm.mutex.RLock()
	sessionIDs := make([]string, 0, len(sm.activeSessions))
	for sessionID := range sm.activeSessions {
		sessionIDs = append(sessionIDs, sessionID)
	}
	sm.mutex.RUnlock()

	for _, sessionID := range sessionIDs {
		if err := sm.ExtendSession(sessionID); err != nil {
			sm.logger.WithError(err).WithField("session_id", sessionID).Warning("Failed to extend session TTL")
		}
	}

	if len(sessionIDs) > 0 {
		sm.logger.WithField("count", len(sessionIDs)).Debug("Extended session TTLs")
	}
}

func (sm *SessionManager) performCleanup() {
	now := time.Now()
	threshold := now.Add(-sm.config.SessionTimeout)

	sm.mutex.Lock()
	var toRemove []string
	for sessionID, sessionInfo := range sm.activeSessions {
		if sessionInfo.LastAccessed.Before(threshold) {
			toRemove = append(toRemove, sessionID)
		}
	}

	for _, sessionID := range toRemove {
		delete(sm.activeSessions, sessionID)
	}
	sm.mutex.Unlock()

	// Cleanup stores
	if err := sm.store.Cleanup(); err != nil {
		sm.logger.WithError(err).Warning("Failed to cleanup primary store")
	}

	if sm.backupStore != nil {
		if err := sm.backupStore.Cleanup(); err != nil {
			sm.logger.WithError(err).Warning("Failed to cleanup backup store")
		}
	}

	if len(toRemove) > 0 {
		sm.logger.WithField("count", len(toRemove)).Info("Cleaned up stale sessions from local cache")
	}
}

// ManagerStats contains session manager statistics
type ManagerStats struct {
	NodeID               string               `json:"node_id"`
	LocalCachedSessions  int                  `json:"local_cached_sessions"`
	StoreStats           *SessionStoreStats   `json:"store_stats"`
	HeartbeatInterval    time.Duration        `json:"heartbeat_interval"`
	CleanupInterval      time.Duration        `json:"cleanup_interval"`
	SessionTimeout       time.Duration        `json:"session_timeout"`
	FailoverEnabled      bool                 `json:"failover_enabled"`
	BackupEnabled        bool                 `json:"backup_enabled"`
	LastChecked          time.Time            `json:"last_checked"`
}