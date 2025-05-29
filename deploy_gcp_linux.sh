#!/bin/bash

# SIPREC Server Deployment Script for Google Cloud Platform Linux
# 
# This script provides automated deployment of the SIPREC server on GCP Linux instances.
# Supports Ubuntu 20.04/22.04, Debian 11/12, CentOS 8/9, RHEL 8/9
# 
# Features:
# - Automatic NAT detection and configuration
# - Complete service setup with systemd integration
# - Security hardening and firewall configuration
# - Production-ready monitoring and logging
#
# Usage: sudo ./deploy_gcp_linux.sh
# 
# Author: SIPREC Server Project
# License: MIT

set -e

# Configuration
SIPREC_USER="siprec"
SIPREC_HOME="/opt/siprec"
SERVICE_NAME="siprec-server"
LOG_DIR="/var/log/siprec"
DATA_DIR="/var/lib/siprec"
CONFIG_DIR="/etc/siprec"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging function
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')] $1${NC}"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING: $1${NC}"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR: $1${NC}"
    exit 1
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        error "This script must be run as root. Use: sudo $0"
    fi
}

# Detect Linux distribution
detect_distro() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        DISTRO=$ID
        VERSION=$VERSION_ID
    else
        error "Cannot detect Linux distribution"
    fi
    
    log "Detected distribution: $DISTRO $VERSION"
}

# Install dependencies based on distribution
install_dependencies() {
    log "Installing system dependencies..."
    
    case $DISTRO in
        "ubuntu"|"debian")
            apt-get update
            apt-get install -y \
                curl \
                wget \
                unzip \
                git \
                build-essential \
                net-tools \
                iptables \
                ufw \
                logrotate \
                supervisor \
                nginx \
                certbot \
                python3-certbot-nginx
            ;;
        "centos"|"rhel"|"rocky"|"almalinux")
            yum update -y
            yum install -y \
                curl \
                wget \
                unzip \
                git \
                gcc \
                net-tools \
                iptables \
                firewalld \
                logrotate \
                supervisor \
                nginx \
                certbot \
                python3-certbot-nginx
            ;;
        *)
            error "Unsupported distribution: $DISTRO"
            ;;
    esac
}

# Install Go if not present
install_go() {
    if command -v go &> /dev/null; then
        GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
        log "Go is already installed: $GO_VERSION"
        return
    fi
    
    log "Installing Go..."
    
    # Download and install Go
    GO_VERSION="1.21.5"
    ARCH=$(uname -m)
    case $ARCH in
        "x86_64") GO_ARCH="amd64" ;;
        "aarch64") GO_ARCH="arm64" ;;
        *) error "Unsupported architecture: $ARCH" ;;
    esac
    
    cd /tmp
    wget -q "https://golang.org/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    
    # Add Go to PATH
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    export PATH=$PATH:/usr/local/go/bin
    
    log "Go ${GO_VERSION} installed successfully"
}

# Create SIPREC user and directories
setup_user_and_directories() {
    log "Setting up SIPREC user and directories..."
    
    # Create siprec user
    if ! id "$SIPREC_USER" &>/dev/null; then
        useradd --system --home-dir "$SIPREC_HOME" --shell /bin/bash "$SIPREC_USER"
        log "Created user: $SIPREC_USER"
    else
        log "User $SIPREC_USER already exists"
    fi
    
    # Create directories
    mkdir -p "$SIPREC_HOME"/{bin,config,logs,recordings,sessions,keys}
    mkdir -p "$LOG_DIR"
    mkdir -p "$DATA_DIR"/{recordings,sessions,keys}
    mkdir -p "$CONFIG_DIR"
    
    # Set ownership
    chown -R "$SIPREC_USER:$SIPREC_USER" "$SIPREC_HOME"
    chown -R "$SIPREC_USER:$SIPREC_USER" "$LOG_DIR"
    chown -R "$SIPREC_USER:$SIPREC_USER" "$DATA_DIR"
    chown -R "$SIPREC_USER:$SIPREC_USER" "$CONFIG_DIR"
    
    # Set permissions
    chmod 755 "$SIPREC_HOME"
    chmod 750 "$SIPREC_HOME"/{config,keys}
    chmod 755 "$SIPREC_HOME"/{bin,logs,recordings,sessions}
    chmod 750 "$DATA_DIR/keys"
    
    log "Directories created and configured"
}

