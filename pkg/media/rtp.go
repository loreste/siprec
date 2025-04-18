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
	"siprec-server/pkg/metrics"
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
			// Use the Cleanup method to ensure all resources are properly released
			forwarder.Cleanup()
		}()

		// Start metrics for this session if enabled
		var endSessionMetrics func()
		if metrics.IsMetricsEnabled() {
			endSessionMetrics = metrics.StartSessionTimer("rtp_forwarding")
			defer endSessionMetrics()
		}

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
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPDroppedPackets(callUUID, "listen_failure", 1)
			}
			return
		}
		// Don't use defer close since we transfer ownership to forwarder
		forwarder.Conn = udpConn
		forwarder.LastRTPTime = time.Now() // Initialize last RTP packet time

		// Set socket buffer sizes for better performance
		SetUDPSocketBuffers(udpConn, forwarder.Logger)

		// Open the file for recording RTP streams
		filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", callUUID))
		forwarder.RecordingFile, err = os.Create(filePath)
		if err != nil {
			forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPDroppedPackets(callUUID, "file_creation_failed", 1)
			}
			return
		}
		// Don't use defer close since we transfer ownership to forwarder

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
				if metrics.IsMetricsEnabled() {
					metrics.RecordSRTPEncryptionErrors(callUUID, "key_generation_failed", 1)
				}
				return
			}
			if _, err := rand.Read(masterSalt); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to generate SRTP master salt")
				if metrics.IsMetricsEnabled() {
					metrics.RecordSRTPEncryptionErrors(callUUID, "salt_generation_failed", 1)
				}
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
				if metrics.IsMetricsEnabled() {
					metrics.RecordSRTPEncryptionErrors(callUUID, "session_setup_failed", 1)
				}
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

		// Main packet processing loop
		for {
			select {
			case <-forwarder.StopChan:
				mainPw.Close()
				return
			case <-ctx.Done():
				mainPw.Close()
				return
			default:
				// Get an appropriately sized buffer for the packet
				buffer, returnBuffer := GetPacketBuffer(1500)
				
				// Read the next packet with timeout
				udpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, _, err := udpConn.ReadFrom(buffer)
				
				// Record metrics for this packet if enabled
				if metrics.IsMetricsEnabled() && n > 0 {
					metrics.RecordRTPPacket(callUUID, n)
				}
				
				if err != nil {
					// Return the buffer to the pool since we're done with it
					returnBuffer(buffer)
					
					// Log error, but only if it's not a timeout
					if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
						if metrics.IsMetricsEnabled() {
							metrics.RecordRTPDroppedPackets(callUUID, "read_error", 1)
						}
					}
					continue
				}

				// Update the time of the last received RTP packet
				forwarder.LastRTPTime = time.Now()

				// Check if recording is paused by the SIP dialog
				if forwarder.RecordingPaused {
					returnBuffer(buffer) // Return buffer to pool if paused
					if metrics.IsMetricsEnabled() {
						metrics.RecordRTPDroppedPackets(callUUID, "recording_paused", 1)
					}
					continue // Skip processing this packet
				}

				// Process RTP packet
				if config.EnableSRTP && srtpSession != nil {
					forwarder.SRTPEnabled = true
					
					// Start measuring processing time for SRTP decryption
					var finishProcessingTimer func()
					if metrics.IsMetricsEnabled() {
						finishProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "srtp_decryption")
					}
					
					// Get another buffer for the decrypted data
					decryptedRTP, returnDecryptedBuffer := GetPacketBuffer(n + 20)  // Extra room for any padding
					
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
						// Return both buffers to their pools
						returnBuffer(buffer)
						returnDecryptedBuffer(decryptedRTP)
						
						forwarder.Logger.WithError(err).WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"ssrc":      ssrc,
						}).Debug("Failed to open SRTP read stream, trying default SSRC")
						
						if metrics.IsMetricsEnabled() {
							metrics.RecordSRTPDecryptionErrors(callUUID, "open_stream_error", 1)
						}
						
						// Try with default SSRC as fallback
						readStream, err = srtpSession.OpenReadStream(0)
						if err != nil {
							if finishProcessingTimer != nil {
								finishProcessingTimer()
							}
							continue
						}
					}

					// Read decrypted data directly into our buffer
					decryptedLen, err := readStream.Read(decryptedRTP[:cap(decryptedRTP)])
					if err != nil {
						// Return both buffers to their pools
						returnBuffer(buffer)
						returnDecryptedBuffer(decryptedRTP)
						
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to read from SRTP stream")
						
						if metrics.IsMetricsEnabled() {
							metrics.RecordSRTPDecryptionErrors(callUUID, "read_error", 1)
						}
						
						if finishProcessingTimer != nil {
							finishProcessingTimer()
						}
						continue
					}
					
					// Track successful SRTP packet processing
					if metrics.IsMetricsEnabled() {
						metrics.RecordSRTPPacketsProcessed(callUUID, "rx", 1)
					}

					// We can release the original encrypted buffer now
					returnBuffer(buffer)
					
					// Finish timing SRTP decryption
					if finishProcessingTimer != nil {
						finishProcessingTimer()
					}

					// Process audio if enabled
					if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
						// Start timer for audio processing
						var finishAudioProcessingTimer func()
						if metrics.IsMetricsEnabled() {
							finishAudioProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "audio_processing")
						}
						
						processingManager := forwarder.AudioProcessor.(*audio.ProcessingManager)
						
						// Add detailed debug log before processing
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"data_len": decryptedLen,
							"srtp": true,
							"audio_processing": "enabled",
						}).Debug("Processing SRTP audio packet with audio processing")
						
						processedData, err := processingManager.ProcessAudio(decryptedRTP[:decryptedLen])
						
						// Finish timing audio processing
						if finishAudioProcessingTimer != nil {
							finishAudioProcessingTimer()
						}
						
						// Return the decrypted buffer now that we have processed its contents
						returnDecryptedBuffer(decryptedRTP)
						
						if err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to process audio")
							if metrics.IsMetricsEnabled() {
								metrics.RecordAudioProcessingError(callUUID, "processing_error", 1)
							}
						} else if len(processedData) > 0 {
							// Log successful processing
							forwarder.Logger.WithFields(logrus.Fields{
								"call_uuid": callUUID,
								"original_size": decryptedLen,
								"processed_size": len(processedData),
							}).Debug("Successfully processed SRTP audio packet")
							
							// Record VAD event if processing included VAD
							if config.AudioProcessing.EnableVAD && metrics.IsMetricsEnabled() {
								// If we have access to VAD status, we could record it here
								// metrics.RecordVADEvent(callUUID, "speech_detected", 1)
							}
							
							// Write processed audio to pipe
							startWrite := time.Now()
							if _, err := mainPw.Write(processedData); err != nil {
								forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write processed audio to stream")
								if metrics.IsMetricsEnabled() {
									metrics.RecordRTPDroppedPackets(callUUID, "write_error", 1)
								}
							} else if metrics.IsMetricsEnabled() {
								// Record write latency
								metrics.RecordRTPLatency(callUUID, time.Since(startWrite))
							}
						}
					} else {
						// Write decrypted packet directly to pipe
						startWrite := time.Now()
						if _, err := mainPw.Write(decryptedRTP[:decryptedLen]); err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP packet to audio stream")
							if metrics.IsMetricsEnabled() {
								metrics.RecordRTPDroppedPackets(callUUID, "write_error", 1)
							}
						} else if metrics.IsMetricsEnabled() {
							// Record write latency
							metrics.RecordRTPLatency(callUUID, time.Since(startWrite))
						}
						
						// Now that we've written the decrypted data, we can return the buffer
						returnDecryptedBuffer(decryptedRTP)
					}
				} else {
					// For non-SRTP traffic, process the RTP payload directly
					
					// Start measuring processing time
					var finishProcessingTimer func()
					if metrics.IsMetricsEnabled() {
						finishProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "rtp_processing")
					}
					
					// Process audio if enabled
					if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
						// Start timer for audio processing 
						var finishAudioProcessingTimer func()
						if metrics.IsMetricsEnabled() {
							finishAudioProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "audio_processing")
						}
						
						processingManager := forwarder.AudioProcessor.(*audio.ProcessingManager)
						
						// Process the RTP payload starting after the header (typically 12 bytes)
						// A proper implementation would extract the payload based on the header values
						var payloadStart int = 12
						if n > payloadStart {
							// Process the audio payload
							processed, err := processingManager.ProcessAudio(buffer[payloadStart:n])
							
							// Finish timing audio processing
							if finishAudioProcessingTimer != nil {
								finishAudioProcessingTimer()
							}
							
							if err != nil {
								forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to process audio from RTP payload")
								if metrics.IsMetricsEnabled() {
									metrics.RecordAudioProcessingError(callUUID, "processing_error", 1)
								}
							} else if len(processed) > 0 {
								// Write processed audio to pipe
								startWrite := time.Now()
								if _, err := mainPw.Write(processed); err != nil {
									forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write processed audio to stream")
									if metrics.IsMetricsEnabled() {
										metrics.RecordRTPDroppedPackets(callUUID, "write_error", 1)
									}
								} else if metrics.IsMetricsEnabled() {
									// Record write latency
									metrics.RecordRTPLatency(callUUID, time.Since(startWrite))
								}
							}
						}
					} else {
						// Write unprocessed RTP payload to pipe - skip the RTP header
						var payloadStart int = 12 
						if n > payloadStart {
							startWrite := time.Now()
							if _, err := mainPw.Write(buffer[payloadStart:n]); err != nil {
								forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write RTP payload to audio stream")
								if metrics.IsMetricsEnabled() {
									metrics.RecordRTPDroppedPackets(callUUID, "write_error", 1)
								}
							} else if metrics.IsMetricsEnabled() {
								// Record write latency
								metrics.RecordRTPLatency(callUUID, time.Since(startWrite))
							}
						}
					}
					
					// Finish timing RTP processing
					if finishProcessingTimer != nil {
						finishProcessingTimer()
					}
					
					// Return the buffer to the pool now that we're done with it
					returnBuffer(buffer)
				}
			}
		}
	}()
}

