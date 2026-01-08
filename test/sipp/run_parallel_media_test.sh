#!/bin/bash
# Parallel media load test for hold/resume and transfer

SERVER="${1:-127.0.0.1}"
SCENARIO="${2:-siprec_hold_resume.xml}"
CALLS="${3:-100}"
CONCURRENT="${4:-20}"
SSH_USER="${SSH_USER:-$(whoami)}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== Parallel Media Load Test ==="
echo "Server: $SERVER:5060"
echo "Scenario: $SCENARIO"
echo "Total Calls: $CALLS"
echo "Concurrent: $CONCURRENT"
echo ""

# Clear old recordings
ssh $SSH_USER@$SERVER "sudo find /opt/siprec-server/recordings/ -name '*.wav' -delete" 2>/dev/null

SUCCESS=0
FAILED=0

run_single_call() {
    local call_num=$1
    local log_file="/tmp/sipp_test_${call_num}.log"

    # Start SIPp
    sipp $SERVER:5060 -sf $SCENARIO -m 1 -l 1 -trace_msg > "$log_file" 2>&1 &
    local sipp_pid=$!

    sleep 1

    # Get RTP port
    local msg_log=$(ls -t ${SCENARIO%.xml}_*_messages.log 2>/dev/null | head -1)
    if [ -n "$msg_log" ]; then
        local rtp_port=$(grep -E 'm=audio [0-9]+' "$msg_log" 2>/dev/null | tail -1 | sed 's/.*m=audio \([0-9]*\).*/\1/')
        if [ -n "$rtp_port" ] && [ "$rtp_port" -gt 0 ] 2>/dev/null; then
            # Send RTP for duration of call
            python3 rtp_audio_sender.py $SERVER $rtp_port test_audio_5min.ulaw 10 > /dev/null 2>&1 &
            local rtp_pid=$!
        fi
        rm -f "$msg_log" 2>/dev/null
    fi

    # Wait for SIPp
    wait $sipp_pid 2>/dev/null
    kill $rtp_pid 2>/dev/null

    # Check result
    if grep -q "Successful call.*1" "$log_file" 2>/dev/null; then
        echo "1"
    else
        echo "0"
    fi
    rm -f "$log_file"
}

export -f run_single_call
export SERVER SCENARIO

echo "Running $CALLS calls with $CONCURRENT concurrent..."
start_time=$(date +%s)

# Run calls in batches
for batch_start in $(seq 1 $CONCURRENT $CALLS); do
    batch_end=$((batch_start + CONCURRENT - 1))
    if [ $batch_end -gt $CALLS ]; then
        batch_end=$CALLS
    fi

    echo -n "  Batch $batch_start-$batch_end: "

    batch_success=0
    pids=""
    for i in $(seq $batch_start $batch_end); do
        run_single_call $i &
        pids="$pids $!"
    done

    # Wait and count results
    for pid in $pids; do
        result=$(wait $pid 2>/dev/null)
        if [ "$result" = "1" ]; then
            ((SUCCESS++))
            ((batch_success++))
        else
            ((FAILED++))
        fi
    done
    echo "$batch_success/$((batch_end - batch_start + 1)) succeeded"
done

end_time=$(date +%s)
duration=$((end_time - start_time))

echo ""
echo "=== Results ==="
echo "Duration: ${duration}s"
echo "Success: $SUCCESS/$CALLS"
echo "Failed: $FAILED/$CALLS"
echo "Success Rate: $(echo "scale=1; $SUCCESS * 100 / $CALLS" | bc)%"
echo ""
echo "=== Checking Recordings ==="
ssh $SSH_USER@$SERVER "echo 'Total files:' && sudo ls /opt/siprec-server/recordings/ | wc -l && echo '' && echo 'Sample sizes:' && sudo ls -lh /opt/siprec-server/recordings/ | head -10"
