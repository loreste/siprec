# SIP Package (v0.1.0)

This package implements Session Initiation Protocol (SIP) handling for SIPREC recording sessions.

## Components

### Handler

The `Handler` manages SIP requests for recording sessions, including:

- Processing INVITE, BYE, and OPTIONS requests
- Managing active calls
- Handling SIPREC metadata
- Supporting session redundancy and recovery

### Sharded Map

The `ShardedMap` implementation provides a high-performance, scalable alternative to Go's built-in `sync.Map` for managing concurrent sessions with reduced lock contention.

Key benefits:

1. **Reduced lock contention**: By dividing the map into multiple independent shards, each with its own lock, multiple goroutines can access different shards concurrently without blocking each other.

2. **Improved performance under high concurrency**: When the application handles many simultaneous SIP sessions, the sharded approach significantly reduces lock waiting time.

3. **Consistent API**: Implements the same interface as `sync.Map` for straightforward migration.

4. **Customizable shard count**: The number of shards can be tuned based on expected concurrency levels.

## Usage

The `ShardCount` configuration option determines the number of shards used. This should be set to a power of 2 (16, 32, 64, etc.) to optimize performance. The default is 32 shards, which provides a good balance for most workloads.

```go
config := &sip.Config{
    ShardCount: 32,
    // other configuration options...
}
handler, err := sip.NewHandler(logger, config, sttCallback)
```

## Performance Considerations

- The optimal shard count depends on the number of CPU cores and expected concurrency
- For servers with 8-16 cores handling thousands of concurrent calls, 32-64 shards is recommended
- For smaller deployments, 16 shards may be sufficient
- Monitor lock contention in production and adjust as needed

## Implementation Notes

The sharded map uses:

- FNV hash algorithm to distribute keys across shards
- Read-write mutexes for each shard to allow concurrent reads
- Shard selection via bit masking for efficient lookup