#!/bin/bash
# Comprehensive SIPREC Local Test Script
# Tests SIP signaling, SIPREC metadata, and session recording

SERVER="35.222.226.67"
SIP_PORT="5060"
HTTP_PORT="8080"
LOCAL_IP=$(ifconfig | grep -E "inet.*broadcast" | awk '{print $2}' | head -1)

if [ -z "$LOCAL_IP" ]; then
    LOCAL_IP="192.168.1.100"
fi

echo "=== SIPREC Comprehensive Local Test ==="
echo "Target Server: $SERVER:$SIP_PORT"
echo "Local IP: $LOCAL_IP"
echo "Test Time: $(date)"
echo ""

# Create test data directory
mkdir -p siprec_test_data
cd siprec_test_data

# Test 1: SIPREC INVITE with full metadata
echo "1. Testing SIPREC INVITE with full metadata..."

CALL_ID="siprec-metadata-test-$(date +%s)@testclient"
BRANCH="z9hG4bK$(date +%s)"
TAG="tag$(date +%s)"
SESSION_ID="sess_$(date +%s)"
RECORDING_SESSION_ID="rs_$(date +%s)"

# Create comprehensive SIPREC SDP with metadata
cat > siprec_invite_full.txt << EOF
INVITE sip:recorder@$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$BRANCH
Max-Forwards: 70
To: <sip:recorder@$SERVER:$SIP_PORT>
From: SRS <sip:srs@testclient.local>;tag=$TAG
Call-ID: $CALL_ID
CSeq: 1 INVITE
Contact: <sip:srs@$LOCAL_IP:5060>
User-Agent: SIPREC-SRS-TestClient/1.0
Content-Type: multipart/mixed;boundary=boundary123
Content-Length: 1500

--boundary123
Content-Type: application/sdp

v=0
o=srs 123456789 987654321 IN IP4 $LOCAL_IP
s=SIPREC Recording Session
c=IN IP4 $LOCAL_IP
t=0 0
a=group:BUNDLE audio1 audio2
m=audio 10000 RTP/AVP 0 8
a=sendonly
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=label:1
m=audio 10002 RTP/AVP 0 8
a=sendonly
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=label:2

--boundary123
Content-Type: application/rs-metadata+xml

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session id="$SESSION_ID">
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
  </session>
  <participant id="part1">
    <nameID aor="sip:alice@example.com">
      <name>Alice Smith</name>
    </nameID>
  </participant>
  <participant id="part2">
    <nameID aor="sip:bob@example.com">
      <name>Bob Johnson</name>
    </nameID>
  </participant>
  <stream id="stream1" session="$SESSION_ID">
    <label>1</label>
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
  </stream>
  <stream id="stream2" session="$SESSION_ID">
    <label>2</label>
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
  </stream>
  <participantstreamassoc id="psa1">
    <participant>part1</participant>
    <stream>stream1</stream>
  </participantstreamassoc>
  <participantstreamassoc id="psa2">
    <participant>part2</participant>
    <stream>stream2</stream>
  </participantstreamassoc>
  <recordingsession id="$RECORDING_SESSION_ID">
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
    <reason>compliance</reason>
  </recordingsession>
</recording>

--boundary123--

EOF

echo "Sending SIPREC INVITE with metadata..."
echo "Call-ID: $CALL_ID"
echo "Session-ID: $SESSION_ID"
echo "Recording-Session-ID: $RECORDING_SESSION_ID"

# Send INVITE
response_file="invite_response_$(date +%s).txt"
(sleep 3; cat siprec_invite_full.txt) | nc -u $SERVER $SIP_PORT > $response_file &
sleep 5

if [ -s $response_file ]; then
    echo "✓ Received INVITE response:"
    echo "--- Response ---"
    cat $response_file
    echo "--- End Response ---"
    
    # Check response code
    if grep -q "200 OK" $response_file; then
        echo "✓ INVITE accepted (200 OK)"
        INVITE_SUCCESS=true
    elif grep -q "100 Trying" $response_file; then
        echo "✓ INVITE processing (100 Trying)"
        INVITE_SUCCESS=true
    else
        echo "⚠ INVITE response received but not 200 OK"
        INVITE_SUCCESS=false
    fi
else
    echo "✗ No INVITE response received"
    INVITE_SUCCESS=false
fi

# Test 2: Send SIPREC UPDATE with metadata changes
if [ "$INVITE_SUCCESS" = true ]; then
    echo ""
    echo "2. Testing SIPREC UPDATE with metadata changes..."
    
    UPDATE_BRANCH="z9hG4bK$(date +%s)"
    
    cat > siprec_update.txt << EOF
UPDATE sip:recorder@$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$UPDATE_BRANCH
Max-Forwards: 70
To: <sip:recorder@$SERVER:$SIP_PORT>
From: SRS <sip:srs@testclient.local>;tag=$TAG
Call-ID: $CALL_ID
CSeq: 2 UPDATE
Contact: <sip:srs@$LOCAL_IP:5060>
User-Agent: SIPREC-SRS-TestClient/1.0
Content-Type: application/rs-metadata+xml
Content-Length: 800

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>partial</datamode>
  <session id="$SESSION_ID">
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
  </session>
  <participant id="part3">
    <nameID aor="sip:charlie@example.com">
      <name>Charlie Brown</name>
    </nameID>
  </participant>
  <stream id="stream3" session="$SESSION_ID">
    <label>3</label>
    <associate-time>$(date -u +%Y-%m-%dT%H:%M:%SZ)</associate-time>
  </stream>
  <participantstreamassoc id="psa3">
    <participant>part3</participant>
    <stream>stream3</stream>
  </participantstreamassoc>
