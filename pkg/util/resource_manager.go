package util

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ResourceManager provides centralized resource management and cleanup
type ResourceManager struct {
	logger    *logrus.Logger
	resources map[string]Resource
	mutex     sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// Resource represents a managed resource
type Resource interface {
	ID() string
	Type() string
	Close() error
	IsActive() bool
	CreatedAt() time.Time
	LastAccessed() time.Time
}

// BaseResource provides common resource functionality
type BaseResource struct {
	id           string
	resourceType string
	createdAt    time.Time
	lastAccessed time.Time
	active       bool
	mutex        sync.RWMutex
}

// NewBaseResource creates a new base resource
func NewBaseResource(id, resourceType string) *BaseResource {
	now := time.Now()
	return &BaseResource{
		id:           id,
		resourceType: resourceType,
		createdAt:    now,
		lastAccessed: now,
		active:       true,
	}
}

func (br *BaseResource) ID() string { return br.id }
func (br *BaseResource) Type() string { return br.resourceType }
func (br *BaseResource) CreatedAt() time.Time { return br.createdAt }
func (br *BaseResource) IsActive() bool {
	br.mutex.RLock()
	defer br.mutex.RUnlock()
	return br.active
}
func (br *BaseResource) LastAccessed() time.Time {
	br.mutex.RLock()
	defer br.mutex.RUnlock()
	return br.lastAccessed
}

func (br *BaseResource) UpdateAccess() {
	br.mutex.Lock()
	defer br.mutex.Unlock()
	br.lastAccessed = time.Now()
}

func (br *BaseResource) Deactivate() {
	br.mutex.Lock()
	defer br.mutex.Unlock()
	br.active = false
}

// FileResource manages file handles
type FileResource struct {
	*BaseResource
	file *os.File
	path string
}

// NewFileResource creates a new file resource
func NewFileResource(id, path string, file *os.File) *FileResource {
	return &FileResource{
		BaseResource: NewBaseResource(id, "file"),
		file:         file,
		path:         path,
	}
}

func (fr *FileResource) Close() error {
	fr.Deactivate()
	if fr.file != nil {
		return fr.file.Close()
	}
	return nil
}

func (fr *FileResource) File() *os.File {
	fr.UpdateAccess()
	return fr.file
}

func (fr *FileResource) Path() string {
	return fr.path
}

// NetworkResource manages network connections
type NetworkResource struct {
	*BaseResource
	conn net.Conn
	addr string
}

// NewNetworkResource creates a new network resource
func NewNetworkResource(id, addr string, conn net.Conn) *NetworkResource {
	return &NetworkResource{
		BaseResource: NewBaseResource(id, "network"),
		conn:         conn,
		addr:         addr,
	}
}

func (nr *NetworkResource) Close() error {
	nr.Deactivate()
	if nr.conn != nil {
		return nr.conn.Close()
	}
	return nil
}

func (nr *NetworkResource) Conn() net.Conn {
	nr.UpdateAccess()
	return nr.conn
}

func (nr *NetworkResource) Addr() string {
	return nr.addr
}

// UDPResource manages UDP connections
type UDPResource struct {
	*BaseResource
	conn *net.UDPConn
	addr *net.UDPAddr
	port int
}

// NewUDPResource creates a new UDP resource
func NewUDPResource(id string, conn *net.UDPConn, addr *net.UDPAddr, port int) *UDPResource {
	return &UDPResource{
		BaseResource: NewBaseResource(id, "udp"),
		conn:         conn,
		addr:         addr,
		port:         port,
	}
}

func (ur *UDPResource) Close() error {
	ur.Deactivate()
	if ur.conn != nil {
		return ur.conn.Close()
	}
	return nil
}

func (ur *UDPResource) Conn() *net.UDPConn {
	ur.UpdateAccess()
	return ur.conn
}

func (ur *UDPResource) Port() int {
	return ur.port
}

// GoroutineResource manages goroutines
type GoroutineResource struct {
	*BaseResource
	cancel context.CancelFunc
	done   chan struct{}
}

// NewGoroutineResource creates a new goroutine resource
func NewGoroutineResource(id string, cancel context.CancelFunc) *GoroutineResource {
	return &GoroutineResource{
		BaseResource: NewBaseResource(id, "goroutine"),
		cancel:       cancel,
		done:         make(chan struct{}),
	}
}

func (gr *GoroutineResource) Close() error {
	gr.Deactivate()
	if gr.cancel != nil {
		gr.cancel()
	}
	// Wait for goroutine to finish with timeout
	select {
	case <-gr.done:
	case <-time.After(5 * time.Second):
		// Timeout waiting for goroutine to finish
	}
	return nil
}

func (gr *GoroutineResource) Done() {
	close(gr.done)
}

// StreamResource manages IO streams
type StreamResource struct {
	*BaseResource
	closer io.Closer
	name   string
}

// NewStreamResource creates a new stream resource
func NewStreamResource(id, name string, closer io.Closer) *StreamResource {
	return &StreamResource{
		BaseResource: NewBaseResource(id, "stream"),
		closer:       closer,
		name:         name,
	}
}

func (sr *StreamResource) Close() error {
	sr.Deactivate()
	if sr.closer != nil {
		return sr.closer.Close()
	}
	return nil
}

func (sr *StreamResource) Name() string {
	return sr.name
}

// NewResourceManager creates a new resource manager
func NewResourceManager(logger *logrus.Logger) *ResourceManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	rm := &ResourceManager{
		logger:    logger,
		resources: make(map[string]Resource),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start cleanup routine
	rm.wg.Add(1)
	go rm.cleanupRoutine()

	return rm
}

// Register adds a resource to the manager
func (rm *ResourceManager) Register(resource Resource) error {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	if _, exists := rm.resources[resource.ID()]; exists {
		return fmt.Errorf("resource with ID %s already exists", resource.ID())
	}

	rm.resources[resource.ID()] = resource
	rm.logger.WithFields(logrus.Fields{
		"resource_id":   resource.ID(),
		"resource_type": resource.Type(),
	}).Debug("Resource registered")

	return nil
}

// Unregister removes and closes a resource
func (rm *ResourceManager) Unregister(id string) error {
	rm.mutex.Lock()
	resource, exists := rm.resources[id]
	if exists {
		delete(rm.resources, id)
	}
	rm.mutex.Unlock()

	if !exists {
		return fmt.Errorf("resource with ID %s not found", id)
	}

	err := resource.Close()
	rm.logger.WithFields(logrus.Fields{
		"resource_id":   resource.ID(),
		"resource_type": resource.Type(),
	}).Debug("Resource unregistered")

	return err
}

// Get retrieves a resource by ID
func (rm *ResourceManager) Get(id string) (Resource, bool) {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()
	resource, exists := rm.resources[id]
	return resource, exists
}

// GetByType retrieves all resources of a specific type
func (rm *ResourceManager) GetByType(resourceType string) []Resource {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	var resources []Resource
	for _, resource := range rm.resources {
		if resource.Type() == resourceType {
			resources = append(resources, resource)
		}
	}
	return resources
}

// Stats returns resource statistics
func (rm *ResourceManager) Stats() ResourceStats {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	stats := ResourceStats{
		Total: len(rm.resources),
		ByType: make(map[string]int),
	}

	for _, resource := range rm.resources {
		stats.ByType[resource.Type()]++
		if resource.IsActive() {
			stats.Active++
		} else {
			stats.Inactive++
		}
	}

	return stats
}

// CleanupStale removes stale resources
func (rm *ResourceManager) CleanupStale(maxAge time.Duration) int {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	now := time.Now()
	staleResources := []string{}

	for id, resource := range rm.resources {
		if !resource.IsActive() || now.Sub(resource.LastAccessed()) > maxAge {
			staleResources = append(staleResources, id)
		}
	}

	for _, id := range staleResources {
		resource := rm.resources[id]
		delete(rm.resources, id)
		
		// Close resource in background to avoid blocking
		go func(r Resource) {
			if err := r.Close(); err != nil {
				rm.logger.WithError(err).WithField("resource_id", r.ID()).Warning("Error closing stale resource")
			}
		}(resource)
	}

	if len(staleResources) > 0 {
		rm.logger.WithField("count", len(staleResources)).Info("Cleaned up stale resources")
	}

	return len(staleResources)
}

// CloseAll closes all resources
func (rm *ResourceManager) CloseAll() error {
	rm.mutex.Lock()
	resources := make([]Resource, 0, len(rm.resources))
	for _, resource := range rm.resources {
		resources = append(resources, resource)
	}
	rm.resources = make(map[string]Resource)
	rm.mutex.Unlock()

	var lastError error
	for _, resource := range resources {
		if err := resource.Close(); err != nil {
			rm.logger.WithError(err).WithField("resource_id", resource.ID()).Error("Error closing resource")
			lastError = err
		}
	}

	return lastError
}

// Shutdown gracefully shuts down the resource manager
func (rm *ResourceManager) Shutdown(timeout time.Duration) error {
	// Cancel cleanup routine
	rm.cancel()

	// Close all resources
	err := rm.CloseAll()

	// Wait for cleanup routine to finish
	done := make(chan struct{})
	go func() {
		rm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		rm.logger.Warning("Resource manager shutdown timed out")
	}

	return err
}

// cleanupRoutine periodically cleans up stale resources
func (rm *ResourceManager) cleanupRoutine() {
	defer rm.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.CleanupStale(5 * time.Minute)
		}
	}
}

