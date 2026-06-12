package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"siprec-server/pkg/auth"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func newRBACTestMiddleware(t *testing.T, enabled bool) *RBACMiddleware {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	rbacManager := auth.NewRBACManager(nil, logger)
	return NewRBACMiddleware(rbacManager, logger, &RBACConfig{
		Enabled:     enabled,
		ExemptPaths: DefaultRBACExemptPaths,
	})
}

func performRBACRequest(t *testing.T, middleware *RBACMiddleware, method, path string, user *auth.UserInfo) *httptest.ResponseRecorder {
	t.Helper()

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(method, path, nil)
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), UserContextKey, user))
	}

	rec := httptest.NewRecorder()
	middleware.Middleware(next).ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		assert.True(t, nextCalled, "next handler should have been invoked")
	} else {
		assert.False(t, nextCalled, "next handler should not have been invoked")
	}
	return rec
}

func TestRBACMiddleware_AllowsAuthorizedUser(t *testing.T) {
	middleware := newRBACTestMiddleware(t, true)

	adminUser := &auth.UserInfo{
		UserID:   "user-1",
		Username: "admin",
		Role:     "admin",
	}

	rec := performRBACRequest(t, middleware, http.MethodGet, "/api/sessions", adminUser)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Viewer role can list sessions
	viewerUser := &auth.UserInfo{
		UserID:   "user-2",
		Username: "viewer",
		Role:     "viewer",
	}
	rec = performRBACRequest(t, middleware, http.MethodGet, "/api/sessions", viewerUser)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRBACMiddleware_DeniesUnauthorizedUser(t *testing.T) {
	middleware := newRBACTestMiddleware(t, true)

	// Viewer role cannot create sessions
	viewerUser := &auth.UserInfo{
		UserID:   "user-2",
		Username: "viewer",
		Role:     "viewer",
	}
	rec := performRBACRequest(t, middleware, http.MethodPost, "/api/sessions", viewerUser)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"forbidden"}`, rec.Body.String())

	// Unknown role is denied
	unknownUser := &auth.UserInfo{
		UserID:   "user-3",
		Username: "ghost",
		Role:     "nonexistent",
	}
	rec = performRBACRequest(t, middleware, http.MethodGet, "/api/sessions", unknownUser)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestRBACMiddleware_FailsClosedWithoutPrincipal(t *testing.T) {
	middleware := newRBACTestMiddleware(t, true)

	rec := performRBACRequest(t, middleware, http.MethodGet, "/api/sessions", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.JSONEq(t, `{"error":"forbidden"}`, rec.Body.String())
}

func TestRBACMiddleware_DisabledPassesThrough(t *testing.T) {
	middleware := newRBACTestMiddleware(t, false)

	// Even unauthenticated requests pass through when RBAC is disabled
	rec := performRBACRequest(t, middleware, http.MethodPost, "/api/sessions", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRBACMiddleware_NilManagerPassesThrough(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	middleware := NewRBACMiddleware(nil, logger, &RBACConfig{Enabled: true})

	rec := performRBACRequest(t, middleware, http.MethodGet, "/api/sessions", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRBACMiddleware_ExemptPathsBypassEnforcement(t *testing.T) {
	middleware := newRBACTestMiddleware(t, true)

	for _, path := range []string{"/health", "/health/live", "/metrics", "/status", "/ws/transcriptions"} {
		rec := performRBACRequest(t, middleware, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusOK, rec.Code, "path %s should be exempt", path)
	}
}
