# Backup & Recovery

Comprehensive backup and disaster recovery procedures for SIPREC Server.

## Backup Strategy

### What to Backup

1. **Configuration Files**
   - Application configuration
   - TLS certificates and keys
   - Encryption keys
   - Environment files

2. **Data**
   - Session metadata
   - Transcription results
   - Audit logs
   - Performance metrics

3. **State Information**
   - Active sessions (for warm recovery)
   - Cache data
   - Persistent queues

### Backup Types

#### Configuration Backup

```bash
#!/bin/bash
# backup-config.sh

BACKUP_DIR="/backup/siprec/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# Application config
tar -czf "$BACKUP_DIR/config.tar.gz" /etc/siprec/

# Certificates
tar -czf "$BACKUP_DIR/certs.tar.gz" /etc/siprec/tls/

# Encryption keys
tar -czf "$BACKUP_DIR/keys.tar.gz" /etc/siprec/keys/

# Environment files
cp /etc/siprec/.env* "$BACKUP_DIR/"

echo "Configuration backup completed: $BACKUP_DIR"
```

#### Data Backup

```bash
#!/bin/bash
# backup-data.sh

BACKUP_DIR="/backup/siprec/$(date +%Y%m%d)"
mkdir -p "$BACKUP_DIR"

# Database dump (if using database)
pg_dump siprec > "$BACKUP_DIR/database.sql"

# Log files
tar -czf "$BACKUP_DIR/logs.tar.gz" /var/log/siprec/

# Transcription data
tar -czf "$BACKUP_DIR/transcriptions.tar.gz" /var/lib/siprec/transcriptions/

echo "Data backup completed: $BACKUP_DIR"
```

## Automated Backup

### Cron Schedule

```bash
# /etc/cron.d/siprec-backup

# Daily config backup at 2 AM
0 2 * * * siprec /opt/siprec/scripts/backup-config.sh

# Daily data backup at 3 AM
0 3 * * * siprec /opt/siprec/scripts/backup-data.sh

# Weekly full backup on Sunday at 1 AM
0 1 * * 0 siprec /opt/siprec/scripts/backup-full.sh
```

### Systemd Timer

```ini
# /etc/systemd/system/siprec-backup.timer
[Unit]
Description=SIPREC Backup Timer
Requires=siprec-backup.service

[Timer]
OnCalendar=daily
Persistent=true

[Install]
WantedBy=timers.target
```

```ini
# /etc/systemd/system/siprec-backup.service
[Unit]
Description=SIPREC Backup Service
Type=oneshot

[Service]
User=siprec
ExecStart=/opt/siprec/scripts/backup-full.sh
```

## Cloud Backup

### AWS S3

```bash
#!/bin/bash
# backup-to-s3.sh

BUCKET="siprec-backups"
DATE=$(date +%Y%m%d)
LOCAL_BACKUP="/backup/siprec/$DATE"

# Upload to S3
aws s3 sync "$LOCAL_BACKUP" "s3://$BUCKET/$DATE/" \
  --storage-class STANDARD_IA \
  --server-side-encryption AES256

# Set lifecycle policy for old backups
aws s3api put-bucket-lifecycle-configuration \
  --bucket "$BUCKET" \
  --lifecycle-configuration file://lifecycle.json
```

### Lifecycle Policy

```json
{
  "Rules": [
    {
      "ID": "SiprecBackupLifecycle",
      "Status": "Enabled",
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "GLACIER"
        },
        {
          "Days": 365,
          "StorageClass": "DEEP_ARCHIVE"
        }
      ],
      "Expiration": {
        "Days": 2555
      }
    }
  ]
}
```

### Google Cloud Storage

```bash
#!/bin/bash
# backup-to-gcs.sh

BUCKET="siprec-backups"
DATE=$(date +%Y%m%d)
LOCAL_BACKUP="/backup/siprec/$DATE"

# Upload to GCS
gsutil -m rsync -r -d "$LOCAL_BACKUP" "gs://$BUCKET/$DATE/"

# Set retention policy
gsutil lifecycle set lifecycle.json "gs://$BUCKET"
```

## Recovery Procedures

### Quick Recovery

For minor issues or configuration rollback:

```bash
#!/bin/bash
# quick-restore.sh

BACKUP_DATE="$1"
BACKUP_DIR="/backup/siprec/$BACKUP_DATE"

# Stop service
systemctl stop siprec

# Restore configuration
tar -xzf "$BACKUP_DIR/config.tar.gz" -C /

# Restore certificates
tar -xzf "$BACKUP_DIR/certs.tar.gz" -C /

# Restore environment
cp "$BACKUP_DIR"/.env* /etc/siprec/

# Start service
systemctl start siprec

echo "Quick recovery completed from $BACKUP_DATE"
```

### Full Recovery

For complete system failure:

