package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"siprec-server/pkg/config"
)

func TestOrchestratorNilSafe(t *testing.T) {
	// Disabled config returns nil orchestrator
	orch, err := NewClusterOrchestrator(&config.ClusterConfig{Enabled: false}, logrus.New())
	assert.NoError(t, err)
	assert.Nil(t, orch)

	// Nil config returns nil orchestrator
	orch, err = NewClusterOrchestrator(nil, logrus.New())
	assert.NoError(t, err)
	assert.Nil(t, orch)
}

func TestOrchestratorGetNodeID(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cfg := &config.ClusterConfig{
		Enabled: true,
		NodeID:  "test-node-42",
	}

	orch, err := NewClusterOrchestrator(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, orch)

	assert.Equal(t, "test-node-42", orch.GetNodeID())
}

func TestOrchestratorNilSubComponentSafety(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cfg := &config.ClusterConfig{
		Enabled: true,
		NodeID:  "safe-node",
	}

	orch, err := NewClusterOrchestrator(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, orch)

	// These should all be safe with nil sub-components (not started)
	assert.True(t, orch.IsLeader(), "single node should be leader")
	assert.True(t, orch.HasQuorum(), "no split-brain detector means quorum")
	assert.False(t, orch.IsPartitioned())
	assert.False(t, orch.IsFenced())
	assert.True(t, orch.AllowCall(context.Background(), "1.2.3.4"), "no rate limiter means allow")

	// Nil tracer operations
	assert.Nil(t, orch.StartTrace(context.Background(), "op", "call"))
	orch.EndTrace(nil, nil) // should not panic

	// Nil RTP state operations
	assert.NoError(t, orch.RegisterRTPStream(&RTPStreamState{CallUUID: "test"}))
	assert.NoError(t, orch.UpdateRTPStream("test", map[string]interface{}{"status": "active"}))
	assert.NoError(t, orch.UnregisterRTPStream("test"))

	// Nil migration
	assert.NoError(t, orch.MigrateAllStreams(context.Background(), "other-node"))

	// Nil sub-component accessors
	assert.Nil(t, orch.GetManager())
	assert.Nil(t, orch.GetRTPStateManager())
	assert.Nil(t, orch.GetRateLimiter())
	assert.Nil(t, orch.GetTracer())
	assert.Nil(t, orch.GetSplitBrainDetector())
	assert.Nil(t, orch.GetStreamMigrator())
}

func TestOrchestratorGetClusterStatus(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cfg := &config.ClusterConfig{
		Enabled: true,
		NodeID:  "status-node",
	}

	orch, err := NewClusterOrchestrator(cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, orch)

	status := orch.GetClusterStatus(context.Background())
	require.NotNil(t, status)
	assert.Equal(t, "status-node", status["node_id"])
	assert.Equal(t, true, status["enabled"])
}

func TestOrchestratorRTPStateWithRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	// Manually create orchestrator with RTP state manager
	orch := &ClusterOrchestrator{
		config:      &config.ClusterConfig{Enabled: true, NodeID: "rtp-node"},
		logger:      logger,
		nodeID:      "rtp-node",
		redisClient: client,
		stopCh:      make(chan struct{}),
	}
	orch.rtpStateManager = NewRTPStateManager(client, "rtp-node", logger)

	// Register stream
	state := &RTPStreamState{
		CallUUID:  "call-rtp-orch",
		LocalPort: 10000,
		RTCPPort:  10001,
		CodecName: "PCMU",
	}
	err := orch.RegisterRTPStream(state)
	require.NoError(t, err)

	// Update
	err = orch.UpdateRTPStream("call-rtp-orch", map[string]interface{}{
		"packets_received": int64(100),
	})
	require.NoError(t, err)

	// Unregister
	err = orch.UnregisterRTPStream("call-rtp-orch")
	require.NoError(t, err)
}

func TestOrchestratorTracerWithRedis(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	orch := &ClusterOrchestrator{
		config:      &config.ClusterConfig{Enabled: true, NodeID: "trace-node"},
		logger:      logger,
		nodeID:      "trace-node",
		redisClient: client,
		stopCh:      make(chan struct{}),
	}

	tracer := NewDistributedTracer(client, "trace-node", logger)
	err := tracer.Start(context.Background())
	require.NoError(t, err)
	defer tracer.Stop()

	orch.tracer = tracer

	// Start and end trace through orchestrator
	span := orch.StartTrace(context.Background(), "sip.invite", "call-trace-orch")
	require.NotNil(t, span)
	assert.Equal(t, "active", span.Status)

	time.Sleep(10 * time.Millisecond)
	orch.EndTrace(span, nil)
	assert.Equal(t, "completed", span.Status)
}
