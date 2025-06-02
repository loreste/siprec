# SIPREC Server Production Deployment Guide

## Overview

This guide covers deploying the SIPREC Server in a production environment. The server is a high-performance SIP recording solution that supports real-time transcription and complies with RFC 7865/7866.

## Prerequisites

- Go 1.21+ installed
- Linux server (Ubuntu 20.04+ or RHEL 8+ recommended)
- Minimum 2GB RAM, 4+ CPU cores recommended
- Open ports: 5060-5061 (SIP), 16384-32768 (RTP), 8080 (HTTP/API)
- (Optional) Google Cloud credentials for Speech-to-Text

## Quick Start

1. **Clone the repository:**
   ```bash
   git clone https://github.com/loreste/siprec.git
   cd siprec
   ```

2. **Run the installation script:**
   ```bash
   chmod +x install_siprec_linux.sh
   ./install_siprec_linux.sh
   ```

3. **Configure environment:**
   ```bash
   cp .env.production /opt/siprec-server/.env
   # Edit /opt/siprec-server/.env as needed
   ```

4. **Start the service:**
   ```bash
   sudo systemctl start siprec-server
   sudo systemctl enable siprec-server
   ```

## Configuration

### Essential Settings

Edit `/opt/siprec-server/.env`:

```bash
# Network
EXTERNAL_IP=your.public.ip  # Or "auto" for automatic detection
INTERNAL_IP=your.private.ip  # Or "auto" for automatic detection
SIP_PORTS=5060,5061

# Performance
MAX_CONCURRENT_CALLS=1000
WORKER_POOL_SIZE=100

# STT Provider (choose one)
STT_VENDORS=mock         # For testing without STT
# STT_VENDORS=google     # Requires GOOGLE_APPLICATION_CREDENTIALS
```

### TLS/SRTP Support

For secure communications:

```bash
TLS_ENABLED=true
TLS_CERT_FILE=/path/to/cert.pem
TLS_KEY_FILE=/path/to/key.pem
TLS_PORT=5061
SRTP_ENABLED=true
```

### Message Queue Integration

For AMQP/RabbitMQ integration:

```bash
AMQP_URL=amqp://user:pass@rabbitmq:5672/
AMQP_QUEUE_NAME=siprec_transcriptions
```

## Deployment Options

### 1. Systemd Service (Recommended)

The installation script creates a systemd service. To manage:

```bash
sudo systemctl status siprec-server
sudo systemctl restart siprec-server
sudo systemctl stop siprec-server
```

View logs:
```bash
sudo journalctl -u siprec-server -f
```

### 2. Docker Deployment

Build and run with Docker:

```bash
docker build -t siprec-server .
docker run -d \
  --name siprec \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 5061:5061/tcp \
  -p 16384-32768:16384-32768/udp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/opt/siprec-server/recordings \
  -v $(pwd)/.env.production:/opt/siprec-server/.env \
  siprec-server
```

### 3. Kubernetes Deployment

Use the provided Helm chart or deploy with kubectl:

```bash
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/configmap.yaml
```

## Health Monitoring

### Health Endpoints

- `/health` - Comprehensive health check
- `/health/live` - Kubernetes liveness probe
- `/health/ready` - Kubernetes readiness probe
- `/metrics` - Prometheus metrics

### Example Health Check

```bash
curl http://localhost:8080/health
```

Response:
```json
{
  "status": "healthy",
  "timestamp": "2024-05-24T20:00:00Z",
  "uptime": "1h30m45s",
  "version": "1.0.0",
  "checks": {
    "sip": {"status": "healthy", "message": "SIP service is running"},
    "websocket": {"status": "healthy", "message": "WebSocket hub is running"},
    "sessions": {"status": "healthy", "message": "Session storage operational"},
    "rtp_ports": {"status": "healthy", "message": "RTP ports available"}
  },
  "system": {
    "goroutines": 150,
    "memory_mb": 245,
    "cpu_count": 8,
    "active_calls": 5,
    "ports_in_use": 10
  }
}
```

## Transport Layer Architecture

### Custom SIP Server Implementation

The SIPREC server features a custom SIP implementation optimized for production environments:

#### TCP Transport Optimization
- **Large Metadata Support**: Handles SIPREC messages with large XML metadata (>1KB)
- **Connection Management**: Persistent TCP connections with proper lifecycle management
- **CRLF Parsing**: Robust line-ending handling for reliable message parsing
- **Concurrent Processing**: Each connection handled in separate goroutines

