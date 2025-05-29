#!/bin/bash

# Quick GCP Deployment Script for SIPREC Server
#
# This script provides one-command deployment of SIPREC server on Google Cloud Platform.
# It creates a VM instance, configures networking, and automatically deploys the server.
#
# Features:
# - Automatic VM creation with optimal configuration
# - Firewall rules setup for SIP and RTP traffic
# - Complete deployment with health monitoring
#
# Usage: ./deploy-quick.sh
#
# Author: SIPREC Server Project
# License: MIT

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
PROJECT_ID=""
ZONE="us-central1-a"
MACHINE_TYPE="e2-standard-2"
INSTANCE_NAME="siprec-server"

# Functions
log() { echo -e "${GREEN}[$(date +'%H:%M:%S')] $1${NC}"; }
warn() { echo -e "${YELLOW}[$(date +'%H:%M:%S')] $1${NC}"; }
error() { echo -e "${RED}[$(date +'%H:%M:%S')] $1${NC}"; exit 1; }

# Check if gcloud is installed
check_gcloud() {
    if ! command -v gcloud &> /dev/null; then
        error "gcloud CLI is not installed. Please install it first."
    fi
    log "gcloud CLI found"
}

# Get project ID if not set
get_project_id() {
    if [ -z "$PROJECT_ID" ]; then
        PROJECT_ID=$(gcloud config get-value project 2>/dev/null)
        if [ -z "$PROJECT_ID" ]; then
            error "No GCP project set. Run: gcloud config set project YOUR_PROJECT_ID"
        fi
    fi
    log "Using project: $PROJECT_ID"
}

# Create firewall rules
create_firewall_rules() {
    log "Creating firewall rules..."
    
    # Check if firewall rule exists
    if gcloud compute firewall-rules describe siprec-firewall --project="$PROJECT_ID" &>/dev/null; then
        warn "Firewall rule 'siprec-firewall' already exists"
    else
        gcloud compute firewall-rules create siprec-firewall \
            --project="$PROJECT_ID" \
            --allow tcp:22,tcp:80,tcp:443,tcp:5060,tcp:5061,tcp:5062,tcp:8080,udp:5060,udp:5061,udp:16384-32768 \
            --source-ranges 0.0.0.0/0 \
            --target-tags siprec-server \
            --description "Firewall rules for SIPREC Server"
        log "Firewall rules created"
    fi
}

# Create VM instance
create_instance() {
    log "Creating VM instance..."
    
    # Check if instance exists
    if gcloud compute instances describe "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" &>/dev/null; then
        warn "Instance '$INSTANCE_NAME' already exists"
        return
    fi
    
    # Create startup script
    cat > /tmp/siprec-startup.sh << 'EOF'
#!/bin/bash
apt-get update -y
apt-get install -y git curl wget

# Clone repository (update with your actual repository)
cd /opt
git clone https://github.com/yourusername/siprec.git
cd siprec
chmod +x deploy_gcp_linux.sh

# Run deployment
./deploy_gcp_linux.sh 2>&1 | tee /var/log/siprec-deployment.log
EOF

    # Create instance
    gcloud compute instances create "$INSTANCE_NAME" \
        --project="$PROJECT_ID" \
        --zone="$ZONE" \
        --machine-type="$MACHINE_TYPE" \
        --network-tier=PREMIUM \
        --maintenance-policy=MIGRATE \
        --provisioning-model=STANDARD \
        --service-account="$PROJECT_ID-compute@developer.gserviceaccount.com" \
        --scopes=https://www.googleapis.com/auth/cloud-platform \
        --tags=siprec-server \
        --create-disk=auto-delete=yes,boot=yes,device-name="$INSTANCE_NAME",image=projects/ubuntu-os-cloud/global/images/family/ubuntu-2204-lts,mode=rw,size=50,type=projects/"$PROJECT_ID"/zones/"$ZONE"/diskTypes/pd-standard \
        --no-shielded-secure-boot \
        --shielded-vtpm \
        --shielded-integrity-monitoring \
        --labels=environment=production,service=siprec \
        --reservation-affinity=any \
        --metadata-from-file startup-script=/tmp/siprec-startup.sh
    
    rm /tmp/siprec-startup.sh
    log "VM instance created"
}

# Wait for instance to be ready
wait_for_instance() {
    log "Waiting for instance to be ready..."
    
    # Wait for instance to be RUNNING
    while true; do
        STATUS=$(gcloud compute instances describe "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" --format="value(status)")
        if [ "$STATUS" = "RUNNING" ]; then
            break
        fi
        echo "Instance status: $STATUS. Waiting..."
        sleep 10
    done
    
    log "Instance is running"
    
    # Wait for startup script to complete
    log "Waiting for SIPREC deployment to complete (this may take 5-10 minutes)..."
    
    for i in {1..60}; do
        if gcloud compute ssh "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" --command="systemctl is-active siprec-server" &>/dev/null; then
            log "SIPREC server is active"
            break
        fi
        echo "Attempt $i/60: Waiting for deployment to complete..."
        sleep 30
    done
}

# Get instance information
get_instance_info() {
    EXTERNAL_IP=$(gcloud compute instances describe "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" --format="value(networkInterfaces[0].accessConfigs[0].natIP)")
    INTERNAL_IP=$(gcloud compute instances describe "$INSTANCE_NAME" --zone="$ZONE" --project="$PROJECT_ID" --format="value(networkInterfaces[0].networkIP)")
    
    log "Instance information retrieved"
}

# Display summary
show_summary() {
    echo
    echo "============================================"
    echo "SIPREC Server GCP Deployment Complete!"
    echo "============================================"
    echo "Project: $PROJECT_ID"
    echo "Zone: $ZONE"
    echo "Instance: $INSTANCE_NAME"
    echo "External IP: $EXTERNAL_IP"
    echo "Internal IP: $INTERNAL_IP"
    echo
    echo "URLs:"
    echo "  Web Interface: http://$EXTERNAL_IP"
    echo "  Health Check:  http://$EXTERNAL_IP:8080/health"
    echo "  Metrics:       http://$EXTERNAL_IP:8080/metrics"
    echo
    echo "SIP Endpoints:"
    echo "  UDP: $EXTERNAL_IP:5060"
    echo "  TCP: $EXTERNAL_IP:5060"
    echo "  TLS: $EXTERNAL_IP:5062"
    echo
    echo "Management Commands:"
    echo "  SSH: gcloud compute ssh $INSTANCE_NAME --zone=$ZONE --project=$PROJECT_ID"
    echo "  Logs: gcloud compute ssh $INSTANCE_NAME --zone=$ZONE --project=$PROJECT_ID --command='journalctl -u siprec-server -f'"
    echo "  Status: gcloud compute ssh $INSTANCE_NAME --zone=$ZONE --project=$PROJECT_ID --command='systemctl status siprec-server'"
    echo
    echo "Testing:"
    echo "  curl http://$EXTERNAL_IP:8080/health"
    echo "  curl http://$EXTERNAL_IP:8080/metrics"
    echo
    echo "Next Steps:"
    echo "1. Configure STT provider credentials"
    echo "2. Set up SSL certificates"
    echo "3. Configure monitoring"
    echo "4. Test SIP functionality"
    echo "============================================"
}

# Main function
main() {
    echo "==========================================="
    echo "SIPREC Server Quick GCP Deployment"
    echo "==========================================="
    
    check_gcloud
    get_project_id
    create_firewall_rules
    create_instance
    wait_for_instance
    get_instance_info
    show_summary
}

# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi