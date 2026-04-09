package cluster

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRTPStateManager(t *testing.T, client redis.UniversalClient, nodeID string) *RTPStateManager {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return NewRTPStateManager(client, nodeID, logger)
}

func TestRTPStateRegisterStream(t *testing.T) {
	mr, client := setupTestRedis(t)
	_ = mr
	mgr := newTestRTPStateManager(t, client, "node-1")

	state := &RTPStreamState{
		CallUUID:         "call-001",
		SessionID:        "session-001",
		LocalPort:        20000,
		RTCPPort:         20001,
		LocalSSRC:        12345,
		CodecName:        "opus",
		CodecPayloadType: 111,
		SampleRate:       48000,
		Channels:         2,
		StartTime:        time.Now(),
	}

	err := mgr.RegisterStream(state)
	require.NoError(t, err)

	// Verify local cache
	mgr.stateMu.RLock()
	cached, exists := mgr.localStates["call-001"]
	mgr.stateMu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, "node-1", cached.NodeID)
	assert.Equal(t, RTPStreamStatusActive, cached.Status)
	assert.Equal(t, "opus", cached.CodecName)

	// Verify Redis
	key := rtpStateKeyPrefix + "call-001"
	data, err := client.Get(context.Background(), key).Bytes()
	require.NoError(t, err)

	var stored RTPStreamState
	require.NoError(t, json.Unmarshal(data, &stored))
	assert.Equal(t, "call-001", stored.CallUUID)
	assert.Equal(t, "node-1", stored.NodeID)
	assert.Equal(t, RTPStreamStatusActive, stored.Status)
	assert.Equal(t, 20000, stored.LocalPort)
}

