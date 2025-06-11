package siprec

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"siprec-server/pkg/util"
)

// SessionManager manages concurrent SIPREC sessions with optimized resource usage
type SessionManager struct {
	sessions     *util.ShardedMap
	cache        *util.StatsCache
	workerPool   *util.WorkerPoolManager
	resourcePool *util.ResourcePool

	// Configuration
	maxSessions     int32
	sessionTimeout  time.Duration
	cleanupInterval time.Duration

	// Statistics
	activeSessions int32
	totalSessions  int64

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SessionManagerConfig configures the session manager
type SessionManagerConfig struct {
	MaxSessions     int
	SessionTimeout  time.Duration
	CleanupInterval time.Duration
	CacheSize       int
	CacheTTL        time.Duration
}

// DefaultSessionManagerConfig returns default configuration
func DefaultSessionManagerConfig() *SessionManagerConfig {
	return &SessionManagerConfig{
		MaxSessions:     1000,
		SessionTimeout:  30 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		CacheSize:       5000,
		CacheTTL:        15 * time.Minute,
	}
}

// NewSessionManager creates a new optimized session manager
func NewSessionManager(config *SessionManagerConfig) *SessionManager {
	if config == nil {
		config = DefaultSessionManagerConfig()
	}

	// Validate configuration and set safe defaults
	if config.SessionTimeout <= 0 {
		config.SessionTimeout = 30 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 5 * time.Minute
	}
	if config.MaxSessions <= 0 {
		config.MaxSessions = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())

	sm := &SessionManager{
		sessions:        util.NewShardedMap(64), // 64 shards for high concurrency
		cache:           util.NewStatsCache(config.CacheSize, config.CacheTTL),
		workerPool:      util.GetGlobalWorkerPoolManager(),
		resourcePool:    util.GetGlobalResourcePool(),
		maxSessions:     int32(config.MaxSessions),
		sessionTimeout:  config.SessionTimeout,
		cleanupInterval: config.CleanupInterval,
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start background tasks
	sm.startBackgroundTasks()

	return sm
}

// CreateSession creates a new recording session with resource optimization
func (sm *SessionManager) CreateSession(sessionID string, metadata *RSMetadata) (*RecordingSession, error) {
	// Check session limits
	current := atomic.LoadInt32(&sm.activeSessions)
	if current >= sm.maxSessions {
		return nil, fmt.Errorf("maximum sessions (%d) reached", sm.maxSessions)
	}

	// Check if session already exists
	if existing, exists := sm.sessions.Load(sessionID); exists {
		if session, ok := existing.(*RecordingSession); ok {
			return session, nil
		}
	}

	// Create new session with optimized initialization
	session := &RecordingSession{
		ID:                 sessionID,
		RecordingState:     "active",
		SequenceNumber:     1,
		AssociatedTime:     time.Now(),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		IsValid:            true,
		PauseResumeAllowed: true,
		Participants:       make([]Participant, 0, 4), // Pre-allocate for typical use
		MediaStreamTypes:   make([]string, 0, 2),      // Pre-allocate for audio/video
	}

	// Set expiration
	SetSessionExpiration(session, sm.sessionTimeout)

	// Process metadata if provided
	if metadata != nil {
		UpdateRecordingSession(session, metadata)
	}

	// Store session
	sm.sessions.Store(sessionID, session)
	sm.cache.Set(sessionID, session)

	// Update statistics
	atomic.AddInt32(&sm.activeSessions, 1)
	atomic.AddInt64(&sm.totalSessions, 1)

	return session, nil
}

// GetSession retrieves a session with caching optimization
func (sm *SessionManager) GetSession(sessionID string) (*RecordingSession, bool) {
	// Try cache first for frequently accessed sessions
	if cached, found := sm.cache.Get(sessionID); found {
		if session, ok := cached.(*RecordingSession); ok {
			// Validate session is still active
			if session.IsValid && !IsSessionExpired(session) {
				return session, true
			}
			// Remove expired/invalid session from cache
			sm.cache.Delete(sessionID)
		}
	}

	// Fall back to main storage
	if stored, exists := sm.sessions.Load(sessionID); exists {
		if session, ok := stored.(*RecordingSession); ok {
			// Validate session
			if !session.IsValid || IsSessionExpired(session) {
				sm.removeSession(sessionID)
				return nil, false
			}

			// Update cache for future access
			sm.cache.Set(sessionID, session)
			return session, true
		}
	}

	return nil, false
}

// UpdateSession updates an existing session with optimized locking
func (sm *SessionManager) UpdateSession(sessionID string, metadata *RSMetadata) error {
	session, exists := sm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Submit update task to worker pool to avoid blocking
	task := func() {
		UpdateRecordingSession(session, metadata)

		// Update cache
		sm.cache.Set(sessionID, session)
	}

	// Use dedicated session pool for session updates
	if !sm.workerPool.SubmitTask("session_updates", task) {
		// Fallback to synchronous update if pool is full
		UpdateRecordingSession(session, metadata)
		sm.cache.Set(sessionID, session)
	}

	return nil
}

// TerminateSession terminates a session and performs cleanup
func (sm *SessionManager) TerminateSession(sessionID string, reason string) error {
	session, exists := sm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Update session state
	err := HandleSiprecStateChange(session, "terminated", reason)
	if err != nil {
		return err
	}

	// Schedule cleanup task
	task := func() {
		CleanupSessionResources(session)
		sm.removeSession(sessionID)
	}

	// Use dedicated cleanup pool
	if !sm.workerPool.SubmitTask("session_cleanup", task) {
		// Fallback to immediate cleanup if pool is full
		CleanupSessionResources(session)
		sm.removeSession(sessionID)
	}

	return nil
}

// PauseSession pauses a recording session
func (sm *SessionManager) PauseSession(sessionID string, reason string) error {
	session, exists := sm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	err := PauseRecording(session, reason)
	if err != nil {
		return err
	}

	// Update cache
	sm.cache.Set(sessionID, session)
	return nil
}

// ResumeSession resumes a paused recording session
func (sm *SessionManager) ResumeSession(sessionID string, reason string) error {
	session, exists := sm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	err := ResumeRecording(session, reason)
	if err != nil {
		return err
	}

	// Update cache
	sm.cache.Set(sessionID, session)
	return nil
}

// GetAllSessions returns all active sessions (paginated for memory efficiency)
func (sm *SessionManager) GetAllSessions(offset, limit int) ([]*RecordingSession, int) {
	var sessions []*RecordingSession
	count := 0

	sm.sessions.Range(func(key, value interface{}) bool {
		if session, ok := value.(*RecordingSession); ok {
			if session.IsValid && !IsSessionExpired(session) {
				if count >= offset && len(sessions) < limit {
					sessions = append(sessions, session)
				}
				count++
			}
		}
		return true
	})

	return sessions, count
}

// GetSessionCount returns the number of active sessions
func (sm *SessionManager) GetSessionCount() int {
	return int(atomic.LoadInt32(&sm.activeSessions))
}

// GetActiveSessionCount returns the number of active sessions (alias for GetSessionCount)
func (sm *SessionManager) GetActiveSessionCount() int {
	return sm.GetSessionCount()
}

// GetStatistics returns comprehensive session manager statistics
func (sm *SessionManager) GetStatistics() SessionManagerStats {
	cacheStats := sm.cache.GetStats()
	workerStats := sm.workerPool.GetAllStats()
	memStats := util.GetMemoryStats()

	return SessionManagerStats{
		ActiveSessions:  atomic.LoadInt32(&sm.activeSessions),
		TotalSessions:   atomic.LoadInt64(&sm.totalSessions),
		MaxSessions:     sm.maxSessions,
		CacheHitRate:    cacheStats.HitRate,
		CacheSize:       cacheStats.Size,
		WorkerPoolStats: workerStats,
		MemoryUsage:     memStats.AllocatedBytes,
		GoroutineCount:  memStats.NumGoroutines,
		GCCycles:        int64(memStats.GCCycles),
	}
}

// SessionManagerStats provides comprehensive statistics
type SessionManagerStats struct {
	ActiveSessions  int32                     `json:"active_sessions"`
	TotalSessions   int64                     `json:"total_sessions"`
	MaxSessions     int32                     `json:"max_sessions"`
	CacheHitRate    float64                   `json:"cache_hit_rate"`
	CacheSize       int                       `json:"cache_size"`
	WorkerPoolStats map[string]util.PoolStats `json:"worker_pool_stats"`
	MemoryUsage     uint64                    `json:"memory_usage_bytes"`
	GoroutineCount  int                       `json:"goroutine_count"`
	GCCycles        int64                     `json:"gc_cycles"`
}

// removeSession removes a session from storage and cache
func (sm *SessionManager) removeSession(sessionID string) {
	sm.sessions.Delete(sessionID)
	sm.cache.Delete(sessionID)
	atomic.AddInt32(&sm.activeSessions, -1)
}

// startBackgroundTasks starts cleanup and maintenance goroutines
func (sm *SessionManager) startBackgroundTasks() {
	// Session cleanup task
	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.cleanupExpiredSessions()
	}()

	// Memory optimization task
	sm.wg.Add(1)
	go func() {
		defer sm.wg.Done()
		sm.memoryOptimization()
	}()
}

// cleanupExpiredSessions periodically removes expired sessions
func (sm *SessionManager) cleanupExpiredSessions() {
	// Validate cleanup interval and set a safe default if invalid
	cleanupInterval := sm.cleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Minute // Safe default
	}
	
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sm.performCleanup()
		case <-sm.ctx.Done():
			return
		}
	}
}

