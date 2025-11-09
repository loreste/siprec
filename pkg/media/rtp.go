package media

import (
	"context"
	"fmt"
	"io"
	"math"
	mathrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"siprec-server/pkg/audio"
	"siprec-server/pkg/metrics"
	"siprec-server/pkg/security"
	"siprec-server/pkg/telemetry/tracing"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/srtp/v2"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func init() {
	// Initialize the random number generator
	mathrand.Seed(time.Now().UnixNano())
}

type audioMetricsCollector struct {
	callID      string
	forwarder   *RTPForwarder
	listener    AudioMetricsListener
	interval    time.Duration
	logger      *logrus.Logger
	dtmfCh      chan AcousticEvent
	lastSilence time.Time
	lastHold    time.Time
}

func newAudioMetricsCollector(callID string, forwarder *RTPForwarder, listener AudioMetricsListener, interval time.Duration, dtmfCh chan AcousticEvent, logger *logrus.Logger) *audioMetricsCollector {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &audioMetricsCollector{
		callID:    callID,
		forwarder: forwarder,
		listener:  listener,
		interval:  interval,
		logger:    logger,
		dtmfCh:    dtmfCh,
	}
}

func (c *audioMetricsCollector) run(ctx context.Context) {
	tp := time.NewTicker(c.interval)
	defer tp.Stop()
	windowStart := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-c.dtmfCh:
			c.listener.OnAcousticEvent(c.callID, event)
		case <-tp.C:
			c.collect(windowStart)
			windowStart = time.Now()
		}
	}
}

func (c *audioMetricsCollector) collect(windowStart time.Time) {
	if c.listener == nil || c.forwarder == nil {
		return
	}

	pm, ok := c.forwarder.AudioProcessor.(*audio.ProcessingManager)
	if !ok || pm == nil {
		return
	}

	stats := pm.GetStats()
	packetLoss, jitterSeconds, _ := c.forwarder.RTPStats.Snapshot()
	metrics := AudioMetrics{
		VoiceRatio:  stats.VoiceRatio,
		NoiseFloor:  stats.NoiseFloor,
		PacketLoss:  packetLoss,
		JitterMs:    jitterSeconds * 1000,
		Timestamp:   time.Now(),
		WindowStart: windowStart,
		WindowEnd:   time.Now(),
	}
	metrics.MOS = calculateMOS(metrics.VoiceRatio, metrics.NoiseFloor, metrics.PacketLoss, metrics.JitterMs)
	if stats.PacketsPerSecond > 0 {
		if metrics.Details == nil {
			metrics.Details = make(map[string]any)
		}
		metrics.Details["packets_per_second"] = stats.PacketsPerSecond
	}

	c.listener.OnAudioMetrics(c.callID, metrics)

	events := c.detectAcousticEvents(metrics, stats)
	for _, event := range events {
		c.listener.OnAcousticEvent(c.callID, event)
	}
}

func (c *audioMetricsCollector) detectAcousticEvents(metrics AudioMetrics, stats audio.AudioProcessingStats) []AcousticEvent {
	var events []AcousticEvent
	now := time.Now()

	if metrics.VoiceRatio < 0.05 {
		if now.Sub(c.lastSilence) > 15*time.Second {
			c.lastSilence = now
			events = append(events, AcousticEvent{
				Type:       "silence",
				Confidence: 0.9,
				Timestamp:  now,
				Details: map[string]interface{}{
					"voice_ratio": metrics.VoiceRatio,
				},
			})
		}
	} else if metrics.VoiceRatio < 0.3 && metrics.NoiseFloor > -45 {
		if now.Sub(c.lastHold) > 20*time.Second {
			c.lastHold = now
			events = append(events, AcousticEvent{
				Type:       "hold_music",
				Confidence: 0.6,
				Timestamp:  now,
				Details: map[string]interface{}{
					"voice_ratio": metrics.VoiceRatio,
					"noise_floor": metrics.NoiseFloor,
				},
			})
		}
	}

	return events
}

