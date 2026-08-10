package auth

import (
	"crypto/md5" // #nosec G401 G501 -- required by RFC 2617 SIP digest authentication
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// SIPAuthenticator handles SIP digest authentication
type SIPAuthenticator struct {
	users        map[string]*SIPUser
	nonces       map[string]*NonceInfo
	mutex        sync.RWMutex
	logger       *logrus.Logger
	realm        string
	nonceTimeout time.Duration
	randomReader io.Reader
}

// SIPUser represents a SIP user with credentials
type SIPUser struct {
	Username string
	Password string
	Realm    string
	Enabled  bool
}

// NonceInfo stores nonce validation information
type NonceInfo struct {
	Value     string
	Timestamp time.Time
	Count     uint32
	ClientIP  string
}

// AuthResult represents authentication result
type AuthResult struct {
	Success   bool
	Username  string
	Reason    string
	Challenge string
}

// DigestCredentials represents parsed digest credentials
type DigestCredentials struct {
	Username  string
	Realm     string
	Nonce     string
	URI       string
	Response  string
	Algorithm string
	Opaque    string
	QOP       string
	NC        string
	CNonce    string
}

// NewSIPAuthenticator creates a new SIP authenticator
func NewSIPAuthenticator(realm string, logger *logrus.Logger) *SIPAuthenticator {
	auth := &SIPAuthenticator{
		users:        make(map[string]*SIPUser),
		nonces:       make(map[string]*NonceInfo),
		logger:       logger,
		realm:        realm,
		nonceTimeout: 300 * time.Second, // 5 minutes default
		randomReader: rand.Reader,
	}

	// Start nonce cleanup routine
	go auth.cleanupNonces()

	logger.WithField("realm", realm).Info("SIP authenticator initialized")
	return auth
}

// AddUser adds a SIP user for authentication
func (s *SIPAuthenticator) AddUser(username, password string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.users[username] = &SIPUser{
		Username: username,
		Password: password,
		Realm:    s.realm,
		Enabled:  true,
	}

	s.logger.WithFields(logrus.Fields{
		"username": username,
		"realm":    s.realm,
	}).Info("SIP user added")
}

// RemoveUser removes a SIP user
func (s *SIPAuthenticator) RemoveUser(username string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	delete(s.users, username)
	s.logger.WithField("username", username).Info("SIP user removed")
}

// DisableUser disables a SIP user without removing
func (s *SIPAuthenticator) DisableUser(username string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if user, exists := s.users[username]; exists {
		user.Enabled = false
		s.logger.WithField("username", username).Info("SIP user disabled")
	}
}

// EnableUser enables a previously disabled SIP user
func (s *SIPAuthenticator) EnableUser(username string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if user, exists := s.users[username]; exists {
		user.Enabled = true
		s.logger.WithField("username", username).Info("SIP user enabled")
	}
}

// Authenticate performs SIP digest authentication
func (s *SIPAuthenticator) Authenticate(authHeader, method, uri, clientIP string) *AuthResult {
	if authHeader == "" {
		// No authentication provided, return challenge
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Reason:    "No authentication provided",
			Challenge: challenge,
		}
	}

	// Parse digest credentials
	creds, err := s.parseDigestAuth(authHeader, uri)
	if err != nil {
		s.logger.WithError(err).WithField("client_ip", clientIP).Warning("Failed to parse digest authentication")
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Reason:    "Invalid authentication format",
			Challenge: challenge,
		}
	}

	// Validate user
	s.mutex.RLock()
	storedUser, exists := s.users[creds.Username]
	var user SIPUser
	if exists && storedUser != nil {
		user = *storedUser
	}
	s.mutex.RUnlock()

	if !exists || !user.Enabled {
		s.logger.WithFields(logrus.Fields{
			"username":  creds.Username,
			"client_ip": clientIP,
		}).Warning("Authentication failed: user not found or disabled")
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Username:  creds.Username,
			Reason:    "User not found or disabled",
			Challenge: challenge,
		}
	}

	// Inspect the nonce without changing replay state. The counter is committed
	// only after the password response has been verified below.
	_, expectedCount, nextCount, err := s.inspectNonce(creds.Nonce, clientIP, creds.NC)
	if err != nil {
		s.logger.WithFields(logrus.Fields{
			"username":  creds.Username,
			"client_ip": clientIP,
			"nonce":     creds.Nonce,
		}).Warning("Authentication failed: invalid or expired nonce")
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Username:  creds.Username,
			Reason:    "Invalid or expired nonce",
			Challenge: challenge,
		}
	}

	// Calculate expected response
	expectedResponse := s.calculateResponse(user.Password, method, uri, creds)

	// Verify response using constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(creds.Response), []byte(expectedResponse)) != 1 {
		s.logger.WithFields(logrus.Fields{
			"username":  creds.Username,
			"client_ip": clientIP,
		}).Warning("Authentication failed: invalid credentials")
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Username:  creds.Username,
			Reason:    "Invalid credentials",
			Challenge: challenge,
		}
	}

	if !s.commitNonceCount(creds.Nonce, expectedCount, nextCount) {
		s.logger.WithFields(logrus.Fields{
			"username":  creds.Username,
			"client_ip": clientIP,
			"nonce":     creds.Nonce,
		}).Warning("Authentication failed: nonce was replayed concurrently")
		challenge := s.generateChallenge(clientIP)
		return &AuthResult{
			Success:   false,
			Username:  creds.Username,
			Reason:    "Nonce replay detected",
			Challenge: challenge,
		}
	}

	// Authentication successful
	s.logger.WithFields(logrus.Fields{
		"username":  creds.Username,
		"client_ip": clientIP,
		"method":    method,
		"uri":       uri,
	}).Info("SIP authentication successful")

	return &AuthResult{
		Success:  true,
		Username: creds.Username,
		Reason:   "Authentication successful",
	}
}

