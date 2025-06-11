package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"siprec-server/pkg/database"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// JWTAuthenticator handles JWT-based API authentication
type JWTAuthenticator struct {
	secretKey         []byte
	issuer            string
	tokenExpiry       time.Duration
	refreshExpiry     time.Duration
	logger            *logrus.Logger
	repo              *database.Repository
	blacklistedTokens map[string]time.Time
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	SecretKey     string
	Issuer        string
	TokenExpiry   time.Duration
	RefreshExpiry time.Duration
}

// Claims represents JWT claims
type Claims struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	jwt.RegisteredClaims
}

// TokenPair represents access and refresh tokens
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	TokenType    string `json:"token_type"`
}

// AuthenticationResult represents authentication result
type AuthenticationResult struct {
	Success     bool           `json:"success"`
	User        *database.User `json:"user,omitempty"`
	Tokens      *TokenPair     `json:"tokens,omitempty"`
	Error       string         `json:"error,omitempty"`
	Permissions []string       `json:"permissions,omitempty"`
}

// NewJWTAuthenticator creates a new JWT authenticator
func NewJWTAuthenticator(config JWTConfig, repo *database.Repository, logger *logrus.Logger) *JWTAuthenticator {
	var secretKey []byte
	if config.SecretKey != "" {
		secretKey = []byte(config.SecretKey)
	} else {
		// Generate a random secret key if not provided
		secretKey = make([]byte, 32)
		rand.Read(secretKey)
		logger.Warning("No JWT secret key provided, using generated key (will not persist across restarts)")
	}

	auth := &JWTAuthenticator{
		secretKey:         secretKey,
		issuer:            config.Issuer,
		tokenExpiry:       config.TokenExpiry,
		refreshExpiry:     config.RefreshExpiry,
		logger:            logger,
		repo:              repo,
		blacklistedTokens: make(map[string]time.Time),
	}

	// Start cleanup routine for blacklisted tokens
	go auth.cleanupBlacklistedTokens()

	logger.WithFields(logrus.Fields{
		"issuer":         config.Issuer,
		"token_expiry":   config.TokenExpiry,
		"refresh_expiry": config.RefreshExpiry,
	}).Info("JWT authenticator initialized")

	return auth
}

