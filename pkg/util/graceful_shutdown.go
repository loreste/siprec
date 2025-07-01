package util

import (
	"context"
	"io"
	"sync"
	"time"
	
	"github.com/sirupsen/logrus"
)

// GracefulShutdown manages graceful shutdown of multiple resources
type GracefulShutdown struct {
	resources []ShutdownResource
	mu        sync.Mutex
	logger    *logrus.Logger
	timeout   time.Duration
}

// ShutdownResource represents a resource that needs graceful shutdown
type ShutdownResource struct {
	Name     string
	Shutdown func(context.Context) error
	Priority int // Lower numbers shut down first
}

// NewGracefulShutdown creates a new graceful shutdown manager
func NewGracefulShutdown(logger *logrus.Logger, timeout time.Duration) *GracefulShutdown {
	if timeout <= 0 {
		timeout = 30 * time.Second // Default 30 second timeout
	}
	
	return &GracefulShutdown{
		resources: make([]ShutdownResource, 0),
		logger:    logger,
		timeout:   timeout,
	}
}

// Register adds a resource to be shut down
func (gs *GracefulShutdown) Register(resource ShutdownResource) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	
	// Insert in priority order (lower priority first)
	inserted := false
	for i, r := range gs.resources {
		if resource.Priority < r.Priority {
			gs.resources = append(gs.resources[:i], append([]ShutdownResource{resource}, gs.resources[i:]...)...)
			inserted = true
			break
		}
	}
	
	if !inserted {
		gs.resources = append(gs.resources, resource)
	}
	
	gs.logger.WithFields(logrus.Fields{
		"resource": resource.Name,
		"priority": resource.Priority,
	}).Debug("Registered resource for graceful shutdown")
}

// RegisterCloser registers an io.Closer for shutdown
func (gs *GracefulShutdown) RegisterCloser(name string, closer io.Closer, priority int) {
	gs.Register(ShutdownResource{
		Name:     name,
		Priority: priority,
		Shutdown: func(ctx context.Context) error {
			return closer.Close()
		},
	})
}

// Shutdown performs graceful shutdown of all registered resources
func (gs *GracefulShutdown) Shutdown(ctx context.Context) error {
	gs.mu.Lock()
	resources := make([]ShutdownResource, len(gs.resources))
	copy(resources, gs.resources)
	gs.mu.Unlock()
	
	gs.logger.WithField("resource_count", len(resources)).Info("Starting graceful shutdown")
	
	// Create a context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, gs.timeout)
	defer cancel()
	
	// Channel to collect errors
	errChan := make(chan error, len(resources))
	
	// Shutdown resources in priority order
	for _, resource := range resources {
		gs.logger.WithField("resource", resource.Name).Debug("Shutting down resource")
		
		// Run shutdown in goroutine with panic recovery
		go func(res ShutdownResource) {
			defer func() {
				if r := recover(); r != nil {
					gs.logger.WithFields(logrus.Fields{
						"panic": r,
						"resource": res.Name,
					}).Error("Panic during resource shutdown")
					errChan <- &ShutdownPanicError{Resource: res.Name, Panic: r}
				}
			}()
			
			// Create a channel for this specific shutdown
			done := make(chan error, 1)
			
			go func() {
				done <- res.Shutdown(shutdownCtx)
			}()
			
			select {
			case err := <-done:
				if err != nil {
					gs.logger.WithError(err).WithField("resource", res.Name).Error("Error shutting down resource")
					errChan <- &ShutdownError{Resource: res.Name, Err: err}
				} else {
					gs.logger.WithField("resource", res.Name).Debug("Resource shut down successfully")
					errChan <- nil
				}
			case <-shutdownCtx.Done():
				gs.logger.WithField("resource", res.Name).Warn("Shutdown timeout for resource")
				errChan <- &ShutdownTimeoutError{Resource: res.Name}
			}
		}(resource)
	}
	
	// Collect results
	var shutdownErrors []error
	for i := 0; i < len(resources); i++ {
		if err := <-errChan; err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	
	if len(shutdownErrors) > 0 {
		return &MultiShutdownError{Errors: shutdownErrors}
	}
	
	gs.logger.Info("Graceful shutdown completed successfully")
	return nil
}

// Shutdown error types
type ShutdownError struct {
	Resource string
	Err      error
}

func (e *ShutdownError) Error() string {
	return "shutdown error for " + e.Resource + ": " + e.Err.Error()
}

type ShutdownTimeoutError struct {
	Resource string
}

func (e *ShutdownTimeoutError) Error() string {
	return "shutdown timeout for " + e.Resource
}

type ShutdownPanicError struct {
	Resource string
	Panic    interface{}
}

func (e *ShutdownPanicError) Error() string {
	return "panic during shutdown of " + e.Resource
}

type MultiShutdownError struct {
	Errors []error
}

func (e *MultiShutdownError) Error() string {
	return "multiple errors during shutdown"
}