func calculateMOS(voiceRatio, noiseFloor, packetLoss, jitterMs float64) float64 {
	voiceQuality := clamp(voiceRatio, 0, 1)
	noiseQuality := 1.0
	if noiseFloor != 0 {
		normalized := clamp((noiseFloor+120)/100, 0, 1)
		noiseQuality = clamp(1-normalized, 0, 1)
	}
	lossQuality := clamp(1-(packetLoss*4), 0, 1)
	jitterQuality := clamp(1-(math.Min(jitterMs, 200)/200), 0, 1)

	score := 0.4*voiceQuality + 0.3*noiseQuality + 0.2*lossQuality + 0.1*jitterQuality
	mos := 1 + 4*score
	return clamp(mos, 1, 5)
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

type encryptedRecordingWriter struct {
	manager   *audio.EncryptedRecordingManager
	sessionID string
}

func (w *encryptedRecordingWriter) Write(p []byte) (int, error) {
	if w.manager == nil || w.sessionID == "" {
		return 0, fmt.Errorf("encrypted recorder not initialized")
	}
	if err := w.manager.WriteAudio(w.sessionID, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// StartRTPForwarding starts forwarding RTP packets for a call

func StartRTPForwarding(ctx context.Context, forwarder *RTPForwarder, callUUID string, config *Config, sttProvider func(context.Context, string, io.Reader, string) error) {
	go func() {
		rtpCtx, rtpSpan := tracing.StartSpan(ctx, "rtp.forward", trace.WithAttributes(
			attribute.String("call.id", callUUID),
			attribute.Int("rtp.local_port", forwarder.LocalPort),
		))
		defer rtpSpan.End()
		ctx = rtpCtx

		defer func() {
			if r := recover(); r != nil {
				forwarder.Logger.WithFields(logrus.Fields{
					"panic":     r,
					"call_uuid": callUUID,
				}).Error("Panic in RTP forwarding goroutine")
				rtpSpan.RecordError(fmt.Errorf("panic: %v", r))
				rtpSpan.SetStatus(codes.Error, "panic during RTP forwarding")
			}
			forwarder.Cleanup()
		}()

		var endSessionMetrics func()
		if metrics.IsMetricsEnabled() {
			endSessionMetrics = metrics.StartSessionTimer("rtp_forwarding")
			if endSessionMetrics != nil {
				defer endSessionMetrics()
			}
		}

		listenAddr := &net.UDPAddr{Port: forwarder.LocalPort}
		if config.BehindNAT {
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
			rtpSpan.RecordError(err)
			rtpSpan.SetStatus(codes.Error, "listen udp failed")
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPDroppedPackets(callUUID, "listen_failure", 1)
			}
			return
		}
		forwarder.Conn = udpConn
		forwarder.LastRTPTime = time.Now()

		SetUDPSocketBuffers(udpConn, forwarder.Logger)

		var rtcpConn *net.UDPConn
		if !forwarder.UseRTCPMux && forwarder.RTCPPort > 0 {
			rtcpAddr := &net.UDPAddr{Port: forwarder.RTCPPort}
			if listenAddr.IP != nil {
				rtcpAddr.IP = listenAddr.IP
			}
			rtcpConn, err = net.ListenUDP("udp", rtcpAddr)
			if err != nil {
				forwarder.Logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"port":      forwarder.RTCPPort,
				}).Error("Failed to listen on UDP port for RTCP")
				rtpSpan.RecordError(err)
				rtpSpan.SetStatus(codes.Error, "listen udp rtcp failed")
				udpConn.Close()
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "rtcp_listen_failure", 1)
				}
				return
			}
			forwarder.RTCPConn = rtcpConn
			SetUDPSocketBuffers(rtcpConn, forwarder.Logger)
		}

		sanitizedUUID := security.SanitizeCallUUID(callUUID)
		forwarder.CallUUID = callUUID
		forwarder.Storage = config.RecordingStorage

		sampleRate := forwarder.SampleRate
		if sampleRate == 0 {
			sampleRate = 8000
		}
		channels := forwarder.Channels
		if channels == 0 {
			channels = 1
		}

		var baseRecordingWriter io.Writer

		if forwarder.EncryptedRecorder != nil {
			sessionID := fmt.Sprintf("%s-%d", sanitizedUUID, forwarder.LocalPort)
			metadata := &audio.RecordingMetadata{
				SessionID:    sessionID,
				Codec:        forwarder.CodecName,
				SampleRate:   sampleRate,
				Channels:     channels,
				FileFormat:   "siprec",
				Participants: nil,
			}

			encSession, err := forwarder.EncryptedRecorder.StartRecording(sessionID, metadata)
			if err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to initialize encrypted recording session")
				rtpSpan.RecordError(err)
				rtpSpan.SetStatus(codes.Error, "encrypted_recording_init_failed")
				return
			}

			forwarder.EncryptedSessionID = sessionID
			forwarder.RecordingPath = encSession.FilePath
			baseRecordingWriter = &encryptedRecordingWriter{
				manager:   forwarder.EncryptedRecorder,
				sessionID: sessionID,
			}

			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid":  callUUID,
				"session_id": sessionID,
				"path":       forwarder.RecordingPath,
			}).Info("Encrypted recording session started")
		} else {
			filePath := filepath.Join(config.RecordingDir, fmt.Sprintf("%s.wav", sanitizedUUID))
			forwarder.RecordingFile, err = os.Create(filePath)
			if err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to create recording file")
				rtpSpan.RecordError(err)
				rtpSpan.SetStatus(codes.Error, "recording file creation failed")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "file_creation_failed", 1)
				}
				return
			}
			forwarder.RecordingPath = filePath

			wavWriter, err := NewWAVWriter(forwarder.RecordingFile, sampleRate, channels)
			if err != nil {
				forwarder.Logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid":   callUUID,
					"sample_rate": sampleRate,
					"channels":    channels,
				}).Error("Failed to initialize WAV writer")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "wav_writer_init_failed", 1)
				}
				return
			}
			forwarder.WAVWriter = wavWriter
			baseRecordingWriter = wavWriter

			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid":   callUUID,
				"sample_rate": sampleRate,
				"channels":    channels,
			}).Debug("Initialized WAV writer for recording")
		}

		if baseRecordingWriter == nil {
			forwarder.Logger.WithField("call_uuid", callUUID).Error("Recording writer was not initialized")
			rtpSpan.SetStatus(codes.Error, "recording_writer_missing")
			return
		}

		var srtpSession *srtp.SessionSRTP
		if config.EnableSRTP {
			if len(forwarder.SRTPMasterKey) == 0 || len(forwarder.SRTPMasterSalt) == 0 {
				err := fmt.Errorf("missing SRTP keying material in SDP offer")
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Cannot establish SRTP session")
				rtpSpan.RecordError(err)
				rtpSpan.SetStatus(codes.Error, "srtp key missing")
				return
			}

			profile := determineSRTPProfile(forwarder.SRTPProfile)
			if profile == 0 {
				profile = srtp.ProtectionProfileAes128CmHmacSha1_80
			}

			localKey := append([]byte(nil), forwarder.SRTPMasterKey...)
			localSalt := append([]byte(nil), forwarder.SRTPMasterSalt...)

			srtpConfig := &srtp.Config{
				Profile: profile,
				Keys: srtp.SessionKeys{
					LocalMasterKey:   localKey,
					LocalMasterSalt:  localSalt,
					RemoteMasterKey:  localKey,
					RemoteMasterSalt: localSalt,
				},
			}

			srtpSession, err = srtp.NewSessionSRTP(udpConn, srtpConfig)
			if err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to set up SRTP session")
				if metrics.IsMetricsEnabled() {
					metrics.RecordSRTPEncryptionErrors(callUUID, "session_setup_failed", 1)
				}
				return
			}

			forwarder.SRTPEnabled = true
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"profile":   srtpProfileName(profile),
			}).Info("SRTP session successfully set up")
		}

		if config.AudioProcessing.Enabled {
			audioConfig := audio.ProcessingConfig{
				EnableVAD:            config.AudioProcessing.EnableVAD,
				VADThreshold:         config.AudioProcessing.VADThreshold,
				VADHoldTime:          config.AudioProcessing.VADHoldTimeMs / 20,
				EnableNoiseReduction: config.AudioProcessing.EnableNoiseReduction,
				NoiseFloor:           config.AudioProcessing.NoiseReductionLevel,
				NoiseAttenuationDB:   12.0,
				ChannelCount:         config.AudioProcessing.ChannelCount,
				MixChannels:          config.AudioProcessing.MixChannels,
				SampleRate:           8000,
				FrameSize:            160,
				BufferSize:           2048,
			}
			forwarder.AudioProcessor = audio.NewProcessingManager(audioConfig, forwarder.Logger)
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid":       callUUID,
				"vad_enabled":     config.AudioProcessing.EnableVAD,
				"noise_reduction": config.AudioProcessing.EnableNoiseReduction,
				"channels":        config.AudioProcessing.ChannelCount,
			}).Info("Audio processing initialized")
		}

		var dtmfCh chan AcousticEvent
		if config.AudioMetricsListener != nil {
			dtmfCh = make(chan AcousticEvent, 16)
			collector := newAudioMetricsCollector(callUUID, forwarder, config.AudioMetricsListener, config.AudioMetricsInterval, dtmfCh, forwarder.Logger)
			go collector.run(ctx)
		}

		mainPr, mainPw := io.Pipe()
		recordingWriter := NewPausableWriter(baseRecordingWriter)
		recordingReader := io.TeeReader(mainPr, recordingWriter)
		transcriptionReader := NewPausableReader(recordingReader)
		forwarder.recordingWriter = recordingWriter
		forwarder.transcriptionReader = transcriptionReader

		forwarder.Logger.WithField("call_uuid", callUUID).Debug("Starting transcription stream")
		rtpSpan.AddEvent("stt.dispatch", trace.WithAttributes(attribute.String("stt.vendor", config.DefaultVendor)))
		go sttProvider(ctx, "", transcriptionReader, callUUID)

		go MonitorRTPTimeout(forwarder, callUUID)
		go startRTCPSender(ctx, forwarder)
		if rtcpConn != nil {
			go readIncomingRTCP(forwarder, rtcpConn)
		}

		decodeAndProcess := func(packet []byte, arrival time.Time, remoteAddr *net.UDPAddr) {
			if len(packet) == 0 {
				return
			}

			var rtpPacket rtp.Packet
			if err := rtpPacket.Unmarshal(packet); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Warn("Failed to unmarshal RTP packet")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "parse_error", 1)
				}
				return
			}

			forwarder.LastRTPTime = time.Now()

			if forwarder.RemoteSSRC == 0 {
				forwarder.RemoteSSRC = rtpPacket.SSRC
			}
			if forwarder.RTPStats != nil {
				forwarder.RTPStats.Update(&rtpPacket, arrival)
			}
			forwarder.updateRemoteSession(remoteAddr, &rtpPacket)

			if forwarder.CodecPayloadType == 0 {
				forwarder.CodecPayloadType = rtpPacket.PayloadType
			}

			if forwarder.CodecName == "" || forwarder.SampleRate == 0 {
				if info, ok := GetCodecInfo(byte(rtpPacket.PayloadType)); ok {
					forwarder.SetCodecInfo(byte(rtpPacket.PayloadType), info.Name, info.SampleRate, info.Channels)
					if forwarder.WAVWriter != nil {
						_ = forwarder.WAVWriter.SetFormat(forwarder.SampleRate, forwarder.Channels)
					}
				}
			}

			payload := rtpPacket.Payload
			if len(payload) == 0 {
				return
			}

			if dtmfCh != nil && (rtpPacket.PayloadType == 101 || strings.EqualFold(forwarder.CodecName, "TELEPHONE-EVENT")) {
				select {
				case dtmfCh <- AcousticEvent{
					Type:       "dtmf",
					Confidence: 0.9,
					Timestamp:  time.Now(),
					Details: map[string]interface{}{
						"payload_type": rtpPacket.PayloadType,
					},
				}:
				default:
				}
			}

			codecName := forwarder.CodecName
			if codecName == "" {
				codecName = "PCMU"
			}

			pcm, err := DecodeAudioPayload(payload, codecName)
			if err != nil {
				forwarder.Logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid":    callUUID,
					"codec":        codecName,
					"payload_type": rtpPacket.PayloadType,
				}).Warn("Failed to decode audio payload to PCM")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "decode_error", 1)
				}
				return
			}
			if len(pcm) == 0 {
				return
			}

			processed := pcm
			if config.AudioProcessing.Enabled && forwarder.AudioProcessor != nil {
				if processingManager, ok := forwarder.AudioProcessor.(*audio.ProcessingManager); ok {
					var finishProcessingTimer func()
					if metrics.IsMetricsEnabled() {
						finishProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "audio_processing")
					}
					processed, err = processingManager.ProcessAudio(pcm)
					if finishProcessingTimer != nil {
						finishProcessingTimer()
					}
					if err != nil {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to process audio chunk")
						if metrics.IsMetricsEnabled() {
							metrics.RecordAudioProcessingError(callUUID, "processing_error", 1)
						}
						return
					}
					if len(processed) == 0 {
						return
					}
				}
			}

			forwarder.pauseMutex.RLock()
			paused := forwarder.RecordingPaused
			forwarder.pauseMutex.RUnlock()
			if paused {
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "recording_paused", 1)
				}
				return
			}

			startWrite := time.Now()
			if _, err := mainPw.Write(processed); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write PCM audio to stream")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets(callUUID, "write_error", 1)
				}
				return
			}
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPLatency(callUUID, time.Since(startWrite))
			}
		}

		for {
			select {
			case <-forwarder.StopChan:
				mainPw.Close()
				return
			case <-ctx.Done():
				mainPw.Close()
				return
			default:
				buffer, returnBuffer := GetPacketBuffer(1500)
				udpConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, addr, err := udpConn.ReadFromUDP(buffer)
				if err != nil {
					returnBuffer(buffer)
					if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to read RTP packet")
						if metrics.IsMetricsEnabled() {
							metrics.RecordRTPDroppedPackets(callUUID, "read_error", 1)
						}
					}
					continue
				}

				if n == 0 {
					returnBuffer(buffer)
					continue
				}

				arrival := time.Now()

				if forwarder.UseRTCPMux && isRTCPPacket(buffer[:n]) {
					handleRTCPPacket(forwarder, buffer[:n], addr)
					returnBuffer(buffer)
					continue
				}

				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPPacket(callUUID, n)
				}

				if config.EnableSRTP && srtpSession != nil {
					forwarder.SRTPEnabled = true
					decryptedRTP, returnDecryptedBuffer := GetPacketBuffer(n + 64)
					var finishProcessingTimer func()
					if metrics.IsMetricsEnabled() {
						finishProcessingTimer = metrics.ObserveRTPProcessing(callUUID, "srtp_decryption")
					}

					var ssrc uint32
					if n >= 12 {
						ssrc = uint32(buffer[8])<<24 | uint32(buffer[9])<<16 | uint32(buffer[10])<<8 | uint32(buffer[11])
					}

					readStream, err := srtpSession.OpenReadStream(ssrc)
					if err != nil {
						if metrics.IsMetricsEnabled() {
							metrics.RecordSRTPDecryptionErrors(callUUID, "open_stream_error", 1)
						}
						forwarder.Logger.WithError(err).WithFields(logrus.Fields{
							"call_uuid": callUUID,
							"ssrc":      ssrc,
						}).Debug("Failed to open SRTP read stream, trying default SSRC")
						if finishProcessingTimer != nil {
							finishProcessingTimer()
						}
						returnBuffer(buffer)
						returnDecryptedBuffer(decryptedRTP)
						continue
					}

					decryptedLen, err := readStream.Read(decryptedRTP[:cap(decryptedRTP)])
					if finishProcessingTimer != nil {
						finishProcessingTimer()
					}
					if err != nil {
						if metrics.IsMetricsEnabled() {
							metrics.RecordSRTPDecryptionErrors(callUUID, "read_error", 1)
						}
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to read from SRTP stream")
						returnBuffer(buffer)
						returnDecryptedBuffer(decryptedRTP)
						continue
					}

					if metrics.IsMetricsEnabled() {
						metrics.RecordSRTPPacketsProcessed(callUUID, "rx", 1)
					}

					decodeAndProcess(decryptedRTP[:decryptedLen], arrival, addr)
					returnDecryptedBuffer(decryptedRTP)
					returnBuffer(buffer)
					continue
				}

				decodeAndProcess(buffer[:n], arrival, addr)
				returnBuffer(buffer)
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
				forwarder.Stop()
				return
			}
		}
	}
}

