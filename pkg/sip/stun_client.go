package sip

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pion/stun"
	"github.com/sirupsen/logrus"
)

// STUNClient handles STUN-based external IP detection
type STUNClient struct {
	servers []string
	logger  *logrus.Logger
	timeout time.Duration
}

// NewSTUNClient creates a new STUN client
func NewSTUNClient(servers []string, logger *logrus.Logger) *STUNClient {
	if len(servers) == 0 {
		// Default public STUN servers
		servers = []string{
			"stun.l.google.com:19302",
			"stun1.l.google.com:19302",
			"stun2.l.google.com:19302",
			"stun3.l.google.com:19302",
			"stun4.l.google.com:19302",
			"stun.stunprotocol.org:3478",
			"stun.voip.blackberry.com:3478",
			"stun.nextcloud.com:3478",
		}
	}

	return &STUNClient{
		servers: servers,
		logger:  logger,
		timeout: 5 * time.Second,
	}
}

// GetExternalIP detects the external IP address using STUN
func (sc *STUNClient) GetExternalIP(ctx context.Context) (string, error) {
	// Try each STUN server until we get a response
	for _, server := range sc.servers {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
			ip, err := sc.querySTUNServer(ctx, server)
			if err != nil {
				sc.logger.WithError(err).WithField("server", server).Debug("STUN query failed")
				continue
			}
			
			sc.logger.WithFields(logrus.Fields{
				"server":      server,
				"external_ip": ip,
			}).Info("Successfully detected external IP via STUN")
			
			return ip, nil
		}
	}

	return "", fmt.Errorf("failed to detect external IP from any STUN server")
}

// querySTUNServer queries a single STUN server
func (sc *STUNClient) querySTUNServer(ctx context.Context, server string) (string, error) {
	// Create a context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, sc.timeout)
	defer cancel()

	// Resolve STUN server address
	serverAddr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return "", fmt.Errorf("failed to resolve STUN server address: %w", err)
	}

	// Create UDP connection
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return "", fmt.Errorf("failed to connect to STUN server: %w", err)
	}
	defer conn.Close()

	// Set deadline based on context
	deadline, ok := queryCtx.Deadline()
	if ok {
		conn.SetDeadline(deadline)
	}

	// Create STUN message
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	// Send STUN request
	if _, err := conn.Write(message.Raw); err != nil {
		return "", fmt.Errorf("failed to send STUN request: %w", err)
	}

	// Read response
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read STUN response: %w", err)
	}

	// Parse STUN response
	response := new(stun.Message)
	response.Raw = buf[:n]
	if err := response.Decode(); err != nil {
		return "", fmt.Errorf("failed to decode STUN response: %w", err)
	}

	// Extract XOR-MAPPED-ADDRESS attribute
	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(response); err != nil {
		// Try MAPPED-ADDRESS as fallback
		var mappedAddr stun.MappedAddress
		if err := mappedAddr.GetFrom(response); err != nil {
			return "", fmt.Errorf("no address found in STUN response")
		}
		return mappedAddr.IP.String(), nil
	}

	return xorAddr.IP.String(), nil
}

// HTTPFallbackClient provides HTTP-based external IP detection as fallback
type HTTPFallbackClient struct {
	services []string
	logger   *logrus.Logger
	timeout  time.Duration
}

// NewHTTPFallbackClient creates a new HTTP fallback client
func NewHTTPFallbackClient(logger *logrus.Logger) *HTTPFallbackClient {
	return &HTTPFallbackClient{
		services: []string{
			"https://api.ipify.org",
			"https://ipinfo.io/ip",
			"https://checkip.amazonaws.com",
			"https://icanhazip.com",
		},
		logger:  logger,
		timeout: 5 * time.Second,
	}
}

// GetExternalIP detects external IP via HTTP services
func (hc *HTTPFallbackClient) GetExternalIP(ctx context.Context) (string, error) {
	// Implementation would use net/http to query these services
	// For now, return error to avoid external dependencies
	return "", fmt.Errorf("HTTP fallback not implemented")
}