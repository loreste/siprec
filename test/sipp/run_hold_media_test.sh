#!/bin/bash
# Test hold/resume with actual RTP media

SERVER="${1:-127.0.0.1}"
PORT="${2:-5060}"
CALLS="${3:-10}"
SSH_USER="${SSH_USER:-$(whoami)}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Hold/Resume Media Test ==="
echo "Server: $SERVER:$PORT"
echo "Calls: $CALLS"
echo ""

# Clear old recordings
ssh $SSH_USER@$SERVER "sudo find /opt/siprec-server/recordings/ -name '*.wav' -delete" 2>/dev/null

# Run multiple calls sequentially with RTP
for i in $(seq 1 $CALLS); do
    echo "--- Call $i/$CALLS ---"

    # Start SIPp in background
    sipp $SERVER:$PORT -sf siprec_hold_resume.xml -m 1 -l 1 -trace_msg > /tmp/sipp_hold_$i.log 2>&1 &
    SIPP_PID=$!

    sleep 2

    # Get RTP port from message log
    MSG_LOG=$(ls -t siprec_hold_resume_*_messages.log 2>/dev/null | head -1)
    if [ -n "$MSG_LOG" ]; then
        RTP_PORT=$(grep -E 'm=audio [0-9]+' "$MSG_LOG" | tail -1 | sed 's/.*m=audio \([0-9]*\).*/\1/')
        if [ -n "$RTP_PORT" ]; then
            echo "  RTP port: $RTP_PORT"
            # Send RTP for 10 seconds (covers the hold/resume cycle)
            python3 rtp_audio_sender.py $SERVER $RTP_PORT test_audio_5min.ulaw 10 &
            RTP_PID=$!
        fi
    fi

    # Wait for SIPp to complete
    wait $SIPP_PID 2>/dev/null
    kill $RTP_PID 2>/dev/null

    # Check result
    if grep -q "Successful call.*1" /tmp/sipp_hold_$i.log 2>/dev/null; then
        echo "  Result: SUCCESS"
    else
        echo "  Result: FAILED"
    fi

    rm -f siprec_hold_resume_*_messages.log 2>/dev/null
done

echo ""
echo "=== Checking Recordings ==="
ssh $SSH_USER@$SERVER "sudo ls -lh /opt/siprec-server/recordings/ | head -20"