// generateChallenge creates a new authentication challenge.
// Returns empty string if nonce generation fails (fail closed).
func (s *SIPAuthenticator) generateChallenge(clientIP string) string {
	nonce, err := s.generateNonce(clientIP)
	if err != nil {
		s.logger.WithError(err).Error("Failed to generate auth challenge — refusing to issue weak nonce")
		return ""
	}

	challenge := fmt.Sprintf(`Digest realm="%s", nonce="%s", algorithm=MD5, qop="auth"`,
		s.realm, nonce)

	return challenge
}

// generateNonce creates a new nonce value using cryptographically strong randomness.
// Returns an error if crypto/rand fails — callers must refuse to issue a challenge.
func (s *SIPAuthenticator) generateNonce(clientIP string) (string, error) {
	randomBytes := make([]byte, 32)
	randomReader := s.randomReader
	if randomReader == nil {
		randomReader = rand.Reader
	}
	if _, err := io.ReadFull(randomReader, randomBytes); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	hash := sha256.Sum256(randomBytes)
	nonce := hex.EncodeToString(hash[:])

	// Store nonce info
	s.mutex.Lock()
	s.nonces[nonce] = &NonceInfo{
		Value:     nonce,
		Timestamp: time.Now(),
		Count:     0,
		ClientIP:  clientIP,
	}
	s.mutex.Unlock()

	return nonce, nil
}

// inspectNonce validates a nonce and returns its current count and the count
// that the caller may commit after digest verification succeeds.
func (s *SIPAuthenticator) inspectNonce(nonce, clientIP, nc string) (*NonceInfo, uint32, uint32, error) {
	ncVal, err := parseNonceCount(nc)
	if err != nil {
		return nil, 0, 0, err
	}

	s.mutex.RLock()
	nonceInfo, exists := s.nonces[nonce]
	if !exists {
		s.mutex.RUnlock()
		return nil, 0, 0, fmt.Errorf("unknown nonce")
	}
	info := *nonceInfo
	s.mutex.RUnlock()

	if time.Since(info.Timestamp) > s.nonceTimeout {
		s.mutex.Lock()
		if current, ok := s.nonces[nonce]; ok && time.Since(current.Timestamp) > s.nonceTimeout {
			delete(s.nonces, nonce)
		}
		s.mutex.Unlock()
		return nil, 0, 0, fmt.Errorf("expired nonce")
	}
	if info.ClientIP != clientIP {
		return nil, 0, 0, fmt.Errorf("nonce client IP mismatch")
	}
	if ncVal <= info.Count {
		return nil, 0, 0, fmt.Errorf("nonce count is not increasing")
	}

	return &info, info.Count, ncVal, nil
}

// commitNonceCount atomically applies a previously inspected nonce count.
// Compare-and-update prevents two valid requests from consuming the same
// nonce count while keeping failed digest responses side-effect free.
func (s *SIPAuthenticator) commitNonceCount(nonce string, expected, next uint32) bool {
	if next <= expected {
		return false
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	nonceInfo, exists := s.nonces[nonce]
	if !exists || nonceInfo.Count != expected || time.Since(nonceInfo.Timestamp) > s.nonceTimeout {
		if exists && time.Since(nonceInfo.Timestamp) > s.nonceTimeout {
			delete(s.nonces, nonce)
		}
		return false
	}
	nonceInfo.Count = next
	return true
}

// validateNonce is retained for callers that only need the legacy inspection
// and commit operation. Authentication uses inspectNonce and commitNonceCount
// separately so invalid responses cannot advance replay state.
func (s *SIPAuthenticator) validateNonce(nonce, clientIP, nc string) bool {
	_, expected, next, err := s.inspectNonce(nonce, clientIP, nc)
	return err == nil && s.commitNonceCount(nonce, expected, next)
}

func parseNonceCount(nc string) (uint32, error) {
	if len(nc) != 8 {
		return 0, fmt.Errorf("nonce count must contain exactly eight hexadecimal digits")
	}
	for _, char := range nc {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return 0, fmt.Errorf("nonce count contains a non-hexadecimal digit")
		}
	}
	value, err := strconv.ParseUint(nc, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid nonce count: %w", err)
	}
	// Keep the historical protocol bound even though the stored counter is
	// uint32. This also preserves interoperability with 32-bit deployments.
	if value > 1<<31-1 {
		return 0, fmt.Errorf("nonce count exceeds supported maximum")
	}
	return uint32(value), nil
}

