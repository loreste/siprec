package sip

import (
	"sync"
	"testing"
)

func TestShardedMap_StoreAndLoad(t *testing.T) {
	// Create a sharded map with 16 shards
	sm := NewShardedMap(16)

	// Test storing and loading values
	sm.Store("key1", "value1")
	sm.Store("key2", "value2")

	// Test loading values
	value1, ok := sm.Load("key1")
	if !ok || value1 != "value1" {
		t.Errorf("Expected value1, got %v, ok=%v", value1, ok)
	}

	value2, ok := sm.Load("key2")
	if !ok || value2 != "value2" {
		t.Errorf("Expected value2, got %v, ok=%v", value2, ok)
	}

	// Test loading a non-existent key
	value3, ok := sm.Load("key3")
	if ok || value3 != nil {
		t.Errorf("Expected nil and false for non-existent key, got %v, ok=%v", value3, ok)
	}
}

func TestShardedMap_Delete(t *testing.T) {
	// Create a sharded map with 16 shards
	sm := NewShardedMap(16)

	// Store some values
	sm.Store("key1", "value1")
	sm.Store("key2", "value2")

	// Delete a key
	sm.Delete("key1")

	// Verify the key is deleted
	value, ok := sm.Load("key1")
	if ok || value != nil {
		t.Errorf("Expected key to be deleted, got %v, ok=%v", value, ok)
	}

	// Verify other keys are still there
	value, ok = sm.Load("key2")
	if !ok || value != "value2" {
		t.Errorf("Expected value2, got %v, ok=%v", value, ok)
	}
}

func TestShardedMap_Range(t *testing.T) {
	// Create a sharded map with 16 shards
	sm := NewShardedMap(16)

	// Store some values
	expected := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range expected {
		sm.Store(k, v)
	}

	// Count items using Range
	count := 0
	items := make(map[string]string)

	sm.Range(func(key, value interface{}) bool {
		k := key.(string)
		v := value.(string)
		items[k] = v
		count++
		return true
	})

	// Verify count and items
	if count != len(expected) {
		t.Errorf("Expected %d items, got %d", len(expected), count)
	}

	for k, v := range expected {
		if items[k] != v {
			t.Errorf("Expected %s for key %s, got %s", v, k, items[k])
		}
	}

	// Test early termination
	earlyCount := 0
	sm.Range(func(key, value interface{}) bool {
		earlyCount++
		return earlyCount < 2 // Stop after the first item
	})

	if earlyCount != 2 {
		t.Errorf("Expected Range to stop after 2 items, processed %d", earlyCount)
	}
}

func TestShardedMap_Count(t *testing.T) {
	// Create a sharded map with 16 shards
	sm := NewShardedMap(16)

	// Store some values
	sm.Store("key1", "value1")
	sm.Store("key2", "value2")
	sm.Store("key3", "value3")

	// Check count
	if count := sm.Count(); count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}

	// Delete a key and check count again
	sm.Delete("key2")
	if count := sm.Count(); count != 2 {
		t.Errorf("Expected count 2 after deletion, got %d", count)
	}
}

func BenchmarkShardedMap_Concurrent(b *testing.B) {
	// Run multiple benchmarks with different shard counts
	for _, shardCount := range []int{1, 4, 16, 32, 64} {
		b.Run("ShardCount_"+string(rune(shardCount)), func(b *testing.B) {
			sm := NewShardedMap(shardCount)
			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				// Each goroutine gets a unique counter
				counter := 0
				for pb.Next() {
					key := "key" + string(rune(counter%100))
					// 75% reads, 20% writes, 5% deletes to simulate real workload
					switch counter % 100 {
					case 0, 1, 2, 3, 4:
						// Delete
						sm.Delete(key)
					case 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24:
						// Write
						sm.Store(key, counter)
					default:
						// Read
						sm.Load(key)
					}
					counter++
				}
			})
		})
	}
}

func BenchmarkSyncMap_Concurrent(b *testing.B) {
	// Benchmark standard sync.Map for comparison
	var sm sync.Map
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine gets a unique counter
		counter := 0
		for pb.Next() {
			key := "key" + string(rune(counter%100))
			// 75% reads, 20% writes, 5% deletes to simulate real workload
			switch counter % 100 {
			case 0, 1, 2, 3, 4:
				// Delete
				sm.Delete(key)
			case 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24:
				// Write
				sm.Store(key, counter)
			default:
				// Read
				sm.Load(key)
			}
			counter++
		}
	})
}
