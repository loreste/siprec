package cluster

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)


func newTestSplitBrainDetector(t *testing.T, client redis.UniversalClient) *SplitBrainDetector {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	cfg := SplitBrainConfig{
		MinQuorum:       2,
		CheckInterval:   100 * time.Millisecond,
		NodeTimeout:     3 * time.Second,
		GracePeriod:     15 * time.Second,
		PartitionAction: "readonly",
		EnableFencing:   true,
	}
	return NewSplitBrainDetector(client, "test-node-1", cfg, logger)
}

func TestSplitBrainDetectorStartStop(t *testing.T) {
	_, client := setupTestRedis(t)
	detector := newTestSplitBrainDetector(t, client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := detector.Start(ctx)
	require.NoError(t, err)

	// Let the detection loop run at least once.
	time.Sleep(200 * time.Millisecond)

	// Stop should complete without hanging.
	done := make(chan struct{})
	go func() {
		detector.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time")
	}
}

func TestSplitBrainHasQuorum(t *testing.T) {
	_, client := setupTestRedis(t)
	detector := newTestSplitBrainDetector(t, client)

	// Default state: reachableNodes is nil (length 0) and MinQuorum is 2,
	// so HasQuorum should be false before any check runs.
	assert.False(t, detector.HasQuorum(), "expected no quorum before any check")

	// After a forced check with no registered nodes, checkNodes returns
	// [nodeID] with length 1, which is still below MinQuorum of 2.
	ctx := context.Background()
	_, err := detector.ForceQuorumCheck(ctx)
	require.NoError(t, err)

	assert.False(t, detector.HasQuorum(), "expected no quorum with single node and MinQuorum=2")

	// Create a detector with MinQuorum = 1 so a single node suffices.
	logger := logrus.New()
	cfg := SplitBrainConfig{MinQuorum: 1}
	singleDetector := NewSplitBrainDetector(client, "test-node-1", cfg, logger)

	_, err = singleDetector.ForceQuorumCheck(ctx)
	require.NoError(t, err)
	assert.True(t, singleDetector.HasQuorum(), "expected quorum with MinQuorum=1 and 1 reachable node")
}

func TestSplitBrainGetStatus(t *testing.T) {
	_, client := setupTestRedis(t)
	detector := newTestSplitBrainDetector(t, client)

	ctx := context.Background()
	_, err := detector.ForceQuorumCheck(ctx)
	require.NoError(t, err)

	status := detector.GetStatus()
	require.NotNil(t, status)

	assert.Equal(t, "test-node-1", status.NodeID)
	assert.Equal(t, 2, status.MinQuorum)
	assert.Equal(t, "readonly", status.Action)
	assert.False(t, status.PartitionDetected)
	assert.False(t, status.LastCheck.IsZero(), "LastCheck should be set after ForceQuorumCheck")
}

func TestSplitBrainForceQuorumCheck(t *testing.T) {
	_, client := setupTestRedis(t)
	detector := newTestSplitBrainDetector(t, client)

	ctx := context.Background()
	status, err := detector.ForceQuorumCheck(ctx)
	require.NoError(t, err)
	require.NotNil(t, status)

	// With no registered nodes, checkNodes returns the current node only.
	assert.Contains(t, status.ReachableNodes, "test-node-1")
	assert.Equal(t, 1, status.TotalNodes)
	assert.False(t, status.LastCheck.IsZero())
}

func TestSplitBrainPartitionCallback(t *testing.T) {
	_, client := setupTestRedis(t)
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	detector := NewSplitBrainDetector(client, "test-node-1", SplitBrainConfig{
		MinQuorum:       1,
		CheckInterval:   1 * time.Second,
		NodeTimeout:     3 * time.Second,
		GracePeriod:     15 * time.Second,
		PartitionAction: "readonly",
	}, logger)

	var mu sync.Mutex
	var callbackDetected *bool
	var callbackNodes []string

	detector.OnPartitionChange(func(detected bool, reachableNodes []string) {
		mu.Lock()
		defer mu.Unlock()
		callbackDetected = &detected
		callbackNodes = reachableNodes
	})

	// Trigger the callback manually via the unexported notifyCallbacks.
	testNodes := []string{"test-node-1", "test-node-2"}
	detector.notifyCallbacks(true, testNodes)

	// The callback runs in a goroutine, so give it a moment.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	require.NotNil(t, callbackDetected, "callback should have been invoked")
	assert.True(t, *callbackDetected)
	assert.Equal(t, testNodes, callbackNodes)
}