func startRTCPSender(ctx context.Context, forwarder *RTPForwarder) {
	if forwarder == nil {
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

forLoop:
	for {
		select {
		case <-forwarder.StopChan:
			break forLoop
		case <-forwarder.rtcpStopChan:
			break forLoop
		case <-ctx.Done():
			break forLoop
		case <-ticker.C:
			forwarder.sendReceiverReport()
		}
	}
}

func readIncomingRTCP(forwarder *RTPForwarder, conn *net.UDPConn) {
	if forwarder == nil || conn == nil {
		return
	}

	buffer := make([]byte, 1500)
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, addr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				select {
				case <-forwarder.StopChan:
					return
				default:
				}
				continue
			}
			return
		}
		handleRTCPPacket(forwarder, buffer[:n], addr)
	}
}

func isRTCPPacket(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}
	packetType := payload[1]
	return packetType >= 200 && packetType <= 211
}

func handleRTCPPacket(forwarder *RTPForwarder, data []byte, addr *net.UDPAddr) {
	if len(data) == 0 || forwarder == nil {
		return
	}

	packets, err := rtcp.Unmarshal(data)
	if err != nil {
		forwarder.Logger.WithError(err).Debug("Failed to unmarshal RTCP packet")
		return
	}

	for _, pkt := range packets {
		switch p := pkt.(type) {
		case *rtcp.SenderReport:
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": forwarder.CallUUID,
				"ssrc":      p.SSRC,
				"addr":      addr,
			}).Trace("Received RTCP Sender Report")
		case *rtcp.Goodbye:
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": forwarder.CallUUID,
				"addr":      addr,
			}).Info("Received RTCP BYE")
		case *rtcp.SourceDescription:
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": forwarder.CallUUID,
				"addr":      addr,
			}).Trace("Received RTCP SDES")
		default:
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid": forwarder.CallUUID,
				"type":      fmt.Sprintf("%T", pkt),
				"addr":      addr,
			}).Trace("Received RTCP packet")
		}
	}
}

