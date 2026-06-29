# IZI SIPREC Server

> An open-source SIPREC recording server written in Go. Point your SBC at it, get recordings out.

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg)](LICENSE)
[![SIPREC](https://img.shields.io/badge/SIPREC-RFC%207865%2F7866-green.svg)](https://datatracker.ietf.org/doc/html/rfc7865)
[![Scalability](https://img.shields.io/badge/Scale-Horizontally%20Scalable-orange.svg)](docs/cluster-configuration.md)

## Overview

IZI SIPREC is an open-source **Session Recording Server (SRS)** that implements RFC 7865/7866. It accepts SIPREC sessions from SBCs, PBXs, or SIP proxies, captures the RTP media, and writes recordings to disk or cloud storage.

Beyond basic recording, it can also transcribe calls in real time (with your choice of seven STT providers), detect PII, track speaker changes, encrypt recordings at rest, and push analytics to Elasticsearch or message queues. All of that runs in a single Go binary—turn on what you need, ignore what you don't.

For larger deployments, multiple instances can share session state through Redis and scale horizontally behind a load balancer.

**Version:** 1.2.4

## Who is this for?

- **Enterprise teams** that need to record calls for QA, training, or regulatory compliance
- **Contact centers** looking for call analytics, transcription, and compliance monitoring
- **Telecom operators** who need a recording endpoint behind their SBCs
- **Organizations with lawful intercept obligations** that require tamper-proof recordings with secure LEA delivery

## Tested With

Works with any SIPREC-compliant source. We've tested against:

- **OpenSIPS** and **Kamailio** SIPREC modules
- **FreeSWITCH** and **Asterisk**
- **Oracle SBC**, **Cisco CUBE**, **Ribbon/GENBAND SBC**
- **AudioCodes Mediant** series, **Avaya SBCE**

The server auto-detects which vendor sent the INVITE and extracts vendor-specific metadata (Oracle UCID, Cisco GUID, Avaya UCID, etc.) without any extra configuration.

## What it does

### SIP & Recording

The server speaks SIP over UDP, TCP, and TLS, with built-in NAT traversal via STUN. It parses RFC 7865/7866 SIPREC metadata, negotiates SDP, and records each RTP stream to disk. Sessions can be stored in memory for simple setups or in Redis when you need persistence and failover.

Supported codecs include PCMU, PCMA, G.722, G.729 (via bcg729), Opus, and EVS. The audio pipeline handles jitter buffering, packet loss concealment, VAD, noise reduction, and automatic stereo merging of call legs. G.729 gets special treatment—each stream has its own decoder instance to avoid cross-call state leakage, and an oscillation detector catches synthesis filter instability after DTX gaps.

SSRC is locked from the first RTP packet on each port, so stale packets from recycled ports can't corrupt a new recording. If the locked source goes silent and a different SSRC shows sustained traffic, the server switches automatically.

### Transcription

Pick from seven cloud STT providers (Google, Deepgram, Azure, Amazon, OpenAI, Speechmatics, ElevenLabs) or run [Whisper](https://github.com/openai/whisper) locally for on-prem transcription. A circuit breaker monitors provider health and fails over automatically. Transcripts stream out in real time over WebSocket and AMQP. See the [Whisper Setup Guide](docs/whisper-setup.md) and [real-time transcription docs](docs/realtime-transcription.md) for details.

Speaker diarization runs alongside transcription—voice features are extracted per-segment, and speakers can be tracked across sessions if needed.

### Security & Compliance

Recordings can be encrypted at rest with AES-256-GCM or ChaCha20-Poly1305, with automatic key rotation. PII detection scans transcripts for SSNs, credit card numbers, phone numbers, and email addresses, and can redact them in real time.

The server includes a lawful intercept module with warrant verification, encrypted delivery to LEAs over mutual TLS, and tamper-proof audit logging. PCI DSS and GDPR compliance modes are available, with data export and erasure APIs.

SIP signaling supports TLS, and media can be secured with SRTP. Access control uses JWT tokens and API keys with role-based permissions.

### Analytics & Monitoring

A built-in analytics pipeline does sentiment analysis, keyword detection, and compliance monitoring on transcripts. Results go to Elasticsearch for historical reporting, stream over WebSocket for live dashboards, and publish to AMQP queues.

On the ops side: Prometheus metrics cover SIP, RTP, STT, and AMQP. OpenTelemetry tracing gives end-to-end visibility across distributed setups. MOS scores are calculated in real time using the ITU-T G.107 E-model.

### Storage & Messaging

Upload recordings to S3, Google Cloud Storage, or Azure Blob Storage—or all three at once. AMQP/RabbitMQ integration handles real-time transcript delivery with batching, retries, and dead letter queues. MySQL/MariaDB can store sessions, transcriptions, and CDRs if you need a relational backend.

### Scaling

Multiple SIPREC nodes can share session state through Redis (standalone, Sentinel, or Cluster mode). The cluster supports RTP state replication, distributed rate limiting, split-brain detection, and live stream migration between nodes. See the [Cluster Configuration Guide](docs/cluster-configuration.md).

Worker pools auto-size based on available CPUs, and you can set memory limits with automatic GC tuning.

### Operations

Pause and resume recording mid-call via REST API, or submit async transcription jobs through `/api/stt/*`. Health and readiness probes are Kubernetes-compatible. Configuration can be hot-reloaded without restarting via `/api/config` endpoints. Alerts go out via email (SMTP), Slack, PagerDuty, or webhooks. Optional RBAC enforcement is available for API endpoints (`AUTH_RBAC_ENABLED`). CDRs are generated automatically.

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

`siprecctl` gives you command-line control over a running server.

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

Configuration is layered, with later sources overriding earlier ones:

1. Built-in defaults
2. `.env` file (handy for local dev)
3. YAML or JSON config file
4. Environment variables (always win)

### Config file

For production, use a YAML or JSON file:

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

The server looks for config files in this order:
- `$CONFIG_FILE` environment variable
- `./config.yaml` or `./config.yml` or `./config.json`
- `/etc/siprec/config.yaml`
- `$HOME/.siprec/config.yaml`

See `config.example.yaml` for a complete reference.

### Environment variables

Env vars override anything in the config file. Use them for secrets:

```bash
export DEEPGRAM_API_KEY=your-api-key
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
export REDIS_PASSWORD=your-password
```

### Basics

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

#### Setting up analytics

1. Point analytics at Elasticsearch:
   ```bash
   export ANALYTICS_ENABLED=true
   export ELASTICSEARCH_ADDRESSES=https://es.example.com:9200
   export ELASTICSEARCH_INDEX=call-analytics
   ```
2. If you want live dashboards, turn on AMQP fan-out:
   ```bash
   export ENABLE_REALTIME_AMQP=true
   export PUBLISH_SENTIMENT_UPDATES=true   # default
   export PUBLISH_KEYWORD_DETECTIONS=true  # default
   ```
3. The `/ws/analytics` WebSocket endpoint comes up automatically when `ANALYTICS_ENABLED=true`.

With analytics on, every transcription chunk gets a sentiment score (lexicon-based with negation handling and intensifier support). Results are published to three places: the WebSocket stream, the AMQP queue, and Elasticsearch.

## HTTP API

### Health & Metrics

- `GET /health` — aggregate health (200 when healthy)
- `GET /health/live` — liveness probe (always 200)
- `GET /health/ready` — readiness probe (fails if dependencies are down)
- `GET /metrics` — Prometheus metrics
- `GET /status` — uptime and version info

### Real-time transcription

- `GET /ws/transcriptions` — WebSocket for live transcription streaming
- `GET /ws/analytics` — WebSocket for real-time analytics

**What happens when STT fails?** Recording continues normally—audio always goes to disk regardless of what the transcription pipeline is doing. If a provider crashes, you'll see `STT provider exited early; transcription will be disabled` in the logs, and the recording finishes without transcripts. Nothing is lost.

#### Recording format

Each SIPREC stream is saved as `<Call-ID>_<stream-label>.wav`—so a two-leg call produces two files (e.g., `B2B.123_leg0.wav` and `B2B.123_leg1.wav`).

If your SBC mixes both legs into a single multi-channel RTP stream, the server preserves that layout as a stereo WAV. Channel counts come from the SDP offer, so no extra flags are needed.

Most SIPREC implementations send separate streams per leg. With `RECORDING_COMBINE_LEGS=true` (the default), the server merges them into `<Call-ID>.wav` with each leg in its own channel. The individual leg files stick around for debugging.

### Pause/Resume & Mute

- `POST /api/sessions/{id}/pause` / `POST /api/sessions/{id}/resume` — pause or resume a session
- `GET /api/sessions/{id}/pause-status` — check pause state
- `POST /api/sessions/pause-all` / `POST /api/sessions/resume-all` — bulk control
- `POST /api/sessions/{id}/mute` / `POST /api/sessions/{id}/unmute` — mute or unmute
- `GET /api/sessions/{id}/mute-status` — check mute state

### Sessions

- `GET /api/sessions` — list active sessions (use `?id=<session-id>` for a single session)
- `GET /api/sessions/stats` — session statistics
- `DELETE /api/sessions/:id` — terminate a session

### Async STT Jobs

Available when `STT_ASYNC_ENABLED=true` (the default):

- `POST /api/stt/submit` — submit audio for async transcription
- `GET /api/stt/jobs` — list jobs
- `GET /api/stt/jobs/{id}` — job status and result
- `GET /api/stt/stats` — queue and worker stats
- `GET /api/stt/metrics` — processing metrics
- `POST /api/stt/queue/purge` — purge queued jobs

### Configuration

Available when hot-reload is active (`hot_reload.enabled`, the default):

- `GET /api/config` — view running config (enable auth before exposing)
- `POST /api/config/validate` — validate a candidate config
- `POST /api/config/reload` — trigger reload
- `GET /api/config/reload/status` — reload history

### GDPR & Compliance

- `GET /api/compliance/status` — compliance feature status
- `POST /api/compliance/gdpr/export` — export user data
- `DELETE /api/compliance/gdpr/erase` — erase user data

When recordings are uploaded to cloud storage, a sidecar `<recording>.locations` file tracks where each copy lives (e.g., `s3://bucket/prefix/file.siprec`). The erase workflow reads that manifest, deletes from every backend, and removes the manifest itself—nothing lingers.

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

- Go 1.25+
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

### Load test results

We test with SIPp, running multi-vendor SIPREC scenarios against Oracle SBC, Avaya SM, and Cisco CUBE profiles:

| Concurrent Calls | Duration | Transport | Success Rate | Peak Memory | Notes |
|-----------------|----------|-----------|--------------|-------------|-------|
| 15 | 2 min | TCP | 80% (12/15) | ~15 MB | Genesys scenario error |
| 100 | 2 min | TCP | 90% (90/100) | ~70 MB | 100% for Oracle/Avaya/Cisco |
| 450 | 2 min | TCP | 100% (450/450) | ~195 MB | 1,350 recording files created |

At 450 concurrent calls the server used about 433 KB per call, 3.6% CPU, and had zero TCP errors across all runs. Memory and CPU scale linearly, so there's plenty of headroom on modern hardware.

### Scaling out

For deployments that need more than a single node can handle, turn on Redis-backed session sharing:

```bash
HORIZONTAL_SCALING=true
REDIS_ADDRESS=cluster:6379
NODE_ID=node-1

MAX_CONCURRENT_CALLS=500
MAX_RTP_STREAMS=1500
WORKER_POOL_SIZE=0  # auto-detect based on CPU
```

Put a load balancer (HAProxy, NGINX, or a cloud LB) in front for SIP distribution, and point recordings at shared storage (NFS, S3, or GCS).

### Running your own load tests

We use SIPp with TCP in `tn` mode (one socket per call):

Ready-made SIPp scenarios are available in [`test/sipp/`](test/sipp/).

```bash
# 500 concurrent calls, 2-minute duration, 5 calls/sec ramp-up
sipp <server>:5060 -t tn -sf test/sipp/siprec_load_test.xml -l 500 -m 500 -r 5 -timeout 300
```

Use `-t tn` instead of `-t t1` to avoid "Address already in use" errors under high concurrency.

## Compliance & Security

- RFC 7865/7866 (SIPREC) compliant
- PCI DSS compatible (with encryption and PII redaction enabled)
- GDPR data export and erasure APIs
- TLS 1.2+ for SIP signaling, SRTP for media
- AES-256-GCM recording encryption with key rotation

## License

GPL v3 — see [LICENSE](LICENSE) for details.

## Contributors

- [Lance Oreste](https://github.com/loreste)
- [Ma91Wa](https://github.com/Ma91Wa)

## Contributing

Contributions welcome. Open an issue or pull request on GitHub.

## Support

- Issues: https://github.com/loreste/siprec/issues
- Docs: https://github.com/loreste/siprec/tree/main/docs
