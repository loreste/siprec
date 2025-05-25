#!/bin/bash
set -e

echo "=== SIPREC Server Linux Installation Script ==="
echo "Installing SIPREC server without TLS/encryption on Linux..."

# Detect Linux distribution
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$NAME
    VERSION=$VERSION_ID
else
    echo "Cannot detect Linux distribution"
    exit 1
fi

echo "Detected OS: $OS $VERSION"

# Install Go if not present
install_go() {
    echo "Installing Go 1.22.3..."
    cd /tmp
    wget -q https://go.dev/dl/go1.22.3.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go1.22.3.linux-amd64.tar.gz
    
    # Add Go to PATH for current session
    export PATH=$PATH:/usr/local/go/bin
    
    # Add Go to PATH permanently
    if ! grep -q "/usr/local/go/bin" ~/.bashrc; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
    fi
    
    # Add to system-wide PATH
    if [ ! -f /etc/profile.d/go.sh ]; then
        sudo tee /etc/profile.d/go.sh > /dev/null << 'EOF'
export PATH=$PATH:/usr/local/go/bin
EOF
    fi
}

# Install dependencies based on distribution
install_dependencies() {
    if command -v apt-get &> /dev/null; then
        # Debian/Ubuntu
        echo "Installing dependencies via apt..."
        sudo apt-get update
        sudo apt-get install -y wget curl git build-essential
    elif command -v yum &> /dev/null; then
        # RHEL/CentOS/Fedora (older)
        echo "Installing dependencies via yum..."
        sudo yum update -y
        sudo yum install -y wget curl git gcc
    elif command -v dnf &> /dev/null; then
        # Fedora (newer)
        echo "Installing dependencies via dnf..."
        sudo dnf update -y
        sudo dnf install -y wget curl git gcc
    elif command -v zypper &> /dev/null; then
        # openSUSE
        echo "Installing dependencies via zypper..."
        sudo zypper refresh
        sudo zypper install -y wget curl git gcc
    else
        echo "Unsupported package manager. Please install: wget, curl, git, build-essential manually"
        exit 1
    fi
}

# Check and install dependencies
echo "Installing system dependencies..."
install_dependencies

# Check if Go is installed and version is adequate
if ! command -v go &> /dev/null; then
    install_go
else
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo $GO_VERSION | cut -d. -f1)
    GO_MINOR=$(echo $GO_VERSION | cut -d. -f2)
    
    echo "Go already installed: $GO_VERSION"
    
    # Check if Go version is at least 1.21
    if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 21 ]); then
        echo "Go version $GO_VERSION is too old, need at least 1.21"
        install_go
    fi
fi

# Verify Go installation
export PATH=$PATH:/usr/local/go/bin
go version

# Create siprec user
if ! id "siprec" &>/dev/null; then
    echo "Creating siprec user..."
    sudo useradd -r -s /bin/false -d /opt/siprec siprec
fi

# Create installation directory
INSTALL_DIR="/opt/siprec"
echo "Creating installation directory: $INSTALL_DIR"
sudo mkdir -p $INSTALL_DIR
sudo chown siprec:siprec $INSTALL_DIR

# Clone repository to temp directory
echo "Cloning SIPREC repository..."
cd /tmp
rm -rf siprec
git clone https://github.com/loreste/siprec.git
cd siprec

# Use the latest production-ready version
echo "Using latest production-ready version..."

# Create required directories
echo "Creating runtime directories..."
sudo mkdir -p $INSTALL_DIR/recordings
sudo mkdir -p $INSTALL_DIR/sessions
sudo mkdir -p $INSTALL_DIR/logs
sudo mkdir -p /var/log/siprec

# Create configuration file
echo "Creating configuration file..."
sudo tee $INSTALL_DIR/.env > /dev/null << 'EOF'
# SIPREC Server Configuration - Linux Production
APP_ENV=production
LOG_LEVEL=info
LOG_FORMAT=json

# Network configuration
SIP_PORTS=5060
RTP_PORT_MIN=16384
RTP_PORT_MAX=32768
EXTERNAL_IP=auto
INTERNAL_IP=auto

# Media configuration
BEHIND_NAT=false
STUN_SERVERS=stun.l.google.com:19302,stun1.l.google.com:19302
RECORDING_DIR=/opt/siprec/recordings
RECORDING_FORMAT=wav
RECORDING_MAX_DURATION=4h
RECORDING_CLEANUP_DAYS=30

# Audio processing
AUDIO_PROCESSING_ENABLED=true
VAD_ENABLED=true
VAD_THRESHOLD=0.02
NOISE_REDUCTION_ENABLED=true
NOISE_REDUCTION_LEVEL=20
MIX_CHANNELS=true
CHANNEL_COUNT=1

# STT configuration (using mock for initial setup)
STT_VENDORS=mock
STT_DEFAULT_VENDOR=mock
STT_DEFAULT_LANGUAGE=en-US
STT_BUFFER_SIZE=8192
STT_BUFFER_DURATION=5s