// Helper functions for common resource creation

// CreateFileResource creates and registers a file resource
func (rm *ResourceManager) CreateFileResource(id, path string, flag int, perm os.FileMode) (*FileResource, error) {
	file, err := os.OpenFile(path, flag, perm)
	if err != nil {
		return nil, err
	}

	resource := NewFileResource(id, path, file)
	if err := rm.Register(resource); err != nil {
		file.Close()
		return nil, err
	}

	return resource, nil
}

// CreateUDPResource creates and registers a UDP resource
func (rm *ResourceManager) CreateUDPResource(id string, port int) (*UDPResource, error) {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	resource := NewUDPResource(id, conn, addr, port)
	if err := rm.Register(resource); err != nil {
		conn.Close()
		return nil, err
	}

	return resource, nil
}

// CreateGoroutineResource creates and registers a goroutine resource
func (rm *ResourceManager) CreateGoroutineResource(id string, fn func(context.Context)) *GoroutineResource {
	ctx, cancel := context.WithCancel(rm.ctx)
	resource := NewGoroutineResource(id, cancel)

	_ = rm.Register(resource)

	go func() {
		defer resource.Done()
		fn(ctx)
	}()

	return resource
}

// ResourceStats contains resource statistics
type ResourceStats struct {
	Total    int            `json:"total"`
	Active   int            `json:"active"`
	Inactive int            `json:"inactive"`
	ByType   map[string]int `json:"by_type"`
}

// Global resource manager
var globalResourceManager *ResourceManager
var globalResourceManagerOnce sync.Once

// GetGlobalResourceManager returns the global resource manager
func GetGlobalResourceManager(logger *logrus.Logger) *ResourceManager {
	globalResourceManagerOnce.Do(func() {
		globalResourceManager = NewResourceManager(logger)
	})
	return globalResourceManager
}