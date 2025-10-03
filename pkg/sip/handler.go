package sip

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"siprec-server/pkg/errors"
	"siprec-server/pkg/media"
	"siprec-server/pkg/siprec"
	"siprec-server/pkg/telemetry/tracing"

	"github.com/sirupsen/logrus"
)

type SessionStore interface {
	// Save stores a call data by key
	Save(key string, data *CallData) error

	// Load retrieves call data by key
	Load(key string) (*CallData, error)

	// Delete removes call data by key
	Delete(key string) error

	// List returns all stored keys
	List() ([]string, error)
}

// Config for the SIP handler
type Config struct {
	// Maximum concurrent calls allowed
	MaxConcurrentCalls int

	// Media configuration
	MediaConfig *media.Config

	// SIP ports for proper NAT configuration
	SIPPorts []int

	// NAT configuration for SIP header rewriting
	NATConfig *NATConfig

	// Redundancy-related configuration
	RedundancyEnabled    bool
	SessionTimeout       time.Duration
	SessionCheckInterval time.Duration

	// Storage type for redundancy (memory, redis)
	RedundancyStorageType string

	// Number of shards for concurrent session handling
	// Higher values reduce lock contention but increase memory usage
	// Must be a power of 2 (16, 32, 64, etc.)
	ShardCount int
}

// Handler for SIP requests - now works with CustomSIPServer
type Handler struct {
	Logger      *logrus.Logger
	Config      *Config
	ActiveCalls *ShardedMap // Sharded map of call UUID to CallData for better concurrency

	// Speech-to-text callback function
	STTCallback func(context.Context, string, io.Reader, string) error

	// For session redundancy
	SessionStore SessionStore

	// For session monitor goroutine
	monitorCtx       context.Context
	monitorCancel    context.CancelFunc
	sessionMonitorWG sync.WaitGroup

	// NAT rewriter for SIP header modification
	NATRewriter *NATRewriter

	// Custom SIP server for handling SIPREC with metadata
	Server *CustomSIPServer
}

// CallData holds information about an active call
type CallData struct {
	// Forwarder for RTP packets
	Forwarder *media.RTPForwarder

	// SIPREC recording session information
	RecordingSession *siprec.RecordingSession

	// Dialog information for the call (required for sending BYE)
	DialogInfo *DialogInfo

	// Last activity timestamp (for session monitoring)
	LastActivity time.Time

	// Remote address for potential reconnection
	RemoteAddress string

	// TraceScope links the call to its OpenTelemetry span
	TraceScope *tracing.CallScope

	// Mutex for protecting mutable fields
	mu sync.RWMutex
}

// DialogInfo holds information about a SIP dialog
type DialogInfo struct {
	// Call-ID for the dialog
	CallID string

	// Tags for From and To headers
	LocalTag  string
	RemoteTag string

	// URI values
	LocalURI  string
	RemoteURI string

	// Sequence numbers
	LocalSeq  int
	RemoteSeq int

	// Contact header
	Contact string

	// Route set
	RouteSet []string
}

// NewHandler creates a new SIP handler
func NewHandler(logger *logrus.Logger, config *Config, sttCallback func(context.Context, string, io.Reader, string) error) (*Handler, error) {
	if config == nil {
		return nil, errors.New("configuration cannot be nil")
	}

	// Determine shard count - use default of 32 if not specified
	shardCount := config.ShardCount
	if shardCount <= 0 {
		shardCount = 32
		logger.WithField("default_shard_count", shardCount).Info("Using default shard count for call map")
	}

	// Create a new sharded map for active calls
	activeCalls := NewShardedMap(shardCount)

	// Create the handler
	handler := &Handler{
		Logger:      logger,
		Config:      config,
		STTCallback: sttCallback,
		ActiveCalls: activeCalls,
	}

	// Initialize NAT rewriter if NAT configuration is provided
	if config.NATConfig != nil {
		natRewriter, err := NewNATRewriter(config.NATConfig, logger)
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize NAT rewriter")
		}
		handler.NATRewriter = natRewriter
		logger.Info("NAT rewriter initialized for SIP header rewriting")
	} else if config.MediaConfig != nil {
		// Try to create NAT config from media config with SIP ports
		natConfig := NewNATConfigFromMediaConfig(config.MediaConfig, config.SIPPorts)
		if natConfig != nil {
			natRewriter, err := NewNATRewriter(natConfig, logger)
			if err != nil {
				logger.WithError(err).Warn("Failed to initialize NAT rewriter from media config")
			} else {
				handler.NATRewriter = natRewriter
				logger.Info("NAT rewriter initialized from media configuration")
			}
		}
	}

	// Initialize the session store if redundancy is enabled
	if config.RedundancyEnabled {
		switch config.RedundancyStorageType {
		case "memory":
			logger.Info("Using in-memory session store")
			handler.SessionStore = NewMemorySessionStore()
		default:
			// Default to memory store for now
			logger.Warn("Unknown storage type, using in-memory session store")
			handler.SessionStore = NewMemorySessionStore()
		}

		// Create a dedicated context for the session monitor
		handler.monitorCtx, handler.monitorCancel = context.WithCancel(context.Background())
		handler.sessionMonitorWG.Add(1)

		// Start the session monitor
		go handler.monitorSessions(handler.monitorCtx)
	}

	// Initialize the custom SIP server
	handler.Server = NewCustomSIPServer(logger, handler)

	return handler, nil
}