# HTTP server configuration
HTTP_ENABLED=true
HTTP_PORT=8080
HTTP_READ_TIMEOUT=10s
HTTP_WRITE_TIMEOUT=30s
HTTP_ENABLE_API=true
HTTP_ENABLE_METRICS=true

# Session configuration
SESSION_CLEANUP_INTERVAL=30s
SESSION_MAX_IDLE_TIME=5m

# Performance tuning
MAX_CONCURRENT_CALLS=1000
WORKER_POOL_SIZE=100

# Encryption configuration (disabled by default)
ENABLE_RECORDING_ENCRYPTION=false
ENABLE_METADATA_ENCRYPTION=false
EOF

# Build the application
echo "Building SIPREC server..."
export CGO_ENABLED=0
export PATH=$PATH:/usr/local/go/bin
go clean -cache
go mod tidy
go build -trimpath -ldflags "-s -w" -o siprec-server ./cmd/siprec

# Install to target directory
echo "Installing binary and configuration..."
sudo cp siprec-server $INSTALL_DIR/
sudo chmod +x $INSTALL_DIR/siprec-server

# Copy production files if they exist
if [ -f .env.production ]; then
    sudo cp .env.production $INSTALL_DIR/
fi
if [ -f validate_production.sh ]; then
    sudo cp validate_production.sh $INSTALL_DIR/
    sudo chmod +x $INSTALL_DIR/validate_production.sh
fi
if [ -f PRODUCTION_DEPLOYMENT.md ]; then
    sudo cp PRODUCTION_DEPLOYMENT.md $INSTALL_DIR/
fi

sudo chown -R siprec:siprec $INSTALL_DIR

# Create systemd service
echo "Creating systemd service..."
sudo tee /etc/systemd/system/siprec.service > /dev/null << 'EOF'
[Unit]
Description=SIPREC Recording Server
Documentation=https://github.com/loreste/siprec
After=network.target

[Service]
Type=simple
User=siprec
Group=siprec
WorkingDirectory=/opt/siprec

# Environment
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
EnvironmentFile=-/opt/siprec/.env

# Executable
ExecStart=/opt/siprec/siprec-server

# Restart policy
Restart=always
RestartSec=5
StartLimitInterval=0

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/siprec/recordings /opt/siprec/logs

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=siprec-server

[Install]
WantedBy=multi-user.target
EOF

# Configure firewall if available
configure_firewall() {
    if command -v ufw &> /dev/null; then
        echo "Configuring UFW firewall..."
        sudo ufw allow 5060/udp comment "SIPREC SIP"
        sudo ufw allow 8080/tcp comment "SIPREC HTTP"
        sudo ufw allow 16384:32768/udp comment "SIPREC RTP"
    elif command -v firewall-cmd &> /dev/null; then
        echo "Configuring firewalld..."
        sudo firewall-cmd --permanent --add-port=5060/udp
        sudo firewall-cmd --permanent --add-port=8080/tcp
        sudo firewall-cmd --permanent --add-port=16384-32768/udp
        sudo firewall-cmd --reload
    else
        echo "No firewall management tool found. Please manually open ports:"
        echo "  - 5060/udp (SIP)"
        echo "  - 8080/tcp (HTTP)"
        echo "  - 16384-32768/udp (RTP)"
    fi
}

configure_firewall

# Enable and start service
echo "Starting SIPREC service..."
sudo systemctl daemon-reload
sudo systemctl enable siprec
sudo systemctl start siprec

# Wait a moment for service to start
sleep 3

# Check service status
echo "Checking service status..."
sudo systemctl status siprec --no-pager

echo ""
echo "=== Installation Complete ==="
echo "✓ SIPREC server installed to: $INSTALL_DIR"
echo "✓ Service: siprec.service"
echo "✓ Configuration: $INSTALL_DIR/.env"
echo "✓ Version: Production-ready with sipgo v0.32.1"
echo ""
echo "Service Management:"
echo "  Status:  sudo systemctl status siprec"
echo "  Logs:    sudo journalctl -u siprec -f"
echo "  Restart: sudo systemctl restart siprec"
echo "  Stop:    sudo systemctl stop siprec"
echo ""
echo "Endpoints:"
echo "  SIP:     udp://0.0.0.0:5060 (TCP also available)"
echo "  HTTP:    http://localhost:8080"
echo "  Health:  http://localhost:8080/health"
echo "  Metrics: http://localhost:8080/metrics"
echo ""
echo "Configuration:"
echo "  Main config:        $INSTALL_DIR/.env"
echo "  Production sample:  $INSTALL_DIR/.env.production"
echo "  Validation script:  $INSTALL_DIR/validate_production.sh"
echo ""
echo "Recordings stored in: /opt/siprec/recordings"
echo ""
echo "To enable Google STT:"
echo "  1. Set GOOGLE_APPLICATION_CREDENTIALS environment variable"
echo "  2. Change STT_VENDORS=google in $INSTALL_DIR/.env"
echo "  3. Restart service: sudo systemctl restart siprec"
echo ""

# Final health check
echo "Performing health check..."
sleep 2
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "✓ Health check passed - SIPREC server is running!"
else
    echo "⚠ Health check failed - check logs: sudo journalctl -u siprec -f"
fi