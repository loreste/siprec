package media

import (
	"fmt"
	"net"
	"sync"
)

// PortManager handles allocation and deallocation of RTP ports
type PortManager struct {
	minPort    int
	maxPort    int
	portsMutex sync.Mutex
	usedPorts  map[int]bool
}

// NewPortManager creates a new port manager with the specified port range
func NewPortManager(minPort, maxPort int) *PortManager {
	if minPort <= 0 || maxPort <= 0 {
		// Default to common RTP port range if invalid values provided
		minPort = 10000
		maxPort = 20000
	}
	
	// Ensure minPort < maxPort
	if minPort >= maxPort {
		minPort = 10000
		maxPort = 20000
	}

	return &PortManager{
		minPort:   minPort,
		maxPort:   maxPort,
		usedPorts: make(map[int]bool),
	}
}

// AllocatePort allocates a free RTP port from the configured range
// Returns the allocated port or an error if no ports are available
func (pm *PortManager) AllocatePort() (int, error) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()
	
	// First try - look for a port that's known to be available
	for port := pm.minPort; port <= pm.maxPort; port += 2 {
		// RTP ports are typically even
		if !pm.usedPorts[port] {
			// Check if the port is actually available
			if isPortAvailable(port) {
				pm.usedPorts[port] = true
				return port, nil
			}
		}
	}
	
	// Second try - check all ports in the range regardless of our usedPorts map
	// This handles cases where ports were released externally
	for port := pm.minPort; port <= pm.maxPort; port += 2 {
		if isPortAvailable(port) {
			pm.usedPorts[port] = true
			return port, nil
		}
	}
	
	return 0, fmt.Errorf("no free ports available in range %d-%d", pm.minPort, pm.maxPort)
}

// ReleasePort releases a previously allocated port
func (pm *PortManager) ReleasePort(port int) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()
	
	delete(pm.usedPorts, port)
}

// GetPortRange returns the configured port range
func (pm *PortManager) GetPortRange() (min, max int) {
	return pm.minPort, pm.maxPort
}

// GetUsedPortCount returns the number of currently allocated ports
func (pm *PortManager) GetUsedPortCount() int {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()
	
	return len(pm.usedPorts)
}

// isPortAvailable checks if a UDP port is available for binding
func isPortAvailable(port int) bool {
	addr := net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}