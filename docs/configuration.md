# Configuration

All settings are provided through environment variables. The core service only requires SIP bind details; everything else is optional.

## SIP & Networking

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_HOST` | Listen address for SIP (UDP & TCP) | `0.0.0.0` |
| `PORTS` | Comma-separated list of SIP ports | `5060,5061` |
| `BEHIND_NAT` | Enable NAT rewriting (Via/Contact) | `false` |
| `EXTERNAL_IP` | Public IP override or `auto` for STUN | `auto` |
| `STUN_SERVER` | STUN server used when `EXTERNAL_IP=auto` | `stun:stun.l.google.com:19302` |

### RTP Configuration

| Variable | Description | Default |
| --- | --- | --- |
| `RTP_PORT_MIN` / `RTP_PORT_MAX` | RTP port range | `10000-20000` |
| `RTP_TIMEOUT` | RTP inactivity timeout before cleanup | `30s` |
| `RTP_BIND_IP` | Bind RTP listener to specific IP (empty = all interfaces) | `` |

**RTP Timeout Configuration:**

The `RTP_TIMEOUT` setting controls how long the server waits for RTP packets before considering a stream dead. This is useful for handling network issues:

- **Default (30s)**: Works for most deployments
- **Increased timeout (60s-120s)**: For networks with intermittent packet loss or high latency
- **Decreased timeout (10s-20s)**: For environments where quick cleanup is preferred

```bash
# Example: Tolerate longer network interruptions
RTP_TIMEOUT=90s

# Example: Fast cleanup for unstable connections
RTP_TIMEOUT=15s
```

**RTP Interface Binding:**

By default, the RTP listener binds to all network interfaces (`0.0.0.0`), which is the most robust configuration. However, you can bind to a specific interface when needed:

```bash
# Default: Bind to all interfaces (recommended)
# RTP_BIND_IP=  # (empty or not set)

# Bind to specific private IP
RTP_BIND_IP=192.168.1.100

# Bind to specific public IP
RTP_BIND_IP=203.0.113.50
```

**Use cases for specific interface binding:**
- **Security**: Restrict RTP to internal network only
- **Multi-homing**: Server has multiple IPs, bind to specific one
- **Routing**: Force RTP through specific network path
- **Firewall**: Bind to IP with specific firewall rules

**Note**: If an invalid IP is provided, the server will fall back to binding on all interfaces and log a warning.

### SIP Authentication & IP Filtering

The server supports SIP Digest authentication and IP-based access control to restrict which SRC devices can send SIPREC sessions.

#### IP-Based Access Control

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_IP_ACCESS_ENABLED` | Enable IP-based filtering | `false` |
| `SIP_IP_DEFAULT_ALLOW` | Default policy when IP not in any list | `true` |
| `SIP_IP_ALLOWED_IPS` | Comma-separated list of allowed IPs | `` |
| `SIP_IP_ALLOWED_NETWORKS` | Comma-separated list of allowed CIDRs | `` |
| `SIP_IP_BLOCKED_IPS` | Comma-separated list of blocked IPs | `` |
| `SIP_IP_BLOCKED_NETWORKS` | Comma-separated list of blocked CIDRs | `` |

**Evaluation Order:**
1. Check if IP is in blocked list → **DENY**
2. Check if IP is in blocked network → **DENY**
3. Check if IP is in allowed list → **ALLOW**
4. Check if IP is in allowed network → **ALLOW**
5. Apply default policy (`SIP_IP_DEFAULT_ALLOW`)

**Examples:**

```bash
# Whitelist mode: Only allow specific SBCs
SIP_IP_ACCESS_ENABLED=true
SIP_IP_DEFAULT_ALLOW=false
SIP_IP_ALLOWED_IPS=192.168.1.10,192.168.1.11
SIP_IP_ALLOWED_NETWORKS=10.0.0.0/8

# Blacklist mode: Block known bad actors
SIP_IP_ACCESS_ENABLED=true
SIP_IP_DEFAULT_ALLOW=true
SIP_IP_BLOCKED_IPS=203.0.113.50
SIP_IP_BLOCKED_NETWORKS=198.51.100.0/24

# Production example: Allow internal networks only
SIP_IP_ACCESS_ENABLED=true
SIP_IP_DEFAULT_ALLOW=false
SIP_IP_ALLOWED_NETWORKS=10.0.0.0/8,172.16.0.0/12,192.168.0.0/16
```

