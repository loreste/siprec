package sip

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/pion/stun"
	"github.com/sirupsen/logrus"

	"siprec-server/pkg/version"
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
	httpClient *http.Client
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
		logger:     logger,
		timeout:    5 * time.Second,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// GetExternalIP detects external IP via HTTP services
func (hc *HTTPFallbackClient) GetExternalIP(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if len(hc.services) == 0 {
		return "", fmt.Errorf("no HTTP fallback services configured")
	}

	client := hc.httpClient
	if client == nil {
		client = &http.Client{Timeout: hc.timeout}
		hc.httpClient = client
	}

	var lastErr error
	for _, service := range hc.services {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		url := strings.TrimSpace(service)
		if url == "" {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			hc.logger.WithError(err).WithField("service", service).Debug("Failed to construct HTTP fallback request")
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", version.UserAgent())

		resp, err := client.Do(req)
		if err != nil {
			hc.logger.WithError(err).WithField("service", service).Debug("HTTP fallback request failed")
			lastErr = err
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if err != nil {
			hc.logger.WithError(err).WithField("service", service).Debug("Failed to read HTTP fallback response")
			lastErr = err
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			err = fmt.Errorf("unexpected status code %d", resp.StatusCode)
			hc.logger.WithError(err).WithField("service", service).Debug("HTTP fallback returned non-success status")
			lastErr = err
			continue
		}

		ipStr := strings.TrimSpace(string(body))
		if ipStr == "" {
			hc.logger.WithField("service", service).Debug("HTTP fallback returned empty body")
			lastErr = fmt.Errorf("empty response")
			continue
		}

		if net.ParseIP(ipStr) == nil {
			hc.logger.WithFields(logrus.Fields{
				"service": service,
				"value":   ipStr,
			}).Debug("HTTP fallback returned invalid IP")
			lastErr = fmt.Errorf("invalid IP response: %s", ipStr)
			continue
		}

		hc.logger.WithFields(logrus.Fields{
			"service":     service,
			"external_ip": ipStr,
		}).Info("Successfully detected external IP via HTTP fallback")
		return ipStr, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("failed HTTP fallback lookup: %w", lastErr)
	}

	return "", fmt.Errorf("failed to detect external IP via HTTP fallback")
}
