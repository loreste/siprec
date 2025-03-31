#!/bin/bash

# Test TLS connectivity to the SIP server
echo "Testing TLS connectivity to SIP server..."
echo -e "OPTIONS sip:127.0.0.1:5061 SIP/2.0\r\nVia: SIP/2.0/TLS 127.0.0.1:9999;branch=z9hG4bK-test\r\nTo: <sip:test@127.0.0.1>\r\nFrom: <sip:tester@127.0.0.1>;tag=test123\r\nCall-ID: test-call-id\r\nCSeq: 1 OPTIONS\r\nMax-Forwards: 70\r\nContent-Length: 0\r\n\r\n" | openssl s_client -connect 127.0.0.1:5061 -quiet

echo "Test complete!"