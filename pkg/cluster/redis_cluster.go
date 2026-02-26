package cluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
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
	PoolSize         int           `json:"pool_size" env:"REDIS_POOL_SIZE" default:"20"`
	MinIdleConns     int           `json:"min_idle_conns" env:"REDIS_MIN_IDLE_CONNS" default:"5"`
	DialTimeout      time.Duration `json:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" default:"5s"`
	ReadTimeout      time.Duration `json:"read_timeout" env:"REDIS_READ_TIMEOUT" default:"3s"`
	WriteTimeout     time.Duration `json:"write_timeout" env:"REDIS_WRITE_TIMEOUT" default:"3s"`
	PoolTimeout      time.Duration `json:"pool_timeout" env:"REDIS_POOL_TIMEOUT" default:"4s"`
	MaxRetries       int           `json:"max_retries" env:"REDIS_MAX_RETRIES" default:"3"`
	MinRetryBackoff  time.Duration `json:"min_retry_backoff" env:"REDIS_MIN_RETRY_BACKOFF" default:"8ms"`
	MaxRetryBackoff  time.Duration `json:"max_retry_backoff" env:"REDIS_MAX_RETRY_BACKOFF" default:"512ms"`

	// TLS configuration
	TLSEnabled         bool   `json:"tls_enabled" env:"REDIS_TLS_ENABLED" default:"false"`
	TLSCertFile        string `json:"tls_cert_file" env:"REDIS_TLS_CERT_FILE"`
	TLSKeyFile         string `json:"tls_key_file" env:"REDIS_TLS_KEY_FILE"`
	TLSCAFile          string `json:"tls_ca_file" env:"REDIS_TLS_CA_FILE"`
	TLSInsecureSkipVerify bool `json:"tls_insecure_skip_verify" env:"REDIS_TLS_INSECURE_SKIP_VERIFY" default:"false"`

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
			"sentinels":    config.SentinelAddresses,
			"master":       config.SentinelMasterName,
			"pool_size":    config.PoolSize,
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
			"nodes":        config.ClusterAddresses,
			"pool_size":    config.PoolSize,
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

// Mode returns the Redis deployment mode
func (r *RedisClusterClient) Mode() RedisMode {
	return r.mode
}

// Health checks Redis connectivity
func (r *RedisClusterClient) Health(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close closes the Redis connection
func (r *RedisClusterClient) Close() error {
	return r.client.Close()
}

// GetClusterInfo returns information about the Redis cluster/sentinel
func (r *RedisClusterClient) GetClusterInfo(ctx context.Context) (*RedisClusterInfo, error) {
	info := &RedisClusterInfo{
		Mode:      r.mode,
		Connected: true,
	}

	// Get basic info
	infoStr, err := r.client.Info(ctx, "server", "replication", "clients").Result()
	if err != nil {
		info.Connected = false
		return info, err
	}

	// Parse info
	info.parseInfo(infoStr)

	// Get cluster-specific info
	switch r.mode {
	case RedisModeCluster:
		if clusterClient, ok := r.client.(*redis.ClusterClient); ok {
			clusterInfo, err := clusterClient.ClusterInfo(ctx).Result()
			if err == nil {
				info.ClusterState = parseClusterState(clusterInfo)
			}

			// Get cluster nodes
			nodes, err := clusterClient.ClusterNodes(ctx).Result()
			if err == nil {
				info.ClusterNodes = parseClusterNodes(nodes)
			}
		}

	case RedisModeSentinel:
		// Get sentinel info
		info.SentinelMaster = r.config.SentinelMasterName
	}

	return info, nil
}

// RedisClusterInfo contains information about the Redis cluster
type RedisClusterInfo struct {
	Mode           RedisMode          `json:"mode"`
	Connected      bool               `json:"connected"`
	Version        string             `json:"version,omitempty"`
	Role           string             `json:"role,omitempty"`
	ConnectedSlaves int               `json:"connected_slaves,omitempty"`
	ConnectedClients int              `json:"connected_clients,omitempty"`
	ClusterState   string             `json:"cluster_state,omitempty"`
	ClusterNodes   []RedisNodeInfo    `json:"cluster_nodes,omitempty"`
	SentinelMaster string             `json:"sentinel_master,omitempty"`
}

// RedisNodeInfo contains information about a cluster node
type RedisNodeInfo struct {
	ID       string `json:"id"`
	Address  string `json:"address"`
	Role     string `json:"role"`
	Master   string `json:"master,omitempty"`
	PingMs   int64  `json:"ping_ms,omitempty"`
	LinkState string `json:"link_state"`
	Slots    string `json:"slots,omitempty"`
}

func (info *RedisClusterInfo) parseInfo(infoStr string) {
	lines := strings.Split(infoStr, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "redis_version:") {
			info.Version = strings.TrimPrefix(line, "redis_version:")
		} else if strings.HasPrefix(line, "role:") {
			info.Role = strings.TrimPrefix(line, "role:")
		} else if strings.HasPrefix(line, "connected_slaves:") {
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "connected_slaves:"), "%d", &info.ConnectedSlaves)
		} else if strings.HasPrefix(line, "connected_clients:") {
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, "connected_clients:"), "%d", &info.ConnectedClients)
		}
	}
}

func parseClusterState(infoStr string) string {
	lines := strings.Split(infoStr, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cluster_state:") {
			return strings.TrimPrefix(line, "cluster_state:")
		}
	}
	return "unknown"
}

func parseClusterNodes(nodesStr string) []RedisNodeInfo {
	var nodes []RedisNodeInfo
	lines := strings.Split(nodesStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 8 {
			continue
		}
		node := RedisNodeInfo{
			ID:        parts[0][:8], // Truncate for readability
			Address:   parts[1],
			LinkState: parts[7],
		}
		if strings.Contains(parts[2], "master") {
			node.Role = "master"
			if len(parts) > 8 {
				node.Slots = strings.Join(parts[8:], " ")
			}
		} else if strings.Contains(parts[2], "slave") {
			node.Role = "slave"
			node.Master = parts[3][:8]
		}
		nodes = append(nodes, node)
	}
	return nodes
}

// SubscribeToFailover subscribes to Redis failover events (Sentinel mode)
func (r *RedisClusterClient) SubscribeToFailover(ctx context.Context, callback func(newMaster string)) error {
	if r.mode != RedisModeSentinel {
		return fmt.Errorf("failover subscription only available in sentinel mode")
	}

	// Subscribe to sentinel switch-master channel
	pubsub := r.client.Subscribe(ctx, "+switch-master")
	defer pubsub.Close()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-pubsub.Channel():
				if msg != nil {
					// Message format: "<master-name> <old-ip> <old-port> <new-ip> <new-port>"
					parts := strings.Fields(msg.Payload)
					if len(parts) >= 5 && parts[0] == r.config.SentinelMasterName {
						newMaster := fmt.Sprintf("%s:%s", parts[3], parts[4])
						r.logger.WithFields(logrus.Fields{
							"old_master": fmt.Sprintf("%s:%s", parts[1], parts[2]),
							"new_master": newMaster,
						}).Warn("Redis master failover detected")
						callback(newMaster)
					}
				}
			}
		}
	}()

	return nil
}
