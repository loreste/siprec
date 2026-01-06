#!/bin/bash
# SIPREC Server Installation Script
# Run this script on the target server as root

set -e

# Configuration
INSTALL_DIR="/opt/siprec-server"
GO_VERSION="1.24.2"
SERVICE_USER="siprec"
SERVICE_GROUP="siprec"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    log_error "Please run this script as root or with sudo"
    exit 1
fi

# Detect OS
if [ -f /etc/os-release ]; then
    . /etc/os-release
    OS=$ID
    OS_VERSION=$VERSION_ID
else
    log_error "Cannot detect OS. This script supports Debian, Ubuntu, CentOS, Rocky, and Alma Linux."
    exit 1
fi

log_info "Detected OS: $OS $OS_VERSION"

# Install dependencies based on OS
install_dependencies() {
    log_info "Installing system dependencies..."

    case $OS in
        ubuntu|debian)
            apt-get update
            apt-get install -y curl wget git make gcc
            ;;
        centos|rocky|almalinux|rhel)
            yum install -y curl wget git make gcc
            ;;
        *)
            log_warn "Unknown OS: $OS. Attempting to continue..."
            ;;
    esac
}

# Install Go
install_go() {
    if command -v go &> /dev/null; then
        CURRENT_GO_VERSION=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+')
        log_info "Go is already installed: version $CURRENT_GO_VERSION"

        # Check if version is >= 1.24
        if [[ "$(printf '%s\n' "1.24" "$CURRENT_GO_VERSION" | sort -V | head -n1)" == "1.24" ]]; then
            log_info "Go version is sufficient"
            return 0
        else
            log_warn "Go version is too old, upgrading..."
        fi
    fi

    log_info "Installing Go $GO_VERSION..."

    # Detect architecture
    ARCH=$(uname -m)
    case $ARCH in
        x86_64)
            GO_ARCH="amd64"
            ;;
        aarch64|arm64)
            GO_ARCH="arm64"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    # Download and install Go
    GO_TAR="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TAR}"

    log_info "Downloading Go from $GO_URL..."
    cd /tmp
    wget -q "$GO_URL" -O "$GO_TAR"

    # Remove old Go installation if exists
    rm -rf /usr/local/go

    # Extract new Go
    tar -C /usr/local -xzf "$GO_TAR"
    rm "$GO_TAR"

    # Add Go to PATH for this session
    export PATH=$PATH:/usr/local/go/bin

    # Add Go to PATH permanently
    if ! grep -q '/usr/local/go/bin' /etc/profile.d/go.sh 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin' > /etc/profile.d/go.sh
        chmod +x /etc/profile.d/go.sh
    fi

    log_info "Go $GO_VERSION installed successfully"
    go version
}

# Create service user
create_service_user() {
    if id "$SERVICE_USER" &>/dev/null; then
        log_info "User $SERVICE_USER already exists"
    else
        log_info "Creating service user $SERVICE_USER..."
        useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
    fi
}

# Setup installation directory
setup_install_dir() {
    log_info "Setting up installation directory at $INSTALL_DIR..."

    mkdir -p "$INSTALL_DIR"
    mkdir -p "$INSTALL_DIR/recordings"
    mkdir -p "$INSTALL_DIR/logs"
    mkdir -p "$INSTALL_DIR/config"
    mkdir -p "$INSTALL_DIR/keys"

    # Set permissions
    chown -R "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DIR"
    chmod 750 "$INSTALL_DIR"
    chmod 750 "$INSTALL_DIR/recordings"
    chmod 750 "$INSTALL_DIR/logs"
    chmod 750 "$INSTALL_DIR/keys"
}

# Build the application (if source is available)
build_application() {
    if [ -f "$INSTALL_DIR/go.mod" ]; then
        log_info "Building SIPREC server from source..."
        cd "$INSTALL_DIR"

        export PATH=$PATH:/usr/local/go/bin
        export GOPATH=/tmp/go
        export GOCACHE=/tmp/go-cache

        go mod download
        CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o siprec-server ./cmd/siprec

        chown "$SERVICE_USER:$SERVICE_GROUP" siprec-server
        chmod 750 siprec-server

        log_info "Build completed successfully"
    elif [ -f "$INSTALL_DIR/siprec-server" ]; then
        log_info "Pre-built binary found, skipping build"
        chown "$SERVICE_USER:$SERVICE_GROUP" "$INSTALL_DIR/siprec-server"
        chmod 750 "$INSTALL_DIR/siprec-server"
    else
        log_error "No source code or binary found in $INSTALL_DIR"
        log_error "Please copy the source code or binary before running this script"
        exit 1
    fi
}

