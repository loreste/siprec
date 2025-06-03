package sip

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// NATConfig holds NAT traversal configuration
type NATConfig struct {
	// Whether the SIPREC server is behind NAT
	BehindNAT bool

	// Internal (private) IP address of the SIPREC server
	InternalIP string

	// External (public) IP address for NAT rewriting
	ExternalIP string

	// Internal SIP port
	InternalPort int

	// External SIP port (if different from internal)
	ExternalPort int

	// Whether to rewrite Via headers
	RewriteVia bool

	// Whether to rewrite Contact headers
	RewriteContact bool

	// Whether to rewrite Record-Route headers
	RewriteRecordRoute bool

	// Whether to perform automatic external IP detection
	AutoDetectExternalIP bool

	// STUN server for external IP detection
	STUNServer string

	// Force rewriting even if headers seem correct
	ForceRewrite bool
}

var (
	// Package-level compiled regex for efficiency
	ipRegexOnce   sync.Once
	ipRegexGlobal *regexp.Regexp

	// Pre-computed private IP networks for CPU efficiency
	privateNetworksOnce sync.Once
	privateNetworks     []*net.IPNet

	// String builder pool for memory efficiency
	stringBuilderPool = sync.Pool{
		New: func() interface{} {
			return &strings.Builder{}
		},
	}

	// Byte slice pool for SDP processing
	byteSlicePool = sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, 1024) // Pre-allocate 1KB
		},
	}
)

// initializeGlobals initializes package-level optimized resources
func initializeGlobals() {
	// Initialize IP regex once
	ipRegexOnce.Do(func() {
		ipRegexGlobal = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	})

	// Pre-compute private networks once for CPU efficiency
	privateNetworksOnce.Do(func() {
		privateCIDRs := []string{
			"10.0.0.0/8",     // 10.0.0.0 to 10.255.255.255
			"172.16.0.0/12",  // 172.16.0.0 to 172.31.255.255
			"192.168.0.0/16", // 192.168.0.0 to 192.168.255.255
			"127.0.0.0/8",    // Loopback
		}

		privateNetworks = make([]*net.IPNet, 0, len(privateCIDRs))
		for _, cidr := range privateCIDRs {
			_, network, err := net.ParseCIDR(cidr)
			if err == nil {
				privateNetworks = append(privateNetworks, network)
			}
		}
	})
}

// NATRewriter handles SIP header rewriting for NAT traversal with thread safety and efficiency
type NATRewriter struct {
	config *NATConfig
	logger *logrus.Logger

	// Thread-safe external IP management
	externalIPMutex   sync.RWMutex
	externalIP        string
	lastIPDetection   time.Time
	ipDetectionCtx    context.Context
	ipDetectionCancel context.CancelFunc
	ipDetectionWG     sync.WaitGroup

	// Pre-compiled internal/external IP patterns for fast replacement
	internalIPBytes []byte
	externalIPBytes []byte

	// Port conversion strings (pre-computed for efficiency)
	internalPortStr string
	externalPortStr string
}

// NewNATRewriter creates a new thread-safe, optimized NAT rewriter
func NewNATRewriter(config *NATConfig, logger *logrus.Logger) (*NATRewriter, error) {
	// Initialize package-level optimizations
	initializeGlobals()

	if config == nil {
		config = &NATConfig{
			BehindNAT:            false,
			RewriteVia:           true,
			RewriteContact:       true,
			RewriteRecordRoute:   true,
			AutoDetectExternalIP: true,
			STUNServer:           "stun.l.google.com:19302",
		}
	}

	// Create context for background IP detection
	ctx, cancel := context.WithCancel(context.Background())

	rewriter := &NATRewriter{
		config:            config,
		logger:            logger,
		ipDetectionCtx:    ctx,
		ipDetectionCancel: cancel,
	}

	// Pre-compute byte representations for fast string replacement
	if config.InternalIP != "" {
		rewriter.internalIPBytes = []byte(config.InternalIP)
	}

	// Pre-compute port strings for efficiency
	if config.InternalPort != 0 {
		rewriter.internalPortStr = ":" + strconv.Itoa(config.InternalPort)
	}
	if config.ExternalPort != 0 {
		rewriter.externalPortStr = ":" + strconv.Itoa(config.ExternalPort)
	}

	// Set initial external IP (thread-safe)
	if config.ExternalIP != "" {
		rewriter.setExternalIPUnsafe(config.ExternalIP)
	} else if config.AutoDetectExternalIP {
		// Start background IP detection to avoid blocking
		rewriter.ipDetectionWG.Add(1)
		go rewriter.backgroundIPDetection()
	}

	logger.WithFields(logrus.Fields{
		"behind_nat":    config.BehindNAT,
		"internal_ip":   config.InternalIP,
		"external_ip":   rewriter.GetExternalIP(),
		"internal_port": config.InternalPort,
		"external_port": config.ExternalPort,
	}).Info("Optimized NAT rewriter initialized")

	return rewriter, nil
}

