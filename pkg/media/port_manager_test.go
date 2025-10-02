package media

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocatePortPairWithoutSystemPorts(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	pm := NewPortManager(20000, 20010)

	pair, err := pm.AllocatePortPair()
	require.NoError(t, err)
	require.Equal(t, 0, pair.RTPPort%2)
	require.Equal(t, pair.RTPPort+1, pair.RTCPPort)

	stats := pm.GetStats()
	require.Equal(t, 2, stats.UsedPorts)
	require.Equal(t, stats.TotalPorts-2, stats.AvailablePorts)

	pm.ReleasePortPair(pair)

	reused, err := pm.AllocatePortPair()
	require.NoError(t, err)
	require.Equal(t, pair.RTPPort, reused.RTPPort, "expected recently freed port to be reused")

	pm.ReleasePortPair(reused)
}

func TestAllocatePortSequenceWithMockedAvailability(t *testing.T) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	defer restore()

	pm := NewPortManager(30000, 30006)

	first, err := pm.AllocatePort()
	require.NoError(t, err)
	require.Equal(t, 0, first%2)

	second, err := pm.AllocatePort()
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.Equal(t, 0, second%2)

	pm.ReleasePort(first)
	pm.ReleasePort(second)

	reused, err := pm.AllocatePort()
	require.NoError(t, err)
	require.Equal(t, first, reused, "expected recently freed port to be reused")

	pm.ReleasePort(reused)
}
