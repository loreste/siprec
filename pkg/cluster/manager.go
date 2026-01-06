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

// Config holds cluster configuration
type Config struct {
	Enabled           bool
	NodeID            string
	HeartbeatInterval time.Duration
	NodeTTL           time.Duration

	// Leader election configuration
	LeaderElectionEnabled bool
	LeaderLockTTL         time.Duration // How long the leader lock is held (default: 10s)
	LeaderRetryInterval   time.Duration // How often non-leaders try to acquire (default: 3s)
}

// NodeInfo represents information about a cluster node
type NodeInfo struct {
	ID        string            `json:"id"`
	Hostname  string            `json:"hostname"`
	StartedAt time.Time         `json:"started_at"`
	LastSeen  time.Time         `json:"last_seen"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// Manager handles cluster node registration and discovery
type Manager struct {
	config   Config
	redis    redis.UniversalClient
	logger   *logrus.Logger
	stopChan chan struct{}
	wg       sync.WaitGroup
	nodeInfo NodeInfo
	mu       sync.RWMutex

	// Leader election state
	isLeader         bool
	leaderMu         sync.RWMutex
	leaderCallbacks  []func(isLeader bool)
	callbacksMu      sync.RWMutex
}

// LeadershipCallback is called when leadership status changes
type LeadershipCallback func(isLeader bool)

// NewManager creates a new cluster manager
func NewManager(config Config, redisClient redis.UniversalClient, logger *logrus.Logger, hostname string) *Manager {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 5 * time.Second
	}
	if config.NodeTTL == 0 {
		config.NodeTTL = 15 * time.Second
	}
	if config.LeaderLockTTL == 0 {
		config.LeaderLockTTL = 10 * time.Second
	}
	if config.LeaderRetryInterval == 0 {
		config.LeaderRetryInterval = 3 * time.Second
	}

	return &Manager{
		config:          config,
		redis:           redisClient,
		logger:          logger,
		stopChan:        make(chan struct{}),
		leaderCallbacks: make([]func(isLeader bool), 0),
		nodeInfo: NodeInfo{
			ID:        config.NodeID,
			Hostname:  hostname,
			StartedAt: time.Now(),
		},
	}
}

// Start begins the heartbeat process and leader election
func (m *Manager) Start() error {
	if !m.config.Enabled {
		return nil
	}

	m.logger.WithFields(logrus.Fields{
		"node_id":           m.config.NodeID,
		"heartbeat_interval": m.config.HeartbeatInterval,
		"leader_election":   m.config.LeaderElectionEnabled,
	}).Info("Starting cluster manager")

	// Register immediately
	if err := m.sendHeartbeat(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	m.wg.Add(1)
	go m.heartbeatLoop()

	// Start leader election if enabled
	if m.config.LeaderElectionEnabled {
		m.wg.Add(1)
		go m.leaderElectionLoop()
		m.logger.WithFields(logrus.Fields{
			"lock_ttl":       m.config.LeaderLockTTL,
			"retry_interval": m.config.LeaderRetryInterval,
		}).Info("Leader election enabled")
	}

	return nil
}

// Stop stops the heartbeat process and deregisters the node
func (m *Manager) Stop() {
	if !m.config.Enabled {
		return
	}

	close(m.stopChan)
	m.wg.Wait()

	// Best-effort deregistration and leader release
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Release leadership if we hold it
	if m.IsLeader() {
		m.releaseLeadership(ctx)
	}

	key := m.nodeKey(m.config.NodeID)
	m.redis.Del(ctx, key)

	m.logger.Info("Cluster manager stopped")
}

// ListNodes returns all active nodes in the cluster
func (m *Manager) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	pattern := "siprec:nodes:*"
	keys, err := m.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	nodes := make([]NodeInfo, 0, len(keys))
	for _, key := range keys {
		data, err := m.redis.Get(ctx, key).Bytes()
		if err != nil {
			continue // key might have expired
		}

		var node NodeInfo
		if err := json.Unmarshal(data, &node); err != nil {
			m.logger.WithError(err).WithField("key", key).Warn("Failed to unmarshal node info")
			continue
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// heartbeatLoop sends periodic heartbeats
func (m *Manager) heartbeatLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if err := m.sendHeartbeat(); err != nil {
				m.logger.WithError(err).Error("Failed to send cluster heartbeat")
			}
		}
	}
}

// sendHeartbeat updates the node's presence in Redis
func (m *Manager) sendHeartbeat() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.mu.Lock()
	m.nodeInfo.LastSeen = time.Now()
	data, err := json.Marshal(m.nodeInfo)
	m.mu.Unlock()

	if err != nil {
		return err
	}

	key := m.nodeKey(m.config.NodeID)
	return m.redis.Set(ctx, key, data, m.config.NodeTTL).Err()
}

func (m *Manager) nodeKey(nodeID string) string {
	return fmt.Sprintf("siprec:nodes:%s", nodeID)
}

// Leader Election Methods

const leaderLockKey = "siprec:leader:lock"

// IsLeader returns true if this node is the current cluster leader
func (m *Manager) IsLeader() bool {
	m.leaderMu.RLock()
	defer m.leaderMu.RUnlock()
	return m.isLeader
}

// GetLeader returns the current leader's node ID, or empty string if no leader
func (m *Manager) GetLeader(ctx context.Context) (string, error) {
	leaderID, err := m.redis.Get(ctx, leaderLockKey).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return leaderID, nil
}

// OnLeadershipChange registers a callback that is called when leadership status changes
func (m *Manager) OnLeadershipChange(callback func(isLeader bool)) {
	m.callbacksMu.Lock()
	defer m.callbacksMu.Unlock()
	m.leaderCallbacks = append(m.leaderCallbacks, callback)
}

// RunIfLeader executes the given function only if this node is the leader
// Returns true if the function was executed, false if not leader
func (m *Manager) RunIfLeader(fn func()) bool {
	if !m.IsLeader() {
		return false
	}
	fn()
	return true
}

// RunIfLeaderWithContext executes the given function only if this node is the leader
// Returns the result of the function or ErrNotLeader if not leader
func (m *Manager) RunIfLeaderWithContext(ctx context.Context, fn func(ctx context.Context) error) error {
	if !m.IsLeader() {
		return ErrNotLeader
	}
	return fn(ctx)
}

// ErrNotLeader is returned when an operation requires leadership but this node is not the leader
var ErrNotLeader = fmt.Errorf("this node is not the cluster leader")

// leaderElectionLoop continuously tries to acquire or maintain leadership
func (m *Manager) leaderElectionLoop() {
	defer m.wg.Done()

	// Try to acquire leadership immediately
	m.tryAcquireLeadership()

	// Use different intervals for leader (renewal) vs non-leader (retry)
	ticker := time.NewTicker(m.config.LeaderRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			if m.IsLeader() {
				// Leader: try to renew the lock
				m.tryRenewLeadership()
				// Check more frequently as leader to maintain lock
				ticker.Reset(m.config.LeaderLockTTL / 3)
			} else {
				// Non-leader: try to acquire
				m.tryAcquireLeadership()
				ticker.Reset(m.config.LeaderRetryInterval)
			}
		}
	}
}

// tryAcquireLeadership attempts to become the leader
func (m *Manager) tryAcquireLeadership() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to set the leader lock with NX (only if not exists)
	// Using SET with NX and EX for atomic operation
	success, err := m.redis.SetNX(ctx, leaderLockKey, m.config.NodeID, m.config.LeaderLockTTL).Result()
	if err != nil {
		m.logger.WithError(err).Error("Failed to attempt leader election")
		return
	}

	if success {
		m.setLeaderStatus(true)
		m.logger.WithField("node_id", m.config.NodeID).Info("This node is now the cluster leader")
	}
}

// tryRenewLeadership attempts to renew the leadership lock
func (m *Manager) tryRenewLeadership() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Check if we still hold the lock
	currentLeader, err := m.redis.Get(ctx, leaderLockKey).Result()
	if err == redis.Nil {
		// Lock expired, try to reacquire
		m.tryAcquireLeadership()
		return
	}
	if err != nil {
		m.logger.WithError(err).Error("Failed to check leader lock")
		return
	}

	if currentLeader != m.config.NodeID {
		// Someone else is leader now
		m.setLeaderStatus(false)
		m.logger.WithField("new_leader", currentLeader).Info("Lost leadership to another node")
		return
	}

	// Renew the lock by extending TTL
	err = m.redis.Expire(ctx, leaderLockKey, m.config.LeaderLockTTL).Err()
	if err != nil {
		m.logger.WithError(err).Error("Failed to renew leader lock")
		// Don't immediately give up leadership, will retry
	}
}

// releaseLeadership voluntarily releases the leadership lock
func (m *Manager) releaseLeadership(ctx context.Context) {
	// Only delete if we are the current holder (use Lua script for atomicity)
	script := redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`)

	_, err := script.Run(ctx, m.redis, []string{leaderLockKey}, m.config.NodeID).Result()
	if err != nil {
		m.logger.WithError(err).Error("Failed to release leader lock")
	} else {
		m.setLeaderStatus(false)
		m.logger.Info("Released cluster leadership")
	}
}

