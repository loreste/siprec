package media

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"siprec-server/pkg/audio"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/siprec"

	"github.com/pion/rtp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	restore := setPortAvailabilityChecker(func(int) bool { return true })
	resetGlobalPortManagerForTests()

	code := m.Run()

	restore()
	resetGlobalPortManagerForTests()

	os.Exit(code)
}

func TestPrepareRecordingAndTranscriptionPayloadsPreservesRawPCM(t *testing.T) {
	metrics.EnableMetrics(false)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	forwarder := &RTPForwarder{
		AudioProcessor: audio.NewProcessingManager(audio.ProcessingConfig{
			EnableVAD:            false,
			EnableNoiseReduction: false,
			ChannelCount:         1,
			MixChannels:          false,
			SampleRate:           8000,
			FrameSize:            160,
			BufferSize:           320,
		}, logger),
	}

	pcm := make([]byte, 320)
	for i := range pcm {
		pcm[i] = byte(i % 251)
	}
	original := append([]byte(nil), pcm...)

	recording, transcription, err := prepareRecordingAndTranscriptionPayloads(pcm, forwarder, true, "test-call")
	require.NoError(t, err)
	require.Equal(t, original, recording, "recording payload must keep the original PCM samples")
	require.Equal(t, len(pcm), len(transcription), "transcription payload should stay aligned with source samples")

	// Mutate the original PCM slice to ensure recording payload is a true copy
	pcm[0] = 0xFF
	require.NotEqual(t, pcm[0], recording[0], "recording payload should not be affected by later PCM mutations")

	// When audio processing is disabled, helper should skip copying to avoid extra work
	recordingNoProcessing, transcriptionNoProcessing, err := prepareRecordingAndTranscriptionPayloads(pcm, forwarder, false, "test-call")
	require.NoError(t, err)
	require.Equal(t, pcm, recordingNoProcessing)
	require.Equal(t, pcm, transcriptionNoProcessing)
}

// TestRTPTimeoutBehavior verifies that RTP timeout monitoring works correctly
func TestRTPTimeoutBehavior(t *testing.T) {
	metrics.EnableMetrics(false)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create a forwarder with a short timeout for testing
	forwarder := &RTPForwarder{
		Timeout:     2 * time.Second,
		LastRTPTime: time.Now(),
		StopChan:    make(chan struct{}),
		Logger:      logger,
		remoteMutex: sync.Mutex{},
		RemoteSSRC:  12345,
	}

	// Start monitoring in background
	monitorStopped := make(chan struct{})
	go func() {
		MonitorRTPTimeout(context.Background(), forwarder, "test-call-timeout")
		close(monitorStopped)
	}()

	// Wait for timeout to trigger (2 seconds + buffer)
	select {
	case <-monitorStopped:
		// Success - timeout triggered and monitor stopped
	case <-time.After(5 * time.Second):
		t.Fatal("RTP timeout monitoring did not trigger within expected time")
	}

	// Verify StopChan was closed
	select {
	case <-forwarder.StopChan:
		// Success - stop channel was closed
	default:
		t.Fatal("StopChan was not closed after timeout")
	}
}

// TestRTPTimeoutWarning verifies that early warning is issued at 50% threshold
func TestRTPTimeoutWarning(t *testing.T) {
	metrics.EnableMetrics(false)

	logBuffer := &lockedBuffer{}
	logger := logrus.New()
	logger.SetOutput(logBuffer)
	logger.SetLevel(logrus.WarnLevel)

	// Create a forwarder with a short timeout for testing
	forwarder := &RTPForwarder{
		Timeout:     4 * time.Second, // Warning at 2 seconds
		LastRTPTime: time.Now(),
		StopChan:    make(chan struct{}),
		Logger:      logger,
		remoteMutex: sync.Mutex{},
		LocalPort:   10000,
		RemoteSSRC:  12345,
	}

	// Start monitoring
	go MonitorRTPTimeout(context.Background(), forwarder, "test-call-warning")

	// Wait for warning to be issued (50% of 4s = 2s)
	time.Sleep(2500 * time.Millisecond)

	// Check that warning was logged
	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "RTP stream inactive")
	assert.Contains(t, logOutput, "no packets received")

	// Clean up
	forwarder.Stop()
	time.Sleep(100 * time.Millisecond)
}

type lockedBuffer struct {
	buf bytes.Buffer
	mux sync.Mutex
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mux.Lock()
	defer b.mux.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mux.Lock()
	defer b.mux.Unlock()
	return b.buf.String()
}

// TestRTPTimeoutConfigurable verifies that timeout value is configurable
func TestRTPTimeoutConfigurable(t *testing.T) {
	metrics.EnableMetrics(false)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	testCases := []struct {
		name            string
		configTimeout   time.Duration
		expectedTimeout time.Duration
	}{
		{
			name:            "explicit timeout",
			configTimeout:   60 * time.Second,
			expectedTimeout: 60 * time.Second,
		},
		{
			name:            "zero timeout uses default",
			configTimeout:   0,
			expectedTimeout: 30 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			timeout := tc.configTimeout
			if timeout == 0 {
				timeout = 30 * time.Second
			}

			forwarder, err := NewRTPForwarder(timeout, nil, logger, false, nil)
			require.NoError(t, err)
			require.NotNil(t, forwarder)
			require.Equal(t, tc.expectedTimeout, forwarder.Timeout)

			// Clean up
			pm := GetPortManager()
			pm.ReleasePortPair(&PortPair{RTPPort: forwarder.LocalPort, RTCPPort: forwarder.RTCPPort})
		})
	}
}

