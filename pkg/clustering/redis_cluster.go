package clustering

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"siprec-server/pkg/database"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// RedisCluster manages distributed session state using Redis
type RedisCluster struct {
	client     redis.UniversalClient
	logger     *logrus.Logger
	keyPrefix  string
	nodeID     string
	heartbeat  time.Duration
	sessionTTL time.Duration
	mutex      sync.RWMutex
	isLeader   bool
	nodes      map[string]*ClusterNode
}

// ClusterNode represents a node in the cluster
type ClusterNode struct {
	ID             string    `json:"id"`
	Address        string    `json:"address"`
	Status         string    `json:"status"` // active, inactive, failed
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	Version        string    `json:"version"`
	LoadFactor     float64   `json:"load_factor"`
	ActiveSessions int       `json:"active_sessions"`
	IsLeader       bool      `json:"is_leader"`
}

// ClusteredSession represents a session stored in Redis
type ClusteredSession struct {
	SessionID     string                 `json:"session_id"`
	CallID        string                 `json:"call_id"`
	NodeID        string                 `json:"node_id"`
	Status        string                 `json:"status"`
	StartTime     time.Time              `json:"start_time"`
	LastUpdate    time.Time              `json:"last_update"`
	Metadata      map[string]interface{} `json:"metadata"`
	Participants  []string               `json:"participants"`
	RecordingPath string                 `json:"recording_path"`
	Transport     string                 `json:"transport"`
	SourceIP      string                 `json:"source_ip"`
}

// RedisConfig holds Redis cluster configuration
type RedisConfig struct {
	Addresses    []string
	Password     string
	Database     int
	MasterName   string // For Redis Sentinel
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolTimeout  time.Duration
}

// NewRedisCluster creates a new Redis cluster manager
func NewRedisCluster(config RedisConfig, nodeID string, logger *logrus.Logger) (*RedisCluster, error) {
	var client redis.UniversalClient

	// Configure Redis client options
	opts := &redis.UniversalOptions{
		Addrs:        config.Addresses,
		Password:     config.Password,
		DB:           config.Database,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PoolTimeout:  config.PoolTimeout,
	}

	// Use Sentinel if MasterName is provided
	if config.MasterName != "" {
		opts.MasterName = config.MasterName
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       config.MasterName,
			SentinelAddrs:    config.Addresses,
			SentinelPassword: config.Password,
			Password:         config.Password,
			DB:               config.Database,
			PoolSize:         config.PoolSize,
			MinIdleConns:     config.MinIdleConns,
			DialTimeout:      config.DialTimeout,
			ReadTimeout:      config.ReadTimeout,
			WriteTimeout:     config.WriteTimeout,
			PoolTimeout:      config.PoolTimeout,
		})
	} else if len(config.Addresses) > 1 {
		// Use Redis Cluster
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        config.Addresses,
			Password:     config.Password,
			PoolSize:     config.PoolSize,
			MinIdleConns: config.MinIdleConns,
			DialTimeout:  config.DialTimeout,
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
			PoolTimeout:  config.PoolTimeout,
		})
	} else {
		// Use single Redis instance
		client = redis.NewClient(&redis.Options{
			Addr:         config.Addresses[0],
			Password:     config.Password,
			DB:           config.Database,
			PoolSize:     config.PoolSize,
			MinIdleConns: config.MinIdleConns,
			DialTimeout:  config.DialTimeout,
			ReadTimeout:  config.ReadTimeout,
			WriteTimeout: config.WriteTimeout,
			PoolTimeout:  config.PoolTimeout,
		})
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	cluster := &RedisCluster{
		client:     client,
		logger:     logger,
		keyPrefix:  "siprec:",
		nodeID:     nodeID,
		heartbeat:  30 * time.Second,
		sessionTTL: 24 * time.Hour,
		nodes:      make(map[string]*ClusterNode),
	}

	// Start cluster management routines
	go cluster.startHeartbeat()
	go cluster.startLeaderElection()
	go cluster.startSessionMonitoring()

	logger.WithFields(logrus.Fields{
		"node_id":   nodeID,
		"addresses": config.Addresses,
	}).Info("Redis cluster initialized")

	return cluster, nil
}

