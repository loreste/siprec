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

## Recording

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_DIR` | Folder for recorded media | `./recordings` |
| `RECORDING_MAX_DURATION` | Max duration per call | `4h` |
| `RECORDING_COMBINE_LEGS` | Merge SIPREC legs into one multi-channel WAV | `true` |

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

