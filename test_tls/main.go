package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
)

func main() {
	// Check if we should run as client or server
	if len(os.Args) > 1 && os.Args[1] == "client" {
		runClient()
		return
	}
	
	// Default: run as server
	runServer()
}

func runServer() {
	cert, err := tls.LoadX509KeyPair("../certs/cert.pem", "../certs/key.pem")
	if err != nil {
		log.Fatalf("Failed to load certificates: %v", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:5063", config)
	if err != nil {
		log.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	fmt.Println("TLS server listening on 127.0.0.1:5063")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn)
	}
}

func runClient() {
	config := &tls.Config{
		InsecureSkipVerify: true, // Skip certificate validation for test
	}

	conn, err := tls.Dial("tcp", "127.0.0.1:5063", config)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send OPTIONS request
	request := "OPTIONS sip:127.0.0.1:5063 SIP/2.0\r\n" +
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=z9hG4bK-test\r\n" +
		"To: <sip:test@127.0.0.1>\r\n" +
		"From: <sip:tester@127.0.0.1>;tag=test123\r\n" +
		"Call-ID: test-call-id\r\n" +
		"CSeq: 1 OPTIONS\r\n" +
		"Max-Forwards: 70\r\n" +
		"Content-Length: 0\r\n\r\n"

	_, err = conn.Write([]byte(request))
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}

	// Read response
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Fatalf("Failed to read response: %v", err)
	}

	fmt.Printf("Received response:\n%s\n", string(buf[:n]))
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read multiple requests
	for {
		buf := make([]byte, 8192)
		n, err := conn.Read(buf)
		if err != nil {
			log.Printf("Connection closed or read error: %v", err)
			return
		}

		request := string(buf[:n])
		fmt.Printf("Received request:\n%s\n", request)

		// Parse the request to construct appropriate response
		if strings.Contains(request, "INVITE") {
			// Extract key headers for the response
			var via, to, from, callID, cseq string
			
			lines := strings.Split(request, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Via:") {
					via = line
				} else if strings.HasPrefix(line, "To:") {
					to = line
				} else if strings.HasPrefix(line, "From:") {
					from = line
				} else if strings.HasPrefix(line, "Call-ID:") {
					callID = line
				} else if strings.HasPrefix(line, "CSeq:") {
					cseq = line
				}
			}
			
			// Create a tag for the To header
			toTag := ";tag=responder-tag"
			if strings.Contains(to, ";tag=") {
				// Already has a tag, don't add another
				toTag = ""
			}
			
			// Detect if this is a SIPREC INVITE
			isSiprec := strings.Contains(request, "application/rs-metadata+xml")
			
			// Create proper response
			if isSiprec {
				// For SIPREC, respond with 200 OK and minimal SDP
				sdp := "v=0\r\n" +
					"o=- 1141236 1141236 IN IP4 127.0.0.1\r\n" +
					"s=SIP Call\r\n" +
					"c=IN IP4 127.0.0.1\r\n" +
					"t=0 0\r\n" +
					"m=audio 20000 RTP/AVP 0 8\r\n" +
					"a=rtpmap:0 PCMU/8000\r\n" +
					"a=rtpmap:8 PCMA/8000\r\n" +
					"a=recvonly\r\n"
				
				contentLength := len(sdp)
				
				response := "SIP/2.0 200 OK\r\n" +
					via + "\r\n" +
					to + toTag + "\r\n" +
					from + "\r\n" +
					callID + "\r\n" +
					cseq + "\r\n" +
					"Contact: <sip:responder@127.0.0.1:5063;transport=tls>\r\n" +
					"Content-Type: application/sdp\r\n" +
					fmt.Sprintf("Content-Length: %d\r\n\r\n", contentLength) +
					sdp
				
				// Send the response
				_, err = conn.Write([]byte(response))
				if err != nil {
					log.Printf("Failed to send SIPREC INVITE response: %v", err)
					return
				}
				
			} else {
				// Regular INVITE, just respond with minimal SDP
				sdp := "v=0\r\n" +
					"o=- 1141236 1141236 IN IP4 127.0.0.1\r\n" +
					"s=SIP Call\r\n" +
					"c=IN IP4 127.0.0.1\r\n" +
					"t=0 0\r\n" +
					"m=audio 20000 RTP/AVP 0 8\r\n" +
					"a=rtpmap:0 PCMU/8000\r\n" +
					"a=rtpmap:8 PCMA/8000\r\n" +
					"a=sendrecv\r\n"
				
				contentLength := len(sdp)
				
				response := "SIP/2.0 200 OK\r\n" +
					via + "\r\n" +
					to + toTag + "\r\n" +
					from + "\r\n" +
					callID + "\r\n" +
					cseq + "\r\n" +
					"Contact: <sip:responder@127.0.0.1:5063;transport=tls>\r\n" +
					"Content-Type: application/sdp\r\n" +
					fmt.Sprintf("Content-Length: %d\r\n\r\n", contentLength) +
					sdp
				
				// Send the response
				_, err = conn.Write([]byte(response))
				if err != nil {
					log.Printf("Failed to send regular INVITE response: %v", err)
					return
				}
			}
			
			log.Printf("Sent 200 OK response to INVITE")
			
		} else if strings.Contains(request, "OPTIONS") {
			// Extract key headers for the response
			var via, to, from, callID, cseq string
			
			lines := strings.Split(request, "\r\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "Via:") {
					via = line
				} else if strings.HasPrefix(line, "To:") {
					to = line
				} else if strings.HasPrefix(line, "From:") {
					from = line
				} else if strings.HasPrefix(line, "Call-ID:") {
					callID = line
				} else if strings.HasPrefix(line, "CSeq:") {
					cseq = line
				}
			}
			
			// Create a tag for the To header
			toTag := ";tag=responder-tag"
			if strings.Contains(to, ";tag=") {
				// Already has a tag, don't add another
				toTag = ""
			}
			
			// Respond to OPTIONS with Allow and Supported headers
			response := "SIP/2.0 200 OK\r\n" +
				via + "\r\n" +
				to + toTag + "\r\n" +
				from + "\r\n" +
				callID + "\r\n" +
				cseq + "\r\n" +
				"Contact: <sip:responder@127.0.0.1:5063;transport=tls>\r\n" +
				"Allow: INVITE, ACK, CANCEL, BYE, OPTIONS\r\n" +
				"Supported: replaces, siprec\r\n" +
				"Content-Length: 0\r\n\r\n"
			
			// Send the response
			_, err = conn.Write([]byte(response))
			if err != nil {
				log.Printf("Failed to send OPTIONS response: %v", err)
				return
			}
			log.Printf("Sent 200 OK response to OPTIONS")
			
		} else {
			// Generic response for other request types
			response := "SIP/2.0 200 OK\r\n" +
				"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=z9hG4bK-test\r\n" +
				"To: <sip:test@127.0.0.1>;tag=responder-tag\r\n" +
				"From: <sip:tester@127.0.0.1>;tag=test123\r\n" +
				"Call-ID: test-call-id\r\n" +
				"CSeq: 1 UNKNOWN\r\n" +
				"Content-Length: 0\r\n\r\n"
				
			_, err = conn.Write([]byte(response))
			if err != nil {
				log.Printf("Failed to send generic response: %v", err)
				return
			}
			log.Printf("Sent generic 200 OK response")
		}
	}
}