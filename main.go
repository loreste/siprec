package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
)

var (
	log = logrus.New()
)

func init() {
	// Set up logger
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(logrus.InfoLevel)

	// Load configuration
	loadConfig()

	// Initialize AMQP
	initAMQP()

	// Initialize Google Speech-to-Text client
	initSpeechClient()
}

func startServer(wg *sync.WaitGroup) {
	defer wg.Done()

	ip := "0.0.0.0" // Listen on all interfaces

	ua, err := sipgo.NewUA()
	if err != nil {
		log.Fatalf("Failed to create UserAgent: %v", err)
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		log.Fatalf("Failed to create SIP server: %v", err)
	}

	// Handle INVITE requests
	server.OnRequest(sip.INVITE, func(req *sip.Request, tx sip.ServerTransaction) {
		handleSiprecInvite(req, tx, server)
	})

	// Handle CANCEL requests
	server.OnRequest(sip.CANCEL, func(req *sip.Request, tx sip.ServerTransaction) {
		handleCancel(req, tx)
	})

	// Listen on configured ports for SIP traffic
	for _, port := range config.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		go func(port int) {
			if err := server.ListenAndServe(context.Background(), "udp", address); err != nil {
				log.Fatalf("Failed to start SIP server on port %d: %v", port, err)
			}
			if err := server.ListenAndServe(context.Background(), "tcp", address); err != nil {
				log.Fatalf("Failed to start SIP server on port %d: %v", port, err)
			}
		}(port)
	}
}

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go startServer(&wg)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill)
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, cleaning up...")
		cleanupActiveCalls()
		log.Println("Cleanup complete. Shutting down.")
		os.Exit(0)
	}()

	wg.Wait()
}
