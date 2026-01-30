# Production SIPREC Server Configuration Guide

**Server:** siprec.izitechnologies.com (192.227.78.73)
**Ports:** 5060 (TCP), 5061 (TLS)
**Status:** Production Ready ✅

---

## Oracle SBC Configuration

### Overview
Configure Oracle Session Border Controller to send SIPREC recordings to your server.

### Prerequisites
- Oracle SBC 8.x or 9.x
- Administrative access to Oracle SBC
- Network connectivity from Oracle SBC to 192.227.78.73

### Step 1: Create SIP Interface for SIPREC

Navigate to: **Configuration > Session Router > SIP Interface**

```
Identifier: SIPREC-OUT
SIP Port: 5060
Transport: TCP
Allow Anonymous: enabled
```

Click **Apply** and **Activate**

### Step 2: Configure Realm for SIPREC Server

Navigate to: **Configuration > Media Manager > Realm**

```
Identifier: SIPREC-REALM
Address Prefix: 192.227.78.0
Network Interfaces: Select your outbound interface
```

Click **Apply**

### Step 3: Create Session Agent for SIPREC Server

Navigate to: **Configuration > Session Router > Session Agent**

```
Hostname: siprec.izitechnologies.com
IP Address: 192.227.78.73
Port: 5060
Transport Method: StaticTCP
Allow Next Hop Routing: enabled
Realm: SIPREC-REALM
Description: SIPREC Recording Server
```

**Advanced Settings:**
```
Ping Interval: 30
Ping Method: OPTIONS
Max Outstanding Messages: 1000
Max Retransmits: 6
```

Click **Apply** and **Activate**

### Step 4: Configure SIP Recording Configuration

Navigate to: **Configuration > Session Router > SIP Recording**

```
State: enabled
Mode: SIPREC
Recording Server: 192.227.78.73:5060
Transport: TCP
Streams: separate (for 3-stream configuration)
Include Metadata: enabled
```

**Metadata Options:**
```
☑ Include Participant Names
☑ Include Call IDs
☑ Include UCIDs
☑ Include Conversation IDs
☑ Include Timestamps
```

**Stream Configuration:**
```
☑ Record Ingress Stream
☑ Record Egress Stream
☑ Record Mixed Stream
```

Click **Apply** and **Activate**

### Step 5: Create SIP Recording Policy

Navigate to: **Configuration > Session Router > Local Policy**

Create a new policy or edit existing:

```
From Address: *
To Address: *
Source Realm: your-realm
Action: SIPREC-RECORD
Next Hop: 192.227.78.73:5060
```

**Recording Triggers:**
```
☑ Record All Calls
OR
☑ Record Based on Header (specify header criteria)
```

Click **Apply** and **Activate**

### Step 6: Configure SIP Headers

Navigate to: **Configuration > Session Router > SIP Manipulation**

Ensure these headers are sent to SIPREC server:

```
X-Oracle-UCID: $ucid
X-Oracle-Conversation-ID: $conversationId
User-Agent: Oracle-SBC/$version
```

### Step 7: Configure Codec for Recording

Navigate to: **Configuration > Media Manager > Media Profile**

Ensure SIPREC sessions support:
```
☑ PCMU (G.711 μ-law) - Codec 0
☑ PCMA (G.711 A-law) - Codec 8
☑ G.729 - Codec 18
☑ telephone-event - Codec 101
```

### Step 8: Test Configuration

1. **Check Session Agent Status:**
   ```
   ACMEPACKET# show session-router session-agent
   ```

   Look for:
   - State: enabled
   - Health: Active
   - Ping Status: Alive

2. **Make a Test Call:**
   - Place a call through the Oracle SBC
   - Verify SIPREC INVITE is sent to 192.227.78.73

3. **Verify on SIPREC Server:**
   ```bash
   ssh ubuntu@siprec.izitechnologies.com
   sudo journalctl -u siprec -f
   ```

   Look for:
   - "vendor_type": "oracle"
   - "oracle_ucid": "..."
   - "oracle_conversation_id": "..."
   - "stream_count": 3

4. **Check Recordings:**
   ```bash
   ls -lh /opt/siprec/recordings/
   ```

   Should see 3 files per call:
   - *ingress-stream.wav
   - *egress-stream.wav
   - *mixed-stream.wav

### Troubleshooting Oracle SBC

**Issue: Session Agent shows "Out of Service"**

