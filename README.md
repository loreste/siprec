# SIPREC Server with End-to-End Encryption (v0.2.0)

A high-performance Session Recording Protocol (SIPREC) server implementation in Go with advanced resource optimization, comprehensive SIPREC metadata processing, production-ready session redundancy (RFC 7245), and optional end-to-end encryption for recordings and metadata.

## Features

### Core Protocol Support
- **Full SIPREC Compliance**: RFC 7865/7866 session recording with complete metadata handling
- **SIP Protocol**: Comprehensive SIP message handling including OPTIONS, INVITE, INFO, BYE
- **Advanced Metadata Processing**: Complex participant management, stream configuration, and state transitions
- **Session Control**: Pause/resume recording with sequence tracking and state validation

### Session Management & Redundancy
- **RFC 7245 Session Redundancy**: Robust session recovery for continuous recording
- **Failover Support**: Automatic session recovery with participant continuity
- **Dialog Replacement**: SIP Replaces header implementation for seamless transitions
- **Media Stream Continuity**: RTP stream recovery with session context preservation

### Resource Optimization
- **Concurrent Session Scaling**: Support for 1000+ concurrent sessions with optimized resource usage
- **Memory Pool Management**: Intelligent buffer pooling reducing GC pressure by 50-70%
- **Worker Pool Architecture**: Dynamic goroutine scaling for optimal CPU utilization
- **Session Caching**: LRU cache with TTL for frequently accessed sessions (configurable hit rates)
- **Port Management**: Enhanced RTP port allocation with reuse optimization

### Media & Security
- **Transport Layer Security (TLS)**: Secure SIP signaling with TLS 1.2+ support
- **Media Encryption (SRTP)**: Secure Real-time Transport Protocol for encrypted media
- **End-to-End Encryption**: Optional AES-256-GCM/ChaCha20-Poly1305 encryption for recordings and metadata
- **Key Management**: Automated key generation, rotation, and secure storage
- **Audio Processing**: Multi-channel support with noise reduction and voice activity detection
- **Codec Support**: PCMU, PCMA, Opus, and EVS codecs with quality metrics

### Monitoring & Operations
- **Performance Metrics**: Comprehensive statistics for sessions, memory, and resource usage
- **Health Monitoring**: Real-time health checks and resource utilization tracking
- **Graceful Shutdown**: Clean termination with configurable timeouts
- **Docker Support**: Production-ready multi-stage containerization with docker-compose
- **Comprehensive Testing**: Full test suite with unit, integration, and E2E tests
- **STT Provider Integration**: Support for multiple speech-to-text providers with testing

### Security & Encryption
- **End-to-End Encryption**: Optional AES-256-GCM/ChaCha20-Poly1305 encryption for recordings and metadata
- **Automatic Key Management**: Secure key generation, rotation, and storage
- **Multiple Key Stores**: File-based persistent storage and memory-based volatile storage
- **Encryption Algorithms**: Support for AES-256-GCM and ChaCha20-Poly1305
- **Session Isolation**: Independent encryption contexts for each recording session
- **Forward Secrecy**: Configurable automatic key rotation for enhanced security

## Project Structure

