# SIPREC Server

> Enterprise-grade SIP recording service with advanced speech-to-text, real-time analytics, PII redaction, and multi-cloud storage.

## Overview

This project implements a production-ready SIPREC-compliant recording endpoint with comprehensive enterprise features. The server handles RFC 7865/7866 metadata parsing, multi-vendor speech-to-text streaming, real-time analytics, PII detection/redaction, encryption, and multi-cloud storage—all within a single lightweight process.

**Version:** 0.0.34

## Core Features

### SIP & SIPREC Protocol
- **RFC 7865/7866 Compliance** – Full SIPREC metadata parsing and validation with enhanced interoperability
- **Custom SIP Stack** – UDP, TCP, and TLS transports with automatic NAT traversal
- **Large Payload Support** – 4096-byte MTU for handling extensive metadata
- **Session Management** – In-memory or Redis-backed session persistence with automatic failover

### Audio & Media Processing
- **Multi-Codec Support** – PCMU, PCMA, G.722, Opus, EVS with automatic transcoding
- **Audio Processing Pipeline** – Voice Activity Detection (VAD), noise reduction, echo cancellation
- **RTP/SRTP Handling** – Secure media transport with SRTP encryption support
- **Audio Quality Metrics** – ITU-T G.107 E-model for MOS score calculation
- **Multi-Channel Recording** – Stereo enhancement, channel separation, and mixing

