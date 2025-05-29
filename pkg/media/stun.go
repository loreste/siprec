// Package media provides STUN discovery functionality for NAT traversal
// This implements basic STUN protocol support for determining external
// IP addresses and NAT type detection in cloud environments.
//
// Author: SIPREC Server Project
// License: MIT

package media

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// NATMapping represents discovered NAT mapping information
type NATMapping struct {
	ExternalIP   string
	ExternalPort int
	NATType      string
	LocalIP      string
	LocalPort    int
}

// NATType constants
const (
	NATTypeUnknown           = "unknown"
	NATTypeOpenInternet      = "open_internet"
	NATTypeFullCone          = "full_cone"
	NATTypeRestrictedCone    = "restricted_cone"
	NATTypePortRestricted    = "port_restricted"
	NATTypeSymmetric         = "symmetric"
	NATTypeBlocked           = "blocked"
)

// BasicSTUNDiscovery performs basic STUN discovery to determine external IP
// This is a simplified STUN implementation for basic NAT detection
func BasicSTUNDiscovery(stunServers []string, logger *logrus.Logger) (*NATMapping, error) {
	if len(stunServers) == 0 {
		return nil, fmt.Errorf("no STUN servers provided")
	}

	// Try each STUN server
	for _, server := range stunServers {
		mapping, err := stunDiscoveryFromServer(server, logger)
		if err != nil {
			logger.WithError(err).WithField("stun_server", server).Warn("STUN discovery failed, trying next server")
			continue
		}
		
		logger.WithFields(logrus.Fields{
			"stun_server":   server,
			"external_ip":   mapping.ExternalIP,
			"external_port": mapping.ExternalPort,
			"nat_type":      mapping.NATType,
		}).Info("STUN discovery successful")
		
		return mapping, nil
	}

	return nil, fmt.Errorf("STUN discovery failed for all servers")
}

// stunDiscoveryFromServer performs STUN discovery from a single server
func stunDiscoveryFromServer(stunServer string, logger *logrus.Logger) (*NATMapping, error) {
	// Parse STUN server address
	server := strings.TrimPrefix(stunServer, "stun:")
	if !strings.Contains(server, ":") {
		server += ":3478" // Default STUN port
	}

	// Resolve STUN server address
	stunAddr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve STUN server %s: %w", server, err)
	}

	// Create local UDP connection
	localConn, err := net.DialUDP("udp", nil, stunAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to STUN server %s: %w", server, err)
	}
	defer localConn.Close()

	// Set timeout
	localConn.SetDeadline(time.Now().Add(5 * time.Second))

	// Get local address
	localAddr := localConn.LocalAddr().(*net.UDPAddr)

	// Create basic STUN binding request
	stunRequest := createSTUNBindingRequest()
	
	// Send STUN request
	_, err = localConn.Write(stunRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send STUN request: %w", err)
	}

	// Read STUN response
	response := make([]byte, 1024)
	n, err := localConn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to read STUN response: %w", err)
	}

	// Parse STUN response
	externalIP, externalPort, err := parseSTUNResponse(response[:n])
	if err != nil {
		return nil, fmt.Errorf("failed to parse STUN response: %w", err)
	}

	// Determine basic NAT type
	natType := determineBasicNATType(localAddr.IP.String(), localAddr.Port, externalIP, externalPort)

	return &NATMapping{
		ExternalIP:   externalIP,
		ExternalPort: externalPort,
		NATType:      natType,
		LocalIP:      localAddr.IP.String(),
		LocalPort:    localAddr.Port,
	}, nil
}

// createSTUNBindingRequest creates a basic STUN binding request
func createSTUNBindingRequest() []byte {
	// Simple STUN Binding Request (RFC 5389)
	// Message Type: Binding Request (0x0001)
	// Message Length: 0 (no attributes)
	// Magic Cookie: 0x2112A442
	// Transaction ID: 12 random bytes (simplified to zeros for basic implementation)
	
	stunPacket := make([]byte, 20)
	
	// Message Type: Binding Request (0x0001)
	stunPacket[0] = 0x00
	stunPacket[1] = 0x01
	
	// Message Length: 0
	stunPacket[2] = 0x00
	stunPacket[3] = 0x00
	
	// Magic Cookie: 0x2112A442
	stunPacket[4] = 0x21
	stunPacket[5] = 0x12
	stunPacket[6] = 0xA4
	stunPacket[7] = 0x42
	
	// Transaction ID (12 bytes) - simplified to zeros for basic implementation
	// In production, this should be random
	for i := 8; i < 20; i++ {
		stunPacket[i] = 0x00
	}
	
	return stunPacket
}

