# Security Features in SIPREC Server

This document outlines the security features implemented in the SIPREC server, focusing on secure signaling and media transmission.

## Overview

The SIPREC server implements multiple security layers to protect both signaling and media:

1. **Transport Layer Security (TLS)** for SIP signaling encryption
2. **Secure Real-time Transport Protocol (SRTP)** for media encryption
3. **Certificate-based authentication** for server identity verification
4. **Secure context handling** for resource protection

## TLS Implementation

### Features

The TLS implementation enables secure SIP signaling by:

- Configuring the SIP server to listen on a dedicated TLS port (default 5063)
- Using X.509 certificates for server identity verification
- Enforcing TLS 1.2+ for enhanced security
- Providing proper validation of certificates

### Configuration

TLS is configured in the `.env` file with:

```
ENABLE_TLS=true
TLS_PORT=5063
TLS_CERT_PATH=/path/to/cert.pem
TLS_KEY_PATH=/path/to/key.pem
```

### Internal Implementation

The TLS implementation uses the sipgo library's `ListenAndServeTLS` method with the "tls" network type. Key implementation aspects include:

- Separate goroutines for each listener type (UDP, TCP, TLS)
- Proper error handling and propagation from listener threads
- Port verification to ensure the TLS server is correctly bound
- Graceful shutdown of TLS connections

## SRTP Implementation

### Features

SRTP (Secure RTP) implementation enables secure media transport by:

- Adding SRTP support to RTP forwarders
- Generating secure crypto attributes in SDP
- Implementing proper SRTP packet processing with authentication
- Supporting AES_CM_128_HMAC_SHA1_80 profile (per RFC 4568)

### Configuration

SRTP is enabled in the `.env` file with:

```
ENABLE_SRTP=true
```

### Internal Implementation

The SRTP implementation includes:

- `SRTPKeyInfo` struct for managing crypto attributes
- Key management for secure key exchange
- Crypto suite negotiation through SDP
- Media packet encryption/decryption

## Security Best Practices

When deploying the SIPREC server in production, follow these security recommendations:

1. **Certificate Management**:
   - Use trusted certificates for production environments
   - Implement certificate rotation and monitoring
   - Validate certificate chain when connecting to external systems

2. **Network Security**:
   - Place the server behind a firewall limiting access to essential ports
   - Use separate networks for management and media traffic
   - Implement network-level encryption when possible

3. **Key Management**:
   - Store cryptographic keys securely
   - Implement key rotation policies
   - Use secure methods for key distribution

4. **Monitoring and Logging**:
   - Monitor for security events and connection attempts
   - Log security-relevant information (authentication attempts, TLS handshakes)
   - Implement alerts for security anomalies

## Testing Security Features

To test the security features:

1. **TLS Testing**:
   - Use OpenSSL to verify TLS connections
   - Check certificate validation
   - Verify TLS version negotiation

   ```bash
   # Test TLS connection
   openssl s_client -connect your-server:5063
   ```

2. **SRTP Testing**:
   - Use SIP clients that support SRTP
   - Verify SRTP negotiation in SDP
   - Check secure media transmission

   ```bash
   # The repository includes test utilities in test_tls/ directory
   cd test_tls
   go build -o mock_invite_tls mock_invite_tls.go 
   ./mock_invite_tls
   ```

## Security Related RFCs

The implementation follows these security-related RFCs:

- **RFC 3261**: Session Initiation Protocol (SIP)
- **RFC 3711**: The Secure Real-time Transport Protocol (SRTP)
- **RFC 4568**: Session Description Protocol (SDP) Security Descriptions
- **RFC 5246**: The Transport Layer Security (TLS) Protocol Version 1.2
- **RFC 6347**: Datagram Transport Layer Security Version 1.2
- **RFC 7245**: SIP-Based Media Recording: Session-Aware Monitoring

## Future Security Enhancements

Planned security improvements include:

1. **DTLS-SRTP Support**: Implement DTLS for SRTP key exchange
2. **Enhanced Authentication**: Add digest authentication for SIP
3. **Key Rotation**: Automatic key rotation for long-lived sessions
4. **TLS 1.3 Support**: Update to latest TLS version when library support is available