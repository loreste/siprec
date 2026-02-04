package cluster

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/config"
)

// ClusterOrchestrator coordinates all cluster components
type ClusterOrchestrator struct {
	config  *config.ClusterConfig
	logger  *logrus.Logger
	nodeID  string
	mu      sync.RWMutex

	// Core components
	redisClient redis.UniversalClient
	manager     *Manager

	// Advanced cluster features
	rtpStateManager *RTPStateManager
	rateLimiter     *DistributedRateLimiter
	tracer          *DistributedTracer
	splitBrain      *SplitBrainDetector
	streamMigrator  *StreamMigrationManager

	// State
	started bool
	stopCh  chan struct{}
}

// NewClusterOrchestrator creates a new cluster orchestrator
func NewClusterOrchestrator(cfg *config.ClusterConfig, logger *logrus.Logger) (*ClusterOrchestrator, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = fmt.Sprintf("node-%d", time.Now().UnixNano())
	}

	return &ClusterOrchestrator{
		config: cfg,
		logger: logger,
		nodeID: nodeID,
		stopCh: make(chan struct{}),
	}, nil
}

// Start initializes and starts all cluster components
func (o *ClusterOrchestrator) Start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.started {
		return nil
	}

	o.logger.WithField("node_id", o.nodeID).Info("Starting cluster orchestrator")

	// Initialize Redis client based on configuration
	var err error
	o.redisClient, err = o.initRedisClient()
	if err != nil {
		return fmt.Errorf("failed to initialize Redis client: %w", err)
	}

	// Test Redis connectivity
	if err := o.redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	o.logger.WithField("redis_mode", o.config.Redis.Mode).Info("Connected to Redis")

	// Initialize base cluster manager
	managerConfig := Config{
		Enabled:               true,
		NodeID:                o.nodeID,
		HeartbeatInterval:     o.config.HeartbeatInterval,
		NodeTTL:               o.config.NodeTTL,
		LeaderElectionEnabled: o.config.LeaderElectionEnabled,
		LeaderLockTTL:         o.config.LeaderLockTTL,
		LeaderRetryInterval:   o.config.LeaderRetryInterval,
	}
	o.manager = NewManager(managerConfig, o.redisClient, o.logger, o.nodeID)
	if err := o.manager.Start(); err != nil {
		return fmt.Errorf("failed to start cluster manager: %w", err)
	}

	// Initialize RTP state replication if enabled
	if o.config.RTPStateReplication {
		o.rtpStateManager = NewRTPStateManager(o.redisClient, o.nodeID, o.logger)
		if err := o.rtpStateManager.Start(ctx); err != nil {
			o.logger.WithError(err).Warn("Failed to start RTP state manager")
		} else {
			o.logger.Info("RTP state replication enabled")
		}
	}

	// Initialize distributed rate limiting if enabled
	if o.config.DistributedRateLimiting {
		rateLimitConfig := RateLimitConfig{
			GlobalCallsPerSecond: 1000,
			GlobalCallsPerMinute: 50000,
			PerIPCallsPerSecond:  10,
			PerIPCallsPerMinute:  100,
		}
		o.rateLimiter = NewDistributedRateLimiter(o.redisClient, rateLimitConfig, o.logger)
		o.rateLimiter.Start()
		o.logger.Info("Distributed rate limiting enabled")
	}

	// Initialize distributed tracing if enabled
	if o.config.DistributedTracing {
		o.tracer = NewDistributedTracer(o.redisClient, o.nodeID, o.logger)
		if err := o.tracer.Start(ctx); err != nil {
			o.logger.WithError(err).Warn("Failed to start distributed tracer")
		} else {
			o.logger.Info("Distributed tracing enabled")
		}
	}

	// Initialize split-brain detection if enabled
	if o.config.SplitBrainDetection.Enabled {
		splitBrainConfig := SplitBrainConfig{
			MinQuorum:       o.config.SplitBrainDetection.MinQuorum,
			CheckInterval:   o.config.SplitBrainDetection.CheckInterval,
			NodeTimeout:     o.config.SplitBrainDetection.NodeTimeout,
			GracePeriod:     o.config.SplitBrainDetection.GracePeriod,
			PartitionAction: o.config.SplitBrainDetection.PartitionAction,
			EnableFencing:   o.config.SplitBrainDetection.EnableFencing,
		}
		o.splitBrain = NewSplitBrainDetector(o.redisClient, o.nodeID, splitBrainConfig, o.logger)
		if err := o.splitBrain.Start(ctx); err != nil {
			o.logger.WithError(err).Warn("Failed to start split-brain detector")
		} else {
			o.logger.Info("Split-brain detection enabled")
		}
	}

	// Initialize stream migration if enabled
	if o.config.StreamMigration {
		o.streamMigrator = NewStreamMigrationManager(o.redisClient, o.nodeID, o.rtpStateManager, o.logger)
		if err := o.streamMigrator.Start(ctx); err != nil {
			o.logger.WithError(err).Warn("Failed to start stream migrator")
		} else {
			o.logger.Info("Stream migration enabled")
		}
	}

	o.started = true
	o.logger.Info("Cluster orchestrator started successfully")
	return nil
}