#### SIP Digest Authentication

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_AUTH_ENABLED` | Enable SIP Digest authentication | `false` |
| `SIP_AUTH_REALM` | Authentication realm | `siprec.local` |
| `SIP_AUTH_NONCE_TIMEOUT` | Nonce validity in seconds | `300` |
| `SIP_AUTH_USERS` | Comma-separated user:password pairs | `` |

**Example:**

```bash
# Enable digest authentication
SIP_AUTH_ENABLED=true
SIP_AUTH_REALM=mycompany.com
SIP_AUTH_USERS=sbc1:secretpass1,sbc2:secretpass2
```

**Combined Example (IP filtering + Digest auth):**

```bash
# Defense in depth: IP whitelist + digest auth
SIP_IP_ACCESS_ENABLED=true
SIP_IP_DEFAULT_ALLOW=false
SIP_IP_ALLOWED_NETWORKS=10.0.0.0/8
SIP_AUTH_ENABLED=true
SIP_AUTH_REALM=secure.example.com
SIP_AUTH_USERS=sbc-primary:$(cat /run/secrets/sbc_password)
```

## Recording

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_DIR` | Folder for recorded media | `./recordings` |
| `RECORDING_MAX_DURATION` | Max duration per call | `4h` |
| `RECORDING_COMBINE_LEGS` | Merge SIPREC legs into one multi-channel WAV | `true` |

### Audio Format Configuration

The server supports multiple audio output formats via FFmpeg encoding. By default, recordings are saved as WAV files, but you can configure automatic conversion to compressed formats.

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_FORMAT` | Output format: `wav`, `mp3`, `opus`, `ogg`, `mp4`, `m4a`, `flac` | `wav` |
| `RECORDING_MP3_BITRATE` | MP3 bitrate in kbps | `128` |
| `RECORDING_OPUS_BITRATE` | Opus bitrate in kbps | `64` |
| `RECORDING_QUALITY` | Quality setting 1-10 (higher = better) | `5` |

**Supported Formats:**

| Format | Codec | Use Case |
| --- | --- | --- |
| `wav` | PCM | Lossless, maximum compatibility |
| `mp3` | LAME MP3 | Good compression, universal playback |
| `opus` | Opus | Excellent compression, VoIP optimized |
| `ogg` | Opus in OGG | Opus in OGG container |
| `mp4` | AAC | Modern container format |
| `m4a` | AAC | Apple-compatible audio |
| `flac` | FLAC | Lossless compression |

**Requirements:**
- FFmpeg must be installed and available in PATH for non-WAV formats
- If FFmpeg is not available, the server falls back to WAV format

**Examples:**
```bash
# High-quality MP3 recordings
RECORDING_FORMAT=mp3
RECORDING_MP3_BITRATE=192
RECORDING_QUALITY=8

# Bandwidth-efficient Opus recordings
RECORDING_FORMAT=opus
RECORDING_OPUS_BITRATE=48
RECORDING_QUALITY=7

# Lossless FLAC compression
RECORDING_FORMAT=flac
RECORDING_QUALITY=8
```

### Per-Call Timeout Configuration

In addition to global timeout settings, the server supports per-call timeout overrides via SIP headers or SIPREC metadata. This allows SRC devices to specify custom timeouts for specific recordings.

**SIP Headers for Per-Call Timeouts:**

| Header | Description | Example |
| --- | --- | --- |
| `X-Recording-Timeout` | RTP inactivity timeout for this call | `X-Recording-Timeout: 60s` |
| `X-Recording-Max-Duration` | Maximum recording duration for this call | `X-Recording-Max-Duration: 2h` |
| `X-Recording-Retention` | Retention period for this recording | `X-Recording-Retention: 30d` |

**SIPREC Metadata:**

Per-call timeouts can also be specified in the SIPREC metadata XML:

```xml
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <session>
    <siprecTimeout>90s</siprecTimeout>
    <siprecMaxDuration>1h</siprecMaxDuration>
    <siprecRetention>7d</siprecRetention>
  </session>
