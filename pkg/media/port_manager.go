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

// AllocatePort allocates a free RTP port from the configured range with optimization.
// Recently freed ports are kept in a cooldown period to prevent cross-talk from
// stale RTP packets arriving on reused ports. Fresh ports are preferred.
func (pm *PortManager) AllocatePort() (int, error) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	coolingDown := pm.buildCooldownSet()

	// First try — prefer ports that are NOT recently freed (cooldown)
	for port := pm.minPort; port <= pm.maxPort; port += 2 {
		if !pm.usedPorts[port] && !coolingDown[port] {
			if portAvailable(port) {
				pm.usedPorts[port] = true
				pm.stats.AllocationCount++
				pm.updateStats()
				return port, nil
			}
		}
	}

	// Fallback — use cooling-down ports rather than failing
	for port := range coolingDown {
		if !pm.usedPorts[port] && portAvailable(port) {
			pm.usedPorts[port] = true
			pm.stats.AllocationCount++
			pm.stats.ReuseHits++
			pm.updateStats()
			return port, nil
		}
	}

	// Last resort — check all ports regardless of usedPorts map
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

// AllocatePortPair allocates an RTP/RTCP port pair according to RFC 3550.
// RTP uses even port, RTCP uses RTP port + 1 (odd port).
// Recently freed ports are kept in cooldown to prevent cross-talk.
func (pm *PortManager) AllocatePortPair() (*PortPair, error) {
	pm.portsMutex.Lock()
	defer pm.portsMutex.Unlock()

	coolingDown := pm.buildCooldownSet()

	// First try — prefer pairs where the RTP port is NOT cooling down
	for port := pm.minPort; port <= pm.maxPort-1; port += 2 {
		if port%2 != 0 {
			continue
		}
		rtpPort := port
		rtcpPort := port + 1

		if !pm.usedPorts[rtpPort] && !pm.usedPorts[rtcpPort] && !coolingDown[rtpPort] {
			if portAvailable(rtpPort) && portAvailable(rtcpPort) {
				pm.usedPorts[rtpPort] = true
				pm.usedPorts[rtcpPort] = true
				pm.stats.AllocationCount += 2
				pm.updateStats()
				return &PortPair{RTPPort: rtpPort, RTCPPort: rtcpPort}, nil
			}
		}
	}

	// Fallback — use cooling-down port pairs rather than failing
	for port := range coolingDown {
		if port%2 != 0 {
			continue
		}
		rtpPort := port
		rtcpPort := port + 1
		if rtcpPort > pm.maxPort {
			continue
		}
		if !pm.usedPorts[rtpPort] && !pm.usedPorts[rtcpPort] {
			if portAvailable(rtpPort) && portAvailable(rtcpPort) {
				pm.usedPorts[rtpPort] = true
				pm.usedPorts[rtcpPort] = true
				pm.stats.AllocationCount += 2
				pm.stats.ReuseHits++
				pm.updateStats()
				return &PortPair{RTPPort: rtpPort, RTCPPort: rtcpPort}, nil
			}
		}
	}

	// Last resort — check all even ports regardless of usedPorts map
	for port := pm.minPort; port <= pm.maxPort-1; port += 2 {
		if port%2 != 0 {
			continue
		}
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

// buildCooldownSet returns a set of ports that were recently freed and should
// be avoided to prevent cross-talk from stale RTP traffic. Caller must hold
// portsMutex.
func (pm *PortManager) buildCooldownSet() map[int]bool {
	ports := pm.getRecentlyFreedPorts()
	set := make(map[int]bool, len(ports))
	for _, p := range ports {
		set[p] = true
	}
	return set
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
