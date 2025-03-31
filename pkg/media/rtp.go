package media

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pion/srtp/v2"
	"github.com/sirupsen/logrus"
)

// StartRTPForwarding starts forwarding RTP packets for a call
func StartRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string, config *Config, sttProvider func(context.Context, string, io.Reader, string) error) {
	go func() {
		var err error

		// Create address to listen on
		listenAddr := &net.UDPAddr{Port: forwarder.LocalPort}

		// If behind NAT, we need specific binding
		if config.BehindNAT {
			// If we know our internal IP, bind specifically to it
			if config.InternalIP != "" {
				listenAddr.IP = net.ParseIP(config.InternalIP)
			}

			forwarder.Logger.WithFields(logrus.Fields{
				"port": forwarder.LocalPort,
				"ip":   listenAddr.IP,
			}).Debug("Binding RTP listener with NAT considerations")
		}

		udpConn, err := net.ListenUDP("udp", listenAddr)
		if err != nil {
			forwarder.Logger.WithError(err).WithField("port", forwarder.LocalPort).Error("Failed to listen on UDP port for RTP forwarding")
			return
		}
		defer udpConn.Close()
		forwarder.Conn = udpConn
		forwarder.LastRTPTime = time.Now() // Initialize last RTP packet time

		// Set socket buffer sizes for better performance
		SetUDPSocketBuffers(udpConn, forwarder.Logger)

		// Open the file for recording RTP streams
		filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", callUUID))
		forwarder.RecordingFile, err = os.Create(filePath)
		if err != nil {
			forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
			return
		}
		defer forwarder.RecordingFile.Close()

		var srtpSession *srtp.SessionSRTP
		if config.EnableSRTP {
			forwarder.Logger.WithField("call_uuid", callUUID).Info("Setting up SRTP session")
			
			// Set up SRTP key management
			keyingMaterial := make([]byte, 30) // 16 bytes for master key, 14 bytes for salt
			if _, err := rand.Read(keyingMaterial); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP keying material")
				return
			}

			// Create SRTP session with AES-128 Counter Mode and HMAC-SHA1 authentication
			// RFC 3711 defines these SRTP parameters
			srtpConfig := &srtp.Config{
				Profile: srtp.ProtectionProfileAes128CmHmacSha1_80,
			}
			
			// Copy key material to configuration
			copy(srtpConfig.Keys.LocalMasterKey[:], keyingMaterial[:16])
			copy(srtpConfig.Keys.LocalMasterSalt[:], keyingMaterial[16:])
			
			// Set up remote key - in a real implementation, this would be negotiated
			// For testing, we'll use the same key for local and remote
			copy(srtpConfig.Keys.RemoteMasterKey[:], keyingMaterial[:16])
			copy(srtpConfig.Keys.RemoteMasterSalt[:], keyingMaterial[16:])
			
			// Store key material in the forwarder for SDP use
			forwarder.SRTPMasterKey = make([]byte, 16)
			forwarder.SRTPMasterSalt = make([]byte, 14)
			copy(forwarder.SRTPMasterKey, keyingMaterial[:16])
			copy(forwarder.SRTPMasterSalt, keyingMaterial[16:])

			// Create SRTP session
			srtpSession, err = srtp.NewSessionSRTP(udpConn, srtpConfig)
			if err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to set up SRTP session")
				return
			}
			
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"profile": "AES_CM_128_HMAC_SHA1_80",
			}).Info("SRTP session successfully set up")
		}

		// Create pipes for streaming audio data
		mainPr, mainPw := io.Pipe()

		// Use TeeReader to duplicate the stream
		recordingReader := io.TeeReader(mainPr, forwarder.RecordingFile)

		// Start transcription based on the selected provider
		go sttProvider(ctx, config.DefaultVendor, recordingReader, callUUID)

		// Start the timeout monitoring routine
		go MonitorRTPTimeout(forwarder, callUUID)

		// Use buffer pool to reduce garbage collection
		bufferPtr := BufferPool.Get()
		buffer := bufferPtr.([]byte)
		defer BufferPool.Put(bufferPtr)
		for {
			select {
			case <-forwarder.StopChan:
				mainPw.Close()
				return
			case <-ctx.Done():
				mainPw.Close()
				return
			default:
				n, _, err := forwarder.Conn.ReadFrom(buffer)
				if err != nil {
					forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
					continue
				}

				// Update the time of the last received RTP packet
				forwarder.LastRTPTime = time.Now()

				// Check if recording is paused by the SIP dialog
				if forwarder.RecordingPaused {
					continue // Skip processing this packet
				}

				// Process RTP packet
				if config.EnableSRTP && srtpSession != nil {
					forwarder.SRTPEnabled = true
					
					// Process SRTP packet
					// Get a buffer from the pool
					bufPtr := BufferPool.Get()
					decryptedRTP := bufPtr.([]byte)
					defer BufferPool.Put(bufPtr)
					
					// Extract SSRC from RTP packet header
					// RFC 3550 - RTP header format:
					// 0                   1                   2                   3
					// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
					// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
					// |V=2|P|X|  CC   |M|     PT      |       sequence number         |
					// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
					// |                           timestamp                           |
					// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
					// |           synchronization source (SSRC) identifier            |
					// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
					
					// SSRC is at bytes 8-11 in the RTP header
					var ssrc uint32
					if n >= 12 { // Ensure we have enough bytes for a complete RTP header
						ssrc = uint32(buffer[8])<<24 | uint32(buffer[9])<<16 | uint32(buffer[10])<<8 | uint32(buffer[11])
					} else {
						// Default SSRC if header is incomplete
						ssrc = 0
					}
					
					// Open a read stream with the correct SSRC
					readStream, err := srtpSession.OpenReadStream(ssrc)
					if err != nil {
						forwarder.Logger.WithError(err).WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"ssrc":      ssrc,
						}).Debug("Failed to open SRTP read stream, trying default SSRC")
						
						// Try with default SSRC as fallback
						readStream, err = srtpSession.OpenReadStream(0)
						if err != nil {
							continue
						}
					}

					n, err = readStream.Read(decryptedRTP)
					if err != nil {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to read from SRTP stream")
						continue
					}

					// Write decrypted packet to pipe
					if _, err := mainPw.Write(decryptedRTP[:n]); err != nil {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
					
					// Log successful SRTP decryption (only for debugging, remove in production)
					if forwarder.Logger.GetLevel() >= logrus.DebugLevel {
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"ssrc":      ssrc,
							"bytes":     n,
						}).Debug("Successfully decrypted SRTP packet")
					}
				} else {
					// Process regular RTP packet
					if _, err := mainPw.Write(buffer[:n]); err != nil {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
					}
				}

				// Update metrics
				RTPPacketsReceived++
				RTPBytesReceived += uint64(n)
			}
		}
	}()
}

