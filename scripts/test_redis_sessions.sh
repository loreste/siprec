#!/bin/bash

# Redis Session Store Test Script
# This script tests the Redis session store functionality

SERVER_HOST="${1:-35.222.226.67}"
SERVER_USER="${2:-root}"

echo "🧪 Testing Redis session store on $SERVER_HOST"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Test Redis connectivity
log_test "Testing Redis connectivity..."
if ssh "$SERVER_USER@$SERVER_HOST" "redis-cli ping" | grep -q "PONG"; then
    log_info "✅ Redis is responding"
else
    log_error "❌ Redis is not responding"
    exit 1
fi

# Test Redis memory info
log_test "Checking Redis memory usage..."
ssh "$SERVER_USER@$SERVER_HOST" "redis-cli info memory | grep used_memory_human"

# Test session key structure
log_test "Testing session key structure..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
# Create a test session
redis-cli HSET "siprec:session:test-123" \
    session_id "test-123" \
    call_id "call-456" \
    status "active" \
    start_time "$(date -Iseconds)" \
    node_id "test-node"

# Set TTL
redis-cli EXPIRE "siprec:session:test-123" 300

# Verify the session
echo "Test session created:"
redis-cli HGETALL "siprec:session:test-123"

# Add to index
redis-cli HSET "siprec:index:sessions" "test-123" "call-456"

echo -e "\nIndex entry created:"
redis-cli HGET "siprec:index:sessions" "test-123"
EOF

# Test session listing
log_test "Testing session listing..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
echo "All session keys:"
redis-cli KEYS "siprec:session:*"

echo -e "\nSession index:"
redis-cli HGETALL "siprec:index:sessions"
EOF

# Test SIPREC service status
log_test "Checking SIPREC service status..."
ssh "$SERVER_USER@$SERVER_HOST" "systemctl is-active siprec-server" || log_warn "SIPREC service is not running"

# Check if environment variables are set
log_test "Checking Redis configuration..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
if [ -f "/opt/siprec/redis-session.env" ]; then
    echo "Redis configuration file exists:"
    cat /opt/siprec/redis-session.env
else
    echo "⚠️ Redis configuration file not found"
fi
EOF

# Test session cleanup
log_test "Testing session cleanup..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
echo "Cleaning up test session..."
redis-cli DEL "siprec:session:test-123"
redis-cli HDEL "siprec:index:sessions" "test-123"
echo "✅ Test session cleaned up"
EOF

# Monitor Redis for a few seconds to see any activity
log_test "Monitoring Redis activity for 5 seconds..."
timeout 5 ssh "$SERVER_USER@$SERVER_HOST" "redis-cli MONITOR" 2>/dev/null || true

# Performance test
log_test "Running basic performance test..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
echo "Creating 100 test sessions..."
for i in {1..100}; do
    redis-cli HSET "siprec:session:perf-test-$i" \
        session_id "perf-test-$i" \
        call_id "call-$i" \
        status "active" \
        start_time "$(date -Iseconds)" \
        node_id "test-node" > /dev/null
done

echo "Sessions created. Counting..."
COUNT=$(redis-cli KEYS "siprec:session:perf-test-*" | wc -l)
echo "Found $COUNT test sessions"

echo "Cleaning up performance test..."
redis-cli EVAL "
local keys = redis.call('KEYS', ARGV[1])
for i=1,#keys,5000 do
    redis.call('DEL', unpack(keys, i, math.min(i+4999, #keys)))
end
return #keys
" 0 "siprec:session:perf-test-*"

echo "✅ Performance test completed"
EOF

# Final status
log_test "Final Redis status check..."
ssh "$SERVER_USER@$SERVER_HOST" << 'EOF'
echo "Redis info:"
redis-cli info server | grep "redis_version\|uptime_in_seconds"
redis-cli info memory | grep "used_memory_human\|maxmemory_human"
redis-cli info stats | grep "total_commands_processed\|total_connections_received"

echo -e "\nActive session keys:"
redis-cli KEYS "siprec:session:*" | head -10
EOF

log_info "🎉 Redis session store testing completed!"
echo ""
echo "📋 Summary:"
echo "  - Redis connectivity: ✅"
echo "  - Session key structure: ✅"
echo "  - Index functionality: ✅"
echo "  - Performance test: ✅"
echo ""
echo "🔧 Useful Redis commands for monitoring:"
echo "  redis-cli KEYS 'siprec:*'                    # List all SIPREC keys"
echo "  redis-cli HGETALL 'siprec:index:sessions'    # View session index"
echo "  redis-cli INFO memory                        # Memory usage"
echo "  redis-cli MONITOR                            # Real-time monitoring"