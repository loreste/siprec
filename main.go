package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/sirupsen/logrus"
)

var (
	logger = logrus.New() // Using logrus for structured logging
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

	// Initialize the speech-to-text client based on the environment configuration
	selectSTTProvider()
}

func selectSTTProvider() {
	vendor := os.Getenv("SPEECH_VENDOR")
	if vendor == "" {
		logger.Fatal("SPEECH_VENDOR not set in .env file")
	} else {
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

	// Listen on regular SIP (UDP/TCP) ports
	for _, port := range config.Ports {
		address := fmt.Sprintf("%s:%d", ip, port)
		go func(port int) {
			// Listen for SIP over UDP
			if err := server.ListenAndServe(context.Background(), "udp", address); err != nil {
				logger.Fatalf("Failed to start SIP server on UDP port %d: %v", port, err)
			}

			// Listen for SIP over TCP
			if err := server.ListenAndServe(context.Background(), "tcp", address); err != nil {
				logger.Fatalf("Failed to start SIP server on TCP port %d: %v", port, err)
			}
		}(port)
	}

	// If TLS is configured, listen on the TLS port
	if config.TLSCertFile != "" && config.TLSKeyFile != "" && config.TLSPort != 0 {
		tlsAddress := fmt.Sprintf("%s:%d", ip, config.TLSPort)
		go func() {
			// Create a TLS config
			cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
			if err != nil {
				logger.Fatalf("Failed to load TLS certificate: %v", err)
			}
			tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

			// Listen for SIP over TLS
			if err := server.ListenAndServeTLS(context.Background(), "tls", tlsAddress, tlsConfig); err != nil {
				logger.Fatalf("Failed to start SIP server on TLS port %d: %v", config.TLSPort, err)
			}
		}()
	} else {
		logger.Warn("TLS_CERT_FILE, TLS_KEY_FILE, or TLS_PORT not set. SIP over TLS will not be enabled.")
	}
}

func main() {
	var wg sync.WaitGroup
	wg.Add(1)
	go startServer(&wg)

	// Graceful shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Println("Received shutdown signal, cleaning up...")
		cleanupActiveCalls()
		logger.Println("Cleanup complete. Shutting down.")
		os.Exit(0)
	}()

	wg.Wait()
}
