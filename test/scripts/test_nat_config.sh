#!/bin/bash

# NAT Configuration Test Script for SIPREC Server
#
# This script provides comprehensive testing and validation of NAT configuration
# for SIPREC server deployments, particularly useful for cloud environments.
#
# Features:
# - GCP metadata service integration testing
# - NAT detection and validation
# - SIP protocol response verification
# - Firewall configuration checking
#
# Usage: ./test_nat_config.sh
#
# Author: SIPREC Server Project
# License: GPL v3

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Functions
log() { echo -e "${GREEN}[$(date +'%H:%M:%S')] $1${NC}"; }
warn() { echo -e "${YELLOW}[$(date +'%H:%M:%S')] $1${NC}"; }
error() { echo -e "${RED}[$(date +'%H:%M:%S')] $1${NC}"; }
info() { echo -e "${BLUE}[$(date +'%H:%M:%S')] $1${NC}"; }

echo "========================================"
echo "SIPREC NAT Configuration Test"
echo "========================================"

# Test 1: IP Detection
log "1. Testing IP Detection"

# Get GCP metadata IPs
GCP_EXTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/external-ip 2>/dev/null)
GCP_INTERNAL_IP=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip 2>/dev/null)

if [ -n "$GCP_EXTERNAL_IP" ] && [ -n "$GCP_INTERNAL_IP" ]; then
    info "✅ GCP Metadata Service Available"
    info "   External IP: $GCP_EXTERNAL_IP"
    info "   Internal IP: $GCP_INTERNAL_IP"
    
    if [ "$GCP_EXTERNAL_IP" != "$GCP_INTERNAL_IP" ]; then
        info "✅ NAT Configuration Detected"
        NAT_DETECTED=true
    else
        warn "⚠️ No NAT detected (External == Internal IP)"
        NAT_DETECTED=false
    fi
