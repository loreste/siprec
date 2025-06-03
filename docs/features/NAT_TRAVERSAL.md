# NAT Traversal Support

The SIPREC server includes comprehensive NAT (Network Address Translation) traversal support to handle deployments where the server is behind a NAT device, such as a router or firewall.

## Overview

NAT traversal is essential for SIPREC servers deployed in private networks that need to communicate with SIP endpoints on the public internet. The NAT rewriter automatically modifies SIP headers and SDP content to replace private IP addresses with public IP addresses, ensuring proper routing and connectivity.

## Features

- **Automatic SIP Header Rewriting**: Modifies Via, Contact, and Record-Route headers
- **SDP Content Rewriting**: Updates connection and origin lines in SDP
- **SIPREC Multipart Support**: Handles multipart SIPREC content with embedded SDP
- **External IP Detection**: Auto-detection via STUN servers or manual configuration
- **Port Translation**: Support for different internal and external ports
- **Selective Rewriting**: Fine-grained control over which headers to rewrite
- **Force Rewrite Mode**: Option to rewrite all RFC1918 private IP addresses

## Configuration

### Basic NAT Configuration

```yaml
nat:
  behind_nat: true
  internal_ip: "192.168.1.100"      # Private IP of SIPREC server
  external_ip: "203.0.113.10"       # Public IP (or leave empty for auto-detection)
  internal_port: 5060               # Internal SIP port
  external_port: 5060               # External SIP port
  
  # Header rewriting options
  rewrite_via: true
  rewrite_contact: true
  rewrite_record_route: true
  
  # External IP detection
  auto_detect_external_ip: false    # Enable if external_ip is empty
  stun_server: "stun.l.google.com:19302"
  
  # Advanced options
  force_rewrite: false              # Rewrite all private IPs found
```

### Media Configuration Integration

You can also configure NAT through the media configuration:

```yaml
media:
  # NAT settings
  behind_nat: true
  internal_ip: "192.168.1.100"
  external_ip: "203.0.113.10"
  sip_internal_port: 5060
  sip_external_port: 5060
  
  # Other media settings
  rtp_port_min: 10000
  rtp_port_max: 20000
  enable_srtp: true
```

## Usage Scenarios

### Scenario 1: Static Public IP

When you have a known static public IP address:

```go
natConfig := &sip.NATConfig{
    BehindNAT:            true,
    InternalIP:           "192.168.1.100",
    ExternalIP:           "203.0.113.10", // Your static public IP
    InternalPort:         5060,
    ExternalPort:         5060,
    RewriteVia:           true,
    RewriteContact:       true,
    RewriteRecordRoute:   true,
    AutoDetectExternalIP: false,
}
```

### Scenario 2: Dynamic IP with Auto-Detection

For dynamic public IP addresses using STUN:

```go
natConfig := &sip.NATConfig{
    BehindNAT:            true,
    InternalIP:           "192.168.1.100",
    ExternalIP:           "", // Will be auto-detected
    InternalPort:         5060,
    ExternalPort:         5060,
    RewriteVia:           true,
    RewriteContact:       true,
    RewriteRecordRoute:   true,
    AutoDetectExternalIP: true,
    STUNServer:           "stun.l.google.com:19302",
}
```

### Scenario 3: Port Forwarding

When using port forwarding with different internal/external ports:

```go
natConfig := &sip.NATConfig{
    BehindNAT:            true,
    InternalIP:           "192.168.1.100",
    ExternalIP:           "203.0.113.10",
    InternalPort:         5060,    // Internal SIP port
    ExternalPort:         15060,   // External port forwarded to 5060
    RewriteVia:           true,
    RewriteContact:       true,
    RewriteRecordRoute:   true,
}
```

### Scenario 4: Force Rewrite Mode

To rewrite any private IP addresses found in headers:

```go
natConfig := &sip.NATConfig{
    BehindNAT:            true,
    InternalIP:           "192.168.1.100",
    ExternalIP:           "203.0.113.10",
    ForceRewrite:         true, // Rewrite any RFC1918 IP found
    RewriteVia:           true,
    RewriteContact:       true,
    RewriteRecordRoute:   true,
}
```

## SIP Header Rewriting

### Via Headers

The NAT rewriter modifies Via headers to replace private IP addresses:

**Before:**
```
Via: SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123
```

**After:**
```
Via: SIP/2.0/UDP 203.0.113.10:5060;branch=z9hG4bK123
```

### Contact Headers

Contact headers are rewritten to ensure proper routing:

**Before:**
```
Contact: <sip:user@192.168.1.100:5060>
```

