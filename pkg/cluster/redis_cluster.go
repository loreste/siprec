package cluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// RedisMode defines the Redis deployment mode
type RedisMode string

const (
	// RedisModeStandalone is a single Redis instance
	RedisModeStandalone RedisMode = "standalone"
	// RedisModeSentinel uses Redis Sentinel for HA
	RedisModeSentinel RedisMode = "sentinel"
	// RedisModeCluster uses Redis Cluster for HA and sharding
	RedisModeCluster RedisMode = "cluster"
)

// RedisClusterConfig holds configuration for Redis cluster/sentinel
type RedisClusterConfig struct {
	// Mode specifies the Redis deployment mode
	Mode RedisMode `json:"mode" env:"REDIS_MODE" default:"standalone"`

	// Standalone configuration
	Address  string `json:"address" env:"REDIS_ADDRESS" default:"localhost:6379"`
	Password string `json:"password" env:"REDIS_PASSWORD"`
	Database int    `json:"database" env:"REDIS_DATABASE" default:"0"`

	// Sentinel configuration
	SentinelAddresses  []string `json:"sentinel_addresses" env:"REDIS_SENTINEL_ADDRESSES"`
	SentinelMasterName string   `json:"sentinel_master_name" env:"REDIS_SENTINEL_MASTER" default:"mymaster"`
	SentinelPassword   string   `json:"sentinel_password" env:"REDIS_SENTINEL_PASSWORD"`

	// Cluster configuration
	ClusterAddresses []string `json:"cluster_addresses" env:"REDIS_CLUSTER_ADDRESSES"`

	// Common configuration
	PoolSize        int           `json:"pool_size" env:"REDIS_POOL_SIZE" default:"20"`
	MinIdleConns    int           `json:"min_idle_conns" env:"REDIS_MIN_IDLE_CONNS" default:"5"`
	DialTimeout     time.Duration `json:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" default:"5s"`
	ReadTimeout     time.Duration `json:"read_timeout" env:"REDIS_READ_TIMEOUT" default:"3s"`
	WriteTimeout    time.Duration `json:"write_timeout" env:"REDIS_WRITE_TIMEOUT" default:"3s"`
	PoolTimeout     time.Duration `json:"pool_timeout" env:"REDIS_POOL_TIMEOUT" default:"4s"`
	MaxRetries      int           `json:"max_retries" env:"REDIS_MAX_RETRIES" default:"3"`
	MinRetryBackoff time.Duration `json:"min_retry_backoff" env:"REDIS_MIN_RETRY_BACKOFF" default:"8ms"`
	MaxRetryBackoff time.Duration `json:"max_retry_backoff" env:"REDIS_MAX_RETRY_BACKOFF" default:"512ms"`

	// TLS configuration
	TLSEnabled            bool   `json:"tls_enabled" env:"REDIS_TLS_ENABLED" default:"false"`
	TLSCertFile           string `json:"tls_cert_file" env:"REDIS_TLS_CERT_FILE"`
	TLSKeyFile            string `json:"tls_key_file" env:"REDIS_TLS_KEY_FILE"`
	TLSCAFile             string `json:"tls_ca_file" env:"REDIS_TLS_CA_FILE"`
	TLSInsecureSkipVerify bool   `json:"tls_insecure_skip_verify" env:"REDIS_TLS_INSECURE_SKIP_VERIFY" default:"false"`

	// Failover configuration
	RouteByLatency bool `json:"route_by_latency" env:"REDIS_ROUTE_BY_LATENCY" default:"true"`
	RouteRandomly  bool `json:"route_randomly" env:"REDIS_ROUTE_RANDOMLY" default:"false"`
}

// RedisClusterClient wraps a Redis client with cluster awareness
type RedisClusterClient struct {
	client redis.UniversalClient
	config RedisClusterConfig
	logger *logrus.Logger
	mode   RedisMode
}

