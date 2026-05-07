// Package test provides live integration tests that send real SIPREC SIP traffic
// to a running server instance. Run with:
//
//	go test ./test/ -run TestLiveSIPREC -v -count=1 -tags live
//
// Requires a SIPREC server listening on 127.0.0.1:5061 (UDP) and HTTP API on :8081.
//go:build live

package test

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"testing"
	"time"

	sipparser "github.com/emiago/sipgo/sip"
)

const (
	sipServerAddr = "127.0.0.1:5061"
	httpAPIAddr   = "http://127.0.0.1:8081"
)

// buildSIPRECInvite constructs a well-formed SIPREC INVITE with multipart body.
func buildSIPRECInvite(callID, fromTag, branch string, localPort int, codecPT int, codecName string) []byte {
	sdp := fmt.Sprintf(
		"v=0\r\n"+
			"o=SBC 100 1 IN IP4 127.0.0.1\r\n"+
			"s=SIPREC\r\n"+
			"c=IN IP4 127.0.0.1\r\n"+
			"t=0 0\r\n"+
			"m=audio %d RTP/AVP %d\r\n"+
			"a=rtpmap:%d %s/8000\r\n"+
			"a=sendonly\r\n"+
			"a=label:0\r\n"+
			"a=ptime:20\r\n",
		localPort, codecPT, codecPT, codecName)

	metadata := fmt.Sprintf(
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n"+
			"<recording xmlns=\"urn:ietf:params:xml:ns:recording:1\" session=\"%s\" state=\"active\">\r\n"+
			"  <session session_id=\"%s\">\r\n"+
			"    <sipSessionID>%s@test</sipSessionID>\r\n"+
			"  </session>\r\n"+
			"  <participant participant_id=\"caller\">\r\n"+
			"    <aor>sip:alice@example.com</aor>\r\n"+
			"    <name>Alice</name>\r\n"+
			"  </participant>\r\n"+
			"  <participant participant_id=\"callee\">\r\n"+
			"    <aor>sip:bob@example.com</aor>\r\n"+
			"    <name>Bob</name>\r\n"+
			"  </participant>\r\n"+
			"  <stream stream_id=\"s1\" label=\"0\">\r\n"+
			"    <label>0</label>\r\n"+
			"  </stream>\r\n"+
			"  <sessionrecordingassoc session_id=\"%s\"/>\r\n"+
			"</recording>",
		callID, callID, callID, callID)

	boundary := "test-boundary-42"
	body := fmt.Sprintf(
		"--%s\r\n"+
			"Content-Type: application/sdp\r\n"+
			"Content-Disposition: session;handling=required\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s\r\n"+
			"Content-Type: application/rs-metadata+xml\r\n"+
			"Content-Disposition: recording-session;handling=required\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s--\r\n",
		boundary, sdp, boundary, metadata, boundary)

	invite := fmt.Sprintf(
		"INVITE sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=%s;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=%s\r\n"+
			"To: <sip:recorder@127.0.0.1>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:sbc@127.0.0.1:15060;transport=udp>\r\n"+
			"Content-Type: multipart/mixed;boundary=\"%s\"\r\n"+
			"Max-Forwards: 70\r\n"+
			"User-Agent: SIPREC-Test/1.0\r\n"+
			"Require: siprec\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n"+
			"%s",
		branch, fromTag, callID, boundary, len(body), body)

	return []byte(invite)
}

func buildACK(callID, fromTag, toTag, branch string) []byte {
	ack := fmt.Sprintf(
		"ACK sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=%s;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=%s\r\n"+
			"To: <sip:recorder@127.0.0.1>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 ACK\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n"+
			"\r\n",
		branch, fromTag, toTag, callID)
	return []byte(ack)
}

func buildBYE(callID, fromTag, toTag, branch string) []byte {
	bye := fmt.Sprintf(
		"BYE sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=%s;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=%s\r\n"+
			"To: <sip:recorder@127.0.0.1>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 2 BYE\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n"+
			"\r\n",
		branch, fromTag, toTag, callID)
	return []byte(bye)
}

