package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	migrationKeyPrefix     = "siprec:migration:"
	migrationQueueKey      = "siprec:migration:queue"
	migrationChannel       = "siprec:migration:events"
	migrationTimeout       = 30 * time.Second
	migrationRetryInterval = 5 * time.Second
	maxMigrationRetries    = 3
)

// StreamMigrationManager handles RTP stream migration between nodes
type StreamMigrationManager struct {
	redis         redis.UniversalClient
	logger        *logrus.Logger
	nodeID        string
	rtpStateManager *RTPStateManager
	stopChan      chan struct{}
	wg            sync.WaitGroup

	// Migration handlers
	migrationMu       sync.RWMutex
	pendingMigrations map[string]*MigrationTask

	// Callbacks
	onMigrationRequest func(task *MigrationTask) error
	onMigrationComplete func(task *MigrationTask)

	// Pub/sub
	pubsub *redis.PubSub
}

// MigrationTask represents a stream migration task
type MigrationTask struct {
	ID              string            `json:"id"`
	CallUUID        string            `json:"call_uuid"`
	SourceNodeID    string            `json:"source_node_id"`
	TargetNodeID    string            `json:"target_node_id"`
	StreamState     *RTPStreamState   `json:"stream_state"`
	Status          MigrationStatus   `json:"status"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     time.Time         `json:"completed_at,omitempty"`
	Error           string            `json:"error,omitempty"`
	Retries         int               `json:"retries"`

	// Migration details
	NewLocalPort    int               `json:"new_local_port,omitempty"`
	NewRTCPPort     int               `json:"new_rtcp_port,omitempty"`
	RecordingOffset int64             `json:"recording_offset,omitempty"`
	LastSequence    uint16            `json:"last_sequence,omitempty"`
	LastTimestamp   uint32            `json:"last_timestamp,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// MigrationStatus represents the status of a migration
type MigrationStatus string

const (
	MigrationStatusPending    MigrationStatus = "pending"
	MigrationStatusPreparing  MigrationStatus = "preparing"
	MigrationStatusTransfer   MigrationStatus = "transfer"
	MigrationStatusCompleting MigrationStatus = "completing"
	MigrationStatusCompleted  MigrationStatus = "completed"
	MigrationStatusFailed     MigrationStatus = "failed"
	MigrationStatusCancelled  MigrationStatus = "cancelled"
)

// NewStreamMigrationManager creates a new stream migration manager
func NewStreamMigrationManager(
	redisClient redis.UniversalClient,
	nodeID string,
	rtpStateManager *RTPStateManager,
	logger *logrus.Logger,
) *StreamMigrationManager {
	return &StreamMigrationManager{
		redis:             redisClient,
		logger:            logger,
		nodeID:            nodeID,
		rtpStateManager:   rtpStateManager,
		stopChan:          make(chan struct{}),
		pendingMigrations: make(map[string]*MigrationTask),
	}
}

// Start begins the migration manager
func (m *StreamMigrationManager) Start(ctx context.Context) error {
	// Subscribe to migration events
	m.pubsub = m.redis.Subscribe(ctx, migrationChannel)

	m.wg.Add(1)
	go m.eventLoop(ctx)

	m.wg.Add(1)
	go m.queueProcessor(ctx)

	m.logger.WithField("node_id", m.nodeID).Info("Stream migration manager started")
	return nil
}

// Stop stops the migration manager
func (m *StreamMigrationManager) Stop() {
	close(m.stopChan)
	if m.pubsub != nil {
		m.pubsub.Close()
	}
	m.wg.Wait()
	m.logger.Info("Stream migration manager stopped")
}

// SetMigrationHandler sets the handler for incoming migration requests
func (m *StreamMigrationManager) SetMigrationHandler(handler func(task *MigrationTask) error) {
	m.onMigrationRequest = handler
}

// SetCompletionHandler sets the handler for migration completion
func (m *StreamMigrationManager) SetCompletionHandler(handler func(task *MigrationTask)) {
	m.onMigrationComplete = handler
}

// InitiateMigration starts migrating a stream to another node
func (m *StreamMigrationManager) InitiateMigration(ctx context.Context, callUUID, targetNodeID string) (*MigrationTask, error) {
	// Get current stream state
	state, err := m.rtpStateManager.GetStream(callUUID)
	if err != nil {
		return nil, fmt.Errorf("stream not found: %w", err)
	}

	if state.NodeID != m.nodeID {
		return nil, fmt.Errorf("stream is not on this node")
	}

	// Create migration task
	task := &MigrationTask{
		ID:           fmt.Sprintf("%s-%d", callUUID, time.Now().UnixNano()),
		CallUUID:     callUUID,
		SourceNodeID: m.nodeID,
		TargetNodeID: targetNodeID,
		StreamState:  state,
		Status:       MigrationStatusPending,
		StartedAt:    time.Now(),
		Metadata:     make(map[string]string),
	}

	// Store migration task
	if err := m.saveMigrationTask(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to save migration task: %w", err)
	}

	// Add to pending
	m.migrationMu.Lock()
	m.pendingMigrations[task.ID] = task
	m.migrationMu.Unlock()

	// Queue for processing
	if err := m.queueMigration(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to queue migration: %w", err)
	}

	// Update RTP state
	m.rtpStateManager.InitiateMigration(callUUID, targetNodeID)

	m.logger.WithFields(logrus.Fields{
		"task_id":     task.ID,
		"call_uuid":   callUUID,
		"target_node": targetNodeID,
	}).Info("Stream migration initiated")

	return task, nil
}

// AcceptMigration accepts an incoming stream migration
func (m *StreamMigrationManager) AcceptMigration(ctx context.Context, taskID string) error {
	// Get migration task
	task, err := m.getMigrationTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("migration task not found: %w", err)
	}

	if task.TargetNodeID != m.nodeID {
		return fmt.Errorf("migration is not targeted at this node")
	}

	// Update status
	task.Status = MigrationStatusPreparing
	m.saveMigrationTask(ctx, task)

	// Allocate resources for the new stream
	if m.onMigrationRequest != nil {
		if err := m.onMigrationRequest(task); err != nil {
			task.Status = MigrationStatusFailed
			task.Error = err.Error()
			m.saveMigrationTask(ctx, task)
			m.publishMigrationEvent(task, "failed")
			return fmt.Errorf("migration handler failed: %w", err)
		}
	}

	// Complete migration on this node
	task.Status = MigrationStatusCompleting
	m.saveMigrationTask(ctx, task)

	// Create new stream state
	newState := *task.StreamState
	newState.NodeID = m.nodeID
	newState.LocalPort = task.NewLocalPort
	newState.RTCPPort = task.NewRTCPPort
	newState.Status = RTPStreamStatusActive
	newState.MigrationTarget = ""

	// Register with RTP state manager
	if err := m.rtpStateManager.CompleteMigration(task.CallUUID, &newState); err != nil {
		task.Status = MigrationStatusFailed
		task.Error = err.Error()
		m.saveMigrationTask(ctx, task)
		m.publishMigrationEvent(task, "failed")
		return fmt.Errorf("failed to complete migration: %w", err)
	}

	// Mark as completed
	task.Status = MigrationStatusCompleted
	task.CompletedAt = time.Now()
	m.saveMigrationTask(ctx, task)

	// Publish completion event
	m.publishMigrationEvent(task, "completed")

	// Notify handlers
	if m.onMigrationComplete != nil {
		m.onMigrationComplete(task)
	}

	m.logger.WithFields(logrus.Fields{
		"task_id":    task.ID,
		"call_uuid":  task.CallUUID,
		"new_port":   task.NewLocalPort,
		"duration":   time.Since(task.StartedAt).Milliseconds(),
	}).Info("Stream migration accepted and completed")

	return nil
}

