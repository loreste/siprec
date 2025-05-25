#!/bin/bash

# Production readiness validation script for SIPREC Server

set -e

echo "=== SIPREC Server Production Readiness Check ==="
echo ""

# Check if running as root (not recommended)
if [ "$EUID" -eq 0 ]; then 
   echo "⚠️  WARNING: Running as root is not recommended for production"
fi

# Check Go version
echo "1. Checking Go version..."
GO_VERSION=$(go version | awk '{print $3}')
echo "   ✓ Go version: $GO_VERSION"

# Check required directories
echo ""
echo "2. Checking required directories..."
DIRS=("./recordings" "./keys" "./logs")
for dir in "${DIRS[@]}"; do
    if [ ! -d "$dir" ]; then
        echo "   ⚠️  Directory missing: $dir (will be created on startup)"
    else
        echo "   ✓ Directory exists: $dir"
    fi
done

# Check environment configuration
echo ""
echo "3. Checking environment configuration..."
if [ -f ".env.production" ]; then
    echo "   ✓ Production config found: .env.production"
else
    echo "   ✗ Production config missing: .env.production"
    exit 1
fi

# Validate configuration
echo ""
echo "4. Validating configuration..."
cp .env .env.backup 2>/dev/null || true
cp .env.production .env

# Check if Google credentials are needed
if grep -q "STT_VENDORS=google" .env.production; then
    if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
        echo "   ⚠️  WARNING: Google STT enabled but GOOGLE_APPLICATION_CREDENTIALS not set"
    else
        echo "   ✓ Google credentials configured"
    fi
fi

# Check network ports
echo ""
echo "5. Checking network ports..."
PORTS=(5060 5061 8080)
for port in "${PORTS[@]}"; do
    if lsof -i :$port >/dev/null 2>&1; then
        echo "   ✗ Port $port is already in use"
        PORTS_IN_USE=true
    else
        echo "   ✓ Port $port is available"
    fi
done

# Check system resources
echo ""
echo "6. Checking system resources..."
MEM_TOTAL=$(free -m 2>/dev/null | awk '/^Mem:/{print $2}' || sysctl -n hw.memsize 2>/dev/null | awk '{print int($1/1024/1024)}')
CPU_COUNT=$(nproc 2>/dev/null || sysctl -n hw.ncpu)
echo "   ✓ Total memory: ${MEM_TOTAL}MB"
echo "   ✓ CPU cores: $CPU_COUNT"

if [ "$MEM_TOTAL" -lt 2048 ]; then
    echo "   ⚠️  WARNING: Less than 2GB RAM available"
fi

# Check ulimits
echo ""
echo "7. Checking system limits..."
ULIMIT_N=$(ulimit -n)
echo "   ✓ Open files limit: $ULIMIT_N"
if [ "$ULIMIT_N" -lt 4096 ]; then
    echo "   ⚠️  WARNING: Open files limit is low, recommend at least 4096"
    echo "   Run: ulimit -n 4096"
fi

# Build the application
echo ""
echo "8. Building application..."
if go build -o siprec-server cmd/siprec/main.go; then
    echo "   ✓ Build successful"
else
    echo "   ✗ Build failed"
    exit 1
fi

# Run basic tests
echo ""
echo "9. Running basic tests..."
if go test -short ./pkg/config ./pkg/sip ./pkg/media 2>&1 | grep -q "FAIL"; then
    echo "   ✗ Some tests failed"
else
    echo "   ✓ Basic tests passed"
fi

# Test configuration loading
echo ""
echo "10. Testing configuration loading..."
if ./siprec-server -validate-config 2>/dev/null; then
    echo "   ✓ Configuration valid"
else
    # Try running with a simple test
    timeout 5s ./siprec-server >/dev/null 2>&1 &
    PID=$!
    sleep 2
    if kill -0 $PID 2>/dev/null; then
        echo "   ✓ Server starts successfully"
        kill $PID 2>/dev/null
    else
        echo "   ✗ Server failed to start"
    fi
fi

# Summary
echo ""
echo "=== Summary ==="
if [ "$PORTS_IN_USE" = true ]; then
    echo "✗ Some ports are already in use. Stop conflicting services."
fi

echo ""
echo "Production deployment checklist:"
echo "1. [ ] Set up proper logging rotation"
echo "2. [ ] Configure monitoring/alerting"
echo "3. [ ] Set up SSL/TLS certificates if needed"
echo "4. [ ] Configure firewall rules for SIP/RTP ports"
echo "5. [ ] Set up systemd service or container"
echo "6. [ ] Configure backup strategy for recordings"
echo "7. [ ] Set resource limits and quotas"
echo "8. [ ] Enable health checks and metrics"
echo ""

# Restore original env
cp .env.backup .env 2>/dev/null || true

echo "✓ Production readiness check complete!"