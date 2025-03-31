#!/bin/bash

# Kill any existing test servers
pkill -f "test_tls" > /dev/null 2>&1

# Go to the correct directory
cd "$(dirname "$0")"

# Build the test programs
echo "Building TLS test program..."
cd test_tls
go build -o test_tls main.go
cd ..

# Start the test TLS server
echo "Starting TLS server..."
cd test_tls
./test_tls > server.log 2>&1 &
SERVER_PID=$!
cd ..

# Wait for the server to start
sleep 2

# Test with our test client
echo "Testing with Go client..."
cd test_tls
./test_tls client

# Test with OpenSSL
echo -e "\nTesting with OpenSSL..."
echo -e "OPTIONS sip:127.0.0.1:5063 SIP/2.0\r\nVia: SIP/2.0/TLS 127.0.0.1:9999;branch=z9hG4bK-test\r\nTo: <sip:test@127.0.0.1>\r\nFrom: <sip:tester@127.0.0.1>;tag=test123\r\nCall-ID: test-call-id\r\nCSeq: 1 OPTIONS\r\nMax-Forwards: 70\r\nContent-Length: 0\r\n\r\n" | openssl s_client -connect 127.0.0.1:5063 -quiet

# Clean up
echo -e "\nCleaning up..."
kill $SERVER_PID

echo "Test complete!"