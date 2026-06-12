package auth

import (
	stdctx "context"
	"fmt"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/database"
	"siprec-server/pkg/security/audit"

	"github.com/sirupsen/logrus"
)

// RBACManager manages role-based access control
type RBACManager struct {
	roles       map[string]*Role
	permissions map[string]*Permission
	mutex       sync.RWMutex
	logger      *logrus.Logger
}

// Role represents a user role with permissions
type Role struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Permission represents a system permission
type Permission struct {
	Name        string    `json:"name"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
}

// AccessContext represents the context for access checks
type AccessContext struct {
	UserID      string                 `json:"user_id"`
	Username    string                 `json:"username"`
	Role        string                 `json:"role"`
	Permissions []string               `json:"permissions"`
	RequestPath string                 `json:"request_path"`
	Method      string                 `json:"method"`
	Resource    string                 `json:"resource"`
	Action      string                 `json:"action"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// AccessResult represents the result of an access check
type AccessResult struct {
	Allowed    bool      `json:"allowed"`
	Reason     string    `json:"reason"`
	Permission string    `json:"permission"`
	Timestamp  time.Time `json:"timestamp"`
}

// NewRBACManager creates a new RBAC manager
func NewRBACManager(repo *database.Repository, logger *logrus.Logger) *RBACManager {
	rbac := &RBACManager{
		roles:       make(map[string]*Role),
		permissions: make(map[string]*Permission),
		logger:      logger,
	}

	// Initialize default permissions and roles
	rbac.initializeDefaultPermissions()
	rbac.initializeDefaultRoles()

	logger.Info("RBAC manager initialized")
	return rbac
}

// CheckAccess checks if a user has access to perform an action
func (r *RBACManager) CheckAccess(context *AccessContext) *AccessResult {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	result := &AccessResult{
		Timestamp: time.Now(),
	}

	logAudit := func(outcome string, reason string) {
		if context == nil {
			return
		}

		tenant := ""
		callID := ""
		if context.Metadata != nil {
			if value, ok := context.Metadata["tenant"].(string); ok {
				tenant = value
			}
			if value, ok := context.Metadata["call_id"].(string); ok {
				callID = value
			}
		}

		users := make([]string, 0, 1)
		if context.Username != "" {
			users = append(users, context.Username)
		} else if context.UserID != "" {
			users = append(users, context.UserID)
		}

		details := map[string]interface{}{
			"resource":     context.Resource,
			"action":       context.Action,
			"permission":   result.Permission,
			"allowed":      outcome == audit.OutcomeSuccess,
			"reason":       reason,
			"request_path": context.RequestPath,
		}

		evt := &audit.Event{
			Category: "policy",
			Action:   "access_check",
			Outcome:  outcome,
			CallID:   callID,
			Tenant:   tenant,
			Users:    users,
			Details:  details,
		}

		audit.Log(stdctx.Background(), r.logger, evt)
	}

	// Build required permission from context
	var requiredPermission string
	if context.Resource != "" && context.Action != "" {
		requiredPermission = fmt.Sprintf("%s:%s", context.Resource, context.Action)
	} else {
		// Try to infer from request path and method
		requiredPermission = r.inferPermissionFromRequest(context.RequestPath, context.Method)
	}

	if requiredPermission == "" {
		result.Allowed = false
		result.Reason = "Could not determine required permission"
		return result
	}

	result.Permission = requiredPermission

	// Check if user has the required permission
	if r.hasPermission(context.Permissions, requiredPermission) {
		result.Allowed = true
		result.Reason = "Permission granted"

		r.logger.WithFields(logrus.Fields{
			"user_id":    context.UserID,
			"username":   context.Username,
			"role":       context.Role,
			"permission": requiredPermission,
			"resource":   context.Resource,
			"action":     context.Action,
		}).Debug("Access granted")

		logAudit(audit.OutcomeSuccess, result.Reason)

		return result
	}

	// Check role-based permissions
	role, exists := r.roles[context.Role]
	if exists {
		if r.hasPermission(role.Permissions, requiredPermission) {
			result.Allowed = true
			result.Reason = "Role permission granted"

			r.logger.WithFields(logrus.Fields{
				"user_id":    context.UserID,
				"username":   context.Username,
				"role":       context.Role,
				"permission": requiredPermission,
			}).Debug("Access granted via role")

			logAudit(audit.OutcomeSuccess, result.Reason)

			return result
		}
	}

	// Access denied
	result.Allowed = false
	result.Reason = "Insufficient permissions"

	r.logger.WithFields(logrus.Fields{
		"user_id":    context.UserID,
		"username":   context.Username,
		"role":       context.Role,
		"permission": requiredPermission,
		"resource":   context.Resource,
		"action":     context.Action,
	}).Warning("Access denied")

	logAudit(audit.OutcomeFailure, result.Reason)

	return result
}

// hasPermission checks if a permission list contains the required permission
func (r *RBACManager) hasPermission(permissions []string, required string) bool {
	for _, permission := range permissions {
		if permission == required {
			return true
		}

		// Check wildcard permissions
		if strings.HasSuffix(permission, ":*") {
			prefix := strings.TrimSuffix(permission, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}

		// Check for admin wildcard
		if permission == "*" || permission == "admin:*" {
			return true
		}
	}

	return false
}

// inferPermissionFromRequest infers the required permission from HTTP request
func (r *RBACManager) inferPermissionFromRequest(path, method string) string {
	// Remove leading slash and split path
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return ""
	}

	// Map common API patterns to permissions
	resource := parts[0]
	if len(parts) > 1 && parts[0] == "api" {
		resource = parts[1]
	}

	var action string
	switch method {
	case "GET":
		if len(parts) > 2 || (len(parts) > 1 && parts[len(parts)-1] != resource) {
			action = "read"
		} else {
			action = "list"
		}
	case "POST":
		action = "create"
	case "PUT", "PATCH":
		action = "update"
	case "DELETE":
		action = "delete"
	default:
		action = "access"
	}

	return fmt.Sprintf("%s:%s", resource, action)
}

// Initialize default permissions and roles

func (r *RBACManager) initializeDefaultPermissions() {
	defaultPermissions := []Permission{
		// Session permissions
		{Name: "sessions:list", Resource: "sessions", Action: "list", Description: "List sessions", IsSystem: true},
		{Name: "sessions:read", Resource: "sessions", Action: "read", Description: "Read session details", IsSystem: true},
		{Name: "sessions:create", Resource: "sessions", Action: "create", Description: "Create sessions", IsSystem: true},
		{Name: "sessions:update", Resource: "sessions", Action: "update", Description: "Update sessions", IsSystem: true},
		{Name: "sessions:delete", Resource: "sessions", Action: "delete", Description: "Delete sessions", IsSystem: true},

		// CDR permissions
		{Name: "cdr:list", Resource: "cdr", Action: "list", Description: "List CDRs", IsSystem: true},
		{Name: "cdr:read", Resource: "cdr", Action: "read", Description: "Read CDR details", IsSystem: true},
		{Name: "cdr:export", Resource: "cdr", Action: "export", Description: "Export CDRs", IsSystem: true},

		// User permissions
		{Name: "users:list", Resource: "users", Action: "list", Description: "List users", IsSystem: true},
		{Name: "users:read", Resource: "users", Action: "read", Description: "Read user details", IsSystem: true},
		{Name: "users:create", Resource: "users", Action: "create", Description: "Create users", IsSystem: true},
		{Name: "users:update", Resource: "users", Action: "update", Description: "Update users", IsSystem: true},
		{Name: "users:delete", Resource: "users", Action: "delete", Description: "Delete users", IsSystem: true},

		// System permissions
		{Name: "system:read", Resource: "system", Action: "read", Description: "Read system information", IsSystem: true},
		{Name: "system:config", Resource: "system", Action: "config", Description: "Configure system", IsSystem: true},
		{Name: "system:restart", Resource: "system", Action: "restart", Description: "Restart system", IsSystem: true},

		// Monitoring permissions
		{Name: "monitoring:read", Resource: "monitoring", Action: "read", Description: "Read monitoring data", IsSystem: true},
		{Name: "monitoring:metrics", Resource: "monitoring", Action: "metrics", Description: "Access metrics", IsSystem: true},
		{Name: "monitoring:health", Resource: "monitoring", Action: "health", Description: "Check health status", IsSystem: true},

		// API permissions
		{Name: "api:access", Resource: "api", Action: "access", Description: "Access API", IsSystem: true},
		{Name: "api:keys", Resource: "api", Action: "keys", Description: "Manage API keys", IsSystem: true},
	}

	for _, perm := range defaultPermissions {
		perm.CreatedAt = time.Now()
		r.permissions[perm.Name] = &perm
	}

	r.logger.WithField("count", len(defaultPermissions)).Info("Default permissions initialized")
}

func (r *RBACManager) initializeDefaultRoles() {
	defaultRoles := []Role{
		{
			Name:        "admin",
			Description: "System administrator with full access",
			Permissions: []string{
				"sessions:*", "cdr:*", "users:*", "system:*", "monitoring:*", "api:*",
			},
			IsSystem:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			Name:        "operator",
			Description: "Operator with read/write access to sessions and CDRs",
			Permissions: []string{
				"sessions:list", "sessions:read", "sessions:create", "sessions:update",
				"cdr:list", "cdr:read", "cdr:export",
				"monitoring:read", "monitoring:metrics", "monitoring:health",
				"api:access",
			},
			IsSystem:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			Name:        "viewer",
			Description: "Read-only access to sessions and CDRs",
			Permissions: []string{
				"sessions:list", "sessions:read",
				"cdr:list", "cdr:read",
				"monitoring:health",
				"api:access",
			},
			IsSystem:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	for _, role := range defaultRoles {
		r.roles[role.Name] = &role
	}

	r.logger.WithField("count", len(defaultRoles)).Info("Default roles initialized")
}
