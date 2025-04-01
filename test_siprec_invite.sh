#!/bin/bash

# Test script to send a SIPREC INVITE to the server
SERVER_IP="127.0.0.1"
SERVER_PORT="5060"

# Create a multipart SIPREC INVITE message
# Generate unique IDs
TIMESTAMP=$(date +%s)
CALL_ID="test-call-$TIMESTAMP"
SESSION_ID="test-session-$TIMESTAMP"

# Create the body first to calculate content length
cat > siprec_body.txt << EOF
--boundary1
Content-Type: application/sdp

v=0
o=recorder 1622133 1622133 IN IP4 192.168.1.100
s=SIPREC Test Call
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=sendonly

--boundary1
Content-Type: application/rs-metadata+xml

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <session session="$SESSION_ID" state="active" direction="inbound">
    <participant id="p1" name="Alice">
      <aor>sip:alice@example.com</aor>
    </participant>
    <participant id="p2" name="Bob">
      <aor>sip:bob@example.com</aor>
    </participant>
    <stream label="stream1" streamid="audio-stream" mode="separate" type="audio"></stream>
    <sessionrecordingassoc sessionid="$SESSION_ID" callid="$CALL_ID"></sessionrecordingassoc>
  </session>
</recording>
--boundary1--
EOF

# Calculate content length
CONTENT_LENGTH=$(wc -c < siprec_body.txt)

# Create the full SIP message with accurate Content-Length
cat > siprec_invite.txt << EOF
INVITE sip:record@127.0.0.1:5060 SIP/2.0
Via: SIP/2.0/UDP 192.168.1.100:5080;branch=z9hG4bK-siprec-test
From: <sip:recorder@example.com>;tag=siprec-from-tag
To: <sip:record@127.0.0.1>
Call-ID: $CALL_ID
CSeq: 1 INVITE
Max-Forwards: 70
Contact: <sip:recorder@192.168.1.100:5080>
Content-Type: multipart/mixed;boundary=boundary1
Content-Length: $CONTENT_LENGTH

EOF

# Append the body to the full message
cat siprec_body.txt >> siprec_invite.txt

# Send the INVITE to the server using netcat
echo "Sending SIPREC INVITE to ${SERVER_IP}:${SERVER_PORT}..."
cat siprec_invite.txt | nc -u ${SERVER_IP} ${SERVER_PORT}

# Print success message
echo "SIPREC INVITE sent!"