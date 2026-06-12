# Audio Pipeline Architecture

This document describes the RTP audio processing pipeline in IZI SIPREC, including the fixes implemented in v1.2.0 to resolve channel desync and audio quality issues.

## Pipeline Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              SBC / PBX                                       │
│                         (Avaya, Cisco, Oracle, etc.)                        │
└─────────────────────────────┬───────────────────────────────────────────────┘
                              │
              ┌───────────────┴───────────────┐
              │                               │
              ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   RTP leg0 (caller)     │     │   RTP leg1 (callee)     │
│   20ms G.729/PCMU/etc   │     │   20ms G.729/PCMU/etc   │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   UDP Socket (port N)   │     │   UDP Socket (port N+2) │
│   SSRC Validation       │     │   SSRC Validation       │
│   Port Cooldown         │     │   Port Cooldown         │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   Packet Drain Loop     │     │   Packet Drain Loop     │
│   (all buffered pkts)   │     │   (all buffered pkts)   │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   Per-Stream G.729      │     │   Per-Stream G.729      │
│   Decoder (isolated)    │     │   Decoder (isolated)    │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   320 bytes PCM         │     │   320 bytes PCM         │
│   (16-bit, 8kHz mono)   │     │   (16-bit, 8kHz mono)   │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ▼                               ▼
┌─────────────────────────┐     ┌─────────────────────────┐
│   Buffered Pipe (4KB)   │     │   Buffered Pipe (4KB)   │
│   Non-blocking writes   │     │   Non-blocking writes   │
└───────────┬─────────────┘     └───────────┬─────────────┘
            │                               │
            ├───────────────┬───────────────┤
            │               │               │
            ▼               ▼               ▼
┌───────────────┐   ┌───────────────┐   ┌───────────────┐
│   WAV Writer  │   │  STT Stream   │   │   WebSocket   │
│   (per-leg)   │   │  (optional)   │   │   (realtime)  │
└───────────────┘   └───────────────┘   └───────────────┘
            │
            ▼
┌─────────────────────────────────────────────────────────┐
│              WAV Combiner (stereo output)               │
│         Start-time alignment with silence padding       │
└─────────────────────────────────────────────────────────┘
```

## Key Components

### SSRC Validation (v1.2.0)

Each RTP stream locks the SSRC from the first received packet. Subsequent packets with mismatched SSRCs are dropped to prevent crosstalk from stale traffic on recycled ports.

```
First RTP packet → Lock SSRC → Accept only matching SSRC
                              ↓
                    Mismatch? → Drop packet (log warning)
```

**SSRC Auto-Correction**: If the locked SSRC goes completely silent (30+ consecutive non-matching packets) and an alternate SSRC shows sustained traffic (50+ packets), the lock switches automatically. This handles:
- First-packet poisoning after restart
- Silent SSRC changes during hold/unhold

### Port Allocation Cooldown (v1.2.0)

When ports are released, they enter a cooldown period before reuse:

```
Port Released → Cooldown Pool → Fresh ports preferred
                     ↓
            Fallback only when fresh ports exhausted
```

This prevents new calls from receiving delayed packets from previous calls on the same port.

### Per-Stream G.729 Decoder (v1.2.0)

Each RTP forwarding goroutine has its own dedicated G.729 decoder instance:

```
┌─────────────────────────────────────────┐
│           RTP Goroutine (leg0)          │
│  ┌─────────────────────────────────┐    │
│  │   G729StreamDecoder (isolated)  │    │
│  │   - Own decoder state           │    │
│  │   - ConcealPackets() for PLC    │    │
│  │   - Auto-reset on SSRC change   │    │
│  └─────────────────────────────────┘    │
└─────────────────────────────────────────┘
```

**Benefits**:
- No cross-call state leakage
- No race conditions on decoder internals
- Proper PLC that advances decoder state

### Packet Loss Concealment (v1.1.1+)

The pipeline distinguishes between real packet loss and DTX silence:

| Gap Type | Detection | Action |
|----------|-----------|--------|
| Real packet loss | Normal 20ms RTP intervals, sequence gap | Insert concealment (G.729) or silence |
| DTX silence | RTP timestamp gap > 60ms | Skip PLC, insert time-aligned silence |
| Large gap (hold) | Timestamp gap 3-120 seconds | Insert proportional silence for alignment |

### Buffered STT Pipe (v1.1.1)

Replaced unbuffered `io.Pipe` with a 4KB buffered pipe:

```
RTP Handler → [4KB Buffer] → STT Provider
                  ↓
     Overflow? → Drop oldest (maintain realtime)
```

**Benefits**:
- Decouples RTP processing from STT backpressure
- Non-blocking writes prevent audio stalls
- Automatic overflow handling

### WAV Start-Time Alignment (v1.1.1)

When combining stereo recordings, each leg's first RTP timestamp is recorded:

```
Leg0 starts: T=0ms    ████████████████████████
Leg1 starts: T=500ms       ████████████████████

Combined output:
Leg0: ████████████████████████
Leg1: [silence]████████████████████
      ↑ 500ms padding
```

## Problems Fixed in v1.2.0

The following audio pipeline issues are resolved:

| Problem | Description | Fix |
|---------|-------------|-----|
| 1 pkt/tick | Single packet read per tick caused latency variance | Drain all buffered packets per tick |
| Zero-buffer pipe | Unbuffered io.Pipe created backpressure coupling | 4KB buffered pipe with overflow handling |
| No timestamps | Audio paired by arrival order, not RTP timestamps | Timestamp-based gap detection and alignment |
| Shared decoder | G.729 decoder pool caused cross-call state leakage | Per-stream isolated decoder instances |
| Port reuse crosstalk | Stale packets on recycled ports corrupted recordings | SSRC validation + port cooldown |
| No PLC | Missing packet loss concealment | DTX-aware PLC with G.729 ConcealPackets() |
| No alignment | Stereo recordings had channel drift | Wall-clock start-time alignment |

## Configuration

### RTP Settings

```yaml
media:
  rtp_port_min: 10000
  rtp_port_max: 20000
  rtp_timeout: 30s
  rtp_bind_ip: ""  # Empty = all interfaces
```

### Audio Processing

```yaml
audio_processing:
  enabled: true
  enable_vad: true
  vad_threshold: 0.3
  enable_noise_reduction: false
```

## Monitoring

Check for SSRC issues in logs:

```bash
# SSRC locked from first packet (normal)
grep "SSRC locked from RTP packet" /var/log/siprec/server.log

# SSRC mismatches being dropped (normal if occasional)
grep "Dropping RTP packet with unexpected SSRC" /var/log/siprec/server.log

# SSRC correction triggered (indicates SBC behavior change)
grep "SSRC switched: locked SSRC went silent" /var/log/siprec/server.log
```

## See Also

- [Configuration Guide](configuration.md)
- [Troubleshooting](../README.md#troubleshooting)
- [G.729 Codec Support](../README.md#g729-codec-support)