</recording>
```

**Priority Order:**
1. SIP headers (highest priority)
2. SIPREC metadata
3. Global configuration (lowest priority)

**Example: Per-Call Override via SIP INVITE:**
```
INVITE sip:recorder@192.168.1.100:5060 SIP/2.0
Via: SIP/2.0/UDP 192.168.1.50:5060
From: <sip:src@192.168.1.50>;tag=abc123
To: <sip:recorder@192.168.1.100>
Call-ID: call-12345@192.168.1.50
X-Recording-Timeout: 120s
X-Recording-Max-Duration: 30m
Content-Type: multipart/mixed;boundary=boundary1
```

## Session Persistence

To persist session data across restarts, initialise a session manager store (e.g. Redis) and pass it to the SIP handler through code:

```go
redisStore, _ := session.NewRedisSessionStore(redisConfig, logger)

handlerConfig := &sip.Config{
    SessionStore:  redisStore,
    SessionNodeID: "recorder-1",
}
handler, _ := sip.NewHandler(logger, handlerConfig, sttManager)
```

Redis connection parameters can be set via:

| Variable | Description | Default |
| --- | --- | --- |
| `REDIS_ENABLED` | Enable Redis-backed session store | `false` |
| `REDIS_ADDRESS` | Redis endpoint | `localhost:6379` |
| `REDIS_PASSWORD` | Password (optional) | `""` |
| `REDIS_DATABASE` | Database index | `0` |
| `REDIS_SESSION_TTL` | TTL for stored sessions | `24h` |

## High Availability & Redundancy

The server supports session redundancy to handle failovers without losing call state. This allows a backup instance to take over if the primary fails.

| Variable | Description | Default |
| --- | --- | --- |
| `ENABLE_REDUNDANCY` | Enable session redundancy | `true` |
| `REDUNDANCY_STORAGE_TYPE` | Storage backend (`memory`, `redis`) | `memory` |
| `SESSION_TIMEOUT` | Time until an inactive session is considered dead | `30s` |
| `SESSION_CHECK_INTERVAL` | Frequency of stale session checks | `10s` |

> **Note**: For production HA, set `REDUNDANCY_STORAGE_TYPE=redis` so that multiple instances can share session state.

## Speech-to-Text (Optional)

The project exposes hooks for a provider manager. Each provider has its own credentials (see `pkg/stt`). Typical variables include:

| Variable | Description |
| --- | --- |
| `DEFAULT_SPEECH_VENDOR` | Preferred provider (e.g. `google`) |
| `SUPPORTED_VENDORS` | Comma-separated provider list |
| Provider-specific keys | e.g. Google service-account JSON, Deepgram API key |

Leave the manager unset to run the recorder without STT streaming.

### Whisper CLI (on-prem)

| Variable | Description | Default |
| --- | --- | --- |
| `WHISPER_ENABLED` | Enable the `whisper` vendor | `false` |
| `WHISPER_BINARY_PATH` | CLI path (e.g. `/usr/local/bin/whisper`) | `whisper` |
| `WHISPER_MODEL` | Model name (`tiny`, `base`, `small`, `medium`, `large`) | `base` |
| `WHISPER_TASK` | `transcribe` or `translate` | `transcribe` |
| `WHISPER_TRANSLATE` | Force translate mode (overrides task) | `false` |
| `WHISPER_LANGUAGE` | Force language (empty for auto-detect) | `""` |
| `WHISPER_OUTPUT_FORMAT` | CLI output format (`json`, `txt`, `srt`, `vtt`, `tsv`, `verbose_json`) | `json` |
| `WHISPER_SAMPLE_RATE` | Sample rate for the buffered WAV | `16000` |
| `WHISPER_CHANNELS` | Channel count for the WAV | `1` |
| `WHISPER_TIMEOUT` | Maximum runtime per call | `10m` |
| `WHISPER_MAX_CONCURRENT` | Max concurrent calls (`-1`=auto, `0`=unlimited, `N`=limit) | `-1` |
| `WHISPER_EXTRA_ARGS` | Additional CLI arguments (e.g., `--device cuda --fp16 True`) | `""` |

**Remote Server Example**:
```bash
# Whisper running on separate GPU server
WHISPER_ENABLED=true
WHISPER_BINARY_PATH=/usr/local/bin/whisper-remote  # SSH or HTTP wrapper
WHISPER_MODEL=large
WHISPER_TIMEOUT=20m  # Increased for network latency
WHISPER_MAX_CONCURRENT=16  # GPU server can handle more
```

See [Speech-to-Text Integration](stt.md) for detailed Whisper configuration including GPU acceleration, remote servers, and performance tuning.

## HTTP Server

| Variable | Description | Default |
| --- | --- | --- |
| `HTTP_ENABLED` | Expose health/control endpoints | `true` |
| `HTTP_PORT` | HTTP listen port | `8080` |

Health (`/healthz`) and readiness (`/readyz`) checks automatically reflect the SIP handler and shared session store.

### Rate Limiting

Rate limiting protects the server from DDoS attacks and abuse. Both HTTP API and SIP requests can be rate-limited.

#### HTTP Rate Limiting

| Variable | Description | Default |
| --- | --- | --- |
| `RATE_LIMIT_ENABLED` | Enable HTTP rate limiting | `false` |
| `RATE_LIMIT_RPS` | Requests per second per client | `100` |
| `RATE_LIMIT_BURST` | Maximum burst size | `200` |
| `RATE_LIMIT_BLOCK_DURATION` | How long to block after exceeding limits | `1m` |
| `RATE_LIMIT_WHITELIST_IPS` | IPs/CIDRs that bypass rate limiting | `127.0.0.1,::1` |
| `RATE_LIMIT_WHITELIST_PATHS` | Paths that bypass rate limiting | `/health,/health/live,/health/ready` |

**Example configurations:**

```bash
# Production: Enable rate limiting with standard settings
RATE_LIMIT_ENABLED=true
RATE_LIMIT_RPS=100
RATE_LIMIT_BURST=200
RATE_LIMIT_BLOCK_DURATION=1m

