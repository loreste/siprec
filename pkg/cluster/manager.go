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
}

// NewManager creates a new cluster manager
func NewManager(config Config, redisClient redis.UniversalClient, logger *logrus.Logger, hostname string) *Manager {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 5 * time.Second
	}
	if config.NodeTTL == 0 {
		config.NodeTTL = 15 * time.Second
	}

	return &Manager{
		config:   config,
		redis:    redisClient,
		logger:   logger,
		stopChan: make(chan struct{}),
		nodeInfo: NodeInfo{
			ID:        config.NodeID,
			Hostname:  hostname,
			StartedAt: time.Now(),
		},
	}
}

// Start begins the heartbeat process
func (m *Manager) Start() error {
	if !m.config.Enabled {
		return nil
	}

	m.logger.WithFields(logrus.Fields{
		"node_id":  m.config.NodeID,
		"interval": m.config.HeartbeatInterval,
	}).Info("Starting cluster manager")

	// Register immediately
	if err := m.sendHeartbeat(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	m.wg.Add(1)
	go m.heartbeatLoop()

	return nil
}

// Stop stops the heartbeat process and deregisters the node
func (m *Manager) Stop() {
	if !m.config.Enabled {
		return
	}

	close(m.stopChan)
	m.wg.Wait()

	// Best-effort deregistration
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

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
