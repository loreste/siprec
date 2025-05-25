# Security Hardening Guide

Comprehensive guide for hardening SIPREC Server deployments.

## System Hardening

### Operating System

#### Linux Hardening

1. **Kernel Parameters**
```bash
# /etc/sysctl.conf
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_max_syn_backlog = 2048
net.ipv4.tcp_synack_retries = 2
net.ipv4.ip_forward = 0
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
net.ipv4.icmp_echo_ignore_broadcasts = 1
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.all.accept_redirects = 0
```

2. **File System**
```bash
# Mount with security options
/var/log/siprec /var/log/siprec ext4 defaults,noexec,nosuid 0 2

# Secure permissions
chmod 700 /etc/siprec
chmod 600 /etc/siprec/*.key
chmod 644 /etc/siprec/*.crt
```

3. **User Management**
```bash
# Create dedicated user
useradd -r -s /bin/false -d /var/lib/siprec siprec

# Set ownership
chown -R siprec:siprec /etc/siprec
chown -R siprec:siprec /var/log/siprec
chown -R siprec:siprec /var/lib/siprec
```

### Network Hardening

#### Firewall Rules

```bash
# iptables rules
# Allow SIP
iptables -A INPUT -p udp --dport 5060 -j ACCEPT
iptables -A INPUT -p tcp --dport 5060 -j ACCEPT

# Allow RTP range
iptables -A INPUT -p udp --dport 30000:40000 -j ACCEPT

# Allow HTTP API (internal only)
iptables -A INPUT -p tcp --dport 8080 -s 10.0.0.0/8 -j ACCEPT

# Drop all other
iptables -A INPUT -j DROP
```

#### Network Segmentation

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Internet  │────▶│   DMZ       │────▶│  Internal   │
│             │     │  (SIPREC)   │     │  (Services) │
└─────────────┘     └─────────────┘     └─────────────┘
                          │                     │
                    ┌─────┴─────┐         ┌─────┴─────┐
                    │ Firewall  │         │ Database  │
                    └───────────┘         └───────────┘
```

## Application Hardening

### Build Security

1. **Secure Build Flags**
```makefile
GOFLAGS = -trimpath -ldflags="-s -w"
CGO_ENABLED = 0
```

2. **Dependency Scanning**
```bash
# Check for vulnerabilities
go list -json -m all | nancy sleuth

# Update dependencies
go get -u ./...
go mod tidy
```

### Runtime Security

1. **Process Isolation**
```yaml
# systemd service
[Service]
Type=simple
User=siprec
Group=siprec
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/siprec /var/log/siprec
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
```

2. **Resource Limits**
```bash
# /etc/security/limits.d/siprec.conf
siprec soft nofile 65536
siprec hard nofile 65536
siprec soft nproc 4096
siprec hard nproc 4096
siprec soft memlock unlimited
siprec hard memlock unlimited
```

### Container Security

#### Docker Hardening

```dockerfile
# Dockerfile.secure
FROM scratch
COPY --from=builder /app/siprec /siprec
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

USER 10001:10001
EXPOSE 5060/udp 5060/tcp 8080/tcp

ENTRYPOINT ["/siprec"]
```

#### Security Scanning

```bash
# Scan image
docker scan siprec:latest

# Run with security options
docker run -d \
  --name siprec \
  --read-only \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  --cap-add NET_BIND_SERVICE \
  -p 5060:5060 \
  siprec:latest
```

## Secrets Management

### Environment Variables

```bash
# Use secrets management
export OPENAI_API_KEY=$(vault kv get -field=key secret/siprec/openai)
export ENCRYPTION_KEY=$(vault kv get -field=key secret/siprec/encryption)
```

### File-based Secrets

```yaml
# kubernetes secrets
apiVersion: v1
kind: Secret
metadata:
  name: siprec-secrets
type: Opaque
data:
  openai-key: <base64-encoded>
  encryption-key: <base64-encoded>
```

## Monitoring & Detection

### Security Monitoring

1. **Log Analysis**
```bash
# Monitor for attacks
grep -E "(INVITE.*sip:.*@.*|REGISTER)" /var/log/siprec/sip.log | \
  awk '{print $1}' | sort | uniq -c | sort -rn | head -20
```

2. **Intrusion Detection**
```yaml
# Falco rules
- rule: Suspicious SIP Traffic
  desc: Detect suspicious SIP patterns
  condition: >
    siprec_sip and (
      sip.method = "REGISTER" and 
      sip.user_agent contains "sipvicious"
    )
  output: "Suspicious SIP scan detected"
  priority: WARNING
```

### Security Metrics

Monitor these metrics:
- Failed authentication attempts
- Unusual traffic patterns
- Resource exhaustion
- Error rates
- Connection patterns

## Compliance Hardening

### HIPAA Compliance

```bash
# Encryption at rest
ENCRYPTION_ENABLED=true
ENCRYPTION_ALGORITHM=AES-256-GCM

# Audit logging
AUDIT_LOG_ENABLED=true
AUDIT_LOG_RETENTION=7y
AUDIT_LOG_ENCRYPTION=true

# Access controls
AUTH_PROVIDER=saml
SESSION_TIMEOUT=15m
MFA_REQUIRED=true
```

### PCI-DSS Compliance

```bash
# Network segmentation
BIND_INTERNAL_ONLY=true
IP_WHITELIST_ENABLED=true

# Strong cryptography
TLS_MIN_VERSION=1.2
TLS_CIPHER_SUITES=TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384

# Key management
KEY_ROTATION_ENABLED=true
KEY_ROTATION_PERIOD=90d
```

## Incident Response

### Preparation

1. **Incident Response Plan**
   - Detection procedures
   - Escalation paths
   - Communication plan
   - Recovery procedures

2. **Forensics Preparation**
```bash
# Enable detailed logging
LOG_LEVEL=debug
LOG_FORENSICS=true
LOG_RETAIN_DAYS=90
```

### Detection

1. **Automated Alerts**
```yaml
alerts:
  - name: brute_force_attack
    condition: failed_auth > 10 in 1m
    action: block_ip
    
  - name: dos_attack
    condition: requests > 1000 in 1s
    action: rate_limit
```

2. **Manual Investigation**
```bash
# Check for scanning
tail -f /var/log/siprec/sip.log | grep -E "OPTIONS|REGISTER"

# Check for exploitation
grep -i "script\|union\|select" /var/log/siprec/http.log
```

## Security Checklist

### Pre-Deployment

- [ ] OS hardened and patched
- [ ] Firewall rules configured
- [ ] TLS certificates installed
- [ ] Secrets management configured
- [ ] Security scanning completed

### Deployment

- [ ] Running as non-root user
- [ ] File permissions secured
- [ ] Network segmentation in place
- [ ] Monitoring configured
- [ ] Backup procedures tested

### Post-Deployment

- [ ] Security audit completed
- [ ] Penetration testing done
- [ ] Incident response tested
- [ ] Documentation updated
- [ ] Team training completed

## Security Tools

### Recommended Tools

1. **Scanning**
   - `nmap` - Network scanning
   - `nikto` - Web vulnerability scanning
   - `ossec` - Host intrusion detection

2. **Monitoring**
   - `fail2ban` - Automated blocking
   - `aide` - File integrity monitoring
   - `auditd` - System audit logging

3. **Analysis**
   - `wireshark` - Network analysis
   - `tcpdump` - Packet capture
   - `sipvicious` - SIP testing