// Login authenticates a user and returns JWT tokens
func (j *JWTAuthenticator) Login(username, password string) (*AuthenticationResult, error) {
	// Get user from database
	user, err := j.repo.GetUserByUsername(username)
	if err != nil {
		j.logger.WithFields(logrus.Fields{
			"username": username,
			"error":    err.Error(),
		}).Warning("Login failed: user not found")

		return &AuthenticationResult{
			Success: false,
			Error:   "Invalid username or password",
		}, nil
	}

	// Check if user is active
	if !user.IsActive {
		j.logger.WithField("username", username).Warning("Login failed: user inactive")
		return &AuthenticationResult{
			Success: false,
			Error:   "Account is disabled",
		}, nil
	}

	// Verify password
	if !j.verifyPassword(password, user.PasswordHash) {
		j.logger.WithField("username", username).Warning("Login failed: invalid password")
		return &AuthenticationResult{
			Success: false,
			Error:   "Invalid username or password",
		}, nil
	}

	// Generate tokens
	tokens, err := j.generateTokenPair(user)
	if err != nil {
		j.logger.WithError(err).WithField("username", username).Error("Failed to generate tokens")
		return &AuthenticationResult{
			Success: false,
			Error:   "Failed to generate authentication tokens",
		}, nil
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if err := j.repo.UpdateUser(user); err != nil {
		j.logger.WithError(err).WithField("username", username).Warning("Failed to update last login")
	}

	// Get user permissions
	permissions := j.getRolePermissions(user.Role)

	j.logger.WithFields(logrus.Fields{
		"username": username,
		"role":     user.Role,
		"user_id":  user.ID,
	}).Info("User login successful")

	return &AuthenticationResult{
		Success:     true,
		User:        user,
		Tokens:      tokens,
		Permissions: permissions,
	}, nil
}

// RefreshToken generates new tokens using a refresh token
func (j *JWTAuthenticator) RefreshToken(refreshToken string) (*AuthenticationResult, error) {
	// Parse and validate refresh token
	claims, err := j.validateToken(refreshToken)
	if err != nil {
		j.logger.WithError(err).Warning("Refresh token validation failed")
		return &AuthenticationResult{
			Success: false,
			Error:   "Invalid refresh token",
		}, nil
	}

	// Check if token is blacklisted
	if j.isTokenBlacklisted(refreshToken) {
		j.logger.WithField("user_id", claims.UserID).Warning("Attempted to use blacklisted refresh token")
		return &AuthenticationResult{
			Success: false,
			Error:   "Token has been revoked",
		}, nil
	}

	// Get user from database
	user, err := j.repo.GetUser(claims.UserID)
	if err != nil {
		j.logger.WithError(err).WithField("user_id", claims.UserID).Error("Failed to get user for token refresh")
		return &AuthenticationResult{
			Success: false,
			Error:   "User not found",
		}, nil
	}

	// Check if user is still active
	if !user.IsActive {
		j.logger.WithField("user_id", claims.UserID).Warning("Token refresh failed: user inactive")
		return &AuthenticationResult{
			Success: false,
			Error:   "Account is disabled",
		}, nil
	}

	// Blacklist the old refresh token
	j.blacklistToken(refreshToken)

	// Generate new tokens
	tokens, err := j.generateTokenPair(user)
	if err != nil {
		j.logger.WithError(err).WithField("user_id", claims.UserID).Error("Failed to generate new tokens")
		return &AuthenticationResult{
			Success: false,
			Error:   "Failed to generate new tokens",
		}, nil
	}

	permissions := j.getRolePermissions(user.Role)

	j.logger.WithFields(logrus.Fields{
		"username": user.Username,
		"user_id":  user.ID,
	}).Info("Token refresh successful")

	return &AuthenticationResult{
		Success:     true,
		User:        user,
		Tokens:      tokens,
		Permissions: permissions,
	}, nil
}

// ValidateToken validates a JWT token and returns claims
func (j *JWTAuthenticator) ValidateToken(tokenString string) (*Claims, error) {
	// Remove Bearer prefix if present
	if strings.HasPrefix(tokenString, "Bearer ") {
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	}

	return j.validateToken(tokenString)
}

// Logout invalidates tokens by adding them to blacklist
func (j *JWTAuthenticator) Logout(accessToken, refreshToken string) error {
	// Remove Bearer prefix if present
	if strings.HasPrefix(accessToken, "Bearer ") {
		accessToken = strings.TrimPrefix(accessToken, "Bearer ")
	}

	// Validate tokens before blacklisting
	claims, err := j.validateToken(accessToken)
	if err != nil {
		return fmt.Errorf("invalid access token: %w", err)
	}

	// Blacklist both tokens
	j.blacklistToken(accessToken)
	if refreshToken != "" {
		j.blacklistToken(refreshToken)
	}

	j.logger.WithFields(logrus.Fields{
		"username": claims.Username,
		"user_id":  claims.UserID,
	}).Info("User logout successful")

	return nil
}

// generateTokenPair creates access and refresh tokens for a user
func (j *JWTAuthenticator) generateTokenPair(user *database.User) (*TokenPair, error) {
	now := time.Now()

	// Access token claims
	accessClaims := &Claims{
		UserID:      user.ID,
		Username:    user.Username,
		Role:        user.Role,
		Permissions: j.getRolePermissions(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   user.ID,
			Audience:  []string{"siprec-api"},
			ExpiresAt: jwt.NewNumericDate(now.Add(j.tokenExpiry)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        j.generateJTI(),
		},
	}

	// Create access token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(j.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token claims (longer expiry, fewer claims)
	refreshClaims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   user.ID,
			Audience:  []string{"siprec-refresh"},
			ExpiresAt: jwt.NewNumericDate(now.Add(j.refreshExpiry)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        j.generateJTI(),
		},
	}

	// Create refresh token
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(j.secretKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    accessClaims.ExpiresAt.Unix(),
		TokenType:    "Bearer",
	}, nil
}

// validateToken validates a JWT token and returns claims
func (j *JWTAuthenticator) validateToken(tokenString string) (*Claims, error) {
	// Check if token is blacklisted
	if j.isTokenBlacklisted(tokenString) {
		return nil, fmt.Errorf("token has been revoked")
	}

	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Validate token
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Extract claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Additional validation
	if claims.Issuer != j.issuer {
		return nil, fmt.Errorf("invalid issuer")
	}

	return claims, nil
}

// verifyPassword verifies a password against its hash
func (j *JWTAuthenticator) verifyPassword(password, hash string) bool {
	// This is a simplified implementation
	// In production, use bcrypt or similar
	return password == hash // TODO: Implement proper password hashing
}

// getRolePermissions returns permissions for a role
func (j *JWTAuthenticator) getRolePermissions(role string) []string {
	switch role {
	case "admin":
		return []string{
			"sessions:read", "sessions:write", "sessions:delete",
			"cdr:read", "cdr:write", "cdr:export",
			"users:read", "users:write", "users:delete",
			"system:read", "system:write", "system:config",
			"monitoring:read", "monitoring:write",
		}
	case "operator":
		return []string{
			"sessions:read", "sessions:write",
			"cdr:read", "cdr:export",
			"monitoring:read",
		}
	case "viewer":
		return []string{
			"sessions:read",
			"cdr:read",
		}
	default:
		return []string{}
	}
}

// generateJTI generates a unique token ID
func (j *JWTAuthenticator) generateJTI() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// blacklistToken adds a token to the blacklist
func (j *JWTAuthenticator) blacklistToken(tokenString string) {
	// Parse token to get expiry time
	token, _ := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return j.secretKey, nil
	})

	var expiryTime time.Time
	if claims, ok := token.Claims.(*Claims); ok && claims.ExpiresAt != nil {
		expiryTime = claims.ExpiresAt.Time
	} else {
		// Default to refresh token expiry if we can't parse
		expiryTime = time.Now().Add(j.refreshExpiry)
	}

	// Add to blacklist with expiry time
	j.blacklistedTokens[tokenString] = expiryTime
}