```
/
├── cmd/
│   ├── siprec/       # Main SIPREC server application
│   └── testenv/      # Environment validation tool
├── docs/             # Documentation
│   ├── SESSION_REDUNDANCY.md  # Session redundancy documentation
│   └── RFC_COMPLIANCE.md      # RFC compliance documentation
├── pkg/
│   ├── audio/        # Audio processing with optimization and encryption
│   │   ├── manager.go          # Audio processing manager
│   │   ├── multi_channel.go    # Multi-channel audio support
│   │   ├── optimized_processor.go # High-performance audio processing
│   │   ├── encrypted_manager.go # Encrypted audio recording management
│   │   └── types.go            # Audio processing types
│   ├── encryption/   # End-to-end encryption system
│   │   ├── manager.go          # Encryption operations and key management
│   │   ├── keystore.go         # Secure key storage implementations
│   │   ├── rotation_service.go # Automatic key rotation service
│   │   └── types.go            # Encryption types and interfaces
│   ├── media/        # Media handling with enhanced port management
│   │   ├── codec.go            # Codec support (PCMU, PCMA, Opus, EVS)
│   │   ├── port_manager.go     # Optimized RTP port allocation
│   │   └── types.go            # Media types and quality metrics
│   ├── sip/          # SIP protocol implementation
│   │   ├── handler.go          # SIP request handling with redundancy
│   │   ├── sharded_map.go      # High-concurrency session storage
│   │   └── types.go            # SIP types and session store
│   ├── siprec/       # SIPREC protocol implementation with encryption
│   │   ├── parser.go           # Comprehensive metadata parsing
│   │   ├── session.go          # Session redundancy (RFC 7245)
│   │   ├── session_manager.go  # Optimized session management
│   │   ├── encrypted_session.go # Encrypted session management
│   │   └── types.go            # SIPREC data structures
│   └── util/         # Resource optimization utilities
│       ├── goroutine_pool.go   # Dynamic worker pool management
│       ├── resource_pool.go    # Memory pool management
│       ├── session_cache.go    # LRU caching with TTL
│       └── sharded_map.go      # High-concurrency data structures
├── scripts/          # Testing and utility scripts
│   └── test_redundancy.sh      # Redundancy feature testing
├── sessions/         # Session storage (for redundancy)
├── test/             # Comprehensive test suite
│   ├── e2e/          # End-to-end tests
│   │   ├── session_recovery_test.go    # Session recovery scenarios
│   │   ├── siprec_redundancy_test.go   # SIPREC redundancy testing
│   │   ├── siprec_simulation_test.go   # Full SIPREC flow simulation
│   │   └── server_test.go              # Complete server functionality tests
│   ├── integration/  # Integration tests
│   │   └── stt_providers_test.go       # STT provider integration testing
│   ├── unit/         # Unit tests
│   │   └── messaging_test.go           # Messaging component tests
│   └── providers/    # Provider resilience tests
│       └── stt_resilience_test.go      # STT provider testing
└── test_tls/         # TLS testing tools and utilities
```

## Quick Start

### Native Installation

1. Clone the repository
2. Copy `.env.example` to `.env` and configure it
3. Run the setup and start the server:

```bash
# Complete setup (builds app, checks env, ensures directories)
make setup

# Start the server
make run
```

### Docker Installation

Run with Docker for production deployment:

```bash
# Build Docker image
make docker-build

# Run with docker-compose (includes RabbitMQ, Redis, PostgreSQL)
make docker-up

# Development environment with all services
docker-compose -f docker-compose.dev.yml up
```

## Development

```bash
# Run environment check only
make env-test

# Format code
make fmt

# Lint code
make lint

# Run all tests
make test

# Run specific test suites
make test-unit              # Unit tests only
make test-integration       # Integration tests (STT providers)
make test-e2e              # End-to-end tests

# Run tests with coverage
make test-coverage

# Build for development
make build

# Clean build artifacts
make clean
```

## Testing the Server

See [TESTING.md](./TESTING.md) for comprehensive documentation on the testing framework.

### Using SIP Test Tools

You can test the SIPREC server using SIP testing tools like SIPp or Kamailio:

```bash
# Example SIPp command to send a SIPREC INVITE:
sipp -sf siprec_invite.xml -m 1 -s 1000 127.0.0.1:5060
```

### TLS and SIPREC XML Testing

The repository includes a TLS test client that verifies:
- Secure SIP over TLS connections
- SIPREC XML content handling according to RFC 7865/7866
- SDP media negotiation with SRTP support

```bash
# Run the TLS test script
./test_tls.sh
```

### Health Check API

The server provides a health check API at port 8080:

```bash
# Check server health
curl http://localhost:8080/health

# Get server metrics
curl http://localhost:8080/metrics

# Check STUN status
curl http://localhost:8080/stun-status
```

## Configuration Options

Edit the `.env` file to configure the server. Key configuration sections:

### Encryption Configuration (New!)
```properties
# End-to-End Encryption (Optional)
ENABLE_RECORDING_ENCRYPTION=false    # Encrypt audio recordings
ENABLE_METADATA_ENCRYPTION=false     # Encrypt session metadata
ENCRYPTION_ALGORITHM=AES-256-GCM     # Encryption algorithm (AES-256-GCM, ChaCha20-Poly1305)
KEY_ROTATION_INTERVAL=24h            # Automatic key rotation interval
MASTER_KEY_PATH=./keys               # Key storage directory
ENCRYPTION_KEY_STORE=file            # Key storage type (file, memory)
KEY_BACKUP_ENABLED=true              # Enable automatic key backups
PBKDF2_ITERATIONS=100000             # PBKDF2 iterations for key derivation
```

### Resource Optimization
```properties
# Concurrent Session Management
MAX_CONCURRENT_CALLS=1000     # Maximum concurrent sessions (optimized for high load)
SHARD_COUNT=64                # Number of shards for session storage (reduce lock contention)

# Memory Management
ENABLE_BUFFER_POOLING=true    # Enable memory pool optimization
CACHE_SIZE=5000               # Session cache size for hot sessions
CACHE_TTL=15m                 # Cache TTL for session data

# Worker Pool Configuration
WORKER_POOL_SIZE=auto         # Worker pool size (auto=CPU cores * 2)
AUDIO_PROCESSING_WORKERS=auto # Audio processing workers (auto=CPU cores)
```

