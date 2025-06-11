package clustering

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/circuitbreaker"
)

// RedisCircuitBreakerWrapper wraps Redis client with circuit breaker protection
type RedisCircuitBreakerWrapper struct {
	client         *redis.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	logger         *logrus.Entry
	name           string
	
	// Fallback cache (in-memory)
	fallbackCache  map[string]interface{}
	fallbackTTL    map[string]time.Time
	cacheMutex     sync.RWMutex
	
	// Fallback metrics
	fallbackMetrics *RedisFallbackMetrics
}

// RedisFallbackMetrics tracks fallback statistics
type RedisFallbackMetrics struct {
	FallbackHits        int64     `json:"fallback_hits"`
	FallbackMisses      int64     `json:"fallback_misses"`
	FallbackStores      int64     `json:"fallback_stores"`
	CacheEvictions      int64     `json:"cache_evictions"`
	LastFallbackTime    time.Time `json:"last_fallback_time"`
}

// NewRedisCircuitBreakerWrapper creates a new Redis circuit breaker wrapper
func NewRedisCircuitBreakerWrapper(client *redis.Client, cbManager *circuitbreaker.Manager, logger *logrus.Logger) *RedisCircuitBreakerWrapper {
	name := "redis_clustering"
	
	// Get circuit breaker with Redis-optimized config
	cb := cbManager.GetCircuitBreaker(name, circuitbreaker.RedisConfig())
	
	wrapper := &RedisCircuitBreakerWrapper{
		client:         client,
		circuitBreaker: cb,
		logger: logger.WithFields(logrus.Fields{
			"component":    "redis_circuit_breaker",
			"circuit_name": name,
		}),
		name:            name,
		fallbackCache:   make(map[string]interface{}),
		fallbackTTL:     make(map[string]time.Time),
		fallbackMetrics: &RedisFallbackMetrics{},
	}
	
	// Start cleanup goroutine for fallback cache
	go wrapper.cleanupFallbackCache()
	
	return wrapper
}

// Set sets a key-value with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return w.circuitBreaker.ExecuteWithFallback(ctx,
		// Primary function
		func(ctx context.Context) error {
			return w.client.Set(ctx, key, value, expiration).Err()
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.FallbackStores++
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			// Store in fallback cache
			w.cacheMutex.Lock()
			defer w.cacheMutex.Unlock()
			
			w.fallbackCache[key] = value
			if expiration > 0 {
				w.fallbackTTL[key] = time.Now().Add(expiration)
			}
			
			w.logger.WithFields(logrus.Fields{
				"key":        key,
				"expiration": expiration,
			}).Debug("Stored in fallback cache due to Redis circuit breaker")
			
			return nil
		},
	)
}

// Get gets a value by key with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Get(ctx context.Context, key string) (string, error) {
	var result string
	var err error
	
	err = w.circuitBreaker.ExecuteWithFallback(ctx,
		// Primary function
		func(ctx context.Context) error {
			result, err = w.client.Get(ctx, key).Result()
			return err
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			// Check fallback cache
			w.cacheMutex.RLock()
			defer w.cacheMutex.RUnlock()
			
			if value, exists := w.fallbackCache[key]; exists {
				// Check TTL
				if ttl, hasTTL := w.fallbackTTL[key]; hasTTL {
					if time.Now().After(ttl) {
						// Expired
						delete(w.fallbackCache, key)
						delete(w.fallbackTTL, key)
						w.fallbackMetrics.FallbackMisses++
						return redis.Nil
					}
				}
				
				// Convert to string
				if str, ok := value.(string); ok {
					result = str
				} else {
					// Try JSON marshaling for non-string values
					if jsonBytes, jsonErr := json.Marshal(value); jsonErr == nil {
						result = string(jsonBytes)
					} else {
						result = fmt.Sprintf("%v", value)
					}
				}
				
				w.fallbackMetrics.FallbackHits++
				w.logger.WithField("key", key).Debug("Fallback cache hit")
				return nil
			}
			
			w.fallbackMetrics.FallbackMisses++
			return redis.Nil
		},
	)
	
	return result, err
}