### Speech-to-Text (STT)
- **7 Provider Support** – Google, Deepgram, Azure, Amazon, OpenAI, Speechmatics, ElevenLabs
- **Circuit Breaker Protection** – Automatic failover and health monitoring for all STT providers
- **Language-Based Routing** – Intelligent provider selection based on detected language
- **Local & Remote Whisper CLI** – Optional on-prem transcription via the open-source [openai/whisper](https://github.com/openai/whisper) binary (run it locally or point SIPREC at a remote SSH/HTTP wrapper; see [Whisper Setup Guide](docs/whisper-setup.md))
- **Real-time Streaming** – Live transcription delivery via WebSocket and AMQP publishers (see [real-time transcription docs](docs/realtime-transcription.md) for message formats)
- **Async Processing** – Queue-based transcription with configurable workers and retries

### Security & Compliance
- **End-to-End Encryption** – AES-256-GCM and ChaCha20-Poly1305 for recordings and metadata
- **Automatic Key Rotation** – Configurable key rotation intervals with secure storage
- **PII Detection & Redaction** – SSN, credit cards, phone numbers, email addresses
- **Encrypted Recording Pipeline** – Streams media through AES-256-GCM into `.siprec` containers with per-recording key metadata
- **PCI DSS Compliance Mode** – Automatic security hardening and required safeguards
- **GDPR Tools** – Data export and erasure APIs with audit trails
- **TLS Support** – Secure SIP signaling with configurable certificates
- **Authentication** – JWT tokens and API key authentication with role-based access

### Analytics & Monitoring
- **Real-Time Analytics** – Sentiment analysis, keyword extraction, compliance monitoring
- **Elasticsearch Integration** – Full-text search and analytics persistence
- **Audio Quality Tracking** – Real-time MOS scoring and packet loss detection
- **Prometheus Metrics** – Comprehensive metrics for SIP, RTP, STT, and AMQP
- **OpenTelemetry Tracing** – Distributed tracing for end-to-end visibility
- **Performance Monitoring** – Memory, CPU, and goroutine leak detection with auto-tuning

### Storage & Messaging
- **Multi-Cloud Storage** – AWS S3, Google Cloud Storage, Azure Blob Storage
- **Recording Management** – Automatic archival with lifecycle policies
- **AMQP/RabbitMQ** – Real-time transcription delivery with batching and retries
- **Multi-Endpoint Fan-Out** – Publish to multiple message queues simultaneously
- **MySQL/MariaDB** – Optional database persistence for sessions, transcriptions, and CDRs

### Operational Features
- **Pause/Resume API** – Control recording and transcription mid-call via REST API
- **Health & Readiness** – Kubernetes-compatible health probes
- **Graceful Shutdown** – Proper cleanup of active sessions and connections
- **Hot-Reload Configuration** – Dynamic configuration updates without restart
- **Call Detail Records** – Comprehensive CDR generation and storage
- **Multi-Channel Alerting** – Email, Slack, webhook notifications
- **Centralized Warnings** – System-wide warning collection and deduplication

## Quick Start

### Build & Run

```bash
git clone https://github.com/loreste/siprec.git
cd siprec

# Run with default configuration (SIP on 0.0.0.0:5060, HTTP on :8080)
go run ./cmd/siprec

# Or build the binary
go build -o siprec ./cmd/siprec
./siprec
```

### Docker Deployment

```bash
# Using docker-compose with RabbitMQ, Redis, and PostgreSQL
docker-compose up -d

# Or standalone container
docker build -t siprec .
docker run -p 5060:5060/udp -p 8080:8080 siprec
```

## Configuration

The server is configured via environment variables. See `.env.example` for a complete list.

### Essential Variables

| Variable | Description | Default |
| --- | --- | --- |
| `SIP_HOST` | Bind address for SIP listeners | `0.0.0.0` |
| `PORTS` | Comma-separated SIP ports (UDP/TCP) | `5060` |
| `HTTP_PORT` | HTTP server port | `8080` |
| `RECORDING_DIR` | Recording output directory | `./recordings` |

### Network & NAT

| Variable | Description | Default |
| --- | --- | --- |
| `BEHIND_NAT` | Enable NAT rewriting | `false` |
| `EXTERNAL_IP` | Public IP or `auto` for STUN discovery | `auto` |
| `STUN_SERVER` | STUN server for IP detection | `stun.l.google.com:19302` |
| `RTP_PORT_MIN` | Minimum RTP port | `10000` |
| `RTP_PORT_MAX` | Maximum RTP port | `20000` |
| `RTP_TIMEOUT` | RTP inactivity timeout before a call is dropped | `30s` |
| `RTP_BIND_IP` | Specific IP address to bind RTP listener to (empty = all interfaces) | `` |
| `ENABLE_SRTP` | Enable SRTP support | `false` |

### Speech-to-Text

| Variable | Description | Default |
| --- | --- | --- |
| `DEFAULT_SPEECH_VENDOR` | Default STT provider | `google` |
| `STT_SUPPORTED_VENDORS` | Comma-separated list of vendors | `google,deepgram` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to Google credentials | - |
| `DEEPGRAM_API_KEY` | Deepgram API key | - |
| `AZURE_SPEECH_KEY` | Azure Speech key | - |
| `AWS_ACCESS_KEY_ID` | AWS credentials for Transcribe | - |

### Security & Compliance

| Variable | Description | Default |
| --- | --- | --- |
| `ENABLE_TLS` | Enable TLS for SIP | `false` |
| `TLS_CERT_FILE` | Path to TLS certificate | - |
| `TLS_KEY_FILE` | Path to TLS private key | - |
| `ENABLE_RECORDING_ENCRYPTION` | Encrypt recordings | `false` |
| `ENCRYPTION_ALGORITHM` | Encryption algorithm | `aes-256-gcm` |
| `PII_DETECTION_ENABLED` | Enable PII detection | `false` |
| `PII_ENABLED_TYPES` | Comma-separated types | `ssn,credit_card,phone,email` |
| `PCI_COMPLIANCE_MODE` | Enable PCI DSS mode | `false` |

### Storage

| Variable | Description | Default |
| --- | --- | --- |
| `STORAGE_ENABLED` | Enable cloud storage | `false` |
| `S3_ENABLED` | Enable S3 upload | `false` |
| `S3_BUCKET` | S3 bucket name | - |
| `GCS_ENABLED` | Enable GCS upload | `false` |
| `GCS_BUCKET` | GCS bucket name | - |
| `AZURE_STORAGE_ENABLED` | Enable Azure upload | `false` |
| `AZURE_STORAGE_ACCOUNT` | Azure storage account | - |

### Messaging

| Variable | Description | Default |
| --- | --- | --- |
| `AMQP_URL` | RabbitMQ connection URL | - |
| `AMQP_QUEUE_NAME` | Queue for transcriptions | - |
| `ENABLE_REALTIME_AMQP` | Enable realtime delivery | `false` |
| `PUBLISH_PARTIAL_TRANSCRIPTS` | Publish partial results | `true` |
| `PUBLISH_FINAL_TRANSCRIPTS` | Publish final results | `true` |

### Database

| Variable | Description | Default |
| --- | --- | --- |
| `DATABASE_ENABLED` | Enable MySQL persistence | `false` |
| `MYSQL_HOST` | MySQL host | `localhost` |
| `MYSQL_PORT` | MySQL port | `3306` |
| `MYSQL_DATABASE` | Database name | `siprec` |
| `MYSQL_USER` | Database user | - |
| `MYSQL_PASSWORD` | Database password | - |

### Analytics

| Variable | Description | Default |
| --- | --- | --- |
| `ANALYTICS_ENABLED` | Enable analytics pipeline | `false` |
| `ELASTICSEARCH_ADDRESSES` | Elasticsearch endpoints | - |
| `ELASTICSEARCH_INDEX` | Index for analytics | `siprec-analytics` |

#### Enabling Sentiment & Analytics

1. Turn on the dispatcher and persistence layer:
   ```bash
   export ANALYTICS_ENABLED=true
   export ELASTICSEARCH_ADDRESSES=https://es.example.com:9200
   export ELASTICSEARCH_INDEX=call-analytics
   ```
   Supplying credentials/timeouts via the matching environment variables allows the server to persist every analytics snapshot (sentiment trend, compliance flags, agent metrics) to Elasticsearch.
2. Enable realtime fan-out if you need live dashboards or queue-based consumers:
   ```bash
   export ENABLE_REALTIME_AMQP=true
   export PUBLISH_SENTIMENT_UPDATES=true   # already true by default
   export PUBLISH_KEYWORD_DETECTIONS=true  # default true
   ```
3. (Optional) Expose `/ws/analytics` by keeping `ANALYTICS_ENABLED=true`; the HTTP server automatically provisions the WebSocket endpoint alongside the dispatcher.

Once enabled, each transcription chunk carries a sentiment payload computed by the built-in analyzer (lexicon + context window + punctuation/intensifier heuristics, with negation handling). The analytics pipeline tracks per-speaker polarity, emits emotion/subjectivity hints, and publishes confidence scores in three places simultaneously:
- `ws://<host>/ws/analytics` WebSocket stream
- AMQP realtime exchange/queue when `ENABLE_REALTIME_AMQP=true`
- Elasticsearch documents in the configured index for historical reporting

## HTTP API Endpoints

### Health & Metrics

- `GET /health` – Aggregate health state (200 if healthy)
- `GET /health/live` – Liveness probe (always returns 200)
- `GET /health/ready` – Readiness probe (fails if dependencies unavailable)
- `GET /metrics` – Prometheus metrics
- `GET /status` – Status with uptime and version info

### Real-Time Transcription

- `GET /ws` – WebSocket endpoint for live transcription streaming
- `GET /ws/analytics` – WebSocket endpoint for real-time analytics

#### Failure Handling

- Recording to disk is fully independent from the STT pipeline. If a provider crashes or is misconfigured, the server logs `STT provider exited early; transcription will be disabled`, keeps writing audio to the `.wav/.siprec` file, and tears down analytics as soon as the BYE is processed.
- After a provider failure, no further transcription events are published for that call (analytics snapshot and call cleanup still complete), so dashboards will show a recording with missing transcripts instead of an empty file.

#### Recording Format

- Every SIPREC stream is persisted as `<Call-ID>_<stream-label>.wav`, so multi-stream calls produce one file per stream (e.g., `B2B.123_leg0.wav` for the caller and `B2B.123_leg1.wav` for the callee).
- When your SBC or PBX mixes both legs into a single multi-channel RTP stream (e.g., `rtpmap:96 opus/48000/2`), the recorder preserves that layout: channel 0 stays the caller, channel 1 stays the callee, and you get a single stereo WAV with intact separation.
- No extra flags are required—channel counts are learned from the SDP offer—just ensure the upstream recorder advertises the desired `/2` channel count so the SIPREC server keeps both legs in one file.
- If the SRC sends **separate** audio streams (most SIPREC implementations), enable `RECORDING_COMBINE_LEGS=true` (default) to automatically merge all legs into `<Call-ID>.wav` with each leg occupying its own channel. Individual leg files remain on disk for debugging.

### Pause/Resume Control

- `POST /api/pause/:callUUID` – Pause recording/transcription for specific call
- `POST /api/resume/:callUUID` – Resume recording/transcription
- `POST /api/pause/all` – Pause all active sessions
- `POST /api/resume/all` – Resume all paused sessions
- `GET /api/status/:callUUID` – Get pause/resume status

### Session Management

- `GET /api/sessions` – List all active sessions
- `GET /api/sessions/:id` – Get session details
- `DELETE /api/sessions/:id` – Terminate session

### GDPR Compliance

- `POST /api/compliance/export` – Export user data
- `POST /api/compliance/erase` – Erase user data (removes local `.wav/.siprec` artifacts and every uploaded copy recorded in the `.locations` manifest)

Every recording that is uploaded to remote storage now has a sidecar `<recording>.locations` file listing the exact URLs that were written (e.g., `s3://bucket/prefix/file.siprec`). The GDPR erase workflow reads that manifest, issues deletes against each backend, and then removes both the manifest and the encrypted object so that nothing remains online.

## Architecture

```
┌─────────────────┐      ┌──────────────────┐
│  SIP Endpoint   │─────▶│  SIPREC Server   │
│  (PBX/SBC)      │      │  (This Project)  │
└─────────────────┘      └──────────────────┘
                                │
                ┌───────────────┼───────────────┐
                │               │               │
         ┌──────▼──────┐ ┌─────▼──────┐ ┌─────▼──────┐
         │ STT Provider│ │   Storage  │ │  Message   │
         │ (7 options) │ │ (S3/GCS)   │ │   Queue    │
         └─────────────┘ └────────────┘ └────────────┘
                │                            │
         ┌──────▼──────┐              ┌─────▼──────┐
         │  Analytics  │              │ WebSocket  │
         │(Elasticsearch)│            │  Clients   │
         └─────────────┘              └────────────┘
```

## Development

### Requirements

- Go 1.23 or newer
- Optional: Docker, RabbitMQ, Redis, MySQL, Elasticsearch

### Build Tags

- `mysql` – Include MySQL/MariaDB support (requires build tag)

```bash
# Build with MySQL support
go build -tags mysql -o siprec ./cmd/siprec

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run integration tests (requires credentials)
go test -tags integration ./pkg/stt/...

# Validate SIPREC leg merging pipeline
go test ./pkg/sip -run TestCombineRecordingLegs
```

### Project Structure

```
siprec/
├── cmd/siprec/          # Main application entry point
├── pkg/
│   ├── alerting/        # Multi-channel alerting system
│   ├── audio/           # Audio processing algorithms
│   ├── auth/            # Authentication and authorization
│   ├── backup/          # Multi-cloud storage backends
│   ├── cdr/             # Call Detail Records
│   ├── circuitbreaker/  # Circuit breaker for STT resilience
│   ├── compliance/      # PCI DSS and GDPR tools
│   ├── config/          # Configuration management
│   ├── database/        # MySQL/MariaDB integration
│   ├── elasticsearch/   # Analytics persistence
│   ├── encryption/      # End-to-end encryption
│   ├── errors/          # Error handling utilities
│   ├── http/            # HTTP server and API handlers
│   ├── media/           # RTP/SRTP and audio processing
│   ├── messaging/       # AMQP/RabbitMQ client
│   ├── metrics/         # Prometheus metrics
│   ├── performance/     # Performance monitoring
│   ├── pii/             # PII detection and redaction
│   ├── realtime/        # Real-time analytics pipeline
│   ├── security/        # Security and audit logging
│   ├── session/         # Session management and Redis
│   ├── sip/             # SIP server and handler
│   ├── siprec/          # SIPREC metadata parsing
│   ├── stt/             # Speech-to-text providers
│   ├── telemetry/       # OpenTelemetry tracing
│   ├── util/            # Utility functions
│   ├── version/         # Version management
│   └── warnings/        # Warning collection system
├── docs/                # Additional documentation
└── examples/            # Example configurations
```

## Documentation

- [Configuration Guide](docs/configuration.md)
- [Speech-to-Text Integration](docs/stt.md)
- [Real-Time Transcription](docs/realtime-transcription.md)
- [API Reference](docs/api.md)
- [Session Management](docs/sessions.md)
- [End-to-End Feature Guide](docs/end-to-end.md)
- [Whisper Setup Guide](docs/whisper-setup.md)
- [CHANGELOG](CHANGELOG.md)

## Troubleshooting

### Empty or Silent WAV Files

If recordings contain no audio or are unexpectedly small:

**1. Check RTP Timeout Settings**

The server may be timing out before receiving RTP packets. Symptoms include logs showing:
```
RTP timeout detected - closing forwarder
```

**Solution**: Increase the RTP timeout to accommodate network conditions:
```bash
# Default is 30s, try increasing for unreliable networks
RTP_TIMEOUT=60s  # or 90s, 120s depending on needs
```

**2. Verify RTP Packets Are Reaching the Server**

Check logs for:
```
First RTP packet received successfully
```

If you see warnings about no RTP packets:
- Verify firewall rules allow UDP traffic on your RTP port range (`RTP_PORT_MIN` to `RTP_PORT_MAX`)
- Check NAT/routing configuration
- Ensure the SIP client is sending RTP to the correct IP address

**3. Network Interface Binding Issues**

By default, the server binds to all interfaces (`0.0.0.0`). If you have multiple network interfaces and RTP packets aren't being received:

```bash
# Bind to a specific interface
RTP_BIND_IP=192.168.1.100  # Your server's IP address
```

**4. Enable Diagnostic Logging**

The server logs detailed RTP timeout information at 50% threshold:
```
RTP stream inactive - no packets received for extended period
```

This helps identify whether the issue is:
- No packets arriving at all (firewall/routing issue)
- Intermittent packet loss (network quality issue)
- Premature timeout (configuration issue)

### NAT and Firewall Configuration

For servers behind NAT or firewalls:

```bash
# Enable NAT handling
BEHIND_NAT=true

# Set your public IP (or use 'auto' for STUN detection)
EXTERNAL_IP=auto
STUN_SERVER=stun.l.google.com:19302

# Ensure RTP port range is open in firewall
# Default range: 10000-20000 UDP
```

### High Latency or Packet Loss Networks

For deployments with unreliable network conditions:

```bash
# Increase RTP timeout
RTP_TIMEOUT=90s

# Consider wider port range for better allocation
RTP_PORT_MIN=10000
RTP_PORT_MAX=30000
```

## Performance

- **Concurrent Calls**: Tested with 1000+ simultaneous sessions
- **Latency**: Sub-50ms for SIP signaling, <100ms for STT streaming
- **Memory**: ~2-3 MB per active session (with audio processing)
- **Throughput**: 10,000+ RTP packets/sec per core

## Compliance & Security

- RFC 7865/7866 (SIPREC) compliant
- PCI DSS Level 1 compatible (with encryption and PII redaction)
- GDPR compliant with data export and erasure tools
- TLS 1.2+ for SIP signaling
- SRTP for media encryption
- AES-256-GCM for recording encryption

## License

GPL-3.0 – see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please open an issue or pull request on GitHub.

## Support

- Issues: https://github.com/loreste/siprec/issues
- Documentation: https://github.com/loreste/siprec/tree/main/docs