// isTokenBlacklisted checks if a token is blacklisted
func (j *JWTAuthenticator) isTokenBlacklisted(tokenString string) bool {
	_, blacklisted := j.blacklistedTokens[tokenString]
	return blacklisted
}

// cleanupBlacklistedTokens removes expired tokens from blacklist
func (j *JWTAuthenticator) cleanupBlacklistedTokens() {
	ticker := time.NewTicker(1 * time.Hour) // Cleanup every hour
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		for token, expiry := range j.blacklistedTokens {
			if now.After(expiry) {
				delete(j.blacklistedTokens, token)
			}
		}
	}
}

// CheckPermission checks if a user has a specific permission
func (j *JWTAuthenticator) CheckPermission(claims *Claims, permission string) bool {
	for _, p := range claims.Permissions {
		if p == permission {
			return true
		}

		// Check wildcard permissions
		if strings.HasSuffix(p, ":*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(permission, prefix) {
				return true
			}
		}
	}

	return false
}

// GetUserInfo returns user information from token claims
func (j *JWTAuthenticator) GetUserInfo(tokenString string) (*UserInfo, error) {
	claims, err := j.ValidateToken(tokenString)
	if err != nil {
		return nil, err
	}

	return &UserInfo{
		UserID:      claims.UserID,
		Username:    claims.Username,
		Role:        claims.Role,
		Permissions: claims.Permissions,
	}, nil
}

// Types

type UserInfo struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

// API Key Authentication

// APIKeyAuthenticator handles API key authentication
type APIKeyAuthenticator struct {
	repo   *database.Repository
	logger *logrus.Logger
}

// NewAPIKeyAuthenticator creates a new API key authenticator
func NewAPIKeyAuthenticator(repo *database.Repository, logger *logrus.Logger) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{
		repo:   repo,
		logger: logger,
	}
}

// ValidateAPIKey validates an API key and returns user information
func (a *APIKeyAuthenticator) ValidateAPIKey(apiKey string) (*database.User, error) {
	// Get API key from database
	key, err := a.repo.GetAPIKeyByKey(apiKey)
	if err != nil {
		a.logger.WithError(err).Warning("API key validation failed")
		return nil, fmt.Errorf("invalid API key")
	}

	// Check if key is active
	if !key.IsActive {
		a.logger.WithField("key_id", key.ID).Warning("Inactive API key used")
		return nil, fmt.Errorf("API key is inactive")
	}

	// Check if key is expired
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		a.logger.WithField("key_id", key.ID).Warning("Expired API key used")
		return nil, fmt.Errorf("API key has expired")
	}

	// Get user
	user, err := a.repo.GetUser(key.UserID)
	if err != nil {
		a.logger.WithError(err).WithField("user_id", key.UserID).Error("Failed to get user for API key")
		return nil, fmt.Errorf("user not found")
	}

	// Check if user is active
	if !user.IsActive {
		a.logger.WithField("user_id", user.ID).Warning("API key used by inactive user")
		return nil, fmt.Errorf("user account is inactive")
	}

	// Update last used time
	now := time.Now()
	key.LastUsed = &now
	if err := a.repo.UpdateAPIKey(key); err != nil {
		a.logger.WithError(err).WithField("key_id", key.ID).Warning("Failed to update API key last used time")
	}

	a.logger.WithFields(logrus.Fields{
		"user_id":  user.ID,
		"username": user.Username,
		"key_id":   key.ID,
	}).Info("API key authentication successful")

	return user, nil
}

// GenerateAPIKey generates a new API key for a user
func (a *APIKeyAuthenticator) GenerateAPIKey(userID, name string, permissions []string, expiresAt *time.Time) (*database.APIKey, string, error) {
	// Generate random API key
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	keyString := hex.EncodeToString(keyBytes)

	// Create API key record
	apiKey := &database.APIKey{
		ID:          "", // Will be set by repository
		UserID:      userID,
		Name:        name,
		KeyHash:     keyString, // TODO: Hash the key for storage
		Permissions: permissions,
		IsActive:    true,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Store in database
	if err := a.repo.CreateAPIKey(apiKey); err != nil {
		return nil, "", fmt.Errorf("failed to create API key: %w", err)
	}

	a.logger.WithFields(logrus.Fields{
		"user_id": userID,
		"key_id":  apiKey.ID,
		"name":    name,
	}).Info("API key generated")

	return apiKey, keyString, nil
}
