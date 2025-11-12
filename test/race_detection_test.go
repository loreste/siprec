package test

import (
	"sync"
	"testing"
	"time"

	"siprec-server/pkg/sip"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// TestCallDataRaceConditions tests for race conditions in CallData
func TestCallDataRaceConditions(t *testing.T) {
	t.Run("concurrent_activity_updates", func(t *testing.T) {
		callData := &sip.CallData{
			LastActivity:  time.Now(),
			RemoteAddress: "192.168.1.100:5060",
		}

		var wg sync.WaitGroup
		numGoroutines := 100

		// Start goroutines that update activity
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					callData.UpdateActivity()
				}
			}()
		}

		// Start goroutines that check staleness
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					_ = callData.IsStale(30 * time.Second)
				}
			}()
		}

		wg.Wait()
	})

	t.Run("concurrent_safe_copy", func(t *testing.T) {
		callData := &sip.CallData{
			LastActivity:  time.Now(),
			RemoteAddress: "192.168.1.100:5060",
		}

		var wg sync.WaitGroup
		numGoroutines := 50

		// Concurrent updates and copies
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					// Update
					callData.UpdateActivity()

					// Copy
					copy := callData.SafeCopy()
					require.NotNil(t, copy)
					require.False(t, copy.LastActivity.IsZero())

					// Small delay
					time.Sleep(time.Microsecond)
				}
			}()
		}

		wg.Wait()
	})
}

// TestShardedMapRaceConditions tests the sharded map for race conditions
func TestShardedMapRaceConditions(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	shardedMap := sip.NewShardedMap(32)

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 1000

	// Test concurrent Store, Load, Delete operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				key := "key" + string(rune((id*numOperations+j)%256))

				// Store
				shardedMap.Store(key, &sip.CallData{
					LastActivity:  time.Now(),
					RemoteAddress: "192.168.1.100:5060",
				})

				// Load
				if val, ok := shardedMap.Load(key); ok {
					callData := val.(*sip.CallData)
					callData.UpdateActivity()
				}

				// Delete (50% chance)
				if j%2 == 0 {
					shardedMap.Delete(key)
				}
			}
		}(i)
	}

	// Test concurrent Range operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				count := 0
				shardedMap.Range(func(key, value interface{}) bool {
					count++
					// Simulate some work
					time.Sleep(time.Microsecond)
					return true
				})

				// Add some delay between ranges
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()
}

// TestMemorySessionStoreRaceConditions tests the session store for race conditions
func TestMemorySessionStoreRaceConditions(t *testing.T) {
	store := sip.NewMemorySessionStore()

	var wg sync.WaitGroup
	numGoroutines := 50
	numSessions := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numSessions; j++ {
				key := "session" + string(rune((id*numSessions+j)%256))

				// Create call data that will be modified concurrently
				callData := &sip.CallData{
					LastActivity:  time.Now(),
					RemoteAddress: "192.168.1.100:5060",
				}

				// Save (with potential concurrent modifications)
				go func() {
					for k := 0; k < 10; k++ {
						callData.UpdateActivity()
						time.Sleep(time.Microsecond)
					}
				}()

				err := store.Save(key, callData)
				require.NoError(t, err)

				// Load
				loaded, err := store.Load(key)
				if err == nil {
					require.NotNil(t, loaded)
					loaded.UpdateActivity()
				}

				// Delete
				err = store.Delete(key)
				require.NoError(t, err)
			}
		}(i)
	}

	wg.Wait()
}