### Session Redundancy
```properties
# Session Redundancy Configuration (RFC 7245)
ENABLE_REDUNDANCY=true        # Enable session redundancy
SESSION_TIMEOUT=30s           # Timeout for session inactivity
SESSION_CHECK_INTERVAL=10s    # Interval for checking session health
REDUNDANCY_STORAGE_TYPE=memory # Storage type for redundancy (memory, redis planned)
```

### Network & Media
```properties
# SIP/RTP Configuration
EXTERNAL_IP=auto              # Public IP address for SDP (auto=detect)
PORTS=5060,5061               # SIP listening ports
RTP_PORT_MIN=10000            # RTP port range minimum
RTP_PORT_MAX=20000            # RTP port range maximum

# TLS/SRTP Configuration
ENABLE_TLS=true               # Enable TLS for secure SIP signaling
TLS_PORT=5063                 # TLS listening port
TLS_CERT_PATH=./certs/cert.pem # Path to TLS certificate file
TLS_KEY_PATH=./certs/key.pem  # Path to TLS key file
ENABLE_SRTP=true              # Enable SRTP for secure media transport

# Audio Processing
ENABLE_AUDIO_PROCESSING=true  # Enable advanced audio processing
ENABLE_VAD=true               # Voice Activity Detection
ENABLE_NOISE_REDUCTION=true   # Noise reduction processing
MULTI_CHANNEL_SUPPORT=true    # Multi-channel audio support

# End-to-End Encryption (Optional)
ENABLE_RECORDING_ENCRYPTION=false    # Encrypt audio recordings
ENABLE_METADATA_ENCRYPTION=false     # Encrypt session metadata
ENCRYPTION_ALGORITHM=AES-256-GCM     # Encryption algorithm (AES-256-GCM, ChaCha20-Poly1305)
KEY_ROTATION_INTERVAL=24h            # Automatic key rotation interval
MASTER_KEY_PATH=./keys               # Key storage directory
ENCRYPTION_KEY_STORE=file            # Key storage type (file, memory)
KEY_BACKUP_ENABLED=true              # Enable automatic key backups
```

### Basic Configuration
```properties
# Storage and Directories
RECORDING_DIR=./recordings    # Directory to store recordings
SESSION_STORAGE_DIR=./sessions # Directory for session redundancy data

# Monitoring and Health
HTTP_ENABLED=true             # Enable HTTP API and health checks
HTTP_PORT=8080                # HTTP server port
ENABLE_METRICS=true           # Enable Prometheus metrics
```

## Docker Deployment

### Multi-Stage Docker Build

The project includes a production-ready multi-stage Docker build system:

- **Builder Stage**: Compiles the Go application with optimized build flags
- **Tester Stage**: Runs the complete test suite during build
- **Production Stage**: Minimal runtime image with security hardening
- **Development Stage**: Full development environment with debugging tools

### Docker Compose Environments

**Production (docker-compose.yml)**:
```bash
make docker-up
```

**Development (docker-compose.dev.yml)**:
```bash
docker-compose -f docker-compose.dev.yml up
```

The development environment includes:
- RabbitMQ for message queuing
- Redis for caching
- PostgreSQL for persistent storage
- Prometheus for metrics
- Grafana for visualization
- Nginx for load balancing

### Docker Configuration

Key Docker features:
- **Security**: Non-root user execution, minimal attack surface
- **Health Checks**: Built-in health monitoring
- **Signal Handling**: Graceful shutdown with proper signal handling
- **Environment Validation**: Startup validation of required configuration
- **Multi-Architecture**: Support for AMD64 and ARM64 platforms

See [RESOURCE_OPTIMIZATION.md](./docs/RESOURCE_OPTIMIZATION.md) for detailed information about performance optimization features, [SESSION_REDUNDANCY.md](./docs/SESSION_REDUNDANCY.md) for session redundancy documentation, [RFC_COMPLIANCE.md](./docs/RFC_COMPLIANCE.md) for RFC compliance details, [SECURITY.md](./docs/SECURITY.md) for TLS and SRTP security features, and [ENCRYPTION.md](./docs/ENCRYPTION.md) for end-to-end encryption capabilities.

## Performance & Optimization

### Resource Management
The server implements advanced resource optimization for high-concurrency scenarios:

- **Memory Pooling**: Intelligent buffer pools reduce garbage collection pressure by 50-70%
- **Worker Pool Architecture**: Dynamic scaling of goroutines based on load
- **Session Caching**: LRU cache with TTL for frequently accessed sessions
- **Sharded Data Structures**: 64-shard maps reduce lock contention for concurrent access

### Concurrent Session Handling
Optimized for handling 1000+ concurrent sessions:

- **Horizontal Scaling**: Sharded session storage distributes load across CPU cores
- **Port Management**: Enhanced RTP port allocation with reuse optimization
- **Asynchronous Processing**: Non-blocking session operations with worker pools
- **Memory Efficiency**: Pre-allocated buffers and object pooling

### Audio Processing Optimization
High-performance audio processing pipeline:

- **Frame-based Processing**: Reduces memory pressure through chunked processing
- **Multi-channel Support**: Concurrent processing of stereo and multi-channel audio
- **Codec Support**: Optimized decoders for PCMU, PCMA, Opus, and EVS
- **Voice Activity Detection**: Intelligent processing based on audio content

### Monitoring & Metrics
Comprehensive performance monitoring:

```bash
# View session statistics
curl http://localhost:8080/api/sessions/stats

# Check resource utilization
curl http://localhost:8080/api/resources/stats

# View cache performance
curl http://localhost:8080/api/cache/stats

# Monitor worker pool performance
curl http://localhost:8080/api/workers/stats
```

## SIPREC Metadata Support

The server provides comprehensive SIPREC metadata handling:

### Supported Features
- **Complete XML Parsing**: Full RFC 7865/7866 metadata schema support
- **Participant Management**: Complex participant configurations with roles and AORs
- **Stream Configuration**: Audio/video stream management with mixing support
- **State Transitions**: Session pause/resume with sequence tracking
- **Validation**: Comprehensive metadata validation with detailed error reporting

### Metadata Examples
```xml
<!-- Basic Session -->
<recording xmlns="urn:ietf:params:xml:ns:recording:1" 
           session="session-123" state="active">
  <participant id="p1">
    <name>Alice</name>
    <aor>sip:alice@example.com</aor>
  </participant>
  <sessionrecordingassoc sessionid="session-123" callid="call-123"/>
</recording>

<!-- Complex Multi-Participant with Streams -->
<recording xmlns="urn:ietf:params:xml:ns:recording:1" 
           session="session-456" state="active" direction="inbound">
  <participant id="p1" role="active">
    <name>Alice Smith</name>
    <aor display="Work">sip:alice@company.com</aor>
    <send>audio1</send>
    <send>video1</send>
  </participant>
  <stream label="audio1" streamid="stream1" type="audio" mode="separate">
    <mixing></mixing>
  </stream>
  <sessionrecordingassoc sessionid="session-456" callid="call-456"/>
</recording>
```

## Redundancy Design

### How Session Redundancy Works

1. **Session State Persistence**:
   - All active recording sessions are tracked in a session store
   - Session state is updated with each SIP transaction
   - Session health is monitored periodically

2. **Failure Detection**:
   - Server monitors session activity and detects stale sessions
   - Clients can detect network failures and initiate recovery

3. **Recovery Process**:
   - Client reconnects using SIP INVITE with Replaces header
   - Server identifies original session from Replaces header
   - Session state is restored from persistent store
   - Media streams are reestablished

4. **Stream Continuity**:
   - RTP streams are resumed with the same session context
   - Recording continues with original session identifiers
   - Recordings can be seamlessly combined

For more details on the session redundancy implementation, see the [SESSION_REDUNDANCY.md](./docs/SESSION_REDUNDANCY.md) documentation.

## Testing

### Performance & Optimization Testing
The server includes comprehensive testing for resource optimization features:

```bash
# Run all tests including optimization features
make test

# Run end-to-end tests with concurrent sessions
make test-e2e

# Test SIPREC metadata handling
go test ./pkg/siprec -v

# Test resource optimization
go test ./pkg/util -v
```

### Session Redundancy Testing
The repository includes a script to test the session redundancy feature:

```bash
# Run the redundancy test script
./scripts/test_redundancy.sh
```

### Test Coverage
The comprehensive test suite includes:
- **Unit Tests**: Session recovery functions, metadata parsing, resource pools
- **Integration Tests**: End-to-end SIPREC flows with metadata validation
- **Performance Tests**: Concurrent session handling, memory optimization
- **Redundancy Tests**: Failover scenarios, media stream continuity
- **Resource Tests**: Memory pool efficiency, worker pool scaling

## License

See the LICENSE file for details.