# High-traffic API: Allow more requests
RATE_LIMIT_ENABLED=true
RATE_LIMIT_RPS=500
RATE_LIMIT_BURST=1000
RATE_LIMIT_WHITELIST_IPS=10.0.0.0/8,192.168.0.0/16

# Strict mode: Lower limits for public-facing servers
RATE_LIMIT_ENABLED=true
RATE_LIMIT_RPS=20
RATE_LIMIT_BURST=50
RATE_LIMIT_BLOCK_DURATION=5m
```

#### SIP Rate Limiting

| Variable | Description | Default |
| --- | --- | --- |
| `RATE_LIMIT_SIP_ENABLED` | Enable SIP rate limiting | `false` |
| `RATE_LIMIT_SIP_INVITE_RPS` | INVITE requests per second per IP | `10` |
| `RATE_LIMIT_SIP_INVITE_BURST` | Maximum INVITE burst | `50` |
| `RATE_LIMIT_SIP_RPS` | Other SIP requests per second per IP | `100` |
| `RATE_LIMIT_SIP_REQUEST_BURST` | Maximum general request burst | `200` |

**SIP rate limiting notes:**
- INVITE requests have stricter limits since they consume more resources
- Other methods (BYE, ACK, OPTIONS) use the general request limits
- Whitelisted IPs from HTTP rate limiting also apply to SIP

```bash
# Protect against SIP flooding attacks
RATE_LIMIT_SIP_ENABLED=true
RATE_LIMIT_SIP_INVITE_RPS=10
RATE_LIMIT_SIP_INVITE_BURST=50