// parseDigestAuth parses digest authentication header
func (s *SIPAuthenticator) parseDigestAuth(authHeader, requestURI string) (*DigestCredentials, error) {
	// Remove "Digest " prefix
	if !strings.HasPrefix(authHeader, "Digest ") {
		return nil, fmt.Errorf("not a digest authentication header")
	}

	digestData := strings.TrimPrefix(authHeader, "Digest ")

	creds := &DigestCredentials{}

	// Parse key-value pairs
	pairs := strings.Split(digestData, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"`)

		switch key {
		case "username":
			creds.Username = value
		case "realm":
			creds.Realm = value
		case "nonce":
			creds.Nonce = value
		case "uri":
			creds.URI = value
		case "response":
			creds.Response = value
		case "algorithm":
			creds.Algorithm = value
		case "opaque":
			creds.Opaque = value
		case "qop":
			creds.QOP = value
		case "nc":
			creds.NC = value
		case "cnonce":
			creds.CNonce = value
		}
	}

	// Validate required fields
	if creds.Username == "" || creds.Realm == "" || creds.Nonce == "" ||
		creds.URI == "" || creds.Response == "" {
		return nil, fmt.Errorf("missing required digest authentication fields")
	}
	if creds.Realm != s.realm {
		return nil, fmt.Errorf("digest realm does not match configured realm")
	}
	if creds.URI != requestURI {
		return nil, fmt.Errorf("digest URI does not match authenticated request URI")
	}
	if !strings.EqualFold(creds.Algorithm, "MD5") {
		return nil, fmt.Errorf("unsupported digest algorithm")
	}
	if creds.QOP != "auth" {
		return nil, fmt.Errorf("unsupported or missing digest qop")
	}
	if creds.CNonce == "" {
		return nil, fmt.Errorf("missing digest cnonce")
	}
	if _, err := parseNonceCount(creds.NC); err != nil {
		return nil, err
	}

	return creds, nil
}

// calculateResponse calculates the expected digest response
func (s *SIPAuthenticator) calculateResponse(password, method, uri string, creds *DigestCredentials) string {
	// HA1 = MD5(username:realm:password)
	ha1Data := fmt.Sprintf("%s:%s:%s", creds.Username, creds.Realm, password)
	// #nosec G401 -- SIP digest authentication requires MD5 for RFC 2617 compatibility.
	ha1Hash := md5.Sum([]byte(ha1Data))
	ha1 := fmt.Sprintf("%x", ha1Hash)

	// HA2 = MD5(method:digestURI)
	ha2Data := fmt.Sprintf("%s:%s", method, uri)
	// #nosec G401 -- SIP digest authentication requires MD5 for RFC 2617 compatibility.
	ha2Hash := md5.Sum([]byte(ha2Data))
	ha2 := fmt.Sprintf("%x", ha2Hash)

	// Response calculation depends on qop
	var responseData string
	if creds.QOP == "auth" || creds.QOP == "auth-int" {
		// Response = MD5(HA1:nonce:nc:cnonce:qop:HA2)
		responseData = fmt.Sprintf("%s:%s:%s:%s:%s:%s",
			ha1, creds.Nonce, creds.NC, creds.CNonce, creds.QOP, ha2)
	} else {
		// Response = MD5(HA1:nonce:HA2)
		responseData = fmt.Sprintf("%s:%s:%s", ha1, creds.Nonce, ha2)
	}

	// #nosec G401 -- SIP digest authentication requires MD5 for RFC 2617 compatibility.
	responseHash := md5.Sum([]byte(responseData))
	return fmt.Sprintf("%x", responseHash)
}

// cleanupNonces removes expired nonces periodically
func (s *SIPAuthenticator) cleanupNonces() {
	ticker := time.NewTicker(60 * time.Second) // Cleanup every minute
	defer ticker.Stop()

	for range ticker.C {
		s.mutex.Lock()
		now := time.Now()
		for nonce, info := range s.nonces {
			if now.Sub(info.Timestamp) > s.nonceTimeout {
				delete(s.nonces, nonce)
			}
		}
		s.mutex.Unlock()
	}
}

// IP-based access control