// Session Management

// StoreSession stores a session in Redis
func (r *RedisCluster) StoreSession(session *database.Session) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clusteredSession := &ClusteredSession{
		SessionID:     session.SessionID,
		CallID:        session.CallID,
		NodeID:        r.nodeID,
		Status:        session.Status,
		StartTime:     session.StartTime,
		LastUpdate:    time.Now(),
		RecordingPath: session.RecordingPath,
		Transport:     session.Transport,
		SourceIP:      session.SourceIP,
		Metadata:      make(map[string]interface{}),
		Participants:  []string{},
	}

	// Serialize session
	data, err := json.Marshal(clusteredSession)
	if err != nil {
		return fmt.Errorf("failed to serialize session: %w", err)
	}

	// Store in Redis with TTL
	key := r.sessionKey(session.SessionID)
	if err := r.client.Set(ctx, key, data, r.sessionTTL).Err(); err != nil {
		return fmt.Errorf("failed to store session in Redis: %w", err)
	}

	// Add to session index
	if err := r.addToSessionIndex(session.SessionID, session.CallID); err != nil {
		r.logger.WithError(err).Warning("Failed to add session to index")
	}

	r.logger.WithFields(logrus.Fields{
		"session_id": session.SessionID,
		"call_id":    session.CallID,
		"node_id":    r.nodeID,
	}).Debug("Session stored in cluster")

	return nil
}

// GetSession retrieves a session from Redis
func (r *RedisCluster) GetSession(sessionID string) (*ClusteredSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("failed to get session from Redis: %w", err)
	}

	var session ClusteredSession
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, fmt.Errorf("failed to deserialize session: %w", err)
	}

	return &session, nil
}

// UpdateSession updates a session in Redis
func (r *RedisCluster) UpdateSession(sessionID string, updates map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get current session
	session, err := r.GetSession(sessionID)
	if err != nil {
		return err
	}

	// Apply updates
	session.LastUpdate = time.Now()
	for key, value := range updates {
		switch key {
		case "status":
			if status, ok := value.(string); ok {
				session.Status = status
			}
		case "recording_path":
			if path, ok := value.(string); ok {
				session.RecordingPath = path
			}
		case "participants":
			if participants, ok := value.([]string); ok {
				session.Participants = participants
			}
		default:
			session.Metadata[key] = value
		}
	}

	// Store updated session
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to serialize updated session: %w", err)
	}

	key := r.sessionKey(sessionID)
	if err := r.client.Set(ctx, key, data, r.sessionTTL).Err(); err != nil {
		return fmt.Errorf("failed to update session in Redis: %w", err)
	}

	r.logger.WithField("session_id", sessionID).Debug("Session updated in cluster")
	return nil
}

// DeleteSession removes a session from Redis
func (r *RedisCluster) DeleteSession(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete session from Redis: %w", err)
	}

	// Remove from session index
	if err := r.removeFromSessionIndex(sessionID); err != nil {
		r.logger.WithError(err).Warning("Failed to remove session from index")
	}

	r.logger.WithField("session_id", sessionID).Debug("Session deleted from cluster")
	return nil
}

// ListSessions returns all active sessions
func (r *RedisCluster) ListSessions() ([]*ClusteredSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get all session keys
	pattern := r.keyPrefix + "session:*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list session keys: %w", err)
	}

	if len(keys) == 0 {
		return []*ClusteredSession{}, nil
	}

	// Get all sessions in batch
	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to execute batch get: %w", err)
	}

	// Parse sessions
	var sessions []*ClusteredSession
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			continue // Skip failed sessions
		}

		var session ClusteredSession
		if err := json.Unmarshal([]byte(data), &session); err != nil {
			r.logger.WithError(err).Warning("Failed to parse session from Redis")
			continue
		}

		sessions = append(sessions, &session)
	}

	return sessions, nil
}

