package util

import (
	"container/list"
	"sync"
	"time"
)

// CacheItem represents an item in the cache
type CacheItem struct {
	Key        string
	Value      interface{}
	Expiration time.Time
	element    *list.Element
}

// LRUCache implements a thread-safe LRU cache with TTL support
type LRUCache struct {
	mu          sync.RWMutex
	items       map[string]*CacheItem
	lruList     *list.List
	maxSize     int
	defaultTTL  time.Duration
	cleanupDone chan struct{}
}

// NewLRUCache creates a new LRU cache
func NewLRUCache(maxSize int, defaultTTL time.Duration) *LRUCache {
	cache := &LRUCache{
		items:       make(map[string]*CacheItem),
		lruList:     list.New(),
		maxSize:     maxSize,
		defaultTTL:  defaultTTL,
		cleanupDone: make(chan struct{}),
	}
	
	// Start cleanup goroutine
	go cache.cleanup()
	
	return cache
}

// Get retrieves an item from the cache
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	item, exists := c.items[key]
	if !exists {
		return nil, false
	}
	
	// Check expiration
	if time.Now().After(item.Expiration) {
		c.removeItem(item)
		return nil, false
	}
	
	// Move to front (most recently used)
	c.lruList.MoveToFront(item.element)
	
	return item.Value, true
}

// Set adds or updates an item in the cache
func (c *LRUCache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL adds or updates an item with custom TTL
func (c *LRUCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	expiration := now.Add(ttl)
	
	// Check if item already exists
	if existing, exists := c.items[key]; exists {
		// Update existing item
		existing.Value = value
		existing.Expiration = expiration
		c.lruList.MoveToFront(existing.element)
		return
	}
	
	// Create new item
	item := &CacheItem{
		Key:        key,
		Value:      value,
		Expiration: expiration,
	}
	
	// Add to front of LRU list
	item.element = c.lruList.PushFront(item)
	c.items[key] = item
	
	// Check if we need to evict items
	c.evictIfNeeded()
}

// Delete removes an item from the cache
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if item, exists := c.items[key]; exists {
		c.removeItem(item)
	}
}

// Clear removes all items from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items = make(map[string]*CacheItem)
	c.lruList.Init()
}

// Size returns the current number of items in the cache
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	return len(c.items)
}

// Keys returns all keys in the cache
func (c *LRUCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	keys := make([]string, 0, len(c.items))
	for key := range c.items {
		keys = append(keys, key)
	}
	return keys
}

// removeItem removes an item from the cache (assumes lock is held)
func (c *LRUCache) removeItem(item *CacheItem) {
	delete(c.items, item.Key)
	c.lruList.Remove(item.element)
}

// evictIfNeeded removes items if cache is over capacity (assumes lock is held)
func (c *LRUCache) evictIfNeeded() {
	for len(c.items) > c.maxSize {
		// Remove least recently used item
		oldest := c.lruList.Back()
		if oldest != nil {
			item := oldest.Value.(*CacheItem)
			c.removeItem(item)
		}
	}
}

// cleanup periodically removes expired items
func (c *LRUCache) cleanup() {
	ticker := time.NewTicker(time.Minute) // Cleanup every minute
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			c.removeExpired()
		case <-c.cleanupDone:
			return
		}
	}
}

// removeExpired removes all expired items from the cache
func (c *LRUCache) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	now := time.Now()
	var toRemove []*CacheItem
	
	// Collect expired items
	for _, item := range c.items {
		if now.After(item.Expiration) {
			toRemove = append(toRemove, item)
		}
	}
	
	// Remove expired items
	for _, item := range toRemove {
		c.removeItem(item)
	}
}

// Close stops the cleanup goroutine
func (c *LRUCache) Close() {
	close(c.cleanupDone)
}

// CacheStats provides cache performance statistics
type CacheStats struct {
	Size        int
	MaxSize     int
	HitRate     float64
	TotalHits   int64
	TotalMisses int64
}

// StatsCache wraps an LRU cache with statistics tracking
type StatsCache struct {
	cache      *LRUCache
	hits       int64
	misses     int64
	statsMutex sync.RWMutex
}

// NewStatsCache creates a new cache with statistics tracking
func NewStatsCache(maxSize int, defaultTTL time.Duration) *StatsCache {
	return &StatsCache{
		cache: NewLRUCache(maxSize, defaultTTL),
	}
}

// Get retrieves an item and updates statistics
func (sc *StatsCache) Get(key string) (interface{}, bool) {
	value, found := sc.cache.Get(key)
	
	sc.statsMutex.Lock()
	if found {
		sc.hits++
	} else {
		sc.misses++
	}
	sc.statsMutex.Unlock()
	
	return value, found
}

// Set adds an item to the cache
func (sc *StatsCache) Set(key string, value interface{}) {
	sc.cache.Set(key, value)
}

// SetWithTTL adds an item with custom TTL
func (sc *StatsCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	sc.cache.SetWithTTL(key, value, ttl)
}

// Delete removes an item from the cache
func (sc *StatsCache) Delete(key string) {
	sc.cache.Delete(key)
}

// GetStats returns cache statistics
func (sc *StatsCache) GetStats() CacheStats {
	sc.statsMutex.RLock()
	defer sc.statsMutex.RUnlock()
	
	total := sc.hits + sc.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(sc.hits) / float64(total)
	}
	
	return CacheStats{
		Size:        sc.cache.Size(),
		MaxSize:     sc.cache.maxSize,
		HitRate:     hitRate,
		TotalHits:   sc.hits,
		TotalMisses: sc.misses,
	}
}

// Clear removes all items and resets statistics
func (sc *StatsCache) Clear() {
	sc.cache.Clear()
	sc.statsMutex.Lock()
	sc.hits = 0
	sc.misses = 0
	sc.statsMutex.Unlock()
}

// Close stops the underlying cache
func (sc *StatsCache) Close() {
	sc.cache.Close()
}

// Global session cache instance
var globalSessionCache = NewStatsCache(10000, 30*time.Minute)

// GetGlobalSessionCache returns the global session cache
func GetGlobalSessionCache() *StatsCache {
	return globalSessionCache
}