// parseSTUNResponse parses a basic STUN response to extract external IP and port
func parseSTUNResponse(response []byte) (string, int, error) {
	if len(response) < 20 {
		return "", 0, fmt.Errorf("STUN response too short")
	}

	// Check if it's a Binding Success Response (0x0101)
	if response[0] != 0x01 || response[1] != 0x01 {
		return "", 0, fmt.Errorf("not a binding success response")
	}

	// Check magic cookie
	if response[4] != 0x21 || response[5] != 0x12 || response[6] != 0xA4 || response[7] != 0x42 {
		return "", 0, fmt.Errorf("invalid magic cookie")
	}

	// Parse attributes (simplified - look for XOR-MAPPED-ADDRESS)
	offset := 20
	msgLength := int(response[2])<<8 | int(response[3])
	
	for offset < 20+msgLength {
		if offset+4 > len(response) {
			break
		}
		
		attrType := int(response[offset])<<8 | int(response[offset+1])
		attrLength := int(response[offset+2])<<8 | int(response[offset+3])
		
		if attrType == 0x0020 { // XOR-MAPPED-ADDRESS
			if offset+4+attrLength > len(response) || attrLength < 8 {
				break
			}
			
			// Parse XOR-MAPPED-ADDRESS
			family := response[offset+5]
			if family == 0x01 { // IPv4
				// XOR port with magic cookie high 16 bits
				port := (int(response[offset+6])<<8 | int(response[offset+7])) ^ 0x2112
				
				// XOR IP with magic cookie
				ip := make([]byte, 4)
				magicCookie := []byte{0x21, 0x12, 0xA4, 0x42}
				for i := 0; i < 4; i++ {
					ip[i] = response[offset+8+i] ^ magicCookie[i]
				}
				
				return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3]), port, nil
			}
		}
		
		offset += 4 + attrLength
		// Pad to 4-byte boundary
		for offset%4 != 0 {
			offset++
		}
	}

	return "", 0, fmt.Errorf("XOR-MAPPED-ADDRESS not found in response")
}

// determineBasicNATType determines basic NAT type based on local and external addresses
func determineBasicNATType(localIP string, localPort int, externalIP string, externalPort int) string {
	// Basic NAT type detection
	if localIP == externalIP {
		if localPort == externalPort {
			return NATTypeOpenInternet
		}
		return NATTypeUnknown
	}
	
	// Behind NAT
	if localPort == externalPort {
		return NATTypeFullCone
	}
	
	// Different ports - could be Port Restricted or Symmetric
	// More sophisticated testing would be needed to distinguish
	return NATTypePortRestricted
}

// ValidateNATConfiguration validates NAT configuration against STUN discovery
func ValidateNATConfiguration(config *Config, stunServers []string, logger *logrus.Logger) error {
	if !config.BehindNAT {
		logger.Info("NAT not configured, skipping STUN validation")
		return nil
	}

	logger.Info("Validating NAT configuration with STUN discovery")
	
	mapping, err := BasicSTUNDiscovery(stunServers, logger)
	if err != nil {
		logger.WithError(err).Warn("STUN discovery failed, cannot validate NAT configuration")
		return nil // Don't fail startup, just warn
	}

	// Compare discovered external IP with configured external IP
	if config.ExternalIP != "" && config.ExternalIP != "auto" && config.ExternalIP != mapping.ExternalIP {
		logger.WithFields(logrus.Fields{
			"configured_external_ip": config.ExternalIP,
			"discovered_external_ip": mapping.ExternalIP,
		}).Warn("Configured external IP differs from STUN discovery")
	}

	logger.WithFields(logrus.Fields{
		"external_ip":   mapping.ExternalIP,
		"external_port": mapping.ExternalPort,
		"nat_type":      mapping.NATType,
		"local_ip":      mapping.LocalIP,
		"local_port":    mapping.LocalPort,
	}).Info("NAT configuration validated via STUN")

	return nil
}