// CancelMigration cancels a pending migration
func (m *StreamMigrationManager) CancelMigration(ctx context.Context, taskID string) error {
	task, err := m.getMigrationTask(ctx, taskID)
	if err != nil {
		return err
	}

	if task.Status == MigrationStatusCompleted {
		return fmt.Errorf("migration already completed")
	}

	task.Status = MigrationStatusCancelled
	m.saveMigrationTask(ctx, task)
	m.publishMigrationEvent(task, "cancelled")

	// Remove from pending
	m.migrationMu.Lock()
	delete(m.pendingMigrations, taskID)
	m.migrationMu.Unlock()

	// Revert RTP state
	if task.StreamState != nil {
		task.StreamState.Status = RTPStreamStatusActive
		task.StreamState.MigrationTarget = ""
		m.rtpStateManager.saveState(task.StreamState)
	}

	m.logger.WithField("task_id", taskID).Info("Stream migration cancelled")
	return nil
}

// GetMigrationStatus returns the status of a migration
func (m *StreamMigrationManager) GetMigrationStatus(ctx context.Context, taskID string) (*MigrationTask, error) {
	return m.getMigrationTask(ctx, taskID)
}

// ListPendingMigrations returns all pending migrations
func (m *StreamMigrationManager) ListPendingMigrations() []*MigrationTask {
	m.migrationMu.RLock()
	defer m.migrationMu.RUnlock()

	tasks := make([]*MigrationTask, 0, len(m.pendingMigrations))
	for _, task := range m.pendingMigrations {
		tasks = append(tasks, task)
	}
	return tasks
}

