# SIP Package (v0.2.0)

This package implements a comprehensive Session Initiation Protocol (SIP) handling system for SIPREC recording sessions, featuring a custom SIP server implementation optimized for TCP transport and large metadata processing.

## Architecture Overview

The SIP package consists of two main components working together:

1. **CustomSIPServer**: A custom SIP server implementation optimized for TCP transport
2. **Handler**: Business logic processor for SIP messages and call management

## Components

### CustomSIPServer

The `CustomSIPServer` is a from-scratch SIP server implementation that provides:

- **Multi-transport support**: UDP, TCP, and TLS protocols
- **Optimized TCP handling**: Proper CRLF parsing for large SIPREC messages
- **Connection management**: Persistent TCP connections with lifecycle tracking
- **Message parsing**: RFC-compliant SIP message parsing and validation
- **Concurrent processing**: Goroutine-based request handling

Key features:
- Handles large multipart SIPREC INVITE messages over TCP
- Proper line-by-line message parsing with CRLF support
- Connection pooling and timeout management
- Built-in SIP response generation

### Handler

The `Handler` manages SIP business logic for recording sessions, including:

- Processing INVITE, BYE, and OPTIONS requests
- Managing active calls and session state
- Handling SIPREC metadata extraction and parsing
- Supporting session redundancy and recovery
- Integrating with speech-to-text providers

### Sharded Map

The `ShardedMap` implementation provides a high-performance, scalable alternative to Go's built-in `sync.Map` for managing concurrent sessions with reduced lock contention.

Key benefits:

1. **Reduced lock contention**: By dividing the map into multiple independent shards, each with its own lock, multiple goroutines can access different shards concurrently without blocking each other.

2. **Improved performance under high concurrency**: When the application handles many simultaneous SIP sessions, the sharded approach significantly reduces lock waiting time.

3. **Consistent API**: Implements the same interface as `sync.Map` for straightforward migration.

4. **Customizable shard count**: The number of shards can be tuned based on expected concurrency levels.

## Usage

### Basic Setup

```go
// Create SIP handler
config := &sip.Config{
    MaxConcurrentCalls: 100,
    ShardCount: 32,
    MediaConfig: mediaConfig,
}
handler, err := sip.NewHandler(logger, config, sttCallback)

// Create custom SIP server
customServer := sip.NewCustomSIPServer(logger, handler)

// Start listeners
go customServer.ListenAndServeUDP(ctx, "0.0.0.0:5060")
go customServer.ListenAndServeTCP(ctx, "0.0.0.0:5060")
go customServer.ListenAndServeTLS(ctx, "0.0.0.0:5065", tlsConfig)
```

### Configuration Options

The `ShardCount` configuration option determines the number of shards used for concurrent session management. This should be set to a power of 2 (16, 32, 64, etc.) to optimize performance. The default is 32 shards, which provides a good balance for most workloads.

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

## Transport Layer Architecture

### TCP Transport Optimization

The custom SIP server includes specific optimizations for TCP transport:

**CRLF Handling**: Proper parsing of `\r\n` line endings for SIP messages, essential for large SIPREC payloads

**Connection Lifecycle**: 
- Persistent TCP connections with proper cleanup
- Connection timeout and activity tracking
- Graceful connection closure handling

**Message Parsing**:
- Line-by-line header parsing using `bufio.Reader`
- Content-Length based body reading for multipart messages
- Support for large SIPREC metadata (>1KB)

**Concurrent Processing**:
- Each TCP connection handled in separate goroutine
- Non-blocking message processing
- Proper error handling and recovery

### UDP Transport

Maintains compatibility with existing UDP-based SIP clients:
- Large buffer support (65KB) for fragmented messages
- Timeout-based packet handling
- Connectionless request/response processing

### TLS Transport

Secure SIP transport with:
- TLS 1.2+ encryption
- Certificate-based authentication
- Same message parsing as TCP with encryption layer

## Integration Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   SIP Client    │────│  CustomSIPServer │────│     Handler     │
│   (TCP/UDP/TLS) │    │                  │    │                 │
└─────────────────┘    │  - Transport     │    │ - Call Logic    │
                       │  - Parsing       │    │ - SIPREC        │
                       │  - Routing       │    │ - Session Mgmt  │
                       └──────────────────┘    └─────────────────┘
                                │                       │
                                │                       ▼
                                │               ┌─────────────────┐
                                │               │ Speech-to-Text  │
                                │               │   Providers     │
                                │               └─────────────────┘
                                ▼
                        ┌──────────────────┐
                        │   RTP/Media      │
                        │   Processing     │
                        └──────────────────┘
```