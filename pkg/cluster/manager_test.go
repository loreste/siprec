package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, redis.UniversalClient) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, client
}

func testConfig(nodeID string) Config {
	return Config{
		Enabled:               true,
		NodeID:                nodeID,
		HeartbeatInterval:     100 * time.Millisecond,
		NodeTTL:               500 * time.Millisecond,
		LeaderElectionEnabled: false,
		LeaderLockTTL:         500 * time.Millisecond,
		LeaderRetryInterval:   100 * time.Millisecond,
	}
}

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	return logger
}

func TestManagerStartStop(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-start-stop")
	mgr := NewManager(cfg, client, testLogger(), "test-host")

	err := mgr.Start()
	require.NoError(t, err)

	// Verify the node registered itself on start
	ctx := context.Background()
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)
	assert.Equal(t, "node-start-stop", nodes[0].ID)

	mgr.Stop()

	// After stop, the node key should be removed
	nodes, err = mgr.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 0)
}

func TestManagerStartDisabled(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-disabled")
	cfg.Enabled = false
	mgr := NewManager(cfg, client, testLogger(), "test-host")

	err := mgr.Start()
	require.NoError(t, err)

	// No node should be registered when disabled
	ctx := context.Background()
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 0)

	// Stop on disabled manager should not panic
	mgr.Stop()
}

func TestManagerNodeRegistration(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-reg-1")
	mgr := NewManager(cfg, client, testLogger(), "host-reg-1")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	ctx := context.Background()
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)

	node := nodes[0]
	assert.Equal(t, "node-reg-1", node.ID)
	assert.Equal(t, "host-reg-1", node.Hostname)
	assert.False(t, node.StartedAt.IsZero())
	assert.False(t, node.LastSeen.IsZero())
	assert.True(t, node.LastSeen.After(node.StartedAt) || node.LastSeen.Equal(node.StartedAt))
}

func TestManagerHeartbeat(t *testing.T) {
	mr, client := setupTestRedis(t)

	cfg := testConfig("node-hb")
	cfg.HeartbeatInterval = 50 * time.Millisecond
	cfg.NodeTTL = 300 * time.Millisecond
	mgr := NewManager(cfg, client, testLogger(), "host-hb")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	ctx := context.Background()

	// Record initial LastSeen
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	initialLastSeen := nodes[0].LastSeen

	// Wait for at least one heartbeat cycle
	time.Sleep(120 * time.Millisecond)

	// Fast-forward miniredis time so we can verify TTL was refreshed
	// The key should still exist because heartbeats keep refreshing it
	nodes, err = mgr.ListNodes(ctx)
	require.NoError(t, err)
	require.Len(t, nodes, 1, "node should still be registered after heartbeat")
	assert.True(t, nodes[0].LastSeen.After(initialLastSeen),
		"LastSeen should be updated by heartbeat")

	// Verify TTL is being refreshed by checking the key still has a TTL
	ttl := mr.TTL("siprec:nodes:node-hb")
	assert.True(t, ttl > 0, "key should have a positive TTL after heartbeat")
}

func TestManagerLeaderElection(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-leader")
	cfg.LeaderElectionEnabled = true
	mgr := NewManager(cfg, client, testLogger(), "host-leader")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Leader election loop tries to acquire immediately; give it a moment
	time.Sleep(200 * time.Millisecond)

	assert.True(t, mgr.IsLeader(), "single node should become leader")

	ctx := context.Background()
	leaderID, err := mgr.GetLeader(ctx)
	require.NoError(t, err)
	assert.Equal(t, "node-leader", leaderID)
}

