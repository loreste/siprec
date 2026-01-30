# Understanding SIPREC: 1 Call vs 3 Streams

## What You See in sngrep vs What's Actually Happening

### In sngrep: 1 SIP Call ✅ (This is correct!)

```
INVITE ────────>
       <──────── 100 Trying
       <──────── 180 Ringing
       <──────── 200 OK
ACK    ────────>
       (call established - 3 RTP streams flowing)
BYE    ────────>
       <──────── 200 OK
```

**You see:** 1 SIP dialog (one call in the call list)

**This is CORRECT!** SIPREC doesn't create 3 separate SIP calls.

### What's Inside That 1 SIP Call: 3 Audio Streams

When you look at the **SDP body** inside the INVITE, you'll see:

```sdp
v=0
o=OracleSBC 987654321 123456789 IN IP4 192.168.1.100
s=Oracle 3-Stream SIPREC Recording
c=IN IP4 192.168.1.100
t=0 0

m=audio 10000 RTP/AVP 0 8 18 101    ← Stream 1 (ingress)
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:18 G729/8000
a=rtpmap:101 telephone-event/8000
a=label:ingress-stream

m=audio 10002 RTP/AVP 0 8 18 101    ← Stream 2 (egress)
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:18 G729/8000
a=rtpmap:101 telephone-event/8000
a=label:egress-stream

m=audio 10004 RTP/AVP 0 8 18 101    ← Stream 3 (mixed)
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:18 G729/8000
a=rtpmap:101 telephone-event/8000
a=label:mixed-stream
```

**Notice:** 3 separate `m=audio` lines = 3 separate RTP streams

### The Result: 3 Recording Files

From that **1 SIP call**, you get **3 separate recordings**:

```
1-235771921681218ingress-stream.wav   (RTP on port 10000)
1-235771921681218egress-stream.wav    (RTP on port 10002)
1-235771921681218mixed-stream.wav     (RTP on port 10004)
```

## How to Verify in sngrep

### Step 1: Select the INVITE message
```
┌─ sngrep ────────────────────────────────────┐
│ INVITE  Oracle-SBC  →  siprec-server        │  ← Select this
│ 200 OK  siprec-server  →  Oracle-SBC        │
│ ACK     Oracle-SBC  →  siprec-server        │
│ BYE     Oracle-SBC  →  siprec-server        │
└─────────────────────────────────────────────┘
```

### Step 2: Press ENTER to view message details

### Step 3: Look for the SDP body

Scroll down past the SIP headers to find:

```
Content-Type: multipart/mixed;boundary=oracleBoundary456

--oracleBoundary456
Content-Type: application/sdp

v=0
o=OracleSBC 987654321 123456789 IN IP4 192.168.1.100
s=Oracle 3-Stream SIPREC Recording
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8 18 101    ← COUNT THESE!
...
m=audio 10002 RTP/AVP 0 8 18 101    ← Stream 2
...
m=audio 10004 RTP/AVP 0 8 18 101    ← Stream 3
...
```

**Count the `m=audio` lines** = Number of streams!

### Step 4: Check the metadata section

After the SDP, you'll see:

```
--oracleBoundary456
Content-Type: application/rs-metadata+xml

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns='urn:ietf:params:xml:ns:recording:1'>
  ...
  <stream stream_id="stream-ingress" session_id="...">
    <label>ingress-stream</label>
  </stream>
  <stream stream_id="stream-egress" session_id="...">
    <label>egress-stream</label>
  </stream>
  <stream stream_id="stream-mixed" session_id="...">
    <label>mixed-stream</label>
  </stream>
  ...
</recording>
```

**Count the `<stream>` elements** = Number of streams!

## Why Only 1 SIP Call?

SIPREC is designed this way for efficiency:

### Alternative (Not Used): 3 Separate SIP Calls
```
❌ Call 1: INVITE for ingress stream
❌ Call 2: INVITE for egress stream
❌ Call 3: INVITE for mixed stream
```

**Problems:**
- 3x SIP signaling overhead
- 3x call state management
- Complex correlation between calls
- Harder to maintain call association

### SIPREC Approach (Used): 1 SIP Call with Multiple Streams
```
✅ 1 SIP Call containing:
   - Stream 1: ingress
   - Stream 2: egress
   - Stream 3: mixed
```

**Benefits:**
- Single SIP dialog
- All streams share same call state
- Automatic correlation
- Efficient signaling

