# Production Deployment Guide

## Table of Contents
- [Prerequisites](#prerequisites)
- [Deployment Options](#deployment-options)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Bare Metal Installation](#bare-metal-installation)
- [High Availability Setup](#high-availability-setup)
- [Database Setup](#database-setup)
- [Cloud Storage Configuration](#cloud-storage-configuration)
- [Security Hardening](#security-hardening)
- [Monitoring & Observability](#monitoring--observability)
- [Performance Tuning](#performance-tuning)
- [Backup & Recovery](#backup--recovery)
- [Troubleshooting](#troubleshooting)

## Prerequisites

### System Requirements

**Minimum Requirements:**
- OS: Ubuntu 20.04+ / Debian 11+ / RHEL 8+ / CentOS 8+
- CPU: 4 cores
- RAM: 8 GB
- Storage: 100 GB SSD
- Network: 100 Mbps

**Recommended Production Requirements:**
- CPU: 8+ cores
- RAM: 16-32 GB
- Storage: 500 GB+ SSD (NVMe preferred)
- Network: 1 Gbps
- Database: MySQL 8.0+ or MariaDB 10.5+ (optional)
- Elasticsearch: 7.10+ cluster (optional)

### Network Requirements

Open the following ports:

```bash
# Firewall configuration
sudo ufw allow 5060/tcp  # SIP TCP
sudo ufw allow 5060/udp  # SIP UDP
sudo ufw allow 5062/tcp  # SIP TLS
sudo ufw allow 16384:32768/udp  # RTP media range
sudo ufw allow 8080/tcp  # HTTP API (restrict to internal network)
sudo ufw allow 9090/tcp  # Prometheus metrics (internal only)
```

## Deployment Options

### Option 1: Docker (Recommended for Single Instance)

```bash
# Pull the latest image
docker pull ghcr.io/loreste/siprec:latest

# Run with production configuration
docker run -d \
  --name siprec-server \
  --restart unless-stopped \
  --network host \
  -v /opt/siprec/recordings:/var/lib/siprec/recordings \
  -v /opt/siprec/config:/etc/siprec \
  -v /opt/siprec/logs:/var/log/siprec \
  -e DATABASE_ENABLED=true \
  -e DB_HOST=mysql.internal \
  -e ANALYTICS_ENABLED=true \
  -e COMPLIANCE_PCI_ENABLED=true \
  ghcr.io/loreste/siprec:latest
```

### Option 2: Kubernetes (Recommended for HA)

```yaml
# siprec-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: siprec-server
  namespace: voip
spec:
  replicas: 3
  selector:
    matchLabels:
      app: siprec
  template:
    metadata:
      labels:
        app: siprec
    spec:
      containers:
      - name: siprec
        image: ghcr.io/loreste/siprec:latest
        resources:
          requests:
            memory: "4Gi"
            cpu: "2"
          limits:
            memory: "8Gi"
            cpu: "4"
        env:
        - name: DATABASE_ENABLED
          value: "true"
        - name: DB_HOST
          valueFrom:
            secretKeyRef:
              name: siprec-db
              key: host
        volumeMounts:
        - name: recordings
          mountPath: /var/lib/siprec/recordings
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
      volumes:
      - name: recordings
        persistentVolumeClaim:
          claimName: siprec-recordings-pvc
```

### Option 3: Bare Metal Installation

```bash
# Install dependencies
sudo apt update
sudo apt install -y git golang-1.21 make mysql-client

# Clone and build
git clone https://github.com/loreste/siprec.git
cd siprec

# Build with MySQL support
make build-mysql

# Install systemd service
sudo cp siprec /usr/local/bin/
sudo cp deploy/siprec.service /etc/systemd/system/
sudo systemctl enable siprec
sudo systemctl start siprec
```

## High Availability Setup

### Active-Active Configuration with Load Balancer

```nginx
# /etc/nginx/sites-available/siprec-lb
upstream siprec_sip_tcp {
    least_conn;
    server siprec-1.internal:5060 max_fails=3 fail_timeout=30s;
    server siprec-2.internal:5060 max_fails=3 fail_timeout=30s;
    server siprec-3.internal:5060 max_fails=3 fail_timeout=30s;
}

upstream siprec_api {
    ip_hash;
    server siprec-1.internal:8080;
    server siprec-2.internal:8080;
    server siprec-3.internal:8080;
}

server {
    listen 5060;
    proxy_pass siprec_sip_tcp;
    proxy_protocol on;
    proxy_timeout 300s;
}

server {
    listen 8080;
    location / {
        proxy_pass http://siprec_api;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### Database Replication Setup

```sql
-- On primary MySQL server
CREATE USER 'replication'@'%' IDENTIFIED BY 'secure_password';
GRANT REPLICATION SLAVE ON *.* TO 'replication'@'%';

-- Get binary log position
SHOW MASTER STATUS;

-- On replica MySQL server
CHANGE MASTER TO
    MASTER_HOST='mysql-primary.internal',
    MASTER_USER='replication',
    MASTER_PASSWORD='secure_password',
    MASTER_LOG_FILE='mysql-bin.000001',
    MASTER_LOG_POS=154;

START SLAVE;
SHOW SLAVE STATUS\G
```

### Session State with Redis

```bash
# Redis cluster setup
redis-cli --cluster create \
  redis-1:6379 redis-2:6379 redis-3:6379 \
  redis-4:6379 redis-5:6379 redis-6:6379 \
  --cluster-replicas 1

# Configure SIPREC
export REDUNDANCY_STORAGE_TYPE=redis
export REDIS_URL=redis://redis-cluster:6379
export REDIS_POOL_SIZE=100
```

## Database Setup

### MySQL Installation and Configuration

```bash
# Install MySQL 8.0
sudo apt install mysql-server-8.0

# Secure installation
sudo mysql_secure_installation

# Create database and user
mysql -u root -p <<EOF
CREATE DATABASE siprec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'siprec'@'%' IDENTIFIED BY 'secure_password';
GRANT ALL PRIVILEGES ON siprec.* TO 'siprec'@'%';
FLUSH PRIVILEGES;
EOF

# Optimize for SIPREC workload
cat >> /etc/mysql/mysql.conf.d/siprec.cnf <<EOF
[mysqld]
# InnoDB optimizations
innodb_buffer_pool_size = 4G
innodb_log_file_size = 512M
innodb_flush_method = O_DIRECT
innodb_flush_log_at_trx_commit = 2

# Connection pool
max_connections = 500
max_connect_errors = 1000000

# Full-text search
innodb_ft_min_token_size = 2
innodb_ft_enable_stopword = OFF

# Performance schema
performance_schema = ON
EOF

sudo systemctl restart mysql
```

### Database Migrations

```bash
# Run migrations
./siprec migrate up

# Verify tables
mysql -u siprec -p siprec -e "SHOW TABLES;"
```

## Cloud Storage Configuration

### AWS S3 Setup

```bash
# Create S3 bucket
aws s3 mb s3://siprec-recordings --region us-east-1

# Configure bucket policy
aws s3api put-bucket-lifecycle-configuration \
  --bucket siprec-recordings \
  --lifecycle-configuration file://s3-lifecycle.json

# IAM policy for SIPREC
aws iam create-policy --policy-name SIPRECStoragePolicy \
  --policy-document file://iam-policy.json

# Configure SIPREC
export RECORDING_STORAGE_S3_ENABLED=true
export RECORDING_STORAGE_S3_BUCKET=siprec-recordings
export RECORDING_STORAGE_S3_REGION=us-east-1
export AWS_ACCESS_KEY_ID=your-access-key-id
export AWS_SECRET_ACCESS_KEY=your-secret-access-key
```

### Google Cloud Storage Setup

```bash
# Create bucket
gsutil mb -c STANDARD -l us-central1 gs://siprec-recordings

# Set lifecycle policy
gsutil lifecycle set gcs-lifecycle.json gs://siprec-recordings

# Create service account
gcloud iam service-accounts create siprec-storage \
  --display-name="SIPREC Storage Service Account"

# Grant permissions
gsutil iam ch serviceAccount:siprec-storage@project.iam.gserviceaccount.com:objectAdmin \
  gs://siprec-recordings

# Configure SIPREC
export RECORDING_STORAGE_GCS_ENABLED=true
export RECORDING_STORAGE_GCS_BUCKET=siprec-recordings
export GOOGLE_APPLICATION_CREDENTIALS=/path/to/service-account.json
```

### Azure Blob Storage Setup

```bash
# Create storage account
az storage account create \
  --name siprecrecordings \
  --resource-group siprec-rg \
  --location eastus \
  --sku Standard_LRS

# Create container
az storage container create \
  --name recordings \
  --account-name siprecrecordings

# Get connection string
az storage account show-connection-string \
  --name siprecrecordings \
  --resource-group siprec-rg

# Configure SIPREC
export RECORDING_STORAGE_AZURE_ENABLED=true
export RECORDING_STORAGE_AZURE_ACCOUNT_NAME=siprecrecordings
export RECORDING_STORAGE_AZURE_ACCOUNT_KEY=your-key
export RECORDING_STORAGE_AZURE_CONTAINER=recordings
```

## Security Hardening

### PCI DSS Compliance Mode

```bash
# Enable PCI compliance (automatically configures security settings)
export COMPLIANCE_PCI_ENABLED=true

# This automatically enables:
# - TLS-only SIP listeners
# - SRTP mandatory for media
# - Recording encryption
# - Audit logging
# - Session timeout enforcement
```

### TLS Certificate Setup

```bash
# Generate certificates with Let's Encrypt
sudo certbot certonly --standalone -d siprec.example.com

# Configure SIPREC
export ENABLE_TLS=true
export TLS_CERT_PATH=/etc/letsencrypt/live/siprec.example.com/fullchain.pem
export TLS_KEY_PATH=/etc/letsencrypt/live/siprec.example.com/privkey.pem
export SIP_REQUIRE_TLS=true

# Auto-renewal
echo "0 0 * * 0 root certbot renew --quiet --post-hook 'systemctl restart siprec'" \
  >> /etc/crontab
```

### GDPR Compliance

```bash
# Enable GDPR features
export COMPLIANCE_GDPR_ENABLED=true
export COMPLIANCE_GDPR_EXPORT_DIR=/var/lib/siprec/gdpr-exports

# Data retention policy
export RECORDING_CLEANUP_DAYS=90
export DATABASE_RETENTION_DAYS=365

# PII detection and redaction
export PII_DETECTION_ENABLED=true
export PII_ENABLED_TYPES=ssn,credit_card,phone,email
export PII_REDACTION_CHAR=*
```

### Audit Logging

```bash
# Enable tamper-proof audit logging
export COMPLIANCE_AUDIT_ENABLED=true
export COMPLIANCE_AUDIT_TAMPER_PROOF=true
export COMPLIANCE_AUDIT_LOG_PATH=/var/log/siprec/audit.log

# Configure log rotation
cat > /etc/logrotate.d/siprec-audit <<EOF
/var/log/siprec/audit.log {
    daily
    rotate 365
    compress
    notifempty
    create 0600 siprec siprec
    sharedscripts
    postrotate
        systemctl reload siprec
    endscript
}
EOF
```

## Monitoring & Observability

### Prometheus Setup

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'siprec'
    static_configs:
      - targets: 
        - 'siprec-1:8080'
        - 'siprec-2:8080'
        - 'siprec-3:8080'
    metrics_path: '/metrics'
    scrape_interval: 5s
```

### Key Metrics to Monitor

```promql
# Active calls
sum(siprec_active_calls)

# Call rate
rate(siprec_sessions_total[5m])

# Error rate
rate(siprec_transcription_errors_total[5m]) / rate(siprec_transcription_requests_total[5m])

# Audio quality (MOS score)
histogram_quantile(0.95, rate(siprec_audio_mos_score_bucket[5m]))

# Provider health
siprec_provider_health_score

# Database latency
histogram_quantile(0.99, rate(siprec_database_query_duration_seconds_bucket[5m]))

# Storage upload success rate
rate(siprec_storage_upload_success_total[5m]) / rate(siprec_storage_upload_attempts_total[5m])
```

### Grafana Dashboard

Import the dashboard from `monitoring/grafana-dashboard.json`:

```bash
# Import via API
curl -X POST http://admin:admin@grafana:3000/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @monitoring/grafana-dashboard.json
```

### Elasticsearch Analytics

```bash
# Configure Elasticsearch cluster
export ANALYTICS_ENABLED=true
export ANALYTICS_ELASTICSEARCH_ADDRESSES=http://es-1:9200,http://es-2:9200,http://es-3:9200
export ANALYTICS_ELASTICSEARCH_INDEX=call-analytics
export ANALYTICS_ELASTICSEARCH_USERNAME=elastic
export ANALYTICS_ELASTICSEARCH_PASSWORD=secure_password

# Create index template
curl -X PUT "localhost:9200/_index_template/call-analytics" \
  -H "Content-Type: application/json" \
  -d @elasticsearch/index-template.json
```

### Application Logs

```bash
# Configure structured logging
export LOG_LEVEL=info
export LOG_FORMAT=json

# Ship logs to ELK stack
cat > /etc/filebeat/filebeat.yml <<EOF
filebeat.inputs:
- type: log
  enabled: true
  paths:
    - /var/log/siprec/*.log
  json.keys_under_root: true
  json.add_error_key: true

output.elasticsearch:
  hosts: ["elasticsearch:9200"]
  index: "siprec-logs-%{+yyyy.MM.dd}"
EOF
```

## Performance Tuning

### System Optimization

```bash
# Kernel parameters for high-performance networking
cat >> /etc/sysctl.conf <<EOF
# Network performance
net.core.rmem_max = 134217728
net.core.wmem_max = 134217728
net.ipv4.tcp_rmem = 4096 87380 134217728
net.ipv4.tcp_wmem = 4096 65536 134217728
net.core.netdev_max_backlog = 30000
net.ipv4.tcp_congestion_control = bbr

# UDP buffers for RTP
net.core.rmem_default = 26214400
net.core.wmem_default = 26214400

# Connection tracking
net.netfilter.nf_conntrack_max = 2000000
net.nf_conntrack_max = 2000000

# File descriptors
fs.file-max = 2097152
fs.nr_open = 2097152
EOF

sysctl -p
```

### Application Tuning

```bash
# SIPREC performance settings
export MAX_CONCURRENT_CALLS=1000
export WORKER_POOL_SIZE=100
export BUFFER_SIZE=65536
export RTP_BUFFER_SIZE=2048

# Go runtime tuning
export GOGC=100
export GOMEMLIMIT=8GiB
export GOMAXPROCS=8

# Connection pooling
export DB_MAX_CONNECTIONS=100
export DB_MAX_IDLE_CONNECTIONS=25
export AMQP_POOL_SIZE=50
export REDIS_POOL_SIZE=100
```

### RTP Optimization

```bash
# RTP port range optimization
export RTP_PORT_MIN=16384
export RTP_PORT_MAX=32768

# Enable RTP multiplexing
export RTP_MULTIPLEXING_ENABLED=true
export RTP_MULTIPLEXING_WINDOW=100ms

# Jitter buffer configuration
export RTP_JITTER_BUFFER_SIZE=200ms
export RTP_JITTER_BUFFER_ADAPTIVE=true
```

## Backup & Recovery

### Database Backup

```bash
#!/bin/bash
# backup-database.sh
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/backup/mysql"

# Backup with compression
mysqldump --single-transaction --routines --triggers \
  -h mysql.internal -u siprec -p \
  siprec | gzip > ${BACKUP_DIR}/siprec_${DATE}.sql.gz

# Upload to S3
aws s3 cp ${BACKUP_DIR}/siprec_${DATE}.sql.gz \
  s3://backup-bucket/mysql/

# Cleanup old backups
find ${BACKUP_DIR} -name "*.sql.gz" -mtime +30 -delete
```

### Recording Backup

```bash
#!/bin/bash
# backup-recordings.sh
DATE=$(date +%Y%m%d)
RECORDING_DIR="/var/lib/siprec/recordings"

# Sync to cloud storage
rclone sync ${RECORDING_DIR} s3:backup-bucket/recordings/ \
  --transfers 10 \
  --checkers 10 \
  --max-age 24h

# Archive old recordings
find ${RECORDING_DIR} -name "*.wav" -mtime +90 | \
  tar czf /backup/recordings_${DATE}.tar.gz -T -
```

### Disaster Recovery

```bash
#!/bin/bash
# disaster-recovery.sh

# Restore database
gunzip < /backup/mysql/siprec_latest.sql.gz | \
  mysql -h mysql.internal -u siprec -p siprec

# Restore recordings
rclone sync s3:backup-bucket/recordings/ \
  /var/lib/siprec/recordings/

# Verify integrity
./siprec verify --check-database --check-recordings
```

## Troubleshooting

### Common Issues

#### High Memory Usage
```bash
# Check memory usage
docker stats siprec-server

# Analyze heap profile
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Tune garbage collector
export GOGC=50  # More aggressive GC
```

#### Database Connection Issues
```bash
# Check connection pool
mysql -u siprec -p -e "SHOW PROCESSLIST;"

# Verify connectivity
nc -zv mysql.internal 3306

# Check slow queries
mysql -u siprec -p -e "SELECT * FROM mysql.slow_log;"
```

#### Audio Quality Problems
```bash
# Check packet loss
tcpdump -i any -n port 5060 or portrange 16384-32768 -w capture.pcap

# Analyze RTP stream
./siprec analyze-rtp --pcap capture.pcap --call-id abc123

# Check jitter buffer stats
curl http://localhost:8080/api/stats/audio
```

#### STT Provider Failures
```bash
# Check provider health
curl http://localhost:8080/api/providers/health

# View circuit breaker status
curl http://localhost:8080/api/providers/circuit-breakers

# Force provider rotation
curl -X POST http://localhost:8080/api/providers/rotate
```

### Debug Mode

```bash
# Enable debug logging
export LOG_LEVEL=debug
export DEBUG_MODE=true

# Enable pprof profiling
export ENABLE_PPROF=true

# Trace specific calls
export TRACE_CALL_IDS=call-123,call-456

# Restart service
systemctl restart siprec
```

### Performance Analysis

```bash
# CPU profiling
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof -http=:8081 cpu.prof

# Memory profiling
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof -http=:8082 heap.prof

# Trace analysis
curl http://localhost:8080/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```

## Support

For production support and enterprise features, contact the maintainers through GitHub Issues or the official support channels.