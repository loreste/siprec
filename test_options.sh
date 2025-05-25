#!/bin/bash

# Send a SIP OPTIONS request to test the server
(cat <<EOF
OPTIONS sip:siprec@127.0.0.1:5060 SIP/2.0
Via: SIP/2.0/UDP 127.0.0.1:5555;branch=z9hG4bK-test-001
From: <sip:test@127.0.0.1>;tag=test123
To: <sip:siprec@127.0.0.1>
Call-ID: test-options-$(date +%s)
CSeq: 1 OPTIONS
Contact: <sip:test@127.0.0.1:5555>
Max-Forwards: 70
User-Agent: SIP Test Client
Content-Length: 0

EOF
) | nc -u -w1 127.0.0.1 5060