## Oracle SBC Specific Behavior

### Oracle Uses 3 Streams By Default

**Stream Labels:**
1. **ingress-stream** - Audio from the calling party (incoming to Oracle SBC)
2. **egress-stream** - Audio from the called party (outgoing from Oracle SBC)
3. **mixed-stream** - Combined audio from both parties

### Oracle Headers

Oracle adds specific headers to the INVITE:

```
X-Oracle-UCID: UCID-ORACLE-12345
X-Oracle-Conversation-ID: CONV-ORACLE-12345
```

These are extracted and stored by the SIPREC server for call correlation.

## Avaya SBC Specific Behavior

### Avaya Also Uses 3 Streams

**Stream Labels:**
1. **caller-stream** - Audio from the caller
2. **callee-stream** - Audio from the callee
3. **mixed-stream** - Combined audio

### Avaya Headers

```
X-Avaya-UCID: UCID-AVAYA-12345
User-Agent: Avaya-SM/7.1.3.0
```

## What Your SIPREC Server Does

When it receives that **1 INVITE** with **3 m=audio lines**:

1. **Parses the INVITE**
   - Detects 3 `m=audio` lines in SDP
   - Extracts 3 `<stream>` elements from metadata

2. **Allocates 3 RTP Ports**
   ```
   Port 10002 → ingress-stream
   Port 10004 → egress-stream
   Port 10006 → mixed-stream
   ```

3. **Creates 3 RTP Forwarders**
   - Each listens on its allocated port
   - Each writes to separate WAV file

4. **Sends 200 OK with 3 m=audio lines**
   ```sdp
   m=audio 10002 RTP/AVP 0 8
   a=recvonly
   a=label:ingress-stream

   m=audio 10004 RTP/AVP 0 8
   a=recvonly
   a=label:egress-stream

   m=audio 10006 RTP/AVP 0 8
   a=recvonly
   a=label:mixed-stream
   ```

5. **Records 3 Streams Simultaneously**
   - Ingress audio → `*ingress-stream.wav`
   - Egress audio → `*egress-stream.wav`
   - Mixed audio → `*mixed-stream.wav`

## Server Logs Show This Clearly

```json
{
  "message": "Detected audio streams in received SDP",
  "audio_stream_count": 3,
  "siprec": true
}

{
  "message": "Generating SDP response for multiple forwarders",
  "forwarder_count": 3,
  "media_desc_count": 3
}

{
  "message": "Created recording session from SIPREC metadata",
  "stream_count": 3,
  "participant_count": 2
}
```

## Verifying 3 Streams Are Working

### Check the Recordings Directory

```bash
ls -l /opt/siprec/recordings/

# You should see 3 files per call:
1-235771921681218ingress-stream.wav
1-235771921681218egress-stream.wav
1-235771921681218mixed-stream.wav
```

**3 files = 3 streams recorded successfully!**

### Check the Server Logs

```bash
sudo journalctl -u siprec --since '5 minutes ago' | grep stream_count

# Look for:
"stream_count": 3
"audio_stream_count": 3
"forwarder_count": 3
```

### Use sngrep to View RTP Streams

In sngrep, after selecting the call:
1. Press **F2** to view RTP streams
2. You should see **3 separate RTP sessions**:
   - Stream 1: Oracle → SIPREC (port 10002)
   - Stream 2: Oracle → SIPREC (port 10004)
   - Stream 3: Oracle → SIPREC (port 10006)

## Summary

| What You See | What's Actually Happening |
|--------------|---------------------------|
| **1 SIP call in sngrep** | Correct! SIPREC uses 1 SIP dialog |
| **3 m=audio lines in SDP** | 3 separate RTP streams |
| **3 different RTP ports** | Each stream on unique port |
| **3 recording files** | One file per stream |
| **3 STT streams** | Independent transcription per stream |

## Your Production Setup

When Oracle SBC connects to your SIPREC server:

**What Oracle sends:**
- 1 INVITE per call being recorded
- 3 m=audio lines in the SDP
- 3 RTP streams (ingress, egress, mixed)

**What you'll see in sngrep:**
- 1 call per recorded conversation
- (But 3 audio streams inside each call)

**What you'll get as output:**
- 3 WAV files per call
- 3 separate transcripts (if STT enabled)
- All properly labeled and correlated

**This is exactly how it should work!** ✅

The "3 calls per SIPREC" is really "3 streams per 1 SIPREC session".
