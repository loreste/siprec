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
	"time"

	"github.com/pion/srtp/v2"
)

type RTPForwarder struct {
	localPort      int
	conn           *net.UDPConn
	stopChan       chan struct{}
	transcriptChan chan string
	recordingFile  *os.File      // Used to store the recorded media stream
	lastRTPTime    time.Time     // Tracks the last time an RTP packet was received
	timeout        time.Duration // Timeout duration for inactive RTP streams
}

var (
	portMutex    sync.Mutex
	usedRTPPorts = make(map[int]bool) // Keeps track of used RTP ports
)

func startRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string) {
	go func() {
		var err error
		udpConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: forwarder.localPort})
		if err != nil {
			logger.WithError(err).WithField("port", forwarder.localPort).Error("Failed to listen on UDP port for RTP forwarding")
			return
		}
		defer udpConn.Close()
		forwarder.conn = udpConn
		forwarder.lastRTPTime = time.Now() // Initialize last RTP packet time

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

		pr, pw := io.Pipe()
		audioStream := pr

		// Start recording
		go startRecording(audioStream, callUUID)

		// Start transcription based on the selected provider
		go StreamToProvider(ctx, config.DefaultVendor, audioStream, callUUID)

		// Start the timeout monitoring routine
		go monitorRTPTimeout(forwarder, callUUID)

		buffer := make([]byte, 1500)
		for {
			select {
			case <-forwarder.stopChan:
				pw.Close()
				return
			case <-ctx.Done():
				pw.Close()
				return
			default:
				n, _, err := forwarder.conn.ReadFrom(buffer)
				if err != nil {
					logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
					continue
				}

				// Update the time of the last received RTP packet
				forwarder.lastRTPTime = time.Now()

				// Write the received RTP packet to the recording file
				if _, err := forwarder.recordingFile.Write(buffer[:n]); err != nil {
					logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to recording file")
				}

				if config.EnableSRTP && srtpSession != nil {
					// Create a read stream for SRTP
					readStream, err := srtpSession.OpenReadStream(0) // Use SSRC 0 for now, adjust if needed
					if err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to open SRTP read stream")
						continue
					}

					// Read and decrypt the SRTP packet
					decryptedRTP := make([]byte, 1500)
					n, err = readStream.Read(decryptedRTP)
					if err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to decrypt SRTP packet")
						continue
					}

					// Write the decrypted packet to the pipe
					if _, err := pw.Write(decryptedRTP[:n]); err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write decrypted RTP packet to audio stream")
					}
				} else {
					// Process RTP packet without decryption
					if _, err := pw.Write(buffer[:n]); err != nil {
						logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
				}
			}
		}
	}()
}

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
