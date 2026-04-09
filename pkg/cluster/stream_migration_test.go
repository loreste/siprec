package cluster

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMigrationManager(t *testing.T, client redis.UniversalClient, nodeID string) (*StreamMigrationManager, *RTPStateManager) {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	rtpMgr := NewRTPStateManager(client, nodeID, logger)
	migMgr := NewStreamMigrationManager(client, nodeID, rtpMgr, logger)
	return migMgr, rtpMgr
}

func TestMigrationInitiate(t *testing.T) {
	mr, client := setupTestRedis(t)
	_ = mr
	migMgr, rtpMgr := newTestMigrationManager(t, client, "source-node")

	// Register a stream first
	require.NoError(t, rtpMgr.RegisterStream(&RTPStreamState{
		CallUUID:  "mig-call-001",
		LocalPort: 60000,
		CodecName: "opus",
		StartTime: time.Now(),
	}))

	ctx := context.Background()
	task, err := migMgr.InitiateMigration(ctx, "mig-call-001", "target-node")
	require.NoError(t, err)
	require.NotNil(t, task)

	// Verify task fields
	assert.Equal(t, "mig-call-001", task.CallUUID)
	assert.Equal(t, "source-node", task.SourceNodeID)
	assert.Equal(t, "target-node", task.TargetNodeID)
	assert.Equal(t, MigrationStatusPending, task.Status)
	assert.False(t, task.StartedAt.IsZero())
	assert.NotEmpty(t, task.ID)

	// Verify task is stored in Redis
	key := migrationKeyPrefix + task.ID
	data, err := client.Get(ctx, key).Bytes()
	require.NoError(t, err)

	var stored MigrationTask
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, task.ID, stored.ID)
	assert.Equal(t, MigrationStatusPending, stored.Status)
	assert.Equal(t, "mig-call-001", stored.CallUUID)

	// Verify task is in pending migrations
	pending := migMgr.ListPendingMigrations()
	assert.Len(t, pending, 1)
	assert.Equal(t, task.ID, pending[0].ID)

	// Verify RTP state was updated to migrating
	stream, err := rtpMgr.GetStream("mig-call-001")
	require.NoError(t, err)
	assert.Equal(t, RTPStreamStatusMigrating, stream.Status)
	assert.Equal(t, "target-node", stream.MigrationTarget)
}

func TestMigrationInitiateStreamNotFound(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, _ := newTestMigrationManager(t, client, "source-node")

	ctx := context.Background()
	task, err := migMgr.InitiateMigration(ctx, "nonexistent", "target-node")
	assert.Error(t, err)
	assert.Nil(t, task)
	assert.Contains(t, err.Error(), "stream not found")
}

func TestMigrationSetHandlers(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, rtpMgr := newTestMigrationManager(t, client, "target-node")

	var requestCalled atomic.Int32
	var completeCalled atomic.Int32

	migMgr.SetMigrationHandler(func(task *MigrationTask) error {
		requestCalled.Add(1)
		// Simulate allocating ports for the migration
		task.NewLocalPort = 61000
		task.NewRTCPPort = 61001
		return nil
	})

	migMgr.SetCompletionHandler(func(task *MigrationTask) {
		completeCalled.Add(1)
	})

	// Register a stream on this node so GetStream works (simulating
	// a stream that was registered by the source node via Redis)
	require.NoError(t, rtpMgr.RegisterStream(&RTPStreamState{
		CallUUID:  "handler-call",
		LocalPort: 60100,
		CodecName: "opus",
	}))

	// Now initiate from this node (source == target for simplicity;
	// the important thing is that AcceptMigration calls the handlers)
	ctx := context.Background()
	task, err := migMgr.InitiateMigration(ctx, "handler-call", "target-node")
	require.NoError(t, err)

	// Accept the migration (this calls the handlers)
	err = migMgr.AcceptMigration(ctx, task.ID)
	require.NoError(t, err)

	assert.Equal(t, int32(1), requestCalled.Load())
	assert.Equal(t, int32(1), completeCalled.Load())

	// Verify the task was marked completed in Redis
	retrieved, err := migMgr.GetMigrationStatus(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, MigrationStatusCompleted, retrieved.Status)
	assert.False(t, retrieved.CompletedAt.IsZero())
}

