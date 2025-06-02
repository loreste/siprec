# SIPREC Protocol Implementation

Complete implementation of Session Recording Protocol (SIPREC) as defined in RFC 7865 and RFC 7866, with optimized TCP transport for large metadata processing.

## Overview

SIPREC (Session Recording Protocol) enables recording of media sessions by establishing a separate recording session between a Session Recording Client (SRC) and a Session Recording Server (SRS).

## Protocol Flow

```
┌─────────┐          ┌─────────┐          ┌─────────┐
│ Party A │          │   SRC   │          │   SRS   │
└────┬────┘          └────┬────┘          └────┬────┘
     │                    │                     │
     │ INVITE (SDP)       │                     │
     │───────────────────>│                     │
     │                    │ INVITE (SIPREC)     │
     │                    │────────────────────>│
     │                    │                     │
     │                    │ 200 OK (SDP)        │
     │                    │<────────────────────│
     │ 200 OK (SDP)       │                     │
     │<───────────────────│ ACK                 │
     │                    │────────────────────>│
     │ ACK                │                     │
     │───────────────────>│                     │
     │                    │                     │
     │◄═══════RTP════════►│◄═══════RTP════════►│
     │                    │                     │
```

## Message Format

### SIPREC INVITE

The SIPREC INVITE contains multipart MIME body:

```
Content-Type: multipart/mixed;boundary=foobar

--foobar
Content-Type: application/sdp

v=0
o=- 1234567890 1 IN IP4 192.168.1.100
s=SIPREC Recording Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 30000 RTP/AVP 0 8 18
a=sendonly
a=label:1

--foobar
Content-Type: application/rs-metadata+xml

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:siprec">
  <datamode>complete</datamode>
  <session id="session-123">
    <start-time>2024-01-15T10:00:00Z</start-time>
  </session>
  <participant id="part-1">
    <nameID aor="sip:alice@example.com"/>
    <send>session-123</send>
  </participant>
  <participant id="part-2">
    <nameID aor="sip:bob@example.com"/>
    <send>session-123</send>
  </participant>
  <stream id="stream-1" session="session-123">
    <label>1</label>
    <mode>separate</mode>
  </stream>
</recording>
--foobar--
```

## Metadata Format

The RS-Metadata XML provides recording context:

### Elements

- **recording**: Root element
  - **datamode**: "complete" or "partial"
  - **session**: Recording session information
    - **id**: Unique session identifier
    - **start-time**: Session start timestamp
    - **stop-time**: Session end timestamp (optional)
  
- **participant**: Information about each participant
  - **id**: Unique participant identifier
  - **nameID**: Participant identity
    - **aor**: Address of Record (SIP URI)
    - **name**: Display name (optional)
  
- **stream**: Media stream information
  - **id**: Unique stream identifier
  - **session**: Associated session ID
  - **label**: SDP label reference
  - **mode**: "separate" or "mixed"

## Transport Optimization

### TCP Transport for Large Metadata

The SIPREC implementation includes specific optimizations for handling large metadata payloads over TCP:

**Enhanced Message Parsing**:
- Proper CRLF (`\r\n`) line ending handling
- Line-by-line header parsing using `bufio.Reader`
- Content-Length based body reading for multipart messages
- Support for metadata payloads exceeding 1KB

**Connection Management**:
- Persistent TCP connections for multiple SIPREC sessions
- Connection timeout and activity tracking
- Graceful handling of connection lifecycle
- Concurrent processing of multiple connections

**Large Payload Support**:
- Streaming multipart message parsing
- Memory-efficient handling of large XML metadata
- Proper boundary detection in multipart MIME
- Robust error handling for malformed messages

### Protocol Transport Support

| Transport | Status | Use Case |
|-----------|--------|----------|
| UDP | ✅ Supported | Small metadata, legacy compatibility |
| TCP | ✅ Optimized | Large metadata, reliable transport |
| TLS | ✅ Supported | Secure environments, encrypted metadata |

## Implementation Details

### Session Management

Sessions are stored in a sharded map for high concurrency:

```go
type RecordingSession struct {
    ID            string
    CallID        string
    Participants  []Participant
    Streams       []MediaStream
    StartTime     time.Time
    Status        SessionStatus
    Metadata      *SIPRECMetadata
}
```

### Metadata Parsing

XML metadata is parsed and validated:

```go
func ParseSIPRECMetadata(data []byte) (*SIPRECMetadata, error) {
    var metadata SIPRECMetadata
    if err := xml.Unmarshal(data, &metadata); err != nil {
        return nil, fmt.Errorf("failed to parse metadata: %w", err)
    }
    
    // Validate required fields
    if err := metadata.Validate(); err != nil {
        return nil, fmt.Errorf("invalid metadata: %w", err)
    }
    
    return &metadata, nil
}
```

### Media Handling

Each recording session establishes RTP streams:

1. **Stream Setup**
   - Parse SDP offer
   - Allocate RTP ports
   - Generate SDP answer
   
2. **Media Flow**
   - Receive RTP packets
   - Demultiplex by SSRC
   - Forward to audio processor
   
3. **Stream Teardown**
   - Stop RTP reception
   - Release ports
   - Clean up resources

## Compliance

### RFC 7865 Compliance

- ✅ SIPREC INVITE handling
- ✅ Multipart MIME support
- ✅ SDP offer/answer
- ✅ Media stream setup
- ✅ Session teardown

### RFC 7866 Compliance

- ✅ RS-Metadata parsing
- ✅ Participant tracking
- ✅ Stream association
- ✅ Timestamp handling
- ✅ Extension support

## Error Handling

Common error scenarios:

1. **Invalid Metadata**
   - Response: 400 Bad Request
   - Log: Detailed parse error

2. **No Available Ports**
   - Response: 503 Service Unavailable
   - Action: Port pool expansion

3. **Unsupported Codec**
   - Response: 488 Not Acceptable
   - SDP: Only supported codecs

4. **Session Limit**
   - Response: 486 Busy Here
   - Action: Queue or reject

## Best Practices

1. **Session Limits**
   - Set appropriate max sessions
   - Monitor resource usage
   - Implement session queuing

2. **Media Handling**
   - Use appropriate codec priorities
   - Implement jitter buffering
   - Handle packet loss gracefully

3. **Metadata Processing**
   - Validate all fields
   - Handle extensions properly
   - Store for compliance

4. **Error Recovery**
   - Implement retry logic
   - Graceful degradation
   - Proper cleanup

## Testing

Test SIPREC implementation:

```bash
# Send test SIPREC INVITE
./test_siprec_invite.sh

# Validate metadata parsing
./validate_siprec.sh

# Load test with multiple sessions
./test_siprec_load.sh -sessions 100
```