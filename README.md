# IZI SIPREC Server (Golang)

> Open Source SIPREC Session Recording Server written in Go for VoIP and telecom call recording.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg)](LICENSE)
[![SIPREC](https://img.shields.io/badge/SIPREC-RFC%207865%2F7866-green.svg)](https://datatracker.ietf.org/doc/html/rfc7865)
[![Scalability](https://img.shields.io/badge/Scale-100k%2B%20Concurrent-orange.svg)](docs/cluster-configuration.md)

## Overview

IZI SIPREC is an open-source **SIPREC Session Recording Server (SRS)** written in Golang. It receives SIPREC recording sessions from SBCs, PBXs, or SIP proxies and captures the RTP streams for storage, analysis, or compliance recording.

The project is designed for high-performance **telecom recording** environments and can scale to **100,000+ concurrent recordings** with horizontal scaling and Redis-backed session management.

This **Golang SIP server** handles RFC 7865/7866 metadata parsing, multi-vendor **AI-powered speech-to-text**, **speaker diarization**, **lawful intercept**, real-time analytics, PII detection/redaction, encryption, and multi-cloud storage—all within a single lightweight process.

**Version:** 1.2.3

## Use Cases

- **Enterprise Call Recording** – Record all inbound/outbound calls for quality assurance and training
- **Compliance Recording** – Meet regulatory requirements for financial services, healthcare, and legal industries
- **Lawful Intercept Recording** – Support lawful interception requirements with secure, tamper-proof recordings
- **Contact Center Recording** – Capture customer interactions for analytics and compliance
- **SBC Recording Server** – Deploy as the recording endpoint for any SIPREC-compliant Session Border Controller
- **VoIP Recording Platform** – Centralized recording for distributed VoIP infrastructure

## Supported Platforms

Works with any SIPREC-compliant source, including:

- **OpenSIPS SIPREC** – Full compatibility with OpenSIPS SIPREC module
- **Kamailio SIPREC** – Native support for Kamailio's SIPREC implementation
- **FreeSWITCH Recording** – Record calls from FreeSWITCH via SIPREC
- **Asterisk SIPREC** – Compatible with Asterisk's SIPREC capabilities
- **Oracle SBC** – Tested with Oracle Communications Session Border Controller
- **Cisco CUBE** – Works with Cisco Unified Border Element
- **Ribbon/GENBAND SBC** – Full metadata extraction support
- **AudioCodes SBC** – Compatible with AudioCodes Mediant series
- **Avaya SBCE** – Avaya Session Border Controller for Enterprise

## Core Features

### SIP & SIPREC Protocol
- **RFC 7865/7866 Compliance** – Full SIPREC metadata parsing and validation with enhanced interoperability
- **Custom SIP Stack** – UDP, TCP, and TLS transports with automatic NAT traversal
- **Large Payload Support** – 4096-byte MTU for handling extensive metadata
- **Session Management** – In-memory or Redis-backed session persistence with automatic failover
- **Multi-Vendor Support** – Automatic detection and metadata extraction for Oracle, Cisco, Avaya, NICE, Genesys, FreeSWITCH, Asterisk, and OpenSIPS SBCs

### Audio & Media Processing
- **Multi-Codec Support** – PCMU, PCMA, G.722, G.729, Opus, EVS with automatic transcoding
- **Audio Processing Pipeline** – Voice Activity Detection (VAD), noise reduction, echo cancellation
- **RTP/SRTP Handling** – Secure media transport with SRTP encryption support
- **Audio Quality Metrics** – ITU-T G.107 E-model for MOS score calculation
- **Multi-Channel Recording** – Stereo enhancement, channel separation, and mixing
- **Jitter Buffer** – Per-leg RTP packet reordering with configurable buffer size and delay
- **Packet Loss Concealment** – DTX-aware silence insertion for time-accurate recordings with G.729 Annex B support
- **G.729 Per-Stream Decoder** – Isolated decoder per RTP stream eliminates cross-call state leakage
- **G.729 Stability** – Oscillation detection prevents decoder artifacts from corrupting recordings
- **SSRC Validation** – Locks SSRC from first RTP packet, drops mismatched packets to prevent crosstalk
- **SSRC Auto-Correction** – Switches SSRC when locked source goes silent and alternate has sustained traffic
- **Port Allocation Cooldown** – Avoids recently freed ports to prevent stale RTP crosstalk
- **Start-Time Alignment** – Wall-clock synchronized multi-leg WAV combining for accurate stereo output
- **Speaker Diarization** – Automatic speaker separation with voice feature extraction, cross-session speaker tracking, and configurable similarity thresholds

### Speech-to-Text (STT)
- **7 Provider Support** – Google, Deepgram, Azure, Amazon, OpenAI, Speechmatics, ElevenLabs
- **Circuit Breaker Protection** – Automatic failover and health monitoring for all STT providers
- **Language-Based Routing** – Intelligent provider selection based on detected language
- **Local & Remote Whisper CLI** – Optional on-prem transcription via the open-source [openai/whisper](https://github.com/openai/whisper) binary (run it locally or point IZI SIPREC at a remote SSH/HTTP wrapper; see [Whisper Setup Guide](docs/whisper-setup.md))
- **Real-time Streaming** – Live transcription delivery via WebSocket and AMQP publishers (see [real-time transcription docs](docs/realtime-transcription.md) for message formats)
- **Async Processing** – Queue-based transcription with configurable workers and retries

### Security & Compliance
- **End-to-End Encryption** – AES-256-GCM and ChaCha20-Poly1305 for recordings and metadata
- **Automatic Key Rotation** – Configurable key rotation intervals with secure storage
- **PII Detection & Redaction** – SSN, credit cards, phone numbers, email addresses
- **Encrypted Recording Pipeline** – Streams media through AES-256-GCM into `.siprec` containers with per-recording key metadata
- **PCI DSS Compliance Mode** – Automatic security hardening and required safeguards
- **GDPR Tools** – Data export and erasure APIs with audit trails
- **Lawful Intercept Support** – Secure LEA delivery with mutual TLS, warrant verification, encryption, and tamper-proof audit logging
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

### Enterprise Scaling
- **100k+ Concurrent Recordings** – Horizontal scaling with Redis-backed session sharing
- **Worker Pool Management** – Configurable worker pools with automatic CPU-based sizing
- **Memory Management** – Configurable memory limits with automatic garbage collection
- **Node Clustering** – Multi-node deployments with unique node IDs for distributed processing
- **RTP Stream Optimization** – Configurable RTP stream limits (typically 2-3x concurrent calls)

### Operational Features
- **Pause/Resume API** – Control recording and transcription mid-call via REST API
- **Async STT Job API** – Submit, track, and manage queued transcription jobs via `/api/stt/*`
- **Health & Readiness** – Kubernetes-compatible health probes
- **Graceful Shutdown** – Proper cleanup of active sessions and connections
- **Hot-Reload Configuration** – Dynamic configuration updates without restart, managed via `/api/config` endpoints
- **Call Detail Records** – Comprehensive CDR generation and storage
- **Multi-Channel Alerting** – Email (SMTP), Slack, PagerDuty, and webhook notifications with delivery metrics
- **Role-Based Access Control** – Optional RBAC enforcement for API endpoints (`AUTH_RBAC_ENABLED`)
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
# Using docker-compose with RabbitMQ (docker-compose.dev.yml adds Redis)
docker-compose up -d

# Or standalone container
docker build -t siprec .
docker run -p 5060:5060/udp -p 8080:8080 siprec
```

### CLI Tool (siprecctl)

The `siprecctl` command-line tool provides administrative control over the SIPREC server.

```bash
# Build the CLI
go build -o siprecctl ./cmd/siprecctl

# Check server health
siprecctl health

# List active sessions
siprecctl sessions list

# Pause/resume recordings
siprecctl pause <call-id>
siprecctl resume <call-id>

# View resource usage
siprecctl resources

# Lawful intercept management
siprecctl li list
siprecctl li register --warrant W123 --target +15551234567
siprecctl li stats

# Generate example config
siprecctl config generate -f yaml -o config.yaml

# Connect to a different server
siprecctl -s http://192.168.1.100:8080 health
```

**Available Commands:**
| Command | Description |
|---------|-------------|
| `health` | Check server health and dependencies |
| `stats` | Show server statistics |
| `sessions list/get/terminate` | Manage recording sessions |
| `pause/resume <call-id>` | Control recording for a call |
| `pause-all/resume-all` | Control all recordings |
| `resources` | Show resource utilization |
| `config validate/generate/show` | Configuration management |
| `li list/register/revoke/stats/audit` | Lawful intercept operations |

## Configuration

The server supports multiple configuration methods with the following priority:

1. **Environment variables** (highest priority - always override)
2. **Configuration file** (YAML or JSON)
3. **`.env` file** (development)
4. **Default values**

### Configuration File (Production)

For production deployments, use a YAML or JSON configuration file:

```bash
# Option 1: Place config.yaml in the working directory
cp config.example.yaml config.yaml
./siprec

# Option 2: Specify config file via environment variable
export CONFIG_FILE=/etc/siprec/config.yaml
./siprec

# Option 3: Use standard location
cp config.example.yaml /etc/siprec/config.yaml
./siprec
```

The server searches for config files in this order:
- `$CONFIG_FILE` environment variable
- `./config.yaml` or `./config.yml` or `./config.json`
- `/etc/siprec/config.yaml`
- `$HOME/.siprec/config.yaml`

See `config.example.yaml` for a complete configuration reference.

### Environment Variables

Environment variables can override any config file setting. For secrets (API keys, passwords), always use environment variables:

```bash
export DEEPGRAM_API_KEY=your-api-key
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
export REDIS_PASSWORD=your-password
```

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
| `SUPPORTED_VENDORS` | Comma-separated list of vendors | `google,deepgram,elevenlabs,speechmatics,openai` |
| `STT_ASYNC_ENABLED` | Enable the async STT job queue and `/api/stt/*` API | `true` |
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to Google credentials | - |
| `DEEPGRAM_API_KEY` | Deepgram API key | - |
| `AZURE_SPEECH_KEY` | Azure Speech key | - |
| `AWS_ACCESS_KEY_ID` | AWS credentials for Transcribe | - |

### Security & Compliance

| Variable | Description | Default |
| --- | --- | --- |
| `ENABLE_TLS` | Enable TLS for SIP | `false` |
| `TLS_CERT_PATH` | Path to TLS certificate | - |
| `TLS_KEY_PATH` | Path to TLS private key | - |
| `ENABLE_RECORDING_ENCRYPTION` | Encrypt recordings | `false` |
| `ENCRYPTION_ALGORITHM` | Encryption algorithm | `aes-256-gcm` |
| `PII_DETECTION_ENABLED` | Enable PII detection | `false` |
| `PII_ENABLED_TYPES` | Comma-separated types | `ssn,credit_card,phone,email` |
| `COMPLIANCE_PCI_ENABLED` | Enable PCI DSS mode | `false` |

### Authentication & RBAC

| Variable | Description | Default |
| --- | --- | --- |
| `AUTH_ENABLED` | Enable HTTP API authentication (JWT/API keys) | `false` |
| `AUTH_JWT_SECRET` | JWT signing secret (required when auth is enabled) | - |
| `AUTH_ENABLE_API_KEYS` | Allow API key authentication | `true` |
| `AUTH_RBAC_ENABLED` | Enforce role-based access control on API endpoints (requires `AUTH_ENABLED=true` and database persistence) | `false` |

### Storage

| Variable | Description | Default |
| --- | --- | --- |
| `RECORDING_STORAGE_ENABLED` | Enable cloud storage upload | `false` |
| `RECORDING_STORAGE_KEEP_LOCAL` | Keep local copies after upload | `true` |
| `RECORDING_STORAGE_S3_ENABLED` | Enable S3 upload | `false` |
| `RECORDING_STORAGE_S3_BUCKET` | S3 bucket name | - |
| `RECORDING_STORAGE_GCS_ENABLED` | Enable GCS upload | `false` |
| `RECORDING_STORAGE_GCS_BUCKET` | GCS bucket name | - |
| `RECORDING_STORAGE_AZURE_ENABLED` | Enable Azure Blob upload | `false` |
| `RECORDING_STORAGE_AZURE_ACCOUNT` | Azure storage account | - |
| `RECORDING_STORAGE_AZURE_CONTAINER` | Azure blob container | - |

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
| `DATABASE_ENABLED` | Enable MySQL persistence (requires `mysql` build tag) | `false` |
| `DB_HOST` | MySQL host | `localhost` |
| `DB_PORT` | MySQL port | `3306` |
| `DB_NAME` | Database name | `siprec` |
| `DB_USERNAME` | Database user | `siprec` |
| `DB_PASSWORD` | Database password | - |

### Enterprise Scaling

| Variable | Description | Default |
| --- | --- | --- |
| `MAX_CONCURRENT_CALLS` | Maximum concurrent recording sessions | `500` |
| `MAX_RTP_STREAMS` | Maximum RTP streams (typically 2-3x calls) | `1500` |
| `WORKER_POOL_SIZE` | Worker pool size (0 = auto based on CPU) | `0` |
| `MAX_MEMORY_MB` | Maximum memory usage in MB (0 = unlimited) | `0` |
| `HORIZONTAL_SCALING` | Enable horizontal scaling mode | `false` |
| `NODE_ID` | Unique node ID for clustered deployments | - |

### Speaker Diarization

| Variable | Description | Default |
| --- | --- | --- |
| `DIARIZATION_ENABLED` | Enable speaker diarization | `true` |
| `DIARIZATION_MAX_SPEAKERS` | Maximum speakers per session | `10` |
| `DIARIZATION_THRESHOLD` | Speaker similarity threshold (0.0-1.0) | `0.7` |
| `DIARIZATION_VOICE_FEATURES` | Enable voice feature extraction | `true` |
| `DIARIZATION_CROSS_SESSION` | Enable cross-session speaker tracking | `false` |
| `DIARIZATION_PROFILE_RETENTION` | Speaker profile retention in days | `30` |

### Lawful Intercept

| Variable | Description | Default |
| --- | --- | --- |
| `LI_ENABLED` | Enable lawful intercept support | `false` |
| `LI_DELIVERY_ENDPOINT` | Secure LEA delivery endpoint (HTTPS) | - |
| `LI_ENCRYPTION_KEY_PATH` | Path to encryption key for intercepts | - |
| `LI_WARRANT_ENDPOINT` | External warrant verification endpoint | - |
| `LI_AUDIT_LOG_PATH` | Audit log path for intercept operations | `/var/log/siprec/li_audit.log` |
| `LI_MUTUAL_TLS` | Require mutual TLS for LEA delivery | `true` |
| `LI_CLIENT_CERT_PATH` | Client certificate for LEA mTLS | - |
| `LI_CLIENT_KEY_PATH` | Client key for LEA mTLS | - |
| `LI_RETENTION_DAYS` | Intercept data retention in days | `365` |

### Analytics

| Variable | Description | Default |
| --- | --- | --- |
| `ANALYTICS_ENABLED` | Enable analytics pipeline | `false` |
| `ELASTICSEARCH_ADDRESSES` | Elasticsearch endpoints | - |
| `ELASTICSEARCH_INDEX` | Index for analytics | `call-analytics` |

### Alerting

| Variable | Description | Default |
| --- | --- | --- |
| `ALERTING_ENABLED` | Enable the alert manager | `false` |
| `ALERTING_EVALUATION_INTERVAL` | Alert rule evaluation interval | `30s` |

Notifications can be delivered through email (SMTP), Slack, PagerDuty, and generic webhook channels. The email channel sends real SMTP mail and accepts the following channel settings:

| Setting | Description |
| --- | --- |
| `smtp_host` | SMTP server hostname (required) |
| `smtp_port` | SMTP server port (default `587`) |
| `username` / `password` | SMTP authentication credentials (optional) |
| `from` | Sender address (required) |
| `to` | Recipient address or list of addresses (required) |
| `tls_mode` | `auto` (default), `implicit` (SMTPS/465), `starttls`, or `none` |
| `insecure_skip_verify` | Skip TLS certificate verification (not recommended) |
| `timeout_seconds` | SMTP dial/send timeout (default `30`) |

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

- `GET /ws/transcriptions` – WebSocket endpoint for live transcription streaming
- `GET /ws/analytics` – WebSocket endpoint for real-time analytics

#### Failure Handling

- Recording to disk is fully independent from the STT pipeline. If a provider crashes or is misconfigured, the server logs `STT provider exited early; transcription will be disabled`, keeps writing audio to the `.wav/.siprec` file, and tears down analytics as soon as the BYE is processed.
- After a provider failure, no further transcription events are published for that call (analytics snapshot and call cleanup still complete), so dashboards will show a recording with missing transcripts instead of an empty file.

#### Recording Format

- Every SIPREC stream is persisted as `<Call-ID>_<stream-label>.wav`, so multi-stream calls produce one file per stream (e.g., `B2B.123_leg0.wav` for the caller and `B2B.123_leg1.wav` for the callee).
- When your SBC or PBX mixes both legs into a single multi-channel RTP stream (e.g., `rtpmap:96 opus/48000/2`), the recorder preserves that layout: channel 0 stays the caller, channel 1 stays the callee, and you get a single stereo WAV with intact separation.
- No extra flags are required—channel counts are learned from the SDP offer—just ensure the upstream recorder advertises the desired `/2` channel count so the SIPREC server keeps both legs in one file.
- If the SRC sends **separate** audio streams (most SIPREC implementations), enable `RECORDING_COMBINE_LEGS=true` (default) to automatically merge all legs into `<Call-ID>.wav` with each leg occupying its own channel. Individual leg files remain on disk for debugging.

### Pause/Resume & Mute Control

- `POST /api/sessions/{id}/pause` – Pause recording/transcription for a session
- `POST /api/sessions/{id}/resume` – Resume recording/transcription
- `GET /api/sessions/{id}/pause-status` – Get pause status for a session
- `POST /api/sessions/pause-all` – Pause all active sessions
- `POST /api/sessions/resume-all` – Resume all paused sessions
- `GET /api/sessions/pause-status` – Get pause status for all sessions
- `POST /api/sessions/{id}/mute` / `POST /api/sessions/{id}/unmute` – Mute or unmute a session
- `GET /api/sessions/{id}/mute-status` – Get mute status

### Session Management

- `GET /api/sessions` – List all active sessions (use `?id=<session-id>` for a single session)
- `GET /api/sessions/stats` – Session statistics

### Async STT Job API

Available when the async STT processor is enabled (`STT_ASYNC_ENABLED=true`, the default):

- `POST /api/stt/submit` – Submit an audio file for asynchronous transcription
- `GET /api/stt/jobs` – List transcription jobs
- `GET /api/stt/jobs/{id}` – Get job status and result
- `GET /api/stt/stats` – Queue and worker statistics
- `GET /api/stt/metrics` – Job processing metrics
- `POST /api/stt/queue/purge` – Purge queued jobs

### Configuration API

Available when configuration hot-reload is active (`hot_reload.enabled`, the default):

- `GET /api/config` – View the running configuration (enable authentication before exposing this endpoint)
- `POST /api/config/validate` – Validate a candidate configuration
- `POST /api/config/reload` – Trigger a configuration reload
- `GET /api/config/reload/status` – Hot-reload status and history

### GDPR Compliance

- `GET /api/compliance/status` – Compliance feature status
- `POST /api/compliance/gdpr/export` – Export user data
- `DELETE /api/compliance/gdpr/erase` – Erase user data (removes local `.wav/.siprec` artifacts and every uploaded copy recorded in the `.locations` manifest)

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

- Go 1.25 or newer
- Optional: Docker, RabbitMQ, Redis, MySQL, Elasticsearch

### G.729 Codec Support

G.729 decoding requires the **bcg729** native library. Without it, G.729 streams will not be decoded correctly.

**Ubuntu/Debian:**
```bash
# Install bcg729 development files
sudo apt-get install libbcg729-dev

# Or build from source:
git clone https://gitlab.linphone.org/BC/public/bcg729.git
cd bcg729
cmake .
make
sudo make install
sudo ldconfig
```

**macOS:**
```bash
brew install bcg729
```

**RHEL/CentOS/Rocky:**
```bash
# Enable EPEL repository first
sudo dnf install epel-release
sudo dnf install bcg729-devel

# Or build from source (same as Ubuntu)
```

**Build with G.729 support:**
```bash
# CGO must be enabled (default for native builds)
CGO_ENABLED=1 go build -o siprec ./cmd/siprec
```

**Verification:** If you see this warning during build, bcg729 is not properly installed:
```
github.com/pidato/audio/g729: build constraints exclude all Go files
```

**Cross-compilation note:** When cross-compiling (e.g., Linux binary on macOS), you need bcg729 compiled for the target platform. The easiest approach is to build directly on the target Linux server.

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
├── cmd/
│   ├── siprec/          # Main server entry point
│   └── siprecctl/       # CLI tool entry point
├── pkg/
│   ├── alerting/        # Multi-channel alerting system
│   ├── audio/           # Audio processing algorithms
│   ├── auth/            # Authentication and authorization
│   ├── backup/          # Multi-cloud storage backends
│   ├── cdr/             # Call Detail Records
│   ├── circuitbreaker/  # Circuit breaker for STT resilience
│   ├── cli/             # CLI tool implementation
│   ├── cluster/         # Redis-backed clustering and failover
│   ├── compliance/      # PCI DSS and GDPR tools
│   ├── config/          # Configuration management
│   ├── core/            # Shared service registry
│   ├── correlation/     # Request correlation ID tracking
│   ├── database/        # MySQL/MariaDB integration
│   ├── elasticsearch/   # Analytics persistence
│   ├── encryption/      # End-to-end encryption
│   ├── errors/          # Error handling utilities
│   ├── http/            # HTTP server and API handlers
│   ├── lawfulintercept/ # Lawful intercept with LEA delivery
│   ├── media/           # RTP/SRTP and audio processing
│   ├── messaging/       # AMQP/RabbitMQ client
│   ├── metrics/         # Prometheus metrics
│   ├── performance/     # Performance monitoring
│   ├── pii/             # PII detection and redaction
│   ├── ratelimit/       # HTTP and SIP rate limiting
│   ├── realtime/        # Real-time analytics pipeline
│   ├── resources/       # Resource management and limits
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

- [Installation Guide](docs/installation.md) – Complete setup instructions for all platforms
- [Configuration Guide](docs/configuration.md) – All configuration options
- [Audio Pipeline Architecture](docs/audio-pipeline.md) – RTP processing, G.729 decoding, SSRC validation
- [Speech-to-Text Integration](docs/stt.md)
- [Real-Time Transcription](docs/realtime-transcription.md)
- [Vendor Integration Guide](docs/vendor-integration.md) – Oracle, Cisco, Avaya, NICE, Genesys, and more
- [API Reference](docs/api.md)
- [Session Management](docs/sessions.md)
- [Whisper Setup Guide](docs/whisper-setup.md)
- [Cluster Configuration](docs/cluster-configuration.md)
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

### SSRC Mismatch and RTP Crosstalk

If you see warnings about SSRC mismatches in logs:
```
Dropping RTP packet with unexpected SSRC
```

This is normal protective behavior. The server locks the SSRC from the first RTP packet received on each port to prevent stale traffic from previous calls on recycled ports from corrupting new recordings.

**Why this happens:**
- UDP ports are reused after calls end
- Delayed packets from previous calls may arrive on ports now assigned to new calls
- Without SSRC validation, these stale packets would corrupt the new recording

**If legitimate SSRC changes are being rejected:**
The server automatically handles these scenarios:
1. **SIP signaling changes** – re-INVITE and UPDATE messages reset the expected SSRC
2. **Silent SSRC change** – If the locked SSRC goes completely silent and a new SSRC shows sustained traffic, the server switches automatically
3. **Hold/Resume** – SSRC is reset when the call resumes from hold

**SSRC correction blocked during hold:**
If the SBC stops sending RTP during hold, the server enters "RTP suspended" state and blocks SSRC correction to prevent accepting stale traffic. This is logged as:
```
RTP timeout on SIPREC forwarder — keeping alive until BYE
```

### G.729 Audio Quality Issues

G.729 codec recordings may have specific issues due to the codec's characteristics:

**1. Audio Desync Between Channels**

G.729 Annex B uses DTX (Discontinuous Transmission) which stops sending RTP packets during silence. The server automatically handles this by:
- Detecting DTX gaps using RTP timestamp analysis (gaps > 60ms)
- Applying packet loss concealment only for real packet loss (normal 20ms arrival intervals)
- Capping silence insertion at 200ms to prevent recording inflation

If you still experience desync, check that both call legs are using the same codec and sample rate.

**2. Buzzing or Distorted Audio**

The G.729 decoder's synthesis filter can become unstable after DTX gaps, producing a 2kHz square wave artifact. The server automatically detects this pattern (>50% railed samples with rapid sign changes) and replaces corrupt frames with silence.

**3. Recordings Much Longer Than Expected**

This was caused by excessive PLC silence insertion during DTX periods. Version 1.1.1+ limits PLC to 10 packets (200ms) maximum per gap and skips PLC entirely for DTX silence periods.

## Performance

### Load Test Results

The server has been extensively load tested with the following results:

| Concurrent Calls | Duration | Transport | Success Rate | Peak Memory | Peak CPU |
|-----------------|----------|-----------|--------------|-------------|----------|
| 100 | 30s | UDP | 100% | 46 MB | ~1% |
| 1,000 | 30s | UDP | 100% | 70 MB | ~2% |
| 5,000 | 30s | UDP | 100% | 356 MB | ~7% |
| 6,000 | 5 min | TCP | 100% | 1,006 MB | ~5% |
| 10,000 | 30s | UDP | 100% | 548 MB | ~11% |
| 20,000 | 30s | UDP | 100% | 1,554 MB | ~17% |

**Key Performance Metrics:**
- **Concurrent Calls**: Tested up to 20,000 simultaneous sessions (single node)
- **Call Duration**: Validated with 5-minute sustained calls at 6,000 concurrent
- **Memory Efficiency**: ~55 KB per concurrent call (signaling only)
- **CPU Efficiency**: Linear scaling, ~0.001% per concurrent call
- **Latency**: Sub-50ms for SIP signaling, <100ms for STT streaming
- **Throughput**: 10,000+ RTP packets/sec per core

### Enterprise Scale (100k+ Concurrent)

For deployments requiring 100,000+ concurrent recordings:

```bash
# Enable horizontal scaling with Redis session sharing
HORIZONTAL_SCALING=true
REDIS_ADDRESS=cluster:6379
NODE_ID=node-1

# Resource configuration per node
MAX_CONCURRENT_CALLS=25000
MAX_RTP_STREAMS=75000
WORKER_POOL_SIZE=64
MAX_MEMORY_MB=16384
```

**Recommended Infrastructure:**
- **4-5 nodes** with 32+ CPU cores and 32GB RAM each
- **Redis Cluster** for session state sharing
- **Load balancer** (HAProxy, NGINX, or cloud LB) for SIP distribution
- **Shared storage** (NFS, S3, or GCS) for recordings

### SIPp Load Testing

For load testing with SIPp, use TCP with `tn` mode (one socket per call) for best reliability:

Ready-made SIPp scenarios are available in [`test/sipp/`](test/sipp/).

```bash
# 6000 concurrent calls, 5-minute duration, 100 calls/sec ramp-up
sipp <server>:5060 -t tn -sf test/sipp/siprec_load_test.xml -l 6000 -m 6000 -r 100 -timeout 600
```

**Note:** On macOS, the standard TCP mode (`-t t1`) may fail with "Address already in use" errors. Use `-t tn` instead for reliable TCP testing.

## Compliance & Security

- RFC 7865/7866 (SIPREC) compliant
- PCI DSS Level 1 compatible (with encryption and PII redaction)
- GDPR compliant with data export and erasure tools
- TLS 1.2+ for SIP signaling
- SRTP for media encryption
- AES-256-GCM for recording encryption

## License

GPL v3 – see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome! Please open an issue or pull request on GitHub.

## Support

- Issues: https://github.com/loreste/siprec/issues
- Documentation: https://github.com/loreste/siprec/tree/main/docs

---

### Keywords

SIPREC, Session Recording Server, SRS, VoIP recording, SIP recording, telecom recording, call recording, Golang SIP server, Go SIP, OpenSIPS SIPREC, Kamailio SIPREC, FreeSWITCH recording, Asterisk SIPREC, SBC recording server, lawful intercept recording, compliance recording, contact center recording, RTP capture, speech-to-text, real-time transcription, PCI DSS recording, GDPR compliant recording, encrypted call recording, Oracle SBC SIPREC, Cisco CUBE recording, AudioCodes SIPREC, Ribbon SBC recording, enterprise call recording, VoIP compliance, AI transcription, speaker diarization, speaker separation, 100k concurrent recordings, horizontal scaling, enterprise scale recording, LEA delivery, CALEA compliant, voice analytics, sentiment analysis