# Build and install SIPREC server
build_and_install() {
    log "Building and installing SIPREC server..."
    
    # Create temporary build directory
    BUILD_DIR="/tmp/siprec-build"
    rm -rf "$BUILD_DIR"
    mkdir -p "$BUILD_DIR"
    
    # Copy source code (assumes script is run from siprec directory)
    if [ ! -f "./go.mod" ]; then
        error "go.mod not found. Please run this script from the SIPREC source directory"
    fi
    
    cp -r . "$BUILD_DIR/"
    cd "$BUILD_DIR"
    
    # Set Go environment
    export PATH=$PATH:/usr/local/go/bin
    export GOPROXY=https://proxy.golang.org,direct
    
    # Build the application
    log "Building SIPREC server binary..."
    go mod download
    go build -ldflags "-X main.version=$(cat VERSION 2>/dev/null || echo 'dev')" -o siprec ./cmd/siprec
    
    # Install binary
    cp siprec "$SIPREC_HOME/bin/"
    chmod +x "$SIPREC_HOME/bin/siprec"
    ln -sf "$SIPREC_HOME/bin/siprec" /usr/local/bin/siprec
    
    # Copy configuration files
    if [ -f ".env.example" ]; then
        cp .env.example "$CONFIG_DIR/.env"
    fi
    
    # Set ownership
    chown -R "$SIPREC_USER:$SIPREC_USER" "$SIPREC_HOME"
    chown "$SIPREC_USER:$SIPREC_USER" "$CONFIG_DIR/.env"
    
    # Cleanup
    cd /
    rm -rf "$BUILD_DIR"
    
    log "SIPREC server installed successfully"
}

# Validate NAT configuration
validate_nat_config() {
    log "Validating NAT configuration..."
    
    # Get GCP instance IPs
    GCP_EXTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/external-ip 2>/dev/null)
    GCP_INTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip 2>/dev/null)
    
    if [ -n "$GCP_EXTERNAL_IP" ] && [ -n "$GCP_INTERNAL_IP" ]; then
        log "GCP Instance detected:"
        log "  External IP: $GCP_EXTERNAL_IP"
        log "  Internal IP: $GCP_INTERNAL_IP"
        
        # Check if IPs are different (indicating NAT)
        if [ "$GCP_EXTERNAL_IP" != "$GCP_INTERNAL_IP" ]; then
            log "NAT configuration detected - will configure for NAT traversal"
            USE_NAT=true
        else
            log "No NAT detected - direct IP configuration"
            USE_NAT=false
        fi
    else
        warn "Could not detect GCP metadata - falling back to external IP detection"
        USE_NAT=true
    fi
}