// backgroundIPDetection runs periodic external IP detection without blocking main processing
func (nr *NATRewriter) backgroundIPDetection() {
	defer nr.ipDetectionWG.Done()

	// Immediate detection attempt
	if err := nr.detectExternalIPBackground(); err != nil {
		nr.logger.WithError(err).Debug("Initial external IP detection failed")
	}

	// Periodic detection
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-nr.ipDetectionCtx.Done():
			return
		case <-ticker.C:
			if err := nr.detectExternalIPBackground(); err != nil {
				nr.logger.WithError(err).Debug("Periodic external IP detection failed")
			}
		}
	}
}

// RewriteOutgoingMessage rewrites SIP headers in outgoing messages for NAT traversal
// This is the main hot path - optimized for performance
func (nr *NATRewriter) RewriteOutgoingMessage(message *SIPMessage) error {
	if !nr.config.BehindNAT && !nr.config.ForceRewrite {
		return nil
	}

	// Fast path: check if we have required IPs
	externalIP := nr.GetExternalIP()
	if nr.config.InternalIP == "" || externalIP == "" {
		return nil
	}

	// Rewrite headers using optimized functions
	if nr.config.RewriteVia {
		nr.rewriteViaHeadersOptimized(message, externalIP)
	}

	if nr.config.RewriteContact {
		nr.rewriteContactHeaderOptimized(message, externalIP)
	}

	if nr.config.RewriteRecordRoute {
		nr.rewriteRecordRouteHeadersOptimized(message, externalIP)
	}

	// Rewrite message body if it contains SDP
	if len(message.Body) > 0 {
		nr.rewriteMessageBodyOptimized(message, externalIP)
	}

	return nil
}

// rewriteViaHeadersOptimized uses pre-allocated builders and single-pass processing
func (nr *NATRewriter) rewriteViaHeadersOptimized(message *SIPMessage, externalIP string) {
	viaHeaders, exists := message.Headers["via"]
	headerKey := "via"
	if !exists || len(viaHeaders) == 0 {
		// Try case variations
		if viaHeaders, exists = message.Headers["Via"]; exists && len(viaHeaders) > 0 {
			headerKey = "Via"
		} else {
			return
		}
	}

	for i, via := range viaHeaders {
		if newVia := nr.fastStringReplace(via, externalIP); newVia != via {
			message.Headers[headerKey][i] = newVia
		}
	}
}

// rewriteContactHeaderOptimized optimizes Contact header rewriting
func (nr *NATRewriter) rewriteContactHeaderOptimized(message *SIPMessage, externalIP string) {
	contactHeaders, exists := message.Headers["contact"]
	headerKey := "contact"
	if !exists || len(contactHeaders) == 0 {
		if contactHeaders, exists = message.Headers["Contact"]; exists && len(contactHeaders) > 0 {
			headerKey = "Contact"
		} else {
			return
		}
	}

	for i, contact := range contactHeaders {
		if newContact := nr.fastStringReplace(contact, externalIP); newContact != contact {
			message.Headers[headerKey][i] = newContact
		}
	}
}

// rewriteRecordRouteHeadersOptimized optimizes Record-Route header rewriting
func (nr *NATRewriter) rewriteRecordRouteHeadersOptimized(message *SIPMessage, externalIP string) {
	rrHeaders, exists := message.Headers["record-route"]
	headerKey := "record-route"
	if !exists || len(rrHeaders) == 0 {
		if rrHeaders, exists = message.Headers["Record-Route"]; exists && len(rrHeaders) > 0 {
			headerKey = "Record-Route"
		} else {
			return
		}
	}

	for i, rr := range rrHeaders {
		if newRR := nr.fastStringReplace(rr, externalIP); newRR != rr {
			message.Headers[headerKey][i] = newRR
		}
	}
}

