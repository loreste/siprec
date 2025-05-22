#!/bin/bash

# Docker entrypoint script for SIPREC server
set -e

# Configuration
APP_USER="siprec"
APP_DIR="/app"
LOG_LEVEL="${LOG_LEVEL:-info}"
HTTP_PORT="${HTTP_PORT:-8080}"
SIP_PORT="${SIP_PORT:-5060}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# Print startup banner
print_banner() {
    cat << 'EOF'
  ____  ___ ____  ____  _____ ____   ____                           
 / ___|_ _|  _ \|  _ \| ____/ ___| / ___|  ___ _ ____   _____ _ __ 
 \___ \| || |_) | |_) |  _|| |     \___ \ / _ \ '__\ \ / / _ \ '__|
  ___) | ||  __/|  _ <| |__| |___   ___) |  __/ |   \ V /  __/ |   
 |____/___|_|   |_| \_\_____\____| |____/ \___|_|    \_/ \___|_|   

EOF
    echo "Version: ${VERSION:-dev}"
    echo "Build: ${BUILD_TIME:-unknown}"
    echo "Commit: ${COMMIT:-unknown}"
    echo "======================================================================"
}

# Validate environment
validate_environment() {
    log_info "Validating environment..."
    
    # Check if running as correct user
    if [ "$(id -u)" -eq 0 ] && [ "${ALLOW_ROOT:-false}" != "true" ]; then
        log_error "Running as root is not recommended. Set ALLOW_ROOT=true to override."
        exit 1
    fi
    
    # Check required directories
    for dir in recordings sessions logs; do
        if [ ! -d "${APP_DIR}/${dir}" ]; then
            log_warn "Directory ${dir} not found, creating..."
            mkdir -p "${APP_DIR}/${dir}"
        fi
        
        # Ensure proper permissions
        if [ "$(id -u)" -eq 0 ]; then
            chown -R ${APP_USER}:${APP_USER} "${APP_DIR}/${dir}"
        fi
    done
    
    # Check port availability
    if command -v netstat >/dev/null 2>&1; then
        if netstat -tuln | grep -q ":${HTTP_PORT} "; then
            log_warn "Port ${HTTP_PORT} appears to be in use"
        fi
    fi
    
    log_success "Environment validation completed"
}

# Setup configuration
setup_configuration() {
    log_info "Setting up configuration..."
    
    # Set default environment variables if not provided
    export GO_ENV="${GO_ENV:-production}"
    export LOG_LEVEL="${LOG_LEVEL}"
    export HTTP_PORT="${HTTP_PORT}"
    export SIP_PORT="${SIP_PORT}"
    export RECORDINGS_DIR="${APP_DIR}/recordings"
    export SESSIONS_DIR="${APP_DIR}/sessions"
    export LOGS_DIR="${APP_DIR}/logs"
    
    # AMQP configuration
    if [ -n "${AMQP_URL}" ]; then
        export AMQP_URL="${AMQP_URL}"
        export AMQP_QUEUE_NAME="${AMQP_QUEUE_NAME:-siprec_transcriptions}"
        log_info "AMQP messaging enabled"
    else
        log_warn "AMQP_URL not set, messaging disabled"
    fi
    
    # STT provider configuration
    if [ -n "${AWS_REGION}" ] || [ -n "${AWS_ACCESS_KEY_ID}" ]; then
        log_info "AWS Transcribe available"
    fi
    
    if [ -n "${AZURE_SPEECH_KEY}" ] && [ -n "${AZURE_SPEECH_REGION}" ]; then
        log_info "Azure Speech Services available"
    fi
    
    if [ -n "${GOOGLE_APPLICATION_CREDENTIALS}" ]; then
        log_info "Google Speech-to-Text available"
    fi
    
    log_success "Configuration setup completed"
}