#### Multi-Transport Support
- **UDP**: Traditional SIP transport with fragmentation support
- **TCP**: Reliable transport for large messages, recommended for SIPREC
- **TLS**: Encrypted transport for secure environments

#### Production Benefits
- **Reduced Memory Usage**: Streaming message parsing instead of full buffering
- **Better Reliability**: Proper error handling and connection recovery
- **Scalability**: Designed for high concurrent connection counts
- **RFC Compliance**: Full RFC 7865/7866 SIPREC implementation

### Transport Configuration

For production deployments with large SIPREC metadata:

```bash
# Prefer TCP for reliable large message handling
ENABLE_TCP=true
ENABLE_TLS=true  # For secure environments

# Configure appropriate timeouts
TCP_READ_TIMEOUT=30s
TCP_WRITE_TIMEOUT=30s
CONNECTION_IDLE_TIMEOUT=5m
```

## Performance Tuning

### System Limits

Increase file descriptor limits:

```bash
# /etc/security/limits.conf
siprec soft nofile 65536
siprec hard nofile 65536
```

### Kernel Parameters

Optimize for high traffic and TCP connections:

```bash
# /etc/sysctl.conf
# Network buffer sizes
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.ipv4.udp_mem = 2097152 4194304 8388608
net.core.netdev_max_backlog = 5000

# TCP optimization for SIPREC
net.ipv4.tcp_rmem = 4096 65536 134217728
net.ipv4.tcp_wmem = 4096 65536 134217728
net.ipv4.tcp_max_syn_backlog = 8192
net.core.somaxconn = 8192

# Connection tracking for high concurrency
net.netfilter.nf_conntrack_max = 524288
```

### Resource Allocation

- **Small deployment** (< 100 concurrent calls): 2 CPU, 2GB RAM
- **Medium deployment** (100-500 calls): 4 CPU, 4GB RAM
- **Large deployment** (500+ calls): 8+ CPU, 8GB+ RAM

## Security Considerations

1. **Firewall Rules:**
   ```bash
   # Allow SIP
   sudo ufw allow 5060/udp
   sudo ufw allow 5060/tcp
   sudo ufw allow 5061/tcp
   
   # Allow RTP range
   sudo ufw allow 16384:32768/udp
   
   # Allow HTTP (restrict source IP in production)
   sudo ufw allow from 10.0.0.0/8 to any port 8080
   ```

2. **SIP Security:**
   - Enable TLS for SIP signaling
   - Use SRTP for media encryption
   - Implement IP whitelisting for SIP peers

3. **API Security:**
   - Place behind reverse proxy (nginx/HAProxy)
   - Implement authentication for API endpoints
   - Use HTTPS for all API traffic

## Troubleshooting

### Common Issues

1. **Port already in use:**
   ```bash
   sudo lsof -i :5060
   sudo systemctl stop existing-sip-service
   ```

2. **Google STT not working:**
   ```bash
   export GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json
   # Or use mock STT provider: STT_VENDORS=mock
   ```

3. **High memory usage:**
   - Check `MAX_CONCURRENT_CALLS` setting
   - Monitor with: `curl localhost:8080/metrics`
   - Enable memory profiling in debug mode

### Debug Mode

For troubleshooting:
```bash
LOG_LEVEL=debug ./siprec-server
```

## Backup and Recovery

1. **Recording Backup:**
   ```bash
   # Backup recordings daily
   rsync -av /opt/siprec-server/recordings/ backup-server:/backups/siprec/
   ```

2. **Configuration Backup:**
   ```bash
   cp /opt/siprec-server/.env /backups/siprec-config-$(date +%Y%m%d).env
   ```

## Monitoring Integration

### Prometheus

Metrics are exposed at `/metrics`:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'siprec'
    static_configs:
      - targets: ['siprec-server:8080']
```

### Grafana Dashboard

Import the provided dashboard from `monitoring/grafana-dashboard.json` for:
- Active calls and sessions
- RTP packet statistics
- STT performance metrics
- System resource usage

## Support

- Issues: https://github.com/loreste/siprec/issues
- Documentation: https://github.com/loreste/siprec/wiki
- Version: 1.0.0

## License

See LICENSE file in the repository.