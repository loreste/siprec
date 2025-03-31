package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
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

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("Failed to read data: %v", err)
		return
	}

	fmt.Printf("Received request:\n%s\n", string(buf[:n]))

	// Send a basic SIP 200 OK response
	response := "SIP/2.0 200 OK\r\n" +
		"Via: SIP/2.0/TLS 127.0.0.1:9999;branch=z9hG4bK-test\r\n" +
		"To: <sip:test@127.0.0.1>\r\n" +
		"From: <sip:tester@127.0.0.1>;tag=test123\r\n" +
		"Call-ID: test-call-id\r\n" +
		"CSeq: 1 OPTIONS\r\n" +
		"Content-Length: 0\r\n\r\n"

	_, err = conn.Write([]byte(response))
	if err != nil {
		log.Printf("Failed to send response: %v", err)
		return
	}
}