// SetupHandlers is a compatibility method - actual handlers are set up by CustomSIPServer
func (h *Handler) SetupHandlers() {
	// Handler setup is now done by CustomSIPServer directly
	// The custom server calls appropriate handler methods based on SIP method
	h.Logger.Info("SIP request handlers configured via CustomSIPServer")
}

// UpdateActivity updates the last activity timestamp for a call
func (c *CallData) UpdateActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastActivity = time.Now()
}

// IsStale checks if a session is stale based on last activity
func (c *CallData) IsStale(timeout time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.LastActivity) > timeout
}

// SafeCopy creates a thread-safe copy of CallData for serialization
func (c *CallData) SafeCopy() CallData {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy with the current values
	copy := CallData{
		Forwarder:        c.Forwarder,
		RecordingSession: c.RecordingSession,
		DialogInfo:       c.DialogInfo,
		LastActivity:     c.LastActivity,
		RemoteAddress:    c.RemoteAddress,
		// Note: Don't copy the mutex
	}
	return copy
}

// monitorSessions periodically checks for stale sessions
func (h *Handler) monitorSessions(ctx context.Context) {
	defer h.sessionMonitorWG.Done()

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			h.Logger.WithFields(logrus.Fields{
				"panic":     r,
				"component": "session_monitor",
			}).Error("Recovered from panic in session monitor")
		}
	}()

	logger := h.Logger.WithField("component", "session_monitor")
	logger.Info("Starting session monitor")

	// Create a ticker for the check interval
	ticker := time.NewTicker(h.Config.SessionCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Session monitor shutting down")
			return
		case <-ticker.C:
			// Check for stale sessions
			h.cleanupStaleSessions()
		}
	}
}

// cleanupStaleSessions checks for and cleans up stale sessions
func (h *Handler) cleanupStaleSessions() {
	logger := h.Logger.WithField("component", "session_cleanup")

	// Define stale criterion
	staleDuration := h.Config.SessionTimeout

	// Collect stale sessions first to avoid modifying map during iteration
	var staleCallUUIDs []string
	var staleCallData []*CallData

	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)

		// Check if the session is stale
		if callData.IsStale(staleDuration) {
			staleCallUUIDs = append(staleCallUUIDs, callUUID)
			staleCallData = append(staleCallData, callData)
		}

		return true // Continue iteration
	})

	// Process stale sessions outside of the iteration
	for i, callUUID := range staleCallUUIDs {
		callData := staleCallData[i]
		logger.WithField("call_uuid", callUUID).Info("Cleaning up stale session")

		// Stop RTP forwarding and properly clean up resources
		if callData.Forwarder != nil {
			// Use mutex to ensure thread-safe access to MarkedForCleanup
			callData.Forwarder.CleanupMutex.Lock()
			callData.Forwarder.MarkedForCleanup = true
			callData.Forwarder.CleanupMutex.Unlock()

			// Safely signal forwarding to stop
			callData.Forwarder.Stop()

			// Perform thorough cleanup of all RTP forwarder resources
			callData.Forwarder.Cleanup()
		}

		if callData.TraceScope != nil {
			callData.TraceScope.End(nil)
		}

		// Remove from active calls
		h.ActiveCalls.Delete(callUUID)

		// Clean up from session store if redundancy is enabled
		if h.Config.RedundancyEnabled {
			h.SessionStore.Delete(callUUID)
		}
	}

	// Clean up stale sessions from the store if redundancy is enabled
	if h.Config.RedundancyEnabled {
		storedSessions, err := h.SessionStore.List()
		if err != nil {
			logger.WithError(err).Error("Failed to list stored sessions")
			return
		}

		for _, sessionID := range storedSessions {
			// Remove orphaned sessions that are too old
			if _, exists := h.ActiveCalls.Load(sessionID); !exists {
				sessionData, err := h.SessionStore.Load(sessionID)
				if err != nil {
					continue
				}

				// Check if the session is too old
				if sessionData.IsStale(h.Config.SessionTimeout * 2) {
					logger.WithField("session_id", sessionID).Info("Removing stale orphaned session")
					h.SessionStore.Delete(sessionID)
				}
			}
		}
	}
}

