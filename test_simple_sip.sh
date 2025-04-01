#!/bin/bash

# Simple SIP OPTIONS message for testing

SERVER_IP="127.0.0.1"
SERVER_PORT="5060"

# Create a simple SIP OPTIONS message
cat > simple_options.txt << EOF
OPTIONS sip:127.0.0.1:5060 SIP/2.0
Via: SIP/2.0/UDP 192.168.1.100:5080;branch=z9hG4bK-test-$(date +%s)
From: <sip:tester@example.com>;tag=test-tag
To: <sip:127.0.0.1>
Call-ID: test-options-$(date +%s)
CSeq: 1 OPTIONS
Max-Forwards: 70
Content-Length: 0

EOF

# Send the OPTIONS to the server using netcat
echo "Sending SIP OPTIONS to ${SERVER_IP}:${SERVER_PORT}..."
cat simple_options.txt | nc -u ${SERVER_IP} ${SERVER_PORT}

# Print success message
echo "SIP OPTIONS sent!"