// Package test_audio provides a test for the audio processing features
// To use this test, rename the package to main and run with go run test_audio.go
package test_audio

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
	
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/audio"
)

// TestAudioProcessing tests the audio processing features
func TestAudioProcessing() {
	// Create a logger
	logger := logrus.New()
	logger.Out = os.Stdout
	logger.Level = logrus.DebugLevel
	
	// Listen on a UDP port
	port := 15000
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		log.Fatalf("Failed to listen on UDP port %d: %v", port, err)
	}
	defer conn.Close()
	
	fmt.Printf("Listening for UDP packets on port %d...\n", port)
	
	// Create audio processing manager with a configuration
	config := audio.ProcessingConfig{
		// Voice Activity Detection
		EnableVAD:           true,
		VADThreshold:        0.02,   // Energy threshold
		VADHoldTime:         20,     // Frames to hold voice detection (400ms)
		
		// Noise Reduction
		EnableNoiseReduction: true,
		NoiseFloor:           0.01,   // Noise floor level
		NoiseAttenuationDB:   12.0,   // Noise attenuation in dB
		
		// Multi-channel Support
		ChannelCount:         1,      // Mono
		MixChannels:          true,   // Mix channels
		
		// General settings
		SampleRate:           8000,   // 8kHz telephony standard
		FrameSize:            160,    // 20ms at 8kHz
		BufferSize:           2048,   // Buffer size
	}
	
	processor := audio.NewProcessingManager(config, logger)
	
	// Stats
	var (
		packetsReceived int
		packetsProcessed int
		bytesReceived int
		mutex sync.Mutex
	)
	
	// Start stats logger in a goroutine
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			mutex.Lock()
			fmt.Printf("Stats: Received %d packets (%d bytes), Processed %d packets\n", 
				packetsReceived, bytesReceived, packetsProcessed)
			mutex.Unlock()
			
			// Print audio processing stats
			stats := processor.GetStats()
			fmt.Printf("Audio Processing: Voice ratio: %.2f, Noise floor: %.6f\n", 
				stats.VoiceRatio, stats.NoiseFloor)
		}
	}()
	
	// Buffer for incoming packets
	buffer := make([]byte, 2048)
	
	// Process incoming packets
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			log.Printf("Error reading UDP packet: %v", err)
			continue
		}
		
		// Update stats
		mutex.Lock()
		packetsReceived++
		bytesReceived += n
		mutex.Unlock()
		
		// Skip RTP header (12 bytes) to get the audio payload
		if n <= 12 {
			continue
		}
		
		// Process the audio data
		payload := buffer[12:n]
		processedData, err := processor.ProcessAudio(payload)
		if err != nil {
			log.Printf("Error processing audio: %v", err)
			continue
		}
		
		// Update processed stats
		mutex.Lock()
		packetsProcessed++
		mutex.Unlock()
		
		// Print VAD status
		if processor.IsVoiceActive() {
			fmt.Print("V") // Voice
		} else {
			fmt.Print(".") // Silence
		}
		
		// Check if payload and processed data differ significantly
		sizeDiff := float64(len(processedData)) / float64(len(payload))
		if sizeDiff < 0.5 || sizeDiff > 1.5 {
			fmt.Printf(" [Size change: %.2f%%]", sizeDiff*100)
		}
		
		// We're not forwarding the processed data anywhere since this is just a test
	}
}