func TestManagerLeadershipCallback(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-cb")
	cfg.LeaderElectionEnabled = true
	// Use a long retry interval so the election loop will not re-acquire
	// leadership immediately after we force-release it.
	cfg.LeaderRetryInterval = 30 * time.Second
	mgr := NewManager(cfg, client, testLogger(), "host-cb")

	var mu sync.Mutex
	var statuses []bool

	mgr.OnLeadershipChange(func(isLeader bool) {
		mu.Lock()
		statuses = append(statuses, isLeader)
		mu.Unlock()
	})

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Wait for leader election callback to fire (acquired)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 1
	}, 2*time.Second, 50*time.Millisecond, "leadership callback should have been called")

	mu.Lock()
	assert.True(t, statuses[0], "first callback should report isLeader=true")
	mu.Unlock()

	// Force release and verify callback fires again with false
	mgr.ForceReleaseLeadership()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 2
	}, 2*time.Second, 50*time.Millisecond, "callback should fire on leadership release")

	mu.Lock()
	assert.False(t, statuses[1], "second callback should report isLeader=false")
	mu.Unlock()
}

func TestManagerMultipleNodes(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg1 := testConfig("node-multi-1")
	mgr1 := NewManager(cfg1, client, testLogger(), "host-1")

	cfg2 := testConfig("node-multi-2")
	mgr2 := NewManager(cfg2, client, testLogger(), "host-2")

	err := mgr1.Start()
	require.NoError(t, err)
	t.Cleanup(mgr1.Stop)

	err = mgr2.Start()
	require.NoError(t, err)
	t.Cleanup(mgr2.Stop)

	ctx := context.Background()
	nodes, err := mgr1.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 2, "both nodes should be visible")

	// Verify both node IDs are present
	ids := make(map[string]bool)
	for _, n := range nodes {
		ids[n.ID] = true
	}
	assert.True(t, ids["node-multi-1"])
	assert.True(t, ids["node-multi-2"])

	// Either manager should see the same nodes
	nodes2, err := mgr2.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes2, 2)
}

func TestManagerMultipleNodesLeaderElection(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg1 := testConfig("node-elect-1")
	cfg1.LeaderElectionEnabled = true
	mgr1 := NewManager(cfg1, client, testLogger(), "host-1")

	cfg2 := testConfig("node-elect-2")
	cfg2.LeaderElectionEnabled = true
	mgr2 := NewManager(cfg2, client, testLogger(), "host-2")

	err := mgr1.Start()
	require.NoError(t, err)
	t.Cleanup(mgr1.Stop)

	err = mgr2.Start()
	require.NoError(t, err)
	t.Cleanup(mgr2.Stop)

	// Wait for election to settle
	time.Sleep(300 * time.Millisecond)

	// Exactly one node should be leader
	leaderCount := 0
	if mgr1.IsLeader() {
		leaderCount++
	}
	if mgr2.IsLeader() {
		leaderCount++
	}
	assert.Equal(t, 1, leaderCount, "exactly one node should be the leader")

	// Both should agree on the leader
	ctx := context.Background()
	leader1, err := mgr1.GetLeader(ctx)
	require.NoError(t, err)
	leader2, err := mgr2.GetLeader(ctx)
	require.NoError(t, err)
	assert.Equal(t, leader1, leader2, "both nodes should agree on the leader")
	assert.NotEmpty(t, leader1)
}

func TestManagerRunIfLeader(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-runif")
	cfg.LeaderElectionEnabled = true
	mgr := NewManager(cfg, client, testLogger(), "host-runif")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Wait for leadership
	require.Eventually(t, func() bool {
		return mgr.IsLeader()
	}, 2*time.Second, 50*time.Millisecond)

	// RunIfLeader should execute
	executed := false
	result := mgr.RunIfLeader(func() {
		executed = true
	})
	assert.True(t, result)
	assert.True(t, executed)

	// RunIfLeaderWithContext should execute
	ctx := context.Background()
	err = mgr.RunIfLeaderWithContext(ctx, func(ctx context.Context) error {
		return nil
	})
	assert.NoError(t, err)

	// After releasing leadership, RunIfLeader should not execute
	mgr.ForceReleaseLeadership()

	executed = false
	result = mgr.RunIfLeader(func() {
		executed = true
	})
	assert.False(t, result)
	assert.False(t, executed)

	err = mgr.RunIfLeaderWithContext(ctx, func(ctx context.Context) error {
		return nil
	})
	assert.ErrorIs(t, err, ErrNotLeader)
}

