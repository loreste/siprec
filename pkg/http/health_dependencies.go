package http

import "sync"

// HealthChecker is implemented by components that can report their health.
type HealthChecker interface {
	Health() error
}

// HealthCheckerFunc adapts a plain function to the HealthChecker interface.
type HealthCheckerFunc func() error

// Health implements the HealthChecker interface.
func (f HealthCheckerFunc) Health() error {
	return f()
}

// healthDependencies holds optional component health checkers that are
// registered by the server wiring at startup. Access is guarded by a mutex
// because registration happens during startup while the health endpoint may
// already be serving requests.
type healthDependencies struct {
	mu         sync.RWMutex
	redis      HealthChecker
	database   HealthChecker
	encryption HealthChecker
}

var healthDeps healthDependencies

// RegisterRedisHealthChecker registers the Redis session store health checker.
// Passing nil removes a previously registered checker.
func RegisterRedisHealthChecker(checker HealthChecker) {
	healthDeps.mu.Lock()
	healthDeps.redis = checker
	healthDeps.mu.Unlock()
}

// RegisterDatabaseHealthChecker registers the database health checker.
// Passing nil removes a previously registered checker.
func RegisterDatabaseHealthChecker(checker HealthChecker) {
	healthDeps.mu.Lock()
	healthDeps.database = checker
	healthDeps.mu.Unlock()
}

// RegisterEncryptionHealthChecker registers the encryption service health checker.
// Passing nil removes a previously registered checker.
func RegisterEncryptionHealthChecker(checker HealthChecker) {
	healthDeps.mu.Lock()
	healthDeps.encryption = checker
	healthDeps.mu.Unlock()
}

// getRedisHealthChecker returns the registered Redis health checker, or nil.
func getRedisHealthChecker() HealthChecker {
	healthDeps.mu.RLock()
	defer healthDeps.mu.RUnlock()
	return healthDeps.redis
}

// getDatabaseHealthChecker returns the registered database health checker, or nil.
func getDatabaseHealthChecker() HealthChecker {
	healthDeps.mu.RLock()
	defer healthDeps.mu.RUnlock()
	return healthDeps.database
}

// getEncryptionHealthChecker returns the registered encryption health checker, or nil.
func getEncryptionHealthChecker() HealthChecker {
	healthDeps.mu.RLock()
	defer healthDeps.mu.RUnlock()
	return healthDeps.encryption
}