// Stop gracefully stops all cluster components
func (o *ClusterOrchestrator) Stop() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.started {
		return
	}

	o.logger.Info("Stopping cluster orchestrator")
	close(o.stopCh)

	// Stop components in reverse order
	if o.streamMigrator != nil {
		o.streamMigrator.Stop()
	}

	if o.splitBrain != nil {
		o.splitBrain.Stop()
	}

	if o.tracer != nil {
		o.tracer.Stop()
	}

	if o.rateLimiter != nil {
		o.rateLimiter.Stop()
	}

	if o.rtpStateManager != nil {
		o.rtpStateManager.Stop()
	}

	if o.manager != nil {
		o.manager.Stop()
	}

	if o.redisClient != nil {
		o.redisClient.Close()
	}

	o.started = false
	o.logger.Info("Cluster orchestrator stopped")
}

// initRedisClient creates the appropriate Redis client based on configuration
func (o *ClusterOrchestrator) initRedisClient() (redis.UniversalClient, error) {
	redisCfg := &o.config.Redis

	// Build RedisClusterConfig from app config
	clusterConfig := RedisClusterConfig{
		Address:            redisCfg.Address,
		Password:           redisCfg.Password,
		Database:           redisCfg.Database,
		SentinelAddresses:  redisCfg.SentinelAddresses,
		SentinelMasterName: redisCfg.SentinelMasterName,
		SentinelPassword:   redisCfg.SentinelPassword,
		ClusterAddresses:   redisCfg.ClusterAddresses,
		PoolSize:           redisCfg.PoolSize,
		MinIdleConns:       redisCfg.MinIdleConns,
		DialTimeout:        redisCfg.DialTimeout,
		ReadTimeout:        redisCfg.ReadTimeout,
		WriteTimeout:       redisCfg.WriteTimeout,
		PoolTimeout:        redisCfg.PoolTimeout,
		MaxRetries:         redisCfg.MaxRetries,
		MinRetryBackoff:    redisCfg.MinRetryBackoff,
		MaxRetryBackoff:    redisCfg.MaxRetryBackoff,
		TLSEnabled:         redisCfg.TLSEnabled,
		TLSCertFile:        redisCfg.TLSCertFile,
		TLSKeyFile:         redisCfg.TLSKeyFile,
		TLSCAFile:          redisCfg.TLSCAFile,
		TLSInsecureSkipVerify: redisCfg.TLSInsecureSkipVerify,
		RouteByLatency:     redisCfg.RouteByLatency,
		RouteRandomly:      redisCfg.RouteRandomly,
	}

	// Set the mode
	switch redisCfg.Mode {
	case "sentinel":
		clusterConfig.Mode = RedisModeSentinel
	case "cluster":
		clusterConfig.Mode = RedisModeCluster
	default:
		clusterConfig.Mode = RedisModeStandalone
		if clusterConfig.Address == "" {
			clusterConfig.Address = "localhost:6379"
		}
	}

	// Create the client
	redisClient, err := NewRedisClusterClient(clusterConfig, o.logger)
	if err != nil {
		return nil, err
	}

	// Return the underlying client
	return redisClient.Client(), nil
}

// Accessor methods for components

// GetManager returns the base cluster manager
func (o *ClusterOrchestrator) GetManager() *Manager {
	return o.manager
}

// GetRedisClient returns the Redis client
func (o *ClusterOrchestrator) GetRedisClient() redis.UniversalClient {
	return o.redisClient
}

// GetRTPStateManager returns the RTP state manager
func (o *ClusterOrchestrator) GetRTPStateManager() *RTPStateManager {
	return o.rtpStateManager
}

// GetRateLimiter returns the distributed rate limiter
func (o *ClusterOrchestrator) GetRateLimiter() *DistributedRateLimiter {
	return o.rateLimiter
}

// GetTracer returns the distributed tracer
func (o *ClusterOrchestrator) GetTracer() *DistributedTracer {
	return o.tracer
}

// GetSplitBrainDetector returns the split-brain detector
func (o *ClusterOrchestrator) GetSplitBrainDetector() *SplitBrainDetector {
	return o.splitBrain
}

// GetStreamMigrator returns the stream migrator
func (o *ClusterOrchestrator) GetStreamMigrator() *StreamMigrationManager {
	return o.streamMigrator
}

// IsLeader returns true if this node is the cluster leader
func (o *ClusterOrchestrator) IsLeader() bool {
	if o.manager == nil {
		return true // Single node mode
	}
	return o.manager.IsLeader()
}

