# SIPREC Server

[![Go Version](https://img.shields.io/badge/Go-1.21%2B-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Documentation](https://img.shields.io/badge/Docs-Available-brightgreen.svg)](docs/README.md)

A high-performance, production-ready SIP recording (SIPREC) server that implements RFC 7865/7866 with real-time transcription capabilities.

## ✨ Key Features

- **📞 SIPREC Protocol** - Full RFC 7865/7866 compliance for SIP session recording
- **🎙️ Real-time Transcription** - Integration with multiple Speech-to-Text providers
- **🔐 Security** - TLS/SRTP support with end-to-end encryption options
- **📊 Scalable** - Handle thousands of concurrent sessions
- **🌐 WebSocket Streaming** - Real-time transcription delivery
- **📨 Message Queue** - AMQP integration for reliable message delivery
- **🎵 Audio Processing** - VAD, noise reduction, multi-channel support
- **📈 Production Ready** - Health checks, metrics, and comprehensive monitoring

## 🚀 Quick Start

### Installation

```bash
# Linux installation (recommended)
wget https://raw.githubusercontent.com/loreste/siprec/main/install_siprec_linux.sh
chmod +x install_siprec_linux.sh
sudo ./install_siprec_linux.sh
```

### Docker

```bash
docker run -d \
  --name siprec \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/opt/siprec/recordings \
  ghcr.io/loreste/siprec:latest
```

### Basic Configuration

Create a `.env` file:

```env
# Network
SIP_PORTS=5060
EXTERNAL_IP=auto

# STT Provider
STT_VENDORS=mock  # or google, deepgram, openai, etc.

# Audio Processing
VAD_ENABLED=true
NOISE_REDUCTION_ENABLED=true
```

For detailed configuration, see [Configuration Guide](docs/configuration/README.md).

## 📖 Documentation

Comprehensive documentation is available in the [docs](docs/README.md) directory:

- 📚 [Getting Started Guide](docs/getting-started/QUICK_START.md)
- 🔧 [Installation Guide](docs/installation/README.md)
- ⚙️ [Configuration Reference](docs/configuration/README.md)
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

### WebSocket

- `WS /ws/transcriptions` - Real-time transcription stream

See [API Reference](docs/reference/README.md) for details.

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](docs/development/CONTRIBUTING.md) for details.

### Development Setup

```bash
# Clone the repository
git clone https://github.com/loreste/siprec.git
cd siprec

# Install dependencies
go mod download

# Run tests
make test

# Build
make build
```

## 📊 Performance

SIPREC Server is designed for high performance:

- Handle 1000+ concurrent sessions
- Process 50,000+ RTP packets/second
- Sub-100ms transcription latency
- Minimal CPU and memory footprint

See [Performance Tuning Guide](docs/operations/RESOURCE_OPTIMIZATION.md) for optimization tips.

## 🔐 Security

Security features include:

- TLS 1.3 for SIP signaling
- SRTP for media encryption
- End-to-end encryption for recordings
- API authentication
- IP whitelisting

See [Security Guide](docs/security/README.md) for configuration.

## 📝 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built with [sipgo](https://github.com/emiago/sipgo) for SIP handling
- Uses [pion/sdp](https://github.com/pion/sdp) for SDP parsing
- Integrates with multiple STT providers

## 📞 Support

- 📚 [Documentation](docs/README.md)
- 🐛 [Issue Tracker](https://github.com/loreste/siprec/issues)
- 💬 [Discussions](https://github.com/loreste/siprec/discussions)

---

**Current Version:** v1.0.0 | **Go Version:** 1.21+ | **Status:** Production Ready