func (forwarder *RTPForwarder) sendReceiverReport() {
	if forwarder == nil || forwarder.RTPStats == nil {
		return
	}

	forwarder.remoteMutex.Lock()
	remoteAddr := forwarder.RemoteRTCPAddr
	forwarder.remoteMutex.Unlock()

	if remoteAddr == nil || forwarder.RemoteSSRC == 0 {
		return
	}

	report := forwarder.RTPStats.buildReceptionReport(forwarder.RemoteSSRC)
	if report == nil {
		return
	}

	rr := &rtcp.ReceiverReport{
		SSRC:    forwarder.LocalSSRC,
		Reports: []rtcp.ReceptionReport{*report},
	}

	cname := fmt.Sprintf("siprec-%s", forwarder.CallUUID)
	sdes := &rtcp.SourceDescription{
		Chunks: []rtcp.SourceDescriptionChunk{
			{
				Source: forwarder.LocalSSRC,
				Items: []rtcp.SourceDescriptionItem{
					{Type: rtcp.SDESCNAME, Text: cname},
				},
			},
		},
	}

	if err := sendRTCPPackets(forwarder, rr, sdes); err != nil {
		forwarder.Logger.WithError(err).WithField("call_uuid", forwarder.CallUUID).Debug("Failed to send RTCP receiver report")
	}
}

