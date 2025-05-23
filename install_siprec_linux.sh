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
    echo "Installing Go 1.21.9..."
    cd /tmp
    wget -q https://go.dev/dl/go1.21.9.linux-amd64.tar.gz
    sudo rm -rf /usr/local/go
    sudo tar -C /usr/local -xzf go1.21.9.linux-amd64.tar.gz
    
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

# Check if Go is installed
if ! command -v go &> /dev/null; then
    install_go
else
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    echo "Go already installed: $GO_VERSION"
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

# Checkout commit before encryption was added
echo "Checking out stable version without encryption..."
git checkout 987d066

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
SIP_LISTEN_ADDR=0.0.0.0:5060
SIP_TLS_ENABLED=false

# Network Configuration
RTP_PORT_MIN=10000
RTP_PORT_MAX=20000
ENABLE_SRTP=false
BEHIND_NAT=false
INTERNAL_IP=
EXTERNAL_IP=

# Recording Configuration
RECORDING_DIRECTORY=/opt/siprec/recordings

# STT Configuration (Mock for testing)
STT_DEFAULT_VENDOR=mock
STT_SUPPORTED_VENDORS=mock

# HTTP Server
HTTP_LISTEN_ADDR=0.0.0.0:8080
HTTP_TLS_ENABLED=false

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Disable all encryption/TLS features
ENCRYPTION_ENABLE_RECORDING=false
ENCRYPTION_ENABLE_METADATA=false
EOF

# Build the application
echo "Building SIPREC server..."
export CGO_ENABLED=0
export PATH=$PATH:/usr/local/go/bin
go clean -cache
go mod tidy
go build -trimpath -ldflags "-s -w" -o siprec ./cmd/siprec

# Install to target directory
echo "Installing binary and configuration..."
sudo cp siprec $INSTALL_DIR/
sudo chmod +x $INSTALL_DIR/siprec
sudo chown -R siprec:siprec $INSTALL_DIR

# Create systemd service
echo "Creating systemd service..."
sudo tee /etc/systemd/system/siprec.service > /dev/null << 'EOF'
[Unit]
Description=SIPREC Session Recording Server
Documentation=https://github.com/loreste/siprec
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=siprec
Group=siprec
WorkingDirectory=/opt/siprec
ExecStart=/opt/siprec/siprec
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=siprec

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/siprec
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF

# Configure firewall if available
configure_firewall() {
    if command -v ufw &> /dev/null; then
        echo "Configuring UFW firewall..."
        sudo ufw allow 5060/udp comment "SIPREC SIP"
        sudo ufw allow 8080/tcp comment "SIPREC HTTP"
        sudo ufw allow 10000:20000/udp comment "SIPREC RTP"
    elif command -v firewall-cmd &> /dev/null; then
        echo "Configuring firewalld..."
        sudo firewall-cmd --permanent --add-port=5060/udp
        sudo firewall-cmd --permanent --add-port=8080/tcp
        sudo firewall-cmd --permanent --add-port=10000-20000/udp
        sudo firewall-cmd --reload
    else
        echo "No firewall management tool found. Please manually open ports:"
        echo "  - 5060/udp (SIP)"
        echo "  - 8080/tcp (HTTP)"
        echo "  - 10000-20000/udp (RTP)"
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
echo ""
echo "Service Management:"
echo "  Status:  sudo systemctl status siprec"
echo "  Logs:    sudo journalctl -u siprec -f"
echo "  Restart: sudo systemctl restart siprec"
echo "  Stop:    sudo systemctl stop siprec"
echo ""
echo "Endpoints:"
echo "  SIP:     udp://0.0.0.0:5060"
echo "  HTTP:    http://localhost:8080"
echo "  Health:  curl http://localhost:8080/health"
echo ""
echo "Recordings stored in: /opt/siprec/recordings"
echo ""

# Final health check
echo "Performing health check..."
sleep 2
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "✓ Health check passed - SIPREC server is running!"
else
    echo "⚠ Health check failed - check logs: sudo journalctl -u siprec -f"
fi