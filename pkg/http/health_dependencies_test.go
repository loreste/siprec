package http

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// resetHealthDependencies clears all registered health checkers
func resetHealthDependencies() {
	RegisterRedisHealthChecker(nil)
	RegisterDatabaseHealthChecker(nil)
	RegisterEncryptionHealthChecker(nil)
}

// performHealthCheck runs the health handler and decodes the response
func performHealthCheck(t *testing.T, server *Server) HealthStatus {
	t.Helper()

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	server.HealthHandler(rec, req)

	var health HealthStatus
	err := json.NewDecoder(rec.Body).Decode(&health)
	assert.NoError(t, err)
	return health
}

func newHealthTestServer(t *testing.T) *Server {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return NewServer(logger, NewDefaultConfig(), nil)
}

func TestHealthHandler_DependenciesNotConfigured(t *testing.T) {
	resetHealthDependencies()
	t.Cleanup(resetHealthDependencies)

	server := newHealthTestServer(t)
	health := performHealthCheck(t, server)

	for _, check := range []string{"redis", "database", "encryption"} {
		result, ok := health.Checks[check]
		assert.True(t, ok, "expected %s check to be present", check)
		assert.Equal(t, "not_configured", result.Status, "check %s", check)
	}
}

func TestHealthHandler_RegisteredCheckersHealthy(t *testing.T) {
	resetHealthDependencies()
	t.Cleanup(resetHealthDependencies)

	healthy := HealthCheckerFunc(func() error { return nil })
	RegisterRedisHealthChecker(healthy)
	RegisterDatabaseHealthChecker(healthy)
	RegisterEncryptionHealthChecker(healthy)

	server := newHealthTestServer(t)
	health := performHealthCheck(t, server)

	for _, check := range []string{"redis", "database", "encryption"} {
		result, ok := health.Checks[check]
		assert.True(t, ok, "expected %s check to be present", check)
		assert.Equal(t, "healthy", result.Status, "check %s", check)
	}
}

func TestHealthHandler_RegisteredCheckersFailing(t *testing.T) {
	resetHealthDependencies()
	t.Cleanup(resetHealthDependencies)

	failing := HealthCheckerFunc(func() error { return errors.New("connection refused") })
	RegisterRedisHealthChecker(failing)
	RegisterDatabaseHealthChecker(failing)
	RegisterEncryptionHealthChecker(failing)

	server := newHealthTestServer(t)
	health := performHealthCheck(t, server)

	for _, check := range []string{"redis", "database", "encryption"} {
		result, ok := health.Checks[check]
		assert.True(t, ok, "expected %s check to be present", check)
		assert.Equal(t, "degraded", result.Status, "check %s", check)
		assert.Contains(t, result.Message, "connection refused", "check %s", check)
	}
}

func TestHealthHandler_UnregisteringChecker(t *testing.T) {
	resetHealthDependencies()
	t.Cleanup(resetHealthDependencies)

	RegisterDatabaseHealthChecker(HealthCheckerFunc(func() error { return nil }))

	server := newHealthTestServer(t)
	health := performHealthCheck(t, server)
	assert.Equal(t, "healthy", health.Checks["database"].Status)

	// Unregister and verify it reverts to not_configured
	RegisterDatabaseHealthChecker(nil)
	health = performHealthCheck(t, server)
	assert.Equal(t, "not_configured", health.Checks["database"].Status)
}