// IPAccessController manages IP-based access control
type IPAccessController struct {
	allowedNetworks []*net.IPNet
	blockedNetworks []*net.IPNet
	allowedIPs      map[string]bool
	blockedIPs      map[string]bool
	mutex           sync.RWMutex
	logger          *logrus.Logger
	defaultAllow    bool
}

// NewIPAccessController creates a new IP access controller
func NewIPAccessController(logger *logrus.Logger, defaultAllow bool) *IPAccessController {
	return &IPAccessController{
		allowedIPs:   make(map[string]bool),
		blockedIPs:   make(map[string]bool),
		logger:       logger,
		defaultAllow: defaultAllow,
	}
}

// AddAllowedIP adds an IP address to the allow list
func (ip *IPAccessController) AddAllowedIP(ipAddr string) error {
	ip.mutex.Lock()
	defer ip.mutex.Unlock()

	if net.ParseIP(ipAddr) == nil {
		return fmt.Errorf("invalid IP address: %s", ipAddr)
	}

	ip.allowedIPs[ipAddr] = true
	ip.logger.WithField("ip", ipAddr).Info("IP added to allow list")
	return nil
}

// AddBlockedIP adds an IP address to the block list
func (ip *IPAccessController) AddBlockedIP(ipAddr string) error {
	ip.mutex.Lock()
	defer ip.mutex.Unlock()

	if net.ParseIP(ipAddr) == nil {
		return fmt.Errorf("invalid IP address: %s", ipAddr)
	}

	ip.blockedIPs[ipAddr] = true
	ip.logger.WithField("ip", ipAddr).Info("IP added to block list")
	return nil
}

// AddAllowedNetwork adds a network to the allow list
func (ip *IPAccessController) AddAllowedNetwork(cidr string) error {
	ip.mutex.Lock()
	defer ip.mutex.Unlock()

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %s", cidr)
	}

	ip.allowedNetworks = append(ip.allowedNetworks, network)
	ip.logger.WithField("network", cidr).Info("Network added to allow list")
	return nil
}

// AddBlockedNetwork adds a network to the block list
func (ip *IPAccessController) AddBlockedNetwork(cidr string) error {
	ip.mutex.Lock()
	defer ip.mutex.Unlock()

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR: %s", cidr)
	}

	ip.blockedNetworks = append(ip.blockedNetworks, network)
	ip.logger.WithField("network", cidr).Info("Network added to block list")
	return nil
}

// IsAllowed checks if an IP address is allowed
func (ip *IPAccessController) IsAllowed(ipAddr string) bool {
	ip.mutex.RLock()
	defer ip.mutex.RUnlock()

	parsedIP := net.ParseIP(ipAddr)
	if parsedIP == nil {
		ip.logger.WithField("ip", ipAddr).Warning("Invalid IP address")
		return false
	}

	// Check if IP is explicitly blocked
	if ip.blockedIPs[ipAddr] {
		ip.logger.WithField("ip", ipAddr).Info("IP explicitly blocked")
		return false
	}

	// Check if IP is in blocked networks
	for _, network := range ip.blockedNetworks {
		if network.Contains(parsedIP) {
			ip.logger.WithFields(logrus.Fields{
				"ip":      ipAddr,
				"network": network.String(),
			}).Info("IP blocked by network rule")
			return false
		}
	}

	// Check if IP is explicitly allowed
	if ip.allowedIPs[ipAddr] {
		ip.logger.WithField("ip", ipAddr).Debug("IP explicitly allowed")
		return true
	}

	// Check if IP is in allowed networks
	for _, network := range ip.allowedNetworks {
		if network.Contains(parsedIP) {
			ip.logger.WithFields(logrus.Fields{
				"ip":      ipAddr,
				"network": network.String(),
			}).Debug("IP allowed by network rule")
			return true
		}
	}

	// Return default policy
	if !ip.defaultAllow {
		ip.logger.WithField("ip", ipAddr).Info("IP blocked by default policy")
	}

	return ip.defaultAllow
}

// GetStats returns access control statistics
func (ip *IPAccessController) GetStats() *IPAccessStats {
	ip.mutex.RLock()
	defer ip.mutex.RUnlock()

	return &IPAccessStats{
		AllowedIPs:      len(ip.allowedIPs),
		BlockedIPs:      len(ip.blockedIPs),
		AllowedNetworks: len(ip.allowedNetworks),
		BlockedNetworks: len(ip.blockedNetworks),
		DefaultAllow:    ip.defaultAllow,
	}
}

// Types

type IPAccessStats struct {
	AllowedIPs      int  `json:"allowed_ips"`
	BlockedIPs      int  `json:"blocked_ips"`
	AllowedNetworks int  `json:"allowed_networks"`
	BlockedNetworks int  `json:"blocked_networks"`
	DefaultAllow    bool `json:"default_allow"`
}
