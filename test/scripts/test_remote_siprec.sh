#!/bin/bash
# Test script for remote SIPREC server

SERVER="35.222.226.67"
SIP_PORT="5060"
HTTP_PORT="8080"

echo "=== SIPREC Remote Server Test ==="
echo "Target: $SERVER:$SIP_PORT"
echo "Time: $(date)"
echo ""

# Test 1: Basic connectivity
echo "1. Testing basic connectivity..."
ping -c 3 $SERVER > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "✓ Server is reachable via ping"
else
    echo "✗ Server is not reachable via ping"
fi

# Test 2: Port connectivity
echo ""
echo "2. Testing port connectivity..."

# Test SIP UDP port
echo "Testing SIP UDP port $SIP_PORT..."
nc -z -u -w 3 $SERVER $SIP_PORT
if [ $? -eq 0 ]; then
    echo "✓ SIP UDP port $SIP_PORT is open"
else
    echo "✗ SIP UDP port $SIP_PORT appears closed or filtered"
fi

# Test HTTP TCP port
echo "Testing HTTP TCP port $HTTP_PORT..."
nc -z -w 3 $SERVER $HTTP_PORT
if [ $? -eq 0 ]; then
    echo "✓ HTTP TCP port $HTTP_PORT is open"
else
    echo "✗ HTTP TCP port $HTTP_PORT appears closed"
fi

# Test 3: HTTP endpoints
echo ""
echo "3. Testing HTTP endpoints..."

# Health check
echo "Testing health endpoint..."
HEALTH_RESPONSE=$(curl -s -w "%{http_code}" -o /tmp/health_response.txt http://$SERVER:$HTTP_PORT/health 2>/dev/null)
if [[ $HEALTH_RESPONSE == "200" ]]; then
    echo "✓ Health endpoint responded with 200 OK"
    echo "  Response: $(cat /tmp/health_response.txt)"
else
    echo "✗ Health endpoint failed with code: $HEALTH_RESPONSE"
fi

# Metrics endpoint
echo "Testing metrics endpoint..."
METRICS_RESPONSE=$(curl -s -w "%{http_code}" -o /dev/null http://$SERVER:$HTTP_PORT/metrics 2>/dev/null)
if [[ $METRICS_RESPONSE == "200" ]]; then
    echo "✓ Metrics endpoint responded with 200 OK"
else
    echo "✗ Metrics endpoint failed with code: $METRICS_RESPONSE"
fi

# Test 4: SIP OPTIONS request
echo ""
echo "4. Testing SIP OPTIONS request..."

CALL_ID="test-$(date +%s)@testclient"
BRANCH="z9hG4bK$(date +%s)"
TAG="tag$(date +%s)"

# Create SIP OPTIONS message
SIP_OPTIONS="OPTIONS sip:$SERVER:$SIP_PORT SIP/2.0\r
Via: SIP/2.0/UDP $(hostname -I | awk '{print $1}'):5060;branch=$BRANCH\r
Max-Forwards: 70\r
To: <sip:$SERVER:$SIP_PORT>\r
From: TestClient <sip:test@testclient.local>;tag=$TAG\r
Call-ID: $CALL_ID\r
CSeq: 1 OPTIONS\r
Contact: <sip:test@$(hostname -I | awk '{print $1}'):5060>\r
User-Agent: SIPREC-Test-Client/1.0\r
Content-Length: 0\r
\r
"

echo "Sending SIP OPTIONS to $SERVER:$SIP_PORT..."
echo "Call-ID: $CALL_ID"

# Send SIP OPTIONS and capture response
timeout 5 bash -c "echo -e '$SIP_OPTIONS' | nc -u $SERVER $SIP_PORT" > /tmp/sip_response.txt 2>&1

if [ -s /tmp/sip_response.txt ]; then
    echo "✓ Received SIP response:"
    cat /tmp/sip_response.txt
else
    echo "✗ No SIP response received (timeout after 5 seconds)"
fi

# Test 5: SIPREC INVITE simulation
echo ""
echo "5. Testing SIPREC INVITE simulation..."

SIPREC_CALL_ID="siprec-test-$(date +%s)@testclient"
SIPREC_BRANCH="z9hG4bK$(date +%s)"
SIPREC_TAG="tag$(date +%s)"

# Basic SIPREC INVITE (simplified)
SIPREC_INVITE="INVITE sip:recorder@$SERVER:$SIP_PORT SIP/2.0\r
Via: SIP/2.0/UDP $(hostname -I | awk '{print $1}'):5060;branch=$SIPREC_BRANCH\r
Max-Forwards: 70\r
To: <sip:recorder@$SERVER:$SIP_PORT>\r
From: SRS <sip:srs@testclient.local>;tag=$SIPREC_TAG\r
Call-ID: $SIPREC_CALL_ID\r
CSeq: 1 INVITE\r
Contact: <sip:srs@$(hostname -I | awk '{print $1}'):5060>\r
User-Agent: SIPREC-SRS-Test/1.0\r
Content-Type: application/sdp\r
Content-Length: 200\r
\r
v=0\r
o=srs 123456 654321 IN IP4 $(hostname -I | awk '{print $1}')\r
s=SIPREC Session\r
c=IN IP4 $(hostname -I | awk '{print $1}')\r
t=0 0\r
m=audio 10000 RTP/AVP 0\r
a=sendonly\r
"

echo "Sending SIPREC INVITE to $SERVER:$SIP_PORT..."
echo "Call-ID: $SIPREC_CALL_ID"

# Send SIPREC INVITE and capture response
timeout 5 bash -c "echo -e '$SIPREC_INVITE' | nc -u $SERVER $SIP_PORT" > /tmp/siprec_response.txt 2>&1

if [ -s /tmp/siprec_response.txt ]; then
    echo "✓ Received SIPREC response:"
    cat /tmp/siprec_response.txt
else
    echo "✗ No SIPREC response received (timeout after 5 seconds)"
fi

# Test 6: Port range test for RTP
echo ""
echo "6. Testing RTP port range (10000-10010 sample)..."
for port in {10000..10010}; do
    nc -z -u -w 1 $SERVER $port 2>/dev/null
    if [ $? -eq 0 ]; then
        echo "✓ RTP port $port is open"
    else
        echo "- RTP port $port is closed/filtered"
    fi
done

# Summary
echo ""
echo "=== Test Summary ==="
echo "Server: $SERVER"
echo "SIP Port: $SIP_PORT"
echo "HTTP Port: $HTTP_PORT"
echo "Test completed at: $(date)"
echo ""
echo "Next steps:"
echo "1. Check server logs: ssh user@$SERVER 'sudo journalctl -u siprec -f'"
echo "2. Verify firewall rules allow UDP/$SIP_PORT and TCP/$HTTP_PORT"
echo "3. Confirm SIPREC service is running: ssh user@$SERVER 'sudo systemctl status siprec'"

# Cleanup temp files
rm -f /tmp/health_response.txt /tmp/sip_response.txt /tmp/siprec_response.txt