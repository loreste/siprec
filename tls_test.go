package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
)

func main() {
	// Load certificate
	cert, err := tls.LoadX509KeyPair("certs/cert.pem", "certs/key.pem")
	if err != nil {
		fmt.Printf("Failed to load certificate: %v\n", err)
		os.Exit(1)
	}

	// Create TLS config
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// Create listener
	listener, err := tls.Listen("tcp", "127.0.0.1:5061", config)
	if err != nil {
		fmt.Printf("Failed to create listener: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("TLS server listening on 127.0.0.1:5061")

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("Error accepting connection: %v\n", err)
			continue
		}

		fmt.Printf("Accepted connection from %s\n", conn.RemoteAddr())

		// Handle connection in a goroutine
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read request
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Printf("Error reading: %v\n", err)
		return
	}

	fmt.Printf("Received: %s\n", string(buf[:n]))

	// Send response
	_, err = conn.Write([]byte("Hello from TLS server\n"))
	if err != nil {
		fmt.Printf("Error writing: %v\n", err)
		return
	}
}