// Cluster Management

// RegisterNode registers this node in the cluster
func (r *RedisCluster) RegisterNode(address, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node := &ClusterNode{
		ID:             r.nodeID,
		Address:        address,
		Status:         "active",
		LastHeartbeat:  time.Now(),
		Version:        version,
		LoadFactor:     0.0,
		ActiveSessions: 0,
		IsLeader:       false,
	}

	data, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}

	key := r.nodeKey(r.nodeID)
	if err := r.client.Set(ctx, key, data, r.heartbeat*3).Err(); err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	r.logger.WithFields(logrus.Fields{
		"node_id": r.nodeID,
		"address": address,
		"version": version,
	}).Info("Node registered in cluster")

	return nil
}

// GetClusterNodes returns all nodes in the cluster
func (r *RedisCluster) GetClusterNodes() ([]*ClusterNode, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pattern := r.keyPrefix + "node:*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list node keys: %w", err)
	}

	if len(keys) == 0 {
		return []*ClusterNode{}, nil
	}

	// Get all nodes in batch
	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to execute batch get: %w", err)
	}

	// Parse nodes
	var nodes []*ClusterNode
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			continue // Skip failed nodes
		}

		var node ClusterNode
		if err := json.Unmarshal([]byte(data), &node); err != nil {
			r.logger.WithError(err).Warning("Failed to parse node from Redis")
			continue
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// startHeartbeat sends periodic heartbeats to maintain node presence
func (r *RedisCluster) startHeartbeat() {
	ticker := time.NewTicker(r.heartbeat)
	defer ticker.Stop()

	for range ticker.C {
		if err := r.sendHeartbeat(); err != nil {
			r.logger.WithError(err).Error("Failed to send heartbeat")
		}
	}
}

// sendHeartbeat updates the node's heartbeat timestamp
func (r *RedisCluster) sendHeartbeat() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get current node info
	key := r.nodeKey(r.nodeID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Node not registered, skip heartbeat
			return nil
		}
		return fmt.Errorf("failed to get node info: %w", err)
	}

	var node ClusterNode
	if err := json.Unmarshal([]byte(data), &node); err != nil {
		return fmt.Errorf("failed to parse node info: %w", err)
	}

	// Update heartbeat and stats
	node.LastHeartbeat = time.Now()
	node.ActiveSessions = r.getActiveSessionCount()
	node.LoadFactor = r.calculateLoadFactor()

	// Store updated node info
	updatedData, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to serialize node: %w", err)
	}

	if err := r.client.Set(ctx, key, updatedData, r.heartbeat*3).Err(); err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	return nil
}

// startLeaderElection manages leader election process
func (r *RedisCluster) startLeaderElection() {
	ticker := time.NewTicker(15 * time.Second) // Check every 15 seconds
	defer ticker.Stop()

	for range ticker.C {
		if err := r.performLeaderElection(); err != nil {
			r.logger.WithError(err).Error("Leader election failed")
		}
	}
}

// performLeaderElection attempts to elect a leader
func (r *RedisCluster) performLeaderElection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	leaderKey := r.keyPrefix + "leader"

	// Try to acquire leader lock
	acquired, err := r.client.SetNX(ctx, leaderKey, r.nodeID, r.heartbeat*2).Result()
	if err != nil {
		return fmt.Errorf("failed to acquire leader lock: %w", err)
	}

	r.mutex.Lock()
	r.isLeader = acquired
	r.mutex.Unlock()

	if acquired {
		r.logger.WithField("node_id", r.nodeID).Info("Became cluster leader")
	}

	return nil
}

// IsLeader returns whether this node is the cluster leader
func (r *RedisCluster) IsLeader() bool {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.isLeader
}

// startSessionMonitoring monitors and cleans up stale sessions
func (r *RedisCluster) startSessionMonitoring() {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		if r.IsLeader() {
			if err := r.cleanupStaleSessions(); err != nil {
				r.logger.WithError(err).Error("Failed to cleanup stale sessions")
			}
		}
	}
}

