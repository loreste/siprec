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

	// Simple SIP INVITE with SDP for RTP audio
	callID := fmt.Sprintf("test-call-%d", time.Now().Unix())
	fromTag := fmt.Sprintf("tag-%d", time.Now().Unix())
	branchID := fmt.Sprintf("z9hG4bK-%d", time.Now().Unix())

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
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=%s\r\n"+
		"To: <sip:record@127.0.0.1>\r\n"+
		"From: <sip:caller@127.0.0.1>;tag=%s\r\n"+
		"Call-ID: %s\r\n"+
		"CSeq: 1 INVITE\r\n"+
		"Contact: <sip:caller@127.0.0.1:9999;transport=tls>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Type: application/sdp\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n%s", branchID, fromTag, callID, contentLength, sdpContent)

	fmt.Println("Sending INVITE...")
	_, err = conn.Write([]byte(invite))
	if err != nil {
		log.Fatalf("Failed to send INVITE: %v", err)
	}

	// Set a short timeout for reading
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	
	// Read response
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Warning - read timeout or error: %v", err)
		fmt.Println("No response received within timeout")
	} else {
		fmt.Printf("Received response:\n%s\n", string(buf[:n]))
	}
	
	// Let's try sending an OPTIONS request to see if that works
	fmt.Println("\nSending OPTIONS as fallback test...")
	options := fmt.Sprintf("OPTIONS sip:record@127.0.0.1:5063 SIP/2.0\r\n"+
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=%s-options\r\n"+
		"To: <sip:record@127.0.0.1>\r\n"+
		"From: <sip:caller@127.0.0.1>;tag=%s-options\r\n"+
		"Call-ID: %s-options\r\n"+
		"CSeq: 1 OPTIONS\r\n"+
		"Contact: <sip:caller@127.0.0.1:9999;transport=tls>\r\n"+
		"Max-Forwards: 70\r\n"+
		"Content-Length: 0\r\n\r\n", branchID, fromTag, callID)
	
	_, err = conn.Write([]byte(options))
	if err != nil {
		log.Fatalf("Failed to send OPTIONS: %v", err)
	}
	
	// Reset deadline and read response
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err = conn.Read(buf)
	if err != nil {
		log.Printf("Warning - read timeout or error (OPTIONS): %v", err)
		fmt.Println("No response received for OPTIONS within timeout")
	} else {
		fmt.Printf("Received OPTIONS response:\n%s\n", string(buf[:n]))
	}
}