func TestRTPStateRegisterStreamMissingCallUUID(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	err := mgr.RegisterStream(&RTPStreamState{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "call_uuid is required")
}

func TestRTPStateUpdateStream(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	state := &RTPStreamState{
		CallUUID:  "call-002",
		LocalPort: 20010,
		CodecName: "opus",
		StartTime: time.Now(),
	}
	require.NoError(t, mgr.RegisterStream(state))

	now := time.Now()
	updates := map[string]interface{}{
		"packets_received": int64(500),
		"bytes_received":   int64(64000),
		"packets_lost":     int64(3),
		"jitter":           float64(1.5),
		"last_packet_time": now,
		"remote_addr":      "192.168.1.100:5060",
		"remote_ssrc":      uint32(99999),
	}
	err := mgr.UpdateStream("call-002", updates)
	require.NoError(t, err)

	// Verify local cache reflects updates
	mgr.stateMu.RLock()
	updated := mgr.localStates["call-002"]
	mgr.stateMu.RUnlock()

	assert.Equal(t, int64(500), updated.PacketsReceived)
	assert.Equal(t, int64(64000), updated.BytesReceived)
	assert.Equal(t, int64(3), updated.PacketsLost)
	assert.InDelta(t, 1.5, updated.Jitter, 0.001)
	assert.Equal(t, "192.168.1.100:5060", updated.RemoteAddr)
	assert.Equal(t, uint32(99999), updated.RemoteSSRC)
	assert.WithinDuration(t, now, updated.LastPacketTime, time.Second)

	// Verify Redis reflects updates
	retrieved, err := mgr.GetStream("call-002")
	require.NoError(t, err)
	assert.Equal(t, int64(500), retrieved.PacketsReceived)
	assert.Equal(t, int64(64000), retrieved.BytesReceived)
}

func TestRTPStateUpdateStreamRecordingPaused(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	require.NoError(t, mgr.RegisterStream(&RTPStreamState{
		CallUUID:  "call-pause",
		LocalPort: 20020,
	}))

	// Pause recording
	err := mgr.UpdateStream("call-pause", map[string]interface{}{
		"recording_paused": true,
	})
	require.NoError(t, err)

	mgr.stateMu.RLock()
	s := mgr.localStates["call-pause"]
	mgr.stateMu.RUnlock()
	assert.True(t, s.RecordingPaused)
	assert.Equal(t, RTPStreamStatusPaused, s.Status)

	// Unpause recording
	err = mgr.UpdateStream("call-pause", map[string]interface{}{
		"recording_paused": false,
	})
	require.NoError(t, err)

	mgr.stateMu.RLock()
	s = mgr.localStates["call-pause"]
	mgr.stateMu.RUnlock()
	assert.False(t, s.RecordingPaused)
	assert.Equal(t, RTPStreamStatusActive, s.Status)
}

func TestRTPStateUpdateStreamNotFound(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	err := mgr.UpdateStream("nonexistent", map[string]interface{}{
		"packets_received": int64(1),
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream not found")
}

func TestRTPStateUnregisterStream(t *testing.T) {
	mr, client := setupTestRedis(t)
	_ = mr
	mgr := newTestRTPStateManager(t, client, "node-1")

	state := &RTPStreamState{
		CallUUID:  "call-003",
		LocalPort: 20030,
		CodecName: "pcmu",
	}
	require.NoError(t, mgr.RegisterStream(state))

	// Confirm it exists
	mgr.stateMu.RLock()
	_, exists := mgr.localStates["call-003"]
	mgr.stateMu.RUnlock()
	require.True(t, exists)

	// Unregister
	err := mgr.UnregisterStream("call-003")
	require.NoError(t, err)

	// Verify removed from local cache
	mgr.stateMu.RLock()
	_, exists = mgr.localStates["call-003"]
	mgr.stateMu.RUnlock()
	assert.False(t, exists)

	// Verify removed from Redis
	key := rtpStateKeyPrefix + "call-003"
	result := client.Get(context.Background(), key)
	assert.ErrorIs(t, result.Err(), redis.Nil)
}

func TestRTPStateUnregisterStreamNonexistent(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	// Unregistering a stream that was never registered should not error
	// (the Del call succeeds even if the key doesn't exist)
	err := mgr.UnregisterStream("nonexistent")
	assert.NoError(t, err)
}

func TestRTPStateListLocalStreams(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	// Register multiple streams
	for i := 0; i < 5; i++ {
		require.NoError(t, mgr.RegisterStream(&RTPStreamState{
			CallUUID:  "local-" + string(rune('A'+i)),
			LocalPort: 30000 + i,
			CodecName: "opus",
		}))
	}

	streams := mgr.ListLocalStreams()
	assert.Len(t, streams, 5)

	// Verify streams are copies (modifying returned state doesn't affect internal state)
	if len(streams) > 0 {
		streams[0].CodecName = "modified"
		mgr.stateMu.RLock()
		original := mgr.localStates[streams[0].CallUUID]
		mgr.stateMu.RUnlock()
		assert.Equal(t, "opus", original.CodecName)
	}
}

func TestRTPStateListStreams(t *testing.T) {
	_, client := setupTestRedis(t)

	// Two managers on different nodes sharing the same Redis
	mgr1 := newTestRTPStateManager(t, client, "node-1")
	mgr2 := newTestRTPStateManager(t, client, "node-2")

	// Register streams on node-1
	require.NoError(t, mgr1.RegisterStream(&RTPStreamState{
		CallUUID:  "n1-call-1",
		LocalPort: 40000,
	}))
	require.NoError(t, mgr1.RegisterStream(&RTPStreamState{
		CallUUID:  "n1-call-2",
		LocalPort: 40002,
	}))

	// Register streams on node-2
	require.NoError(t, mgr2.RegisterStream(&RTPStreamState{
		CallUUID:  "n2-call-1",
		LocalPort: 40010,
	}))

	ctx := context.Background()

	// List all streams (no node filter)
	all, err := mgr1.ListStreams(ctx, "")
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// List streams filtered by node-1
	node1Streams, err := mgr1.ListStreams(ctx, "node-1")
	require.NoError(t, err)
	assert.Len(t, node1Streams, 2)
	for _, s := range node1Streams {
		assert.Equal(t, "node-1", s.NodeID)
	}

	// List streams filtered by node-2
	node2Streams, err := mgr1.ListStreams(ctx, "node-2")
	require.NoError(t, err)
	assert.Len(t, node2Streams, 1)
	assert.Equal(t, "node-2", node2Streams[0].NodeID)

	// List streams filtered by non-existent node
	none, err := mgr1.ListStreams(ctx, "node-99")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestRTPStateConcurrentAccess(t *testing.T) {
	_, client := setupTestRedis(t)
	mgr := newTestRTPStateManager(t, client, "node-1")

	const goroutines = 20
	var wg sync.WaitGroup

	// Concurrent register
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := mgr.RegisterStream(&RTPStreamState{
				CallUUID:  "concurrent-" + string(rune('A'+idx)),
				LocalPort: 50000 + idx,
				CodecName: "opus",
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	streams := mgr.ListLocalStreams()
	assert.Len(t, streams, goroutines)

	// Concurrent update
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			callUUID := "concurrent-" + string(rune('A'+idx))
			err := mgr.UpdateStream(callUUID, map[string]interface{}{
				"packets_received": int64(100 + idx),
				"jitter":           float64(idx) * 0.1,
			})
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify updates applied
	for i := 0; i < goroutines; i++ {
		callUUID := "concurrent-" + string(rune('A'+i))
		s, err := mgr.GetStream(callUUID)
		require.NoError(t, err)
		assert.Equal(t, int64(100+i), s.PacketsReceived)
	}

	// Concurrent unregister
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			callUUID := "concurrent-" + string(rune('A'+idx))
			err := mgr.UnregisterStream(callUUID)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	streams = mgr.ListLocalStreams()
	assert.Empty(t, streams)
}