# Configure environment
configure_environment() {
    log "Configuring environment..."
    
    # Validate NAT first
    validate_nat_config
    
    # Create environment configuration
    cat > "$CONFIG_DIR/.env" << EOF
# SIPREC Server Configuration for GCP
SIPREC_CONFIG_FILE=$CONFIG_DIR/config.yaml

# Network Configuration - GCP NAT Optimized
BEHIND_NAT=${USE_NAT:-true}
EXTERNAL_IP=${GCP_EXTERNAL_IP:-$(curl -s https://api.ipify.org || curl -s http://checkip.amazonaws.com || echo "auto")}
INTERNAL_IP=${GCP_INTERNAL_IP:-$(hostname -I | awk '{print $1}')}
PORTS=5060,5061
ENABLE_TLS=false
TLS_PORT=5062
RTP_PORT_MIN=16384
RTP_PORT_MAX=32768

# STUN Configuration for NAT Traversal
STUN_SERVER=stun:stun.l.google.com:19302,stun:stun1.l.google.com:19302,stun:stun2.l.google.com:19302

# HTTP Configuration
HTTP_ENABLED=true
HTTP_PORT=8080
HTTP_ENABLE_METRICS=true
HTTP_ENABLE_API=true

# Recording Configuration
RECORDING_DIR=$DATA_DIR/recordings
RECORDING_MAX_DURATION=4h
RECORDING_CLEANUP_DAYS=30

# STT Configuration
STT_DEFAULT_VENDOR=mock
STT_SUPPORTED_VENDORS=mock,google

# Security
ENABLE_RECORDING_ENCRYPTION=false
ENABLE_METADATA_ENCRYPTION=false
ENCRYPTION_KEY_STORE=file
MASTER_KEY_PATH=$DATA_DIR/keys/master.key

# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Resource Limits
MAX_CONCURRENT_CALLS=100

# GCP Specific
GCP_PROJECT_ID=
GOOGLE_APPLICATION_CREDENTIALS=
EOF

    # Set ownership and permissions
    chown "$SIPREC_USER:$SIPREC_USER" "$CONFIG_DIR/.env"
    chmod 640 "$CONFIG_DIR/.env"
    
    log "Environment configuration created"
}

# Create systemd service
create_systemd_service() {
    log "Creating systemd service..."
    
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=SIPREC Server - SIP Recording Server
Documentation=https://github.com/yourusername/siprec
After=network.target
Wants=network.target

[Service]
Type=simple
User=$SIPREC_USER
Group=$SIPREC_USER
WorkingDirectory=$SIPREC_HOME
ExecStart=$SIPREC_HOME/bin/siprec
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=5
StartLimitInterval=0

# Environment
Environment=SIPREC_CONFIG_FILE=$CONFIG_DIR/.env
EnvironmentFile=-$CONFIG_DIR/.env

# Security settings
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$SIPREC_HOME $LOG_DIR $DATA_DIR $CONFIG_DIR
CapabilityBoundingSet=CAP_NET_BIND_SERVICE

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=siprec-server

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd and enable service
    systemctl daemon-reload
    systemctl enable "${SERVICE_NAME}.service"
    
    log "Systemd service created and enabled"
}

# Configure firewall
configure_firewall() {
    log "Configuring firewall..."
    
    case $DISTRO in
        "ubuntu"|"debian")
            # Configure UFW
            ufw --force reset
            ufw default deny incoming
            ufw default allow outgoing
            
            # Allow SSH
            ufw allow ssh
            
            # Allow SIP ports
            ufw allow 5060/udp comment 'SIP UDP'
            ufw allow 5060/tcp comment 'SIP TCP'
            ufw allow 5061/udp comment 'SIP UDP Alt'
            ufw allow 5061/tcp comment 'SIP TCP Alt'
            ufw allow 5062/tcp comment 'SIP TLS'
            
            # Allow RTP ports
            ufw allow 16384:32768/udp comment 'RTP Media'
            
            # Allow HTTP/HTTPS
            ufw allow 80/tcp comment 'HTTP'
            ufw allow 443/tcp comment 'HTTPS'
            ufw allow 8080/tcp comment 'SIPREC API'
            
            # Enable firewall
            ufw --force enable
            ;;
            
        "centos"|"rhel"|"rocky"|"almalinux")
            # Configure firewalld
            systemctl enable firewalld
            systemctl start firewalld
            
            # Add services
            firewall-cmd --permanent --add-service=ssh
            firewall-cmd --permanent --add-service=http
            firewall-cmd --permanent --add-service=https
            
            # Add SIP ports
            firewall-cmd --permanent --add-port=5060/udp
            firewall-cmd --permanent --add-port=5060/tcp
            firewall-cmd --permanent --add-port=5061/udp
            firewall-cmd --permanent --add-port=5061/tcp
            firewall-cmd --permanent --add-port=5062/tcp
            
            # Add RTP port range
            firewall-cmd --permanent --add-port=16384-32768/udp
            
            # Add API port
            firewall-cmd --permanent --add-port=8080/tcp
            
            # Reload firewall
            firewall-cmd --reload
            ;;
    esac
    
    log "Firewall configured"
}

# Configure nginx reverse proxy
configure_nginx() {
    log "Configuring Nginx reverse proxy..."
    
    # Create nginx configuration
    cat > "/etc/nginx/sites-available/siprec" << EOF
server {
    listen 80;
    server_name _;
    
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        
        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
    }
    
    location /health {
        proxy_pass http://127.0.0.1:8080/health;
        access_log off;
    }
    
    location /metrics {
        proxy_pass http://127.0.0.1:8080/metrics;
        allow 127.0.0.1;
        deny all;
    }
}
EOF

    # Enable site (Ubuntu/Debian)
    if [ -d "/etc/nginx/sites-enabled" ]; then
        ln -sf /etc/nginx/sites-available/siprec /etc/nginx/sites-enabled/
        rm -f /etc/nginx/sites-enabled/default
    fi
    
    # Test nginx configuration
    nginx -t
    
    # Enable and start nginx
    systemctl enable nginx
    systemctl restart nginx
    
    log "Nginx configured and started"
}