// sendAndReceive sends a SIP message via UDP and waits for a response.
func sendAndReceive(t *testing.T, conn *net.UDPConn, msg []byte, timeout time.Duration) string {
	t.Helper()
	_, err := conn.Write(msg)
	if err != nil {
		t.Fatalf("Failed to send SIP message: %v", err)
	}
	buf := make([]byte, 65535)
	conn.SetReadDeadline(time.Now().Add(timeout))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Failed to read SIP response: %v", err)
	}
	return string(buf[:n])
}

// collectResponses reads all responses within a timeout window.
func collectResponses(t *testing.T, conn *net.UDPConn, timeout time.Duration) []string {
	t.Helper()
	var responses []string
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 65535)
	for {
		conn.SetReadDeadline(deadline)
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		responses = append(responses, string(buf[:n]))
	}
	return responses
}

// extractToTag pulls the To-tag from a SIP response.
func extractToTag(response string) string {
	for _, line := range strings.Split(response, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "to:") {
			if idx := strings.Index(line, "tag="); idx != -1 {
				tag := line[idx+4:]
				if end := strings.IndexAny(tag, ";>\r\n "); end != -1 {
					return tag[:end]
				}
				return tag
			}
		}
	}
	return ""
}

// extractStatusCode gets the response status code.
func extractStatusCode(response string) int {
	if len(response) < 12 {
		return 0
	}
	// SIP/2.0 XXX ...
	var code int
	fmt.Sscanf(response, "SIP/2.0 %d", &code)
	return code
}

// buildRTPPacket creates a minimal RTP packet with the given payload type.
func buildRTPPacket(seq uint16, ts uint32, ssrc uint32, pt byte, payload []byte) []byte {
	pkt := make([]byte, 12+len(payload))
	pkt[0] = 0x80         // V=2, no padding, no extension, no CSRC
	pkt[1] = pt           // payload type, no marker
	binary.BigEndian.PutUint16(pkt[2:], seq)
	binary.BigEndian.PutUint32(pkt[4:], ts)
	binary.BigEndian.PutUint32(pkt[8:], ssrc)
	copy(pkt[12:], payload)
	return pkt
}

// -----------------------------------------------------------------------
// Test: Full SIPREC call flow with PCMU (G.711u, PT=0)
// -----------------------------------------------------------------------
func TestLiveSIPREC_PCMU_FullCallFlow(t *testing.T) {
	runFullCallFlow(t, "live-pcmu-call", 0, "PCMU")
}

// -----------------------------------------------------------------------
// Test: Full SIPREC call flow with PCMA (G.711a, PT=8)
// -----------------------------------------------------------------------
func TestLiveSIPREC_PCMA_FullCallFlow(t *testing.T) {
	runFullCallFlow(t, "live-pcma-call", 8, "PCMA")
}

// -----------------------------------------------------------------------
// Test: Full SIPREC call flow with G.729 (PT=18)
// -----------------------------------------------------------------------
func TestLiveSIPREC_G729_FullCallFlow(t *testing.T) {
	runFullCallFlow(t, "live-g729-call", 18, "G729")
}

