# SIPREC Server Encryption Documentation

This document describes the comprehensive end-to-end encryption capabilities of the SIPREC server, providing secure protection for both audio recordings and session metadata.

## Overview

The SIPREC server supports optional end-to-end encryption for:

- **Audio Recordings**: Encrypt recorded RTP audio streams with industry-standard algorithms
- **Session Metadata**: Encrypt SIPREC session metadata and participant information
- **Key Management**: Automated key generation, rotation, and secure storage
- **Multiple Algorithms**: Support for AES-256-GCM and ChaCha20-Poly1305

## Features

### 🔐 Strong Encryption
- **AES-256-GCM**: Advanced Encryption Standard with Galois/Counter Mode
- **ChaCha20-Poly1305**: Modern authenticated encryption algorithm
- **PBKDF2 Key Derivation**: Secure key derivation from master passwords
- **Random Nonce Generation**: Cryptographically secure random nonce for each encryption operation

### 🔑 Advanced Key Management
- **Automatic Key Generation**: Generate new encryption keys as needed
- **Key Rotation**: Configurable automatic key rotation intervals
- **Key Versioning**: Support for multiple key versions with seamless transitions
- **Secure Storage**: File-based or memory-based key storage with obfuscation

### 📁 Flexible Storage Options
- **File-based Key Store**: Persistent storage with separate metadata and key data files
- **Memory-based Key Store**: Volatile storage for development and testing
- **Encrypted Backups**: Automatic backup of encryption keys with master password protection

### 🛡️ Security Features
- **Authentication**: Built-in authentication tags prevent tampering
- **Session Isolation**: Each session uses independent encryption contexts
- **Forward Secrecy**: Key rotation provides forward secrecy properties
- **Audit Logging**: Comprehensive logging of encryption operations

## Configuration

### Environment Variables

Enable encryption by setting these environment variables:

```bash
# Enable/disable encryption
ENABLE_RECORDING_ENCRYPTION=true
ENABLE_METADATA_ENCRYPTION=true

# Algorithm selection
ENCRYPTION_ALGORITHM=AES-256-GCM          # or ChaCha20-Poly1305
KEY_DERIVATION_METHOD=PBKDF2              # Key derivation method

# Key management
MASTER_KEY_PATH=./keys                    # Directory for key storage
KEY_ROTATION_INTERVAL=24h                 # Automatic rotation interval
KEY_BACKUP_ENABLED=true                   # Enable key backups

# Security parameters
ENCRYPTION_KEY_SIZE=32                    # Key size in bytes (256 bits)
ENCRYPTION_NONCE_SIZE=12                  # Nonce size for GCM
ENCRYPTION_SALT_SIZE=32                   # Salt size for key derivation
PBKDF2_ITERATIONS=100000                  # PBKDF2 iteration count

# Storage options
ENCRYPTION_KEY_STORE=memory               # memory (default), file, or vault
```

### Configuration Examples

#### Production Setup with Maximum Security
```bash
ENABLE_RECORDING_ENCRYPTION=true
ENABLE_METADATA_ENCRYPTION=true
ENCRYPTION_ALGORITHM=AES-256-GCM
KEY_ROTATION_INTERVAL=12h
KEY_BACKUP_ENABLED=true
MASTER_KEY_PATH=/secure/keys
PBKDF2_ITERATIONS=200000
ENCRYPTION_KEY_STORE=file
```

#### Development Setup
```bash
ENABLE_RECORDING_ENCRYPTION=true
ENABLE_METADATA_ENCRYPTION=false
ENCRYPTION_ALGORITHM=ChaCha20-Poly1305
KEY_ROTATION_INTERVAL=1h
ENCRYPTION_KEY_STORE=memory
```

#### Recording-Only Encryption
```bash
ENABLE_RECORDING_ENCRYPTION=true
ENABLE_METADATA_ENCRYPTION=false
ENCRYPTION_ALGORITHM=AES-256-GCM
MASTER_KEY_PATH=./recording-keys
```

## File Format

### Encrypted Recording Files

Encrypted recordings use the `.siprec` format with the following structure:

```
┌─────────────────┬──────────────────┬─────────────────┬─────────────────┐
│   File Header   │   Header Data    │   Chunk 1       │   Chunk 2       │
│   (4 bytes)     │   (Variable)     │   (Variable)    │   (Variable)    │
└─────────────────┴──────────────────┴─────────────────┴─────────────────┘
```