# Whitelist trusted SBC IPs
RATE_LIMIT_WHITELIST_IPS=192.168.1.10,192.168.1.11,10.0.0.0/8
```

**Rate limit metrics:**

When rate limiting is enabled, the following Prometheus metrics are exported:

| Metric | Description |
| --- | --- |
| `siprec_rate_limit_requests_total` | Total requests processed by rate limiter |
| `siprec_rate_limit_blocked_total` | Total requests blocked by rate limiter |
| `siprec_rate_limit_bucket_tokens` | Current tokens in rate limit bucket |
| `siprec_sip_rate_limited_total` | SIP requests blocked by rate limiter |

### Request Correlation IDs

Correlation IDs enable distributed tracing and request tracking across the system. Every HTTP and SIP request is assigned a unique correlation ID that flows through the entire request lifecycle.

**Features:**
- Automatic correlation ID generation for all requests
- Support for incoming correlation IDs via standard headers
- Correlation IDs included in all log entries
- Correlation IDs returned in HTTP response headers
- SIP responses include correlation ID in custom header

**HTTP Headers:**
| Header | Description |
| --- | --- |
| `X-Correlation-ID` | Primary correlation ID header (request/response) |
| `X-Request-ID` | Alternative correlation ID header (request/response) |
| `X-Trace-ID` | OpenTelemetry-compatible trace ID (request only) |

**SIP Headers:**
| Header | Description |
| --- | --- |
| `X-Correlation-ID` | Correlation ID for SIP request tracking |

**Usage:**
- Send `X-Correlation-ID` header with requests to use your own correlation ID
- If no correlation ID is provided, one is automatically generated
- Use correlation IDs to trace requests across logs and systems
- Correlation IDs appear in all audit logs for security events

**Example log entry:**
```json
{
  "correlation_id": "1704531234567-a1b2c3d4-0001",
  "client_ip": "192.168.1.100",
  "method": "POST",
  "path": "/api/sessions",
  "status": 200,
  "duration_ms": 45
}
```

## Audio Processing & VAD

Basic audio enhancement can be applied before transcription.

| Variable | Description | Default |
| --- | --- | --- |
| `AUDIO_ENHANCEMENT_ENABLED` | Enable audio enhancement pipeline | `true` |
| `NOISE_SUPPRESSION_ENABLED` | Enable noise suppression | `true` |
| `VAD_THRESHOLD` | Energy threshold for speech detection (0.0-1.0) | `0.3` |
| `NOISE_SUPPRESSION_LEVEL` | Aggressiveness of noise reduction (0.0-1.0) | `0.7` |
| `AGC_ENABLED` | Enable Automatic Gain Control | `true` |
| `ECHO_CANCELLATION_ENABLED` | Enable Acoustic Echo Cancellation | `true` |


## PII Detection & Redaction

The server can detect and redact Personally Identifiable Information (PII) from transcripts and mark it in audio.

| Variable | Description | Default |
| --- | --- | --- |
| `PII_DETECTION_ENABLED` | Master switch for PII subsystem | `false` |
| `PII_ENABLED_TYPES` | Comma-separated types: `ssn,credit_card,phone,email` | `ssn,credit_card` |
| `PII_REDACTION_CHAR` | Character used for masking (e.g. `*` or `#`) | `*` |
| `PII_PRESERVE_FORMAT` | If `true`, keeps separators (e.g. `***-**-1234`) | `true` |
| `PII_APPLY_TO_TRANSCRIPTIONS`| Redact text in real-time streams | `true` |
| `PII_APPLY_TO_RECORDINGS` | Mark audio metadata for post-processing redaction | `false` |


## End-to-End Encryption

Secure your data both in transit and at rest.

