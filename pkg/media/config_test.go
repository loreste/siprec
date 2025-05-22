package media

import (
	"testing"

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

func TestPortManager(t *testing.T) {
	minPort := 10000
	maxPort := 10010

	// Create a new port manager for testing
	pm := NewPortManager(minPort, maxPort)

	// Allocate a bunch of ports
	var ports []int
	for i := 0; i < 5; i++ {
		port, err := pm.AllocatePort()
		assert.NoError(t, err, "Should be able to allocate port")
		assert.GreaterOrEqual(t, port, minPort, "Port should be >= min")
		assert.LessOrEqual(t, port, maxPort, "Port should be <= max")
		ports = append(ports, port)
	}

	// Release all ports
	for _, port := range ports {
		pm.ReleasePort(port)
	}

	// Test that we can allocate again after releasing
	for i := 0; i < 5; i++ {
		port, err := pm.AllocatePort()
		assert.NoError(t, err, "Should be able to allocate port after release")
		pm.ReleasePort(port)
	}
}

func TestPortManagerBasicFunctionality(t *testing.T) {
	minPort := 50000 // Use high port numbers to avoid conflicts
	maxPort := 50004 // Only even ports: 50000, 50002, 50004 (3 ports available)

	pm := NewPortManager(minPort, maxPort)

	// Test allocation and release
	port1, err := pm.AllocatePort()
	assert.NoError(t, err, "Should be able to allocate first port")
	assert.True(t, port1 >= minPort && port1 <= maxPort, "Port should be in range")
	assert.True(t, port1%2 == 0, "Port should be even")

	port2, err := pm.AllocatePort()
	assert.NoError(t, err, "Should be able to allocate second port")
	assert.NotEqual(t, port1, port2, "Should get different ports")

	// Release first port
	pm.ReleasePort(port1)

	// Allocate again - might get the same port back
	port3, err := pm.AllocatePort()
	assert.NoError(t, err, "Should be able to allocate after release")

	// Clean up
	pm.ReleasePort(port2)
	pm.ReleasePort(port3)
}

func TestGlobalPortManager(t *testing.T) {
	// Test the global port manager functions
	InitPortManager(20000, 20010)
	pm := GetPortManager()

	assert.NotNil(t, pm, "Global port manager should not be nil")

	// Test allocation
	port, err := pm.AllocatePort()
	assert.NoError(t, err, "Should be able to allocate from global manager")
	assert.GreaterOrEqual(t, port, 20000, "Port should be in range")
	assert.LessOrEqual(t, port, 20010, "Port should be in range")

	// Release the port
	pm.ReleasePort(port)
}
