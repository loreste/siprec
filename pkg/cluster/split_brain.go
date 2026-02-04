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
	splitBrainKeyPrefix   = "siprec:splitbrain:"
	quorumCheckKey        = "siprec:quorum:check"
	networkPartitionKey   = "siprec:partition:detected"
	splitBrainCheckInterval = 5 * time.Second
)

// SplitBrainDetector detects and handles network partitions
type SplitBrainDetector struct {
	redis       redis.UniversalClient
	logger      *logrus.Logger
	nodeID      string
	config      SplitBrainConfig
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// State
	stateMu           sync.RWMutex
	lastQuorumCheck   time.Time
	partitionDetected bool
	reachableNodes    []string
	totalNodes        int

	// Callbacks
	callbacksMu sync.RWMutex
	onPartition []func(detected bool, reachableNodes []string)
}

// SplitBrainConfig holds split-brain detection configuration
type SplitBrainConfig struct {
	// Minimum nodes required for quorum
	MinQuorum int `json:"min_quorum" default:"2"`

	// Check interval
	CheckInterval time.Duration `json:"check_interval" default:"5s"`

	// Timeout for node checks
	NodeTimeout time.Duration `json:"node_timeout" default:"3s"`

	// Grace period before declaring partition
	GracePeriod time.Duration `json:"grace_period" default:"15s"`

	// Action on partition: "readonly", "shutdown", "continue"
	PartitionAction string `json:"partition_action" default:"readonly"`

	// Enable fencing (prevent partitioned nodes from accepting calls)
	EnableFencing bool `json:"enable_fencing" default:"true"`
}

// NewSplitBrainDetector creates a new split-brain detector
func NewSplitBrainDetector(redisClient redis.UniversalClient, nodeID string, config SplitBrainConfig, logger *logrus.Logger) *SplitBrainDetector {
	if config.MinQuorum == 0 {
		config.MinQuorum = 2
	}
	if config.CheckInterval == 0 {
		config.CheckInterval = splitBrainCheckInterval
	}
	if config.NodeTimeout == 0 {
		config.NodeTimeout = 3 * time.Second
	}
	if config.GracePeriod == 0 {
		config.GracePeriod = 15 * time.Second
	}
	if config.PartitionAction == "" {
		config.PartitionAction = "readonly"
	}

	return &SplitBrainDetector{
		redis:    redisClient,
		logger:   logger,
		nodeID:   nodeID,
		config:   config,
		stopChan: make(chan struct{}),
	}
}

// Start begins split-brain detection
func (s *SplitBrainDetector) Start(ctx context.Context) error {
	s.wg.Add(1)
	go s.detectionLoop(ctx)

	s.logger.WithFields(logrus.Fields{
		"min_quorum":   s.config.MinQuorum,
		"check_interval": s.config.CheckInterval,
		"action":       s.config.PartitionAction,
	}).Info("Split-brain detector started")

	return nil
}

// Stop stops the split-brain detector
func (s *SplitBrainDetector) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	s.logger.Info("Split-brain detector stopped")
}

// OnPartitionChange registers a callback for partition detection
func (s *SplitBrainDetector) OnPartitionChange(callback func(detected bool, reachableNodes []string)) {
	s.callbacksMu.Lock()
	defer s.callbacksMu.Unlock()
	s.onPartition = append(s.onPartition, callback)
}

// IsPartitioned returns whether a partition is currently detected
func (s *SplitBrainDetector) IsPartitioned() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.partitionDetected
}

// HasQuorum returns whether this node has quorum
func (s *SplitBrainDetector) HasQuorum() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return len(s.reachableNodes) >= s.config.MinQuorum
}

// GetReachableNodes returns the list of reachable nodes
func (s *SplitBrainDetector) GetReachableNodes() []string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	nodes := make([]string, len(s.reachableNodes))
	copy(nodes, s.reachableNodes)
	return nodes
}

