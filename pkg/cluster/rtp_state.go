package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	rtpStateKeyPrefix     = "siprec:rtp:state:"
	rtpStateTTL           = 5 * time.Minute
	rtpStateUpdateChannel = "siprec:rtp:updates"
)

// RTPStreamState represents the state of an RTP stream for cluster replication
type RTPStreamState struct {
	CallUUID        string            `json:"call_uuid"`
	SessionID       string            `json:"session_id"`
	NodeID          string            `json:"node_id"`
	LocalPort       int               `json:"local_port"`
	RTCPPort        int               `json:"rtcp_port,omitempty"`
	RemoteAddr      string            `json:"remote_addr,omitempty"`
	LocalSSRC       uint32            `json:"local_ssrc"`
	RemoteSSRC      uint32            `json:"remote_ssrc"`
	CodecName       string            `json:"codec_name"`
	CodecPayloadType uint8            `json:"codec_payload_type"`
	SampleRate      int               `json:"sample_rate"`
	Channels        int               `json:"channels"`
	RecordingPath   string            `json:"recording_path,omitempty"`
	RecordingPaused bool              `json:"recording_paused"`
	SRTPEnabled     bool              `json:"srtp_enabled"`
	StartTime       time.Time         `json:"start_time"`
	LastPacketTime  time.Time         `json:"last_packet_time"`
	PacketsReceived int64             `json:"packets_received"`
	BytesReceived   int64             `json:"bytes_received"`
	PacketsLost     int64             `json:"packets_lost"`
	Jitter          float64           `json:"jitter"`
	Status          RTPStreamStatus   `json:"status"`
	Metadata        map[string]string `json:"metadata,omitempty"`

	// For stream migration
	MigrationTarget  string    `json:"migration_target,omitempty"`
	MigrationStarted time.Time `json:"migration_started,omitempty"`
}

// RTPStreamStatus represents the status of an RTP stream
type RTPStreamStatus string

const (
	RTPStreamStatusActive     RTPStreamStatus = "active"
	RTPStreamStatusPaused     RTPStreamStatus = "paused"
	RTPStreamStatusMigrating  RTPStreamStatus = "migrating"
	RTPStreamStatusTerminated RTPStreamStatus = "terminated"
)

// RTPStateManager manages RTP stream state replication across cluster
type RTPStateManager struct {
	redis    redis.UniversalClient
	logger   *logrus.Logger
	nodeID   string
	stopChan chan struct{}
	wg       sync.WaitGroup

	// Local state cache
	localStates map[string]*RTPStreamState
	stateMu     sync.RWMutex

	// Update callbacks
	updateCallbacks []func(state *RTPStreamState)
	callbackMu      sync.RWMutex

	// Pub/sub for real-time updates
	pubsub *redis.PubSub
}

// NewRTPStateManager creates a new RTP state manager
func NewRTPStateManager(redisClient redis.UniversalClient, nodeID string, logger *logrus.Logger) *RTPStateManager {
	return &RTPStateManager{
		redis:       redisClient,
		logger:      logger,
		nodeID:      nodeID,
		stopChan:    make(chan struct{}),
		localStates: make(map[string]*RTPStreamState),
	}
}

// Start begins the state manager
func (m *RTPStateManager) Start(ctx context.Context) error {
	// Subscribe to state updates
	m.pubsub = m.redis.Subscribe(ctx, rtpStateUpdateChannel)

	m.wg.Add(1)
	go m.subscriptionLoop(ctx)

	m.wg.Add(1)
	go m.syncLoop(ctx)

	m.logger.WithField("node_id", m.nodeID).Info("RTP state manager started")
	return nil
}

// Stop stops the state manager
func (m *RTPStateManager) Stop() {
	close(m.stopChan)
	if m.pubsub != nil {
		if err := m.pubsub.Close(); err != nil {
			m.logger.WithError(err).Warn("Error closing pubsub")
		}
	}
	m.wg.Wait()
	m.logger.Info("RTP state manager stopped")
}

// RegisterStream registers a new RTP stream and replicates state
func (m *RTPStateManager) RegisterStream(state *RTPStreamState) error {
	if state.CallUUID == "" {
		return fmt.Errorf("call_uuid is required")
	}

	state.NodeID = m.nodeID
	state.Status = RTPStreamStatusActive

	// Store locally
	m.stateMu.Lock()
	m.localStates[state.CallUUID] = state
	m.stateMu.Unlock()

	// Replicate to Redis
	if err := m.saveState(state); err != nil {
		return fmt.Errorf("failed to replicate RTP state: %w", err)
	}

	// Publish update
	m.publishUpdate(state, "register")

	m.logger.WithFields(logrus.Fields{
		"call_uuid":  state.CallUUID,
		"local_port": state.LocalPort,
		"codec":      state.CodecName,
	}).Debug("RTP stream registered in cluster")

	return nil
}

