#\!/bin/bash

# Send a SIPREC INVITE request
BOUNDARY="boundary1234"
CALL_ID="siprec-test-$(date +%s)"

cat <<EOI | nc -u -w2 127.0.0.1 5060
INVITE sip:siprec@127.0.0.1:5060 SIP/2.0
Via: SIP/2.0/UDP 127.0.0.1:5555;branch=z9hG4bK-siprec-001
From: <sip:recorder@pbx.example.com>;tag=recorder123
To: <sip:siprec@127.0.0.1>
Call-ID: $CALL_ID
CSeq: 1 INVITE
Contact: <sip:recorder@127.0.0.1:5555>
Max-Forwards: 70
User-Agent: PBX Recording Agent
Content-Type: multipart/mixed;boundary=$BOUNDARY
Content-Length: 1200

--$BOUNDARY
Content-Type: application/sdp

v=0
o=- 123456 654321 IN IP4 127.0.0.1
s=Recording Session
c=IN IP4 127.0.0.1
t=0 0
m=audio 16384 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=sendonly

--$BOUNDARY
Content-Type: application/rs-metadata+xml

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session>
    <sessionid>call-12345</sessionid>
  </session>
  <participant id="p1">
    <name>Alice</name>
    <aor>sip:alice@example.com</aor>
  </participant>
  <participant id="p2">
    <name>Bob</name>
    <aor>sip:bob@example.com</aor>
  </participant>
</recording>
--$BOUNDARY--
EOI
EOF < /dev/null