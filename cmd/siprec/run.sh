#!/bin/bash

# Script to run the SIPREC server from the cmd/siprec directory
set -e

# Get the directory of the script
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/../.." && pwd )"
RECORDINGS_DIR="$PROJECT_ROOT/recordings"

# Check if recordings directory exists
if [ ! -d "$RECORDINGS_DIR" ]; then
    echo "Creating recordings directory..."
    mkdir -p "$RECORDINGS_DIR"
fi

# Check if .env file exists
if [ ! -f "$PROJECT_ROOT/.env" ] && [ -f "$PROJECT_ROOT/.env.example" ]; then
    echo "No .env file found. Copying from .env.example..."
    cp "$PROJECT_ROOT/.env.example" "$PROJECT_ROOT/.env"
    echo "Please edit .env file with your configuration!"
    exit 1
fi

# Build the application
echo "Building SIPREC server..."
cd "$PROJECT_ROOT"
go build -o siprec-server ./cmd/siprec

# Run the application
echo "Starting SIPREC server..."
cd "$PROJECT_ROOT"
./siprec-server