func TestMigrationListPending(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, rtpMgr := newTestMigrationManager(t, client, "source-node")

	ctx := context.Background()

	// Register multiple streams
	callUUIDs := []string{"pending-1", "pending-2", "pending-3"}
	for _, uuid := range callUUIDs {
		require.NoError(t, rtpMgr.RegisterStream(&RTPStreamState{
			CallUUID:  uuid,
			LocalPort: 62000,
			CodecName: "pcmu",
		}))
	}

	// Initiate migrations for all
	for _, uuid := range callUUIDs {
		_, err := migMgr.InitiateMigration(ctx, uuid, "target-node")
		require.NoError(t, err)
	}

	// Verify all are listed as pending
	pending := migMgr.ListPendingMigrations()
	assert.Len(t, pending, 3)

	// Collect call UUIDs from pending list
	pendingUUIDs := make(map[string]bool)
	for _, task := range pending {
		pendingUUIDs[task.CallUUID] = true
	}
	for _, uuid := range callUUIDs {
		assert.True(t, pendingUUIDs[uuid], "expected %s in pending list", uuid)
	}
}

func TestMigrationMigrateAllStreams(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, rtpMgr := newTestMigrationManager(t, client, "source-node")

	ctx := context.Background()

	// Register several streams
	streamCount := 4
	for i := 0; i < streamCount; i++ {
		require.NoError(t, rtpMgr.RegisterStream(&RTPStreamState{
			CallUUID:  "all-" + string(rune('A'+i)),
			LocalPort: 63000 + i,
			CodecName: "opus",
		}))
	}

	// Verify local streams exist
	localBefore := rtpMgr.ListLocalStreams()
	assert.Len(t, localBefore, streamCount)

	// Migrate all streams to a target node
	err := migMgr.MigrateAllStreams(ctx, "target-node")
	require.NoError(t, err)

	// Verify all migrations were created as pending
	pending := migMgr.ListPendingMigrations()
	assert.Len(t, pending, streamCount)

	// Verify each migration targets the correct node
	for _, task := range pending {
		assert.Equal(t, "source-node", task.SourceNodeID)
		assert.Equal(t, "target-node", task.TargetNodeID)
		assert.Equal(t, MigrationStatusPending, task.Status)
	}

	// Verify each stream state was set to migrating
	for i := 0; i < streamCount; i++ {
		callUUID := "all-" + string(rune('A'+i))
		stream, err := rtpMgr.GetStream(callUUID)
		require.NoError(t, err)
		assert.Equal(t, RTPStreamStatusMigrating, stream.Status)
		assert.Equal(t, "target-node", stream.MigrationTarget)
	}
}

func TestMigrationMigrateAllStreamsEmpty(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, _ := newTestMigrationManager(t, client, "source-node")

	// Migrating with no streams should succeed with nil error
	err := migMgr.MigrateAllStreams(context.Background(), "target-node")
	assert.NoError(t, err)
}

func TestMigrationCancelMigration(t *testing.T) {
	_, client := setupTestRedis(t)
	migMgr, rtpMgr := newTestMigrationManager(t, client, "source-node")

	ctx := context.Background()

	require.NoError(t, rtpMgr.RegisterStream(&RTPStreamState{
		CallUUID:  "cancel-call",
		LocalPort: 64000,
		CodecName: "pcmu",
	}))

	task, err := migMgr.InitiateMigration(ctx, "cancel-call", "target-node")
	require.NoError(t, err)

	// Cancel the migration
	err = migMgr.CancelMigration(ctx, task.ID)
	require.NoError(t, err)

	// Verify task is cancelled in Redis
	retrieved, err := migMgr.GetMigrationStatus(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, MigrationStatusCancelled, retrieved.Status)

	// Verify removed from pending list
	pending := migMgr.ListPendingMigrations()
	assert.Empty(t, pending)

	// Verify stream state was reverted to active in Redis.
	// CancelMigration calls saveState directly (bypassing local cache),
	// so we verify the Redis value by reading the key directly.
	ctx2 := context.Background()
	key := rtpStateKeyPrefix + "cancel-call"
	data, getErr := client.Get(ctx2, key).Bytes()
	require.NoError(t, getErr)

	var stored RTPStreamState
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, RTPStreamStatusActive, stored.Status)
	assert.Empty(t, stored.MigrationTarget)
}
