// Package test_sipgo provides a test for the sipgo API
// To use this test, rename the package to main and run with go run test_sipgo.go
package test_sipgo

import (
	"fmt"
	"net"
	
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
)

// TestSipGo tests the sipgo API
func TestSipGo() {
	fmt.Println("Testing sipgo API")
	
	logger := logrus.New()
	
	// Create IP addresses for SIP listening
	hostIPs := []net.IP{net.ParseIP("127.0.0.1")}
	
	// Create user agent - v0.30.0 style
	ua, err := sipgo.NewUA()
	if err != nil {
		logger.Fatalf("Failed to create user agent: %s", err)
	}
	
	// Set logger manually if needed
	// ua.Log = logger
	
	// Create server
	server, err := sipgo.NewServer(ua)
	if err != nil {
		logger.Fatalf("Failed to create server: %s", err)
	}
	
	// Print types
	fmt.Printf("UA type: %T\n", ua)
	fmt.Printf("Server type: %T\n", server) 
	
	// Register a handler directly on server
	server.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println("Got INVITE")
	})
	
	fmt.Println("SipGo API test completed")
}