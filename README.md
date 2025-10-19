# SIPREC Server

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-GPL%20v3-blue.svg)](LICENSE)
[![Documentation](https://img.shields.io/badge/Docs-Available-brightgreen.svg)](docs/README.md)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](.)
[![NAT Support](https://img.shields.io/badge/NAT-Supported-blue.svg)](docs/configuration/README.md)
[![TCP Optimized](https://img.shields.io/badge/TCP-Optimized-orange.svg)](docs/architecture/SIP_ARCHITECTURE.md)

A high-performance, enterprise-grade SIP recording (SIPREC) server that implements RFC 7865/7866 with custom TCP-optimized transport, advanced real-time transcription capabilities, and comprehensive NAT support for cloud deployments.

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

### Enterprise Features
- **🔐 Security** - End-to-end encryption with TLS/SRTP and configurable key rotation
- **📨 Message Queue** - AMQP integration with delivery guarantees, multi-endpoint fan-out, connection pooling, and TLS support
- **📈 Monitoring** - Comprehensive metrics, health checks, and operational visibility with Prometheus integration
- **☁️ Cloud Ready** - Optimized for GCP, AWS, Azure with automatic configuration
- **⚠️ Advanced Warning System** - Intelligent warning collection with severity levels and automatic resolution
- **✅ Configuration Validation** - Comprehensive startup validation with detailed error reporting
- **🔄 Provider Resilience** - Automatic failover with score-based provider selection

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

# STUN Configuration
STUN_SERVER=stun:stun.l.google.com:19302

# Security
ENABLE_TLS=true                    # Enable TLS for SIP
TLS_CERT_PATH=/path/to/cert.pem    # TLS certificate
TLS_KEY_PATH=/path/to/key.pem      # TLS private key
ENABLE_SRTP=true                   # Enable SRTP for media

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

# AMQP (Message Queue)
AMQP_URL=amqps://guest:guest@rabbitmq.internal:5671/
AMQP_QUEUE_NAME=siprec.transcriptions
AMQP_TLS_ENABLED=true
AMQP_TLS_CA_FILE=/etc/rabbitmq/ca.pem

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

## 🧪 Testing

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
# Run test suite
go test -v ./...

# Run E2E tests
./run_test.sh

# SIPREC simulation
./test/e2e/siprec_simulation_test.go
```

## 🏢 Production Deployment

### System Requirements
- **OS**: Ubuntu 20.04+, Debian 11+, CentOS 8+, RHEL 8+
- **Memory**: 4GB RAM minimum, 8GB recommended
- **CPU**: 2 cores minimum, 4 cores recommended
- **Storage**: 50GB minimum for recordings
- **Network**: Public IP for external access

### Performance Characteristics
- **Concurrent Sessions**: 100+ simultaneous recordings
- **Audio Quality**: PCM/G.711/G.722 codec support
- **Latency**: <100ms for real-time streaming transcription
- **Throughput**: 1000+ RTP packets/second per session
- **Streaming Support**: Real-time interim and final transcription results
- **Provider Agnostic**: Unified interface across all STT vendors

### High Availability Setup
```bash
# Multiple instances with load balancer
# Session state stored in external database
# Shared storage for recordings
# AMQP clustering for message reliability
```

### Monitoring & Alerting
```bash
# Prometheus metrics
curl http://localhost:8080/metrics

# Key metrics to monitor:
# - siprec_active_calls
# - siprec_rtp_packets_received
# - siprec_transcription_errors
# - siprec_session_duration
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

## 📊 Comparison

| Feature | SIPREC Server | Commercial Solutions | Open Source Alternatives |
|---------|---------------|---------------------|--------------------------|
| RFC Compliance | ✅ Full RFC 7865/7866 | ✅ Yes | ⚠️ Limited |
| Real-time Transcription | ✅ Multi-provider | ✅ Yes | ❌ No |
| NAT Support | ✅ Advanced | ✅ Yes | ⚠️ Basic |
| Cloud Ready | ✅ Optimized | ✅ Yes | ⚠️ Manual |
| Cost | ✅ Open Source | ❌ Expensive | ✅ Free |
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
