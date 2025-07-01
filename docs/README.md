# SIPREC Server Documentation

Welcome to the SIPREC Server documentation. This guide will help you navigate through all available documentation.

## 📚 Documentation Structure

### Getting Started
- [Installation Guide](installation/README.md) - How to install SIPREC Server
  - [Linux Installation](installation/LINUX.md)
  - [Docker Installation](installation/DOCKER.md)
  - [Kubernetes Deployment](installation/KUBERNETES.md)
- [Quick Start Guide](getting-started/QUICK_START.md) - Get up and running quickly
- [Configuration Reference](configuration/README.md) - All configuration options

### Core Features
- [SIP/SIPREC Implementation](features/SIP_SIPREC.md) - RFC 7865/7866 compliance
- [Audio Processing](features/AUDIO_PROCESSING.md) - VAD, noise reduction, multi-channel
- [Speech-to-Text Integration](features/STT_INTEGRATION.md) - Provider setup and usage
- [PII Detection & Redaction](features/PII_DETECTION.md) - Automatic PII detection and redaction
- [Real-time Streaming](features/WEBSOCKET_STREAMING.md) - WebSocket transcription
- [Message Queue Integration](features/AMQP_INTEGRATION.md) - AMQP/RabbitMQ setup
- [Pause/Resume Control](features/PAUSE_RESUME_API.md) - Session control via REST API

### Security
- [Security Overview](security/README.md) - Security features and best practices
- [TLS/SRTP Configuration](security/TLS_SRTP.md) - Secure communications
- [Encryption](security/ENCRYPTION.md) - Recording and metadata encryption
- [Authentication & Authorization](security/AUTH.md) - Access control

### Operations
- [Production Deployment](operations/PRODUCTION_DEPLOYMENT.md) - Production setup guide
- [Performance Tuning](operations/PERFORMANCE_TUNING.md) - Optimization guide
- [Monitoring & Metrics](operations/MONITORING.md) - Health checks and metrics
- [Troubleshooting](operations/TROUBLESHOOTING.md) - Common issues and solutions
- [Backup & Recovery](operations/BACKUP_RECOVERY.md) - Data protection

### Development
- [Architecture Overview](development/ARCHITECTURE.md) - System design
- [API Reference](development/API_REFERENCE.md) - HTTP/WebSocket APIs
- [Package Documentation](development/PACKAGES.md) - Code organization
- [Testing Guide](development/TESTING.md) - Running and writing tests
- [Contributing](development/CONTRIBUTING.md) - How to contribute

### Reference
- [Configuration Reference](reference/CONFIGURATION.md) - All config options
- [Error Codes](reference/ERROR_CODES.md) - Error handling
- [RFC Compliance](reference/RFC_COMPLIANCE.md) - Standards compliance
- [Changelog](../CHANGELOG.md) - Version history
- [License](../LICENSE) - License information

## 🚀 Quick Links

- **New to SIPREC?** Start with the [Quick Start Guide](getting-started/QUICK_START.md)
- **Installing on Linux?** See [Linux Installation](installation/LINUX.md)
- **Setting up production?** Read [Production Deployment](operations/PRODUCTION_DEPLOYMENT.md)
- **Need help?** Check [Troubleshooting](operations/TROUBLESHOOTING.md)

## 📖 Documentation Versions

This documentation is for SIPREC Server v1.0.0. For other versions, see the [releases page](https://github.com/loreste/siprec/releases).

## 🤝 Contributing to Documentation

Found an issue or want to improve the documentation? See our [Contributing Guide](development/CONTRIBUTING.md).