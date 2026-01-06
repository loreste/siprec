#!/bin/bash
# SIPREC Media Test - Coordinates SIP signaling with RTP streaming

SERVER_IP="192.227.78.77"
SERVER_SIP_PORT="5060"
LOCAL_IP=$(ifconfig | grep -A5 'en' | grep 'inet ' | head -1 | awk '{print $2}')
DURATION=5

echo "=== SIPREC Media Test ==="
echo "Server: $SERVER_IP:$SERVER_SIP_PORT"
echo "Local IP: $LOCAL_IP"
echo "Duration: ${DURATION}s per call"
echo ""

# Create a scenario with longer pause for RTP
cat > /tmp/siprec_media_test.xml << 'XMLEOF'
<?xml version="1.0" encoding="ISO-8859-1" ?>
<!DOCTYPE scenario SYSTEM "sipp.dtd">
<scenario name="SIPREC Media Test">
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
      Subject: Media Test
      Content-Type: multipart/mixed;boundary=boundary123
      Content-Length: [len]

--boundary123
Content-Type: application/sdp
Content-Disposition: session;handling=required

v=0
o=user1 53655765 2353687637 IN IP[local_ip_type] [local_ip]
s=-
c=IN IP[media_ip_type] [media_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000

--boundary123
Content-Type: application/rs-metadata+xml
Content-Disposition: recording-session

<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session session_id="[call_id]"><sipSessionID>[call_id]</sipSessionID></session>
  <participant participant_id="p1"><nameID aor="sip:alice@test.com"><name>Alice</name></nameID></participant>
  <participant participant_id="p2"><nameID aor="sip:bob@test.com"><name>Bob</name></nameID></participant>
  <stream stream_id="s1" session_id="[call_id]"><label>audio</label></stream>
  <sessionrecordingassoc session_id="[call_id]"/>
  <participantsessionassoc participant_id="p1" session_id="[call_id]"/>
  <participantsessionassoc participant_id="p2" session_id="[call_id]"/>
  <participantstreamassoc participant_id="p1"><send>s1</send></participantstreamassoc>
  <participantstreamassoc participant_id="p2"><recv>s1</recv></participantstreamassoc>
</recording>
--boundary123--
    ]]>
  </send>
  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>
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
  <pause milliseconds="7000"/>
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
XMLEOF

echo "Starting SIPp in background..."
sipp -sf /tmp/siprec_media_test.xml $SERVER_IP:$SERVER_SIP_PORT -s siprec -m 1 -trace_msg -message_file /tmp/sipp_messages.log -bg

sleep 2  # Wait for call to establish

# Extract RTP port from SDP response
RTP_PORT=$(grep -o 'm=audio [0-9]*' /tmp/sipp_messages.log 2>/dev/null | tail -1 | awk '{print $2}')

if [ -z "$RTP_PORT" ]; then
    echo "Could not extract RTP port from SDP, using default 13544"
    RTP_PORT=13544
fi

echo "Server RTP port: $RTP_PORT"
echo "Sending RTP stream for ${DURATION}s..."

# Send RTP
python3 test/sipp/rtp_sender.py $SERVER_IP $RTP_PORT $DURATION

echo ""
echo "Waiting for call to complete..."
sleep 3

echo "Test complete!"
