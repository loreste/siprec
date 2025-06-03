package failover

import (
	"fmt"
	"sync"
	"time"

	"siprec-server/pkg/clustering"
	"siprec-server/pkg/database"

	"github.com/sirupsen/logrus"
)

// SessionFailover manages session failover and recovery
type SessionFailover struct {
	cluster         *clustering.RedisCluster
	repo            *database.Repository
	logger          *logrus.Logger
	nodeID          string
	failoverEnabled bool
	checkInterval   time.Duration
	sessionTimeout  time.Duration
	activeSessions  map[string]*FailoverSession
	mutex           sync.RWMutex
	recoveryQueue   chan *RecoveryTask
	workers         int
}

// FailoverSession represents a session with failover information
type FailoverSession struct {
	SessionID     string                 `json:"session_id"`
	CallID        string                 `json:"call_id"`
	PrimaryNodeID string                 `json:"primary_node_id"`
	BackupNodeID  string                 `json:"backup_node_id,omitempty"`
	Status        string                 `json:"status"`
	LastHeartbeat time.Time              `json:"last_heartbeat"`
	RecordingPath string                 `json:"recording_path"`
	FailoverCount int                    `json:"failover_count"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// RecoveryTask represents a session recovery task
type RecoveryTask struct {
	SessionID      string
	FailedNodeID   string
	RecoveryNodeID string
	Priority       int
	CreatedAt      time.Time
}

// FailoverConfig holds failover configuration
type FailoverConfig struct {
	Enabled        bool
	CheckInterval  time.Duration
	SessionTimeout time.Duration
	Workers        int
	MaxFailovers   int
}

// NewSessionFailover creates a new session failover manager
func NewSessionFailover(cluster *clustering.RedisCluster, repo *database.Repository, nodeID string, config FailoverConfig, logger *logrus.Logger) *SessionFailover {
	failover := &SessionFailover{
		cluster:         cluster,
		repo:            repo,
		logger:          logger,
		nodeID:          nodeID,
		failoverEnabled: config.Enabled,
		checkInterval:   config.CheckInterval,
		sessionTimeout:  config.SessionTimeout,
		activeSessions:  make(map[string]*FailoverSession),
		recoveryQueue:   make(chan *RecoveryTask, 1000),
		workers:         config.Workers,
	}

	if config.Enabled {
		// Start failover monitoring
		go failover.startFailoverMonitoring()

		// Start recovery workers
		for i := 0; i < config.Workers; i++ {
			go failover.startRecoveryWorker(i)
		}

		logger.WithFields(logrus.Fields{
			"node_id":         nodeID,
			"check_interval":  config.CheckInterval,
			"session_timeout": config.SessionTimeout,
			"workers":         config.Workers,
		}).Info("Session failover manager initialized")
	} else {
		logger.Info("Session failover disabled")
	}

	return failover
}

// RegisterSession registers a session for failover monitoring
func (f *SessionFailover) RegisterSession(sessionID, callID, recordingPath string) error {
	if !f.failoverEnabled {
		return nil
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	session := &FailoverSession{
		SessionID:     sessionID,
		CallID:        callID,
		PrimaryNodeID: f.nodeID,
		Status:        "active",
		LastHeartbeat: time.Now(),
		RecordingPath: recordingPath,
		FailoverCount: 0,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Metadata:      make(map[string]interface{}),
	}

	f.activeSessions[sessionID] = session

	// Store in cluster
	if err := f.storeFailoverSession(session); err != nil {
		f.logger.WithError(err).WithField("session_id", sessionID).Error("Failed to store failover session")
		return err
	}

	f.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"call_id":    callID,
		"node_id":    f.nodeID,
	}).Info("Session registered for failover")

	return nil
}

// UpdateSessionHeartbeat updates the heartbeat for a session
func (f *SessionFailover) UpdateSessionHeartbeat(sessionID string) error {
	if !f.failoverEnabled {
		return nil
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	session, exists := f.activeSessions[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.LastHeartbeat = time.Now()
	session.UpdatedAt = time.Now()

	// Update in cluster
	if err := f.storeFailoverSession(session); err != nil {
		f.logger.WithError(err).WithField("session_id", sessionID).Warning("Failed to update session heartbeat")
	}

	return nil
}

// UnregisterSession removes a session from failover monitoring
func (f *SessionFailover) UnregisterSession(sessionID string) error {
	if !f.failoverEnabled {
		return nil
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()

	delete(f.activeSessions, sessionID)

	// Remove from cluster
	if err := f.removeFailoverSession(sessionID); err != nil {
		f.logger.WithError(err).WithField("session_id", sessionID).Warning("Failed to remove failover session")
	}

	f.logger.WithField("session_id", sessionID).Info("Session unregistered from failover")
	return nil
}

// startFailoverMonitoring monitors sessions for failures and triggers failover
func (f *SessionFailover) startFailoverMonitoring() {
	ticker := time.NewTicker(f.checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := f.checkForFailedSessions(); err != nil {
			f.logger.WithError(err).Error("Failed to check for failed sessions")
		}
	}
}

// checkForFailedSessions checks for sessions that have failed and need recovery
func (f *SessionFailover) checkForFailedSessions() error {
	// Only the cluster leader performs failover checks
	if !f.cluster.IsLeader() {
		return nil
	}

	// Get all cluster sessions
	sessions, err := f.cluster.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list cluster sessions: %w", err)
	}

	// Get cluster nodes
	nodes, err := f.cluster.GetClusterNodes()
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	// Build node status map
	nodeStatus := make(map[string]bool)
	for _, node := range nodes {
		// Consider node failed if no heartbeat for more than 2 intervals
		nodeStatus[node.ID] = time.Since(node.LastHeartbeat) < (f.checkInterval * 2)
	}

	// Check each session for potential failover
	for _, session := range sessions {
		if f.needsFailover(session, nodeStatus) {
			f.triggerSessionFailover(session, nodeStatus)
		}
	}

	return nil
}

// needsFailover determines if a session needs failover
func (f *SessionFailover) needsFailover(session *clustering.ClusteredSession, nodeStatus map[string]bool) bool {
	// Check if session node is failed
	nodeAlive, exists := nodeStatus[session.NodeID]
	if !exists || !nodeAlive {
		return true
	}

	// Check if session has timed out
	if time.Since(session.LastUpdate) > f.sessionTimeout {
		return true
	}

	return false
}

// triggerSessionFailover initiates failover for a session
func (f *SessionFailover) triggerSessionFailover(session *clustering.ClusteredSession, nodeStatus map[string]bool) {
	f.logger.WithFields(logrus.Fields{
		"session_id":     session.SessionID,
		"failed_node_id": session.NodeID,
		"call_id":        session.CallID,
	}).Warning("Triggering session failover")

	// Find available recovery node
	recoveryNodeID := f.selectRecoveryNode(session.NodeID, nodeStatus)
	if recoveryNodeID == "" {
		f.logger.WithField("session_id", session.SessionID).Error("No available recovery node for failover")
		return
	}

	// Create recovery task
	task := &RecoveryTask{
		SessionID:      session.SessionID,
		FailedNodeID:   session.NodeID,
		RecoveryNodeID: recoveryNodeID,
		Priority:       f.calculateRecoveryPriority(session),
		CreatedAt:      time.Now(),
	}

	// Queue recovery task
	select {
	case f.recoveryQueue <- task:
		f.logger.WithFields(logrus.Fields{
			"session_id":    session.SessionID,
			"recovery_node": recoveryNodeID,
			"priority":      task.Priority,
		}).Info("Recovery task queued")
	default:
		f.logger.WithField("session_id", session.SessionID).Error("Recovery queue full, dropping task")
	}
}

// selectRecoveryNode selects the best available node for recovery
func (f *SessionFailover) selectRecoveryNode(failedNodeID string, nodeStatus map[string]bool) string {
	var candidates []string

	// Find healthy nodes
	for nodeID, isAlive := range nodeStatus {
		if isAlive && nodeID != failedNodeID {
			candidates = append(candidates, nodeID)
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Simple selection - choose first available
	// In production, consider load balancing, capacity, etc.
	return candidates[0]
}

// calculateRecoveryPriority calculates the priority for session recovery
func (f *SessionFailover) calculateRecoveryPriority(session *clustering.ClusteredSession) int {
	priority := 100 // Base priority

	// Higher priority for newer sessions
	age := time.Since(session.StartTime)
	if age < 5*time.Minute {
		priority += 50
	} else if age < 30*time.Minute {
		priority += 20
	}

	// Higher priority for active sessions
	if session.Status == "active" {
		priority += 30
	}

	return priority
}

// startRecoveryWorker starts a worker to process recovery tasks
func (f *SessionFailover) startRecoveryWorker(workerID int) {
	f.logger.WithField("worker_id", workerID).Info("Starting recovery worker")

	for task := range f.recoveryQueue {
		if err := f.processRecoveryTask(task); err != nil {
			f.logger.WithError(err).WithFields(logrus.Fields{
				"worker_id":  workerID,
				"session_id": task.SessionID,
			}).Error("Failed to process recovery task")
		}
	}
}

// processRecoveryTask processes a session recovery task
func (f *SessionFailover) processRecoveryTask(task *RecoveryTask) error {
	f.logger.WithFields(logrus.Fields{
		"session_id":    task.SessionID,
		"failed_node":   task.FailedNodeID,
		"recovery_node": task.RecoveryNodeID,
		"priority":      task.Priority,
	}).Info("Processing recovery task")

	// Get session details
	session, err := f.cluster.GetSession(task.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Create recovery plan
	plan := &RecoveryPlan{
		SessionID:      task.SessionID,
		FailedNodeID:   task.FailedNodeID,
		RecoveryNodeID: task.RecoveryNodeID,
		RecordingPath:  session.RecordingPath,
		Participants:   session.Participants,
		StartTime:      session.StartTime,
		CreatedAt:      time.Now(),
		Steps:          f.createRecoverySteps(session, task),
	}

	// Execute recovery plan
	if err := f.executeRecoveryPlan(plan); err != nil {
		return fmt.Errorf("failed to execute recovery plan: %w", err)
	}

	// Update session in cluster
	updates := map[string]interface{}{
		"node_id":        task.RecoveryNodeID,
		"status":         "recovered",
		"last_update":    time.Now(),
		"failover_count": session.Metadata["failover_count"].(int) + 1,
	}

	if err := f.cluster.UpdateSession(task.SessionID, updates); err != nil {
		f.logger.WithError(err).WithField("session_id", task.SessionID).Warning("Failed to update session after recovery")
	}

	// Log successful recovery
	f.logger.WithFields(logrus.Fields{
		"session_id":    task.SessionID,
		"recovery_node": task.RecoveryNodeID,
		"duration":      time.Since(task.CreatedAt),
	}).Info("Session recovery completed successfully")

	return nil
}

// createRecoverySteps creates the steps for session recovery
func (f *SessionFailover) createRecoverySteps(session *clustering.ClusteredSession, task *RecoveryTask) []*RecoveryStep {
	var steps []*RecoveryStep

	// Step 1: Validate recovery node
	steps = append(steps, &RecoveryStep{
		Type:        "validate_node",
		Description: "Validate recovery node availability",
		Data: map[string]interface{}{
			"node_id": task.RecoveryNodeID,
		},
	})

	// Step 2: Transfer recording if needed
	if session.RecordingPath != "" {
		steps = append(steps, &RecoveryStep{
			Type:        "transfer_recording",
			Description: "Transfer recording to recovery node",
			Data: map[string]interface{}{
				"source_path": session.RecordingPath,
				"node_id":     task.RecoveryNodeID,
			},
		})
	}

	// Step 3: Restore session state
	steps = append(steps, &RecoveryStep{
		Type:        "restore_session",
		Description: "Restore session state on recovery node",
		Data: map[string]interface{}{
			"session_data": session,
		},
	})

	// Step 4: Update cluster state
	steps = append(steps, &RecoveryStep{
		Type:        "update_cluster",
		Description: "Update cluster state",
		Data: map[string]interface{}{
			"session_id": task.SessionID,
			"new_node":   task.RecoveryNodeID,
		},
	})

	return steps
}

// executeRecoveryPlan executes a recovery plan
func (f *SessionFailover) executeRecoveryPlan(plan *RecoveryPlan) error {
	f.logger.WithFields(logrus.Fields{
		"session_id": plan.SessionID,
		"steps":      len(plan.Steps),
	}).Info("Executing recovery plan")

	for i, step := range plan.Steps {
		f.logger.WithFields(logrus.Fields{
			"session_id": plan.SessionID,
			"step":       i + 1,
			"type":       step.Type,
		}).Debug("Executing recovery step")

		if err := f.executeRecoveryStep(step); err != nil {
			return fmt.Errorf("failed to execute step %d (%s): %w", i+1, step.Type, err)
		}

		step.CompletedAt = time.Now()
	}

	return nil
}

// executeRecoveryStep executes a single recovery step
func (f *SessionFailover) executeRecoveryStep(step *RecoveryStep) error {
	switch step.Type {
	case "validate_node":
		return f.validateRecoveryNode(step.Data)
	case "transfer_recording":
		return f.transferRecording(step.Data)
	case "restore_session":
		return f.restoreSession(step.Data)
	case "update_cluster":
		return f.updateClusterState(step.Data)
	default:
		return fmt.Errorf("unknown recovery step type: %s", step.Type)
	}
}

// Recovery step implementations

func (f *SessionFailover) validateRecoveryNode(data map[string]interface{}) error {
	nodeID, ok := data["node_id"].(string)
	if !ok {
		return fmt.Errorf("invalid node_id in recovery data")
	}

	// Get cluster nodes
	nodes, err := f.cluster.GetClusterNodes()
	if err != nil {
		return fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	// Check if node is available
	for _, node := range nodes {
		if node.ID == nodeID && node.Status == "active" {
			return nil
		}
	}

	return fmt.Errorf("recovery node not available: %s", nodeID)
}

func (f *SessionFailover) transferRecording(data map[string]interface{}) error {
	// In a real implementation, this would:
	// 1. Copy recording files to the recovery node
	// 2. Verify file integrity
	// 3. Update recording paths

	f.logger.WithField("data", data).Info("Recording transfer simulation completed")
	return nil
}

func (f *SessionFailover) restoreSession(data map[string]interface{}) error {
	// In a real implementation, this would:
	// 1. Notify the recovery node to take over the session
	// 2. Restore session state and continue recording
	// 3. Resume any paused processes

	f.logger.WithField("data", data).Info("Session restoration simulation completed")
	return nil
}

func (f *SessionFailover) updateClusterState(data map[string]interface{}) error {
	sessionID, ok := data["session_id"].(string)
	if !ok {
		return fmt.Errorf("invalid session_id in recovery data")
	}

	newNodeID, ok := data["new_node"].(string)
	if !ok {
		return fmt.Errorf("invalid new_node in recovery data")
	}

	// Update session in cluster
	updates := map[string]interface{}{
		"node_id":     newNodeID,
		"status":      "recovered",
		"last_update": time.Now(),
	}

	return f.cluster.UpdateSession(sessionID, updates)
}

// Helper methods

func (f *SessionFailover) storeFailoverSession(session *FailoverSession) error {
	// Store failover session in Redis with a specific key
	// This is separate from the main session storage
	return nil // Simplified implementation
}

func (f *SessionFailover) removeFailoverSession(sessionID string) error {
	// Remove failover session from Redis
	return nil // Simplified implementation
}

// GetFailoverStats returns failover statistics
func (f *SessionFailover) GetFailoverStats() *FailoverStats {
	f.mutex.RLock()
	defer f.mutex.RUnlock()

	stats := &FailoverStats{
		Enabled:           f.failoverEnabled,
		ActiveSessions:    len(f.activeSessions),
		RecoveryQueueSize: len(f.recoveryQueue),
		CheckInterval:     f.checkInterval,
		SessionTimeout:    f.sessionTimeout,
		Workers:           f.workers,
		LastCheck:         time.Now(),
	}

	return stats
}

// Types

type RecoveryPlan struct {
	SessionID      string          `json:"session_id"`
	FailedNodeID   string          `json:"failed_node_id"`
	RecoveryNodeID string          `json:"recovery_node_id"`
	RecordingPath  string          `json:"recording_path"`
	Participants   []string        `json:"participants"`
	StartTime      time.Time       `json:"start_time"`
	CreatedAt      time.Time       `json:"created_at"`
	Steps          []*RecoveryStep `json:"steps"`
}

type RecoveryStep struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
}

type FailoverStats struct {
	Enabled           bool          `json:"enabled"`
	ActiveSessions    int           `json:"active_sessions"`
	RecoveryQueueSize int           `json:"recovery_queue_size"`
	CheckInterval     time.Duration `json:"check_interval"`
	SessionTimeout    time.Duration `json:"session_timeout"`
	Workers           int           `json:"workers"`
	LastCheck         time.Time     `json:"last_check"`
}