// UpdateStream updates an RTP stream state
func (m *RTPStateManager) UpdateStream(callUUID string, updates map[string]interface{}) error {
	m.stateMu.Lock()
	state, exists := m.localStates[callUUID]
	if !exists {
		m.stateMu.Unlock()
		return fmt.Errorf("stream not found: %s", callUUID)
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "packets_received":
			if v, ok := value.(int64); ok {
				state.PacketsReceived = v
			}
		case "bytes_received":
			if v, ok := value.(int64); ok {
				state.BytesReceived = v
			}
		case "packets_lost":
			if v, ok := value.(int64); ok {
				state.PacketsLost = v
			}
		case "jitter":
			if v, ok := value.(float64); ok {
				state.Jitter = v
			}
		case "last_packet_time":
			if v, ok := value.(time.Time); ok {
				state.LastPacketTime = v
			}
		case "recording_paused":
			if v, ok := value.(bool); ok {
				state.RecordingPaused = v
				if v {
					state.Status = RTPStreamStatusPaused
				} else {
					state.Status = RTPStreamStatusActive
				}
			}
		case "status":
			if v, ok := value.(RTPStreamStatus); ok {
				state.Status = v
			}
		case "remote_addr":
			if v, ok := value.(string); ok {
				state.RemoteAddr = v
			}
		case "remote_ssrc":
			if v, ok := value.(uint32); ok {
				state.RemoteSSRC = v
			}
		}
	}
	m.stateMu.Unlock()

	// Replicate to Redis
	if err := m.saveState(state); err != nil {
		return fmt.Errorf("failed to update RTP state: %w", err)
	}

	// Publish update
	m.publishUpdate(state, "update")

	return nil
}

// UnregisterStream removes an RTP stream
func (m *RTPStateManager) UnregisterStream(callUUID string) error {
	m.stateMu.Lock()
	state, exists := m.localStates[callUUID]
	if exists {
		state.Status = RTPStreamStatusTerminated
		delete(m.localStates, callUUID)
	}
	m.stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Remove from Redis
	key := rtpStateKeyPrefix + callUUID
	if err := m.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to remove RTP state: %w", err)
	}

	// Publish removal
	if state != nil {
		m.publishUpdate(state, "unregister")
	}

	m.logger.WithField("call_uuid", callUUID).Debug("RTP stream unregistered from cluster")
	return nil
}

// GetStream retrieves an RTP stream state
func (m *RTPStateManager) GetStream(callUUID string) (*RTPStreamState, error) {
	// Check local cache first
	m.stateMu.RLock()
	if state, exists := m.localStates[callUUID]; exists {
		stateCopy := *state
		m.stateMu.RUnlock()
		return &stateCopy, nil
	}
	m.stateMu.RUnlock()

	// Fetch from Redis
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := rtpStateKeyPrefix + callUUID
	data, err := m.redis.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("stream not found: %s", callUUID)
		}
		return nil, fmt.Errorf("failed to get RTP state: %w", err)
	}

	var state RTPStreamState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal RTP state: %w", err)
	}

	return &state, nil
}

// ListStreams returns all active streams
func (m *RTPStateManager) ListStreams(ctx context.Context, nodeID string) ([]*RTPStreamState, error) {
	pattern := rtpStateKeyPrefix + "*"
	keys, err := m.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list RTP states: %w", err)
	}

	if len(keys) == 0 {
		return []*RTPStreamState{}, nil
	}

	// Batch get
	pipe := m.redis.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to batch get RTP states: %w", err)
	}

	var states []*RTPStreamState
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}

		var state RTPStreamState
		if err := json.Unmarshal(data, &state); err != nil {
			continue
		}

		// Filter by node if specified
		if nodeID != "" && state.NodeID != nodeID {
			continue
		}

		states = append(states, &state)
	}

	return states, nil
}

// ListLocalStreams returns streams owned by this node
func (m *RTPStateManager) ListLocalStreams() []*RTPStreamState {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()

	states := make([]*RTPStreamState, 0, len(m.localStates))
	for _, state := range m.localStates {
		stateCopy := *state
		states = append(states, &stateCopy)
	}
	return states
}

// OnStateUpdate registers a callback for state updates
func (m *RTPStateManager) OnStateUpdate(callback func(state *RTPStreamState)) {
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	m.updateCallbacks = append(m.updateCallbacks, callback)
}

// InitiateMigration starts migrating a stream to another node
func (m *RTPStateManager) InitiateMigration(callUUID, targetNodeID string) error {
	m.stateMu.Lock()
	state, exists := m.localStates[callUUID]
	if !exists {
		m.stateMu.Unlock()
		return fmt.Errorf("stream not found locally: %s", callUUID)
	}

	state.Status = RTPStreamStatusMigrating
	state.MigrationTarget = targetNodeID
	state.MigrationStarted = time.Now()
	m.stateMu.Unlock()

	// Save state
	if err := m.saveState(state); err != nil {
		return fmt.Errorf("failed to save migration state: %w", err)
	}

	// Publish migration event
	m.publishUpdate(state, "migrate")

	m.logger.WithFields(logrus.Fields{
		"call_uuid":     callUUID,
		"target_node":   targetNodeID,
	}).Info("RTP stream migration initiated")

	return nil
}