// NewRedisClusterClient creates a new Redis client based on configuration
func NewRedisClusterClient(config RedisClusterConfig, logger *logrus.Logger) (*RedisClusterClient, error) {
	var client redis.UniversalClient
	var mode RedisMode

	// Build TLS config if enabled
	var tlsConfig *tls.Config
	if config.TLSEnabled {
		tlsConfig = &tls.Config{
			InsecureSkipVerify: config.TLSInsecureSkipVerify,
		}
	}

	switch config.Mode {
	case RedisModeSentinel:
		if len(config.SentinelAddresses) == 0 {
			return nil, fmt.Errorf("sentinel mode requires sentinel_addresses")
		}
		mode = RedisModeSentinel
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       config.SentinelMasterName,
			SentinelAddrs:    config.SentinelAddresses,
			SentinelPassword: config.SentinelPassword,
			Password:         config.Password,
			DB:               config.Database,
			PoolSize:         config.PoolSize,
			MinIdleConns:     config.MinIdleConns,
			DialTimeout:      config.DialTimeout,
			ReadTimeout:      config.ReadTimeout,
			WriteTimeout:     config.WriteTimeout,
			PoolTimeout:      config.PoolTimeout,
			MaxRetries:       config.MaxRetries,
			MinRetryBackoff:  config.MinRetryBackoff,
			MaxRetryBackoff:  config.MaxRetryBackoff,
			TLSConfig:        tlsConfig,
			RouteByLatency:   config.RouteByLatency,
			RouteRandomly:    config.RouteRandomly,
		})
		logger.WithFields(logrus.Fields{
			"sentinels":     config.SentinelAddresses,
			"master":        config.SentinelMasterName,
			"pool_size":     config.PoolSize,
			"route_latency": config.RouteByLatency,
		}).Info("Redis Sentinel client initialized")

	case RedisModeCluster:
		if len(config.ClusterAddresses) == 0 {
			return nil, fmt.Errorf("cluster mode requires cluster_addresses")
		}
		mode = RedisModeCluster
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:           config.ClusterAddresses,
			Password:        config.Password,
			PoolSize:        config.PoolSize,
			MinIdleConns:    config.MinIdleConns,
			DialTimeout:     config.DialTimeout,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
			PoolTimeout:     config.PoolTimeout,
			MaxRetries:      config.MaxRetries,
			MinRetryBackoff: config.MinRetryBackoff,
			MaxRetryBackoff: config.MaxRetryBackoff,
			TLSConfig:       tlsConfig,
			RouteByLatency:  config.RouteByLatency,
			RouteRandomly:   config.RouteRandomly,
		})
		logger.WithFields(logrus.Fields{
			"nodes":         config.ClusterAddresses,
			"pool_size":     config.PoolSize,
			"route_latency": config.RouteByLatency,
		}).Info("Redis Cluster client initialized")

	default:
		// Standalone mode
		mode = RedisModeStandalone
		client = redis.NewClient(&redis.Options{
			Addr:            config.Address,
			Password:        config.Password,
			DB:              config.Database,
			PoolSize:        config.PoolSize,
			MinIdleConns:    config.MinIdleConns,
			DialTimeout:     config.DialTimeout,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
			PoolTimeout:     config.PoolTimeout,
			MaxRetries:      config.MaxRetries,
			MinRetryBackoff: config.MinRetryBackoff,
			MaxRetryBackoff: config.MaxRetryBackoff,
			TLSConfig:       tlsConfig,
		})
		logger.WithFields(logrus.Fields{
			"address":   config.Address,
			"database":  config.Database,
			"pool_size": config.PoolSize,
		}).Info("Redis standalone client initialized")
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), config.DialTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis (%s mode): %w", mode, err)
	}

	return &RedisClusterClient{
		client: client,
		config: config,
		logger: logger,
		mode:   mode,
	}, nil
}

// Client returns the underlying Redis client
func (r *RedisClusterClient) Client() redis.UniversalClient {
	return r.client
}