</recording>

EOF

    update_response_file="update_response_$(date +%s).txt"
    echo "Sending UPDATE with new participant..."
    (sleep 2; cat siprec_update.txt) | nc -u $SERVER $SIP_PORT > $update_response_file &
    sleep 3
    
    if [ -s $update_response_file ]; then
        echo "✓ Received UPDATE response:"
        cat $update_response_file
    else
        echo "✗ No UPDATE response received"
    fi
fi

# Test 3: Test session pause/resume
echo ""
echo "3. Testing session pause/resume..."

if [ "$INVITE_SUCCESS" = true ]; then
    # Send INFO for pause
    INFO_BRANCH="z9hG4bK$(date +%s)"
    
    cat > siprec_pause.txt << EOF
INFO sip:recorder@$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$INFO_BRANCH
Max-Forwards: 70
To: <sip:recorder@$SERVER:$SIP_PORT>
From: SRS <sip:srs@testclient.local>;tag=$TAG
Call-ID: $CALL_ID
CSeq: 3 INFO
Contact: <sip:srs@$LOCAL_IP:5060>
User-Agent: SIPREC-SRS-TestClient/1.0
Content-Type: application/rs-metadata+xml
Content-Length: 400

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>partial</datamode>
  <recordingsession id="$RECORDING_SESSION_ID">
    <state>pause</state>
    <reason>user_request</reason>
  </recordingsession>
</recording>

EOF

    pause_response_file="pause_response_$(date +%s).txt"
    echo "Sending pause request..."
    (sleep 2; cat siprec_pause.txt) | nc -u $SERVER $SIP_PORT > $pause_response_file &
    sleep 3
    
    if [ -s $pause_response_file ]; then
        echo "✓ Received pause response:"
        cat $pause_response_file
    else
        echo "✗ No pause response received"
    fi
fi

# Test 4: Send BYE to terminate session
if [ "$INVITE_SUCCESS" = true ]; then
    echo ""
    echo "4. Testing session termination..."
    
    BYE_BRANCH="z9hG4bK$(date +%s)"
    
    cat > siprec_bye.txt << EOF
BYE sip:recorder@$SERVER:$SIP_PORT SIP/2.0
Via: SIP/2.0/UDP $LOCAL_IP:5060;branch=$BYE_BRANCH
Max-Forwards: 70
To: <sip:recorder@$SERVER:$SIP_PORT>
From: SRS <sip:srs@testclient.local>;tag=$TAG
Call-ID: $CALL_ID
CSeq: 4 BYE
Contact: <sip:srs@$LOCAL_IP:5060>
User-Agent: SIPREC-SRS-TestClient/1.0
Content-Length: 0

EOF

    bye_response_file="bye_response_$(date +%s).txt"
    echo "Sending BYE to terminate session..."
    (sleep 2; cat siprec_bye.txt) | nc -u $SERVER $SIP_PORT > $bye_response_file &
    sleep 3
    
    if [ -s $bye_response_file ]; then
        echo "✓ Received BYE response:"
        cat $bye_response_file
    else
        echo "✗ No BYE response received"
    fi
fi

# Test 5: Check server metrics after test
echo ""
echo "5. Checking server metrics after test..."

METRICS_AFTER=$(curl -s http://$SERVER:$HTTP_PORT/metrics | grep -E "(siprec_active_calls|siprec_total_sessions)")
echo "Metrics after test:"
echo "$METRICS_AFTER"

# Test 6: Simulate RTP traffic
echo ""
echo "6. Simulating RTP traffic..."

echo "Sending test RTP packets to port 10000..."
for i in {1..5}; do
    echo "RTP packet $i" | nc -u $SERVER 10000 2>/dev/null
    sleep 0.1
done
echo "RTP simulation complete"

# Test 7: Check recordings directory (if accessible via HTTP)
echo ""
echo "7. Checking for recording artifacts..."

RECORDINGS_CHECK=$(curl -s -w "%{http_code}" -o /dev/null http://$SERVER:$HTTP_PORT/recordings/ 2>/dev/null)
if [ "$RECORDINGS_CHECK" = "200" ]; then
    echo "✓ Recordings endpoint accessible"
else
    echo "- Recordings endpoint not accessible (expected for security)"
fi

# Summary
echo ""
echo "=== Test Summary ==="
echo "Test completed: $(date)"
echo "Call-ID: $CALL_ID"
echo "Session-ID: $SESSION_ID"
echo "Recording-Session-ID: $RECORDING_SESSION_ID"
echo ""
echo "Generated test files:"
ls -la *.txt 2>/dev/null || echo "No response files generated"
echo ""
echo "To verify server processing:"
echo "1. Check server logs: ssh user@$SERVER 'sudo journalctl -u siprec -f'"
echo "2. Check recordings directory: ssh user@$SERVER 'ls -la /opt/siprec/recordings/'"
echo "3. Review session data: ssh user@$SERVER 'ls -la /opt/siprec/sessions/'"
echo ""
echo "Test complete! 🎉"

cd ..