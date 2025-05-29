# SIPREC Server Linux Deployment Guide

This guide provides multiple methods to deploy the SIPREC server on Google Cloud Platform (GCP) Linux instances.

## Prerequisites

- GCP account with active project
- `gcloud` CLI installed and configured
- Appropriate IAM permissions for Compute Engine
- Source code cloned locally

## Deployment Methods

### Method 1: Quick Deployment Script

The fastest way to deploy SIPREC server on GCP:

```bash
# Make script executable
chmod +x deploy-quick.sh

# Run deployment
./deploy-quick.sh
```

This script will:
- Create firewall rules
- Launch a VM instance with Ubuntu 22.04 LTS
- Automatically install and configure SIPREC server
- Display connection information

### Method 2: Full Deployment Script

For manual deployment on an existing Linux instance:

```bash
# Copy script to your Linux instance
scp deploy_gcp_linux.sh user@your-instance:/tmp/

# SSH to your instance
ssh user@your-instance

# Run deployment as root
sudo /tmp/deploy_gcp_linux.sh
```

### Method 3: Terraform Infrastructure as Code

For production deployments with infrastructure as code:

```bash
# Initialize Terraform
terraform init

# Review deployment plan
terraform plan -var="project_id=YOUR_PROJECT_ID"

# Deploy infrastructure
terraform apply -var="project_id=YOUR_PROJECT_ID"
```

### Method 4: GCP Startup Script

For VM instances created through GCP Console:

1. Copy the contents of `gcp-startup-script.sh`
2. When creating a VM instance, paste the script in the "Automation" → "Startup script" section
3. Or use metadata: `startup-script-url=gs://your-bucket/gcp-startup-script.sh`

## Configuration

### Environment Variables

The deployment creates a configuration file at `/etc/siprec/.env`:

```bash
# Network Configuration - GCP NAT Optimized
BEHIND_NAT=true                    # Automatically detected for GCP
EXTERNAL_IP=auto-detected          # Uses GCP metadata service
INTERNAL_IP=auto-detected          # Uses GCP VPC private IP
PORTS=5060,5061
RTP_PORT_MIN=16384
RTP_PORT_MAX=32768

# STUN Configuration for NAT Traversal
STUN_SERVER=stun:stun.l.google.com:19302,stun:stun1.l.google.com:19302

# HTTP Configuration
HTTP_ENABLED=true
HTTP_PORT=8080

# Recording Configuration
RECORDING_DIR=/var/lib/siprec/recordings

# STT Configuration (configure your preferred provider)
STT_DEFAULT_VENDOR=mock
GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

### NAT Configuration

The deployment automatically configures NAT settings for GCP:

- **Auto-Detection**: Uses GCP metadata service to detect external/internal IPs
- **NAT Mode**: Automatically enables `BEHIND_NAT=true` when external ≠ internal IP
- **STUN Servers**: Configures Google STUN servers for NAT traversal
- **SDP Handling**: Uses external IP in SDP responses for proper media routing

### Firewall Rules

The deployment automatically configures these ports:

- **22/tcp**: SSH
- **80/tcp**: HTTP (Nginx proxy)
- **443/tcp**: HTTPS (Nginx proxy)
- **5060/udp,tcp**: SIP signaling
- **5061/udp,tcp**: SIP signaling (alt)
- **5062/tcp**: SIP TLS
- **8080/tcp**: SIPREC API
- **16384-32768/udp**: RTP media

## Post-Deployment Setup

### 1. Configure STT Provider

For Google Speech-to-Text:

```bash
# Create service account and download key
gcloud iam service-accounts create siprec-stt
gcloud iam service-accounts keys create /etc/siprec/google-stt.json \
    --iam-account siprec-stt@YOUR_PROJECT.iam.gserviceaccount.com

# Grant permissions
gcloud projects add-iam-policy-binding YOUR_PROJECT \
    --member="serviceAccount:siprec-stt@YOUR_PROJECT.iam.gserviceaccount.com" \
    --role="roles/speech.editor"

# Update configuration
echo "GOOGLE_APPLICATION_CREDENTIALS=/etc/siprec/google-stt.json" >> /etc/siprec/.env
echo "STT_DEFAULT_VENDOR=google" >> /etc/siprec/.env

# Restart service
systemctl restart siprec-server
```

### 2. SSL Certificate Setup

```bash
# Install certificate for domain
certbot --nginx -d your-domain.com

# Auto-renewal is configured via cron
```

### 3. Monitoring Setup

Health checks and monitoring are automatically configured:

```bash
# Check service status
systemctl status siprec-server

# View logs
journalctl -u siprec-server -f

# Health check
curl http://localhost:8080/health

# Metrics
curl http://localhost:8080/metrics
```

## Service Management

### SystemD Commands

```bash
# Start service
systemctl start siprec-server

# Stop service
systemctl stop siprec-server

