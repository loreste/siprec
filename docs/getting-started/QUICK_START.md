# Quick Start Guide

Get your SIPREC server up and running in minutes!

## Prerequisites

- Linux server (Ubuntu 20.04+ or RHEL 8+ recommended)
- Go 1.21+ (installer will handle this)
- 2GB RAM minimum
- Open ports: 5060 (SIP), 16384-32768 (RTP), 8080 (HTTP)

## 1. Installation (5 minutes)

### Option A: Automated Linux Installation (Recommended)

```bash
wget https://raw.githubusercontent.com/loreste/siprec/main/install_siprec_linux.sh
chmod +x install_siprec_linux.sh
sudo ./install_siprec_linux.sh
```

This script will:
- Install Go if needed
- Create siprec user and directories
- Build and install the server
- Configure systemd service
- Set up firewall rules
- Start the service

### Option B: Docker

```bash
docker run -d \
  --name siprec \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 16384-32768:16384-32768/udp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/opt/siprec/recordings \
  ghcr.io/loreste/siprec:latest
```

### Option C: Manual Build

```bash
git clone https://github.com/loreste/siprec.git
cd siprec
go build -o siprec-server ./cmd/siprec
./siprec-server
```

## 2. Verify Installation

Check that the server is running:

```bash
# Check service status
sudo systemctl status siprec

# Check health endpoint
curl http://localhost:8080/health

# View logs
sudo journalctl -u siprec -f
```

## 3. Test with SIP Client

### Send a test OPTIONS request:

```bash
# Using sipsak
sipsak -vv -s sip:test@your-server-ip:5060

# Using ncat
echo -e "OPTIONS sip:test@localhost:5060 SIP/2.0\r\nVia: SIP/2.0/UDP localhost:5999\r\nFrom: <sip:test@localhost>\r\nTo: <sip:test@localhost>\r\nCall-ID: test123\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n" | nc -u localhost 5060
```

### Send a test SIPREC INVITE:

Create a test script `test_siprec.sh`:

```bash
#!/bin/bash
./test_options.sh  # First test with OPTIONS
```

## 4. Configure Your SIP PBX

### For Asterisk:

Add to `sip.conf`:
```ini
[siprec]
type=peer
host=your-siprec-server-ip
port=5060
context=siprec-context
```

### For FreeSWITCH:

Add to SIP profile:
```xml
<param name="siprec-server" value="sip:your-siprec-server-ip:5060"/>
```

### For Kamailio:

```cfg
modparam("siprec", "siprec_server", "sip:your-siprec-server-ip:5060")
```

## 5. Basic Configuration

The default configuration (`/opt/siprec/.env`) works out of the box with:
- Mock STT provider (no external dependencies)
- Local recording storage
- Basic audio processing enabled

To customize, edit `/opt/siprec/.env`:

```bash
# Change STT provider (requires credentials)
STT_VENDORS=google
GOOGLE_APPLICATION_CREDENTIALS=/path/to/credentials.json

# Enable AMQP
AMQP_URL=amqp://user:pass@rabbitmq:5672/
AMQP_QUEUE_NAME=transcriptions

# Enable TLS/SRTP
TLS_ENABLED=true
TLS_CERT_FILE=/path/to/cert.pem
TLS_KEY_FILE=/path/to/key.pem
```

## 6. Monitor Your Server

### View real-time transcriptions:

Open `http://your-server-ip:8080/websocket-client` in a browser.

### Check metrics:

```bash
curl http://localhost:8080/metrics
```

### Health monitoring:

```bash
# Detailed health check
curl http://localhost:8080/health?detailed=true

# Simple liveness check
curl http://localhost:8080/health/live
```

## 7. Next Steps

- 📖 Read the [Configuration Guide](../configuration/README.md) for all options
- 🔒 Review [Security Best Practices](../security/README.md)
- 🚀 Follow [Production Deployment Guide](../operations/PRODUCTION_DEPLOYMENT.md)
- 🔧 Explore [API Reference](../development/API_REFERENCE.md)

## Common Issues

### Port 5060 already in use
```bash
# Find what's using the port
sudo lsof -i :5060
# Stop the conflicting service or change SIPREC port in .env
```

### No audio in recordings
- Check firewall allows UDP ports 16384-32768
- Verify RTP is reaching the server with tcpdump
- Check logs for RTP timeout errors

### STT not working
- Verify credentials are set correctly
- Check network connectivity to STT provider
- Try mock provider first: `STT_VENDORS=mock`

## Getting Help

- Check [Troubleshooting Guide](../operations/TROUBLESHOOTING.md)
- View logs: `sudo journalctl -u siprec -f`
- Open an [issue](https://github.com/loreste/siprec/issues)

---

🎉 **Congratulations!** Your SIPREC server is ready to record and transcribe calls!