// -----------------------------------------------------------------------
// Test: INVITE missing SDP → expect 400
// -----------------------------------------------------------------------
func TestLiveSIPREC_MissingSDP(t *testing.T) {
	conn := dialSIP(t)
	defer conn.Close()

	callID := fmt.Sprintf("live-missing-sdp-%d", time.Now().UnixNano())
	fromTag := "miss-sdp-from"
	branch := "z9hG4bK-miss-sdp"
	boundary := "test-boundary-42"

	metadata :=
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n" +
			"<recording xmlns=\"urn:ietf:params:xml:ns:recording:1\" session=\"s1\" state=\"active\">\r\n" +
			"  <session session_id=\"s1\"><sipSessionID>s1@test</sipSessionID></session>\r\n" +
			"  <participant participant_id=\"p1\"><aor>sip:alice@example.com</aor></participant>\r\n" +
			"  <stream stream_id=\"s1\" label=\"0\"><label>0</label></stream>\r\n" +
			"  <sessionrecordingassoc session_id=\"s1\"/>\r\n" +
			"</recording>"
	body := fmt.Sprintf(
		"--%s\r\n"+
			"Content-Type: application/rs-metadata+xml\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s--\r\n",
		boundary, metadata, boundary)

	invite := fmt.Sprintf(
		"INVITE sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=%s;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=%s\r\n"+
			"To: <sip:recorder@127.0.0.1>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:sbc@127.0.0.1:15060>\r\n"+
			"Content-Type: multipart/mixed;boundary=\"%s\"\r\n"+
			"Require: siprec\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n"+
			"%s",
		branch, fromTag, callID, boundary, len(body), body)

	responses := sendAndCollect(t, conn, []byte(invite), 3*time.Second)
	assertHasStatus(t, responses, 400, "missing SDP should get 400")
	t.Logf("Correctly rejected: got 400 for missing SDP")
}

// -----------------------------------------------------------------------
// Test: BYE for unknown Call-ID → expect 481
// -----------------------------------------------------------------------
func TestLiveSIPREC_UnknownBYE(t *testing.T) {
	conn := dialSIP(t)
	defer conn.Close()

	callID := fmt.Sprintf("live-unknown-bye-%d", time.Now().UnixNano())
	bye := buildBYE(callID, "tag-a", "tag-b", "z9hG4bK-unknown-bye")

	responses := sendAndCollect(t, conn, bye, 3*time.Second)
	assertHasStatus(t, responses, 481, "BYE for unknown call should get 481")
	t.Logf("Correctly rejected: got 481 for unknown BYE")
}

// -----------------------------------------------------------------------
// Test: OPTIONS keepalive → expect 200
// -----------------------------------------------------------------------
func TestLiveSIPREC_Options(t *testing.T) {
	conn := dialSIP(t)
	defer conn.Close()

	options := fmt.Sprintf(
		"OPTIONS sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=z9hG4bK-options;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=opts\r\n"+
			"To: <sip:recorder@127.0.0.1>\r\n"+
			"Call-ID: live-options-%d\r\n"+
			"CSeq: 1 OPTIONS\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n"+
			"\r\n",
		time.Now().UnixNano())

	responses := sendAndCollect(t, conn, []byte(options), 3*time.Second)
	assertHasStatus(t, responses, 200, "OPTIONS should get 200")
	t.Logf("OPTIONS keepalive: got 200 OK")
}

// -----------------------------------------------------------------------
// Test: Concurrent calls don't interfere with each other
// -----------------------------------------------------------------------
func TestLiveSIPREC_ConcurrentCalls(t *testing.T) {
	const numCalls = 5
	done := make(chan error, numCalls)

	for i := 0; i < numCalls; i++ {
		go func(idx int) {
			callID := fmt.Sprintf("live-concurrent-%d-%d", idx, time.Now().UnixNano())
			err := runCallFlow(callID, 0, "PCMU", 500*time.Millisecond)
			done <- err
		}(i)
	}

	for i := 0; i < numCalls; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("concurrent call %d failed: %v", i, err)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("concurrent call %d timed out", i)
		}
	}
	t.Logf("All %d concurrent calls completed successfully", numCalls)
}

