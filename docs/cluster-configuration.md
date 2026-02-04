# SIPREC Cluster Configuration Guide

This guide covers the high-availability and clustering features of SIPREC Server, enabling production deployments with redundancy, failover, and horizontal scaling.

## Overview

SIPREC Server supports full clustering with the following capabilities:

| Feature | Description |
|---------|-------------|
| Redis Sentinel/Cluster | High-availability Redis with automatic failover |
| RTP State Replication | Real-time sync of media stream state across nodes |
| Distributed Rate Limiting | Cluster-wide rate limits to prevent overload |
| Split-Brain Detection | Network partition detection and fencing |
| Distributed Tracing | Cross-node call flow tracing |
| Stream Migration | Seamless RTP stream handoff between nodes |

## Redis Deployment Modes

### Standalone Mode

**Best for**: Development, testing, single-server deployments

```yaml
cluster:
  enabled: true
  redis:
    mode: "standalone"
    address: "localhost:6379"
    password: ""
    database: 0
```

**How it works**:
- Connects to a single Redis instance
- No automatic failover
- If Redis fails, cluster features become unavailable
- Sessions in Redis are lost if Redis restarts without persistence

**When to use**:
- Development environments
- Small deployments with a single SIPREC node
- When Redis persistence (RDB/AOF) provides sufficient durability

---

### Sentinel Mode

**Best for**: Production deployments requiring high availability

```yaml
cluster:
  enabled: true
  redis:
    mode: "sentinel"
    sentinel_addresses:
      - "sentinel1.example.com:26379"
      - "sentinel2.example.com:26379"
      - "sentinel3.example.com:26379"
    sentinel_master_name: "mymaster"
    sentinel_password: ""
    password: "redis-password"
```

**How it works**:
- Redis Sentinel monitors master and replica instances
- Automatic failover: if master fails, Sentinel promotes a replica
- SIPREC automatically reconnects to the new master
- Minimum 3 Sentinels recommended for quorum

**Architecture**:
```
                    ┌─────────────┐
                    │  Sentinel 1 │
                    └──────┬──────┘
                           │
┌──────────┐        ┌──────┴──────┐        ┌──────────┐
│ SIPREC 1 │───────▶│   Master    │◀───────│ SIPREC 2 │
└──────────┘        └──────┬──────┘        └──────────┘
                           │
                    ┌──────┴──────┐
                    │   Replica   │
                    └─────────────┘
```

**Failover process**:
1. Sentinel detects master is unreachable
2. Sentinels vote to confirm failure (quorum)
3. Sentinel promotes replica to master
4. SIPREC clients automatically reconnect
5. Call sessions continue without interruption

**When to use**:
- Production environments with 2+ SIPREC nodes
- When you need automatic Redis failover
- Deployments requiring 99.9%+ uptime

---

### Cluster Mode

**Best for**: Large-scale deployments requiring horizontal scaling

```yaml
cluster:
  enabled: true
  redis:
    mode: "cluster"
    cluster_addresses:
      - "redis1.example.com:6379"
      - "redis2.example.com:6379"
      - "redis3.example.com:6379"
      - "redis4.example.com:6379"
      - "redis5.example.com:6379"
      - "redis6.example.com:6379"
    password: "redis-password"
```

**How it works**:
- Data is sharded across multiple Redis nodes (16384 hash slots)
- Each master has one or more replicas
- Automatic failover within each shard
- Linear scalability for reads and writes

**Architecture**:
```
┌─────────────────────────────────────────────────────────┐
│                    Redis Cluster                         │
│  ┌─────────┐    ┌─────────┐    ┌─────────┐             │
│  │Master 1 │    │Master 2 │    │Master 3 │             │
│  │0-5460   │    │5461-10922│   │10923-16383│           │
│  └────┬────┘    └────┬────┘    └────┬────┘             │
│       │              │              │                   │
│  ┌────┴────┐    ┌────┴────┐    ┌────┴────┐             │
│  │Replica 1│    │Replica 2│    │Replica 3│             │
│  └─────────┘    └─────────┘    └─────────┘             │
└─────────────────────────────────────────────────────────┘
          ▲              ▲              ▲
          │              │              │
     ┌────┴────┐    ┌────┴────┐    ┌────┴────┐
     │SIPREC 1 │    │SIPREC 2 │    │SIPREC 3 │
     └─────────┘    └─────────┘    └─────────┘
```

