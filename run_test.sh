#!/bin/bash

# Kill any existing processes
pkill -f siprec || true

# Set up environment variables with different ports
export PORTS=6060,6061
export HTTP_PORT=9090
export RTP_PORT_MIN=30000
export RTP_PORT_MAX=40000

# Run the main SIPREC server in the background
go run cmd/siprec/main.go &
SERVER_PID=$!

echo "SIPREC server started with PID $SERVER_PID"
echo "Waiting for server to initialize..."
sleep 5

echo "Starting WebSocket test client..."
# Run the WebSocket test client
go run test_websocket.go

# When the WebSocket test client exits, kill the server
kill $SERVER_PID