func sendRTCPPackets(forwarder *RTPForwarder, packets ...rtcp.Packet) error {
	if forwarder == nil || len(packets) == 0 {
		return nil
	}

	forwarder.remoteMutex.Lock()
	remote := forwarder.RemoteRTCPAddr
	forwarder.remoteMutex.Unlock()

	if remote == nil {
		return fmt.Errorf("no remote RTCP address")
	}

	raw, err := rtcp.Marshal(packets)
	if err != nil {
		return err
	}

	var conn *net.UDPConn
	if forwarder.UseRTCPMux {
		conn = forwarder.Conn
	} else {
		conn = forwarder.RTCPConn
	}
	if conn == nil {
		return fmt.Errorf("no RTCP socket")
	}

	_, err = conn.WriteToUDP(raw, remote)
	return err
}

func sendRTCPBye(forwarder *RTPForwarder) {
	if forwarder == nil {
		return
	}
	bye := &rtcp.Goodbye{Sources: []uint32{forwarder.LocalSSRC}}
	if err := sendRTCPPackets(forwarder, bye); err != nil {
		forwarder.Logger.WithError(err).WithField("call_uuid", forwarder.CallUUID).Debug("Failed to send RTCP BYE")
	}
}

func (forwarder *RTPForwarder) updateRemoteSession(addr *net.UDPAddr, pkt *rtp.Packet) {
	if forwarder == nil || addr == nil {
		return
	}

	forwarder.remoteMutex.Lock()
	defer forwarder.remoteMutex.Unlock()

	if forwarder.RemoteRTPAddr == nil {
		forwarder.RemoteRTPAddr = copyUDPAddr(addr)
	}

	if forwarder.RemoteRTCPAddr == nil {
		forwarder.RemoteRTCPAddr = forwarder.deriveRemoteRTCPAddr(addr)
	}

	if pkt != nil && forwarder.RemoteSSRC == 0 {
		forwarder.RemoteSSRC = pkt.SSRC
	}
}