else
    warn "⚠️ GCP Metadata not available - testing fallback"
    
    # Fallback IP detection
    FALLBACK_EXTERNAL=$(curl -s https://api.ipify.org 2>/dev/null)
    FALLBACK_INTERNAL=$(hostname -I | awk '{print $1}')
    
    if [ -n "$FALLBACK_EXTERNAL" ] && [ -n "$FALLBACK_INTERNAL" ]; then
        info "✅ Fallback IP Detection"
        info "   External IP: $FALLBACK_EXTERNAL"
        info "   Internal IP: $FALLBACK_INTERNAL"
        
        if [ "$FALLBACK_EXTERNAL" != "$FALLBACK_INTERNAL" ]; then
            info "✅ NAT Configuration Detected (fallback)"
            NAT_DETECTED=true
        else
            warn "⚠️ No NAT detected (fallback)"
            NAT_DETECTED=false
        fi
    else
        error "❌ IP detection failed"
        exit 1
    fi
fi

echo

# Test 2: SIPREC Configuration
log "2. Testing SIPREC Configuration"

if [ -f "/etc/siprec/.env" ]; then
    info "✅ SIPREC config file found"
    
    # Source the config
    source /etc/siprec/.env
    
    info "   BEHIND_NAT: ${BEHIND_NAT:-not_set}"
    info "   EXTERNAL_IP: ${EXTERNAL_IP:-not_set}"
    info "   INTERNAL_IP: ${INTERNAL_IP:-not_set}"
    info "   STUN_SERVER: ${STUN_SERVER:-not_set}"
    
    # Validate configuration
    if [ "$BEHIND_NAT" = "true" ] && [ "$NAT_DETECTED" = "true" ]; then
        info "✅ NAT configuration matches detection"
    elif [ "$BEHIND_NAT" = "false" ] && [ "$NAT_DETECTED" = "false" ]; then
        info "✅ Direct configuration matches detection"
    else
        warn "⚠️ Configuration mismatch with NAT detection"
    fi
else
    warn "⚠️ SIPREC config file not found at /etc/siprec/.env"
fi

echo

# Test 3: Network Connectivity
log "3. Testing Network Connectivity"

# Test external IP reachability
if [ -n "${GCP_EXTERNAL_IP:-$FALLBACK_EXTERNAL}" ]; then
    EXTERNAL_IP_TEST="${GCP_EXTERNAL_IP:-$FALLBACK_EXTERNAL}"
    
    # Test if we can reach ourselves (basic connectivity)
    if ping -c 1 -W 5 "$EXTERNAL_IP_TEST" >/dev/null 2>&1; then
        info "✅ External IP is reachable: $EXTERNAL_IP_TEST"
    else
        warn "⚠️ External IP not reachable: $EXTERNAL_IP_TEST"
    fi
fi

# Test internal IP binding
if [ -n "${GCP_INTERNAL_IP:-$FALLBACK_INTERNAL}" ]; then
    INTERNAL_IP_TEST="${GCP_INTERNAL_IP:-$FALLBACK_INTERNAL}"
    
    if ping -c 1 -W 5 "$INTERNAL_IP_TEST" >/dev/null 2>&1; then
        info "✅ Internal IP is reachable: $INTERNAL_IP_TEST"
    else
        warn "⚠️ Internal IP not reachable: $INTERNAL_IP_TEST"
    fi
fi

echo

# Test 4: SIPREC Service
log "4. Testing SIPREC Service"

if systemctl is-active --quiet siprec-server 2>/dev/null; then
    info "✅ SIPREC service is running"
    
    # Test health endpoint
    if curl -f -s http://localhost:8080/health >/dev/null 2>&1; then
        info "✅ Health endpoint responding"
    else
        warn "⚠️ Health endpoint not responding"
    fi
    
    # Test SIP ports
    for port in 5060 5061; do
        if netstat -ln | grep ":$port " >/dev/null 2>&1; then
            info "✅ SIP port $port is listening"
        else
            warn "⚠️ SIP port $port not listening"
        fi
    done
    
else
    warn "⚠️ SIPREC service is not running"
fi

echo

# Test 5: SIP NAT Response
log "5. Testing SIP NAT Response"

if systemctl is-active --quiet siprec-server 2>/dev/null; then
    # Send SIP OPTIONS and check response
    SIP_RESPONSE=$(echo -e "OPTIONS sip:test@localhost:5060 SIP/2.0\r\nVia: SIP/2.0/UDP test:5070;branch=z9hG4bK-test\r\nFrom: sip:test@test;tag=test\r\nTo: sip:test@localhost:5060\r\nCall-ID: nat-test-$(date +%s)\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n" | nc -u -w 3 localhost 5060 2>/dev/null)
    
    if [ -n "$SIP_RESPONSE" ]; then
        info "✅ SIP service responding"
        
        # Check if response contains external IP (indicating proper NAT handling)
        if [ -n "$EXTERNAL_IP_TEST" ] && echo "$SIP_RESPONSE" | grep -q "$EXTERNAL_IP_TEST"; then
            info "✅ SIP response contains external IP (NAT configured correctly)"
        elif [ "$NAT_DETECTED" = "true" ]; then
            warn "⚠️ SIP response may not contain external IP (check NAT config)"
        else
            info "✅ SIP response (direct IP configuration)"
        fi
        
        # Check for SIPREC support
        if echo "$SIP_RESPONSE" | grep -i "supported:" | grep -q "siprec"; then
            info "✅ SIPREC support advertised"
        else
            warn "⚠️ SIPREC support not advertised in response"
        fi
    else
        warn "⚠️ No SIP response received"
    fi
else
    warn "⚠️ Cannot test SIP response - service not running"
fi

echo

# Test 6: Firewall Configuration
log "6. Testing Firewall Configuration"

# Check if UFW is active (Ubuntu/Debian)
if command -v ufw >/dev/null 2>&1; then
    if ufw status | grep -q "Status: active"; then
        info "✅ UFW firewall is active"
        
        # Check SIP ports
        for port in 5060 5061; do
            if ufw status | grep -q "$port"; then
                info "✅ SIP port $port allowed in firewall"
            else
                warn "⚠️ SIP port $port not found in firewall rules"
            fi
        done
        
        # Check RTP port range
        if ufw status | grep -q "16384:32768/udp"; then
            info "✅ RTP port range allowed in firewall"
        else
            warn "⚠️ RTP port range not found in firewall rules"
        fi
    else
        warn "⚠️ UFW firewall is not active"
    fi
fi

# Check if firewalld is active (CentOS/RHEL)
if command -v firewall-cmd >/dev/null 2>&1; then
    if systemctl is-active --quiet firewalld; then
        info "✅ firewalld is active"
        
        # Check ports
        for port in 5060 5061; do
            if firewall-cmd --list-ports | grep -q "${port}/udp\|${port}/tcp"; then
                info "✅ SIP port $port allowed in firewall"
            else
                warn "⚠️ SIP port $port not found in firewall rules"
            fi
        done
    else
        warn "⚠️ firewalld is not active"
    fi
fi

echo

# Summary
log "7. Configuration Summary"

echo "Network Configuration:"
echo "  NAT Detected: ${NAT_DETECTED}"
echo "  External IP: ${GCP_EXTERNAL_IP:-$FALLBACK_EXTERNAL}"
echo "  Internal IP: ${GCP_INTERNAL_IP:-$FALLBACK_INTERNAL}"
echo "  SIPREC NAT Config: ${BEHIND_NAT:-unknown}"

echo
echo "Recommendations:"

if [ "$NAT_DETECTED" = "true" ]; then
    if [ "$BEHIND_NAT" = "true" ]; then
        info "✅ NAT configuration is correct"
    else
        warn "⚠️ Set BEHIND_NAT=true in /etc/siprec/.env"
    fi
    
    info "📋 For optimal NAT traversal:"
    echo "   - Ensure EXTERNAL_IP is set correctly"
    echo "   - Configure STUN servers"
    echo "   - Test with external SIP clients"
else
    if [ "$BEHIND_NAT" = "false" ]; then
        info "✅ Direct IP configuration is correct"
    else
        warn "⚠️ Consider setting BEHIND_NAT=false for direct IP"
    fi
fi

echo
echo "========================================"
echo "NAT Configuration Test Complete"
echo "========================================"