// AllocateRTPPort dynamically allocates an RTP port for use
func AllocateRTPPort(minPort, maxPort int, logger *logrus.Logger) int {
	PortMutex.Lock()
	defer PortMutex.Unlock()

	for {
		// Secure port allocation using crypto/rand
		port, err := rand.Int(rand.Reader, big.NewInt(int64(maxPort-minPort)))
		if err != nil {
			logger.Fatal("Failed to generate random port")
		}
		rtpPort := int(port.Int64()) + minPort
		if !UsedRTPPorts[rtpPort] {
			UsedRTPPorts[rtpPort] = true
			return rtpPort
		}
	}
}

// ReleaseRTPPort releases a previously allocated RTP port
func ReleaseRTPPort(port int) {
	PortMutex.Lock()
	defer PortMutex.Unlock()
	delete(UsedRTPPorts, port)
}

// MonitorRTPTimeout periodically checks if RTP packets are received within a given timeout period
func MonitorRTPTimeout(forwarder *RTPForwarder, callUUID string) {
	ticker := time.NewTicker(5 * time.Second) // Check every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-forwarder.StopChan:
			return
		case <-ticker.C:
			// Check if the last RTP packet was received more than the timeout duration ago
			if time.Since(forwarder.LastRTPTime) > forwarder.Timeout {
				forwarder.Logger.WithField("call_uuid", callUUID).Warn("RTP timeout reached, terminating session")
				forwarder.StopChan <- struct{}{}
				return
			}
		}
	}
}

// SetUDPSocketBuffers sets appropriate buffer sizes for UDP sockets
func SetUDPSocketBuffers(conn *net.UDPConn, logger *logrus.Logger) {
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