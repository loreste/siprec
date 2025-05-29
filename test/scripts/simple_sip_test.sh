#!/bin/bash
# Simple SIP test for remote SIPREC server

SERVER="35.222.226.67"
SIP_PORT="5060"

echo "=== Simple SIP Test for SIPREC Server ==="
echo "Target: $SERVER:$SIP_PORT"
echo ""

# Get local IP for SIP headers
LOCAL_IP=$(ifconfig | grep -E "inet.*broadcast" | awk '{print $2}' | head -1)
if [ -z "$LOCAL_IP" ]; then
    LOCAL_IP="192.168.1.100"  # fallback
fi

echo "Using local IP: $LOCAL_IP"

# Test SIP OPTIONS
echo ""
echo "1. Sending SIP OPTIONS request..."

CALL_ID="test-$(date +%s)@testclient"
BRANCH="z9hG4bK$(date +%s)"
TAG="tag$(date +%s)"

# Create SIP OPTIONS message (properly formatted)
cat > /tmp/sip_options.txt << EOF
OPTIONS sip:$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$BRANCH
Max-Forwards: 70
To: <sip:$SERVER:$SIP_PORT>
From: TestClient <sip:test@testclient.local>;tag=$TAG
Call-ID: $CALL_ID
CSeq: 1 OPTIONS
Contact: <sip:test@$LOCAL_IP:5060>
User-Agent: SIPREC-Test-Client/1.0
Content-Length: 0

EOF

echo "Call-ID: $CALL_ID"
echo "Sending OPTIONS request..."

# Send SIP message and wait for response
(sleep 2; cat /tmp/sip_options.txt) | nc -u $SERVER $SIP_PORT > /tmp/sip_response.txt &
sleep 3

if [ -s /tmp/sip_response.txt ]; then
    echo "✓ Received SIP response:"
    echo "---"
    cat /tmp/sip_response.txt
    echo "---"
else
    echo "✗ No response received"
fi

# Test SIPREC INVITE
echo ""
echo "2. Sending SIPREC INVITE request..."

INVITE_CALL_ID="siprec-$(date +%s)@testclient"
INVITE_BRANCH="z9hG4bK$(date +%s)"
INVITE_TAG="tag$(date +%s)"

# Create SIPREC INVITE with proper SDP
cat > /tmp/siprec_invite.txt << EOF
INVITE sip:recorder@$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$INVITE_BRANCH
Max-Forwards: 70
To: <sip:recorder@$SERVER:$SIP_PORT>
From: SRS <sip:srs@testclient.local>;tag=$INVITE_TAG
Call-ID: $INVITE_CALL_ID
CSeq: 1 INVITE
Contact: <sip:srs@$LOCAL_IP:5060>
User-Agent: SIPREC-SRS-Test/1.0
Content-Type: application/sdp
Content-Length: 298

v=0
o=srs 123456 654321 IN IP4 $LOCAL_IP
s=SIPREC Test Session
c=IN IP4 $LOCAL_IP
t=0 0
m=audio 10000 RTP/AVP 0 8
a=sendonly
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000

EOF

echo "Call-ID: $INVITE_CALL_ID"
echo "Sending INVITE request..."

# Send INVITE and wait for response
(sleep 2; cat /tmp/siprec_invite.txt) | nc -u $SERVER $SIP_PORT > /tmp/invite_response.txt &
sleep 3

if [ -s /tmp/invite_response.txt ]; then
    echo "✓ Received INVITE response:"
    echo "---"
    cat /tmp/invite_response.txt
    echo "---"
else
    echo "✗ No INVITE response received"
fi

# Cleanup
rm -f /tmp/sip_options.txt /tmp/sip_response.txt /tmp/siprec_invite.txt /tmp/invite_response.txt

echo ""
echo "Test completed. Check server logs for detailed processing information."