// GetStatus returns the current split-brain detection status
func (s *SplitBrainDetector) GetStatus() *SplitBrainStatus {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	return &SplitBrainStatus{
		NodeID:            s.nodeID,
		PartitionDetected: s.partitionDetected,
		HasQuorum:         len(s.reachableNodes) >= s.config.MinQuorum,
		ReachableNodes:    s.reachableNodes,
		TotalNodes:        s.totalNodes,
		MinQuorum:         s.config.MinQuorum,
		LastCheck:         s.lastQuorumCheck,
		Action:            s.config.PartitionAction,
	}
}

// SplitBrainStatus contains the current split-brain status
type SplitBrainStatus struct {
	NodeID            string    `json:"node_id"`
	PartitionDetected bool      `json:"partition_detected"`
	HasQuorum         bool      `json:"has_quorum"`
	ReachableNodes    []string  `json:"reachable_nodes"`
	TotalNodes        int       `json:"total_nodes"`
	MinQuorum         int       `json:"min_quorum"`
	LastCheck         time.Time `json:"last_check"`
	Action            string    `json:"action"`
}

// detectionLoop runs the split-brain detection
func (s *SplitBrainDetector) detectionLoop(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.CheckInterval)
	defer ticker.Stop()

	var partitionStartTime time.Time
	var inGracePeriod bool

	for {
		select {
		case <-s.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			reachable, total, err := s.checkNodes(ctx)
			if err != nil {
				s.logger.WithError(err).Error("Failed to check cluster nodes")
				continue
			}

			s.stateMu.Lock()
			s.reachableNodes = reachable
			s.totalNodes = total
			s.lastQuorumCheck = time.Now()

			hasQuorum := len(reachable) >= s.config.MinQuorum

			if !hasQuorum {
				if !inGracePeriod {
					inGracePeriod = true
					partitionStartTime = time.Now()
					s.logger.WithFields(logrus.Fields{
						"reachable": len(reachable),
						"total":     total,
						"min_quorum": s.config.MinQuorum,
					}).Warn("Potential network partition detected, starting grace period")
				} else if time.Since(partitionStartTime) > s.config.GracePeriod {
					// Grace period expired, declare partition
					if !s.partitionDetected {
						s.partitionDetected = true
						s.stateMu.Unlock()
						s.handlePartition(true, reachable)
						s.stateMu.Lock()
					}
				}
			} else {
				// Has quorum
				inGracePeriod = false
				if s.partitionDetected {
					s.partitionDetected = false
					s.stateMu.Unlock()
					s.handlePartition(false, reachable)
					s.stateMu.Lock()
				}
			}
			s.stateMu.Unlock()
		}
	}
}

// checkNodes checks which nodes are reachable
func (s *SplitBrainDetector) checkNodes(ctx context.Context) ([]string, int, error) {
	// Get all registered nodes
	pattern := "siprec:nodes:*"
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, 0, err
	}

	if len(keys) == 0 {
		return []string{s.nodeID}, 1, nil
	}

	var reachable []string
	now := time.Now()

	for _, key := range keys {
		data, err := s.redis.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var nodeInfo NodeInfo
		if err := json.Unmarshal(data, &nodeInfo); err != nil {
			continue
		}

		// Check if node is alive (within timeout)
		if now.Sub(nodeInfo.LastSeen) < s.config.NodeTimeout*2 {
			reachable = append(reachable, nodeInfo.ID)
		}
	}

	return reachable, len(keys), nil
}

// handlePartition handles partition detection/recovery
func (s *SplitBrainDetector) handlePartition(detected bool, reachableNodes []string) {
	if detected {
		s.logger.WithFields(logrus.Fields{
			"reachable_nodes": reachableNodes,
			"action":         s.config.PartitionAction,
		}).Error("Network partition confirmed")

		// Store partition event
		s.recordPartitionEvent(true, reachableNodes)

		// Apply fencing if enabled
		if s.config.EnableFencing {
			s.applyFencing()
		}
	} else {
		s.logger.WithFields(logrus.Fields{
			"reachable_nodes": reachableNodes,
		}).Info("Network partition recovered")

		// Store recovery event
		s.recordPartitionEvent(false, reachableNodes)

		// Remove fencing
		if s.config.EnableFencing {
			s.removeFencing()
		}
	}

	// Notify callbacks
	s.notifyCallbacks(detected, reachableNodes)
}

