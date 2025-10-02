# Recording Storage Integration

This guide explains how to copy SIPREC audio files to cloud object storage after they are captured locally. The feature is designed for PCI/HIPAA workloads that require encrypted, redundant retention outside of the collector.

## Overview

1. Every call is written to the local `RECORDING_DIR` as before.
2. When the RTP forwarder shuts down, the finished file is uploaded to one or more backends.
3. Depending on configuration, the local file is either retained for troubleshooting or removed after a successful upload.
4. Upload failures are logged and the local copy is left in place for later replay.

> **Important:** Enable `ENABLE_RECORDING_ENCRYPTION=true` so that WAV files are encrypted on disk before any upload takes place. This is required for PCI and HIPAA compliance.

## Quick Start

```env
# Enable remote storage and remove local copies once uploaded
RECORDING_STORAGE_ENABLED=true
RECORDING_STORAGE_KEEP_LOCAL=false
ENABLE_RECORDING_ENCRYPTION=true

# Example: upload to an S3 bucket
RECORDING_STORAGE_S3_ENABLED=true
RECORDING_STORAGE_S3_BUCKET=secure-call-recordings
RECORDING_STORAGE_S3_REGION=us-east-1
RECORDING_STORAGE_S3_ACCESS_KEY=AKIA...
RECORDING_STORAGE_S3_SECRET_KEY=...
RECORDING_STORAGE_S3_PREFIX=prod/
```

Restart the SIPREC server after adjusting the environment variables. Each new recording will be uploaded as soon as the call ends.

## Supported Backends

### Amazon S3

| Variable | Description |
|----------|-------------|
| `RECORDING_STORAGE_S3_ENABLED` | Set to `true` to enable S3 uploads |
| `RECORDING_STORAGE_S3_BUCKET` | Destination bucket |
| `RECORDING_STORAGE_S3_REGION` | AWS region (for example `us-east-1`) |
| `RECORDING_STORAGE_S3_ACCESS_KEY` / `RECORDING_STORAGE_S3_SECRET_KEY` | Credentials with `PutObject` permissions |
| `RECORDING_STORAGE_S3_PREFIX` | Optional folder/prefix applied to each object |

Use an IAM user or role limited to the target bucket. For cross-account buckets, attach the appropriate resource policy.

### Google Cloud Storage

| Variable | Description |
|----------|-------------|
| `RECORDING_STORAGE_GCS_ENABLED` | Enable GCS uploads |
| `RECORDING_STORAGE_GCS_BUCKET` | Target bucket |
| `RECORDING_STORAGE_GCS_SERVICE_ACCOUNT` | Path to or base64 contents of the service account JSON |
| `RECORDING_STORAGE_GCS_PREFIX` | Optional prefix |

Grant the service account the `roles/storage.objectCreator` permission on the bucket.

### Azure Blob Storage

| Variable | Description |
|----------|-------------|
| `RECORDING_STORAGE_AZURE_ENABLED` | Enable Azure uploads |
| `RECORDING_STORAGE_AZURE_ACCOUNT` | Storage account name |
| `RECORDING_STORAGE_AZURE_CONTAINER` | Container name |
| `RECORDING_STORAGE_AZURE_ACCESS_KEY` | Access key with write permissions |
| `RECORDING_STORAGE_AZURE_PREFIX` | Optional virtual folder prefix |

## Operational Recommendations

- **Encryption:** Ensure `ENABLE_RECORDING_ENCRYPTION=true`; combine with key rotation for compliance.
- **Retention:** Configure lifecycle policies directly in the storage backend (for example, S3 object lifecycle rules).
- **Monitoring:** Watch the SIPREC logs for `"Recording persisted to external storage"` messages and alert on any upload failures.
- **Local Copies:** Set `RECORDING_STORAGE_KEEP_LOCAL=true` during migrations so you retain a safety copy until remote uploads are verified.
- **Networking:** Allow outbound HTTPS traffic from the SIPREC host to the target storage endpoints.

## Troubleshooting

| Symptom | Resolution |
|---------|------------|
| Upload fails with authentication errors | Verify credentials and bucket/container names |
| Local files not deleted after upload | Confirm `RECORDING_STORAGE_KEEP_LOCAL=false` and check for upload warnings |
| Uploads succeed but encryption disabled | Set `ENABLE_RECORDING_ENCRYPTION=true` and restart the service |
| Buckets stay empty | Ensure `RECORDING_STORAGE_ENABLED=true` and at least one backend `*_ENABLED=true` |

For detailed encryption guidance, review [security/ENCRYPTION.md](../security/ENCRYPTION.md).