### Transport Layer
| Variable | Description | Default |
| --- | --- | --- |
| `ENABLE_TLS` | Enable TLS for SIP signaling | `false` |
| `TLS_CERT_PATH` | Path to server certificate (PEM) | `` |
| `TLS_KEY_PATH` | Path to private key (PEM) | `` |
| `SIP_REQUIRE_TLS` | Reject non-TLS connections | `false` |
| `ENABLE_SRTP` | Enable SRTP for media (SDES key exchange) | `false` |
| `SIP_REQUIRE_SRTP` | Reject non-SRTP media sessions | `false` |

### Storage Layer (At-Rest)
| Variable | Description | Default |
| --- | --- | --- |
| `ENABLE_RECORDING_ENCRYPTION` | Encrypt WAV files before writing to disk | `false` |
| `ENABLE_METADATA_ENCRYPTION` | Encrypt session metadata JSON | `false` |
| `ENCRYPTION_ALGORITHM` | `AES-256-GCM` or `ChaCha20-Poly1305` | `AES-256-GCM` |
| `MASTER_KEY_PATH` | Directory for encryption keys | `./keys` |
| `KEY_ROTATION_INTERVAL` | Time before rotating active key | `24h` |

## Audit Trail & SIP Headers Logging

The server maintains a comprehensive audit trail for compliance and troubleshooting. All SIP-related events now include complete SIP header information for full traceability.

### Audit Events

The following events are automatically logged with SIP header information:

| Event | Description | Headers Included |
| --- | --- | --- |
| `sip.invite.success` | Successful SIPREC session establishment | Full INVITE headers |
| `sip.invite.failure` | Failed session establishment | Full INVITE headers + error details |
| `sip.bye.received` | BYE received from SRC | Full BYE headers |
| `sip.cancel.received` | CANCEL received before session established | Full CANCEL headers |
| `recording.started` | Recording stream started | Session metadata |
| `recording.stopped` | Recording stream stopped | Session metadata + duration |

### Captured SIP Headers

Each audit event captures the following SIP header categories:

**Core Headers:**
- `Method`, `Request-URI`, `From`, `To`, `Call-ID`, `CSeq`, `Via`, `Contact`

**Authentication Headers:**
- `Authorization`, `Proxy-Authorization`, `WWW-Authenticate`

**Routing Headers:**
- `Route`, `Record-Route`

**Session Headers:**
- `Allow`, `Supported`, `Require`, `User-Agent`, `Server`

**Media Headers:**
- `Content-Type`, `Content-Length`, `Accept`

**Transport Info:**
- Transport protocol (UDP/TCP/TLS), Remote address, Local address

**Custom Headers:**
- Any vendor-specific or custom headers (e.g., `X-Recording-*`)

### Audit Log Format

Audit events are logged in structured JSON format with the `audit: true` field for easy filtering:

```json
{
  "audit": true,
  "audit_category": "sip",
  "audit_action": "invite.success",
  "audit_outcome": "success",
  "call_id": "call-12345@192.168.1.50",
  "session_id": "sess-abc123",
  "tenant": "customer-1",
  "timestamp": "2024-01-15T10:30:45.123456789Z",
  "sip_method": "INVITE",
  "sip_from": "<sip:agent@pbx.example.com>;tag=xyz",
  "sip_to": "<sip:recorder@srs.example.com>",
  "sip_via": "SIP/2.0/UDP 192.168.1.50:5060;branch=z9hG4bK-abc",
  "sip_user_agent": "Cisco-Gateway/1.0",
  "sip_remote_addr": "192.168.1.50:5060"
}
```

### Filtering Audit Logs

Use log aggregation tools to filter audit events:

```bash
# Filter all audit events
grep '"audit":true' /var/log/siprec.log

# Filter failed sessions
grep '"audit_outcome":"failure"' /var/log/siprec.log

# Filter by Call-ID
grep '"call_id":"call-12345"' /var/log/siprec.log
```

### Security Note

Authorization headers are automatically redacted in audit logs to prevent credential exposure. The redacted format shows `[REDACTED-<length>]` to indicate the header was present without exposing sensitive data.