# Health check function
health_check() {
    local max_attempts=30
    local attempt=1
    
    log_info "Waiting for application to be ready..."
    
    while [ $attempt -le $max_attempts ]; do
        if curl -sf "http://localhost:${HTTP_PORT}/health" >/dev/null 2>&1; then
            log_success "Application is ready!"
            return 0
        fi
        
        if [ $attempt -eq 1 ]; then
            log_info "Waiting for health check to pass..."
        fi
        
        sleep 2
        attempt=$((attempt + 1))
    done
    
    log_error "Health check failed after ${max_attempts} attempts"
    return 1
}

# Signal handlers
cleanup() {
    log_info "Received shutdown signal, cleaning up..."
    
    # Kill child processes
    if [ -n "$APP_PID" ]; then
        kill -TERM "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
    
    log_success "Cleanup completed"
    exit 0
}

# Trap signals
trap cleanup SIGTERM SIGINT

# Pre-flight checks
preflight_checks() {
    log_info "Running pre-flight checks..."
    
    # Check if binary exists and is executable
    if [ ! -x "${APP_DIR}/siprec" ]; then
        log_error "SIPREC binary not found or not executable"
        exit 1
    fi
    
    # Check TLS certificates if TLS is enabled
    if [ "${TLS_ENABLED:-false}" = "true" ]; then
        if [ ! -f "${TLS_CERT_FILE:-/app/certs/server.crt}" ] || [ ! -f "${TLS_KEY_FILE:-/app/certs/server.key}" ]; then
            log_error "TLS enabled but certificate files not found"
            exit 1
        fi
        log_info "TLS certificates found"
    fi
    
    # Check available disk space
    local available_space=$(df "${APP_DIR}" | awk 'NR==2 {print $4}')
    local min_space=1048576  # 1GB in KB
    
    if [ "$available_space" -lt "$min_space" ]; then
        log_warn "Low disk space: ${available_space}KB available"
    fi
    
    log_success "Pre-flight checks completed"
}

# Initialize application data
initialize_data() {
    log_info "Initializing application data..."
    
    # Create default configuration if it doesn't exist
    if [ ! -f "${APP_DIR}/config.yaml" ] && [ "${CREATE_DEFAULT_CONFIG:-true}" = "true" ]; then
        log_info "Creating default configuration..."
        cat > "${APP_DIR}/config.yaml" << EOF
# SIPREC Server Configuration
server:
  http_port: ${HTTP_PORT}
  sip_port: ${SIP_PORT}
  log_level: ${LOG_LEVEL}
  
recording:
  enabled: true
  storage_path: "${APP_DIR}/recordings"
  
transcription:
  enabled: true
  providers:
    - name: mock
      enabled: true
      
messaging:
  enabled: false
EOF
    fi
    
    log_success "Data initialization completed"
}

# Main execution
main() {
    print_banner
    validate_environment
    setup_configuration
    preflight_checks
    initialize_data
    
    # Handle different commands
    case "${1:-}" in
        "siprec"|"")
            log_info "Starting SIPREC server..."
            exec "${APP_DIR}/siprec" "${@:2}" &
            APP_PID=$!
            
            # Wait for application to start
            sleep 5
            
            # Run health check in background mode
            if [ "${SKIP_HEALTH_CHECK:-false}" != "true" ]; then
                health_check || exit 1
            fi
            
            # Wait for the application
            wait $APP_PID
            ;;
            
        "testenv")
            log_info "Starting test environment..."
            exec "${APP_DIR}/testenv" "${@:2}"
            ;;
            
        "health")
            # Health check command
            if curl -sf "http://localhost:${HTTP_PORT}/health" >/dev/null 2>&1; then
                log_success "Application is healthy"
                exit 0
            else
                log_error "Application is not healthy"
                exit 1
            fi
            ;;
            
        "version")
            # Version information
            echo "SIPREC Server"
            echo "Version: ${VERSION:-dev}"
            echo "Build Time: ${BUILD_TIME:-unknown}"
            echo "Commit: ${COMMIT:-unknown}"
            exit 0
            ;;
            
        "shell"|"bash")
            # Interactive shell
            log_info "Starting interactive shell..."
            exec /bin/sh
            ;;
            
        *)
            # Unknown command, try to execute it
            log_info "Executing command: $*"
            exec "$@"
            ;;
    esac
}

# Execute main function
main "$@"