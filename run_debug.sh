#!/bin/bash

# Kill any existing SIPREC server
pkill -f siprec-server || true

# Wait for the process to terminate
sleep 1

# Build and run the SIPREC server with debug output
echo "Building SIPREC server for debug..."
cd /Users/lanceoreste/opensource/siprec
go build -o siprec-server ./cmd/siprec/

# Set debug level in env
export LOG_LEVEL=debug

# Run the server with debug output
echo "Starting SIPREC server in debug mode..."
./siprec-server 2>&1 | tee debug_output.log