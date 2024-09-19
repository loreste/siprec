package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	ExternalIP      string
	InternalIP      string
	Ports           []int
	EnableSRTP      bool
	RTPPortMin      int
	RTPPortMax      int
	RecordingDir    string
	SupportedCodecs []string
}

var (
	config Config
)

func loadConfig() {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	config.EnableSRTP = os.Getenv("ENABLE_SRTP") == "true"
	config.ExternalIP = os.Getenv("EXTERNAL_IP")
	config.InternalIP = os.Getenv("INTERNAL_IP")
	config.SupportedCodecs = strings.Split(os.Getenv("SUPPORTED_CODECS"), ",")

	// Load SIP ports
	ports := strings.Split(os.Getenv("PORTS"), ",")
	for _, portStr := range ports {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			log.Fatalf("Invalid port in PORTS: %v", err)
		}
		config.Ports = append(config.Ports, port)
	}

	// Load RTP port range
	config.RTPPortMin, _ = strconv.Atoi(os.Getenv("RTP_PORT_MIN"))
	config.RTPPortMax, _ = strconv.Atoi(os.Getenv("RTP_PORT_MAX"))

	// Load recording directory
	config.RecordingDir = os.Getenv("RECORDING_DIR")
	if config.RecordingDir == "" {
		log.Fatal("RECORDING_DIR not set in .env file")
	}
}