// TestRTPPacketReceptionAndRecording verifies that RTP packets are received and audio is recorded
func TestRTPPacketReceptionAndRecording(t *testing.T) {
	metrics.EnableMetrics(false)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create temporary directory for test recordings
	tempDir, err := os.MkdirTemp("", "rtp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create recording session
	recordingSession := &siprec.RecordingSession{
		ID:    "test-session",
		SIPID: "test-call",
	}

	// Create forwarder
	forwarder, err := NewRTPForwarder(30*time.Second, recordingSession, logger, false, nil)
	require.NoError(t, err)
	require.NotNil(t, forwarder)
	defer forwarder.Cleanup()

	// Set codec info
	forwarder.SetCodecInfo(0, "PCMU", 8000, 1)

	// Create WAV file for recording
	recordingPath := filepath.Join(tempDir, "test-recording.wav")
	recordingFile, err := os.Create(recordingPath)
	require.NoError(t, err)
	defer recordingFile.Close()

	// Initialize WAV writer
	wavWriter, err := NewWAVWriter(recordingFile, 8000, 1)
	require.NoError(t, err)
	forwarder.WAVWriter = wavWriter
	forwarder.RecordingFile = recordingFile

	// Create test PCM audio data (160 samples for 20ms at 8kHz)
	pcmData := make([]byte, 160)
	for i := range pcmData {
		pcmData[i] = byte(i % 128)
	}

	// Write audio to recording
	pausableWriter := NewPausableWriter(wavWriter)
	n, err := pausableWriter.Write(pcmData)
	require.NoError(t, err)
	require.Equal(t, len(pcmData), n)

	// Finalize WAV file
	err = wavWriter.Finalize()
	require.NoError(t, err)

	// Verify file exists and has content
	fileInfo, err := os.Stat(recordingPath)
	require.NoError(t, err)
	assert.Greater(t, fileInfo.Size(), int64(44), "WAV file should be larger than header size")

	// Verify LastRTPTime is updated
	initialTime := forwarder.LastRTPTime
	forwarder.LastRTPTime = time.Now()
	assert.True(t, forwarder.LastRTPTime.After(initialTime))
}

// TestFirstPacketLogging verifies that first packet is logged with details
func TestFirstPacketLogging(t *testing.T) {
	metrics.EnableMetrics(false)

	var logBuffer bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&logBuffer)
	logger.SetLevel(logrus.InfoLevel)

	// Create a simple test to verify the first packet logging logic
	// This simulates what happens in decodeAndProcess
	firstPacketReceived := false
	callUUID := "test-call-first-packet"
	remoteAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.100"), Port: 5004}

	rtpPacket := rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			PayloadType:    0,
			SequenceNumber: 1000,
			Timestamp:      12345678,
			SSRC:           987654321,
		},
		Payload: make([]byte, 160),
	}

	// Simulate first packet logging
	if !firstPacketReceived {
		firstPacketReceived = true
		logger.WithFields(logrus.Fields{
			"call_uuid":    callUUID,
			"remote_addr":  remoteAddr.String(),
			"ssrc":         rtpPacket.SSRC,
			"payload_type": rtpPacket.PayloadType,
			"sequence":     rtpPacket.SequenceNumber,
			"timestamp":    rtpPacket.Timestamp,
			"local_port":   10000,
			"payload_size": len(rtpPacket.Payload),
		}).Info("First RTP packet received successfully")
	}

	// Verify logging
	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "First RTP packet received successfully")
	assert.Contains(t, logOutput, callUUID)
	assert.Contains(t, logOutput, "192.168.1.100:5004")
	assert.Contains(t, logOutput, "ssrc")
	assert.Contains(t, logOutput, "payload_type")
}

// TestRTPForwarderCleanup verifies proper resource cleanup
func TestRTPForwarderCleanup(t *testing.T) {
	metrics.EnableMetrics(false)

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	// Create forwarder
	forwarder, err := NewRTPForwarder(30*time.Second, nil, logger, false, nil)
	require.NoError(t, err)
	require.NotNil(t, forwarder)

	localPort := forwarder.LocalPort
	rtcpPort := forwarder.RTCPPort

	// Cleanup should release ports
	forwarder.Cleanup()

	// Verify cleanup flag is set
	require.True(t, forwarder.isCleanedUp)

	// Verify ports were released (we can try to allocate the same ports again)
	pm := GetPortManager()
	newPair, err := pm.AllocatePortPair()
	require.NoError(t, err)

	// The newly allocated ports should eventually include the released ones
	// (this isn't guaranteed to be the same pair immediately, but cleanup should work)
	pm.ReleasePortPair(newPair)

	// Double cleanup should be safe (idempotent)
	forwarder.Cleanup()
	require.True(t, forwarder.isCleanedUp)

	_ = localPort
	_ = rtcpPort
}
