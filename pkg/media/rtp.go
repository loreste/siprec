package media

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pion/srtp/v2"
	"github.com/sirupsen/logrus"
	"siprec-server/pkg/audio"
)

func init() {
	// Initialize the random number generator
	mathrand.Seed(time.Now().UnixNano())
}

// StartRTPForwarding starts forwarding RTP packets for a call
func StartRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string, config *Config, sttProvider func(context.Context, string, io.Reader, string) error) {
	go func() {
		var err error

		// Ensure we release the port when done
		defer func() {
			// Get the port manager and release the port
			pm := GetPortManager()
			pm.ReleasePort(forwarder.LocalPort)
			forwarder.Logger.WithField("port", forwarder.LocalPort).Debug("Released RTP port")
		}()

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
			
			// Set up SRTP key management for pion/srtp v2
			// Create master key and salt of the correct size
			masterKey := make([]byte, 16)
			masterSalt := make([]byte, 14)
			
			// Generate random key and salt separately
			if _, err := rand.Read(masterKey); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP master key")
				return
			}
			if _, err := rand.Read(masterSalt); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP master salt")
				return
			}

			// Create SRTP session with AES-128 Counter Mode and HMAC-SHA1 authentication
			srtpConfig := &srtp.Config{
				Profile: srtp.ProtectionProfileAes128CmHmacSha1_80,
				Keys: srtp.SessionKeys{
					LocalMasterKey:   masterKey,
					LocalMasterSalt:  masterSalt,
					RemoteMasterKey:  masterKey,  // Same key for both directions
					RemoteMasterSalt: masterSalt, // Same salt for both directions
				},
			}
				
			// Store key material in the forwarder for SDP use
			forwarder.SRTPMasterKey = make([]byte, 16)
			forwarder.SRTPMasterSalt = make([]byte, 14)
			copy(forwarder.SRTPMasterKey, masterKey)
			copy(forwarder.SRTPMasterSalt, masterSalt)
			
			// Log for debugging
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"key_len": len(masterKey),
				"salt_len": len(masterSalt),
			}).Debug("SRTP keying material generated")

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

		// Initialize audio processing if enabled
		if config.AudioProcessing.Enabled {
			// Convert to audio processing configuration
			audioConfig := audio.ProcessingConfig{
				// Voice Activity Detection
				EnableVAD:           config.AudioProcessing.EnableVAD,
				VADThreshold:        config.AudioProcessing.VADThreshold,
				VADHoldTime:         config.AudioProcessing.VADHoldTimeMs / 20, // Convert ms to frames
				
				// Noise Reduction
				EnableNoiseReduction: config.AudioProcessing.EnableNoiseReduction,
				NoiseFloor:           config.AudioProcessing.NoiseReductionLevel,
				NoiseAttenuationDB:   12.0, // Default 12dB noise reduction
				
				// Multi-channel Support
				ChannelCount:         config.AudioProcessing.ChannelCount,
				MixChannels:          config.AudioProcessing.MixChannels,
				
				// General Processing (8kHz telephony standard)
				SampleRate:           8000,
				FrameSize:            160, // 20ms at 8kHz
				BufferSize:           2048,
			}
			
			// Create audio processing manager
			forwarder.AudioProcessor = audio.NewProcessingManager(audioConfig, forwarder.Logger)
			
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid":        callUUID,
				"vad_enabled":      config.AudioProcessing.EnableVAD,
				"noise_reduction":  config.AudioProcessing.EnableNoiseReduction,
				"channels":         config.AudioProcessing.ChannelCount,
			}).Info("Audio processing initialized")
		}

		// Create pipes for streaming audio data
		mainPr, mainPw := io.Pipe()

		// Set up reader for final output
		var recordingReader io.Reader = mainPr
		
		// Apply audio processing if enabled
		if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
			processingManager := forwarder.AudioProcessor.(*audio.ProcessingManager)
			recordingReader = processingManager.WrapReader(mainPr)
		}

		// Use TeeReader to duplicate the stream to recording file
		recordingReader = io.TeeReader(recordingReader, forwarder.RecordingFile)

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

					// Process audio if enabled
					if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
						processingManager := forwarder.AudioProcessor.(*audio.ProcessingManager)
						
						// Add detailed debug log before processing
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"data_len": n,
							"srtp": true,
							"audio_processing": "enabled",
						}).Debug("Processing SRTP audio packet with audio processing")
						
						processedData, err := processingManager.ProcessAudio(decryptedRTP[:n])
						if err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to process audio")
						} else if len(processedData) > 0 {
							// Log successful processing
							forwarder.Logger.WithFields(logrus.Fields{
								"call_uuid": callUUID,
								"original_size": n,
								"processed_size": len(processedData),
							}).Debug("Successfully processed SRTP audio packet")
							
							// Write processed audio to pipe
							if _, err := mainPw.Write(processedData); err != nil {
								forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write processed audio to stream")
							}
						}
					} else {
						// Write decrypted packet to pipe
						if _, err := mainPw.Write(decryptedRTP[:n]); err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
						}
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
					// Process regular RTP packet with audio processing if enabled
					if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
						processingManager := forwarder.AudioProcessor.(*audio.ProcessingManager)
						
						// Add detailed debug log before processing
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"data_len": n,
							"srtp": false,
							"audio_processing": "enabled",
							"payload_type": buffer[1] & 0x7F, // Extract payload type from RTP header
						}).Debug("Processing regular RTP audio packet")
						
						processedData, err := processingManager.ProcessAudio(buffer[:n])
						if err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to process audio")
							continue
						}
						
						if len(processedData) > 0 {
							// Log successful processing
							forwarder.Logger.WithFields(logrus.Fields{
								"call_uuid": callUUID,
								"original_size": n,
								"processed_size": len(processedData),
							}).Debug("Successfully processed RTP audio packet")
							
							// Write processed audio to pipe
							if _, err := mainPw.Write(processedData); err != nil {
								forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write processed audio to stream")
							}
						}
					} else {
						// No processing, just forward
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"data_len": n,
							"audio_processing": "disabled",
						}).Debug("Forwarding unprocessed RTP packet")
						
						if _, err := mainPw.Write(buffer[:n]); err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
						}
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
	// Initialize port manager if needed
	portManagerOnce.Do(func() {
		portManager = NewPortManager(minPort, maxPort)
		logger.WithFields(logrus.Fields{
			"min_port": minPort,
			"max_port": maxPort,
		}).Info("Initialized RTP port manager")
	})
	
	// Get the port manager and allocate a port
	pm := GetPortManager()
	port, err := pm.AllocatePort()
	if err != nil {
		logger.WithError(err).Error("Failed to allocate RTP port")
		// As a fallback, try a random port
		randomPort := minPort + mathrand.Intn(maxPort-minPort)
		logger.WithField("port", randomPort).Warn("Using fallback random port")
		return randomPort
	}
	
	logger.WithField("port", port).Debug("Allocated RTP port")
	return port
}

// ReleaseRTPPort releases a previously allocated RTP port
func ReleaseRTPPort(port int) {
	// Get the port manager and release the port
	pm := GetPortManager()
	pm.ReleasePort(port)
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