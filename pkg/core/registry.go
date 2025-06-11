package core

import (
	"sync"

	"siprec-server/pkg/config"
	"siprec-server/pkg/stt"
)

// ServiceRegistry holds global service instances
type ServiceRegistry struct {
	mutex            sync.RWMutex
	asyncSTTProcessor *stt.AsyncSTTProcessor
	hotReloadManager  *config.HotReloadManager
}

var (
	globalRegistry *ServiceRegistry
	registryOnce   sync.Once
)

// GetServiceRegistry returns the global service registry
func GetServiceRegistry() *ServiceRegistry {
	registryOnce.Do(func() {
		globalRegistry = &ServiceRegistry{}
	})
	return globalRegistry
}

// SetAsyncSTTProcessor sets the global async STT processor
func (r *ServiceRegistry) SetAsyncSTTProcessor(processor *stt.AsyncSTTProcessor) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.asyncSTTProcessor = processor
}

// GetAsyncSTTProcessor returns the global async STT processor
func (r *ServiceRegistry) GetAsyncSTTProcessor() *stt.AsyncSTTProcessor {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.asyncSTTProcessor
}

// SetHotReloadManager sets the global hot reload manager
func (r *ServiceRegistry) SetHotReloadManager(manager *config.HotReloadManager) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.hotReloadManager = manager
}

// GetHotReloadManager returns the global hot reload manager
func (r *ServiceRegistry) GetHotReloadManager() *config.HotReloadManager {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return r.hotReloadManager
}