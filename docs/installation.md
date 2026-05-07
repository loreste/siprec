# Installation Guide

This guide covers installing IZI SIPREC v1.2.1 on various platforms.

## System Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 1 GB | 4+ GB |
| Disk | 10 GB | 100+ GB (for recordings) |
| OS | Linux (kernel 4.x+), macOS 12+ | Ubuntu 22.04 LTS, Rocky Linux 9 |
| Go | 1.25+ | 1.25+ |

## Prerequisites

### Go 1.25+

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install -y golang-go

# Or install latest from golang.org
wget https://go.dev/dl/go1.25.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.25.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin

# Verify
go version
```

### G.729 Codec Support (bcg729)

G.729 decoding requires the bcg729 native library. Without it, G.729 audio streams will fail to decode.

**Ubuntu/Debian:**
```bash
sudo apt-get install -y libbcg729-dev

# Or build from source:
git clone https://gitlab.linphone.org/BC/public/bcg729.git
cd bcg729
cmake .
make
sudo make install
sudo ldconfig
```

**macOS:**
```bash
brew install bcg729
```

**RHEL/CentOS/Rocky Linux:**
```bash
sudo dnf install -y epel-release
sudo dnf install -y bcg729-devel

# Or build from source (same as Ubuntu)
```

**Verify bcg729 installation:**
```bash
# Should find the library
ldconfig -p | grep bcg729
# or on macOS:
brew list bcg729
```

## Installation Methods

### Method 1: Build from Source

```bash
# Clone the repository
git clone https://github.com/loreste/siprec.git
cd siprec

# Build the server and CLI
CGO_ENABLED=1 go build -o siprec ./cmd/siprec
go build -o siprecctl ./cmd/siprecctl

# Verify build
./siprec --version
./siprecctl --version
```

### Method 2: Docker

```bash
# Build the image
docker build -t izi-siprec .

# Run with basic settings
docker run -d \
  --name siprec \
  -p 5060:5060/udp \
  -p 5060:5060/tcp \
  -p 8080:8080 \
  -v $(pwd)/recordings:/recordings \
  -v $(pwd)/config.yaml:/etc/siprec/config.yaml \
  izi-siprec

# Or use docker-compose for full stack (with Redis, RabbitMQ)
docker-compose up -d
```

### Method 3: Linux Service Installation

```bash
# Build the binary
CGO_ENABLED=1 go build -o siprec ./cmd/siprec
go build -o siprecctl ./cmd/siprecctl

# Install binaries
sudo cp siprec /usr/local/bin/
sudo cp siprecctl /usr/local/bin/
sudo chmod +x /usr/local/bin/siprec /usr/local/bin/siprecctl

# Create directories
sudo mkdir -p /etc/siprec
sudo mkdir -p /var/lib/siprec/recordings
sudo mkdir -p /var/log/siprec

# Create service user
sudo useradd -r -s /bin/false siprec

# Set ownership
sudo chown -R siprec:siprec /var/lib/siprec
sudo chown -R siprec:siprec /var/log/siprec

# Copy configuration
sudo cp config.example.yaml /etc/siprec/config.yaml
sudo chown siprec:siprec /etc/siprec/config.yaml
sudo chmod 600 /etc/siprec/config.yaml

# Install systemd service
sudo cp siprec-server.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable siprec-server
sudo systemctl start siprec-server

# Check status
sudo systemctl status siprec-server
```

## Configuration

### Minimal Configuration

Create `/etc/siprec/config.yaml`:

```yaml
network:
  host: "0.0.0.0"
  ports:
    - 5060

http:
  port: 8080

media:
  recording_dir: "/var/lib/siprec/recordings"
  rtp_port_min: 10000
  rtp_port_max: 20000
  rtp_timeout: 30s

logging:
  level: "info"
```

### Production Configuration

```yaml
network:
  host: "0.0.0.0"
  ports:
    - 5060
  enable_tcp: true
  enable_tls: false

