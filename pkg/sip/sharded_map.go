package sip

import (
	"hash/fnv"
	"sync"
)

// ShardedMap provides a map with multiple shards to reduce lock contention
// for concurrent access patterns. It's a drop-in replacement for sync.Map
// with better performance under high concurrency.
type ShardedMap struct {
	shards     []*mapShard
	shardMask  uint32
	shardCount uint32
}

// mapShard represents a single shard in the sharded map
type mapShard struct {
	items map[string]interface{}
	mu    sync.RWMutex
}

// NewShardedMap creates a new sharded map with the specified number of shards
// shardCount must be a power of two for efficient shard selection
func NewShardedMap(shardCount int) *ShardedMap {
	// Ensure shard count is a power of 2 for efficient masking
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		// Default to 16 shards if invalid count provided
		shardCount = 16
	}

	// Initialize the sharded map
	sm := &ShardedMap{
		shards:     make([]*mapShard, shardCount),
		shardMask:  uint32(shardCount - 1),
		shardCount: uint32(shardCount),
	}

	// Initialize each shard
	for i := 0; i < shardCount; i++ {
		sm.shards[i] = &mapShard{
			items: make(map[string]interface{}),
		}
	}

	return sm
}

// getShard returns the appropriate shard for a given key
func (sm *ShardedMap) getShard(key string) *mapShard {
	// Hash the key to determine which shard to use
	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()

	// Use the hash to select a shard via masking (fast modulo for powers of 2)
	return sm.shards[hash&sm.shardMask]
}

// Store adds or updates a key-value pair in the map
func (sm *ShardedMap) Store(key, value interface{}) {
	// Type assertion for key
	keyStr, ok := key.(string)
	if !ok {
		// If not a string, convert it to string
		keyStr = toString(key)
	}

	// Get the appropriate shard
	shard := sm.getShard(keyStr)

	// Lock the shard for writing
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Store the value
	shard.items[keyStr] = value
}

// Load retrieves a value from the map
func (sm *ShardedMap) Load(key interface{}) (value interface{}, ok bool) {
	// Type assertion for key
	keyStr, ok := key.(string)
	if !ok {
		// If not a string, convert it to string
		keyStr = toString(key)
	}

	// Get the appropriate shard
	shard := sm.getShard(keyStr)

	// Lock the shard for reading
	shard.mu.RLock()
	defer shard.mu.RUnlock()

	// Retrieve the value
	value, ok = shard.items[keyStr]
	return
}

// Delete removes a key-value pair from the map
func (sm *ShardedMap) Delete(key interface{}) {
	// Type assertion for key
	keyStr, ok := key.(string)
	if !ok {
		// If not a string, convert it to string
		keyStr = toString(key)
	}

	// Get the appropriate shard
	shard := sm.getShard(keyStr)

	// Lock the shard for writing
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Delete the key
	delete(shard.items, keyStr)
}

// Range iterates over all key-value pairs in the map
// The provided function is called for each key-value pair
// If the function returns false, iteration stops
func (sm *ShardedMap) Range(f func(key, value interface{}) bool) {
	// Process each shard sequentially
	for _, shard := range sm.shards {
		// Lock the shard for reading
		shard.mu.RLock()

		// Iterate over all items in the shard
		for k, v := range shard.items {
			// Call the function with key and value
			if !f(k, v) {
				// If the function returns false, release the lock and stop iteration
				shard.mu.RUnlock()
				return
			}
		}

		// Release the lock for this shard
		shard.mu.RUnlock()
	}
}

// Count returns the total number of items across all shards
func (sm *ShardedMap) Count() int {
	count := 0

	// Process each shard sequentially
	for _, shard := range sm.shards {
		// Lock the shard for reading
		shard.mu.RLock()

		// Add the number of items in this shard
		count += len(shard.items)

		// Release the lock for this shard
		shard.mu.RUnlock()
	}

	return count
}

// toString converts an interface to a string
// This is used to handle non-string keys
func toString(key interface{}) string {
	// Use type assertion for common types
	switch v := key.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		// Fallback to string representation
		return key.(string)
	}
}
