package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

// UserInfo represents user information
type UserInfo struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

// Claims represents JWT claims
type Claims struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	jwt.RegisteredClaims
}

// SimpleAuthenticator provides simple in-memory authentication
type SimpleAuthenticator struct {
	users        map[string]*SimpleUser
	apiKeys      map[string]*SimpleAPIKey
	secretKey    []byte
	issuer       string
	tokenExpiry  time.Duration
	logger       *logrus.Logger
	mutex        sync.RWMutex
}

// SimpleUser represents a simple user
type SimpleUser struct {
	Username    string
	Password    string // In production, this should be hashed
	Role        string
	IsActive    bool
	Permissions []string
}

// SimpleAPIKey represents a simple API key
type SimpleAPIKey struct {
	Key         string
	UserID      string
	Username    string
	Role        string
	IsActive    bool
	Permissions []string
	CreatedAt   time.Time
}

// NewSimpleAuthenticator creates a new simple authenticator
func NewSimpleAuthenticator(secretKey, issuer string, tokenExpiry time.Duration, logger *logrus.Logger) *SimpleAuthenticator {
	var secret []byte
	if secretKey != "" {
		secret = []byte(secretKey)
	} else {
		// Generate random secret if not provided
		secret = make([]byte, 32)
		rand.Read(secret)
		logger.Warning("No JWT secret provided, using generated key")
	}

	auth := &SimpleAuthenticator{
		users:       make(map[string]*SimpleUser),
		apiKeys:     make(map[string]*SimpleAPIKey),
		secretKey:   secret,
		issuer:      issuer,
		tokenExpiry: tokenExpiry,
		logger:      logger,
	}

	// Add default admin user
	auth.AddUser("admin", "admin123", "admin")
	
	logger.Info("Simple authenticator initialized")
	return auth
}

// AddUser adds a user
func (s *SimpleAuthenticator) AddUser(username, password, role string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	permissions := s.getRolePermissions(role)
	s.users[username] = &SimpleUser{
		Username:    username,
		Password:    password,
		Role:        role,
		IsActive:    true,
		Permissions: permissions,
	}

	s.logger.WithFields(logrus.Fields{
		"username": username,
		"role":     role,
	}).Info("User added")
}

// GenerateAPIKey generates a new API key for a user
func (s *SimpleAuthenticator) GenerateAPIKey(username string) (string, error) {
	s.mutex.RLock()
	user, exists := s.users[username]
	s.mutex.RUnlock()

	if !exists {
		return "", fmt.Errorf("user not found")
	}

	// Generate random API key
	keyBytes := make([]byte, 32)
	rand.Read(keyBytes)
	apiKey := hex.EncodeToString(keyBytes)

	s.mutex.Lock()
	s.apiKeys[apiKey] = &SimpleAPIKey{
		Key:         apiKey,
		UserID:      username,
		Username:    username,
		Role:        user.Role,
		IsActive:    true,
		Permissions: user.Permissions,
		CreatedAt:   time.Now(),
	}
	s.mutex.Unlock()

	s.logger.WithFields(logrus.Fields{
		"username": username,
		"role":     user.Role,
	}).Info("API key generated")

	return apiKey, nil
}

// ValidateAPIKey validates an API key
func (s *SimpleAuthenticator) ValidateAPIKey(apiKey string) (*UserInfo, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	key, exists := s.apiKeys[apiKey]
	if !exists || !key.IsActive {
		return nil, fmt.Errorf("invalid or inactive API key")
	}

	return &UserInfo{
		UserID:      key.UserID,
		Username:    key.Username,
		Role:        key.Role,
		Permissions: key.Permissions,
	}, nil
}

// Login authenticates a user and returns a JWT token
func (s *SimpleAuthenticator) Login(username, password string) (string, error) {
	s.mutex.RLock()
	user, exists := s.users[username]
	s.mutex.RUnlock()

	if !exists || !user.IsActive {
		return "", fmt.Errorf("invalid username or password")
	}

	// Simple password check (in production, use bcrypt)
	if user.Password != password {
		return "", fmt.Errorf("invalid username or password")
	}

	// Generate JWT token
	token, err := s.generateToken(user)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"username": username,
		"role":     user.Role,
	}).Info("User login successful")

	return token, nil
}

// ValidateToken validates a JWT token
func (s *SimpleAuthenticator) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.secretKey, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	if claims.Issuer != s.issuer {
		return nil, fmt.Errorf("invalid issuer")
	}

	return claims, nil
}

// generateToken creates a JWT token for a user
func (s *SimpleAuthenticator) generateToken(user *SimpleUser) (string, error) {
	now := time.Now()
	
	claims := &Claims{
		UserID:      user.Username,
		Username:    user.Username,
		Role:        user.Role,
		Permissions: user.Permissions,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   user.Username,
			Audience:  []string{"siprec-api"},
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenExpiry)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        s.generateJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secretKey)
}

// generateJTI generates a unique token ID
func (s *SimpleAuthenticator) generateJTI() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// getRolePermissions returns permissions for a role
func (s *SimpleAuthenticator) getRolePermissions(role string) []string {
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