// -----------------------------------------------------------------------
// Test: Re-INVITE mid-call updates session
// -----------------------------------------------------------------------
func TestLiveSIPREC_ReInvite(t *testing.T) {
	conn := dialSIP(t)
	defer conn.Close()

	callID := fmt.Sprintf("live-reinvite-%d", time.Now().UnixNano())
	fromTag := "reinv-from"
	branch := "z9hG4bK-reinvite-1"

	// Send initial INVITE
	invite := buildSIPRECInvite(callID, fromTag, branch, 30000, 0, "PCMU")
	responses := sendAndCollect(t, conn, invite, 3*time.Second)
	toTag := ""
	for _, r := range responses {
		if code := extractStatusCode(r); code == 200 {
			toTag = extractToTag(r)
			break
		}
	}
	if toTag == "" {
		t.Fatalf("Did not get 200 OK with To-tag from initial INVITE; responses: %v", statusCodes(responses))
	}

	// Send ACK
	ack := buildACK(callID, fromTag, toTag, "z9hG4bK-reinvite-ack")
	conn.Write(ack)
	time.Sleep(200 * time.Millisecond)

	// Send re-INVITE with updated SDP (different port)
	reInviteBranch := "z9hG4bK-reinvite-2"
	reInvite := buildReINVITE(callID, fromTag, toTag, reInviteBranch, 30002, 0, "PCMU")
	reResponses := sendAndCollect(t, conn, reInvite, 3*time.Second)
	assertHasStatus(t, reResponses, 200, "re-INVITE should get 200")
	t.Logf("re-INVITE accepted with 200 OK")

	// Send ACK for re-INVITE
	ack2 := buildACK(callID, fromTag, toTag, "z9hG4bK-reinvite-ack2")
	conn.Write(ack2)
	time.Sleep(100 * time.Millisecond)

	// BYE
	bye := buildBYE(callID, fromTag, toTag, "z9hG4bK-reinvite-bye")
	byeResponses := sendAndCollect(t, conn, bye, 3*time.Second)
	assertHasStatus(t, byeResponses, 200, "BYE should get 200")
	t.Logf("Full re-INVITE flow completed successfully")
}

// =======================================================================
// Helpers
// =======================================================================