# Configure log rotation
configure_logrotate() {
    log "Configuring log rotation..."
    
    cat > "/etc/logrotate.d/siprec" << EOF
$LOG_DIR/*.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    postrotate
        systemctl reload ${SERVICE_NAME} > /dev/null 2>&1 || true
    endscript
    su $SIPREC_USER $SIPREC_USER
}

$DATA_DIR/recordings/*.log {
    weekly
    rotate 12
    compress
    delaycompress
    missingok
    notifempty
    su $SIPREC_USER $SIPREC_USER
}
EOF

    log "Log rotation configured"
}

# Create monitoring scripts
create_monitoring() {
    log "Creating monitoring scripts..."
    
    # Health check script
    cat > "$SIPREC_HOME/bin/health-check.sh" << 'EOF'
#!/bin/bash

# SIPREC Health Check Script
SERVICE_NAME="siprec-server"
API_URL="http://127.0.0.1:8080/health"
LOG_FILE="/var/log/siprec/health-check.log"

# Check if service is running
if ! systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "$(date): ERROR: $SERVICE_NAME is not running" >> "$LOG_FILE"
    exit 1
fi

# Check if API is responding
if ! curl -f -s "$API_URL" > /dev/null; then
    echo "$(date): ERROR: API health check failed" >> "$LOG_FILE"
    exit 1
fi

echo "$(date): OK: Service is healthy" >> "$LOG_FILE"
exit 0
EOF

    # Backup script
    cat > "$SIPREC_HOME/bin/backup.sh" << EOF
#!/bin/bash

# SIPREC Backup Script
BACKUP_DIR="/opt/backups/siprec"
DATE=\$(date +%Y%m%d_%H%M%S)

mkdir -p "\$BACKUP_DIR"

# Backup configuration
tar -czf "\$BACKUP_DIR/config_\$DATE.tar.gz" -C / "$CONFIG_DIR"

# Backup keys
if [ -d "$DATA_DIR/keys" ]; then
    tar -czf "\$BACKUP_DIR/keys_\$DATE.tar.gz" -C / "$DATA_DIR/keys"
fi

# Remove old backups (keep 7 days)
find "\$BACKUP_DIR" -name "*.tar.gz" -mtime +7 -delete

echo "\$(date): Backup completed: \$DATE"
EOF

    # Make scripts executable
    chmod +x "$SIPREC_HOME/bin/health-check.sh"
    chmod +x "$SIPREC_HOME/bin/backup.sh"
    chown "$SIPREC_USER:$SIPREC_USER" "$SIPREC_HOME/bin/"*.sh
    
    # Add health check to cron
    cat > "/etc/cron.d/siprec-health" << EOF
# SIPREC Health Check
*/5 * * * * $SIPREC_USER $SIPREC_HOME/bin/health-check.sh
EOF

    # Add backup to cron
    cat > "/etc/cron.d/siprec-backup" << EOF
# SIPREC Daily Backup
0 2 * * * $SIPREC_USER $SIPREC_HOME/bin/backup.sh
EOF

    log "Monitoring scripts created"
}

# Start services
start_services() {
    log "Starting SIPREC server..."
    
    # Start and enable services
    systemctl start "${SERVICE_NAME}.service"
    systemctl status "${SERVICE_NAME}.service" --no-pager
    
    # Wait for service to start
    sleep 5
    
    # Check if service is running
    if systemctl is-active --quiet "${SERVICE_NAME}.service"; then
        log "SIPREC server started successfully"
    else
        error "Failed to start SIPREC server"
    fi
}

# Display deployment summary
show_summary() {
    EXTERNAL_IP=$(curl -s https://api.ipify.org 2>/dev/null || echo "unknown")
    
    log "Deployment completed successfully!"
    echo
    echo "========================================"
    echo "SIPREC Server Deployment Summary"
    echo "========================================"
    echo "Service Name: ${SERVICE_NAME}"
    echo "Install Directory: ${SIPREC_HOME}"
    echo "Configuration: ${CONFIG_DIR}/.env"
    echo "Logs: ${LOG_DIR}"
    echo "Data: ${DATA_DIR}"
    echo
    echo "Network Configuration:"
    echo "  External IP: ${EXTERNAL_IP}"
    echo "  SIP Ports: 5060/udp, 5060/tcp, 5061/udp, 5061/tcp"
    echo "  RTP Ports: 16384-32768/udp"
    echo "  HTTP API: http://${EXTERNAL_IP}:8080"
    echo "  Web UI: http://${EXTERNAL_IP}"
    echo
    echo "Management Commands:"
    echo "  Start:   systemctl start ${SERVICE_NAME}"
    echo "  Stop:    systemctl stop ${SERVICE_NAME}"
    echo "  Status:  systemctl status ${SERVICE_NAME}"
    echo "  Logs:    journalctl -u ${SERVICE_NAME} -f"
    echo "  Config:  nano ${CONFIG_DIR}/.env"
    echo
    echo "Health Check: curl http://127.0.0.1:8080/health"
    echo "Metrics: curl http://127.0.0.1:8080/metrics"
    echo
    echo "Next Steps:"
    echo "1. Configure your STT provider credentials in ${CONFIG_DIR}/.env"
    echo "2. Update firewall rules if needed"
    echo "3. Set up SSL certificates with: certbot --nginx"
    echo "4. Configure monitoring and alerting"
    echo "========================================"
}

# Main deployment function
main() {
    echo "========================================"
    echo "SIPREC Server Deployment for GCP Linux"
    echo "========================================"
    
    check_root
    detect_distro
    install_dependencies
    install_go
    setup_user_and_directories
    build_and_install
    configure_environment
    create_systemd_service
    configure_firewall
    configure_nginx
    configure_logrotate
    create_monitoring
    start_services
    show_summary
}

# Run deployment
main "$@"