package http

import (
	"context"
	"net/http"
	"strings"

	"siprec-server/pkg/auth"
	"siprec-server/pkg/errors"

	"github.com/sirupsen/logrus"
)

// AuthMiddleware provides authentication middleware for HTTP handlers
type AuthMiddleware struct {
	simpleAuth *auth.SimpleAuthenticator
	logger     *logrus.Logger
	config     *AuthConfig
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	Enabled          bool
	RequireAuth      bool
	AllowAPIKey      bool
	AllowJWT         bool
	ExemptPaths      []string // Paths that don't require authentication
	RequiredScopes   map[string][]string // Required scopes per path
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(simpleAuth *auth.SimpleAuthenticator, logger *logrus.Logger, config *AuthConfig) *AuthMiddleware {
	if config == nil {
		config = &AuthConfig{
			Enabled:     true,
			RequireAuth: true,
			AllowAPIKey: true,
			AllowJWT:    true,
			ExemptPaths: []string{"/health", "/metrics", "/liveness", "/readiness", "/websocket-client"},
		}
	}

	return &AuthMiddleware{
		simpleAuth: simpleAuth,
		logger:     logger,
		config:     config,
	}
}

// Middleware returns the authentication middleware handler
func (am *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth if disabled
		if !am.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Check if path is exempt
		if am.isPathExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Try to authenticate
		userInfo, err := am.authenticate(r)
		if err != nil {
			if am.config.RequireAuth {
				am.logger.WithFields(logrus.Fields{
					"path":   r.URL.Path,
					"method": r.Method,
					"error":  err.Error(),
				}).Warning("Authentication failed")

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			// If auth not required, continue without user info
			next.ServeHTTP(w, r)
			return
		}

		// Check required scopes
		if !am.hasRequiredScopes(r.URL.Path, userInfo.Permissions) {
			am.logger.WithFields(logrus.Fields{
				"path":        r.URL.Path,
				"user":        userInfo.Username,
				"permissions": userInfo.Permissions,
			}).Warning("Insufficient permissions")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"error":"insufficient permissions"}`))
			return
		}

		// Add user info to context
		ctx := context.WithValue(r.Context(), "user", userInfo)
		r = r.WithContext(ctx)

		// Continue with authenticated user
		next.ServeHTTP(w, r)
	})
}

// WebSocketAuth authenticates WebSocket connections
func (am *AuthMiddleware) WebSocketAuth(r *http.Request) (*auth.UserInfo, error) {
	if !am.config.Enabled {
		return &auth.UserInfo{
			UserID:   "anonymous",
			Username: "anonymous",
			Role:     "viewer",
		}, nil
	}

	return am.authenticate(r)
}

// authenticate tries various authentication methods
func (am *AuthMiddleware) authenticate(r *http.Request) (*auth.UserInfo, error) {
	if am.simpleAuth == nil {
		return nil, errors.ErrUnauthorized
	}

	// Try JWT authentication first
	if am.config.AllowJWT {
		// Check Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := am.simpleAuth.ValidateToken(token)
			if err == nil {
				return &auth.UserInfo{
					UserID:      claims.UserID,
					Username:    claims.Username,
					Role:        claims.Role,
					Permissions: claims.Permissions,
				}, nil
			}
			am.logger.WithError(err).Debug("JWT authentication failed")
		}

		// Check for token in query params (for WebSocket)
		token := r.URL.Query().Get("token")
		if token != "" && isWebSocketRequest(r) {
			claims, err := am.simpleAuth.ValidateToken(token)
			if err == nil {
				return &auth.UserInfo{
					UserID:      claims.UserID,
					Username:    claims.Username,
					Role:        claims.Role,
					Permissions: claims.Permissions,
				}, nil
			}
			am.logger.WithError(err).Debug("JWT query param authentication failed")
		}
	}

	// Try API key authentication
	if am.config.AllowAPIKey {
		// Check X-API-Key header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != "" {
			userInfo, err := am.simpleAuth.ValidateAPIKey(apiKey)
			if err == nil {
				return userInfo, nil
			}
			am.logger.WithError(err).Debug("API key authentication failed")
		}

		// Check for API key in query params (for WebSocket)
		apiKey = r.URL.Query().Get("api_key")
		if apiKey != "" && isWebSocketRequest(r) {
			userInfo, err := am.simpleAuth.ValidateAPIKey(apiKey)
			if err == nil {
				return userInfo, nil
			}
			am.logger.WithError(err).Debug("API key query param authentication failed")
		}
	}

	return nil, errors.ErrUnauthorized
}

// isPathExempt checks if a path is exempt from authentication
func (am *AuthMiddleware) isPathExempt(path string) bool {
	for _, exempt := range am.config.ExemptPaths {
		if path == exempt || strings.HasPrefix(path, exempt) {
			return true
		}
	}
	return false
}

// hasRequiredScopes checks if user has required scopes for a path
func (am *AuthMiddleware) hasRequiredScopes(path string, userPermissions []string) bool {
	requiredScopes, exists := am.config.RequiredScopes[path]
	if !exists {
		// No specific scopes required
		return true
	}

	// Check if user has at least one required scope
	for _, required := range requiredScopes {
		for _, userScope := range userPermissions {
			if userScope == required || strings.HasSuffix(userScope, ":*") {
				// Check wildcard permissions
				prefix := strings.TrimSuffix(userScope, "*")
				if strings.HasPrefix(required, prefix) {
					return true
				}
			}
		}
	}

	return false
}

func isWebSocketRequest(r *http.Request) bool {
	upgrade := r.Header.Get("Upgrade")
	if strings.EqualFold(upgrade, "websocket") {
		return true
	}

	for _, token := range strings.Split(r.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
			return true
		}
	}

	return false
}

// getRolePermissions returns permissions for a role
func (am *AuthMiddleware) getRolePermissions(role string) []string {
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

// GetUserFromContext extracts user info from request context
func GetUserFromContext(ctx context.Context) (*auth.UserInfo, bool) {
	user, ok := ctx.Value("user").(*auth.UserInfo)
	return user, ok
}