```bash
#!/bin/bash
# full-restore.sh

BACKUP_DATE="$1"
BACKUP_DIR="/backup/siprec/$BACKUP_DATE"

# Install SIPREC if needed
if ! command -v siprec &> /dev/null; then
    /opt/siprec/install_siprec_linux.sh
fi

# Restore all data
tar -xzf "$BACKUP_DIR/config.tar.gz" -C /
tar -xzf "$BACKUP_DIR/certs.tar.gz" -C /
tar -xzf "$BACKUP_DIR/keys.tar.gz" -C /
tar -xzf "$BACKUP_DIR/logs.tar.gz" -C /

# Restore database
if [ -f "$BACKUP_DIR/database.sql" ]; then
    psql siprec < "$BACKUP_DIR/database.sql"
fi

# Set permissions
chown -R siprec:siprec /etc/siprec
chown -R siprec:siprec /var/lib/siprec
chown -R siprec:siprec /var/log/siprec

# Start services
systemctl enable siprec
systemctl start siprec

echo "Full recovery completed from $BACKUP_DATE"
```

## Disaster Recovery

### Multi-Site Setup

```yaml
# Primary Site (Site A)
primary:
  location: "us-east-1"
  siprec_instances:
    - ip: "10.1.1.10"
    - ip: "10.1.1.11"
  backup_schedule: "daily"
  replication_target: "us-west-2"

# Secondary Site (Site B)
secondary:
  location: "us-west-2"
  siprec_instances:
    - ip: "10.2.1.10"
    - ip: "10.2.1.11"
  backup_schedule: "hourly"
  standby_mode: true
```

### Failover Procedure

```bash
#!/bin/bash
# failover.sh

# 1. Stop primary site
echo "Stopping primary site..."
ssh primary-lb "systemctl stop nginx"

# 2. Promote secondary
echo "Promoting secondary site..."
ssh secondary-1 "systemctl start siprec"
ssh secondary-2 "systemctl start siprec"

# 3. Update DNS
echo "Updating DNS..."
aws route53 change-resource-record-sets \
  --hosted-zone-id Z123456789 \
  --change-batch file://failover-dns.json

# 4. Verify
echo "Verifying failover..."
curl http://siprec.example.com/health

echo "Failover completed"
```

## Testing Backup & Recovery

### Backup Verification

```bash
#!/bin/bash
# verify-backup.sh

BACKUP_DATE="$1"
BACKUP_DIR="/backup/siprec/$BACKUP_DATE"

echo "Verifying backup for $BACKUP_DATE..."

# Check file integrity
if [ -f "$BACKUP_DIR/config.tar.gz" ]; then
    tar -tzf "$BACKUP_DIR/config.tar.gz" > /dev/null
    echo "✓ Config backup valid"
else
    echo "✗ Config backup missing"
fi

# Check encryption
if [ -f "$BACKUP_DIR/keys.tar.gz" ]; then
    tar -tzf "$BACKUP_DIR/keys.tar.gz" > /dev/null
    echo "✓ Keys backup valid"
else
    echo "✗ Keys backup missing"
fi

# Check database
if [ -f "$BACKUP_DIR/database.sql" ]; then
    head -n 1 "$BACKUP_DIR/database.sql" | grep -q "PostgreSQL"
    echo "✓ Database backup valid"
else
    echo "✗ Database backup missing"
fi
```

### Recovery Testing

```bash
#!/bin/bash
# test-recovery.sh

# Create test environment
docker run -d --name siprec-test \
  -p 15060:5060 \
  -p 18080:8080 \
  siprec:latest

# Wait for startup
sleep 10

# Test basic functionality
curl http://localhost:18080/health

# Simulate failure and recovery
docker stop siprec-test
./restore-to-container.sh latest
docker start siprec-test

# Verify recovery
sleep 5
curl http://localhost:18080/health

# Cleanup
docker rm -f siprec-test
```

## Monitoring Backup Status

### Backup Metrics

```bash
# Add to cron job
#!/bin/bash
# backup-metrics.sh

BACKUP_SUCCESS=1
BACKUP_SIZE=$(du -sb /backup/siprec/$(date +%Y%m%d) | cut -f1)
BACKUP_TIME=$(date +%s)

# Send metrics to monitoring
curl -X POST http://prometheus-pushgateway:9091/metrics/job/siprec-backup \
  -H "Content-Type: text/plain" \
  --data "backup_success $BACKUP_SUCCESS
backup_size_bytes $BACKUP_SIZE
backup_timestamp $BACKUP_TIME"
```

### Backup Alerts

```yaml
# Prometheus alert rules
- alert: BackupFailed
  expr: backup_success == 0
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "SIPREC backup failed"

- alert: BackupOld
  expr: time() - backup_timestamp > 86400
  for: 1m
  labels:
    severity: warning
  annotations:
    summary: "SIPREC backup is over 24 hours old"
```

## Best Practices

1. **Backup Frequency**
   - Critical data: Every 6 hours
   - Configuration: Daily
   - Full backup: Weekly

2. **Retention Policy**
   - Daily backups: 30 days
   - Weekly backups: 12 weeks
   - Monthly backups: 12 months

3. **Security**
   - Encrypt backups at rest
   - Secure backup transmission
   - Test recovery regularly

4. **Automation**
   - Automate all backup processes
   - Monitor backup success
   - Alert on failures

5. **Documentation**
   - Document all procedures
   - Keep runbooks updated
   - Train operations team