// HasQuorum returns true if the cluster has quorum
func (o *ClusterOrchestrator) HasQuorum() bool {
	if o.splitBrain == nil {
		return true // No split-brain detection
	}
	return o.splitBrain.HasQuorum()
}

// IsPartitioned returns true if a network partition is detected
func (o *ClusterOrchestrator) IsPartitioned() bool {
	if o.splitBrain == nil {
		return false
	}
	return o.splitBrain.IsPartitioned()
}

// IsFenced returns true if this node is fenced
func (o *ClusterOrchestrator) IsFenced() bool {
	if o.splitBrain == nil {
		return false
	}
	return o.splitBrain.IsFenced()
}

// AllowCall checks distributed rate limits and returns whether a call should be allowed
func (o *ClusterOrchestrator) AllowCall(ctx context.Context, remoteIP string) bool {
	if o.rateLimiter == nil {
		return true // No rate limiting
	}

	// Check if partitioned/fenced
	if o.IsFenced() {
		o.logger.WithField("remote_ip", remoteIP).Warn("Call rejected: node is fenced")
		return false
	}

	result := o.rateLimiter.AllowCall(ctx, remoteIP)
	return result.Allowed
}

// StartTrace starts a new distributed trace for a call
func (o *ClusterOrchestrator) StartTrace(ctx context.Context, operation, callUUID string) *TraceContext {
	if o.tracer == nil {
		return nil
	}
	return o.tracer.StartTrace(ctx, operation, callUUID)
}

// EndTrace ends a distributed trace span
func (o *ClusterOrchestrator) EndTrace(span *TraceContext, err error) {
	if o.tracer == nil || span == nil {
		return
	}
	o.tracer.EndSpan(span, err)
}

// RegisterRTPStream registers an RTP stream state for replication
func (o *ClusterOrchestrator) RegisterRTPStream(state *RTPStreamState) error {
	if o.rtpStateManager == nil {
		return nil
	}
	return o.rtpStateManager.RegisterStream(state)
}

// UpdateRTPStream updates an RTP stream state
func (o *ClusterOrchestrator) UpdateRTPStream(callUUID string, updates map[string]interface{}) error {
	if o.rtpStateManager == nil {
		return nil
	}
	return o.rtpStateManager.UpdateStream(callUUID, updates)
}

// UnregisterRTPStream removes an RTP stream from replication
func (o *ClusterOrchestrator) UnregisterRTPStream(callUUID string) error {
	if o.rtpStateManager == nil {
		return nil
	}
	return o.rtpStateManager.UnregisterStream(callUUID)
}

// MigrateAllStreams migrates all streams from this node (for graceful shutdown)
func (o *ClusterOrchestrator) MigrateAllStreams(ctx context.Context, targetNodeID string) error {
	if o.streamMigrator == nil {
		return nil
	}
	return o.streamMigrator.MigrateAllStreams(ctx, targetNodeID)
}

// GetClusterStatus returns comprehensive cluster status
func (o *ClusterOrchestrator) GetClusterStatus(ctx context.Context) map[string]interface{} {
	status := map[string]interface{}{
		"node_id":   o.nodeID,
		"enabled":   true,
		"started":   o.started,
		"is_leader": o.IsLeader(),
	}

	// Add manager status
	if o.manager != nil {
		clusterStatus, err := o.manager.GetClusterStatus(ctx)
		if err == nil {
			status["cluster"] = clusterStatus
		}
	}

	// Add split-brain status
	if o.splitBrain != nil {
		sbStatus := o.splitBrain.GetStatus()
		status["split_brain"] = map[string]interface{}{
			"partition_detected": sbStatus.PartitionDetected,
			"has_quorum":         sbStatus.HasQuorum,
			"reachable_nodes":    sbStatus.ReachableNodes,
			"total_nodes":        sbStatus.TotalNodes,
			"is_fenced":          o.IsFenced(),
		}
	}

	// Add rate limiter status
	if o.rateLimiter != nil {
		status["rate_limiter"] = o.rateLimiter.GetMetrics()
	}

	// Add tracer status
	if o.tracer != nil {
		status["tracer"] = o.tracer.GetStats()
	}

	// Add RTP state status
	if o.rtpStateManager != nil {
		status["rtp_state"] = o.rtpStateManager.GetStats()
	}

	// Add stream migration status
	if o.streamMigrator != nil {
		status["stream_migration"] = o.streamMigrator.GetStats()
	}

	return status
}

// OnLeadershipChange registers a callback for leadership changes
func (o *ClusterOrchestrator) OnLeadershipChange(callback func(isLeader bool)) {
	if o.manager != nil {
		o.manager.OnLeadershipChange(callback)
	}
}

// OnPartitionChange registers a callback for partition detection changes
func (o *ClusterOrchestrator) OnPartitionChange(callback func(detected bool, reachableNodes []string)) {
	if o.splitBrain != nil {
		o.splitBrain.OnPartitionChange(callback)
	}
}