// recordPartitionEvent records partition events in Redis
func (s *SplitBrainDetector) recordPartitionEvent(detected bool, reachableNodes []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	event := map[string]interface{}{
		"node_id":         s.nodeID,
		"detected":        detected,
		"reachable_nodes": reachableNodes,
		"timestamp":       time.Now().Unix(),
	}

	data, _ := json.Marshal(event)

	key := fmt.Sprintf("%s%s:%d", splitBrainKeyPrefix, s.nodeID, time.Now().Unix())
	s.redis.Set(ctx, key, data, 24*time.Hour)

	// Update global partition status
	if detected {
		s.redis.Set(ctx, networkPartitionKey, s.nodeID, time.Hour)
	} else {
		// Only clear if we set it
		current, _ := s.redis.Get(ctx, networkPartitionKey).Result()
		if current == s.nodeID {
			s.redis.Del(ctx, networkPartitionKey)
		}
	}
}

// applyFencing prevents this node from accepting new calls
func (s *SplitBrainDetector) applyFencing() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := splitBrainKeyPrefix + "fenced:" + s.nodeID
	s.redis.Set(ctx, key, "1", time.Hour)

	s.logger.WithField("node_id", s.nodeID).Warn("Node fenced due to network partition")
}

// removeFencing allows this node to accept calls again
func (s *SplitBrainDetector) removeFencing() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := splitBrainKeyPrefix + "fenced:" + s.nodeID
	s.redis.Del(ctx, key)

	s.logger.WithField("node_id", s.nodeID).Info("Node fencing removed")
}

// IsFenced returns whether this node is fenced
func (s *SplitBrainDetector) IsFenced() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := splitBrainKeyPrefix + "fenced:" + s.nodeID
	exists, _ := s.redis.Exists(ctx, key).Result()
	return exists > 0
}

// notifyCallbacks notifies all registered callbacks
func (s *SplitBrainDetector) notifyCallbacks(detected bool, reachableNodes []string) {
	s.callbacksMu.RLock()
	callbacks := make([]func(bool, []string), len(s.onPartition))
	copy(callbacks, s.onPartition)
	s.callbacksMu.RUnlock()

	for _, cb := range callbacks {
		go func(callback func(bool, []string)) {
			defer func() {
				if r := recover(); r != nil {
					s.logger.WithField("panic", r).Error("Partition callback panicked")
				}
			}()
			callback(detected, reachableNodes)
		}(cb)
	}
}

// ForceQuorumCheck forces an immediate quorum check
func (s *SplitBrainDetector) ForceQuorumCheck(ctx context.Context) (*SplitBrainStatus, error) {
	reachable, total, err := s.checkNodes(ctx)
	if err != nil {
		return nil, err
	}

	s.stateMu.Lock()
	s.reachableNodes = reachable
	s.totalNodes = total
	s.lastQuorumCheck = time.Now()
	s.stateMu.Unlock()

	return s.GetStatus(), nil
}

// GetPartitionHistory returns recent partition events
func (s *SplitBrainDetector) GetPartitionHistory(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	pattern := splitBrainKeyPrefix + s.nodeID + ":*"
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return []map[string]interface{}{}, nil
	}

	// Sort keys by timestamp (descending)
	// Keys are in format: siprec:splitbrain:node-1:1234567890
	if len(keys) > limit {
		keys = keys[len(keys)-limit:]
	}

	var events []map[string]interface{}
	for i := len(keys) - 1; i >= 0; i-- {
		data, err := s.redis.Get(ctx, keys[i]).Bytes()
		if err != nil {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}
