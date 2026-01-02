package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// RedisSessionStore implements session storage using Redis
type RedisSessionStore struct {
	client    redis.UniversalClient
	logger    *logrus.Logger
	keyPrefix string
	ttl       time.Duration
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Address      string        `yaml:"address" env:"REDIS_ADDRESS" default:"localhost:6379"`
	Password     string        `yaml:"password" env:"REDIS_PASSWORD"`
	Database     int           `yaml:"database" env:"REDIS_DATABASE" default:"0"`
	PoolSize     int           `yaml:"pool_size" env:"REDIS_POOL_SIZE" default:"10"`
	DialTimeout  time.Duration `yaml:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" default:"5s"`
	ReadTimeout  time.Duration `yaml:"read_timeout" env:"REDIS_READ_TIMEOUT" default:"3s"`
	WriteTimeout time.Duration `yaml:"write_timeout" env:"REDIS_WRITE_TIMEOUT" default:"3s"`
	TTL          time.Duration `yaml:"ttl" env:"REDIS_SESSION_TTL" default:"24h"`
}

// SessionData represents session data stored in Redis
type SessionData struct {
	SessionID     string                 `json:"session_id"`
	CallID        string                 `json:"call_id"`
	Status        string                 `json:"status"`
	StartTime     time.Time              `json:"start_time"`
	LastUpdate    time.Time              `json:"last_update"`
	RecordingPath string                 `json:"recording_path"`
	Metadata      map[string]interface{} `json:"metadata"`
	NodeID        string                 `json:"node_id"`
}

// NewRedisSessionStore creates a new Redis session store
func NewRedisSessionStore(config RedisConfig, logger *logrus.Logger) (*RedisSessionStore, error) {
	// Configure Redis client
	opts := &redis.Options{
		Addr:         config.Address,
		Password:     config.Password,
		DB:           config.Database,
		PoolSize:     config.PoolSize,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
	}

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.DialTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	store := &RedisSessionStore{
		client:    client,
		logger:    logger,
		keyPrefix: "siprec:session:",
		ttl:       config.TTL,
	}

	logger.WithFields(logrus.Fields{
		"address":  config.Address,
		"database": config.Database,
		"ttl":      config.TTL,
	}).Info("Redis session store initialized")

	return store, nil
}

// GetClient returns the underlying Redis client
func (r *RedisSessionStore) GetClient() redis.UniversalClient {
	return r.client
}

// Store saves a session to Redis
func (r *RedisSessionStore) Store(sessionID string, data *SessionData) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Update last update time
	data.LastUpdate = time.Now()

	// Serialize session data
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	// Store in Redis with TTL
	key := r.sessionKey(sessionID)
	if err := r.client.Set(ctx, key, jsonData, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to store session in Redis: %w", err)
	}

	// Also add to session index for quick lookups
	if err := r.addToIndex(sessionID, data.CallID, data.NodeID); err != nil {
		r.logger.WithError(err).Warning("Failed to add session to index")
	}

	r.logger.WithFields(logrus.Fields{
		"session_id": sessionID,
		"call_id":    data.CallID,
		"node_id":    data.NodeID,
	}).Debug("Session stored in Redis")

	return nil
}

// Get retrieves a session from Redis
func (r *RedisSessionStore) Get(sessionID string) (*SessionData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	jsonData, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("failed to get session from Redis: %w", err)
	}

	var data SessionData
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &data, nil
}

// Update updates specific fields of a session
func (r *RedisSessionStore) Update(sessionID string, updates map[string]interface{}) error {
	// Get current session
	data, err := r.Get(sessionID)
	if err != nil {
		return err
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "status":
			if status, ok := value.(string); ok {
				data.Status = status
			}
		case "recording_path":
			if path, ok := value.(string); ok {
				data.RecordingPath = path
			}
		case "node_id":
			if nodeID, ok := value.(string); ok {
				data.NodeID = nodeID
			}
		default:
			// Store in metadata
			if data.Metadata == nil {
				data.Metadata = make(map[string]interface{})
			}
			data.Metadata[key] = value
		}
	}

	// Store updated session
	return r.Store(sessionID, data)
}

// Delete removes a session from Redis
func (r *RedisSessionStore) Delete(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete session from Redis: %w", err)
	}

	// Remove from index
	if err := r.removeFromIndex(sessionID); err != nil {
		r.logger.WithError(err).Warning("Failed to remove session from index")
	}

	r.logger.WithField("session_id", sessionID).Debug("Session deleted from Redis")
	return nil
}

// List returns all sessions (optionally filtered by node)
func (r *RedisSessionStore) List(nodeID string) ([]*SessionData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get all session keys
	pattern := r.keyPrefix + "*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list session keys: %w", err)
	}

	if len(keys) == 0 {
		return []*SessionData{}, nil
	}

	// Get all sessions in batch
	pipe := r.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to execute batch get: %w", err)
	}

	// Parse sessions
	var sessions []*SessionData
	for _, cmd := range cmds {
		jsonData, err := cmd.Result()
		if err != nil {
			continue // Skip failed sessions
		}

		var data SessionData
		if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
			r.logger.WithError(err).Warning("Failed to parse session from Redis")
			continue
		}

		// Filter by node if specified
		if nodeID != "" && data.NodeID != nodeID {
			continue
		}

		sessions = append(sessions, &data)
	}

	return sessions, nil
}

