package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"
)

func main() {
	// Check command line arguments
	if len(os.Args) > 1 {
		if os.Args[1] == "client" {
			runTLSClient()
			return
		} else if os.Args[1] == "audio_test" {
			// Run with a fixed port for RTP rather than trying to use SIP signaling
			runDirectAudioTest()
			return
		} else if os.Args[1] == "server" {
			runServer()
			return
		}
	}
	
	// Default - client just sends direct RTP packets
	fmt.Println("Sending direct RTP packets to port 15000 (no SIP signaling)")
	sendTestRTPPackets(15000)
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

	listener, err := tls.Listen("tcp", "127.0.0.1:5064", config) // Use a different port 5064
	if err != nil {
		log.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	fmt.Println("TLS server listening on 127.0.0.1:5064")

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

// runDirectAudioTest sends test audio directly to a UDP port
func runDirectAudioTest() {
    fmt.Println("Running direct RTP packet test to target port 15000...")
    
    // Fixed port for SIPREC server test
    rtpPort := 15000
    
    // Send RTP packets to test audio processing
    fmt.Printf("Sending test RTP packets to port %d...\n", rtpPort)
    sendTestRTPPackets(rtpPort)
    
    fmt.Println("RTP test complete")
}

// sendTestRTPPackets sends test RTP packets to the specified port
func sendTestRTPPackets(port int) {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Printf("Failed to resolve address: %v", err)
		return
	}
	
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		log.Printf("Failed to connect to UDP: %v", err)
		return
	}
	defer conn.Close()
	
	// Generate test RTP packets (G.711 PCMU - 8000Hz, 20ms frames)
	sequenceNumber := uint16(1000) // Starting sequence number
	timestamp := uint32(0)         // Starting timestamp
	ssrc := uint32(0x12345678)     // SSRC identifier
	
	// Create a basic RTP packet
	createRTPPacket := func(seqNum uint16, ts uint32, payload []byte) []byte {
		// RTP header - 12 bytes
		/*
		0                   1                   2                   3
		0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|V=2|P|X|  CC   |M|     PT      |       sequence number         |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|                           timestamp                           |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		|           synchronization source (SSRC) identifier            |
		+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		*/
		
		packet := make([]byte, 12+len(payload))
		
		// Version=2, Padding=0, Extension=0, CSRC count=0
		packet[0] = 0x80
		// Marker=0, Payload Type=0 (PCMU)
		packet[1] = 0x00
		
		// Sequence number (16 bits)
		packet[2] = byte(seqNum >> 8)
		packet[3] = byte(seqNum)
		
		// Timestamp (32 bits)
		packet[4] = byte(ts >> 24)
		packet[5] = byte(ts >> 16)
		packet[6] = byte(ts >> 8)
		packet[7] = byte(ts)
		
		// SSRC (32 bits)
		packet[8] = byte(ssrc >> 24)
		packet[9] = byte(ssrc >> 16)
		packet[10] = byte(ssrc >> 8)
		packet[11] = byte(ssrc)
		
		// Copy payload
		copy(packet[12:], payload)
		
		return packet
	}
	
	// Create synthetic audio data patterns
	createAudioPattern := func(patternType string, length int) []byte {
		data := make([]byte, length)
		
		switch patternType {
		case "silence":
			// Silence in PCMU is represented by value 0xFF (255)
			for i := range data {
				data[i] = 0xFF
			}
		case "tone":
			// Simple tone pattern (alternating values)
			for i := range data {
				data[i] = byte(128 + (i % 16) * 8)
			}
		case "noise":
			// Random noise
			for i := range data {
				data[i] = byte(rand.Intn(256))
			}
		default:
			// Default pattern - continuous wave
			for i := range data {
				data[i] = byte(128 + int(127*math.Sin(float64(i)/10)))
			}
		}
		
		return data
	}
	
	// Send a series of test patterns to exercise audio processing
	// Each G.711 frame is 160 bytes (20ms at 8kHz)
	frameSize := 160
	
	// First send silence for 1 second (50 frames) to establish noise floor
	fmt.Println("Sending silence frames to establish noise floor...")
	for i := 0; i < 50; i++ {
		silenceFrame := createAudioPattern("silence", frameSize)
		packet := createRTPPacket(sequenceNumber, timestamp, silenceFrame)
		
		_, err = conn.Write(packet)
		if err != nil {
			log.Printf("Failed to send RTP packet: %v", err)
			return
		}
		
		sequenceNumber++
		timestamp += uint32(frameSize)
		time.Sleep(20 * time.Millisecond) // 20ms frame interval
	}
	
	// Then send a tone pattern for 1 second (50 frames)
	fmt.Println("Sending tone frames to test voice detection...")
	for i := 0; i < 50; i++ {
		toneFrame := createAudioPattern("tone", frameSize)
		packet := createRTPPacket(sequenceNumber, timestamp, toneFrame)
		
		_, err = conn.Write(packet)
		if err != nil {
			log.Printf("Failed to send RTP packet: %v", err)
			return
		}
		
		sequenceNumber++
		timestamp += uint32(frameSize)
		time.Sleep(20 * time.Millisecond)
	}
	
	// Then send alternating silence and voice to test VAD
	fmt.Println("Sending alternating silence/tone frames to test VAD...")
	for i := 0; i < 100; i++ {
		var frame []byte
		if i%4 < 2 { // 2 frames of silence, then 2 frames of tone
			frame = createAudioPattern("silence", frameSize)
		} else {
			frame = createAudioPattern("tone", frameSize)
		}
		
		packet := createRTPPacket(sequenceNumber, timestamp, frame)
		
		_, err = conn.Write(packet)
		if err != nil {
			log.Printf("Failed to send RTP packet: %v", err)
			return
		}
		
		sequenceNumber++
		timestamp += uint32(frameSize)
		time.Sleep(20 * time.Millisecond)
	}
	
	// Finally, send noise pattern to test noise reduction
	fmt.Println("Sending noise frames to test noise reduction...")
	for i := 0; i < 50; i++ {
		noiseFrame := createAudioPattern("noise", frameSize)
		packet := createRTPPacket(sequenceNumber, timestamp, noiseFrame)
		
		_, err = conn.Write(packet)
		if err != nil {
			log.Printf("Failed to send RTP packet: %v", err)
			return
		}
		
		sequenceNumber++
		timestamp += uint32(frameSize)
		time.Sleep(20 * time.Millisecond)
	}
	
	fmt.Println("Finished sending test RTP packets")
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