// Del deletes a key with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Del(ctx context.Context, keys ...string) (int64, error) {
	var result int64
	var err error
	
	err = w.circuitBreaker.ExecuteWithFallback(ctx,
		// Primary function
		func(ctx context.Context) error {
			result, err = w.client.Del(ctx, keys...).Result()
			return err
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			// Delete from fallback cache
			w.cacheMutex.Lock()
			defer w.cacheMutex.Unlock()
			
			deleted := int64(0)
			for _, key := range keys {
				if _, exists := w.fallbackCache[key]; exists {
					delete(w.fallbackCache, key)
					delete(w.fallbackTTL, key)
					deleted++
				}
			}
			
			result = deleted
			w.logger.WithFields(logrus.Fields{
				"keys":    keys,
				"deleted": deleted,
			}).Debug("Deleted from fallback cache")
			
			return nil
		},
	)
	
	return result, err
}

// Exists checks if keys exist with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Exists(ctx context.Context, keys ...string) (int64, error) {
	var result int64
	var err error
	
	err = w.circuitBreaker.ExecuteWithFallback(ctx,
		// Primary function
		func(ctx context.Context) error {
			result, err = w.client.Exists(ctx, keys...).Result()
			return err
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			// Check fallback cache
			w.cacheMutex.RLock()
			defer w.cacheMutex.RUnlock()
			
			count := int64(0)
			for _, key := range keys {
				if _, exists := w.fallbackCache[key]; exists {
					// Check TTL
					if ttl, hasTTL := w.fallbackTTL[key]; hasTTL {
						if time.Now().After(ttl) {
							continue // Expired
						}
					}
					count++
				}
			}
			
			result = count
			return nil
		},
	)
	
	return result, err
}

// Expire sets expiration with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Expire(ctx context.Context, key string, expiration time.Duration) (bool, error) {
	var result bool
	var err error
	
	err = w.circuitBreaker.ExecuteWithFallback(ctx,
		// Primary function
		func(ctx context.Context) error {
			result, err = w.client.Expire(ctx, key, expiration).Result()
			return err
		},
		// Fallback function
		func(ctx context.Context) error {
			w.fallbackMetrics.LastFallbackTime = time.Now()
			
			// Set TTL in fallback cache
			w.cacheMutex.Lock()
			defer w.cacheMutex.Unlock()
			
			if _, exists := w.fallbackCache[key]; exists {
				w.fallbackTTL[key] = time.Now().Add(expiration)
				result = true
			} else {
				result = false
			}
			
			return nil
		},
	)
	
	return result, err
}

// Ping pings Redis with circuit breaker protection
func (w *RedisCircuitBreakerWrapper) Ping(ctx context.Context) error {
	return w.circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		return w.client.Ping(ctx).Err()
	})
}

// cleanupFallbackCache periodically cleans up expired entries
func (w *RedisCircuitBreakerWrapper) cleanupFallbackCache() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		w.cacheMutex.Lock()
		now := time.Now()
		evicted := 0
		
		for key, ttl := range w.fallbackTTL {
			if now.After(ttl) {
				delete(w.fallbackCache, key)
				delete(w.fallbackTTL, key)
				evicted++
			}
		}
		
		if evicted > 0 {
			w.fallbackMetrics.CacheEvictions += int64(evicted)
			w.logger.WithField("evicted", evicted).Debug("Cleaned up expired fallback cache entries")
		}
		
		w.cacheMutex.Unlock()
	}
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (w *RedisCircuitBreakerWrapper) GetCircuitBreakerStats() *circuitbreaker.Statistics {
	return w.circuitBreaker.GetStatistics()
}

// GetFallbackMetrics returns fallback metrics
func (w *RedisCircuitBreakerWrapper) GetFallbackMetrics() *RedisFallbackMetrics {
	return w.fallbackMetrics
}

// GetFallbackCacheSize returns the current size of fallback cache
func (w *RedisCircuitBreakerWrapper) GetFallbackCacheSize() int {
	w.cacheMutex.RLock()
	defer w.cacheMutex.RUnlock()
	return len(w.fallbackCache)
}

// ResetCircuitBreaker resets the circuit breaker
func (w *RedisCircuitBreakerWrapper) ResetCircuitBreaker() {
	w.circuitBreaker.Reset()
	w.logger.Info("Redis circuit breaker reset")
}