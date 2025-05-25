# SIPREC Server Installation Guide for Linux

This guide provides detailed instructions for installing and configuring the SIPREC server on Linux systems.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation Options](#installation-options)
  - [Option 1: Docker Installation](#option-1-docker-installation)
  - [Option 2: Native Installation](#option-2-native-installation)
- [Configuration](#configuration)
- [Starting the Server](#starting-the-server)
- [Verifying Installation](#verifying-installation)
- [Troubleshooting](#troubleshooting)
- [Advanced Configuration](#advanced-configuration)

## Prerequisites

Before installing the SIPREC server, ensure your Linux system meets the following requirements:

### System Requirements

- Linux (Ubuntu 20.04+, Debian 11+, CentOS 8+, or similar)
- 2 CPU cores minimum (4+ recommended for production)
- 2GB RAM minimum (4GB+ recommended for production)
- 10GB available disk space for recordings and logs
- Open ports:
  - 5060-5062 (SIP signaling)
  - 10000-20000 (RTP media)
  - 8080 (Health API)

### Required Packages

Install these dependencies before proceeding:

```bash
# For Debian/Ubuntu
sudo apt update
sudo apt install -y git make curl build-essential

# For CentOS/RHEL
sudo dnf install -y git make curl gcc
```

## Installation Options

### Option 1: Docker Installation

The recommended way to install SIPREC server is using Docker, as it handles all dependencies and simplifies setup.

#### 1. Install Docker and Docker Compose

```bash
# Install Docker (if not already installed)
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Install Docker Compose
sudo curl -L "https://github.com/docker/compose/releases/download/v2.18.1/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

# Add your user to the docker group (optional)
sudo usermod -aG docker $USER
newgrp docker  # Apply group changes without logout
```

#### 2. Clone the Repository

```bash
git clone https://github.com/loreste/siprec.git
cd siprec
```

#### 3. Configure Environment

```bash
# Copy example environment file
cp .env.example .env

# Edit configuration (see Configuration section)
nano .env
```

#### 4. Start the Server with Docker Compose

```bash
# Create required directories (automatically handled by docker-compose)
mkdir -p recordings certs sessions

# Start all services (SIPREC server and RabbitMQ)
docker-compose up -d

# View logs
docker-compose logs -f
```

### Option 2: Native Installation

For systems where Docker isn't available or for development purposes, follow these steps for native installation.

#### 1. Install Go

First, install Go 1.22 or newer:

```bash
# Download and install Go 1.22.0
wget https://go.dev/dl/go1.22.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.22.0.linux-amd64.tar.gz

# Add Go to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
source ~/.profile

# Verify Go installation
go version
```

#### 2. Clone the Repository

```bash
git clone https://github.com/loreste/siprec.git
cd siprec
```

#### 3. Install Dependencies

```bash
# Install Go dependencies
go mod download
```

#### 4. Configure Environment

```bash
# Copy example environment file
cp .env.example .env

# Edit configuration (see Configuration section)
nano .env
```

#### 5. Build the Application

```bash
# Create required directories
mkdir -p recordings sessions

# Build the application
go build -o siprec-server ./cmd/siprec/

# For production builds with optimization
go build -ldflags="-s -w" -o siprec-server ./cmd/siprec/
```

The `-ldflags="-s -w"` option reduces the binary size by removing debugging information.

#### Cross-Compilation for Different Linux Architectures

If you need to build for a different architecture:

```bash
# For 64-bit Linux (most common)
GOOS=linux GOARCH=amd64 go build -o siprec-server-amd64 ./cmd/siprec/

# For 32-bit Linux
GOOS=linux GOARCH=386 go build -o siprec-server-386 ./cmd/siprec/

# For ARM64 (e.g., Raspberry Pi 4 with 64-bit OS)
GOOS=linux GOARCH=arm64 go build -o siprec-server-arm64 ./cmd/siprec/

# For ARM (e.g., older Raspberry Pi models)
GOOS=linux GOARCH=arm go build -o siprec-server-arm ./cmd/siprec/
```

## Configuration

The SIPREC server is configured via the `.env` file. Key configuration options include:

### Basic Configuration

```properties
# Basic SIP configuration
EXTERNAL_IP=auto              # Set to 'auto' for auto-detection or specify your server's public IP
INTERNAL_IP=auto              # Set to 'auto' for auto-detection or specify your server's local IP
PORTS=5060,5061               # Comma-separated list of SIP ports

# RTP configuration
RTP_PORT_MIN=10000            # Minimum port for RTP media
RTP_PORT_MAX=20000            # Maximum port for RTP media

# Recording configuration
RECORDING_DIR=./recordings    # Directory to store recordings
```

### Session Redundancy Configuration

```properties
# Session Redundancy configuration
ENABLE_REDUNDANCY=true        # Enable/disable session redundancy
SESSION_TIMEOUT=30s           # Timeout for session inactivity (30s, 1m, etc.)
SESSION_CHECK_INTERVAL=10s    # Interval for checking session health
REDUNDANCY_STORAGE_TYPE=memory # Storage type for redundancy (memory, redis planned)
```

### Advanced Configuration

```properties
# Resource limits
MAX_CONCURRENT_CALLS=500      # Maximum concurrent calls

# Logging
LOG_LEVEL=info                # Logging level (debug, info, warn, error, fatal)
```

### Configuration Notes

- **EXTERNAL_IP**: Set to your server's public IP address. If set to `auto`, the server will attempt to detect it automatically.
- **INTERNAL_IP**: Should be set to your server's local IP address. If set to `auto`, the server will use the primary network interface.
- **RECORDING_DIR**: Ensure this directory exists and is writable by the server.

## Starting the Server

### Starting with Docker Compose

If you installed using Docker Compose:

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Stop all services
docker-compose down
```

### Starting Natively

If you installed natively:

```bash
# Start the server
make run

# Run as a service (optional)
# Create a systemd service file at /etc/systemd/system/siprec.service
```

Example systemd service file:

```ini
[Unit]
Description=SIPREC Server
After=network.target

[Service]
User=siprec
WorkingDirectory=/path/to/siprec
ExecStart=/path/to/siprec/siprec-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Then enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable siprec
sudo systemctl start siprec
sudo systemctl status siprec
```

## Verifying Installation

After starting the server, verify it's running correctly:

### Check Server Health

```bash
# Check server health API
curl http://localhost:8080/health

# Check STUN status
curl http://localhost:8080/stun-status

# Check metrics
curl http://localhost:8080/metrics
```

### Check Open Ports

```bash
# Verify SIP ports are listening
sudo netstat -tuln | grep 506
sudo netstat -tuln | grep 8080

# Check RTP port range
sudo netstat -tuln | grep -E '100[0-9][0-9]|1[1-9][0-9][0-9][0-9]|20000'
```

### Test with SIP Tools

You can test the server using SIP testing tools:

```bash
# Example using SIPp (if installed)
sipp -sf siprec_invite.xml -m 1 -s 1000 <server-ip>:5060
```

## Troubleshooting

Here are solutions to common installation issues:

### Docker Issues

1. **Container fails to start**:
   ```bash
   # Check container logs
   docker-compose logs siprec
   
   # Check container status
   docker-compose ps
   ```

2. **Port conflicts**:
   ```bash
   # Check if ports are already in use
   sudo netstat -tuln | grep 5060
   sudo netstat -tuln | grep 8080
   
   # Modify ports in docker-compose.yml if needed
   ```

### Native Installation Issues

1. **Dependency errors**:
   ```bash
   # Ensure all Go dependencies are installed
   go mod tidy
   go mod download
   ```

2. **Permission issues**:
   ```bash
   # Check directory permissions
   sudo chown -R $USER:$USER ./recordings ./sessions
   chmod -R 755 ./recordings ./sessions
   ```

3. **Network issues**:
   ```bash
   # Check firewall settings
   sudo ufw status
   sudo ufw allow 5060/tcp
   sudo ufw allow 5060/udp
   sudo ufw allow 10000:20000/udp
   sudo ufw allow 8080/tcp
   ```

### Configuration Issues

1. **Environment loading issues**:
   ```bash
   # Run environment test
   make env-test
   
   # Run environment validation
   go run ./cmd/testenv/main.go --validate
   ```

2. **IP detection issues**:
   ```bash
   # Manually set IP addresses in .env file
   EXTERNAL_IP=<your-public-ip>
   INTERNAL_IP=<your-local-ip>
   ```

## Advanced Configuration

### Securing the Server

For production environments, consider these security enhancements:

#### 1. Enable TLS for SIP

Create self-signed certificates:

```bash
mkdir -p certs
openssl req -x509 -newkey rsa:4096 -keyout certs/key.pem -out certs/cert.pem -days 365 -nodes
```

Configure in `.env`:

```properties
USE_TLS=true
CERT_FILE=./certs/cert.pem
KEY_FILE=./certs/key.pem
```

#### 2. Configure Firewall

```bash
# Allow only necessary ports
sudo ufw allow 5060/tcp
sudo ufw allow 5060/udp
sudo ufw allow 5061/tcp
sudo ufw allow 5061/udp
sudo ufw allow 10000:20000/udp
sudo ufw allow 8080/tcp
sudo ufw enable
```

### Performance Tuning

For high-traffic deployments, consider these optimizations:

#### 1. System Limits

```bash
# Add to /etc/security/limits.conf
*               soft    nofile          65535
*               hard    nofile          65535

# Add to /etc/sysctl.conf
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 87380 16777216
```

#### 2. Go Runtime Configuration

```properties
# Add to .env
GOMAXPROCS=0               # Use all available cores (default)
GOGC=100                   # Adjust garbage collection (lower = more frequent)
```

### Configuring for High Availability

For redundant server setups:

```properties
# On primary server
SERVER_ROLE=primary
CLUSTER_ID=siprec-cluster-1

# On backup server
SERVER_ROLE=backup
CLUSTER_ID=siprec-cluster-1
PRIMARY_SERVER=<primary-server-ip>
```

For detailed information on session redundancy, refer to the [SESSION_REDUNDANCY.md](./docs/SESSION_REDUNDANCY.md) documentation.

## Next Steps

- See [TESTING.md](./TESTING.md) for comprehensive testing documentation
- Review [RFC_COMPLIANCE.md](./docs/RFC_COMPLIANCE.md) for details on RFC compliance
- Explore advanced redundancy features in [SESSION_REDUNDANCY.md](./docs/SESSION_REDUNDANCY.md)