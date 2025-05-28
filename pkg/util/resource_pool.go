package util

import (
	"runtime"
	"sync"
	"time"
)

// ResourcePool manages a pool of reusable resources to reduce GC pressure
type ResourcePool struct {
	pools map[string]*sync.Pool
	mu    sync.RWMutex
}

// NewResourcePool creates a new resource pool
func NewResourcePool() *ResourcePool {
	return &ResourcePool{
		pools: make(map[string]*sync.Pool),
	}
}

// GetBufferPool returns a buffer pool for the specified size
func (rp *ResourcePool) GetBufferPool(size int) *sync.Pool {
	key := bufferPoolKey(size)

	rp.mu.RLock()
	pool, exists := rp.pools[key]
	rp.mu.RUnlock()

	if exists {
		return pool
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := rp.pools[key]; exists {
		return pool
	}

	// Create new pool
	pool = &sync.Pool{
		New: func() interface{} {
			return make([]byte, size)
		},
	}
	rp.pools[key] = pool
	return pool
}

// GetBuffer gets a buffer from the pool
func (rp *ResourcePool) GetBuffer(size int) []byte {
	pool := rp.GetBufferPool(size)
	return pool.Get().([]byte)
}

// PutBuffer returns a buffer to the pool
func (rp *ResourcePool) PutBuffer(buf []byte) {
	if len(buf) == 0 {
		return
	}

	size := cap(buf)
	pool := rp.GetBufferPool(size)

	// Reset buffer to ensure clean state
	buf = buf[:0]
	pool.Put(buf)
}

// GetStringBuilderPool returns a pool for string builders
func (rp *ResourcePool) GetStringBuilderPool() *sync.Pool {
	const key = "string_builder"

	rp.mu.RLock()
	pool, exists := rp.pools[key]
	rp.mu.RUnlock()

	if exists {
		return pool
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	// Double-check after acquiring write lock
	if pool, exists := rp.pools[key]; exists {
		return pool
	}

	// Create new pool
	pool = &sync.Pool{
		New: func() interface{} {
			return &StringBuilder{}
		},
	}
	rp.pools[key] = pool
	return pool
}

// StringBuilder is a reusable string builder
type StringBuilder struct {
	buf []byte
}

// WriteString appends a string to the builder
func (sb *StringBuilder) WriteString(s string) {
	sb.buf = append(sb.buf, s...)
}

// WriteByte appends a byte to the builder
func (sb *StringBuilder) WriteByte(b byte) error {
	sb.buf = append(sb.buf, b)
	return nil
}

// String returns the built string
func (sb *StringBuilder) String() string {
	return string(sb.buf)
}

// Reset clears the builder
func (sb *StringBuilder) Reset() {
	sb.buf = sb.buf[:0]
}

// GetStringBuilder gets a string builder from the pool
func (rp *ResourcePool) GetStringBuilder() *StringBuilder {
	pool := rp.GetStringBuilderPool()
	sb := pool.Get().(*StringBuilder)
	sb.Reset()
	return sb
}

// PutStringBuilder returns a string builder to the pool
func (rp *ResourcePool) PutStringBuilder(sb *StringBuilder) {
	if sb == nil {
		return
	}

	pool := rp.GetStringBuilderPool()
	sb.Reset()
	pool.Put(sb)
}

// MemoryStats provides memory usage statistics
type MemoryStats struct {
	AllocatedBytes   uint64
	TotalAllocations uint64
	SystemBytes      uint64
	GCCycles         uint32
	NextGCThreshold  uint64
	NumGoroutines    int
	LastGCTime       time.Time
}

// GetMemoryStats returns current memory statistics
func GetMemoryStats() *MemoryStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	return &MemoryStats{
		AllocatedBytes:   ms.Alloc,
		TotalAllocations: ms.TotalAlloc,
		SystemBytes:      ms.Sys,
		GCCycles:         ms.NumGC,
		NextGCThreshold:  ms.NextGC,
		NumGoroutines:    runtime.NumGoroutine(),
		LastGCTime:       time.Unix(0, int64(ms.LastGC)),
	}
}

// bufferPoolKey generates a key for buffer pools
func bufferPoolKey(size int) string {
	// Use a simple string conversion for key generation
	return "buffer_" + string(rune('0'+size/1000)) + string(rune('0'+(size%1000)/100)) + string(rune('0'+(size%100)/10)) + string(rune('0'+size%10))
}

// Global resource pool instance
var globalResourcePool = NewResourcePool()

// GetGlobalResourcePool returns the global resource pool
func GetGlobalResourcePool() *ResourcePool {
	return globalResourcePool
}