// CleanupActiveCalls cleans up all active calls
func (h *Handler) CleanupActiveCalls() {
	logger := h.Logger.WithField("component", "cleanup")
	logger.Info("Cleaning up all active calls")

	// Track if redundancy is enabled
	isRedundancyEnabled := h.Config.RedundancyEnabled

	// Collect all active calls first to avoid modifying map during iteration
	var activeCallUUIDs []string
	var activeCallData []*CallData

	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callUUID := key.(string)
		callData := value.(*CallData)
		activeCallUUIDs = append(activeCallUUIDs, callUUID)
		activeCallData = append(activeCallData, callData)
		return true // Continue iteration
	})

	// Process all active calls outside of the iteration
	for i, callUUID := range activeCallUUIDs {
		callData := activeCallData[i]

		// Stop RTP forwarding
		if callData.Forwarder != nil {
			logger.WithField("call_uuid", callUUID).Debug("Stopping RTP forwarding")
			close(callData.Forwarder.StopChan)
		}

		// Update recording session state if needed
		if callData.RecordingSession != nil {
			callData.RecordingSession.RecordingState = "stopped"
			callData.RecordingSession.EndTime = time.Now()

			// If redundancy is enabled, update the session store
			if isRedundancyEnabled {
				h.SessionStore.Save(callUUID, callData)
			}
		}

		// Remove from the active calls map
		h.ActiveCalls.Delete(callUUID)

		if callData.TraceScope != nil {
			callData.TraceScope.End(nil)
		}
	}

	// Log status of persistent sessions
	if isRedundancyEnabled {
		sessions, err := h.SessionStore.List()
		if err != nil {
			logger.WithError(err).Error("Failed to list persistent sessions during shutdown")
		} else {
			logger.WithField("preserved_sessions", len(sessions)).
				Info("Sessions preserved in persistent store for recovery")
		}
	}
}

// GetActiveCallCount returns the number of currently active calls
func (h *Handler) GetActiveCallCount() int {
	return h.ActiveCalls.Count()
}

// GetSession returns information about a specific session
func (h *Handler) GetSession(id string) (interface{}, error) {
	// Try to load from active calls
	if value, exists := h.ActiveCalls.Load(id); exists {
		callData := value.(*CallData)

		// Create session info response
		sessionInfo := map[string]interface{}{
			"id":            id,
			"state":         "active",
			"last_activity": callData.LastActivity,
			"remote_addr":   callData.RemoteAddress,
		}

		// Add recording session info if available
		if callData.RecordingSession != nil {
			sessionInfo["recording"] = map[string]interface{}{
				"session_id":   callData.RecordingSession.ID,
				"state":        callData.RecordingSession.RecordingState,
				"start_time":   callData.RecordingSession.StartTime,
				"participants": len(callData.RecordingSession.Participants),
				"media_types":  callData.RecordingSession.MediaStreamTypes,
			}
		}

		// Add dialog info if available
		if callData.DialogInfo != nil {
			sessionInfo["dialog"] = map[string]interface{}{
				"call_id":    callData.DialogInfo.CallID,
				"local_tag":  callData.DialogInfo.LocalTag,
				"remote_tag": callData.DialogInfo.RemoteTag,
				"local_uri":  callData.DialogInfo.LocalURI,
				"remote_uri": callData.DialogInfo.RemoteURI,
			}
		}

		return sessionInfo, nil
	}

	// Try to load from persistent store if enabled
	if h.Config.RedundancyEnabled && h.SessionStore != nil {
		storedData, err := h.SessionStore.Load(id)
		if err == nil && storedData != nil {
			// Return stored session info
			return map[string]interface{}{
				"id":            id,
				"state":         "stored",
				"last_activity": storedData.LastActivity,
				"remote_addr":   storedData.RemoteAddress,
			}, nil
		}
	}

	return nil, errors.New("session not found")
}

// GetAllSessions returns information about all active sessions
func (h *Handler) GetAllSessions() ([]interface{}, error) {
	sessions := make([]interface{}, 0)

	// Collect active sessions
	h.ActiveCalls.Range(func(key, value interface{}) bool {
		id := key.(string)
		sessionInfo, err := h.GetSession(id)
		if err == nil {
			sessions = append(sessions, sessionInfo)
		}
		return true
	})

	// Add stored sessions if redundancy is enabled
	if h.Config.RedundancyEnabled && h.SessionStore != nil {
		storedIDs, err := h.SessionStore.List()
		if err == nil {
			for _, id := range storedIDs {
				// Skip if already in active calls
				if _, exists := h.ActiveCalls.Load(id); exists {
					continue
				}

				sessionInfo, err := h.GetSession(id)
				if err == nil {
					sessions = append(sessions, sessionInfo)
				}
			}
		}
	}

	return sessions, nil
}

