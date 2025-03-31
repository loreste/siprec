package media

import (
	"testing"
	
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {
	// Test that we can create and use a Config struct
	config := &Config{
		RTPPortMin:    10000,
		RTPPortMax:    20000,
		EnableSRTP:    true,
		RecordingDir:  "/tmp/recordings",
		BehindNAT:     false,
		InternalIP:    "192.168.1.100",
		ExternalIP:    "203.0.113.1",
		DefaultVendor: "mock",
	}
	
	assert.Equal(t, 10000, config.RTPPortMin, "RTPPortMin should be set")
	assert.Equal(t, 20000, config.RTPPortMax, "RTPPortMax should be set")
	assert.True(t, config.EnableSRTP, "EnableSRTP should be true")
	assert.Equal(t, "/tmp/recordings", config.RecordingDir, "RecordingDir should be set")
	assert.False(t, config.BehindNAT, "BehindNAT should be false")
	assert.Equal(t, "192.168.1.100", config.InternalIP, "InternalIP should be set")
	assert.Equal(t, "203.0.113.1", config.ExternalIP, "ExternalIP should be set")
	assert.Equal(t, "mock", config.DefaultVendor, "DefaultVendor should be set")
}

func TestAllocateRTPPort(t *testing.T) {
	logger := logrus.New()
	minPort := 10000
	maxPort := 10010
	
	// Clear any existing port allocations
	PortMutex.Lock()
	UsedRTPPorts = make(map[int]bool)
	PortMutex.Unlock()
	
	// Allocate a bunch of ports
	var ports []int
	for i := 0; i < 5; i++ {
		port := AllocateRTPPort(minPort, maxPort, logger)
		ports = append(ports, port)
		
		// Verify port is in range
		assert.GreaterOrEqual(t, port, minPort, "Port should be >= minPort")
		assert.LessOrEqual(t, port, maxPort, "Port should be <= maxPort")
	}
	
	// Test releasing a specific port
	if len(ports) > 0 {
		portToRelease := ports[0]
		ReleaseRTPPort(portToRelease)
		
		// Allocate another port - should eventually get the one we just released
		found := false
		for i := 0; i < 10; i++ { // Try multiple times because it's random
			newPort := AllocateRTPPort(minPort, maxPort, logger)
			if newPort == portToRelease {
				found = true
				break
			}
			// Release it for the next attempt
			ReleaseRTPPort(newPort)
		}
		
		assert.True(t, found, "Should eventually allocate the released port")
	}
}

func TestRTPPortReuse(t *testing.T) {
	// Reset the UsedRTPPorts map to ensure a clean test
	PortMutex.Lock()
	UsedRTPPorts = make(map[int]bool)
	PortMutex.Unlock()
	
	logger := logrus.New()
	minPort := 10000
	maxPort := 10002 // Only 3 ports available
	
	// Allocate all ports
	port1 := AllocateRTPPort(minPort, maxPort, logger)
	port2 := AllocateRTPPort(minPort, maxPort, logger)
	port3 := AllocateRTPPort(minPort, maxPort, logger)
	
	// All ports should be in the valid range
	assert.GreaterOrEqual(t, port1, minPort)
	assert.LessOrEqual(t, port1, maxPort)
	assert.GreaterOrEqual(t, port2, minPort)
	assert.LessOrEqual(t, port2, maxPort)
	assert.GreaterOrEqual(t, port3, minPort)
	assert.LessOrEqual(t, port3, maxPort)
	
	// All ports should be different
	assert.NotEqual(t, port1, port2)
	assert.NotEqual(t, port2, port3)
	assert.NotEqual(t, port1, port3)
	
	// Release a port
	ReleaseRTPPort(port2)
	
	// Should reuse the released port
	newPort := AllocateRTPPort(minPort, maxPort, logger)
	assert.Equal(t, port2, newPort, "Released port should be reused")
}