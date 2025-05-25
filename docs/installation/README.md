# Installation Guide

This guide covers all installation methods for SIPREC Server.

## Installation Methods

1. **[Linux Installation](LINUX.md)** - Automated script for production deployments
2. **[Docker Installation](DOCKER.md)** - Containerized deployment
3. **[Kubernetes Installation](KUBERNETES.md)** - Cloud-native deployment
4. **[Manual Installation](MANUAL.md)** - Build from source

## Quick Decision Guide

| Method | Best For | Pros | Cons |
|--------|----------|------|------|
| **Linux Script** | Production servers | Automated, systemd integration, production-ready | Linux only |
| **Docker** | Testing, microservices | Portable, isolated, easy updates | Overhead, networking complexity |
| **Kubernetes** | Cloud deployments | Scalable, resilient, cloud-native | Complex setup |
| **Manual** | Development, customization | Full control, custom builds | Manual updates |

## System Requirements

### Minimum Requirements
- **OS**: Linux (Ubuntu 20.04+, RHEL 8+, Debian 10+)
- **CPU**: 2 cores
- **RAM**: 2GB
- **Disk**: 10GB + recording storage
- **Go**: 1.21+ (installer handles this)

### Recommended for Production
- **CPU**: 4+ cores
- **RAM**: 8GB
- **Disk**: SSD with 100GB+ for recordings
- **Network**: 1Gbps connection
- **OS**: Ubuntu 22.04 LTS or RHEL 9

### Network Requirements

Open the following ports:

| Port | Protocol | Purpose |
|------|----------|---------|
| 5060 | UDP/TCP | SIP signaling |
| 5061 | TCP | SIP-TLS (optional) |
| 16384-32768 | UDP | RTP media |
| 8080 | TCP | HTTP API/WebSocket |

## Pre-Installation Checklist

- [ ] Server meets minimum requirements
- [ ] Required ports are available
- [ ] Firewall rules configured
- [ ] DNS resolution working
- [ ] Time synchronized (NTP)
- [ ] Storage mounted for recordings

## Post-Installation Steps

1. **Verify Installation**
   ```bash
   curl http://localhost:8080/health
   ```

2. **Configure STT Provider** (optional)
   - See [STT Integration Guide](../features/STT_INTEGRATION.md)

3. **Set Up Monitoring**
   - Configure Prometheus/Grafana
   - See [Monitoring Guide](../operations/MONITORING.md)

4. **Security Hardening**
   - Enable TLS/SRTP
   - Configure firewall
   - See [Security Guide](../security/README.md)

5. **Performance Tuning**
   - Adjust system limits
   - Optimize for your workload
   - See [Performance Guide](../operations/PERFORMANCE_TUNING.md)

## Upgrading

### From Previous Versions

1. **Backup Configuration**
   ```bash
   cp /opt/siprec/.env /opt/siprec/.env.backup
   ```

2. **Stop Service**
   ```bash
   sudo systemctl stop siprec
   ```

3. **Update and Restart**
   - For Linux: Re-run installation script
   - For Docker: Pull new image
   - For Kubernetes: Update deployment

4. **Verify**
   ```bash
   curl http://localhost:8080/health
   ```

## Uninstallation

### Linux
```bash
sudo systemctl stop siprec
sudo systemctl disable siprec
sudo rm -rf /opt/siprec
sudo userdel siprec
```

### Docker
```bash
docker stop siprec
docker rm siprec
```

### Kubernetes
```bash
kubectl delete -f siprec-deployment.yaml
```

## Next Steps

- Follow the [Quick Start Guide](../getting-started/QUICK_START.md)
- Review [Configuration Options](../configuration/README.md)
- Set up [Production Deployment](../operations/PRODUCTION_DEPLOYMENT.md)