// Exists checks if a session exists
func (r *RedisSessionStore) Exists(sessionID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	count, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check session existence: %w", err)
	}

	return count > 0, nil
}

// Extend extends the TTL of a session
func (r *RedisSessionStore) Extend(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := r.sessionKey(sessionID)
	if err := r.client.Expire(ctx, key, r.ttl).Err(); err != nil {
		return fmt.Errorf("failed to extend session TTL: %w", err)
	}

	return nil
}

// ListByCallID finds sessions by call ID
func (r *RedisSessionStore) ListByCallID(callID string) ([]*SessionData, error) {
	sessions, err := r.List("")
	if err != nil {
		return nil, err
	}

	var filtered []*SessionData
	for _, session := range sessions {
		if session.CallID == callID {
			filtered = append(filtered, session)
		}
	}

	return filtered, nil
}

// GetStats returns Redis session store statistics
func (r *RedisSessionStore) GetStats() (*SessionStoreStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get total sessions
	pattern := r.keyPrefix + "*"
	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to count sessions: %w", err)
	}

	stats := &SessionStoreStats{
		TotalSessions: len(keys),
		StoreType:     "redis",
		LastChecked:   time.Now(),
	}

	// Get Redis info
	info, err := r.client.Info(ctx, "memory").Result()
	if err == nil {
		stats.RedisInfo = info
	}

	return stats, nil
}

// Cleanup removes expired sessions
func (r *RedisSessionStore) Cleanup() error {
	// Redis handles TTL automatically, but we can clean up orphaned index entries
	return r.cleanupIndex()
}

// Health check
func (r *RedisSessionStore) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return r.client.Ping(ctx).Err()
}

// Close closes the Redis connection
func (r *RedisSessionStore) Close() error {
	return r.client.Close()
}

// Private helper methods

func (r *RedisSessionStore) sessionKey(sessionID string) string {
	return r.keyPrefix + sessionID
}

func (r *RedisSessionStore) indexKey() string {
	return "siprec:index:sessions"
}

func (r *RedisSessionStore) nodeIndexKey(nodeID string) string {
	return "siprec:index:node:" + nodeID
}

func (r *RedisSessionStore) addToIndex(sessionID, callID, nodeID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pipe := r.client.Pipeline()

	// Add to main index
	indexData := map[string]interface{}{
		"call_id": callID,
		"node_id": nodeID,
		"created": time.Now().Unix(),
	}
	indexJSON, _ := json.Marshal(indexData)
	pipe.HSet(ctx, r.indexKey(), sessionID, indexJSON)

	// Add to node index
	pipe.SAdd(ctx, r.nodeIndexKey(nodeID), sessionID)

	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisSessionStore) removeFromIndex(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Get session info to find node
	indexData, err := r.client.HGet(ctx, r.indexKey(), sessionID).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	pipe := r.client.Pipeline()

	// Remove from main index
	pipe.HDel(ctx, r.indexKey(), sessionID)

	// Remove from node index if we have the data
	if err == nil {
		var data map[string]interface{}
		if json.Unmarshal([]byte(indexData), &data) == nil {
			if nodeID, ok := data["node_id"].(string); ok {
				pipe.SRem(ctx, r.nodeIndexKey(nodeID), sessionID)
			}
		}
	}

	_, err = pipe.Exec(ctx)
	return err
}

func (r *RedisSessionStore) cleanupIndex() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get all sessions in index
	indexData, err := r.client.HGetAll(ctx, r.indexKey()).Result()
	if err != nil {
		return err
	}

	var toRemove []string
	for sessionID := range indexData {
		// Check if session still exists
		exists, err := r.Exists(sessionID)
		if err != nil {
			continue
		}
		if !exists {
			toRemove = append(toRemove, sessionID)
		}
	}

	// Remove orphaned index entries
	if len(toRemove) > 0 {
		pipe := r.client.Pipeline()
		for _, sessionID := range toRemove {
			pipe.HDel(ctx, r.indexKey(), sessionID)
		}
		_, err = pipe.Exec(ctx)

		if err == nil && len(toRemove) > 0 {
			r.logger.WithField("count", len(toRemove)).Info("Cleaned up orphaned session index entries")
		}
	}

	return err
}

// SessionStoreStats contains session store statistics
type SessionStoreStats struct {
	TotalSessions int       `json:"total_sessions"`
	StoreType     string    `json:"store_type"`
	LastChecked   time.Time `json:"last_checked"`
	RedisInfo     string    `json:"redis_info,omitempty"`
}
