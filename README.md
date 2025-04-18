# SIPREC Server with RFC 7245 Session Redundancy (v0.1.0)

A Session Recording Protocol (SIPREC) server implementation in Go with a focus on high availability through session redundancy (RFC 7245) to maintain recording continuity during network failures or server restarts.

## Features

- **RFC 7245 Compliant Session Redundancy**: Robust session recovery for continuous recording
- **Failover Support**: Automatic session recovery after connection failures
- **Dialog Replacement**: SIP Replaces header implementation for session continuity
- **Media Stream Continuity**: Maintains RTP stream continuity during failovers
- **Transport Layer Security (TLS)**: Secure SIP signaling with TLS support
- **Media Encryption (SRTP)**: Secure Real-time Transport Protocol for encrypted media
- **Concurrent Session Handling**: Sharded map implementation for high-throughput session management
- **Memory Optimization**: Efficient buffer pooling for RTP packet processing
- **Performance Metrics**: Comprehensive Prometheus metrics for production monitoring
- **Docker Support**: Easy deployment with Docker and docker-compose
- **Extensive Testing**: Comprehensive test suite for redundancy features
- **SIP/SIPREC Protocol**: Support for RFC 7865/7866 for recording sessions
- **Graceful Shutdown**: Clean termination of all resources and connections

## Project Structure

```
/
├── cmd/
│   └── siprec/       # Main application entry point
├── docs/             # Documentation
│   ├── SESSION_REDUNDANCY.md  # Session redundancy documentation
│   └── RFC_COMPLIANCE.md      # RFC compliance documentation
├── pkg/
│   ├── sip/          # SIP protocol implementation
│   │   ├── handler.go      # SIP request handling with redundancy support
│   │   ├── sdp.go          # SDP processing for media negotiation
│   │   └── types.go        # SIP types including session store
│   └── siprec/       # SIPREC protocol implementation
│       ├── parser.go       # SIPREC metadata parsing
│       ├── session.go      # Session redundancy implementation (RFC 7245)
│       └── types.go        # SIPREC data structures
├── scripts/          # Testing and utility scripts
│   └── test_redundancy.sh  # Script to test redundancy features
├── sessions/         # Session storage (for redundancy)
└── test/             # Test suite
    └── e2e/          # End-to-end tests
        ├── session_recovery_test.go    # Basic recovery tests
        ├── sip_mock_test.go            # SIP mocking utilities
        └── siprec_redundancy_test.go   # Advanced redundancy tests
```

## Quick Start

1. Clone the repository
2. Copy `.env.example` to `.env` and configure it
3. Run the setup and start the server:

```bash
# Complete setup (builds app, checks env, ensures directories)
make setup

# Start the server
make run

# Alternatively, run with docker-compose (with RabbitMQ)
make docker-up
```

## Development

```bash
# Run environment check only
make env-test

# Format code
make fmt

# Lint code
make lint

# Run unit tests
make test

# Run end-to-end tests
make test-e2e

# Run tests with coverage
make test-coverage
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

Edit the `.env` file to configure the server. Key redundancy options:

```properties
# Session Redundancy Configuration
ENABLE_REDUNDANCY=true        # Enable session redundancy
SESSION_TIMEOUT=30s           # Timeout for session inactivity
SESSION_CHECK_INTERVAL=10s    # Interval for checking session health
REDUNDANCY_STORAGE_TYPE=memory # Storage type for redundancy (memory, redis planned)
SHARD_COUNT=32                # Number of shards for concurrent session handling

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

# Basic Configuration
RECORDING_DIR=./recordings    # Directory to store recordings
MAX_CONCURRENT_CALLS=500      # Maximum concurrent calls
```

See [SESSION_REDUNDANCY.md](./docs/SESSION_REDUNDANCY.md) for detailed information about the session redundancy feature, [RFC_COMPLIANCE.md](./docs/RFC_COMPLIANCE.md) for details on RFC compliance, and [SECURITY.md](./docs/SECURITY.md) for information about TLS and SRTP security features.

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

## Testing Redundancy

The repository includes a script to test the session redundancy feature:

```bash
# Run the redundancy test script
./scripts/test_redundancy.sh
```

The test suite includes:
- Unit tests for session recovery functions
- End-to-end tests for failover scenarios
- Concurrent session recovery tests
- Media stream continuity tests

## License

See the LICENSE file for details.