// SetUDPSocketBuffers sets optimal socket buffer sizes for RTP traffic
func SetUDPSocketBuffers(conn *net.UDPConn, logger *logrus.Logger) {
	// Set read buffer size to handle network bursts
	const readBufferSize = 16 * 1024 * 1024
	err := conn.SetReadBuffer(readBufferSize)
	if err != nil {
		logger.WithError(err).Warn("Failed to set UDP read buffer size, using system default")
	} else {
		logger.WithField("size_bytes", readBufferSize).Debug("Set UDP read buffer size")
	}

	// Set write buffer size for outgoing packets
	const writeBufferSize = 1 * 1024 * 1024
	err = conn.SetWriteBuffer(writeBufferSize)
	if err != nil {
		logger.WithError(err).Warn("Failed to set UDP write buffer size, using system default")
	} else {
		logger.WithField("size_bytes", writeBufferSize).Debug("Set UDP write buffer size")
	}

	// Configure socket to not delay small packets
	if fd, err := conn.File(); err == nil {
		syscall.SetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_RCVBUF, readBufferSize)
		syscall.SetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_SNDBUF, writeBufferSize)
		fd.Close()
	}
}

// MonitorRTPTimeout monitors for RTP inactivity and cleans up forwarder
func MonitorRTPTimeout(forwarder *RTPForwarder, callUUID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-forwarder.StopChan:
			return
		case <-ticker.C:
			// Check if we've timed out
			if time.Since(forwarder.LastRTPTime) > forwarder.Timeout {
				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"last_rtp":  forwarder.LastRTPTime,
					"timeout":   forwarder.Timeout,
				}).Info("RTP timeout detected, closing forwarder")
				
				// Signal the main goroutine to stop
				close(forwarder.StopChan)
				return
			}
		}
	}
}

// AllocateRTPPort allocates a port for RTP traffic
func AllocateRTPPort(minPort, maxPort int, logger *logrus.Logger) int {
	// Use the port manager to get an available port
	pm := GetPortManager()
	port, err := pm.AllocatePort()
	if err != nil {
		logger.WithError(err).Error("Failed to allocate RTP port, using default port")
		return 10000 // Default fallback port
	}
	
	// Update metrics
	if metrics.IsMetricsEnabled() {
		metrics.PortsInUse.Inc()
	}
	
	logger.WithField("port", port).Debug("Allocated RTP port")
	return port
}