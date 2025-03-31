#!/bin/bash

# Script to build and run the SIPREC server
set -e

# Check if recordings directory exists
if [ ! -d "./recordings" ]; then
    echo "Creating recordings directory..."
    mkdir -p ./recordings
fi

# Check if certs directory exists
if [ ! -d "./certs" ]; then
    echo "Creating certs directory..."
    mkdir -p ./certs
fi

# Check if .env file exists
if [ ! -f "./.env" ] && [ -f "./.env.example" ]; then
    echo "No .env file found. Copying from .env.example..."
    cp .env.example .env
    echo "Please edit .env file with your configuration!"
    exit 1
fi

# Build the application
echo "Building SIPREC server..."
go build -o siprec-server ./cmd/siprec

# Run the application
echo "Starting SIPREC server..."
./siprec-server