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

	// Log configuration on startup
	logStartupConfig()
}

func selectSTTProvider() {
	vendor := config.DefaultVendor
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
		logger.Warnf("Unsupported STT vendor: %v, defaulting to Google", vendor)
		initSpeechClient() // Default to Google STT
	}
}

func createTLSConfig() (*tls.Config, error) {
	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate and key: %v", err)
	}

	// Create and return a TLS config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	return tlsConfig, nil
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
			defer wg.Done()
			logger.Infof("Starting SIP server on UDP and TCP at %s", address)
			if err := server.ListenAndServe(context.Background(), "udp", address); err != nil {
				logger.Fatalf("Failed to start SIP server on port %d (UDP): %v", port, err)
			}
			if err := server.ListenAndServe(context.Background(), "tcp", address); err != nil {
				logger.Fatalf("Failed to start SIP server on port %d (TCP): %v", port, err)
			}
		}(port)
		wg.Add(1) // Add to WaitGroup to ensure all ports are handled
	}

	// Check if TLS is enabled
	if config.EnableTLS && config.TLSPort != 0 {
		tlsAddress := fmt.Sprintf("%s:%d", ip, config.TLSPort)
		tlsConfig, err := createTLSConfig()
		if err != nil {
			logger.Fatalf("Failed to create TLS config: %v", err)
		}
		go func() {
			defer wg.Done()
			logger.Infof("Starting SIP server with TLS on %s", tlsAddress)
			if err := server.ListenAndServeTLS(context.Background(), "tcp", tlsAddress, tlsConfig); err != nil {
				logger.Fatalf("Failed to start SIP server over TLS on port %d: %v", config.TLSPort, err)
			}
		}()
		wg.Add(1) // Add to WaitGroup for TLS
	}
}

func logStartupConfig() {
	logger.Infof("SIP Server is starting with the following configuration:")
	logger.Infof("External IP: %s", config.ExternalIP)
	logger.Infof("Internal IP: %s", config.InternalIP)
	logger.Infof("SIP Ports: %v", config.Ports)
	logger.Infof("TLS Enabled: %v", config.EnableTLS)
	if config.EnableTLS {
		logger.Infof("TLS Port: %d", config.TLSPort)
		logger.Infof("TLS Cert File: %s", config.TLSCertFile)
		logger.Infof("TLS Key File: %s", config.TLSKeyFile)
	}
	logger.Infof("SRTP Enabled: %v", config.EnableSRTP)
	logger.Infof("RTP Port Range: %d-%d", config.RTPPortMin, config.RTPPortMax)
	logger.Infof("Recording Directory: %s", config.RecordingDir)
	logger.Infof("Speech-to-Text Vendor: %s", config.DefaultVendor)
	logger.Infof("Log Level: %s", config.LogLevel.String())
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