// saveMigrationTask saves a migration task to Redis
func (m *StreamMigrationManager) saveMigrationTask(ctx context.Context, task *MigrationTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	key := migrationKeyPrefix + task.ID
	return m.redis.Set(ctx, key, data, 24*time.Hour).Err()
}

// getMigrationTask retrieves a migration task from Redis
func (m *StreamMigrationManager) getMigrationTask(ctx context.Context, taskID string) (*MigrationTask, error) {
	key := migrationKeyPrefix + taskID
	data, err := m.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("migration task not found: %s", taskID)
		}
		return nil, err
	}

	var task MigrationTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}

	return &task, nil
}

// queueMigration adds a migration to the processing queue
func (m *StreamMigrationManager) queueMigration(ctx context.Context, task *MigrationTask) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}

	return m.redis.RPush(ctx, migrationQueueKey, data).Err()
}

// publishMigrationEvent publishes a migration event
func (m *StreamMigrationManager) publishMigrationEvent(task *MigrationTask, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	event := map[string]interface{}{
		"action":       action,
		"task_id":      task.ID,
		"call_uuid":    task.CallUUID,
		"source_node":  task.SourceNodeID,
		"target_node":  task.TargetNodeID,
		"status":       task.Status,
		"timestamp":    time.Now().UnixNano(),
	}

	data, _ := json.Marshal(event)
	m.redis.Publish(ctx, migrationChannel, data)
}

// eventLoop handles migration events
func (m *StreamMigrationManager) eventLoop(ctx context.Context) {
	defer m.wg.Done()

	ch := m.pubsub.Channel()
	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		case msg := <-ch:
			if msg == nil {
				continue
			}
			m.handleMigrationEvent(msg.Payload)
		}
	}
}

// handleMigrationEvent processes migration events
func (m *StreamMigrationManager) handleMigrationEvent(payload string) {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}

	action, _ := event["action"].(string)
	taskID, _ := event["task_id"].(string)
	targetNode, _ := event["target_node"].(string)

	// Handle events targeted at this node
	if targetNode == m.nodeID {
		switch action {
		case "pending":
			// New migration request for this node
			go m.processMigrationRequest(taskID)
		case "completed":
			// Migration to this node completed
			m.migrationMu.Lock()
			delete(m.pendingMigrations, taskID)
			m.migrationMu.Unlock()
		}
	}

	sourceNode, _ := event["source_node"].(string)
	if sourceNode == m.nodeID {
		switch action {
		case "completed":
			// Our outgoing migration completed
			m.handleOutgoingMigrationComplete(taskID)
		case "failed":
			// Our outgoing migration failed
			m.handleOutgoingMigrationFailed(taskID)
		}
	}
}

// processMigrationRequest processes an incoming migration request
func (m *StreamMigrationManager) processMigrationRequest(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), migrationTimeout)
	defer cancel()

	if err := m.AcceptMigration(ctx, taskID); err != nil {
		m.logger.WithError(err).WithField("task_id", taskID).Error("Failed to accept migration")
	}
}

// handleOutgoingMigrationComplete handles completion of outgoing migration
func (m *StreamMigrationManager) handleOutgoingMigrationComplete(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := m.getMigrationTask(ctx, taskID)
	if err != nil {
		return
	}

	// Clean up local resources
	m.rtpStateManager.UnregisterStream(task.CallUUID)

	m.migrationMu.Lock()
	delete(m.pendingMigrations, taskID)
	m.migrationMu.Unlock()

	m.logger.WithFields(logrus.Fields{
		"task_id":     taskID,
		"call_uuid":   task.CallUUID,
		"target_node": task.TargetNodeID,
	}).Info("Outgoing migration completed, local resources cleaned up")
}

