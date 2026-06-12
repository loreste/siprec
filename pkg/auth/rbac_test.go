package auth

import (
	"net/http"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func newTestRBACManager(t *testing.T) *RBACManager {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return NewRBACManager(nil, logger)
}

func TestRBACManager_CheckAccess_RolePermissions(t *testing.T) {
	rbac := newTestRBACManager(t)

	tests := []struct {
		name    string
		role    string
		path    string
		method  string
		allowed bool
	}{
		{"admin can create sessions", "admin", "/api/sessions", http.MethodPost, true},
		{"admin can delete users", "admin", "/api/users/123", http.MethodDelete, true},
		{"operator can list sessions", "operator", "/api/sessions", http.MethodGet, true},
		{"operator cannot delete sessions", "operator", "/api/sessions/123", http.MethodDelete, false},
		{"viewer can list sessions", "viewer", "/api/sessions", http.MethodGet, true},
		{"viewer cannot create sessions", "viewer", "/api/sessions", http.MethodPost, false},
		{"unknown role denied", "nonexistent", "/api/sessions", http.MethodGet, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := rbac.CheckAccess(&AccessContext{
				UserID:      "user-1",
				Username:    "tester",
				Role:        tc.role,
				RequestPath: tc.path,
				Method:      tc.method,
			})
			assert.Equal(t, tc.allowed, result.Allowed, "reason: %s", result.Reason)
		})
	}
}

func TestRBACManager_CheckAccess_ExplicitResourceAction(t *testing.T) {
	rbac := newTestRBACManager(t)

	// Direct user permissions take precedence over role permissions
	result := rbac.CheckAccess(&AccessContext{
		UserID:      "user-1",
		Username:    "tester",
		Role:        "viewer",
		Permissions: []string{"cdr:export"},
		Resource:    "cdr",
		Action:      "export",
	})
	assert.True(t, result.Allowed)
	assert.Equal(t, "cdr:export", result.Permission)

	// Denied when neither user nor role permissions match
	result = rbac.CheckAccess(&AccessContext{
		UserID:   "user-1",
		Username: "tester",
		Role:     "viewer",
		Resource: "system",
		Action:   "restart",
	})
	assert.False(t, result.Allowed)
}

func TestRBACManager_CheckAccess_WildcardPermissions(t *testing.T) {
	rbac := newTestRBACManager(t)

	// Resource wildcard
	result := rbac.CheckAccess(&AccessContext{
		Username:    "tester",
		Permissions: []string{"sessions:*"},
		Resource:    "sessions",
		Action:      "delete",
	})
	assert.True(t, result.Allowed)

	// Global wildcard
	result = rbac.CheckAccess(&AccessContext{
		Username:    "tester",
		Permissions: []string{"*"},
		Resource:    "system",
		Action:      "restart",
	})
	assert.True(t, result.Allowed)
}

func TestRBACManager_CheckAccess_UndeterminablePermission(t *testing.T) {
	rbac := newTestRBACManager(t)

	// Empty path and no resource/action fails closed
	result := rbac.CheckAccess(&AccessContext{
		Username: "tester",
		Role:     "admin",
	})
	assert.False(t, result.Allowed)
}