#### File Header Format
```json
{
  "magic": "SIPREC01",
  "version": 1,
  "algorithm": "AES-256-GCM",
  "key_id": "abc123...",
  "key_version": 1,
  "nonce_size": 12,
  "tag_size": 16
}
```

#### Chunk Format
Each audio chunk is encrypted separately:
```
┌─────────────────┬─────────────────┐
│   Chunk Length  │   Encrypted     │
│   (4 bytes)     │   Data          │
└─────────────────┴─────────────────┘
```

### Encrypted Metadata Files

Metadata files use the `.metadata` format:

```json
{
  "algorithm": "AES-256-GCM",
  "key_id": "def456...",
  "key_version": 1,
  "nonce": "base64-encoded-nonce",
  "ciphertext": "base64-encoded-data",
  "encrypted_at": "2023-12-07T10:30:00Z"
}
```

## API Usage

### Programmatic Encryption

```go
import "siprec-server/pkg/encryption"

// Create encryption manager
config := &encryption.EncryptionConfig{
    EnableRecordingEncryption: true,
    EnableMetadataEncryption:  true,
    Algorithm:                 "AES-256-GCM",
    KeySize:                   32,
}

keyStore := encryption.NewMemoryKeyStore()
manager, err := encryption.NewManager(config, keyStore, logger)

// Encrypt audio data
sessionID := "call-session-123"
audioData := []byte("raw audio data")
encryptedData, err := manager.EncryptRecording(sessionID, audioData)

// Decrypt audio data
decryptedData, err := manager.DecryptRecording(sessionID, encryptedData)

// Encrypt metadata
metadata := map[string]interface{}{
    "participants": []string{"Alice", "Bob"},
    "codec":        "PCMU",
}
encryptedMetadata, err := manager.EncryptMetadata(sessionID, metadata)

// Decrypt metadata
decryptedMetadata, err := manager.DecryptMetadata(sessionID, encryptedMetadata)
```

### Session Management with Encryption

```go
import "siprec-server/pkg/siprec"

// Create encrypted session manager
encSessionMgr, err := siprec.NewEncryptedSessionManager(
    sessionMgr,     // Base session manager
    encManager,     // Encryption manager
    "./recordings", // Recording directory
    "./metadata",   // Metadata directory
    logger,
)

// Create encrypted session with security policy
policy := &siprec.SessionSecurityPolicy{
    RequireEncryption:    true,
    AllowedAlgorithms:    []string{"AES-256-GCM"},
    KeyRotationRequired:  true,
    AccessControlEnabled: true,
    AuditLoggingEnabled:  true,
}

session, err := encSessionMgr.CreateEncryptedSession(
    ctx,
    sessionID,
    recordingSession,
    policy,
)

// Write encrypted audio data
err = encSessionMgr.WriteAudioData(sessionID, audioData)

// Stop and finalize session
err = encSessionMgr.StopEncryptedSession(sessionID, policy)
```

## Key Management

### Automatic Key Rotation

The server supports automatic key rotation:

```go
// Start key rotation service
rotationService := encryption.NewRotationService(encManager, config, logger)
err := rotationService.Start()

// Manual rotation trigger
err = rotationService.TriggerRotation()

// Stop rotation service
err = rotationService.Stop()
```

### Key Storage Options

#### File-based Storage
- Keys stored in separate files for security
- Metadata and actual key data stored separately
- Basic obfuscation of key material
- Automatic loading on startup

#### Memory-based Storage
- Keys stored only in memory
- No persistent storage
- Suitable for development and testing
- Keys lost on restart

### Key Backup and Recovery

```bash
# Keys are automatically backed up when enabled
ls ./keys/
# abc123def456.key      - Key metadata
# abc123def456.keydata  - Obfuscated key data
# backup.1701234567     - Encrypted backup
```

## Security Considerations

### Best Practices

1. **Use Strong Master Passwords**: If using password-based key derivation
2. **Secure Key Storage**: Protect the key storage directory with appropriate file permissions
3. **Regular Key Rotation**: Enable automatic key rotation for forward secrecy
4. **Backup Strategy**: Implement secure backup and recovery procedures
5. **Access Control**: Limit access to encryption keys and configuration
6. **Audit Logging**: Enable comprehensive audit logging for security monitoring

### Algorithm Selection

#### AES-256-GCM (Recommended)
- **Pros**: Well-established, hardware acceleration, NIST approved
- **Cons**: Larger implementation, potential side-channel attacks
- **Use Case**: High-security environments, compliance requirements