**When to use**:
- Very large deployments (1000+ concurrent calls)
- When single Redis instance memory is insufficient
- Geographic distribution requirements

---

## Cluster Features

### RTP State Replication

Synchronizes RTP media stream state across all cluster nodes in real-time.

```yaml
cluster:
  rtp_state_replication: true
```

**What it does**:
- Stores active stream metadata in Redis (codec, ports, SSRC, stats)
- Enables any node to see all active calls
- Supports stream migration during failover
- Updates every 30 seconds + on significant events

**Stored state**:
- Call UUID and session ID
- Local/remote ports and addresses
- Codec information (name, payload type, sample rate)
- Recording path and pause state
- Packet statistics (received, lost, jitter)
- SRTP configuration

**Use case**: When a SIPREC node fails, another node can see which calls were active and potentially recover them.

---

### Distributed Rate Limiting

Enforces rate limits across the entire cluster, not just per-node.

```yaml
cluster:
  distributed_rate_limiting: true
```

**How it works**:
- Uses Redis sorted sets with sliding window algorithm
- Lua scripts ensure atomic operations
- Limits applied globally across all nodes

**Default limits**:
| Limit Type | Default | Description |
|------------|---------|-------------|
| Global CPS | 1000 | Calls per second across cluster |
| Global CPM | 50000 | Calls per minute across cluster |
| Per-IP CPS | 10 | Calls per second per source IP |
| Per-IP CPM | 100 | Calls per minute per source IP |
| Bandwidth | 100 MB/s | Total cluster bandwidth |

**Example**: If you have 3 SIPREC nodes with a global limit of 1000 CPS:
- Node 1 receives 400 calls/sec
- Node 2 receives 400 calls/sec
- Node 3 receives 300 calls/sec
- Total: 1100 CPS → 100 calls rejected cluster-wide

**Without distributed limiting**: Each node would allow 1000 CPS = 3000 CPS total, potentially overwhelming backend systems.

---

### Split-Brain Detection

Detects network partitions and prevents split-brain scenarios.

```yaml
cluster:
  split_brain_detection:
    enabled: true
    min_quorum: 2
    check_interval: "5s"
    grace_period: "15s"
    partition_action: "readonly"
    enable_fencing: true
```

**Configuration options**:

| Option | Description |
|--------|-------------|
| `min_quorum` | Minimum nodes required to accept calls |
| `check_interval` | How often to check cluster health |
| `grace_period` | Wait time before declaring partition |
| `partition_action` | Action when partitioned: `readonly`, `shutdown`, `continue` |
| `enable_fencing` | Prevent partitioned nodes from accepting new calls |

**Partition actions**:

- **readonly**: Node stops accepting new calls but continues existing ones
- **shutdown**: Node gracefully shuts down, migrating calls if possible
- **continue**: Node continues operating (risk of duplicate recordings)

**How it works**:
```
Normal Operation:
┌─────────┐     ┌─────────┐     ┌─────────┐
│ Node 1  │◀───▶│  Redis  │◀───▶│ Node 2  │
└─────────┘     └─────────┘     └─────────┘
     │                               │
     └───────────────────────────────┘
           All nodes see each other

Network Partition:
┌─────────┐     ┌─────────┐     ┌─────────┐
│ Node 1  │◀───▶│  Redis  │     │ Node 2  │ (isolated)
└─────────┘     └─────────┘     └─────────┘
                                     │
                               ┌─────┴─────┐
                               │  FENCED   │
                               │ No new    │
                               │  calls    │
                               └───────────┘
```