http:
  port: 8080

media:
  recording_dir: "/var/lib/siprec/recordings"
  rtp_port_min: 10000
  rtp_port_max: 20000
  rtp_timeout: 30s
  combine_legs: true

logging:
  level: "info"
  format: "json"

# Optional: Redis for session persistence
session:
  store_type: "redis"
  redis_url: "redis://localhost:6379"

# Optional: Speech-to-text
stt:
  enabled: false
  default_vendor: "google"
```

See [Configuration Guide](configuration.md) for all options.

## Firewall Configuration

Open the following ports:

| Port | Protocol | Purpose |
|------|----------|---------|
| 5060 | UDP/TCP | SIP signaling |
| 8080 | TCP | HTTP API / Health checks |
| 10000-20000 | UDP | RTP media (configurable) |

**UFW (Ubuntu):**
```bash
sudo ufw allow 5060/udp
sudo ufw allow 5060/tcp
sudo ufw allow 8080/tcp
sudo ufw allow 10000:20000/udp
```

**firewalld (RHEL/Rocky):**
```bash
sudo firewall-cmd --permanent --add-port=5060/udp
sudo firewall-cmd --permanent --add-port=5060/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=10000-20000/udp
sudo firewall-cmd --reload
```

## Verification

### Health Check

```bash
# HTTP health endpoint
curl http://localhost:8080/health

# Expected response:
# {"status":"healthy","version":"1.2.0",...}

# Using siprecctl
siprecctl health
```

### SIP Connectivity Test

```bash
# Using sipsak (if installed)
sipsak -s sip:test@localhost:5060

# Or send a test OPTIONS request
siprecctl status
```

### Test Recording

Configure your SBC to send SIPREC sessions to the server and verify:

```bash
# Check active sessions
siprecctl sessions list

# Check recordings directory
ls -la /var/lib/siprec/recordings/

# View logs
sudo journalctl -u siprec-server -f
```

## Troubleshooting

### Build Fails with bcg729 Error

```
github.com/pidato/audio/g729: build constraints exclude all Go files
```

**Solution:** Install bcg729 development files:
```bash
# Ubuntu/Debian
sudo apt-get install libbcg729-dev

# macOS
brew install bcg729
```

### Permission Denied on Port 5060

```
listen udp 0.0.0.0:5060: bind: permission denied
```

**Solution:** Either run as root, use a port > 1024, or grant capabilities:
```bash
sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/siprec
```

### No RTP Packets Received

Check firewall rules and verify RTP port range is open:
```bash
# Test UDP connectivity
nc -vzu localhost 10000

# Check logs for RTP binding
grep "Binding RTP listener" /var/log/siprec/server.log
```

### High Memory Usage

Configure memory limits in config.yaml:
```yaml
resources:
  max_memory_mb: 4096
  max_concurrent_calls: 1000
```

## Upgrading

### From v1.1.x to v1.2.1

1. Stop the service:
   ```bash
   sudo systemctl stop siprec-server
   ```

2. Backup configuration:
   ```bash
   cp /etc/siprec/config.yaml /etc/siprec/config.yaml.bak
   ```

3. Build and install new version:
   ```bash
   cd siprec
   git pull
   CGO_ENABLED=1 go build -o siprec ./cmd/siprec
   sudo cp siprec /usr/local/bin/
   ```

4. Start the service:
   ```bash
   sudo systemctl start siprec-server
   ```

No configuration changes are required for v1.2.1. New features (SSRC validation, port cooldown, per-stream G.729 decoder) are enabled automatically.

## Next Steps

- [Configuration Guide](configuration.md) - Full configuration reference
- [Audio Pipeline](audio-pipeline.md) - RTP processing architecture
- [Vendor Integration](vendor-integration.md) - SBC-specific setup
- [API Reference](api.md) - HTTP API documentation
