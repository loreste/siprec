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

// PortPair represents an RTP/RTCP port pair as required by RFC 3550
type PortPair struct {
	RTPPort  int // Even port for RTP
	RTCPPort int // Odd port for RTCP (RTP + 1)
}

var (
	portCheckMu sync.RWMutex
	portCheckFn = defaultPortAvailabilityCheck
)

// setPortAvailabilityChecker overrides the UDP port availability check. It is
// intended for use in tests where the runtime sandbox forbids binding to
// arbitrary UDP ports. The returned function restores the previous checker and
// should typically be deferred by the caller.
func setPortAvailabilityChecker(fn func(int) bool) func() {
	portCheckMu.Lock()
	previous := portCheckFn
	portCheckFn = fn
	portCheckMu.Unlock()

	return func() {
		portCheckMu.Lock()
		portCheckFn = previous
		portCheckMu.Unlock()
	}
}

func portAvailable(port int) bool {
	portCheckMu.RLock()
	checker := portCheckFn
	portCheckMu.RUnlock()
	return checker(port)
}

// PortManagerStats tracks port allocation statistics
type PortManagerStats struct {
	TotalPorts        int
	UsedPorts         int
	AvailablePorts    int
	AllocationCount   int64
	DeallocationCount int64
	ReuseHits         int64
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
	cacheSize := totalPorts / 4               // Cache 25% of ports for reuse optimization

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
			if !pm.usedPorts[port] && portAvailable(port) {
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
			if portAvailable(port) {
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
		if portAvailable(port) {
			pm.usedPorts[port] = true
			pm.stats.AllocationCount++
			pm.updateStats()
			return port, nil
		}
	}

	return 0, fmt.Errorf("no free ports available in range %d-%d", pm.minPort, pm.maxPort)
}

// AllocatePortPair allocates an RTP/RTCP port pair according to RFC 3550
// RTP uses even port, RTCP uses RTP port + 1 (odd port)
// Returns PortPair or error if no pairs are available
func (pm *PortManager) AllocatePortPair() (*PortPair, error) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	// Try to reuse recently freed port pairs first
	if cachedPorts := pm.getRecentlyFreedPorts(); len(cachedPorts) > 0 {
		for _, rtpPort := range cachedPorts {
			rtcpPort := rtpPort + 1
			// Ensure RTP port is even and both ports are available
			if rtpPort%2 == 0 && rtcpPort <= pm.maxPort &&
				!pm.usedPorts[rtpPort] && !pm.usedPorts[rtcpPort] &&
				portAvailable(rtpPort) && portAvailable(rtcpPort) {

				pm.usedPorts[rtpPort] = true
				pm.usedPorts[rtcpPort] = true
				pm.stats.AllocationCount += 2 // Count both ports
				pm.stats.ReuseHits++
				pm.updateStats()
				return &PortPair{RTPPort: rtpPort, RTCPPort: rtcpPort}, nil
			}
		}
	}

	// First try - look for consecutive even/odd port pairs
	for port := pm.minPort; port <= pm.maxPort-1; port += 2 {
		// Ensure port is even (RTP requirement)
		if port%2 == 0 {
			rtpPort := port
			rtcpPort := port + 1

			// Check if both ports are available
			if !pm.usedPorts[rtpPort] && !pm.usedPorts[rtcpPort] {
				if portAvailable(rtpPort) && portAvailable(rtcpPort) {
					pm.usedPorts[rtpPort] = true
					pm.usedPorts[rtcpPort] = true
					pm.stats.AllocationCount += 2 // Count both ports
					pm.updateStats()
					return &PortPair{RTPPort: rtpPort, RTCPPort: rtcpPort}, nil
				}
			}
		}
	}

	// Second try - check all even ports regardless of our usedPorts map
	for port := pm.minPort; port <= pm.maxPort-1; port += 2 {
		if port%2 == 0 {
			rtpPort := port
			rtcpPort := port + 1

			if portAvailable(rtpPort) && portAvailable(rtcpPort) {
				pm.usedPorts[rtpPort] = true
				pm.usedPorts[rtcpPort] = true
				pm.stats.AllocationCount += 2
				pm.updateStats()
				return &PortPair{RTPPort: rtpPort, RTCPPort: rtcpPort}, nil
			}
		}
	}

	return nil, fmt.Errorf("no free RTP/RTCP port pairs available in range %d-%d", pm.minPort, pm.maxPort)
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

// ReleasePortPair releases a previously allocated RTP/RTCP port pair
func (pm *PortManager) ReleasePortPair(pair *PortPair) {
	if pair == nil {
		return
	}

	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	// Release both RTP and RTCP ports
	if pm.usedPorts[pair.RTPPort] {
		delete(pm.usedPorts, pair.RTPPort)
		pm.stats.DeallocationCount++
		// Cache RTP port for reuse (RTCP port will be RTP + 1)
		pm.recentlyUsed.Set(fmt.Sprintf("port_%d", pair.RTPPort), pair.RTPPort)
	}

	if pm.usedPorts[pair.RTCPPort] {
		delete(pm.usedPorts, pair.RTCPPort)
		pm.stats.DeallocationCount++
	}

	pm.updateStats()
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

func defaultPortAvailabilityCheck(port int) bool {
	addr := net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
