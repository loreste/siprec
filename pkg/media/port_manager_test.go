package media

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocatePortPairCooldown(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	pm := NewPortManager(20000, 20020)

	pair, err := pm.AllocatePortPair()
	require.NoError(t, err)
	require.Equal(t, 0, pair.RTPPort%2)
	require.Equal(t, pair.RTPPort+1, pair.RTCPPort)

	stats := pm.GetStats()
	require.Equal(t, 2, stats.UsedPorts)
	require.Equal(t, stats.TotalPorts-2, stats.AvailablePorts)

	pm.ReleasePortPair(pair)

	// After release the port enters cooldown; the allocator should pick a
	// different port to avoid cross-talk from stale RTP traffic.
	next, err := pm.AllocatePortPair()
	require.NoError(t, err)
	require.NotEqual(t, pair.RTPPort, next.RTPPort, "expected cooldown to prevent immediate reuse")

	pm.ReleasePortPair(next)
}

func TestAllocatePortPairFallbackToCooldown(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	// Range 20000-20020 has 10 valid port pairs (20000/20001 through 20018/20019).
	pm := NewPortManager(20000, 20020)

	// Allocate first pair and release — it enters cooldown.
	first, err := pm.AllocatePortPair()
	require.NoError(t, err)
	pm.ReleasePortPair(first)

	// Allocate the remaining 9 fresh pairs to exhaust fresh ports.
	var others []*PortPair
	for i := 0; i < 9; i++ {
		p, err := pm.AllocatePortPair()
		require.NoError(t, err)
		require.NotEqual(t, first.RTPPort, p.RTPPort, "fresh scan should not return cooldown port")
		others = append(others, p)
	}

	// Now all fresh ports are used; only the cooldown pair is available.
	reused, err := pm.AllocatePortPair()
	require.NoError(t, err)
	require.Equal(t, first.RTPPort, reused.RTPPort, "should fall back to cooldown port when no fresh ports available")

	pm.ReleasePortPair(reused)
	for _, p := range others {
		pm.ReleasePortPair(p)
	}
}

func TestAllocatePortCooldown(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	pm := NewPortManager(30000, 30020)

	first, err := pm.AllocatePort()
	require.NoError(t, err)
	require.Equal(t, 0, first%2)

	second, err := pm.AllocatePort()
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, 0, second%2)

	pm.ReleasePort(first)
	pm.ReleasePort(second)

	// Both released ports are cooling down; allocator should prefer a fresh
	// port that was never used.
	reused, err := pm.AllocatePort()
	require.NoError(t, err)
	require.NotEqual(t, first, reused, "expected cooldown to prevent immediate reuse of first")
	require.NotEqual(t, second, reused, "expected cooldown to prevent immediate reuse of second")

	pm.ReleasePort(reused)
}

func TestAllocatePortFallbackToCooldown(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	// Range 30000-30020 has 11 even ports (30000, 30002, ..., 30020).
	pm := NewPortManager(30000, 30020)

	// Allocate first port and release — it enters cooldown.
	first, err := pm.AllocatePort()
	require.NoError(t, err)
	pm.ReleasePort(first)

	// Allocate remaining fresh ports to exhaust them.
	var others []int
	for i := 0; i < 10; i++ {
		p, err := pm.AllocatePort()
		require.NoError(t, err)
		require.NotEqual(t, first, p, "fresh scan should not return cooldown port")
		others = append(others, p)
	}

	// Only the cooldown port is free; must fall back.
	reused, err := pm.AllocatePort()
	require.NoError(t, err)
	require.Equal(t, first, reused, "should fall back to cooldown port when no fresh ports available")

	pm.ReleasePort(reused)
	for _, p := range others {
		pm.ReleasePort(p)
	}
}
