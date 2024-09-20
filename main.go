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
	logger = logrus.New() // Renamed to avoid conflict with the standard log package
)

func init() {
	// Set up logger
	logger.SetFormatter(&logrus.JSONFormatter{})
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.InfoLevel)

	// Load configuration
	loadConfig()

	// Initialize AMQP
	initAMQP()

	// Initialize the STT provider (Google, Deepgram, OpenAI)
	selectSTTProvider()
}

// selectSTTProvider sets up the appropriate STT provider based on config
func selectSTTProvider() {
	for _, vendor := range config.SupportedVendors {
		switch vendor {
		case "google":
			initSpeechClient() // Initialize Google STT
		case "deepgram":
			if err := initDeepgramClient(); err != nil {
				logger.Fatalf("Error initializing Deepgram client: %v", err)
			}
		case "openai":
			if err := initOpenAIClient(); err != nil {
				logger.Fatalf("Error initializing OpenAI client: %v", err)
			}
		default:
			logger.Fatalf("Unsupported STT vendor: %v", vendor)
		}
	}
}

func startServer(wg *sync.WaitGroup) {
	defer wg.Done()

	ip := "0.0.0.0" // Listen on all interfaces

	ua, err := sipgo.NewUA()
	if err != nil {
		logger.Fatalf("Failed to create UserAgent: %v", err)
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		logger.Fatalf("Failed to create SIP server: %v", err)
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
				logger.Fatalf("Failed to start SIP server on port %d: %v", port, err)
			}
			if err := server.ListenAndServe(context.Background(), "tcp", address); err != nil {
				logger.Fatalf("Failed to start SIP server on port %d: %v", port, err)
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
		logger.Println("Received shutdown signal, cleaning up...")
		cleanupActiveCalls()
		logger.Println("Cleanup complete. Shutting down.")
		os.Exit(0)
	}()

	wg.Wait()
}
