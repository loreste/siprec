package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
)

func main() {
	// Load the certificates
	cert, err := tls.LoadX509KeyPair("certs/cert.pem", "certs/key.pem")
	if err != nil {
		fmt.Printf("Failed to load certificates: %v\n", err)
		os.Exit(1)
	}

	// Create a TLS config
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Create a TLS listener
	listener, err := tls.Listen("tcp", "127.0.0.1:5061", config)
	if err != nil {
		fmt.Printf("Failed to create listener: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("TLS server listening on 127.0.0.1:5061")

	// Accept a connection
	conn, err := listener.Accept()
	if err != nil {
		fmt.Printf("Failed to accept connection: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Printf("Accepted connection from %s\n", conn.RemoteAddr())

	// Read data from the connection
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("Failed to read data: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Received: %s\n", string(buffer[:n]))

	// Send a response
	response := "HTTP/1.1 200 OK\r\nContent-Length: 13\r\n\r\nHello, World!"
	_, err = conn.Write([]byte(response))
	if err != nil {
		fmt.Printf("Failed to send response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Response sent")
}
