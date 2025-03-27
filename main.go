package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/stun"
	"github.com/sirupsen/logrus"
)

var (
	logger = logrus.New() // Using logrus for structured logging

	// STUN-related variables
	stunMutex       sync.RWMutex
	externalIP      string
	externalPort    int
	stunInitialized bool
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

	// Initialize STUN client if behind NAT
	if config.BehindNAT {
		if err := initSTUNClient(); err != nil {
			logger.WithError(err).Warn("Failed to initialize STUN client, NAT traversal may not work correctly")
		}
	}

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

// initSTUNClient initializes the STUN client with Google's STUN servers
func initSTUNClient() error {
	if !config.BehindNAT {
		logger.Info("NAT traversal disabled, skipping STUN initialization")
		return nil
	}

	if len(config.STUNServers) == 0 {
		logger.Warn("No STUN servers configured, using Google's default stun.l.google.com:19302")
		config.STUNServers = []string{"stun.l.google.com:19302"}
	}

	// Get the first STUN server from the list
	stunServer := getNextSTUNServer()
	logger.WithField("stun_server", stunServer).Info("Initializing STUN client with Google STUN server")

	// Perform initial STUN binding to get external IP and port
	if err := performSTUNBinding(stunServer); err != nil {
		logger.WithError(err).Warn("Initial STUN binding failed")
		// Don't return error, we'll retry later
	}

	// Start a periodic STUN refresh to keep NAT bindings active
	go startSTUNRefresher()

	return nil
}

// performSTUNBinding performs a STUN binding request and updates external IP/port
func performSTUNBinding(serverAddr string) error {
	// Create a connection to the STUN server
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return fmt.Errorf("failed to listen for STUN: %w", err)
	}
	defer conn.Close()

	// Set a reasonable timeout for the request
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	// Parse the STUN server address
	serverUDPAddr, err := net.ResolveUDPAddr("udp4", serverAddr)
	if err != nil {
		return fmt.Errorf("failed to resolve STUN server address: %w", err)
	}

	// Create STUN message
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	// Send the request
	if _, err = conn.WriteToUDP(message.Raw, serverUDPAddr); err != nil {
		return fmt.Errorf("failed to send STUN request: %w", err)
	}

	// Create a buffer to receive the response
	buffer := make([]byte, 1024)

	// Wait for the response
	n, _, err := conn.ReadFromUDP(buffer)
	if err != nil {
		return fmt.Errorf("failed to read STUN response: %w", err)
	}

	// Decode the message
	response := &stun.Message{Raw: buffer[:n]}
	if err := response.Decode(); err != nil {
		return fmt.Errorf("failed to decode STUN message: %w", err)
	}

	// Extract mapped address (our external IP:port)
	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(response); err != nil {
		return fmt.Errorf("failed to get XOR-MAPPED-ADDRESS: %w", err)
	}

	// Update our external IP and port
	stunMutex.Lock()
	externalIP = xorAddr.IP.String()
	externalPort = xorAddr.Port
	stunInitialized = true
	stunMutex.Unlock()

	logger.WithFields(logrus.Fields{
		"external_ip":   externalIP,
		"external_port": externalPort,
		"stun_server":   serverAddr,
	}).Info("STUN binding successful")

	// Update the config's external IP if it was auto-detected
	if config.ExternalIP == "auto" || config.ExternalIP == "" {
		config.ExternalIP = externalIP
		logger.WithField("external_ip", externalIP).Info("Updated external IP from STUN")
	}

	return nil
}

// startSTUNRefresher periodically refreshes the STUN binding
func startSTUNRefresher() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Keep track of consecutive failures
	consecutiveFailures := 0

	// Use for range with the ticker.C channel
	for range ticker.C {
		// Rotate through STUN servers
		stunServer := getNextSTUNServer()
		logger.WithField("stun_server", stunServer).Debug("Refreshing STUN binding")

		if err := performSTUNBinding(stunServer); err != nil {
			consecutiveFailures++
			logger.WithError(err).WithFields(logrus.Fields{
				"stun_server": stunServer,
				"failures":    consecutiveFailures,
			}).Warn("STUN refresh failed")

			// If we've failed too many times, try the next server
			if consecutiveFailures >= 3 {
				stunServer = getNextSTUNServer() // Get a different server
				logger.WithField("stun_server", stunServer).Info("Trying different STUN server after repeated failures")
			}
		} else {
			// Reset failure counter on success
			consecutiveFailures = 0
		}
	}
}