#### ChaCha20-Poly1305
- **Pros**: Resistant to timing attacks, faster on some platforms
- **Cons**: Less widespread adoption, newer algorithm
- **Use Case**: Performance-critical environments, embedded systems

### Key Rotation Strategy

```bash
# Conservative (High Security)
KEY_ROTATION_INTERVAL=6h

# Balanced (Recommended)
KEY_ROTATION_INTERVAL=24h

# Relaxed (Development)
KEY_ROTATION_INTERVAL=168h  # 1 week
```

## Monitoring and Observability

### Health Checks

The server provides encryption-specific health checks:

```bash
curl http://localhost:8080/health
{
  "status": "healthy",
  "encryption": {
    "enabled": true,
    "algorithm": "AES-256-GCM",
    "key_rotation_running": true,
    "active_keys": 1
  }
}
```

### Metrics

Encryption metrics are available via Prometheus:

```
# Active encryption keys
siprec_encryption_active_keys{algorithm="AES-256-GCM"} 1

# Encryption operations
siprec_encryption_operations_total{type="encrypt", algorithm="AES-256-GCM"} 150
siprec_encryption_operations_total{type="decrypt", algorithm="AES-256-GCM"} 45

# Key rotations
siprec_encryption_key_rotations_total 5

# Encryption errors
siprec_encryption_errors_total{type="key_generation"} 0
```

### Audit Logging

When audit logging is enabled, encryption operations are logged:

```json
{
  "timestamp": "2023-12-07T10:30:00Z",
  "action": "session_created",
  "session_id": "call-123",
  "security_level": "high",
  "encrypted": true,
  "details": {
    "algorithm": "AES-256-GCM",
    "key_id": "abc123...",
    "recording_encrypted": true,
    "metadata_encrypted": true
  }
}
```

## Performance Impact

### Encryption Overhead

Typical performance impact of encryption:

| Operation | Overhead | Notes |
|-----------|----------|-------|
| Audio Encryption | 2-5% | Depends on chunk size |
| Metadata Encryption | <1% | Small data size |
| Key Generation | One-time | 10-50ms per key |
| Key Rotation | Minimal | Background operation |

### Optimization Tips

1. **Larger Chunks**: Process larger audio chunks to reduce overhead
2. **Hardware Acceleration**: Use AES-NI instructions when available
3. **Memory Pools**: Reuse buffers to reduce GC pressure
4. **Batch Operations**: Process multiple operations together

## Troubleshooting

### Common Issues

#### Encryption Not Working
```bash
# Check configuration
curl http://localhost:8080/api/config | jq '.encryption'

# Verify key store
ls -la ./keys/

# Check logs
tail -f siprec.log | grep encryption
```

#### Key Rotation Issues
```bash
# Check rotation service status
curl http://localhost:8080/api/encryption/rotation/status

# Manually trigger rotation
curl -X POST http://localhost:8080/api/encryption/rotation/trigger

# Check rotation logs
grep "key rotation" siprec.log
```

#### Decryption Failures
```bash
# Verify key availability
curl http://localhost:8080/api/encryption/keys

# Check algorithm compatibility
grep "algorithm" ./keys/*.key

# Verify file integrity
file encrypted_recording.siprec
```

### Performance Issues

```bash
# Monitor encryption metrics
curl http://localhost:8080/metrics | grep encryption

# Check CPU usage during encryption
top -p $(pgrep siprec)

# Profile encryption operations
go tool pprof http://localhost:8080/debug/pprof/profile
```

## Migration and Compatibility

### Enabling Encryption on Existing Installations

1. **Backup Existing Data**: Always backup before enabling encryption
2. **Gradual Rollout**: Enable encryption for new sessions first
3. **Dual Mode**: Run with encryption optional initially
4. **Migration Scripts**: Use provided tools to encrypt existing data

### Version Compatibility

- **Encrypted Files**: Include version information in headers
- **Key Format**: Backward compatible key storage format
- **Algorithm Support**: Multiple algorithm support for migration

## Future Enhancements

### Planned Features

- **Hardware Security Module (HSM)** support
- **HashiCorp Vault** integration
- **Key escrow** capabilities
- **Certificate-based** key exchange
- **Zero-knowledge** key recovery
- **Quantum-resistant** algorithms

### Contributing

To contribute to encryption features:

1. Follow security best practices
2. Include comprehensive tests
3. Document security implications
4. Submit for security review

---

For additional security questions or implementation details, please refer to the source code documentation or submit an issue on the project repository.