# Restart service
systemctl restart siprec-server

# Enable auto-start
systemctl enable siprec-server

# View status
systemctl status siprec-server

# View logs
journalctl -u siprec-server -f
```

### Configuration Files

- **Service**: `/etc/systemd/system/siprec-server.service`
- **Environment**: `/etc/siprec/.env`
- **Nginx**: `/etc/nginx/sites-available/siprec`
- **Logs**: `/var/log/siprec/`
- **Data**: `/var/lib/siprec/`
- **Binary**: `/opt/siprec/bin/siprec`

## Testing the Deployment

### Basic Health Check

```bash
# Test API health
curl http://YOUR_EXTERNAL_IP:8080/health

# Test SIP OPTIONS
echo -e "OPTIONS sip:test@YOUR_EXTERNAL_IP SIP/2.0\r\nVia: SIP/2.0/UDP test:5070;branch=z9hG4bK-test\r\nFrom: sip:test@test;tag=test\r\nTo: sip:test@YOUR_EXTERNAL_IP\r\nCall-ID: test\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n" | nc -u YOUR_EXTERNAL_IP 5060
```

### Load Testing

```bash
# Install SIPp for load testing
apt-get install sipp

# Run basic SIP test
sipp -sn uac YOUR_EXTERNAL_IP:5060 -m 10 -r 1
```

## Troubleshooting

### Common Issues

1. **Service won't start**:
   ```bash
   journalctl -u siprec-server -n 50
   ```

2. **Port binding issues**:
   ```bash
   netstat -tulpn | grep 5060
   ```

3. **Firewall issues**:
   ```bash
   # Ubuntu/Debian
   ufw status
   
   # CentOS/RHEL
   firewall-cmd --list-all
   ```

4. **Configuration issues**:
   ```bash
   /opt/siprec/bin/siprec envcheck
   ```

5. **NAT Configuration Issues**:
   ```bash
   # Test NAT configuration
   ./test_nat_config.sh
   
   # Check IP detection
   curl -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/external-ip
   curl -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip
   
   # Verify SIP response contains external IP
   echo -e "OPTIONS sip:test@localhost SIP/2.0\r\nVia: SIP/2.0/UDP test:5070;branch=z9hG4bK-test\r\nFrom: sip:test@test;tag=test\r\nTo: sip:test@localhost\r\nCall-ID: test\r\nCSeq: 1 OPTIONS\r\nContent-Length: 0\r\n\r\n" | nc -u localhost 5060
   ```

6. **RTP Media Issues**:
   ```bash
   # Check RTP port range
   netstat -ulpn | grep -E "1638[4-9]|163[9-9][0-9]|16[4-9][0-9][0-9]"
   
   # Test media connectivity
   # (External testing required with SIP client)
   ```

### Log Files

- **Application**: `journalctl -u siprec-server`
- **Nginx**: `/var/log/nginx/access.log`, `/var/log/nginx/error.log`
- **System**: `/var/log/syslog`
- **SIPREC**: `/var/log/siprec/`

## Security Considerations

### Firewall Configuration

- Only open necessary ports
- Consider IP whitelisting for API access
- Use VPN for management access

### SSL/TLS Configuration

- Use strong ciphers
- Enable HSTS
- Regular certificate renewal

### System Hardening

- Regular security updates
- Fail2ban for SSH protection
- Log monitoring and alerting

## Backup and Recovery

### Automated Backups

Backup script runs daily via cron:

```bash
# Manual backup
/opt/siprec/bin/backup.sh

# Restore from backup
tar -xzf /opt/backups/siprec/config_YYYYMMDD_HHMMSS.tar.gz -C /
```

### Data Backup

Important directories to backup:
- `/etc/siprec/` - Configuration
- `/var/lib/siprec/keys/` - Encryption keys
- `/var/lib/siprec/recordings/` - Audio recordings (if local storage)

## Performance Tuning

### System Limits

```bash
# Increase file descriptor limits
echo "siprec soft nofile 65536" >> /etc/security/limits.conf
echo "siprec hard nofile 65536" >> /etc/security/limits.conf
```

### Network Optimization

```bash
# Optimize for RTP traffic
echo 'net.core.rmem_max = 268435456' >> /etc/sysctl.conf
echo 'net.core.wmem_max = 268435456' >> /etc/sysctl.conf
sysctl -p
```

## Support

For deployment issues:

1. Check logs: `journalctl -u siprec-server -f`
2. Verify configuration: `/opt/siprec/bin/siprec envcheck`
3. Test connectivity: Network and firewall checks
4. Review documentation and GitHub issues

## Updates

To update the SIPREC server:

```bash
# Stop service
systemctl stop siprec-server

# Backup current installation
/opt/siprec/bin/backup.sh

# Pull latest code and rebuild
cd /tmp/siprec-source
git pull
./deploy_gcp_linux.sh

# Start service
systemctl start siprec-server
```