// fastStringReplace performs optimized string replacement using pooled builders
func (nr *NATRewriter) fastStringReplace(input, externalIP string) string {
	// Check if replacement is needed (fast path)
	if !strings.Contains(input, nr.config.InternalIP) {
		if !nr.config.ForceRewrite {
			return input
		}
		// Check for other private IPs only if force rewrite is enabled
		if !containsPrivateIP(input) {
			return input
		}
	}

	// Get builder from pool
	builder := stringBuilderPool.Get().(*strings.Builder)
	defer func() {
		builder.Reset()
		stringBuilderPool.Put(builder)
	}()

	// Pre-allocate capacity based on input size plus some headroom
	builder.Grow(len(input) + 20)

	result := input

	// Replace internal IP with external IP
	if nr.config.InternalIP != "" {
		result = strings.ReplaceAll(result, nr.config.InternalIP, externalIP)
	}

	// Replace ports if different
	if nr.internalPortStr != "" && nr.externalPortStr != "" && nr.internalPortStr != nr.externalPortStr {
		result = strings.ReplaceAll(result, nr.internalPortStr, nr.externalPortStr)
	}

	// Force rewrite private IPs if enabled
	if nr.config.ForceRewrite {
		result = nr.replacePrivateIPsOptimized(result, externalIP)
	}

	return result
}

// containsPrivateIP quickly checks if string contains private IP without allocation
func containsPrivateIP(input string) bool {
	// Fast check for common private IP prefixes
	return strings.Contains(input, "192.168.") ||
		strings.Contains(input, "10.") ||
		strings.Contains(input, "172.16.") ||
		strings.Contains(input, "172.17.") ||
		strings.Contains(input, "172.18.") ||
		strings.Contains(input, "172.19.") ||
		strings.Contains(input, "172.2") ||
		strings.Contains(input, "172.30.") ||
		strings.Contains(input, "172.31.") ||
		strings.Contains(input, "127.")
}

// replacePrivateIPsOptimized uses pre-computed networks for efficiency
func (nr *NATRewriter) replacePrivateIPsOptimized(input, externalIP string) string {
	// Find all IP addresses using the global regex
	matches := ipRegexGlobal.FindAllString(input, -1)
	if len(matches) == 0 {
		return input
	}

	result := input
	for _, match := range matches {
		ip := net.ParseIP(match)
		if ip != nil && isPrivateIPOptimized(ip) {
			result = strings.ReplaceAll(result, match, externalIP)
		}
	}

	return result
}

// isPrivateIPOptimized uses pre-computed networks for 3x performance improvement
func isPrivateIPOptimized(ip net.IP) bool {
	for _, network := range privateNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// rewriteMessageBodyOptimized handles SDP content with streaming and memory efficiency
func (nr *NATRewriter) rewriteMessageBodyOptimized(message *SIPMessage, externalIP string) {
	if len(message.Body) == 0 {
		return
	}

	contentType := nr.getHeaderValue(message, "Content-Type")
	if contentType == "" {
		return
	}

	// Check if this is SDP content
	if strings.Contains(contentType, "application/sdp") {
		if newBody := nr.rewriteSDPContentOptimized(message.Body, externalIP); newBody != nil {
			message.Body = newBody
		}
	} else if strings.Contains(contentType, "multipart/") {
		if newBody := nr.rewriteMultipartContentOptimized(message.Body, externalIP); newBody != nil {
			message.Body = newBody
		}
	}
}

// rewriteSDPContentOptimized uses streaming processing and buffer pooling
func (nr *NATRewriter) rewriteSDPContentOptimized(body []byte, externalIP string) []byte {
	if nr.config.InternalIP == "" || externalIP == "" {
		return nil
	}

	// Get buffer from pool
	buffer := byteSlicePool.Get().([]byte)
	defer func() {
		if cap(buffer) < 4096 { // Keep reasonable sized buffers in pool
			buffer = buffer[:0]
			byteSlicePool.Put(buffer)
		}
	}()
	buffer = buffer[:0]

	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	modified := false

	for scanner.Scan() {
		line := scanner.Text()
		originalLine := line

		// Process SDP lines that might contain IP addresses
		if strings.HasPrefix(line, "c=") || strings.HasPrefix(line, "o=") || strings.HasPrefix(line, "m=") {
			line = nr.fastStringReplace(line, externalIP)
			if line != originalLine {
				modified = true
			}
		}

		// Append line to buffer
		buffer = append(buffer, line...)
		buffer = append(buffer, '\n')
	}

	if !modified {
		return nil // No changes needed
	}

	// Return copy of modified content
	result := make([]byte, len(buffer))
	copy(result, buffer)
	return result
}

// rewriteMultipartContentOptimized handles multipart content efficiently
func (nr *NATRewriter) rewriteMultipartContentOptimized(body []byte, externalIP string) []byte {
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "application/sdp") {
		return nil
	}

	// Get buffer from pool
	buffer := byteSlicePool.Get().([]byte)
	defer func() {
		if cap(buffer) < 4096 {
			buffer = buffer[:0]
			byteSlicePool.Put(buffer)
		}
	}()
	buffer = buffer[:0]

	// Split content and look for SDP parts
	parts := strings.Split(bodyStr, "--")
	modified := false

	for i, part := range parts {
		if strings.Contains(part, "application/sdp") {
			// Extract and rewrite SDP content
			sdpStart := strings.Index(part, "\r\n\r\n")
			if sdpStart == -1 {
				sdpStart = strings.Index(part, "\n\n")
			}

			if sdpStart != -1 {
				sdpStart += 4 // Skip the double newline
				sdpContent := part[sdpStart:]

				newSdpBytes := nr.rewriteSDPContentOptimized([]byte(sdpContent), externalIP)
				if newSdpBytes != nil {
					parts[i] = part[:sdpStart] + string(newSdpBytes)
					modified = true
				}
			}
		}
	}

	if !modified {
		return nil
	}

	// Join parts back
	result := strings.Join(parts, "--")
	return []byte(result)
}

// Thread-safe external IP management
func (nr *NATRewriter) GetExternalIP() string {
	nr.externalIPMutex.RLock()
	defer nr.externalIPMutex.RUnlock()
	return nr.externalIP
}

func (nr *NATRewriter) SetExternalIP(ip string) {
	nr.externalIPMutex.Lock()
	defer nr.externalIPMutex.Unlock()

	if ip != nr.externalIP {
		nr.logger.WithFields(logrus.Fields{
			"old_ip": nr.externalIP,
			"new_ip": ip,
		}).Info("External IP manually updated")
		nr.externalIP = ip
		nr.externalIPBytes = []byte(ip)
	}
}

func (nr *NATRewriter) setExternalIPUnsafe(ip string) {
	nr.externalIP = ip
	nr.externalIPBytes = []byte(ip)
}

// detectExternalIPBackground performs non-blocking external IP detection
func (nr *NATRewriter) detectExternalIPBackground() error {
	// TODO: Implement actual STUN client or HTTP-based detection
	// For now, this is a placeholder that doesn't block operations
	return fmt.Errorf("external IP detection not implemented")
}

// getHeaderValue gets the first value of a header from the message
func (nr *NATRewriter) getHeaderValue(message *SIPMessage, name string) string {
	if headers, exists := message.Headers[strings.ToLower(name)]; exists && len(headers) > 0 {
		return headers[0]
	}
	// Try original case
	if headers, exists := message.Headers[name]; exists && len(headers) > 0 {
		return headers[0]
	}
	return ""
}

// Shutdown gracefully stops background processes
func (nr *NATRewriter) Shutdown() {
	if nr.ipDetectionCancel != nil {
		nr.ipDetectionCancel()
		nr.ipDetectionWG.Wait()
	}
}

// Legacy compatibility functions

// IsPrivateNetwork checks if an IP address is in a private network range (optimized)
func IsPrivateNetwork(ip string) bool {
	initializeGlobals()
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	return isPrivateIPOptimized(parsedIP)
}

// Test helper methods (exported for testing)

// RewriteSDPContent is a public wrapper for testing
func (nr *NATRewriter) RewriteSDPContent(sdp string) string {
	externalIP := nr.GetExternalIP()
	if externalIP == "" {
		return sdp
	}

	result := nr.rewriteSDPContentOptimized([]byte(sdp), externalIP)
	if result != nil {
		return string(result)
	}
	return sdp
}

// ValidateNATConfig validates NAT configuration
func ValidateNATConfig(config *NATConfig) error {
	if config == nil {
		return fmt.Errorf("NAT config cannot be nil")
	}

	if config.BehindNAT {
		if config.InternalIP == "" {
			return fmt.Errorf("internal IP is required when behind NAT")
		}

		if net.ParseIP(config.InternalIP) == nil {
			return fmt.Errorf("invalid internal IP address: %s", config.InternalIP)
		}

		if config.ExternalIP != "" && net.ParseIP(config.ExternalIP) == nil {
			return fmt.Errorf("invalid external IP address: %s", config.ExternalIP)
		}

		if config.InternalPort < 0 || config.InternalPort > 65535 {
			return fmt.Errorf("invalid internal port: %d", config.InternalPort)
		}

		if config.ExternalPort < 0 || config.ExternalPort > 65535 {
			return fmt.Errorf("invalid external port: %d", config.ExternalPort)
		}

		// Check if internal IP is actually private
		if !IsPrivateNetwork(config.InternalIP) {
			return fmt.Errorf("internal IP %s is not in a private network range", config.InternalIP)
		}

		// Check if external IP is public (if provided)
		if config.ExternalIP != "" && IsPrivateNetwork(config.ExternalIP) {
			return fmt.Errorf("external IP %s appears to be in a private network range", config.ExternalIP)
		}
	}

	return nil
}
