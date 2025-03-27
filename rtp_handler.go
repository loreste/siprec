package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/pion/srtp/v2"
	"github.com/sirupsen/logrus"
)

type RTPForwarder struct {
	localPort        int
	conn             *net.UDPConn
	stopChan         chan struct{}
	transcriptChan   chan string
	recordingFile    *os.File          // Used to store the recorded media stream
	lastRTPTime      time.Time         // Tracks the last time an RTP packet was received
	timeout          time.Duration     // Timeout duration for inactive RTP streams
	recordingSession *RecordingSession // SIPREC session information
	recordingPaused  bool              // Flag to indicate if recording is paused
}

var (
	portMutex    sync.Mutex
	usedRTPPorts = make(map[int]bool) // Keeps track of used RTP ports
)

func startRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string) {
	go func() {
		var err error

		// Create address to listen on
		listenAddr := &net.UDPAddr{Port: forwarder.localPort}

		// If behind NAT, we need specific binding
		if config.BehindNAT {
			// If we know our internal IP, bind specifically to it
			if config.InternalIP != "" {
				listenAddr.IP = net.ParseIP(config.InternalIP)
			}

			logger.WithFields(logrus.Fields{
				"port": forwarder.localPort,
				"ip":   listenAddr.IP,
			}).Debug("Binding RTP listener with NAT considerations")
		}

		udpConn, err := net.ListenUDP("udp", listenAddr)
		if err != nil {
			logger.WithError(err).WithField("port", forwarder.localPort).Error("Failed to listen on UDP port for RTP forwarding")
			return
		}
		defer udpConn.Close()
		forwarder.conn = udpConn
		forwarder.lastRTPTime = time.Now() // Initialize last RTP packet time

		// Set socket buffer sizes for better performance
		setUDPSocketBuffers(udpConn)

		// Open the file for recording RTP streams
		filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", callUUID))
		forwarder.recordingFile, err = os.Create(filePath)
		if err != nil {
			logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
			return
		}
		defer forwarder.recordingFile.Close()

		var srtpSession *srtp.SessionSRTP
		if config.EnableSRTP {
			// Set up SRTP key management
			keyingMaterial := make([]byte, 30) // 16 bytes for master key, 14 bytes for salt
			if _, err := rand.Read(keyingMaterial); err != nil {
				logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP keying material")
				return
			}

			// Create SRTP session
			srtpConfig := &srtp.Config{
				Profile: srtp.ProtectionProfileAes128CmHmacSha1_80,
			}
			copy(srtpConfig.Keys.LocalMasterKey[:], keyingMaterial[:16])
			copy(srtpConfig.Keys.LocalMasterSalt[:], keyingMaterial[16:])

			srtpSession, err = srtp.NewSessionSRTP(udpConn, srtpConfig)
			if err != nil {
				logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to set up SRTP session")
				return
			}
		}

		// Create pipes for streaming audio data
		mainPr, mainPw := io.Pipe()

		// Use TeeReader to duplicate the stream
		recordingReader := io.TeeReader(mainPr, forwarder.recordingFile)

		// Start transcription based on the selected provider
		go StreamToProvider(ctx, config.DefaultVendor, recordingReader, callUUID)

		// Start the timeout monitoring routine
		go monitorRTPTimeout(forwarder, callUUID)

		// Use buffer pool to reduce garbage collection
		bufferPtr := bufferPool.Get()
		buffer := bufferPtr.([]byte)
		defer bufferPool.Put(bufferPtr)
		for {
			select {
			case <-forwarder.stopChan:
				mainPw.Close()
				return
			case <-ctx.Done():
				mainPw.Close()
				return
			default:
				n, _, err := forwarder.conn.ReadFrom(buffer)
				if err != nil {
					logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
					continue
				}

				// Update the time of the last received RTP packet
				forwarder.lastRTPTime = time.Now()

				// Check if recording is paused by the SIP dialog
				if forwarder.recordingPaused {
					continue // Skip processing this packet
				}

				// Process RTP packet
				if config.EnableSRTP && srtpSession != nil {
					// Process SRTP packet (simplified - real impl would handle SSRC correctly)
					// Get a buffer from the pool
					bufPtr := bufferPool.Get()
					decryptedRTP := bufPtr.([]byte)
					defer bufferPool.Put(bufPtr)

					// In a real implementation, we would use the correct SSRC
					readStream, err := srtpSession.OpenReadStream(0)
					if err != nil {
						continue
					}

					n, err = readStream.Read(decryptedRTP)
					if err != nil {
						continue
					}

					// Write decrypted packet to pipe
					if _, err := mainPw.Write(decryptedRTP[:n]); err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
				} else {
					// Process regular RTP packet
					if _, err := mainPw.Write(buffer[:n]); err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
				}

				// Update metrics if we're tracking them
				rtpPacketsReceived++
				rtpBytesReceived += uint64(n)
			}
		}
	}()
}

