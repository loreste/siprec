package stt

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// ProviderFactory constructs a Provider instance at runtime.
type ProviderFactory func(logger *logrus.Logger, svc *TranscriptionService) (Provider, error)

var (
	providerFactoriesMu sync.RWMutex
	providerFactories   = make(map[string]ProviderFactory)
)

// RegisterProviderFactory registers a factory that can build a provider by name.
func RegisterProviderFactory(name string, factory ProviderFactory) {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" || factory == nil {
		return
	}

	providerFactoriesMu.Lock()
	defer providerFactoriesMu.Unlock()
	providerFactories[trimmed] = factory
}

// BuildRegisteredProvider instantiates a provider if a factory has been registered.
func BuildRegisteredProvider(name string, logger *logrus.Logger, svc *TranscriptionService) (Provider, error) {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	providerFactoriesMu.RLock()
	factory, ok := providerFactories[trimmed]
	providerFactoriesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no provider factory registered for %s", name)
	}
	return factory(logger, svc)
}
