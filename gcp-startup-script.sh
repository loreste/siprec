#!/bin/bash

# GCP VM Startup Script for SIPREC Server
#
# This script provides automated initialization and deployment of SIPREC server
# when creating GCP VM instances. It can be used as a startup script via
# GCP metadata or Cloud Init.
#
# Usage: 
# - VM metadata: startup-script-url=gs://your-bucket/gcp-startup-script.sh
# - VM metadata: startup-script="$(cat gcp-startup-script.sh)"
#
# Author: SIPREC Server Project
# License: GPL v3

set -e

# Configuration
REPO_URL="https://github.com/yourusername/siprec.git"  # Update with your repo
BRANCH="main"
DEPLOY_USER="siprec"

# Logging
exec > >(tee /var/log/siprec-startup.log)
exec 2>&1

echo "=== SIPREC GCP Startup Script ==="
echo "Starting at: $(date)"

# Update system
apt-get update -y

# Install git if not present
if ! command -v git &> /dev/null; then
    apt-get install -y git
fi

# Clone repository to temporary location
TEMP_DIR="/tmp/siprec-deploy"
rm -rf "$TEMP_DIR"
git clone "$REPO_URL" "$TEMP_DIR"
cd "$TEMP_DIR"

# Checkout specific branch
git checkout "$BRANCH"

# Make deployment script executable
chmod +x deploy_gcp_linux.sh

# Run deployment
./deploy_gcp_linux.sh

echo "=== SIPREC Startup Complete ==="
echo "Completed at: $(date)"

# Signal completion to GCP
/usr/bin/google_metadata_script_runner --script-type startup --debug