---

### Distributed Tracing

Correlates request traces across multiple nodes for debugging and monitoring.

```yaml
cluster:
  distributed_tracing: true
```

**What it does**:
- Generates unique trace IDs for each call
- Propagates trace context via SIP headers (X-Trace-ID, X-Span-ID)
- Stores traces in Redis for cross-node visibility
- Supports parent-child span relationships

**Headers used**:
```
X-Trace-ID: abc123def456...
X-Span-ID: 12345678
X-Parent-ID: 87654321
```

**Use case**: Debug why a call failed when it touched multiple SIPREC nodes:
1. Query by call UUID
2. Get all spans from all nodes
3. See complete call flow with timing

---

### Stream Migration

Enables seamless handoff of RTP streams between nodes.

```yaml
cluster:
  stream_migration: true
```

**When migration happens**:
- Graceful node shutdown (planned maintenance)
- Node overload (too many concurrent streams)
- Manual rebalancing

**Migration process**:
```
1. Source node initiates migration
   ┌─────────┐                    ┌─────────┐
   │ Node 1  │───Migration Req───▶│ Node 2  │
   │(source) │                    │(target) │
   └─────────┘                    └─────────┘

2. Target allocates ports and resources
   ┌─────────┐                    ┌─────────┐
   │ Node 1  │◀──Port Info────────│ Node 2  │
   └─────────┘                    └─────────┘

3. State transfer via Redis
   ┌─────────┐     ┌─────────┐    ┌─────────┐
   │ Node 1  │────▶│  Redis  │───▶│ Node 2  │
   └─────────┘     └─────────┘    └─────────┘

4. SBC redirected to new node (via SIP re-INVITE)
   ┌─────────┐                    ┌─────────┐
   │   SBC   │────RTP Stream─────▶│ Node 2  │
   └─────────┘                    └─────────┘

5. Source cleans up
   ┌─────────┐
   │ Node 1  │ (stream released)
   └─────────┘
```

**Graceful shutdown example**:
```bash
# On node being retired:
curl -X POST http://localhost:8080/admin/drain

# SIPREC will:
# 1. Stop accepting new calls
# 2. Migrate all active streams to other nodes
# 3. Wait for migrations to complete
# 4. Shutdown cleanly
```

---

## Complete Configuration Example

```yaml
cluster:
  enabled: true
  node_id: "siprec-prod-1"  # Unique per node
  heartbeat_interval: "5s"
  node_ttl: "15s"

  # Leader election
  leader_election_enabled: true
  leader_lock_ttl: "10s"
  leader_retry_interval: "3s"

  # Redis (Sentinel mode for production)
  redis:
    mode: "sentinel"
    sentinel_addresses:
      - "sentinel1.internal:26379"
      - "sentinel2.internal:26379"
      - "sentinel3.internal:26379"
    sentinel_master_name: "siprec-master"
    password: "${REDIS_PASSWORD}"
    pool_size: 20
    min_idle_conns: 5
    dial_timeout: "5s"
    read_timeout: "3s"
    write_timeout: "3s"
    max_retries: 3
    route_by_latency: true

    # TLS (recommended for production)
    tls_enabled: true
    tls_cert_file: "/etc/siprec/redis-client.crt"
    tls_key_file: "/etc/siprec/redis-client.key"
    tls_ca_file: "/etc/siprec/redis-ca.crt"

  # Features
  rtp_state_replication: true
  distributed_rate_limiting: true
  distributed_tracing: true
  stream_migration: true

  # Split-brain protection
  split_brain_detection:
    enabled: true
    min_quorum: 2
    check_interval: "5s"
    grace_period: "15s"
    partition_action: "readonly"
    enable_fencing: true
```

## Environment Variables

All cluster settings can be configured via environment variables:

```bash
# Basic cluster
CLUSTER_ENABLED=true
CLUSTER_NODE_ID=siprec-prod-1
CLUSTER_HEARTBEAT_INTERVAL=5s
CLUSTER_NODE_TTL=15s

# Redis mode
REDIS_MODE=sentinel
REDIS_SENTINEL_ADDRESSES=sentinel1:26379,sentinel2:26379,sentinel3:26379
REDIS_SENTINEL_MASTER=siprec-master
REDIS_PASSWORD=secretpassword

# Connection pool
REDIS_POOL_SIZE=20
REDIS_MIN_IDLE_CONNS=5
REDIS_DIAL_TIMEOUT=5s
REDIS_READ_TIMEOUT=3s
REDIS_WRITE_TIMEOUT=3s

# TLS
REDIS_TLS_ENABLED=true
REDIS_TLS_CERT_FILE=/etc/siprec/redis-client.crt
REDIS_TLS_KEY_FILE=/etc/siprec/redis-client.key
REDIS_TLS_CA_FILE=/etc/siprec/redis-ca.crt

# Features
CLUSTER_RTP_STATE_REPLICATION=true
CLUSTER_DISTRIBUTED_RATE_LIMITING=true
CLUSTER_DISTRIBUTED_TRACING=true
CLUSTER_STREAM_MIGRATION=true

# Split-brain
CLUSTER_SPLIT_BRAIN_ENABLED=true
CLUSTER_MIN_QUORUM=2
CLUSTER_PARTITION_ACTION=readonly
CLUSTER_ENABLE_FENCING=true
```

## Monitoring

### Health Endpoints

```bash
# Cluster status
curl http://localhost:8080/health/cluster

# Response:
{
  "node_id": "siprec-prod-1",
  "is_leader": true,
  "cluster_nodes": 3,
  "redis_mode": "sentinel",
  "redis_connected": true,
  "has_quorum": true,
  "partition_detected": false,
  "active_streams": 150,
  "pending_migrations": 0
}
```

### Prometheus Metrics

```
# Cluster metrics
siprec_cluster_nodes_total{} 3
siprec_cluster_is_leader{node_id="siprec-prod-1"} 1
siprec_cluster_has_quorum{} 1

# Rate limiting
siprec_ratelimit_requests_total{result="allowed"} 50000
siprec_ratelimit_requests_total{result="rejected"} 150

# Stream migration
siprec_migration_total{status="completed"} 25
siprec_migration_total{status="failed"} 2
siprec_migration_duration_seconds{quantile="0.99"} 1.5
```

## Troubleshooting

### Redis Connection Issues

```bash
# Test Redis connectivity
redis-cli -h sentinel1.internal -p 26379 SENTINEL master siprec-master

# Check Sentinel status
redis-cli -h sentinel1.internal -p 26379 SENTINEL sentinels siprec-master
```

### Split-Brain Issues

```bash
# Check partition status
curl http://localhost:8080/admin/split-brain/status

# Force quorum check
curl -X POST http://localhost:8080/admin/split-brain/check

# View partition history
curl http://localhost:8080/admin/split-brain/history
```

### Migration Issues

```bash
# List pending migrations
curl http://localhost:8080/admin/migrations/pending

# Cancel a stuck migration
curl -X DELETE http://localhost:8080/admin/migrations/{task_id}

# Force migrate all streams (graceful shutdown)
curl -X POST http://localhost:8080/admin/drain?target_node=siprec-prod-2
```

## Best Practices

1. **Always use Sentinel or Cluster mode in production**
2. **Deploy at least 3 Sentinel nodes** for proper quorum
3. **Set `min_quorum` to majority** (e.g., 2 for 3 nodes, 3 for 5 nodes)
4. **Enable TLS** for Redis connections in production
5. **Monitor Redis memory** - set appropriate maxmemory policies
6. **Test failover regularly** - run chaos engineering exercises
7. **Use node affinity** in Kubernetes to spread nodes across availability zones
