package media

import (
	"fmt"
	"net"
	"sync"
	"time"

	"siprec-server/pkg/util"
)

// PortManager handles allocation and deallocation of RTP ports with optimization
type PortManager struct {
	minPort      int
	maxPort      int
	portsMutex   sync.RWMutex
	usedPorts    map[int]bool
	recentlyUsed *util.LRUCache
	stats        PortManagerStats
}

// PortManagerStats tracks port allocation statistics
type PortManagerStats struct {
	TotalPorts       int
	UsedPorts        int
	AvailablePorts   int
	AllocationCount  int64
	DeallocationCount int64
	ReuseHits        int64
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

	totalPorts := (maxPort - minPort + 1) / 2 // Even ports only
	cacheSize := totalPorts / 4 // Cache 25% of ports for reuse optimization

	return &PortManager{
		minPort:      minPort,
		maxPort:      maxPort,
		usedPorts:    make(map[int]bool),
		recentlyUsed: util.NewLRUCache(cacheSize, 5*time.Minute),
		stats: PortManagerStats{
			TotalPorts: totalPorts,
		},
	}
}

// AllocatePort allocates a free RTP port from the configured range with optimization
// Returns the allocated port or an error if no ports are available
func (pm *PortManager) AllocatePort() (int, error) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	// Try to reuse recently freed ports first (better for OS and networking)
	if cachedPorts := pm.getRecentlyFreedPorts(); len(cachedPorts) > 0 {
		for _, port := range cachedPorts {
			if !pm.usedPorts[port] && isPortAvailableWithLock(port) {
				pm.usedPorts[port] = true
				pm.stats.AllocationCount++
				pm.stats.ReuseHits++
				pm.updateStats()
				return port, nil
			}
		}
	}

	// First try - look for a port that's known to be available
	for port := pm.minPort; port <= pm.maxPort; port += 2 {
		// RTP ports are typically even
		if !pm.usedPorts[port] {
			// Check if the port is actually available while holding the lock
			// to prevent race conditions with other goroutines
			if isPortAvailableWithLock(port) {
				pm.usedPorts[port] = true
				pm.stats.AllocationCount++
				pm.updateStats()
				return port, nil
			}
		}
	}

	// Second try - check all ports in the range regardless of our usedPorts map
	// This handles cases where ports were released externally
	for port := pm.minPort; port <= pm.maxPort; port += 2 {
		if isPortAvailableWithLock(port) {
			pm.usedPorts[port] = true
			pm.stats.AllocationCount++
			pm.updateStats()
			return port, nil
		}
	}

	return 0, fmt.Errorf("no free ports available in range %d-%d", pm.minPort, pm.maxPort)
}

// ReleasePort releases a previously allocated port with optimization
func (pm *PortManager) ReleasePort(port int) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	if pm.usedPorts[port] {
		delete(pm.usedPorts, port)
		pm.stats.DeallocationCount++
		
		// Cache recently freed port for reuse optimization
		pm.recentlyUsed.Set(fmt.Sprintf("port_%d", port), port)
		
		pm.updateStats()
	}
}

// GetPortRange returns the configured port range
func (pm *PortManager) GetPortRange() (min, max int) {
	return pm.minPort, pm.maxPort
}

// GetUsedPortCount returns the number of currently allocated ports
func (pm *PortManager) GetUsedPortCount() int {
	pm.portsMutex.RLock()
	defer pm.portsMutex.RUnlock()

	return len(pm.usedPorts)
}

// GetStats returns port manager statistics
func (pm *PortManager) GetStats() PortManagerStats {
	pm.portsMutex.RLock()
	defer pm.portsMutex.RUnlock()

	stats := pm.stats
	stats.UsedPorts = len(pm.usedPorts)
	stats.AvailablePorts = pm.stats.TotalPorts - stats.UsedPorts
	return stats
}

// getRecentlyFreedPorts returns recently freed ports for reuse optimization
func (pm *PortManager) getRecentlyFreedPorts() []int {
	var ports []int
	keys := pm.recentlyUsed.Keys()
	
	for _, key := range keys {
		if cached, found := pm.recentlyUsed.Get(key); found {
			if port, ok := cached.(int); ok {
				ports = append(ports, port)
			}
		}
	}
	
	return ports
}

// updateStats updates internal statistics (assumes lock is held)
func (pm *PortManager) updateStats() {
	pm.stats.UsedPorts = len(pm.usedPorts)
	pm.stats.AvailablePorts = pm.stats.TotalPorts - pm.stats.UsedPorts
}

// isPortAvailable checks if a UDP port is available for binding
// This function should only be called from outside AllocatePort
func isPortAvailable(port int) bool {
	addr := net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// isPortAvailableWithLock checks if a UDP port is available for binding
// This variant is used within AllocatePort while the mutex is held
func isPortAvailableWithLock(port int) bool {
	addr := net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