**After:**
```
Contact: <sip:user@203.0.113.10:5060>
```

### Record-Route Headers

Record-Route headers are updated for proper routing:

**Before:**
```
Record-Route: <sip:192.168.1.100:5060;lr>
```

**After:**
```
Record-Route: <sip:203.0.113.10:5060;lr>
```

## SDP Content Rewriting

### Connection Lines

SDP connection lines (c=) are rewritten:

**Before:**
```
c=IN IP4 192.168.1.100
```

**After:**
```
c=IN IP4 203.0.113.10
```

### Origin Lines

SDP origin lines (o=) are updated:

**Before:**
```
o=user 123456 654321 IN IP4 192.168.1.100
```

**After:**
```
o=user 123456 654321 IN IP4 203.0.113.10
```

### Media Lines

Media lines with IP addresses are rewritten if necessary:

**Before:**
```
m=audio 10000 RTP/AVP 0
```

**After:**
```
m=audio 10000 RTP/AVP 0
```
*(Port numbers are handled by RTP port allocation)*

## SIPREC Multipart Content

The NAT rewriter handles SIPREC multipart content by:

1. Parsing multipart boundaries
2. Identifying SDP parts within the multipart content
3. Rewriting only the SDP portions while preserving metadata
4. Reconstructing the multipart content

Example multipart rewriting:

**Before:**
```
--boundary123
Content-Type: application/rs-metadata+xml

<metadata>...</metadata>

--boundary123
Content-Type: application/sdp

v=0
o=user 123456 654321 IN IP4 192.168.1.100
c=IN IP4 192.168.1.100
...
--boundary123--
```

**After:**
```
--boundary123
Content-Type: application/rs-metadata+xml

<metadata>...</metadata>

--boundary123
Content-Type: application/sdp

v=0
o=user 123456 654321 IN IP4 203.0.113.10
c=IN IP4 203.0.113.10
...
--boundary123--
```

## Runtime Management

### Manual IP Updates

You can manually update the external IP at runtime:

```go
if handler.NATRewriter != nil {
    handler.NATRewriter.SetExternalIP("203.0.113.20")
}
```

### Getting Current External IP

```go
if handler.NATRewriter != nil {
    externalIP := handler.NATRewriter.GetExternalIP()
    fmt.Printf("Current external IP: %s\n", externalIP)
}
```

### Auto-Detection Refresh

The NAT rewriter automatically refreshes the external IP every 5 minutes when auto-detection is enabled.

## Network Requirements

### Firewall Configuration

Ensure your firewall allows:

1. **Inbound SIP traffic** on the external SIP port (default 5060)
2. **Inbound RTP traffic** on the configured RTP port range
3. **Outbound STUN traffic** to the STUN server (if using auto-detection)

### Port Forwarding

Configure your NAT device to forward:

1. **SIP port** (e.g., 5060) to the internal SIPREC server
2. **RTP port range** (e.g., 10000-20000) for media

### STUN Server Access

For auto-detection, ensure access to STUN servers:

- Default: `stun.l.google.com:19302`
- Alternative: `stun1.l.google.com:19302`
- Custom STUN servers can be configured

## Troubleshooting

### Common Issues

1. **SIP signaling works but no audio**
   - Check RTP port range forwarding
   - Verify media configuration

2. **Auto-detection fails**
   - Check STUN server accessibility
   - Verify firewall allows UDP traffic to STUN port

3. **Headers not being rewritten**
   - Verify `behind_nat: true` is set
   - Check that internal IP matches server's IP
   - Ensure rewrite flags are enabled

### Debug Logging

Enable debug logging to see NAT rewriting activity:

```go
logger.SetLevel(logrus.DebugLevel)
```

This will log:
- Header rewriting operations
- SDP content modifications
- External IP detection attempts
- Configuration validation

### Validation

Use the validation function to check your configuration:

```go
if err := sip.ValidateNATConfig(natConfig); err != nil {
    log.Fatalf("Invalid NAT configuration: %v", err)
}
```

Common validation errors:
- Invalid IP address format
- Internal IP not in private range
- External IP in private range
- Invalid port numbers

## Performance Considerations

- NAT rewriting adds minimal overhead to message processing
- SDP parsing is optimized for performance
- Auto-detection uses caching to minimize STUN requests
- Thread-safe operations for concurrent message handling

## Security Considerations

- Private IP addresses are logged in debug mode
- External IP detection may reveal network topology
- STUN traffic is unencrypted
- Configure appropriate firewall rules to limit exposure