// CompleteMigration completes the migration on the target node
func (m *RTPStateManager) CompleteMigration(callUUID string, newState *RTPStreamState) error {
	newState.NodeID = m.nodeID
	newState.Status = RTPStreamStatusActive
	newState.MigrationTarget = ""
	newState.MigrationStarted = time.Time{}

	// Store locally
	m.stateMu.Lock()
	m.localStates[callUUID] = newState
	m.stateMu.Unlock()

	// Save to Redis
	if err := m.saveState(newState); err != nil {
		return fmt.Errorf("failed to save migrated state: %w", err)
	}

	// Publish completion
	m.publishUpdate(newState, "migrate_complete")

	m.logger.WithFields(logrus.Fields{
		"call_uuid":  callUUID,
		"local_port": newState.LocalPort,
	}).Info("RTP stream migration completed")

	return nil
}

// saveState saves state to Redis
func (m *RTPStateManager) saveState(state *RTPStreamState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	key := rtpStateKeyPrefix + state.CallUUID
	return m.redis.Set(ctx, key, data, rtpStateTTL).Err()
}

// publishUpdate publishes a state update to other nodes
func (m *RTPStateManager) publishUpdate(state *RTPStreamState, action string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	update := map[string]interface{}{
		"action":    action,
		"call_uuid": state.CallUUID,
		"node_id":   state.NodeID,
		"timestamp": time.Now().UnixNano(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		m.logger.WithError(err).Error("Failed to marshal state update")
		return
	}

	if err := m.redis.Publish(ctx, rtpStateUpdateChannel, data).Err(); err != nil {
		m.logger.WithError(err).Error("Failed to publish state update")
	}
}

// subscriptionLoop handles incoming state updates
func (m *RTPStateManager) subscriptionLoop(ctx context.Context) {
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
			m.handleUpdate(msg.Payload)
		}
	}
}

// handleUpdate processes an incoming state update
func (m *RTPStateManager) handleUpdate(payload string) {
	var update map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &update); err != nil {
		return
	}

	nodeID, _ := update["node_id"].(string)
	if nodeID == m.nodeID {
		return // Ignore our own updates
	}

	callUUID, _ := update["call_uuid"].(string)
	action, _ := update["action"].(string)

	m.logger.WithFields(logrus.Fields{
		"action":    action,
		"call_uuid": callUUID,
		"from_node": nodeID,
	}).Debug("Received RTP state update from cluster")

	// Fetch updated state if needed
	if action == "migrate" {
		state, err := m.GetStream(callUUID)
		if err == nil && state.MigrationTarget == m.nodeID {
			// We are the migration target
			m.notifyCallbacks(state)
		}
	}
}

// syncLoop periodically syncs local state to Redis
func (m *RTPStateManager) syncLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.syncLocalStates()
		}
	}
}

// syncLocalStates syncs all local states to Redis
func (m *RTPStateManager) syncLocalStates() {
	m.stateMu.RLock()
	states := make([]*RTPStreamState, 0, len(m.localStates))
	for _, state := range m.localStates {
		states = append(states, state)
	}
	m.stateMu.RUnlock()

	for _, state := range states {
		if err := m.saveState(state); err != nil {
			m.logger.WithError(err).WithField("call_uuid", state.CallUUID).Error("Failed to sync RTP state")
		}
	}
}

// notifyCallbacks calls all registered callbacks
func (m *RTPStateManager) notifyCallbacks(state *RTPStreamState) {
	m.callbackMu.RLock()
	callbacks := make([]func(*RTPStreamState), len(m.updateCallbacks))
	copy(callbacks, m.updateCallbacks)
	m.callbackMu.RUnlock()

	for _, cb := range callbacks {
		go func(callback func(*RTPStreamState)) {
			defer func() {
				if r := recover(); r != nil {
					m.logger.WithField("panic", r).Error("RTP state callback panicked")
				}
			}()
			callback(state)
		}(cb)
	}
}

// GetStats returns statistics about RTP state management
func (m *RTPStateManager) GetStats() map[string]interface{} {
	m.stateMu.RLock()
	localCount := len(m.localStates)
	m.stateMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Count total streams in cluster
	pattern := rtpStateKeyPrefix + "*"
	keys, _ := m.redis.Keys(ctx, pattern).Result()

	return map[string]interface{}{
		"local_streams":   localCount,
		"cluster_streams": len(keys),
		"node_id":         m.nodeID,
	}
}
