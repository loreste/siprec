package main

import (
	"fmt"
	"net"
	
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
)

func main() {
	fmt.Println("Testing sipgo API")
	
	logger := logrus.New()
	
	// Create IP addresses for SIP listening
	hostIPs := []net.IP{net.ParseIP("127.0.0.1")}
	
	// Create user agent
	ua, err := sipgo.NewUA(
		sipgo.WithLogger(logger),
		sipgo.WithHostIPs(hostIPs),
	)
	if err != nil {
		logger.Fatalf("Failed to create user agent: %s", err)
	}
	
	// Create server
	server, err := sipgo.NewServer(ua)
	if err != nil {
		logger.Fatalf("Failed to create server: %s", err)
	}
	
	// Get router
	router := server.Router()
	
	// Print types
	fmt.Printf("UA type: %T\n", ua)
	fmt.Printf("Server type: %T\n", server) 
	fmt.Printf("Router type: %T\n", router)
	
	// Register a handler
	router.HandleFunc("INVITE", func(req *sip.Request, tx sip.ServerTransaction) {
		fmt.Println("Got INVITE")
	})
	
	fmt.Println("SipGo API test completed")
}