// setLeaderStatus updates the leader status and notifies callbacks
func (m *Manager) setLeaderStatus(isLeader bool) {
	m.leaderMu.Lock()
	wasLeader := m.isLeader
	m.isLeader = isLeader
	m.leaderMu.Unlock()

	// Only notify if status changed
	if wasLeader != isLeader {
		m.notifyLeadershipChange(isLeader)
	}
}

// notifyLeadershipChange calls all registered callbacks
func (m *Manager) notifyLeadershipChange(isLeader bool) {
	m.callbacksMu.RLock()
	callbacks := make([]func(isLeader bool), len(m.leaderCallbacks))
	copy(callbacks, m.leaderCallbacks)
	m.callbacksMu.RUnlock()

	for _, callback := range callbacks {
		go func(cb func(bool)) {
			defer func() {
				if r := recover(); r != nil {
					m.logger.WithField("panic", r).Error("Leadership callback panicked")
				}
			}()
			cb(isLeader)
		}(callback)
	}
}

// ForceReleaseLeadership forces this node to give up leadership (e.g., for graceful shutdown)
func (m *Manager) ForceReleaseLeadership() {
	if !m.IsLeader() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.releaseLeadership(ctx)
}

// GetClusterStatus returns information about the cluster state
func (m *Manager) GetClusterStatus(ctx context.Context) (*ClusterStatus, error) {
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	leader, err := m.GetLeader(ctx)
	if err != nil {
		return nil, err
	}

	return &ClusterStatus{
		NodeCount:     len(nodes),
		Nodes:         nodes,
		LeaderID:      leader,
		IsThisLeader:  m.IsLeader(),
		ThisNodeID:    m.config.NodeID,
	}, nil
}

// ClusterStatus represents the current state of the cluster
type ClusterStatus struct {
	NodeCount    int        `json:"node_count"`
	Nodes        []NodeInfo `json:"nodes"`
	LeaderID     string     `json:"leader_id"`
	IsThisLeader bool       `json:"is_this_leader"`
	ThisNodeID   string     `json:"this_node_id"`
}