// handleOutgoingMigrationFailed handles failure of outgoing migration
func (m *StreamMigrationManager) handleOutgoingMigrationFailed(taskID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := m.getMigrationTask(ctx, taskID)
	if err != nil {
		return
	}

	// Check retry count
	if task.Retries < maxMigrationRetries {
		task.Retries++
		task.Status = MigrationStatusPending
		m.saveMigrationTask(ctx, task)
		m.queueMigration(ctx, task)
		m.logger.WithFields(logrus.Fields{
			"task_id": taskID,
			"retry":   task.Retries,
		}).Warn("Retrying failed migration")
	} else {
		// Max retries reached, restore local state
		task.Status = MigrationStatusFailed
		m.saveMigrationTask(ctx, task)

		if task.StreamState != nil {
			task.StreamState.Status = RTPStreamStatusActive
			task.StreamState.MigrationTarget = ""
			m.rtpStateManager.saveState(task.StreamState)
		}

		m.migrationMu.Lock()
		delete(m.pendingMigrations, taskID)
		m.migrationMu.Unlock()

		m.logger.WithField("task_id", taskID).Error("Migration failed after max retries")
	}
}

// queueProcessor processes the migration queue
func (m *StreamMigrationManager) queueProcessor(ctx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		default:
			// Try to get next migration from queue
			data, err := m.redis.BLPop(ctx, time.Second, migrationQueueKey).Result()
			if err != nil {
				if err == redis.Nil || err == context.DeadlineExceeded {
					continue
				}
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if len(data) < 2 {
				continue
			}

			var task MigrationTask
			if err := json.Unmarshal([]byte(data[1]), &task); err != nil {
				continue
			}

			// Only process if targeted at this node
			if task.TargetNodeID == m.nodeID {
				go m.processMigrationRequest(task.ID)
			} else {
				// Re-publish for the target node
				m.publishMigrationEvent(&task, "pending")
			}
		}
	}
}

// GetStats returns migration manager statistics
func (m *StreamMigrationManager) GetStats() map[string]interface{} {
	m.migrationMu.RLock()
	pendingCount := len(m.pendingMigrations)
	m.migrationMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Count queue size
	queueSize, _ := m.redis.LLen(ctx, migrationQueueKey).Result()

	return map[string]interface{}{
		"pending_migrations": pendingCount,
		"queue_size":         queueSize,
		"node_id":            m.nodeID,
	}
}

// MigrateAllStreams migrates all streams from this node (for graceful shutdown)
func (m *StreamMigrationManager) MigrateAllStreams(ctx context.Context, targetNodeID string) error {
	streams := m.rtpStateManager.ListLocalStreams()
	if len(streams) == 0 {
		return nil
	}

	m.logger.WithFields(logrus.Fields{
		"stream_count": len(streams),
		"target_node":  targetNodeID,
	}).Info("Migrating all streams for graceful shutdown")

	var failed int
	for _, stream := range streams {
		_, err := m.InitiateMigration(ctx, stream.CallUUID, targetNodeID)
		if err != nil {
			m.logger.WithError(err).WithField("call_uuid", stream.CallUUID).Error("Failed to initiate migration")
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("failed to migrate %d of %d streams", failed, len(streams))
	}

	return nil
}

// WaitForMigrations waits for all pending migrations to complete
func (m *StreamMigrationManager) WaitForMigrations(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.migrationMu.RLock()
			pending := len(m.pendingMigrations)
			m.migrationMu.RUnlock()

			if pending == 0 {
				return nil
			}

			m.logger.WithField("pending", pending).Debug("Waiting for migrations to complete")
		}
	}
}

// AllocatePortsForMigration allocates RTP/RTCP ports for an incoming migration
func AllocatePortsForMigration() (rtpPort, rtcpPort int, err error) {
	// Find available UDP port for RTP
	rtpListener, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to allocate RTP port: %w", err)
	}
	rtpPort = rtpListener.LocalAddr().(*net.UDPAddr).Port
	rtpListener.Close()

	// Use RTP port + 1 for RTCP (standard convention)
	rtcpPort = rtpPort + 1

	return rtpPort, rtcpPort, nil
}
