# Session Redundancy Feature Guide

This document describes the session redundancy functionality in the SIPREC server, which implements RFC 7245 for SIP-based Communications Session Continuity.

## Overview

The session redundancy feature provides resilience against network failures, SIP server restarts, and connection issues by maintaining session state and enabling seamless recovery of ongoing recording sessions.

## Features

- **Session State Persistence**: Maintains call state across network interruptions or server restarts
- **Dialog Recovery**: Implements SIP dialog recovery using the Replaces header mechanism
- **Media Stream Continuity**: Ensures media streams can be resumed after reconnection
- **Conformance to Standards**: Full compliance with SIP standards including RFC 7245, RFC 6341, RFC 7865, and RFC 7866

## Configuration

Session redundancy is configured through environment variables:

```properties
# Enable or disable session redundancy
ENABLE_REDUNDANCY=true

# Timeout for session inactivity (after which a session is considered stale)
SESSION_TIMEOUT=30s

# Interval for checking session health
SESSION_CHECK_INTERVAL=10s

# Storage type for redundancy (currently supports 'memory', with 'redis' planned for future)
REDUNDANCY_STORAGE_TYPE=memory
```

## Redundancy Flow

1. **Normal Operation**:
   - When a call is received, the server stores session information in the session store
   - Session health is periodically monitored
   - Session state is updated with each SIP transaction

2. **Failure Scenario**:
   - When a network failure or server restart occurs, active sessions are preserved
   - Upon server restart, orphaned sessions are identified in the session store

3. **Recovery Process**:
   - When a client reconnects with a Replaces header, the server identifies the original session
   - Session state and media streams are recovered
   - Recording continues uninterrupted with the same session ID

## Implementation Details

The redundancy implementation consists of several key components:

### 1. Session Storage

Sessions are stored in a pluggable storage backend:

- **InMemorySessionStore**: Default implementation that stores sessions in memory
- **NoOpSessionStore**: Used when redundancy is disabled
- Future support for Redis and other distributed storage systems

### 2. SIP Dialog Management

SIP dialog information is tracked to enable recovery:

- Dialog identifiers (Call-ID, From/To tags)
- Sequence numbers
- Contact information
- Routing information

### 3. SIP Recovery Mechanisms

The server supports RFC 7245 recovery mechanisms:

- Processing of Replaces headers in re-INVITE requests
- Session transfer with dialog replacement
- Media reestablishment

### 4. Data Structures

Key data structures for redundancy:

- **DialogInfo**: Stores SIP dialog parameters
- **CallData**: Contains call state, contexts, and recording information
- **RSMetadata**: Specialized SIPREC metadata for session recovery

## Testing Redundancy

You can test the redundancy feature by:

1. Starting the SIPREC server with redundancy enabled
2. Establishing a recording session
3. Simulating a network failure or restarting the server
4. Reconnecting the client with a Replaces header
5. Verifying that the recording continues with the same session ID

## Troubleshooting

Common issues and solutions:

1. **Sessions not recovering**:
   - Ensure redundancy is enabled
   - Check session timeout values
   - Verify client is sending correct Replaces header

2. **Media streams not resuming**:
   - Check if RTP ports are properly allocated
   - Verify media stream types are correctly recovered
   - Ensure client is sending compatible SDP

3. **Session store issues**:
   - For in-memory storage, ensure server has sufficient memory
   - For future distributed storage, check connectivity to the storage system

## Conclusion

The session redundancy feature significantly enhances the reliability of the SIPREC server, making it suitable for deployment in critical environments where call recording continuity is essential. By implementing RFC 7245 and related standards, the server ensures interoperability with compliant SIP clients and systems.