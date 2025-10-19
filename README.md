# SIPREC Server

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg)](LICENSE)
[![Documentation](https://img.shields.io/badge/Docs-Available-brightgreen.svg)](docs/README.md)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](.)
[![NAT Support](https://img.shields.io/badge/NAT-Supported-blue.svg)](docs/configuration/README.md)
[![TCP Optimized](https://img.shields.io/badge/TCP-Optimized-orange.svg)](docs/architecture/SIP_ARCHITECTURE.md)

A high-performance, enterprise-grade SIP recording (SIPREC) server that implements RFC 7865/7866 with custom TCP-optimized transport, advanced real-time transcription capabilities, comprehensive NAT support for cloud deployments, and production-ready features including database persistence, cloud storage, real-time analytics, and compliance support.

**Complete Feature Set**: 7 STT providers • MySQL/MariaDB persistence • S3/GCS/Azure storage • Elasticsearch analytics • WebSocket streaming • PCI/GDPR compliance • Advanced audio processing • High availability clustering • OpenTelemetry tracing • Automated backups • Multi-channel alerting

## ✨ Key Features

### Core SIPREC Capabilities
- **📞 RFC Compliance** - Complete RFC 7865/7866 implementation for SIP session recording
- **🔄 Session Management** - Advanced session lifecycle management with failover support
- **⏸️ Pause/Resume Control** - Real-time pause and resume of recording and transcription via REST API
- **🎯 NAT Traversal** - Comprehensive NAT support with STUN integration for cloud deployments
- **🔗 SIP Integration** - Custom SIP server implementation optimized for TCP transport and large metadata

### Transcription & Processing
- **🎙️ Real-time Streaming Transcription** - Enhanced multi-provider STT with full WebSocket streaming support (Google, Deepgram, Speechmatics, ElevenLabs, OpenAI, Azure, Amazon)
- **⚡ Streaming Response Support** - Real-time callback-based transcription with interim and final results
- **👥 Speaker Diarization** - Multi-speaker identification and word-level speaker tagging
- **🛡️ PII Detection & Redaction** - Automatic detection and redaction of SSNs, credit cards, and other sensitive data from transcriptions and audio
- **🎵 Advanced Audio Processing** - Comprehensive audio enhancement pipeline:
  - Spectral subtraction noise suppression with adaptive learning
  - Automatic Gain Control (AGC) with attack/release control
  - Echo cancellation with double-talk detection
  - Multi-channel recording with synchronization
  - Audio fingerprinting for duplicate detection
  - Parametric equalizer with presets
  - Dynamic range compression
- **🌐 WebSocket Streaming** - Real-time transcription delivery with circuit breaker patterns
- **📊 Quality Metrics** - Audio quality monitoring and adaptive processing with performance optimization
- **🔄 Provider Interface Standardization** - Unified callback interface across all STT providers
- **🏥 Provider Health Monitoring** - Automatic health checks, circuit breakers, and intelligent failover

### Analytics & Intelligence
- **📊 Real-time Analytics** - Live call analytics with sentiment analysis, keyword extraction, and compliance monitoring
- **🔍 Elasticsearch Integration** - Scalable analytics storage with full-text search capabilities
- **🌊 WebSocket Analytics Stream** - Real-time analytics updates with event-driven alerts
- **📈 Audio Metrics** - MOS scoring, packet loss analysis, jitter monitoring, and quality degradation detection
- **🎯 Event Detection** - Acoustic event detection including music, silence, and speech patterns
- **⚠️ Intelligent Alerting** - Automatic detection of quality issues, sentiment changes, and compliance violations

### Data Persistence & Storage
- **💾 Database Support** - Optional MySQL/MariaDB persistence with full CRUD operations
- **☁️ Cloud Storage** - Multi-cloud storage support (AWS S3, Google Cloud Storage, Azure Blob)
- **🔍 Full-text Search** - Database-backed search across transcriptions and metadata
- **📦 Automatic Archival** - Configurable retention policies with automatic cloud upload
- **🗄️ CDR Generation** - Call Detail Records with comprehensive metadata

### Compliance & Security
- **🔒 PCI DSS Compliance** - PCI compliance mode with automatic security hardening
- **🇪🇺 GDPR Support** - Data privacy controls with export and deletion capabilities  
- **📝 Audit Logging** - Tamper-proof audit logs with blockchain-style chaining
- **🔐 Encryption at Rest** - Optional recording encryption with key management
- **🛡️ Security Enforcement** - Configurable TLS/SRTP requirements with certificate validation

### Enterprise Features
- **🔐 Security** - End-to-end encryption with TLS/SRTP, configurable key rotation, and HSM support
- **📨 Message Queue** - AMQP integration with DLQ handling, exchange management, delivery guarantees, and exponential retry
- **📈 Monitoring** - Prometheus metrics, OpenTelemetry tracing, CPU/memory profiling, and goroutine monitoring
- **☁️ Cloud Ready** - Optimized for GCP, AWS, Azure with automatic configuration
- **⚠️ Advanced Warning System** - Intelligent warning collection with severity levels and automatic resolution
- **✅ Configuration Validation** - Comprehensive startup validation with detailed error reporting
- **🔄 Provider Resilience** - Automatic failover with score-based provider selection and circuit breakers
- **🌍 Language Routing** - Intelligent routing of calls to STT providers based on language detection

### Operations & Resilience
- **🔔 Alerting System** - Multi-channel alerts (email, Slack, webhook) with aggregation and deduplication
- **💾 Backup & Recovery** - Automated backups with point-in-time recovery and rotation policies
- **🌐 Clustering** - Leader election, distributed session management, and node health monitoring
- **📊 Telemetry** - OpenTelemetry integration with distributed tracing and custom metrics
- **⚡ Performance** - Auto-tuning, worker pool optimization, and resource limit management
- **🔄 Business Continuity** - Disaster recovery, automatic failover, and data replication
- **🔧 DNS Management** - SRV records, DNS-based load balancing, and failover strategies

## 🚀 Quick Start

### Cloud Deployment (Recommended)

#### Google Cloud Platform
```bash
# One-command deployment
./deploy-quick.sh

# Or with Terraform
terraform init
terraform apply -var="project_id=YOUR_PROJECT_ID"
```

#### Manual Linux Installation
```bash
# Download and run deployment script
wget https://raw.githubusercontent.com/loreste/siprec/main/deploy_gcp_linux.sh
chmod +x deploy_gcp_linux.sh
sudo ./deploy_gcp_linux.sh
```

### Docker Deployment

```bash
# Production deployment
docker run -d \
  --name siprec-server \
  --restart unless-stopped \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 5061:5061/udp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/var/lib/siprec/recordings \
  -v $(pwd)/config:/etc/siprec \
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

MySQL persistence is delivered via an optional build tag so tenants that do not
require it can keep images lean:

```bash
# Build without MySQL (default)
make build

# Build with MySQL enabled
make build-mysql

# Run unit tests with MySQL enabled
make test-mysql

# Or call go build/test directly
GO_BUILD_TAGS=mysql make build
GO_BUILD_TAGS=mysql make test
```

Executables built without the `mysql` tag will return a clear
`mysql support not enabled` error if MySQL is requested at runtime.

## ⚙️ Configuration

### Environment Variables

The server can be configured via environment variables or a `.env` file:

```env
# Network Configuration (NAT Optimized)
BEHIND_NAT=true                    # Enable NAT support
EXTERNAL_IP=auto                   # Auto-detect external IP
INTERNAL_IP=auto                   # Auto-detect internal IP
PORTS=5060,5061                    # SIP listening ports
RTP_PORT_MIN=16384                 # RTP port range start
RTP_PORT_MAX=32768                 # RTP port range end
SIP_REQUIRE_TLS=false              # Set true to enforce TLS-only SIP listeners

# STUN Configuration
STUN_SERVER=stun:stun.l.google.com:19302

# Security
ENABLE_TLS=true                    # Enable TLS for SIP
TLS_CERT_PATH=/path/to/cert.pem    # TLS certificate
TLS_KEY_PATH=/path/to/key.pem      # TLS private key
ENABLE_SRTP=true                   # Enable SRTP for media
SIP_REQUIRE_SRTP=false             # Require inbound calls to negotiate SRTP
SIP_REQUIRE_TLS=false              # Enforce TLS-only SIP listeners (no UDP/TCP)

# Transcription
STT_DEFAULT_VENDOR=google-enhanced           # Default STT provider
STT_PROVIDERS=google-enhanced,deepgram-enhanced,speechmatics,elevenlabs,openai
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json

# Speechmatics (optional)
SPEECHMATICS_STT_ENABLED=true
SPEECHMATICS_API_KEY=your-speechmatics-token
SPEECHMATICS_LANGUAGE=en-US

# ElevenLabs (optional)
ELEVENLABS_STT_ENABLED=true
ELEVENLABS_API_KEY=xi-your-api-key
ELEVENLABS_LANGUAGE=en

# Recording
RECORDING_DIR=/var/lib/siprec/recordings
RECORDING_MAX_DURATION=4h
ENABLE_RECORDING_ENCRYPTION=false

# STT Provider
STT_ENABLE_DIARIZATION=true    # Enable speaker diarization
STT_ENABLE_WORD_TIMESTAMPS=true # Enable word-level timestamps
STT_ENABLE_STREAMING=true      # Enable real-time streaming transcription
# Route languages to providers (language:provider comma-separated)
LANGUAGE_ROUTING=en-US:google,es-ES:deepgram

# AMQP (Message Queue)
AMQP_URL=amqps://guest:guest@rabbitmq.internal:5671/
AMQP_QUEUE_NAME=siprec.transcriptions
AMQP_TLS_ENABLED=true
AMQP_TLS_CA_FILE=/etc/rabbitmq/ca.pem

# Database Persistence (optional, requires build tag `mysql`)
DATABASE_ENABLED=false
DB_HOST=localhost
DB_PORT=3306
DB_NAME=siprec
DB_USERNAME=siprec
DB_PASSWORD=secret
DB_MAX_CONNECTIONS=25
DB_MAX_IDLE_CONNECTIONS=5
DB_CONNECTION_LIFETIME=5m

# Cloud Storage (optional)
RECORDING_STORAGE_ENABLED=false
RECORDING_STORAGE_KEEP_LOCAL=true

# AWS S3
RECORDING_STORAGE_S3_ENABLED=false
RECORDING_STORAGE_S3_BUCKET=siprec-recordings
RECORDING_STORAGE_S3_REGION=us-east-1
RECORDING_STORAGE_S3_ACCESS_KEY=your-access-key-here
RECORDING_STORAGE_S3_SECRET_KEY=your-secret-key-here
RECORDING_STORAGE_S3_PREFIX=recordings/

# Google Cloud Storage
RECORDING_STORAGE_GCS_ENABLED=false
RECORDING_STORAGE_GCS_BUCKET=siprec-recordings
RECORDING_STORAGE_GCS_PROJECT_ID=your-project-id
RECORDING_STORAGE_GCS_CREDENTIALS_PATH=/path/to/service-account.json

# Azure Blob Storage
RECORDING_STORAGE_AZURE_ENABLED=false
RECORDING_STORAGE_AZURE_ACCOUNT_NAME=youraccountname
RECORDING_STORAGE_AZURE_ACCOUNT_KEY=youraccountkey
RECORDING_STORAGE_AZURE_CONTAINER=siprec-recordings

# Compliance Features
COMPLIANCE_PCI_ENABLED=false              # Enable PCI compliance mode
COMPLIANCE_GDPR_ENABLED=false             # Enable GDPR features
COMPLIANCE_GDPR_EXPORT_DIR=./exports      # Directory for GDPR exports
COMPLIANCE_AUDIT_ENABLED=true             # Enable audit logging
COMPLIANCE_AUDIT_TAMPER_PROOF=false       # Enable tamper-proof audit logs
COMPLIANCE_AUDIT_LOG_PATH=./logs/audit.log

# Real-time Analytics
ANALYTICS_ENABLED=false
ANALYTICS_ELASTICSEARCH_ADDRESSES=http://localhost:9200
ANALYTICS_ELASTICSEARCH_INDEX=call-analytics
ANALYTICS_ELASTICSEARCH_USERNAME=elastic
ANALYTICS_ELASTICSEARCH_PASSWORD=changeme
ANALYTICS_WEBSOCKET_ENABLED=true          # Enable WebSocket analytics streaming
ANALYTICS_BUFFER_SIZE=1000                # Analytics buffer size
ANALYTICS_FLUSH_INTERVAL=5s               # Analytics flush interval

# Audio Processing
VAD_ENABLED=true
NOISE_REDUCTION_ENABLED=true
AUDIO_ENHANCEMENT_ENABLED=true      # Enable audio enhancement pipeline
NOISE_SUPPRESSION_LEVEL=0.7         # Noise suppression level (0-1)
AGC_ENABLED=true                    # Enable automatic gain control
AGC_TARGET_LEVEL=-18                # Target level in dB
ECHO_CANCELLATION_ENABLED=true      # Enable echo cancellation
MULTI_CHANNEL_ENABLED=false         # Enable multi-channel recording
FINGERPRINTING_ENABLED=true         # Enable audio fingerprinting

# Pause/Resume Control API
PAUSE_RESUME_ENABLED=true          # Enable pause/resume API
PAUSE_RESUME_REQUIRE_AUTH=true     # Require API key authentication
PAUSE_RESUME_API_KEY=your-api-key  # API key for authentication
PAUSE_RESUME_PER_SESSION=true      # Allow per-session control

# PII Detection & Redaction
PII_DETECTION_ENABLED=true         # Enable PII detection
PII_ENABLED_TYPES=ssn,credit_card  # Types to detect: ssn, credit_card, phone, email
PII_REDACTION_CHAR=*               # Character used for redaction
PII_APPLY_TO_TRANSCRIPTIONS=true   # Apply PII filtering to transcriptions
PII_APPLY_TO_RECORDINGS=true       # Apply PII marking to audio recordings
PII_PRESERVE_FORMAT=true           # Preserve original format when redacting
PII_CONTEXT_LENGTH=10              # Context characters around detected PII

# Alerting Configuration
ALERTING_ENABLED=true               # Enable alerting system
ALERT_CHANNELS=email,slack,webhook  # Alert delivery channels
ALERT_EMAIL_TO=ops@example.com      # Email recipients
ALERT_SLACK_WEBHOOK=https://hooks.slack.com/services/xxx
ALERT_AGGREGATION_WINDOW=5m         # Alert aggregation window

# Clustering & HA
CLUSTER_ENABLED=false               # Enable clustering
CLUSTER_NODE_ID=node-1              # Unique node identifier
CLUSTER_PEERS=node-2:8080,node-3:8080  # Peer nodes
LEADER_ELECTION_ENABLED=true        # Enable leader election

# Telemetry & Tracing
TELEMETRY_ENABLED=true              # Enable OpenTelemetry
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
OTEL_SERVICE_NAME=siprec
TRACING_ENABLED=true                # Enable distributed tracing
TRACING_SAMPLE_RATE=0.1             # Trace sampling rate

# Performance Monitoring
PERFORMANCE_MONITORING_ENABLED=true  # Enable performance monitoring
AUTO_TUNING_ENABLED=true            # Enable auto-tuning
WORKER_POOL_MIN=10                  # Minimum worker pool size
WORKER_POOL_MAX=100                 # Maximum worker pool size
GOROUTINE_LIMIT=10000               # Maximum goroutines

# Backup Configuration
BACKUP_ENABLED=true                 # Enable automatic backups
BACKUP_SCHEDULE="0 2 * * *"        # Cron schedule for backups
BACKUP_RETENTION_DAYS=30            # Backup retention period
BACKUP_STORAGE_PATH=/backups        # Backup storage location
```

For detailed configuration, see [Configuration Guide](docs/configuration/README.md).
Additional examples for multi-provider STT, Speechmatics/ElevenLabs setup, and multi-endpoint AMQP fan-out (including TLS) are covered in:
- [STT Providers Guide](docs/features/STT_PROVIDERS.md)
- [AMQP Transcription Guide](docs/features/AMQP_GUIDE.md)

## 📖 Documentation

Comprehensive documentation is available in the [docs](docs/README.md) directory:

- 📚 [Getting Started Guide](docs/getting-started/QUICK_START.md)
- 🔧 [Installation Guide](docs/installation/README.md)
- ⚙️ [Configuration Reference](docs/configuration/README.md)
- 🎙️ [STT Providers Guide](docs/features/STT_PROVIDERS.md)
- 🚀 [Production Deployment](docs/operations/PRODUCTION_DEPLOYMENT.md)
- 🔒 [Security Guide](docs/security/README.md)

## 🏗️ Architecture

SIPREC Server is built with a modular architecture:

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

- `POST /api/sessions/{id}/pause` - Pause recording/transcription for specific session
- `POST /api/sessions/{id}/resume` - Resume recording/transcription for specific session  
- `GET /api/sessions/{id}/pause-status` - Get pause status for specific session
- `POST /api/sessions/pause-all` - Pause all active sessions
- `POST /api/sessions/resume-all` - Resume all paused sessions
- `GET /api/sessions/pause-status` - Get pause status for all sessions

#### Example Usage

```bash
# Pause recording for a specific session
curl -X POST http://localhost:8080/api/sessions/session-123/pause \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": true, "pause_transcription": false}'

# Resume a session
curl -X POST http://localhost:8080/api/sessions/session-123/resume \
  -H "X-API-Key: your-api-key"

# Get pause status
curl -H "X-API-Key: your-api-key" \
  http://localhost:8080/api/sessions/session-123/pause-status

# Pause all sessions
curl -X POST http://localhost:8080/api/sessions/pause-all \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"pause_recording": true, "pause_transcription": true}'
```

### WebSocket

- `WS /ws/transcriptions` - Real-time transcription stream
- `WS /ws/analytics` - Real-time analytics stream with event filtering

#### WebSocket Analytics Events

```javascript
// Connect to analytics WebSocket
const ws = new WebSocket('ws://localhost:8080/ws/analytics?call_id=session-123');

// Receive real-time updates
ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  switch(data.type) {
    case 'analytics_snapshot':
      // Full analytics update
      console.log('Quality Score:', data.quality_score);
      console.log('Sentiment:', data.sentiment_trend);
      break;
    case 'sentiment_alert':
      // Negative sentiment detected
      console.log('Alert:', data.message);
      break;
    case 'audio_quality_alert':
      // Audio quality degraded
      console.log('MOS:', data.mos, 'Packet Loss:', data.packet_loss);
      break;
    case 'compliance_violation':
      // Compliance rule violated
      console.log('Violation:', data.rule_id, data.severity);
      break;
  }
};
```

## 🧪 Testing

### Comprehensive Test Suite

The application includes a complete testing framework with unit tests, integration tests, and end-to-end tests.

#### Running Tests

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

# Run integration tests
make test-integration

# Run end-to-end tests  
make test-e2e

# Generate coverage report
make coverage-html
open coverage.html
```

#### Test Coverage Areas

- **Unit Tests**: Core business logic, parsers, processors
- **Integration Tests**: STT providers, database, message queue
- **E2E Tests**: Complete call flows, SIPREC protocol
- **Performance Tests**: Load testing, concurrent sessions
- **Compliance Tests**: PCI/GDPR requirements validation

### Validate Installation
```bash
# Check service status
systemctl status siprec-server

# Test health endpoint
curl http://localhost:8080/health

# Test SIP response
echo -e "OPTIONS sip:test@localhost SIP/2.0\r\nVia: SIP/2.0/UDP test:5070\r\nFrom: sip:test@test\r\nTo: sip:test@localhost\r\nCall-ID: test\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n" | nc -u localhost 5060
```

### NAT Configuration Test
```bash
# Run comprehensive NAT testing
./test_nat_config.sh

# Check NAT detection
curl -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/external-ip
```

### Load Testing
```bash
# Run performance benchmarks
go test -bench=. ./pkg/siprec/...

# Simulate concurrent calls
./test/load/simulate_calls.sh 100

# SIPREC simulation
go run ./test/e2e/siprec_simulation_test.go
```

## 🏢 Production Deployment

### System Requirements
- **OS**: Ubuntu 20.04+, Debian 11+, CentOS 8+, RHEL 8+
- **Memory**: 4GB RAM minimum, 8GB recommended (16GB for analytics)
- **CPU**: 2 cores minimum, 4 cores recommended (8 cores for analytics)
- **Storage**: 50GB minimum for recordings (500GB+ for long-term storage)
- **Network**: Public IP for external access, 1Gbps recommended
- **Database**: MySQL 8.0+ or MariaDB 10.5+ (optional)
- **Elasticsearch**: 7.10+ for analytics (optional)

### Performance Characteristics
- **Concurrent Sessions**: 500+ simultaneous recordings (with proper resources)
- **Audio Quality**: PCM/G.711/G.722/Opus codec support
- **Latency**: <50ms for real-time streaming transcription
- **Throughput**: 10,000+ RTP packets/second total capacity
- **Analytics Processing**: Real-time with <1s latency
- **Database Operations**: <10ms query response time
- **Storage Upload**: Parallel upload to multiple cloud providers

### High Availability Setup

#### Active-Active Configuration
```yaml
# docker-compose-ha.yml
version: '3.8'
services:
  siprec-1:
    image: ghcr.io/loreste/siprec:latest
    environment:
      - DATABASE_ENABLED=true
      - DB_HOST=mysql-primary
      - REDUNDANCY_ENABLED=true
      - REDUNDANCY_STORAGE_TYPE=redis
      - REDIS_URL=redis://redis-cluster:6379
    deploy:
      replicas: 3
      
  mysql-primary:
    image: mysql:8.0
    environment:
      - MYSQL_REPLICATION_MODE=master
      
  mysql-replica:
    image: mysql:8.0
    environment:
      - MYSQL_REPLICATION_MODE=slave
      
  redis-cluster:
    image: redis:7-alpine
    command: redis-server --cluster-enabled yes
```

#### Load Balancing
```nginx
# nginx.conf
upstream siprec_sip {
    least_conn;
    server siprec-1:5060 max_fails=3 fail_timeout=30s;
    server siprec-2:5060 max_fails=3 fail_timeout=30s;
    server siprec-3:5060 max_fails=3 fail_timeout=30s;
}
```

### Monitoring & Alerting

#### Prometheus Configuration
```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'siprec'
    static_configs:
      - targets: ['siprec-1:8080', 'siprec-2:8080', 'siprec-3:8080']
    metrics_path: '/metrics'
```

#### Key Metrics to Monitor
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
- siprec_storage_upload_duration_seconds

# Analytics Metrics
- siprec_analytics_buffer_size
- siprec_analytics_processing_time_ms
- siprec_websocket_connected_clients
- siprec_compliance_violations_total

# Health Metrics
- siprec_provider_health_score{provider="..."}
- siprec_circuit_breaker_state{provider="..."}
```

#### Grafana Dashboard
Import the provided dashboard from `monitoring/grafana-dashboard.json` for comprehensive visualization.

### Security Hardening

#### PCI DSS Compliance Mode
```bash
# Enable PCI compliance (auto-configures security settings)
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
ufw allow 5062/tcp  # SIP TLS
ufw allow 16384:32768/udp  # RTP range
ufw allow 8080/tcp  # HTTP API (restrict to internal)
```

#### Certificate Management
```bash
# Generate certificates
certbot certonly --standalone -d siprec.example.com

# Auto-renewal
crontab -e
0 0 * * 0 certbot renew --post-hook "systemctl restart siprec-server"
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

# Configuration reload
sudo systemctl reload siprec-server
```

### Troubleshooting
```bash
# Check configuration
./siprec envcheck

# Test connectivity
netstat -tulpn | grep 5060

# Debug NAT issues
./test_nat_config.sh

# View detailed logs
tail -f /var/log/siprec/siprec.log
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

## 📊 Feature Comparison

| Feature | SIPREC Server | Commercial Solutions | Open Source Alternatives |
|---------|---------------|---------------------|--------------------------|
| RFC Compliance | ✅ Full RFC 7865/7866 | ✅ Yes | ⚠️ Limited |
| Real-time Transcription | ✅ 7 Providers | ⚠️ 1-2 Providers | ❌ No |
| Database Persistence | ✅ MySQL/MariaDB | ✅ Yes | ⚠️ Basic |
| Cloud Storage | ✅ S3/GCS/Azure | ✅ Yes | ❌ No |
| Real-time Analytics | ✅ Elasticsearch | ⚠️ Limited | ❌ No |
| WebSocket Streaming | ✅ Yes | ⚠️ Some | ❌ No |
| PCI/GDPR Compliance | ✅ Built-in | ✅ Yes | ❌ No |
| Audio Processing | ✅ Advanced | ⚠️ Basic | ❌ No |
| Clustering/HA | ✅ Yes | ✅ Yes | ❌ No |
| NAT Support | ✅ Advanced | ✅ Yes | ⚠️ Basic |
| Cloud Native | ✅ Optimized | ✅ Yes | ⚠️ Manual |
| Cost | ✅ Open Source | ❌ $$$$ | ✅ Free |
| Customization | ✅ Full Control | ❌ Limited | ✅ Yes |

## 🤝 Contributing

We welcome contributions from the community! Please see our [Contributing Guide](docs/development/CONTRIBUTING.md) for details.

### Development Setup
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

### Coding Standards
- Follow Go best practices and idioms
- Write comprehensive tests for new features
- Update documentation for user-facing changes
- Use conventional commit messages

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

### Professional Support
For enterprise support, custom development, or consulting services, please contact us through the repository.

---

**Built with ❤️ for the VoIP and telecommunications community**