// Buffer pool for RTP packets to reduce GC pressure
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1500)
	},
}

// Global metrics - in a production environment, these would be proper metrics
var (
	rtpPacketsReceived uint64
	rtpBytesReceived   uint64
)

// allocateRTPPort dynamically allocates an RTP port for use.
func allocateRTPPort() int {
	portMutex.Lock()
	defer portMutex.Unlock()

	for {
		// Secure port allocation using crypto/rand
		port, err := rand.Int(rand.Reader, big.NewInt(int64(config.RTPPortMax-config.RTPPortMin)))
		if err != nil {
			logger.Fatal("Failed to generate random port")
		}
		rtpPort := int(port.Int64()) + config.RTPPortMin
		if !usedRTPPorts[rtpPort] {
			usedRTPPorts[rtpPort] = true
			return rtpPort
		}
	}
}

// releaseRTPPort releases a previously allocated RTP port.
func releaseRTPPort(port int) {
	portMutex.Lock()
	defer portMutex.Unlock()
	delete(usedRTPPorts, port)
}

// monitorRTPTimeout periodically checks if RTP packets are received within a given timeout period.
func monitorRTPTimeout(forwarder *RTPForwarder, callUUID string) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-forwarder.stopChan:
			return
		case <-ticker.C:
			// Check if the last RTP packet was received more than the timeout duration ago
			if time.Since(forwarder.lastRTPTime) > forwarder.timeout {
				logger.WithField("call_uuid", callUUID).Warn("RTP timeout reached, terminating session")
				forwarder.stopChan <- struct{}{}
				return
			}
		}
	}
}

// startRecording saves the audio stream to a file.
func startRecording(audioStream io.Reader, callUUID string) {
	filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", callUUID))
	file, err := os.Create(filePath)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
		return
	}
	defer file.Close()

	_, err = io.Copy(file, audioStream)
	if err != nil {
		logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write audio stream to file")
	}
}

// Helper function to set UDP socket buffers appropriately for production use
func setUDPSocketBuffers(conn *net.UDPConn) {
	// Get the file descriptor from the UDPConn
	file, err := conn.File()
	if err != nil {
		logger.WithError(err).Warn("Failed to get file descriptor for UDP socket")
		return
	}
	defer file.Close()

	fd := int(file.Fd())

	// Set receive buffer to 1MB
	err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_RCVBUF, 1024*1024)
	if err != nil {
		logger.WithError(err).Warn("Failed to set UDP receive buffer size")
	}

	// Set send buffer to 1MB
	err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_SNDBUF, 1024*1024)
	if err != nil {
		logger.WithError(err).Warn("Failed to set UDP send buffer size")
	}
}

// Detect codec from RTP packet
func detectCodec(rtpPacket []byte) (string, error) {
	if len(rtpPacket) < 12 {
		return "", fmt.Errorf("RTP packet too short")
	}

	// Extract payload type from RTP header
	payloadType := rtpPacket[1] & 0x7F

	switch payloadType {
	case 0:
		return "PCMU", nil // G.711 μ-law
	case 8:
		return "PCMA", nil // G.711 a-law
	case 9:
		return "G722", nil // G.722
	default:
		return "unknown", fmt.Errorf("unsupported payload type: %d", payloadType)
	}
}

// Process audio packet for the specific codec
func processAudioPacket(data []byte, payloadType byte) ([]byte, error) {
	// Extract the RTP payload
	if len(data) < 12 {
		return nil, fmt.Errorf("RTP packet too short")
	}

	payload := data[12:]

	// Process based on payload type
	switch payloadType {
	case 0: // PCMU (G.711 μ-law)
		// G.711 μ-law is already in a format that can be written to WAV
		return payload, nil
	case 8: // PCMA (G.711 a-law)
		// G.711 a-law is already in a format that can be written to WAV
		return payload, nil
	case 9: // G.722
		// G.722 processing might require special handling
		// Depending on your needs, you might need to convert it
		return payload, nil
	default:
		return nil, fmt.Errorf("unsupported payload type: %d", payloadType)
	}
}