Solution:
```bash
# On SIPREC server, check if port is listening
sudo netstat -tlnp | grep 5060

# Check firewall
sudo ufw status | grep 5060

# Test connectivity from Oracle SBC
ping 192.227.78.73
telnet 192.227.78.73 5060
```

**Issue: INVITE sent but no response**

Check:
1. Firewall rules on SIPREC server
2. Network route from Oracle SBC to SIPREC server
3. SIP Interface configuration (correct realm)

**Issue: Only 1 stream recorded instead of 3**

Solution:
- Check SIP Recording Configuration
- Ensure "Streams: separate" is configured
- Verify all 3 stream checkboxes are enabled

---

## Avaya Session Manager Configuration

### Overview
Configure Avaya Session Manager (Aura) to send SIPREC recordings.

### Prerequisites
- Avaya Aura Session Manager 7.x or 8.x
- System Manager access
- Network connectivity to 192.227.78.73

### Step 1: Add SIPREC Server as SIP Entity

**System Manager > Elements > Session Manager > SIP Entities**

Click **New**

```
Name: SIPREC-Server
FQDN or IP Address: siprec.izitechnologies.com
Type: Recording Server
Port: 5060
Transport: TCP
```

**Advanced:**
```
☑ Enable Recording
Recording Protocol: SIPREC
```

Click **Commit**

### Step 2: Create Entity Link

**Session Manager > Entity Links**

Click **New**

```
Name: SM-to-SIPREC
SIP Entity 1: Session-Manager
SIP Entity 2: SIPREC-Server
Protocol: TCP
Port: 5060
Trusted: Yes
```

Click **Commit**

### Step 3: Configure Recording Profile

**Session Manager > Session Manager Administration > Recording**

Click **New Recording Profile**

```
Profile Name: Production-Recording
Recording Server: SIPREC-Server
Recording Type: SIPREC
Stream Mode: Separate (3 streams)
Protocol: TCP
Port: 5060
```

**Metadata Settings:**
```
☑ Include Participant Information
☑ Include Call Direction
☑ Include UCID
☑ Include Extension Numbers
☑ Include Display Names
```

**Stream Configuration:**
```
☑ Caller Stream
☑ Callee Stream
☑ Mixed Stream
```

Click **Commit**

### Step 4: Apply Recording to Users/Trunks

**Option A: Record All Calls on a Trunk**

Navigate to: **Session Manager > Routing > SIP Trunks**

Select your trunk, then:
```
Recording Profile: Production-Recording
Recording Trigger: All Calls
```

**Option B: Record Specific Users**

Navigate to: **Communication Manager > Users**

For each user:
```
Recording: Enabled
Recording Profile: Production-Recording
```

**Option C: Record Based on Dial Plan**

Navigate to: **Session Manager > Dial Patterns**

Add dial pattern:
```
Pattern: *
Min/Max: as needed
Recording Profile: Production-Recording
```

Click **Commit**

### Step 5: Configure SIP Headers

**Session Manager > Features > Header Manipulation**

Ensure headers are passed to SIPREC:

```
X-Avaya-UCID: Pass Through
User-Agent: Pass Through (Avaya-SM/version)
```

### Step 6: Configure Codecs

**Session Manager > Codec Sets**

Ensure SIPREC codec set includes:
```
☑ G.711MU (PCMU)
☑ G.711A (PCMA)
☑ G.729
```

Apply to Recording Profile.

### Step 7: Test Configuration

1. **Verify SIP Entity Status:**

   **Session Manager > Dashboard > SIP Entity Status**

   SIPREC-Server should show: **Active/Green**

2. **Make Test Call:**
   - Place call from monitored user/trunk
   - Check System Manager logs for SIPREC INVITE

3. **Verify on SIPREC Server:**
   ```bash
   ssh ubuntu@siprec.izitechnologies.com
   sudo journalctl -u siprec -f | grep -i avaya
   ```

   Look for:
   - "vendor_type": "avaya"
   - "User-Agent": "Avaya-SM/..."
   - "stream_count": 3

4. **Check Recordings:**
   ```bash
   ls -lh /opt/siprec/recordings/
   ```

   Should see 3 files per call:
   - *caller-stream.wav
   - *callee-stream.wav
   - *mixed-stream.wav

### Troubleshooting Avaya

**Issue: SIP Entity shows "Offline"**

Check:
1. Network connectivity: `ping 192.227.78.73`
2. Port open: `telnet 192.227.78.73 5060`
3. Entity Link configured correctly
4. Firewall rules on both sides

**Issue: Calls not being recorded**