func TestManagerGetClusterStatus(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-status")
	cfg.LeaderElectionEnabled = true
	mgr := NewManager(cfg, client, testLogger(), "host-status")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Wait for leadership
	require.Eventually(t, func() bool {
		return mgr.IsLeader()
	}, 2*time.Second, 50*time.Millisecond)

	ctx := context.Background()
	status, err := mgr.GetClusterStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, status.NodeCount)
	assert.Equal(t, "node-status", status.ThisNodeID)
	assert.Equal(t, "node-status", status.LeaderID)
	assert.True(t, status.IsThisLeader)
	require.Len(t, status.Nodes, 1)
	assert.Equal(t, "node-status", status.Nodes[0].ID)
}

func TestManagerConcurrentAccess(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-concurrent")
	cfg.LeaderElectionEnabled = true
	mgr := NewManager(cfg, client, testLogger(), "host-concurrent")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Wait for leader election to settle
	time.Sleep(200 * time.Millisecond)

	ctx := context.Background()
	var wg sync.WaitGroup
	const goroutines = 20

	// Concurrently call various methods to detect race conditions
	for i := 0; i < goroutines; i++ {
		wg.Add(4)

		go func() {
			defer wg.Done()
			_, _ = mgr.ListNodes(ctx)
		}()

		go func() {
			defer wg.Done()
			_ = mgr.IsLeader()
		}()

		go func() {
			defer wg.Done()
			_, _ = mgr.GetLeader(ctx)
		}()

		go func() {
			defer wg.Done()
			_, _ = mgr.GetClusterStatus(ctx)
		}()
	}

	// Also concurrently register callbacks
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.OnLeadershipChange(func(isLeader bool) {})
		}()
	}

	wg.Wait()
}

func TestManagerDefaultConfigValues(t *testing.T) {
	_, client := setupTestRedis(t)

	// Use zero-value durations to verify defaults are applied
	cfg := Config{
		Enabled: true,
		NodeID:  "node-defaults",
	}
	mgr := NewManager(cfg, client, testLogger(), "host-defaults")

	assert.Equal(t, 5*time.Second, mgr.config.HeartbeatInterval)
	assert.Equal(t, 15*time.Second, mgr.config.NodeTTL)
	assert.Equal(t, 10*time.Second, mgr.config.LeaderLockTTL)
	assert.Equal(t, 3*time.Second, mgr.config.LeaderRetryInterval)
}

func TestManagerForceReleaseLeadershipWhenNotLeader(t *testing.T) {
	_, client := setupTestRedis(t)

	cfg := testConfig("node-no-leader")
	cfg.LeaderElectionEnabled = false
	mgr := NewManager(cfg, client, testLogger(), "host-no-leader")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	// Should not panic when not leader
	assert.False(t, mgr.IsLeader())
	mgr.ForceReleaseLeadership()
	assert.False(t, mgr.IsLeader())
}

func TestManagerNodeExpiration(t *testing.T) {
	mr, client := setupTestRedis(t)

	cfg := testConfig("node-expire")
	cfg.NodeTTL = 200 * time.Millisecond
	cfg.HeartbeatInterval = 10 * time.Second // long interval so heartbeat won't refresh
	mgr := NewManager(cfg, client, testLogger(), "host-expire")

	err := mgr.Start()
	require.NoError(t, err)
	t.Cleanup(mgr.Stop)

	ctx := context.Background()
	nodes, err := mgr.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 1)

	// Fast-forward miniredis past the TTL
	mr.FastForward(300 * time.Millisecond)

	nodes, err = mgr.ListNodes(ctx)
	require.NoError(t, err)
	assert.Len(t, nodes, 0, "node should have expired after TTL")
}
