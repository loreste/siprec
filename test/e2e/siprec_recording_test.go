package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/media"
	"siprec-server/pkg/metrics"
)

// TestE2E_RecordingStorage verifies that the server correctly writes RTP audio to a WAV file
func TestE2E_RecordingStorage(t *testing.T) {
	// 1. Setup temporary recording directory
	tempDir, err := os.MkdirTemp("", "siprec-test-recordings-*")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tempDir) // Cleanup after test

	logger := logrus.New()
	if testing.Verbose() {
		logger.SetLevel(logrus.DebugLevel)
	} else {
		logger.SetLevel(logrus.WarnLevel)
	}

	// Disable metrics to avoid panic due to uninitialized global metrics registry
	metrics.EnableMetrics(false)

	// 2. Configure the server/forwarder
	callID := "recording-test-call-uuid"
	var rtpPort int

	cfg := &media.Config{
		RecordingDir: tempDir,
		ExternalIP:   "127.0.0.1",
	}
	// Initialize PortManager
	media.InitPortManager(18000, 18100)

	// Create RTP Forwarder using constructor
	forwarder, err := media.NewRTPForwarder(5*time.Second, nil, logger, false, nil)
	require.NoError(t, err, "Failed to create RTP forwarder")

	// We need to know the port it grabbed
	rtpPort = forwarder.LocalPort
	t.Logf("Allocated RTP port: %d", rtpPort)

	// Set additional fields that might be needed
	forwarder.RemoteSSRC = 12345
	forwarder.LocalSSRC = 54321
	forwarder.RemoteRTPAddr = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 50000}

	// 3. Start RTP Forwarding
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// StartRTPForwarding runs in a goroutine
	media.StartRTPForwarding(ctx, forwarder, callID, cfg, nil)

	// Wait for listener to bind (simple sleep for this test pattern)
	time.Sleep(100 * time.Millisecond)

	// 4. Send RTP packets
	go func() {
		conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", rtpPort))
		if err != nil {
			t.Logf("Failed to dial valid UDP: %v", err)
			return
		}
		defer conn.Close()

		// Create a simple RTP packet (12-byte header + data)
		// Version 2, PCMU
		header := []byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
		payload := make([]byte, 160) // 20ms of silence
		for i := 0; i < len(payload); i++ {
			payload[i] = 0x7F // Silence in PCMU
		}
		packet := append(header, payload...)

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for i := 0; i < 50; i++ { // Send for 1 second
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Ensure simple sequence number increment
				header[3] = byte(i)
				conn.Write(packet)
			}
		}
	}()

	// 5. Wait for recording
	time.Sleep(2 * time.Second)

	// Stop forwarder to flush files
	close(forwarder.StopChan)
	// Give it a moment to close
	time.Sleep(100 * time.Millisecond)

	// 6. Verify file existence and content
	// The file name format is usually sanitizedUUID.wav
	expectedFile := filepath.Join(tempDir, callID+".wav")
	info, err := os.Stat(expectedFile)
	assert.NoError(t, err, "Recording file should exist")
	if err == nil {
		t.Logf("Recording file created: %s, size: %d bytes", expectedFile, info.Size())
		assert.Greater(t, info.Size(), int64(44), "Recording file should be larger than WAV header (44 bytes)")
	}
}