Check:
1. Recording Profile applied to correct users/trunks
2. Recording license available
3. Check System Manager logs for errors
4. Verify dial pattern matches

**Issue: Only getting 1 or 2 streams**

Solution:
- Recording Profile > Stream Mode: **Separate**
- Enable all 3 stream checkboxes
- Check codec compatibility

---

## Network Configuration

### Firewall Rules Required

**On SIPREC Server (192.227.78.73):**

```bash
# Allow SIP TCP from Oracle SBC network
sudo ufw allow from <oracle-sbc-ip> to any port 5060 proto tcp comment 'Oracle SBC SIP'

# Allow SIP TCP from Avaya SM network
sudo ufw allow from <avaya-sm-ip> to any port 5060 proto tcp comment 'Avaya SM SIP'

# Allow SIP TLS (if using TLS)
sudo ufw allow from <oracle-sbc-ip> to any port 5061 proto tcp comment 'Oracle SBC SIP TLS'
sudo ufw allow from <avaya-sm-ip> to any port 5061 proto tcp comment 'Avaya SM SIP TLS'

# Allow RTP ports for media
sudo ufw allow from <oracle-sbc-ip> to any port 10000:20000 proto udp comment 'Oracle RTP'
sudo ufw allow from <avaya-sm-ip> to any port 10000:20000 proto udp comment 'Avaya RTP'
```

**Example with your IPs:**
```bash
# Home office IP
sudo ufw allow from 104.203.204.52 to any port 5060 proto tcp
sudo ufw allow from 104.203.204.52 to any port 5061 proto tcp
sudo ufw allow from 104.203.204.52 to any port 10000:20000 proto udp

# Local network
sudo ufw allow from 192.227.78.0/24 to any port 5060 proto tcp
sudo ufw allow from 192.227.78.0/24 to any port 5061 proto tcp
sudo ufw allow from 192.227.78.0/24 to any port 10000:20000 proto udp
```

### DNS Configuration (Optional but Recommended)

If using DNS name instead of IP:

1. Ensure `siprec.izitechnologies.com` resolves correctly
2. Add SRV records for redundancy (optional):
   ```
   _sip._tcp.siprec.izitechnologies.com. 3600 IN SRV 10 10 5060 siprec.izitechnologies.com.
   _sips._tcp.siprec.izitechnologies.com. 3600 IN SRV 10 10 5061 siprec.izitechnologies.com.
   ```

---

## TLS/Secure Configuration (Recommended for Production)

### Using Port 5061 with TLS

**Benefits:**
- Encrypted SIP signaling
- Secure metadata transmission
- Compliance requirements (HIPAA, PCI, etc.)

### Oracle SBC TLS Configuration

In Session Agent configuration:
```
Port: 5061
Transport Method: StaticTLS
TLS Profile: Select your TLS profile
```

### Avaya TLS Configuration

In SIP Entity configuration:
```
Port: 5061
Transport: TLS
Certificate: Select Avaya certificate
```

### SIPREC Server Already Configured

Your server already has Let's Encrypt certificates:
- Valid until: 2026-04-30
- Automatic renewal enabled
- TLS listening on port 5061 ✅

---

## Monitoring and Maintenance

### Check SIPREC Server Health

```bash
# Service status
sudo systemctl status siprec

# Recent calls
sudo journalctl -u siprec --since '1 hour ago' | grep -i invite | wc -l

# Active calls
curl -s https://localhost:8080/health -k | jq '.system.active_calls'

# Disk usage
du -sh /opt/siprec/recordings/

# Check specific vendor traffic
sudo journalctl -u siprec -f | grep -i oracle
sudo journalctl -u siprec -f | grep -i avaya
```

### Recording File Management

**Set up automatic cleanup:**

Create `/opt/siprec/cleanup.sh`:
```bash
#!/bin/bash
# Delete recordings older than 30 days
find /opt/siprec/recordings -name "*.wav" -mtime +30 -delete
```

Add to crontab:
```bash
# Run daily at 2 AM
0 2 * * * /opt/siprec/cleanup.sh
```

### Monitoring Script

Create `/opt/siprec/monitor.sh`:
```bash
#!/bin/bash
# Monitor SIPREC server

# Check service
if ! systemctl is-active --quiet siprec; then
    echo "ALERT: SIPREC service is down!"
    exit 1
fi

# Check disk space
USAGE=$(df /opt/siprec/recordings | tail -1 | awk '{print $5}' | sed 's/%//')
if [ $USAGE -gt 85 ]; then
    echo "WARNING: Disk usage at ${USAGE}%"
fi

# Check active calls
CALLS=$(curl -s https://localhost:8080/health -k | jq '.system.active_calls')
echo "Active calls: $CALLS"

# Check recent errors
ERRORS=$(sudo journalctl -u siprec --since '5 minutes ago' | grep -i error | wc -l)
if [ $ERRORS -gt 10 ]; then
    echo "WARNING: $ERRORS errors in last 5 minutes"
fi
```