// GetExternalAddress returns the external IP and port discovered through STUN
func GetExternalAddress() (string, int, bool) {
	stunMutex.RLock()
	defer stunMutex.RUnlock()
	return externalIP, externalPort, stunInitialized
}

func createTLSConfig() (*tls.Config, error) {
	// Load TLS certificate and key
	cert, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate and key: %v", err)
	}

	// Create and return a TLS config with modern security settings
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []tls.CurveID{
			tls.CurveP256,
			tls.X25519,
		},
		PreferServerCipherSuites: true,
	}
	return tlsConfig, nil
}

// recoverMiddleware wraps a SIP handler with panic recovery
func recoverMiddleware(handler func(*sip.Request, sip.ServerTransaction)) func(*sip.Request, sip.ServerTransaction) {
	return func(req *sip.Request, tx sip.ServerTransaction) {
		defer func() {
			if r := recover(); r != nil {
				logger.WithFields(logrus.Fields{
					"call_uuid": req.CallID().String(),
					"method":    req.Method, // Changed from req.Method() to req.Method
					"panic":     r,
				}).Error("Recovered from panic in SIP handler")

				// Try to send a 500 response if possible
				// Removed check for tx.Responded() as it doesn't exist
				resp := sip.NewResponseFromRequest(req, 500, "Internal Server Error", nil)
				tx.Respond(resp)
			}
		}()

		// Call the original handler
		handler(req, tx)
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

	// Register SIP request handlers with recovery middleware
	server.OnRequest(sip.INVITE, recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		// Check if this is an initial INVITE or re-INVITE
		toTag, _ := req.To().Params.Get("tag")
		if toTag != "" {
			// This is a re-INVITE (in-dialog)
			handleReInvite(req, tx, server)
		} else {
			// Initial INVITE
			handleSiprecInvite(req, tx, server)
		}
	}))

	// Handle CANCEL requests
	server.OnRequest(sip.CANCEL, recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		handleCancel(req, tx)
	}))

	// Handle OPTIONS requests
	server.OnRequest(sip.OPTIONS, recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		handleOptions(req, tx)
	}))

	// Handle BYE requests
	server.OnRequest(sip.BYE, recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		callUUID := req.CallID().String()
		handleBye(req, tx, callUUID)
	}))

	// Handle UPDATE requests (for in-dialog updates)
	server.OnRequest(sip.UPDATE, recoverMiddleware(func(req *sip.Request, tx sip.ServerTransaction) {
		// Implementation will depend on your specific needs
		// For now, just respond with 200 OK
		resp := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(resp)
		logger.WithField("call_uuid", req.CallID().String()).Info("Responded to UPDATE request")
	}))

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

	// Start the health check server
	go startHealthServer()
}

// Start a simple health check HTTP server
func startHealthServer() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Count active calls
		activeCallCount := 0
		activeCalls.Range(func(_, _ interface{}) bool {
			activeCallCount++
			return true
		})

		// Check disk space
		diskUsage, err := CheckRecordingDiskSpace()
		if err != nil {
			logger.WithError(err).Warn("Failed to check recording disk space")
		}

		// Return 200 OK if all checks pass
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","active_calls":%d,"disk_usage_percent":%d,"timestamp":"%s"}`,
			activeCallCount, diskUsage, time.Now().Format(time.RFC3339))
	})

	// Add more endpoints as needed
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		// In a production environment, you would use a proper metrics library
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"rtp_packets_received":%d,"rtp_bytes_received":%d}`, rtpPacketsReceived, rtpBytesReceived)
	})

	// Add STUN status endpoint
	http.HandleFunc("/stun-status", func(w http.ResponseWriter, r *http.Request) {
		stunIP, stunPort, stunInit := GetExternalAddress()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"initialized":%t,"external_ip":"%s","external_port":%d,"behind_nat":%t,"stun_servers":%q}`,
			stunInit, stunIP, stunPort, config.BehindNAT, config.STUNServers)
	})

	logger.Info("Starting health check server on port 8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logger.WithError(err).Error("Failed to start health check server")
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
	logger.Infof("NAT Traversal: %v", config.BehindNAT)
	if config.BehindNAT {
		logger.Infof("STUN Servers: %v", config.STUNServers)
	}
	logger.Infof("Supported Codecs: %v", config.SupportedCodecs)
	logger.Infof("Max Concurrent Calls: %d", config.MaxConcurrentCalls)
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