// cleanupStaleSessions removes stale sessions from the cluster
func (r *RedisCluster) cleanupStaleSessions() error {
	sessions, err := r.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	staleThreshold := time.Now().Add(-1 * time.Hour) // 1 hour without update
	var staleCount int

	for _, session := range sessions {
		if session.LastUpdate.Before(staleThreshold) {
			if err := r.DeleteSession(session.SessionID); err != nil {
				r.logger.WithError(err).WithField("session_id", session.SessionID).Warning("Failed to delete stale session")
			} else {
				staleCount++
			}
		}
	}

	if staleCount > 0 {
		r.logger.WithField("count", staleCount).Info("Cleaned up stale sessions")
	}

	return nil
}

// Helper methods

func (r *RedisCluster) sessionKey(sessionID string) string {
	return r.keyPrefix + "session:" + sessionID
}

func (r *RedisCluster) nodeKey(nodeID string) string {
	return r.keyPrefix + "node:" + nodeID
}

func (r *RedisCluster) addToSessionIndex(sessionID, callID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	indexKey := r.keyPrefix + "index:sessions"
	return r.client.HSet(ctx, indexKey, sessionID, callID).Err()
}

func (r *RedisCluster) removeFromSessionIndex(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	indexKey := r.keyPrefix + "index:sessions"
	return r.client.HDel(ctx, indexKey, sessionID).Err()
}

func (r *RedisCluster) getActiveSessionCount() int {
	sessions, err := r.ListSessions()
	if err != nil {
		return 0
	}

	count := 0
	for _, session := range sessions {
		if session.NodeID == r.nodeID && session.Status == "active" {
			count++
		}
	}

	return count
}

func (r *RedisCluster) calculateLoadFactor() float64 {
	// Simple load factor based on active sessions
	// In production, consider CPU, memory, and other metrics
	activeCount := r.getActiveSessionCount()
	maxSessions := 1000 // Configure based on server capacity

	return float64(activeCount) / float64(maxSessions)
}

// Health and diagnostics

// GetClusterHealth returns cluster health information
func (r *RedisCluster) GetClusterHealth() (*ClusterHealth, error) {
	nodes, err := r.GetClusterNodes()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	sessions, err := r.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	health := &ClusterHealth{
		TotalNodes:     len(nodes),
		ActiveNodes:    0,
		LeaderNodeID:   "",
		TotalSessions:  len(sessions),
		ActiveSessions: 0,
		NodeStats:      make(map[string]*NodeStats),
		LastChecked:    time.Now(),
	}

	for _, node := range nodes {
		if time.Since(node.LastHeartbeat) < r.heartbeat*2 {
			health.ActiveNodes++
		}

		if node.IsLeader {
			health.LeaderNodeID = node.ID
		}

		health.NodeStats[node.ID] = &NodeStats{
			Status:         node.Status,
			LastHeartbeat:  node.LastHeartbeat,
			LoadFactor:     node.LoadFactor,
			ActiveSessions: node.ActiveSessions,
		}
	}

	for _, session := range sessions {
		if session.Status == "active" {
			health.ActiveSessions++
		}
	}

	return health, nil
}

// Close closes the Redis cluster connection
func (r *RedisCluster) Close() error {
	return r.client.Close()
}

// Types

type ClusterHealth struct {
	TotalNodes     int                   `json:"total_nodes"`
	ActiveNodes    int                   `json:"active_nodes"`
	LeaderNodeID   string                `json:"leader_node_id"`
	TotalSessions  int                   `json:"total_sessions"`
	ActiveSessions int                   `json:"active_sessions"`
	NodeStats      map[string]*NodeStats `json:"node_stats"`
	LastChecked    time.Time             `json:"last_checked"`
}

type NodeStats struct {
	Status         string    `json:"status"`
	LastHeartbeat  time.Time `json:"last_heartbeat"`
	LoadFactor     float64   `json:"load_factor"`
	ActiveSessions int       `json:"active_sessions"`
}
