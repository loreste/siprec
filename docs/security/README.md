# Security Documentation

Comprehensive security guide for the SIPREC Server.

## Security Features

- [Security Overview](SECURITY.md) - Security architecture and best practices
- [Encryption Guide](ENCRYPTION.md) - Encryption at rest and in transit
- [Authentication & Authorization](AUTH.md) - Access control mechanisms
- [Security Hardening](HARDENING.md) - Production security hardening

## Quick Security Setup

### Enable TLS for SIP

```bash
SIP_TLS_ENABLED=true
SIP_TLS_CERT=/etc/siprec/tls/cert.pem
SIP_TLS_KEY=/etc/siprec/tls/key.pem
```

### Enable Encryption at Rest

```bash
ENCRYPTION_ENABLED=true
ENCRYPTION_KEY_PATH=/etc/siprec/keys
```

### Enable Rate Limiting

```bash
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=1m
```

## Security Checklist

### Network Security
- [ ] TLS enabled for SIP signaling
- [ ] SRTP enabled for media (if supported)
- [ ] Firewall rules configured
- [ ] Rate limiting enabled
- [ ] DDoS protection in place

### Application Security
- [ ] Authentication enabled
- [ ] API keys rotated regularly
- [ ] Encryption keys managed properly
- [ ] Sensitive data encrypted
- [ ] Security headers configured

### Operational Security
- [ ] Regular security updates
- [ ] Security monitoring enabled
- [ ] Incident response plan
- [ ] Regular security audits
- [ ] Compliance requirements met

## Compliance

The SIPREC Server supports various compliance requirements:

- **GDPR**: Data encryption, retention policies, audit logs
- **HIPAA**: Encryption, access controls, audit trails
- **PCI-DSS**: Network segmentation, encryption, access control
- **SOC 2**: Security controls, monitoring, incident response

## Security Contact

Report security vulnerabilities to: security@example.com

Please do not file public issues for security vulnerabilities.