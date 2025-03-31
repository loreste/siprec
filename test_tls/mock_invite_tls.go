package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"time"
)

func main() {
	config := &tls.Config{
		InsecureSkipVerify: true,
	}

	conn, err := tls.Dial("tcp", "127.0.0.1:5063", config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Generate unique identifiers for the call
	callID := fmt.Sprintf("test-call-%d", time.Now().Unix())
	fromTag := fmt.Sprintf("tag-%d", time.Now().Unix())
	branchID := fmt.Sprintf("z9hG4bK-%d", time.Now().Unix())
	sessionID := fmt.Sprintf("session-%d", time.Now().Unix())
	
	// Create a timestamp for the SIPREC metadata
	timestamp := time.Now().Format(time.RFC3339)

	// First, send a SIPREC INVITE with multipart content
	fmt.Println("Sending SIPREC INVITE over TLS...")
	sendSiprecInvite(conn, branchID, fromTag, callID, sessionID, timestamp)

	// Wait briefly to allow processing
	time.Sleep(1 * time.Second)

	// Then try a regular INVITE with SDP only
	fmt.Println("\nSending regular INVITE over TLS...")
	sendRegularInvite(conn, branchID, fromTag, callID)

	// Wait briefly to allow processing
	time.Sleep(1 * time.Second)

	// Finally, send an OPTIONS request
	fmt.Println("\nSending OPTIONS over TLS...")
	sendOptions(conn, branchID, fromTag, callID)
}

func sendSiprecInvite(conn *tls.Conn, branchID, fromTag, callID, sessionID, timestamp string) {
	// Create SDP content for media negotiation
	sdpContent := "v=0\r\n" +
		"o=- 1141236 1141236 IN IP4 127.0.0.1\r\n" +
		"s=SIPREC Call\r\n" +
		"c=IN IP4 127.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 10000 RTP/AVP 0 8\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=rtpmap:8 PCMA/8000\r\n" +
		"a=sendrecv\r\n" +
		"a=ptime:20\r\n" +
		"a=ssrc:1234567890 cname:test-session\r\n" +
		"a=recvonly\r\n"

	// Create SIPREC XML metadata according to RFC 7865/7866
	siprecXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <datamode>complete</datamode>
  <session session_id="%s">
    <sipSessionID>%s</sipSessionID>
    <start-time>%s</start-time>
  </session>
  <participantstreamlist>
    <participantstream participant_id="1">
      <nameID aor="sip:caller@example.com">
        <n>Caller</n>
      </nameID>
    </participantstream>
    <participantstream participant_id="2">
      <nameID aor="sip:callee@example.com">
        <n>Callee</n>
      </nameID>
    </participantstream>
  </participantstreamlist>
  <stream stream_id="1" session_id="%s">
    <label>audio</label>
  </stream>
  <sessionrecordingassoc session_id="%s">
    <associate-time>%s</associate-time>
  </sessionrecordingassoc>
  <participantsessionassoc participant_id="1" session_id="%s">
    <associate-time>%s</associate-time>
  </participantsessionassoc>
  <participantsessionassoc participant_id="2" session_id="%s">
    <associate-time>%s</associate-time>
  </participantsessionassoc>
  <participantstreamassoc participant_id="1" stream_id="1">
    <associate-time>%s</associate-time>
    <send>true</send>
  </participantstreamassoc>
  <participantstreamassoc participant_id="2" stream_id="1">
    <associate-time>%s</associate-time>
    <recv>true</recv>
  </participantstreamassoc>
</recording>`, sessionID, callID, timestamp, sessionID, sessionID, timestamp, sessionID, timestamp, sessionID, timestamp, timestamp, timestamp)

	// Create a MIME multipart boundary
	boundary := "boundary1234"

	// Create multipart body with both SDP and SIPREC XML
	multipartBody := fmt.Sprintf("--%s\r\n"+
		"Content-Type: application/sdp\r\n"+
		"\r\n"+
		"%s\r\n"+
		"--%s\r\n"+
		"Content-Type: application/rs-metadata+xml\r\n"+
		"Content-Disposition: recording-session\r\n"+
		"\r\n"+
		"%s\r\n"+
		"--%s--\r\n", boundary, sdpContent, boundary, siprecXML, boundary)

	// Calculate content length
	contentLength := len(multipartBody)

	// Create SIPREC INVITE with multipart content
	invite := fmt.Sprintf("INVITE sip:recorder@127.0.0.1:5063 SIP/2.0\r\n"+
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=%s\r\n"+
		"To: <sip:recorder@127.0.0.1>\r\n"+
		"From: <sip:sipclient@127.0.0.1>;tag=%s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Contact: <sip:sipclient@127.0.0.1:9999;transport=tls>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: multipart/mixed;boundary=\"%s\"\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n%s", branchID, fromTag, callID, boundary, contentLength, multipartBody)

	// Send the INVITE
	_, err := conn.Write([]byte(invite))
	if err != nil {
		log.Fatalf("Failed to send SIPREC INVITE: %v", err)
	}

	// Read the response with timeout
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 8192) // Larger buffer for multipart responses
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Warning - read timeout or error for SIPREC INVITE: %v", err)
		fmt.Println("No response received for SIPREC INVITE within timeout")
	} else {
		fmt.Printf("Received SIPREC INVITE response:\n%s\n", string(buf[:n]))
	}
}

func sendRegularInvite(conn *tls.Conn, branchID, fromTag, callID string) {
	// Create proper SDP content with correct line endings and format
	sdpContent := "v=0\r\n" +
		"o=- 1141236 1141236 IN IP4 127.0.0.1\r\n" +
		"s=SIP Call\r\n" +
		"c=IN IP4 127.0.0.1\r\n" +
		"t=0 0\r\n" +
		"m=audio 10000 RTP/AVP 0 8\r\n" +
		"a=rtpmap:0 PCMU/8000\r\n" +
		"a=rtpmap:8 PCMA/8000\r\n" +
		"a=sendrecv\r\n" +
		"a=ptime:20\r\n" +
		"a=maxptime:150\r\n" +
		"a=ssrc:1234567890 cname:test-session\r\n" +
		"a=recvonly\r\n"
	
	// Calculate the correct content length
	contentLength := len(sdpContent)
	
	invite := fmt.Sprintf("INVITE sip:record@127.0.0.1:5063 SIP/2.0\r\n"+
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=%s-regular\r\n"+
		"To: <sip:record@127.0.0.1>\r\n"+
		"From: <sip:caller@127.0.0.1>;tag=%s-regular\r\n"+
		"Call-ID: %s-regular\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Contact: <sip:caller@127.0.0.1:9999;transport=tls>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: application/sdp\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n%s", branchID, fromTag, callID, contentLength, sdpContent)

	_, err := conn.Write([]byte(invite))
	if err != nil {
		log.Fatalf("Failed to send regular INVITE: %v", err)
	}

	// Set a short timeout for reading
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	
	// Read response
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Warning - read timeout or error for regular INVITE: %v", err)
		fmt.Println("No response received for regular INVITE within timeout")
	} else {
		fmt.Printf("Received regular INVITE response:\n%s\n", string(buf[:n]))
	}
}

func sendOptions(conn *tls.Conn, branchID, fromTag, callID string) {
	options := fmt.Sprintf("OPTIONS sip:record@127.0.0.1:5063 SIP/2.0\r\n"+
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=%s-options\r\n"+
		"To: <sip:record@127.0.0.1>\r\n"+
		"From: <sip:caller@127.0.0.1>;tag=%s-options\r\n"+
		"Call-ID: %s-options\r\n"+
		"CSeq: 1 OPTIONS\r\n"+
		"Contact: <sip:caller@127.0.0.1:9999;transport=tls>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n", branchID, fromTag, callID)
	
	_, err := conn.Write([]byte(options))
	if err != nil {
		log.Fatalf("Failed to send OPTIONS: %v", err)
	}
	
	// Reset deadline and read response
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Warning - read timeout or error (OPTIONS): %v", err)
		fmt.Println("No response received for OPTIONS within timeout")
	} else {
		fmt.Printf("Received OPTIONS response:\n%s\n", string(buf[:n]))
	}
}