func (forwarder *RTPForwarder) deriveRemoteRTCPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	if forwarder.UseRTCPMux {
		return copyUDPAddr(addr)
	}
	port := forwarder.ExpectedRemoteRTCPPort
	if port == 0 {
		port = addr.Port + 1
	}
	return &net.UDPAddr{IP: append([]byte(nil), addr.IP...), Port: port, Zone: addr.Zone}
}

func copyUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	return &net.UDPAddr{IP: append([]byte(nil), addr.IP...), Port: addr.Port, Zone: addr.Zone}
}

func determineSRTPProfile(profile string) srtp.ProtectionProfile {
	switch strings.ToUpper(strings.TrimSpace(profile)) {
	case "AES_CM_128_HMAC_SHA1_32":
		return srtp.ProtectionProfileAes128CmHmacSha1_32
	case "AEAD_AES_128_GCM":
		return srtp.ProtectionProfileAeadAes128Gcm
	case "AEAD_AES_256_GCM":
		return srtp.ProtectionProfileAeadAes256Gcm
	default:
		return srtp.ProtectionProfileAes128CmHmacSha1_80
	}
}

func srtpProfileName(profile srtp.ProtectionProfile) string {
	switch profile {
	case srtp.ProtectionProfileAes128CmHmacSha1_80:
		return "AES_CM_128_HMAC_SHA1_80"
	case srtp.ProtectionProfileAes128CmHmacSha1_32:
		return "AES_CM_128_HMAC_SHA1_32"
	case srtp.ProtectionProfileAeadAes128Gcm:
		return "AEAD_AES_128_GCM"
	case srtp.ProtectionProfileAeadAes256Gcm:
		return "AEAD_AES_256_GCM"
	default:
		return fmt.Sprintf("profile_%d", profile)
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
	if metrics.IsMetricsEnabled() && metrics.PortsInUse != nil {
		metrics.PortsInUse.Inc()
	}

	logger.WithField("port", port).Debug("Allocated RTP port")
	return port
}
