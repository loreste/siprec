# SIPREC Server

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](.)
[![NAT Support](https://img.shields.io/badge/NAT-Supported-blue.svg)](docs/configuration/README.md)

A high-performance, production-ready SIP recording (SIPREC) server that implements RFC 7865/7866 with advanced real-time transcription, comprehensive NAT support for cloud deployments, and enterprise features including database persistence, cloud storage, and compliance support.

## ✨ Key Features

### Core SIPREC Capabilities
- **📞 RFC Compliance** - Complete RFC 7865/7866 implementation for SIP session recording
- **🔄 Session Management** - Advanced session lifecycle management with persistence support
- **⏸️ Pause/Resume Control** - Real-time pause and resume of recording and transcription via REST API
- **🎯 NAT Traversal** - Comprehensive NAT support with STUN integration and dynamic IP detection
- **🔗 SIP Integration** - Custom SIP server implementation optimized for TCP transport and large metadata
- **🔐 Security** - TLS/SRTP support with configurable enforcement and certificate management

### Transcription & Processing
- **🎙️ Real-time Transcription** - Multi-provider STT support (Google, Deepgram, Speechmatics, ElevenLabs, OpenAI, Azure, Amazon)
- **⚡ Streaming Support** - Real-time callback-based transcription with WebSocket streaming
- **👥 Speaker Diarization** - Multi-speaker identification and word-level speaker tagging
- **🛡️ PII Detection & Redaction** - Automatic detection and redaction of SSNs, credit cards, and other sensitive data
- **🎵 Audio Processing** - Noise suppression, automatic gain control, echo cancellation, VAD
- **🌐 WebSocket Streaming** - Real-time transcription delivery with circuit breaker protection
- **🏥 Provider Health Monitoring** - Automatic health checks, circuit breakers, and intelligent failover

### Data Persistence & Storage
- **💾 Database Support** - Optional MySQL/MariaDB persistence with full CRUD operations
- **☁️ Cloud Storage** - Multi-cloud storage support (AWS S3, Google Cloud Storage, Azure Blob)
- **🔍 Full-text Search** - Database-backed search across transcriptions and metadata (requires MySQL)
- **📦 Automatic Archival** - Configurable retention policies with automatic cloud upload
- **🗄️ CDR Generation** - Call Detail Records with comprehensive metadata

### Compliance & Security
- **🔒 PCI DSS Compliance** - PCI compliance mode with automatic security hardening
- **🇪🇺 GDPR Support** - Data privacy controls with export and deletion capabilities
- **📝 Audit Logging** - Tamper-proof audit logs with blockchain-style chaining
- **🔐 Encryption at Rest** - Optional recording encryption with key rotation
- **🛡️ Security Enforcement** - Configurable TLS/SRTP requirements

### Enterprise Features
- **📨 Message Queue** - AMQP integration with TLS, DLQ handling, and delivery guarantees
- **📈 Monitoring** - Prometheus metrics, OpenTelemetry tracing, and performance profiling
- **☁️ Cloud Ready** - Optimized for GCP, AWS, Azure with automatic configuration
- **⚠️ Warning System** - Centralized warning collection with severity levels and automatic cleanup
- **🔔 Alerting** - Configurable alerting system (requires manual rule/channel setup)
- **🔐 Authentication** - JWT and API key authentication (optional, disabled by default)
- **🔄 Provider Resilience** - Automatic failover with circuit breakers and health monitoring

## 🚀 Quick Start

### Docker Deployment

```bash
# Production deployment
docker run -d \
  --name siprec-server \
  --restart unless-stopped \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 5061:5061/tcp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/var/lib/siprec/recordings \
  -e BEHIND_NAT=true \
  -e EXTERNAL_IP=auto \
  ghcr.io/loreste/siprec:latest
```

### Development Setup

```bash
# Clone repository
git clone https://github.com/loreste/siprec.git
cd siprec

# Build from source
go build -o siprec ./cmd/siprec

# Run with default configuration
./siprec
```

### Optional MySQL Support

MySQL persistence is delivered via an optional build tag:

```bash
# Build without MySQL (default)
make build

# Build with MySQL enabled
make build-mysql

# Run tests with MySQL enabled
make test-mysql
```

## ⚙️ Configuration

### Core Environment Variables

```env
# Network Configuration (NAT Optimized)
BEHIND_NAT=true                    # Enable NAT support
EXTERNAL_IP=auto                   # Auto-detect external IP
INTERNAL_IP=auto                   # Auto-detect internal IP
PORTS=5060,5061                    # SIP listening ports
RTP_PORT_MIN=16384                 # RTP port range start
RTP_PORT_MAX=32768                 # RTP port range end
SIP_HOST=0.0.0.0                   # SIP bind address

# STUN Configuration
STUN_SERVER=stun:stun.l.google.com:19302

# Security
ENABLE_TLS=true                    # Enable TLS for SIP
TLS_CERT_PATH=/path/to/cert.pem    # TLS certificate
TLS_KEY_PATH=/path/to/key.pem      # TLS private key
ENABLE_SRTP=true                   # Enable SRTP for media
SIP_REQUIRE_SRTP=false             # Require SRTP
SIP_REQUIRE_TLS=false              # Require TLS

# Transcription
STT_DEFAULT_VENDOR=google-enhanced
STT_PROVIDERS=google-enhanced,deepgram-enhanced,speechmatics,elevenlabs,openai
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# Recording
RECORDING_DIR=/var/lib/siprec/recordings
RECORDING_MAX_DURATION=4h
ENABLE_RECORDING_ENCRYPTION=false

# Database Persistence (optional, requires build tag `mysql`)
DATABASE_ENABLED=false
DB_HOST=localhost
DB_PORT=3306
DB_NAME=siprec
DB_USERNAME=siprec
DB_PASSWORD=secret

# Cloud Storage (optional)
RECORDING_STORAGE_ENABLED=false
RECORDING_STORAGE_KEEP_LOCAL=true

# AWS S3
RECORDING_STORAGE_S3_ENABLED=false
RECORDING_STORAGE_S3_BUCKET=siprec-recordings
RECORDING_STORAGE_S3_REGION=us-east-1

# Google Cloud Storage
RECORDING_STORAGE_GCS_ENABLED=false
RECORDING_STORAGE_GCS_BUCKET=siprec-recordings
RECORDING_STORAGE_GCS_PROJECT_ID=your-project-id

# Azure Blob Storage
RECORDING_STORAGE_AZURE_ENABLED=false
RECORDING_STORAGE_AZURE_ACCOUNT_NAME=youraccountname
RECORDING_STORAGE_AZURE_CONTAINER=siprec-recordings

# Compliance Features
COMPLIANCE_PCI_ENABLED=false
COMPLIANCE_GDPR_ENABLED=false
COMPLIANCE_GDPR_EXPORT_DIR=./exports
COMPLIANCE_AUDIT_ENABLED=true
COMPLIANCE_AUDIT_TAMPER_PROOF=false
COMPLIANCE_AUDIT_LOG_PATH=./logs/audit.log

# Real-time Analytics (requires Elasticsearch)
ANALYTICS_ENABLED=false
ANALYTICS_ELASTICSEARCH_ADDRESSES=http://localhost:9200
ANALYTICS_ELASTICSEARCH_INDEX=call-analytics

# Audio Processing
VAD_ENABLED=true
NOISE_REDUCTION_ENABLED=true
AUDIO_ENHANCEMENT_ENABLED=true
AGC_ENABLED=true
ECHO_CANCELLATION_ENABLED=true

# Pause/Resume Control API
PAUSE_RESUME_ENABLED=true
PAUSE_RESUME_REQUIRE_AUTH=true
PAUSE_RESUME_API_KEY=your-api-key

# PII Detection & Redaction
PII_DETECTION_ENABLED=false
PII_ENABLED_TYPES=ssn,credit_card,phone,email
PII_APPLY_TO_TRANSCRIPTIONS=true
PII_APPLY_TO_RECORDINGS=false

# Authentication (optional, disabled by default)
AUTH_ENABLED=false
AUTH_JWT_SECRET=your-secret-key
AUTH_ADMIN_USERNAME=admin
AUTH_ADMIN_PASSWORD=secure-password

# Alerting (requires manual rule/channel configuration)
ALERTING_ENABLED=false
ALERTING_EVALUATION_INTERVAL=30s

# Telemetry & Tracing
TELEMETRY_ENABLED=true
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_SERVICE_NAME=siprec
TRACING_ENABLED=true

# Performance Monitoring
PERFORMANCE_MONITORING_ENABLED=true
PERFORMANCE_MONITOR_INTERVAL=30s
PERFORMANCE_MEMORY_LIMIT_MB=512
PERFORMANCE_CPU_LIMIT=80

# AMQP (Message Queue)
AMQP_URL=amqps://guest:guest@rabbitmq:5671/
AMQP_QUEUE_NAME=siprec.transcriptions
AMQP_TLS_ENABLED=true
```

## 🔌 API Endpoints

### HTTP API

- `GET /health` - Health check endpoint
- `GET /health/live` - Kubernetes liveness probe
- `GET /health/ready` - Kubernetes readiness probe
- `GET /metrics` - Prometheus metrics
- `GET /api/sessions` - Active sessions
- `GET /api/sessions/stats` - Session statistics
- `GET /api/sessions/{id}` - Session details
- `GET /api/sessions/search` - Search sessions (requires MySQL)
- `GET /api/transcriptions/search` - Search transcriptions (requires MySQL)

### Pause/Resume Control API

- `POST /api/sessions/{id}/pause` - Pause recording/transcription
- `POST /api/sessions/{id}/resume` - Resume recording/transcription
- `GET /api/sessions/{id}/pause-status` - Get pause status
- `POST /api/sessions/pause-all` - Pause all active sessions
- `POST /api/sessions/resume-all` - Resume all paused sessions

### WebSocket

- `WS /ws/transcriptions` - Real-time transcription stream
- `WS /ws/analytics` - Real-time analytics stream (requires Elasticsearch)

## 🧪 Testing

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Run tests with MySQL support
make test-mysql

# Run specific test packages
go test -v ./pkg/siprec/...
go test -v ./pkg/stt/...

# Generate coverage report
make coverage-html
```

## 🏗️ Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   SIP/RTP   │────▶│   Audio     │────▶│     STT     │
│   Handler   │     │ Processing  │     │  Provider   │
└─────────────┘     └─────────────┘     └─────────────┘
                            │                    │
                            ▼                    ▼
                    ┌─────────────┐     ┌─────────────┐
                    │  Recording  │     │ WebSocket/  │
                    │   Storage   │     │    AMQP     │
                    └─────────────┘     └─────────────┘
```

## 🏢 Production Deployment

### System Requirements
- **OS**: Ubuntu 20.04+, Debian 11+, CentOS 8+, RHEL 8+
- **Memory**: 4GB RAM minimum, 8GB recommended
- **CPU**: 2 cores minimum, 4 cores recommended
- **Storage**: 50GB minimum for recordings
- **Network**: Public IP for external access, 1Gbps recommended
- **Database**: MySQL 8.0+ or MariaDB 10.5+ (optional)
- **Elasticsearch**: 7.10+ for analytics (optional)

### Performance Characteristics
- **Concurrent Sessions**: 500+ simultaneous recordings (with proper resources)
- **Audio Quality**: PCM/G.711/G.722/Opus codec support
- **Latency**: <50ms for real-time streaming transcription
- **Throughput**: 10,000+ RTP packets/second capacity

### Security Hardening

#### PCI DSS Compliance Mode
```bash
export COMPLIANCE_PCI_ENABLED=true
export SIP_REQUIRE_TLS=true
export SIP_REQUIRE_SRTP=true
export ENABLE_RECORDING_ENCRYPTION=true
```

#### Network Security
```bash
# Firewall rules
ufw allow 5060/tcp  # SIP TCP
ufw allow 5060/udp  # SIP UDP
ufw allow 5061/tcp  # SIP TLS
ufw allow 16384:32768/udp  # RTP range
ufw allow 8080/tcp  # HTTP API (restrict to internal)
```

## 🔧 Administration

### Service Management
```bash
# SystemD commands
sudo systemctl start siprec-server
sudo systemctl stop siprec-server
sudo systemctl restart siprec-server
sudo systemctl status siprec-server

# View logs
sudo journalctl -u siprec-server -f
```

### Monitoring

#### Prometheus Metrics
```bash
# Core Metrics
- siprec_active_calls{instance="..."}
- siprec_rtp_packets_received_total
- siprec_transcription_errors_total{provider="..."}
- siprec_session_duration_seconds

# Performance Metrics
- siprec_audio_processing_latency_ms
- siprec_stt_response_time_ms{provider="..."}
- siprec_database_query_duration_ms

# Health Metrics
- siprec_provider_health_score{provider="..."}
- siprec_circuit_breaker_state{provider="..."}
```

## 🌟 Use Cases

### Contact Centers
- **Call Recording**: Automatic SIPREC-compliant call recording
- **Quality Assurance**: Real-time transcription for agent monitoring
- **Compliance**: Regulatory compliance with complete audit trails

### Enterprise Communications
- **Meeting Recording**: Conference call recording and transcription
- **Training**: Call analysis and training material generation
- **Analytics**: Voice analytics and sentiment analysis

### Telecommunications
- **Service Provider**: SIPREC recording for telecom operators
- **Legal Compliance**: Lawful intercept and recording capabilities
- **Network Analysis**: Call quality and performance monitoring

## ⚠️ Not Yet Implemented

The following features are documented in older versions but not yet integrated:

- **Clustering/HA**: Multi-node clustering with leader election (pkg/clustering exists but not integrated)
- **Automated Backup**: Backup and recovery system (pkg/backup exists but not integrated)
- **Session Failover**: Automatic session failover between nodes (pkg/failover exists but not integrated)
- **DNS Management**: SRV records and DNS-based load balancing
- **High Availability Setup**: Active-active configuration with Redis

These features can be integrated when needed. The packages exist in the codebase but are not wired up in the main application.

## 🤝 Contributing

We welcome contributions! Please follow Go best practices and write comprehensive tests for new features.

```bash
# Clone and setup
git clone https://github.com/loreste/siprec.git
cd siprec

# Install dependencies
go mod download

# Run tests
go test -v ./...

# Build
go build -o siprec ./cmd/siprec
```

## 📄 License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## 🆘 Support

### Documentation
- **Complete Docs**: [docs/README.md](docs/README.md)
- **Deployment Guide**: [README-DEPLOYMENT.md](README-DEPLOYMENT.md)
- **NAT Analysis**: [nat_analysis.md](nat_analysis.md)

### Community Support
- **Issues**: [GitHub Issues](https://github.com/loreste/siprec/issues)
- **Discussions**: [GitHub Discussions](https://github.com/loreste/siprec/discussions)

---

**Built with ❤️ for the VoIP and telecommunications community**