func dialSIP(t *testing.T) *net.UDPConn {
	t.Helper()
	raddr, err := net.ResolveUDPAddr("udp", sipServerAddr)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendAndCollect(t *testing.T, conn *net.UDPConn, msg []byte, timeout time.Duration) []string {
	t.Helper()
	_, err := conn.Write(msg)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	return collectResponses(t, conn, timeout)
}

func assertHasStatus(t *testing.T, responses []string, expected int, ctx string) {
	t.Helper()
	for _, r := range responses {
		if extractStatusCode(r) == expected {
			return
		}
	}
	t.Fatalf("%s: expected status %d in responses %v", ctx, expected, statusCodes(responses))
}

func statusCodes(responses []string) []int {
	codes := make([]int, len(responses))
	for i, r := range responses {
		codes[i] = extractStatusCode(r)
	}
	return codes
}

func buildReINVITE(callID, fromTag, toTag, branch string, localPort int, codecPT int, codecName string) []byte {
	sdp := fmt.Sprintf(
		"v=0\r\n"+
			"o=SBC 100 2 IN IP4 127.0.0.1\r\n"+
			"s=SIPREC\r\n"+
			"c=IN IP4 127.0.0.1\r\n"+
			"t=0 0\r\n"+
			"m=audio %d RTP/AVP %d\r\n"+
			"a=rtpmap:%d %s/8000\r\n"+
			"a=sendonly\r\n"+
			"a=label:0\r\n"+
			"a=ptime:20\r\n",
		localPort, codecPT, codecPT, codecName)

	metadata := fmt.Sprintf(
		"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n"+
			"<recording xmlns=\"urn:ietf:params:xml:ns:recording:1\" session=\"%s\" state=\"active\" sequence=\"2\">\r\n"+
			"  <session session_id=\"%s\"><sipSessionID>%s@test</sipSessionID></session>\r\n"+
			"  <participant participant_id=\"caller\"><aor>sip:alice@example.com</aor></participant>\r\n"+
			"  <participant participant_id=\"callee\"><aor>sip:bob@example.com</aor></participant>\r\n"+
			"  <stream stream_id=\"s1\" label=\"0\"><label>0</label></stream>\r\n"+
			"  <sessionrecordingassoc session_id=\"%s\"/>\r\n"+
			"</recording>",
		callID, callID, callID, callID)

	boundary := "test-boundary-42"
	body := fmt.Sprintf(
		"--%s\r\n"+
			"Content-Type: application/sdp\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s\r\n"+
			"Content-Type: application/rs-metadata+xml\r\n"+
			"\r\n"+
			"%s\r\n"+
			"--%s--\r\n",
		boundary, sdp, boundary, metadata, boundary)

	invite := fmt.Sprintf(
		"INVITE sip:recorder@127.0.0.1:5060 SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP 127.0.0.1:15060;branch=%s;rport\r\n"+
			"From: <sip:sbc@127.0.0.1>;tag=%s\r\n"+
			"To: <sip:recorder@127.0.0.1>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 2 INVITE\r\n"+
			"Contact: <sip:sbc@127.0.0.1:15060>\r\n"+
			"Content-Type: multipart/mixed;boundary=\"%s\"\r\n"+
			"Require: siprec\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: %d\r\n"+
			"\r\n"+
			"%s",
		branch, fromTag, toTag, callID, boundary, len(body), body)

	return []byte(invite)
}

// runFullCallFlow is the shared implementation for codec-specific call flow tests.
func runFullCallFlow(t *testing.T, prefix string, codecPT int, codecName string) {
	t.Helper()
	conn := dialSIP(t)
	defer conn.Close()

	callID := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	fromTag := prefix + "-from"
	branch := "z9hG4bK-" + prefix

	// Pick a random local port for our RTP "sender"
	rtpPort := 30000 + rand.Intn(1000)

	t.Logf("=== INVITE (%s, PT=%d) call_id=%s", codecName, codecPT, callID)
	invite := buildSIPRECInvite(callID, fromTag, branch, rtpPort, codecPT, codecName)
	responses := sendAndCollect(t, conn, invite, 5*time.Second)

	codes := statusCodes(responses)
	t.Logf("    Responses: %v", codes)

	// Should have 180 and 200
	assertHasStatus(t, responses, 200, "INVITE should get 200 OK")

	// Extract To-tag from 200 OK
	toTag := ""
	for _, r := range responses {
		if extractStatusCode(r) == 200 {
			toTag = extractToTag(r)
			// Verify response has multipart body with SDP
			if !strings.Contains(r, "application/sdp") {
				t.Logf("    WARNING: 200 OK may not contain SDP")
			}
			if strings.Contains(r, "rs-metadata") {
				t.Logf("    200 OK contains rs-metadata response (good)")
			}
			break
		}
	}
	if toTag == "" {
		t.Fatalf("No To-tag in 200 OK")
	}
	t.Logf("    To-tag: %s", toTag)

	// === ACK ===
	t.Logf("=== ACK")
	ack := buildACK(callID, fromTag, toTag, "z9hG4bK-"+prefix+"-ack")
	conn.Write(ack)
	time.Sleep(200 * time.Millisecond)

	// === Send some RTP packets ===
	t.Logf("=== Sending RTP packets (%s)", codecName)
	// We need to find the server's RTP port from the 200 OK SDP response
	// For now, just verify the dialog is established — RTP goes to the port in the SDP answer
	serverRTPPort := extractRTPPort(t, responses)
	if serverRTPPort > 0 {
		sendTestRTP(t, serverRTPPort, codecPT, codecName, 50)
		t.Logf("    Sent 50 RTP packets to port %d", serverRTPPort)
	} else {
		t.Logf("    Skipping RTP (could not extract server port from SDP)")
	}

	// === BYE ===
	t.Logf("=== BYE")
	bye := buildBYE(callID, fromTag, toTag, "z9hG4bK-"+prefix+"-bye")
	byeResponses := sendAndCollect(t, conn, bye, 3*time.Second)
	assertHasStatus(t, byeResponses, 200, "BYE should get 200 OK")
	t.Logf("    BYE accepted: 200 OK")
	t.Logf("=== Call completed successfully (%s)", codecName)
}

// runCallFlow is a non-test version for goroutines in concurrent tests.
func runCallFlow(callID string, codecPT int, codecName string, rtpDuration time.Duration) error {
	raddr, err := net.ResolveUDPAddr("udp", sipServerAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	fromTag := "conc-" + callID[len(callID)-8:]
	branch := "z9hG4bK-conc-" + callID[len(callID)-8:]
	rtpPort := 30000 + rand.Intn(1000)

	// INVITE
	invite := buildSIPRECInvite(callID, fromTag, branch, rtpPort, codecPT, codecName)
	conn.Write(invite)

	responses := collectAll(conn, 5*time.Second)
	toTag := ""
	got200 := false
	for _, r := range responses {
		code := extractStatusCode(r)
		if code == 200 {
			toTag = extractToTag(r)
			got200 = true
			break
		}
	}
	if !got200 || toTag == "" {
		return fmt.Errorf("call %s: no 200 OK (got %v)", callID, statusCodesStr(responses))
	}

	// ACK
	conn.Write(buildACK(callID, fromTag, toTag, branch+"-ack"))
	time.Sleep(rtpDuration)

	// BYE
	conn.Write(buildBYE(callID, fromTag, toTag, branch+"-bye"))
	byeResponses := collectAll(conn, 3*time.Second)
	for _, r := range byeResponses {
		if extractStatusCode(r) == 200 {
			return nil
		}
	}
	return fmt.Errorf("call %s: no 200 OK for BYE (got %v)", callID, statusCodesStr(byeResponses))
}

func collectAll(conn *net.UDPConn, timeout time.Duration) []string {
	var responses []string
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 65535)
	for {
		conn.SetReadDeadline(deadline)
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		responses = append(responses, string(buf[:n]))
	}
	return responses
}

func statusCodesStr(responses []string) []int {
	codes := make([]int, len(responses))
	for i, r := range responses {
		codes[i] = extractStatusCode(r)
	}
	return codes
}

// extractRTPPort parses the server's RTP port from the SDP in the 200 OK response.
func extractRTPPort(t *testing.T, responses []string) int {
	t.Helper()
	for _, r := range responses {
		if extractStatusCode(r) != 200 {
			continue
		}
		// Find m=audio <port> in the response body
		for _, line := range strings.Split(r, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "m=audio ") {
				var port int
				fmt.Sscanf(line, "m=audio %d", &port)
				if port > 0 {
					return port
				}
			}
		}
	}
	return 0
}

// sendTestRTP sends test RTP packets to the server's RTP port.
func sendTestRTP(t *testing.T, port int, pt int, codecName string, count int) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Logf("Cannot resolve RTP addr: %v", err)
		return
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Logf("Cannot dial RTP: %v", err)
		return
	}
	defer conn.Close()

	ssrc := rand.Uint32()
	var payloadSize int
	switch codecName {
	case "G729":
		payloadSize = 10 // 10 bytes for 10ms G.729 frame
	default:
		payloadSize = 160 // 160 bytes for 20ms G.711
	}

	payload := make([]byte, payloadSize)
	// Fill with silence pattern
	if codecName == "PCMU" {
		for i := range payload {
			payload[i] = 0xFF // μ-law silence
		}
	} else if codecName == "PCMA" {
		for i := range payload {
			payload[i] = 0xD5 // A-law silence
		}
	}
	// G.729 silence is all zeros which is the default

	for i := 0; i < count; i++ {
		pkt := buildRTPPacket(uint16(i), uint32(i*160), ssrc, byte(pt), payload)
		conn.Write(pkt)
		time.Sleep(20 * time.Millisecond) // 20ms ptime
	}
}

// Ensure sipparser is used (imported for build consistency with test suite)
var _ = sipparser.INVITE