// performCleanup removes expired and invalid sessions
func (sm *SessionManager) performCleanup() {
	var toRemove []string

	// Collect expired sessions
	sm.sessions.Range(func(key, value interface{}) bool {
		if sessionID, ok := key.(string); ok {
			if session, ok := value.(*RecordingSession); ok {
				if !session.IsValid || IsSessionExpired(session) {
					toRemove = append(toRemove, sessionID)
				}
			}
		}
		return true
	})

	// Remove expired sessions
	for _, sessionID := range toRemove {
		if session, exists := sm.GetSession(sessionID); exists {
			CleanupSessionResources(session)
		}
		sm.removeSession(sessionID)
	}
}

// memoryOptimization performs periodic memory optimization
func (sm *SessionManager) memoryOptimization() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Force garbage collection periodically under high load
			if sm.GetSessionCount() > int(sm.maxSessions)/2 {
				runtime.GC()
			}
		case <-sm.ctx.Done():
			return
		}
	}
}

// Shutdown gracefully shuts down the session manager
func (sm *SessionManager) Shutdown(timeout time.Duration) error {
	sm.cancel()

	// Wait for background tasks to complete
	done := make(chan struct{})
	go func() {
		sm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(timeout):
		return fmt.Errorf("shutdown timeout exceeded")
	}

	// Close cache
	sm.cache.Close()

	return nil
}

// Global session manager instance
var globalSessionManager *SessionManager
var sessionManagerOnce sync.Once

// GetGlobalSessionManager returns the global session manager
func GetGlobalSessionManager() *SessionManager {
	sessionManagerOnce.Do(func() {
		globalSessionManager = NewSessionManager(nil)
	})
	return globalSessionManager
}