// GetSessionStatistics returns detailed session statistics
func (h *Handler) GetSessionStatistics() map[string]interface{} {
	activeCalls := h.GetActiveCallCount()

	stats := map[string]interface{}{
		"active_calls":      activeCalls,
		"metrics_available": true,
		"timestamp":         time.Now().Unix(),
	}

	// Count different session states
	var recording, connected int

	h.ActiveCalls.Range(func(key, value interface{}) bool {
		callData := value.(*CallData)
		if callData.RecordingSession != nil {
			recording++
			if callData.RecordingSession.RecordingState == "active" {
				connected++
			}
		}
		return true
	})

	stats["recording_sessions"] = recording
	stats["connected_sessions"] = connected

	// Add memory stats if available
	if h.SessionStore != nil {
		if storedIDs, err := h.SessionStore.List(); err == nil {
			stats["stored_sessions"] = len(storedIDs)
		}
	}

	// Add port usage stats
	available, total := media.GetPortManagerStats()
	stats["rtp_ports"] = map[string]interface{}{
		"available": available,
		"total":     total,
		"used":      total - available,
	}

	return stats
}

// Shutdown gracefully shuts down the SIP handler and all its components
func (h *Handler) Shutdown(ctx context.Context) error {
	logger := h.Logger.WithField("operation", "sip_shutdown")
	logger.Info("Shutting down SIP handler and all components")

	// Log the number of active calls before shutdown
	callCount := h.GetActiveCallCount()
	logger.WithField("active_calls", callCount).Info("Active calls before shutdown")

	// First clean up all active calls
	h.CleanupActiveCalls()

	// Stop the session monitor
	if h.monitorCancel != nil {
		h.monitorCancel()

		// Wait for the monitor goroutine to exit with a timeout
		done := make(chan struct{})
		go func() {
			h.sessionMonitorWG.Wait()
			close(done)
		}()

		select {
		case <-done:
			logger.Debug("Session monitor stopped gracefully")
		case <-ctx.Done():
			logger.Warn("Timed out waiting for session monitor to stop")
		}
	}

	// Shutdown NAT rewriter background processes
	if h.NATRewriter != nil {
		h.NATRewriter.Shutdown()
		logger.Debug("NAT rewriter background processes stopped")
	}

	// SIP server resources are now managed by CustomSIPServer
	logger.Info("SIP Handler shutdown - server resources managed by CustomSIPServer")

	// Close session store if needed
	if closer, ok := h.SessionStore.(io.Closer); ok {
		if err := closer.Close(); err != nil {
			logger.WithError(err).Warn("Error closing session store")
		}
	}

	logger.Info("SIP handler shutdown complete")
	return nil
}

// MemorySessionStore is an in-memory implementation of the SessionStore interface
type MemorySessionStore struct {
	sessions sync.Map
}

// NewMemorySessionStore creates a new in-memory session store
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{}
}

// Save stores a call data by key
func (s *MemorySessionStore) Save(key string, data *CallData) error {
	// Create a safe copy to avoid race conditions during serialization
	safeCopy := data.SafeCopy()

	// Serialize the call data to JSON
	jsonData, err := json.Marshal(safeCopy)
	if err != nil {
		return err
	}

	// Store the serialized data
	s.sessions.Store(key, jsonData)
	return nil
}

// Load retrieves call data by key
func (s *MemorySessionStore) Load(key string) (*CallData, error) {
	// Get the serialized data
	value, ok := s.sessions.Load(key)
	if !ok {
		return nil, errors.New("session not found")
	}

	// Deserialize the data
	jsonData, ok := value.([]byte)
	if !ok {
		return nil, errors.New("invalid session data format")
	}

	// Unmarshal the data
	var callData CallData
	err := json.Unmarshal(jsonData, &callData)
	if err != nil {
		return nil, err
	}

	return &callData, nil
}

// Delete removes call data by key
func (s *MemorySessionStore) Delete(key string) error {
	s.sessions.Delete(key)
	return nil
}

// List returns all stored keys
func (s *MemorySessionStore) List() ([]string, error) {
	keys := []string{}
	s.sessions.Range(func(key, _ interface{}) bool {
		keys = append(keys, key.(string))
		return true
	})
	return keys, nil
}
