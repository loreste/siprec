#!/bin/bash
# Run SIPREC load test with actual RTP media
# Requires sudo for SIPp to send RTP packets

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER="${1:-192.227.78.77}"
PORT="${2:-5060}"
CALLS="${3:-100}"
RATE="${4:-20}"
DURATION="${5:-30}"

# Update scenario with desired duration
PAUSE_MS=$((DURATION * 1000))

echo "=== SIPREC Media Load Test ==="
echo "Server: $SERVER:$PORT"
echo "Calls: $CALLS"
echo "Rate: $RATE cps"
echo "Duration: ${DURATION}s per call"
echo ""

# Check if pcap file exists
if [ ! -f "$SCRIPT_DIR/test_audio_5min.pcap" ]; then
    echo "Error: test_audio_5min.pcap not found"
    echo "Please run the audio preparation scripts first"
    exit 1
fi

# Create a temporary scenario with the pcap reference
cat > "$SCRIPT_DIR/siprec_media_test_temp.xml" << 'EOF'
<?xml version="1.0" encoding="ISO-8859-1" ?>
<!DOCTYPE scenario SYSTEM "sipp.dtd">

<scenario name="SIPREC with RTP Media">

  <send retrans="500">
    <![CDATA[
      INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[pid]SIPpTag00[call_number]
      To: sut <sip:[service]@[remote_ip]:[remote_port]>
      Call-ID: [call_id]
      CSeq: 1 INVITE
      Contact: sip:sipp@[local_ip]:[local_port]
      Max-Forwards: 70
      Subject: Media Test Call [call_number]
      Content-Type: multipart/mixed;boundary=boundary123
      Content-Length: [len]

--boundary123
Content-Type: application/sdp
Content-Disposition: session;handling=required

v=0
o=user1 53655765 2353687637 IN IP[local_ip_type] [local_ip]
s=SIPREC Media Test
c=IN IP[media_ip_type] [media_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000
a=ptime:20
a=sendrecv

--boundary123
Content-Type: application/rs-metadata+xml
Content-Disposition: recording-session

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session session_id="[call_id]">
    <sipSessionID>[call_id]</sipSessionID>
  </session>
  <participant participant_id="caller-[call_number]">
    <nameID aor="sip:caller[call_number]@example.com">
      <name xml:lang="en">Caller [call_number]</name>
    </nameID>
  </participant>
  <participant participant_id="callee-[call_number]">
    <nameID aor="sip:callee[call_number]@example.com">
      <name xml:lang="en">Callee [call_number]</name>
    </nameID>
  </participant>
  <stream stream_id="audio-[call_number]" session_id="[call_id]">
    <label>main-audio</label>
  </stream>
</recording>
--boundary123--
    ]]>
  </send>

  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>
  <recv response="183" optional="true"/>
  <recv response="200" rtd="true"/>

  <send>
    <![CDATA[
      ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[pid]SIPpTag00[call_number]
      To: sut <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
      Call-ID: [call_id]
      CSeq: 1 ACK
      Contact: sip:sipp@[local_ip]:[local_port]
      Max-Forwards: 70
      Content-Length: 0

    ]]>
  </send>

  <!-- Play RTP audio -->
  <nop>
    <action>
      <exec play_pcap_audio="test_audio_5min.pcap"/>
    </action>
  </nop>

  <!-- Wait for call duration -->
  <pause milliseconds="PAUSE_PLACEHOLDER"/>

  <send retrans="500">
    <![CDATA[
      BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
      Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
      From: sipp <sip:sipp@[local_ip]:[local_port]>;tag=[pid]SIPpTag00[call_number]
      To: sut <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
      Call-ID: [call_id]
      CSeq: 2 BYE
      Contact: sip:sipp@[local_ip]:[local_port]
      Max-Forwards: 70
      Content-Length: 0

    ]]>
  </send>

  <recv response="200" crlf="true"/>

</scenario>
EOF

# Replace pause duration
sed -i.bak "s/PAUSE_PLACEHOLDER/$PAUSE_MS/" "$SCRIPT_DIR/siprec_media_test_temp.xml"

echo "Starting SIPp with RTP media playback..."
echo "Note: This requires sudo for raw socket access"
echo ""

cd "$SCRIPT_DIR"

sudo sipp "$SERVER:$PORT" \
    -t u1 \
    -sf siprec_media_test_temp.xml \
    -l "$CALLS" \
    -m "$CALLS" \
    -r "$RATE" \
    -timeout $((DURATION + 120)) \
    -trace_stat \
    -stf media_test_stats.csv

rm -f siprec_media_test_temp.xml siprec_media_test_temp.xml.bak

echo ""
echo "Test complete!"
