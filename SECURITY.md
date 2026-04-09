# Security Policy

## Supported Versions

The following versions of SIPREC Server receive security updates:

| Version | Supported          |
| ------- | ------------------ |
| 1.2.x   | :white_check_mark: |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take the security of SIPREC Server seriously. If you discover a security vulnerability, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

### How to Report

1. **GitHub Private Reporting**: Use [GitHub's private vulnerability reporting](https://github.com/loreste/siprec/security/advisories/new) to submit a report directly.
2. **Email**: Send details to the repository owner via their GitHub profile contact information.

### What to Include

- A description of the vulnerability and its potential impact
- Steps to reproduce the issue
- Affected versions
- Any suggested fix (if available)

### What to Expect

- **Acknowledgment**: Within 48 hours of your report
- **Status update**: Within 7 days with an initial assessment
- **Resolution timeline**: Critical vulnerabilities will be patched as soon as possible, typically within 14 days

### Scope

The following areas are in scope for security reports:

- SIP/RTP protocol handling and session management
- Authentication and authorization (TLS, mTLS, API keys)
- Audio recording storage and encryption
- STT provider credential handling
- AMQP/messaging credential and connection security
- PII detection and compliance features
- HTTP API endpoints
- Configuration file parsing and environment variable handling

### Out of Scope

- Denial of service via high-volume SIP traffic (expected operational concern)
- Issues in third-party dependencies (report these upstream; we monitor via Dependabot)
- Security issues in development/test configurations

## Security Practices

- **Dependency scanning**: Dependabot monitors all Go module dependencies for known vulnerabilities
- **Race condition testing**: The test suite runs with Go's `-race` detector
- **Leak detection**: Automated tests verify no goroutine or memory leaks in critical paths (AMQP, STT)
- **Circuit breaker isolation**: AMQP and STT subsystems are isolated so failures cannot cascade into core SIP/RTP handling
- **TLS support**: All external connections (SIP, AMQP, STT providers, HTTP API) support TLS/mTLS
- **PII protection**: Built-in PII detection and redaction for compliance requirements