# Create environment file
create_env_file() {
    ENV_FILE="$INSTALL_DIR/.env.production"

    if [ -f "$ENV_FILE" ]; then
        log_warn "Environment file already exists, not overwriting"
        return 0
    fi

    log_info "Creating environment file..."

    cat > "$ENV_FILE" << 'EOF'
# SIPREC Server Configuration
# Network Configuration
SIP_EXTERNAL_IP=
SIP_HOST=0.0.0.0
SIP_PORTS=5060

# RTP Configuration
RTP_PORT_MIN=10000
RTP_PORT_MAX=20000
RTP_BIND_IP=0.0.0.0

# HTTP API Configuration
HTTP_ENABLED=true
HTTP_PORT=8080
HTTP_ENABLE_METRICS=true
HTTP_ENABLE_API=true

# Recording Configuration
RECORDING_DIR=/opt/siprec-server/recordings
RECORDING_COMBINE_LEGS=true

# STT Configuration (uncomment and configure as needed)
# STT_DEFAULT_VENDOR=deepgram
# STT_DEEPGRAM_ENABLED=true
# STT_DEEPGRAM_API_KEY=your-api-key

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Encryption
ENABLE_RECORDING_ENCRYPTION=false
ENCRYPTION_KEY_STORE=memory
EOF

    chown "$SERVICE_USER:$SERVICE_GROUP" "$ENV_FILE"
    chmod 600 "$ENV_FILE"

    log_info "Environment file created at $ENV_FILE"
    log_warn "Please edit $ENV_FILE to configure your deployment"
}

# Install systemd service
install_service() {
    log_info "Installing systemd service..."

    cat > /etc/systemd/system/siprec-server.service << EOF
[Unit]
Description=SIPREC Recording Server
Documentation=https://github.com/loreste/siprec
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=$INSTALL_DIR

# Environment
Environment="PATH=/usr/local/bin:/usr/bin:/bin"
EnvironmentFile=-$INSTALL_DIR/.env.production

# Executable
ExecStart=$INSTALL_DIR/siprec-server

# Restart policy
Restart=always
RestartSec=5
StartLimitInterval=0

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$INSTALL_DIR/recordings $INSTALL_DIR/logs

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

    systemctl daemon-reload
    log_info "Systemd service installed"
}

# Configure firewall
configure_firewall() {
    log_info "Configuring firewall..."

    # Check for firewalld
    if command -v firewall-cmd &> /dev/null && systemctl is-active --quiet firewalld; then
        log_info "Configuring firewalld..."
        firewall-cmd --permanent --add-port=5060/udp  # SIP UDP
        firewall-cmd --permanent --add-port=5060/tcp  # SIP TCP
        firewall-cmd --permanent --add-port=5061/tcp  # SIP TLS
        firewall-cmd --permanent --add-port=8080/tcp  # HTTP API
        firewall-cmd --permanent --add-port=10000-20000/udp  # RTP
        firewall-cmd --reload
        log_info "Firewalld configured"
    # Check for ufw
    elif command -v ufw &> /dev/null && ufw status | grep -q "active"; then
        log_info "Configuring ufw..."
        ufw allow 5060/udp  # SIP UDP
        ufw allow 5060/tcp  # SIP TCP
        ufw allow 5061/tcp  # SIP TLS
        ufw allow 8080/tcp  # HTTP API
        ufw allow 10000:20000/udp  # RTP
        log_info "ufw configured"
    else
        log_warn "No firewall detected or firewall is inactive"
        log_warn "Please manually configure your firewall to allow:"
        log_warn "  - UDP/TCP 5060 (SIP)"
        log_warn "  - TCP 5061 (SIP TLS)"
        log_warn "  - TCP 8080 (HTTP API)"
        log_warn "  - UDP 10000-20000 (RTP)"
    fi
}

# Main installation
main() {
    log_info "Starting SIPREC Server installation..."

    install_dependencies
    install_go
    create_service_user
    setup_install_dir

    # Check if we should build or use pre-built binary
    if [ "$1" == "--binary-only" ]; then
        log_info "Binary-only mode: expecting pre-built binary"
    else
        build_application
    fi

    create_env_file
    install_service
    configure_firewall

    log_info "============================================"
    log_info "SIPREC Server installation completed!"
    log_info "============================================"
    log_info ""
    log_info "Next steps:"
    log_info "1. Edit the configuration file:"
    log_info "   nano $INSTALL_DIR/.env.production"
    log_info ""
    log_info "2. Enable and start the service:"
    log_info "   systemctl enable siprec-server"
    log_info "   systemctl start siprec-server"
    log_info ""
    log_info "3. Check service status:"
    log_info "   systemctl status siprec-server"
    log_info ""
    log_info "4. View logs:"
    log_info "   journalctl -u siprec-server -f"
    log_info ""
}

main "$@"
