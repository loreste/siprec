package http

import (
	"net/http"
	"strings"

	"siprec-server/pkg/auth"

	"github.com/sirupsen/logrus"
)

// RBACConfig holds configuration for the RBAC enforcement middleware
type RBACConfig struct {
	// Enabled determines whether RBAC enforcement is active. When false the
	// middleware passes all requests through unchanged (backward compatible).
	Enabled bool

	// ExemptPaths are path prefixes that bypass RBAC enforcement
	// (e.g. health probes and metrics scraping endpoints).
	ExemptPaths []string
}

// DefaultRBACExemptPaths are the paths exempt from RBAC enforcement by default.
// These endpoints either perform their own authentication (WebSockets) or must
// remain reachable by infrastructure probes.
var DefaultRBACExemptPaths = []string{
	"/health",
	"/metrics",
	"/status",
	"/ws/",
	"/websocket-client",
}

// RBACMiddleware enforces role-based access control on HTTP requests. It must
// run after the authentication middleware so the authenticated principal is
// available in the request context.
type RBACMiddleware struct {
	rbac   *auth.RBACManager
	logger *logrus.Logger
	config *RBACConfig
}

// NewRBACMiddleware creates a new RBAC enforcement middleware
func NewRBACMiddleware(rbac *auth.RBACManager, logger *logrus.Logger, config *RBACConfig) *RBACMiddleware {
	if config == nil {
		config = &RBACConfig{
			Enabled:     true,
			ExemptPaths: DefaultRBACExemptPaths,
		}
	}
	if config.ExemptPaths == nil {
		config.ExemptPaths = DefaultRBACExemptPaths
	}

	return &RBACMiddleware{
		rbac:   rbac,
		logger: logger,
		config: config,
	}
}

// Middleware returns the RBAC enforcement handler
func (rm *RBACMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Pass through when RBAC is disabled or not wired up
		if !rm.config.Enabled || rm.rbac == nil {
			next.ServeHTTP(w, r)
			return
		}

		// Skip exempt paths
		if rm.isPathExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Resolve the authenticated principal placed in the context by the
		// authentication middleware. Fail closed when it is missing.
		userInfo, ok := GetUserFromContext(r.Context())
		if !ok || userInfo == nil {
			rm.logger.WithFields(logrus.Fields{
				"path":   r.URL.Path,
				"method": r.Method,
			}).Warning("RBAC denied request without authenticated principal")
			rm.deny(w)
			return
		}

		// Derive resource/action from the route and method and check access.
		// Resource and Action are left empty so the RBAC manager infers them
		// from the request path and method.
		accessCtx := &auth.AccessContext{
			UserID:      userInfo.UserID,
			Username:    userInfo.Username,
			Role:        userInfo.Role,
			Permissions: userInfo.Permissions,
			RequestPath: r.URL.Path,
			Method:      r.Method,
		}

		result := rm.rbac.CheckAccess(accessCtx)
		if !result.Allowed {
			rm.logger.WithFields(logrus.Fields{
				"path":       r.URL.Path,
				"method":     r.Method,
				"user":       userInfo.Username,
				"role":       userInfo.Role,
				"permission": result.Permission,
				"reason":     result.Reason,
			}).Warning("RBAC denied request")
			rm.deny(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// deny writes a standardized 403 response
func (rm *RBACMiddleware) deny(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"forbidden"}`))
}

// isPathExempt checks if a path is exempt from RBAC enforcement
func (rm *RBACMiddleware) isPathExempt(path string) bool {
	for _, exempt := range rm.config.ExemptPaths {
		if path == exempt || strings.HasPrefix(path, exempt) {
			return true
		}
	}
	return false
}
