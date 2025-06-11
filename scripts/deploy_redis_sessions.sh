#!/bin/bash

# Redis Session Store Deployment Script
# This script deploys the Redis session store implementation to the SIPREC server

set -e

# Configuration
SERVER_HOST="${1:-35.222.226.67}"
SERVER_USER="${2:-root}"
PROJECT_DIR="/opt/siprec"
SERVICE_NAME="siprec-server"

echo "🚀 Deploying Redis session store to $SERVER_HOST"

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

# Check if server is accessible
log_info "Checking server connectivity..."
if ! ssh -o ConnectTimeout=10 "$SERVER_USER@$SERVER_HOST" "echo 'Connected successfully'"; then
    log_error "Cannot connect to server $SERVER_HOST"
    exit 1
fi

# Create deployment package
log_info "Creating deployment package..."
TEMP_DIR=$(mktemp -d)
PACKAGE_DIR="$TEMP_DIR/siprec-redis-sessions"
mkdir -p "$PACKAGE_DIR"

# Copy session management files
mkdir -p "$PACKAGE_DIR/pkg/session"
cp pkg/session/redis_store.go "$PACKAGE_DIR/pkg/session/"
cp pkg/session/manager.go "$PACKAGE_DIR/pkg/session/"
cp pkg/session/integration.go "$PACKAGE_DIR/pkg/session/"

# Copy go.mod and go.sum for dependencies
cp go.mod "$PACKAGE_DIR/"
cp go.sum "$PACKAGE_DIR/"

# Create deployment script for the server
cat > "$PACKAGE_DIR/deploy.sh" << 'EOF'
#!/bin/bash

set -e

echo "🔧 Installing Redis session store..."

# Stop the service
systemctl stop siprec-server || true

# Backup existing files
BACKUP_DIR="/opt/siprec/backup/$(date +%Y%m%d_%H%M%S)"
mkdir -p "$BACKUP_DIR"

if [ -f "/opt/siprec/pkg/session/manager.go" ]; then
    cp -r /opt/siprec/pkg/session "$BACKUP_DIR/" 2>/dev/null || true
fi

# Copy new session files
mkdir -p /opt/siprec/pkg/session
cp pkg/session/*.go /opt/siprec/pkg/session/

# Update go.mod if needed
cd /opt/siprec
if ! grep -q "github.com/redis/go-redis/v9" go.mod; then
    echo "Adding Redis dependency..."
    go mod tidy
fi

# Build the application
echo "Building SIPREC server with Redis session store..."
make build-linux

if [ $? -eq 0 ]; then
    echo "✅ Build successful"
else
    echo "❌ Build failed"
    exit 1
fi

# Check if Redis is available (install if needed)
if ! command -v redis-cli &> /dev/null; then
    echo "Installing Redis..."
    apt-get update
    apt-get install -y redis-server
    
    # Configure Redis for production
    sed -i 's/# maxmemory <bytes>/maxmemory 512mb/' /etc/redis/redis.conf
    sed -i 's/# maxmemory-policy noeviction/maxmemory-policy allkeys-lru/' /etc/redis/redis.conf
    
    systemctl enable redis-server
    systemctl start redis-server
else
    echo "✅ Redis already installed"
fi

# Test Redis connection
if redis-cli ping | grep -q PONG; then
    echo "✅ Redis is running"
else
    echo "❌ Redis connection failed"
    systemctl start redis-server
    sleep 2
    if ! redis-cli ping | grep -q PONG; then
        echo "❌ Could not start Redis"
        exit 1
    fi
fi

# Create Redis configuration for SIPREC
cat > /opt/siprec/redis-session.env << 'EOL'
# Redis Session Store Configuration
REDIS_ENABLED=true
REDIS_ADDRESS=localhost:6379
REDIS_PASSWORD=
REDIS_DATABASE=0
REDIS_POOL_SIZE=10
REDIS_SESSION_TTL=24h
NODE_ID=siprec-node-1
ENABLE_FAILOVER=true
ENABLE_BACKUP=false
SESSION_TIMEOUT=1h
EOL

echo "✅ Redis session store installed successfully"
echo "📝 Configuration file created at /opt/siprec/redis-session.env"
echo "🔄 Please restart the SIPREC service to enable Redis sessions"
EOF

chmod +x "$PACKAGE_DIR/deploy.sh"

# Create the package archive
cd "$TEMP_DIR"
tar -czf siprec-redis-sessions.tar.gz siprec-redis-sessions/

log_info "Uploading deployment package to server..."
scp siprec-redis-sessions.tar.gz "$SERVER_USER@$SERVER_HOST:/tmp/"

# Execute deployment on server
log_info "Executing deployment on server..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
cd /tmp
tar -xzf siprec-redis-sessions.tar.gz
cd siprec-redis-sessions
chmod +x deploy.sh
./deploy.sh
EOF

if [ $? -eq 0 ]; then
    log_info "✅ Redis session store deployment completed successfully"
    
    # Provide instructions
    echo ""
    echo "📋 Next Steps:"
    echo "1. Source the Redis configuration:"
    echo "   ssh $SERVER_USER@$SERVER_HOST 'cd /opt/siprec && source redis-session.env'"
    echo ""
    echo "2. Restart the SIPREC service:"
    echo "   ssh $SERVER_USER@$SERVER_HOST 'systemctl restart siprec-server'"
    echo ""
    echo "3. Check service status:"
    echo "   ssh $SERVER_USER@$SERVER_HOST 'systemctl status siprec-server'"
    echo ""
    echo "4. Monitor Redis sessions:"
    echo "   ssh $SERVER_USER@$SERVER_HOST 'redis-cli keys \"siprec:session:*\"'"
    echo ""
    echo "🔧 Redis Management Commands:"
    echo "   - View all sessions: redis-cli HGETALL siprec:index:sessions"
    echo "   - Monitor Redis: redis-cli MONITOR"
    echo "   - Redis info: redis-cli INFO memory"
else
    log_error "❌ Deployment failed"
    exit 1
fi

# Cleanup
rm -rf "$TEMP_DIR"

log_info "🎉 Deployment completed successfully!"