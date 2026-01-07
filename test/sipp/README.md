# SIPp Test Scenarios

This directory contains SIPp scenarios for testing the SIPREC server.

## Available Scenarios

| File | Description |
|------|-------------|
| `siprec_load_test.xml` | Load testing scenario with configurable call duration |
| `siprec_with_media.xml` | SIPREC call with RTP media streaming |
| `siprec_hold_resume.xml` | Test hold/resume functionality |
| `siprec_transfer.xml` | Test call transfer scenarios |
| `siprec_rtp_stream.xml` | RTP streaming test |

## Load Testing

### Basic Load Test

```bash
# 100 concurrent calls, 30-second duration
sipp <server>:5060 -sf siprec_load_test.xml -l 100 -m 100 -r 10

# 1000 concurrent calls
sipp <server>:5060 -sf siprec_load_test.xml -l 1000 -m 1000 -r 50
```

### High-Concurrency Testing

For high-concurrency tests (5000+ calls), use TCP with `tn` mode:

```bash
# 6000 concurrent calls, 5-minute duration, 100 cps ramp-up
sipp <server>:5060 -t tn -sf siprec_load_test.xml -l 6000 -m 6000 -r 100 -timeout 600
```

### SIPp Transport Modes

| Mode | Flag | Description | Recommended For |
|------|------|-------------|-----------------|
| UDP single socket | `-t u1` | Default UDP mode | Basic testing |
| UDP multi-socket | `-t un` | One UDP socket per call | High concurrency UDP |
| TCP single socket | `-t t1` | Single TCP connection | May have issues on macOS |
| TCP multi-socket | `-t tn` | One TCP socket per call | **High concurrency TCP** |

**Note:** On macOS, `-t t1` may fail with "Address already in use" errors. Always use `-t tn` for TCP testing.

### Key Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| `-l <n>` | Max concurrent calls | `-l 5000` |
| `-m <n>` | Total calls to make | `-m 5000` |
| `-r <n>` | Call rate (calls/sec) | `-r 100` |
| `-rp <ms>` | Rate period in ms | `-rp 1000` |
| `-timeout <s>` | Global timeout | `-timeout 600` |
| `-bg` | Run in background | |

### Modifying Call Duration

The call duration is controlled by the `<pause>` element in the scenario. Edit `siprec_load_test.xml`:

```xml
<!-- 30 seconds -->
<pause milliseconds="30000"/>

<!-- 5 minutes -->
<pause milliseconds="300000"/>
```

## Tested Performance Results

| Concurrent Calls | Duration | Transport | Success Rate |
|-----------------|----------|-----------|--------------|
| 100 | 30s | UDP | 100% |
| 1,000 | 30s | UDP | 100% |
| 5,000 | 30s | UDP | 100% |
| 6,000 | 5 min | TCP (tn) | 100% |
| 10,000 | 30s | UDP | 100% |
| 20,000 | 30s | UDP | 100% |

## Helper Scripts

### `run_media_test.sh`

Runs a SIPREC test with actual RTP media:

```bash
./run_media_test.sh <server_ip> <server_port>
```

### `rtp_sender.py`

Python script to send RTP audio packets:

```bash
python3 rtp_sender.py --host <server> --port <rtp_port>
```

## Troubleshooting

### "Address already in use" on macOS

Use TCP multi-socket mode:
```bash
sipp <server>:5060 -t tn -sf scenario.xml ...
```

### Low ACK Rate with UDP

At very high call rates, UDP may experience packet loss. Solutions:
1. Use TCP (`-t tn`) for reliable delivery
2. Increase UDP buffers on the server:
   ```bash
   sudo sysctl -w net.core.rmem_max=26214400
   sudo sysctl -w net.core.wmem_max=26214400
   ```

### SIPp Exits Early

Ensure sufficient timeout for long-duration calls:
```bash
sipp ... -timeout 900  # 15 minutes
```