---

## Production Checklist

Before going live, verify:

### SIPREC Server
- [ ] Service running and enabled
- [ ] TLS certificates valid
- [ ] Firewall rules configured
- [ ] Disk space available (500GB+ recommended)
- [ ] Backup strategy in place
- [ ] Monitoring configured

### Oracle SBC
- [ ] Session Agent active
- [ ] Recording profile configured
- [ ] Test call successful
- [ ] 3 streams recording
- [ ] Headers being sent (UCID, Conversation-ID)

### Avaya Session Manager
- [ ] SIP Entity active
- [ ] Entity Link configured
- [ ] Recording profile applied
- [ ] Test call successful
- [ ] 3 streams recording
- [ ] Proper user/trunk assignment

### Network
- [ ] Firewall rules on both sides
- [ ] Network route verified
- [ ] TCP port 5060 accessible
- [ ] UDP ports 10000-20000 accessible
- [ ] DNS resolving (if using FQDN)

### Testing
- [ ] Oracle test call completed
- [ ] Avaya test call completed
- [ ] 3 WAV files per call
- [ ] Files contain audio (not empty)
- [ ] Metadata correctly extracted
- [ ] Vendor detection working

---

## Support and Troubleshooting

### Common Issues

**Problem: No audio in recordings**

Causes:
1. RTP packets not reaching server (firewall)
2. Codec mismatch
3. NAT/routing issue

Solution:
```bash
# Check RTP packets arriving
sudo tcpdump -i any -n udp port 10000:20000

# Check active RTP forwarders
sudo journalctl -u siprec -f | grep RTP
```

**Problem: Metadata missing**

Check:
1. Vendor headers being sent
2. Multipart MIME format correct
3. SIPREC metadata XML valid

**Problem: High CPU/Memory usage**

Monitor:
```bash
# Check resource usage
top -p $(pgrep siprec)

# Check active streams
curl -s https://localhost:8080/health -k | jq '.system'
```

### Getting Help

1. **Check logs:**
   ```bash
   sudo journalctl -u siprec --since '10 minutes ago' --no-pager | tail -100
   ```

2. **Enable debug logging:**
   Edit `/opt/siprec/.env`:
   ```
   LOG_LEVEL=debug
   ```
   Restart: `sudo systemctl restart siprec`

3. **Capture SIP traffic:**
   ```bash
   sudo tcpdump -i any -s 0 -w /tmp/siprec.pcap port 5060
   # Analyze with sngrep or Wireshark
   ```

---

## Performance Recommendations

### Expected Load

**Per Call:**
- SIP: ~2 KB (INVITE + responses)
- RTP: ~10 KB/second per stream
- Recording: ~960 KB/minute per stream at G.711
- Memory: ~2 MB per active stream

**Example with 100 concurrent calls (3 streams each):**
- Active RTP streams: 300
- Disk write: ~48 MB/second
- Memory: ~600 MB
- CPU: 20-30% (depends on transcription)

### Scaling Guidelines

| Concurrent Calls | CPU Cores | RAM | Disk I/O | Network |
|------------------|-----------|-----|----------|---------|
| 50 | 4 | 8 GB | 25 MB/s | 100 Mbps |
| 100 | 8 | 16 GB | 50 MB/s | 200 Mbps |
| 250 | 16 | 32 GB | 125 MB/s | 500 Mbps |
| 500 | 32 | 64 GB | 250 MB/s | 1 Gbps |

**Current Server:** 8 CPU cores, likely handles 100-150 concurrent calls comfortably.

---

## Summary

Your SIPREC server is **production ready** for both Oracle and Avaya!

**Key Points:**
- ✅ 3-stream configuration working
- ✅ Oracle vendor detection active
- ✅ Avaya vendor detection active
- ✅ TLS certificates configured
- ✅ Automatic renewal enabled
- ✅ Health monitoring available

**Next Steps:**
1. Apply configurations to Oracle SBC and/or Avaya SM
2. Run test calls from production systems
3. Verify recordings and metadata
4. Enable for production traffic
5. Monitor and maintain

**Configuration Complete!** 🎉
