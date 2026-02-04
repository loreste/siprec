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

	// 4. Send RTP packets synchronously for more reliable test
	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", rtpPort))
	require.NoError(t, err, "Failed to dial UDP")
	defer conn.Close()

	// Create a simple RTP packet (12-byte header + data)
	// Version 2, PCMU (payload type 0)
	// RTP header: V=2, P=0, X=0, CC=0, M=0, PT=0
	// Bytes 0-1: V(2)|P|X|CC | M|PT
	// Bytes 2-3: Sequence number
	// Bytes 4-7: Timestamp
	// Bytes 8-11: SSRC (must match forwarder.RemoteSSRC = 12345 = 0x00003039)
	packet := make([]byte, 172) // 12-byte header + 160-byte payload
	packet[0] = 0x80            // V=2, P=0, X=0, CC=0
	packet[1] = 0x00            // M=0, PT=0 (PCMU)
	// SSRC = 12345 = 0x00003039 (bytes 8-11, big-endian)
	packet[8] = 0x00
	packet[9] = 0x00
	packet[10] = 0x30
	packet[11] = 0x39
	// Fill payload with PCMU silence (0x7F)
	for i := 12; i < 172; i++ {
		packet[i] = 0x7F
	}

	// Send packets with timing
	timestamp := uint32(0)
	for i := 0; i < 100; i++ { // Send 100 packets (2 seconds worth)
		// Update sequence number (bytes 2-3, big-endian)
		packet[2] = byte(i >> 8)
		packet[3] = byte(i & 0xFF)
		// Update timestamp (bytes 4-7, big-endian)
		packet[4] = byte(timestamp >> 24)
		packet[5] = byte(timestamp >> 16)
		packet[6] = byte(timestamp >> 8)
		packet[7] = byte(timestamp & 0xFF)
		timestamp += 160 // 160 samples per 20ms at 8kHz

		n, err := conn.Write(packet)
		if err != nil {
			t.Logf("Failed to write packet %d: %v", i, err)
			break
		}
		if i == 0 {
			t.Logf("First RTP packet sent: %d bytes to port %d", n, rtpPort)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 5. Stop forwarder to flush files
	t.Logf("Stopping forwarder after sending packets")
	close(forwarder.StopChan)
	// Give it a moment to close and write the WAV file
	time.Sleep(500 * time.Millisecond)

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
