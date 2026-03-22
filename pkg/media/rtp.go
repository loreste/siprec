package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// Note: As of Go 1.20, the random number generator is automatically seeded

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
			c.logger.WithField("call_id", c.callID).Info("Audio metrics collector exiting via ctx.Done()")
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
		_, rtpSpan := tracing.StartSpan(ctx, "rtp.forward", trace.WithAttributes(
			attribute.String("call.id", callUUID),
			attribute.Int("rtp.local_port", forwarder.LocalPort),
		))
		defer rtpSpan.End()
		// Use original ctx for cancellation - don't overwrite with tracing context!

		// Log when goroutine exits (before cleanup)
		defer func() {
			forwarder.Logger.WithField("call_uuid", callUUID).Info("Main RTP goroutine exited (defer)")
		}()

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

		// Finalize WAV before Cleanup so the header is updated.
		// Ensures recordings are playable if the goroutine exits for any reason.
		defer func() {
			if forwarder.WAVWriter != nil {
				if err := forwarder.WAVWriter.Finalize(); err != nil && forwarder.Logger != nil {
					forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Warn("Failed to finalize WAV on RTP goroutine exit")
				}
			}
		}()

		var endSessionMetrics func()
		if metrics.IsMetricsEnabled() {
			endSessionMetrics = metrics.StartSessionTimer("rtp_forwarding")
			if endSessionMetrics != nil {
				defer endSessionMetrics()
			}
		}

		listenAddr := &net.UDPAddr{Port: forwarder.LocalPort}

		// Allow binding to a specific interface if configured
		bindAddr := "0.0.0.0"
		if config.RTPBindIP != "" {
			listenAddr.IP = net.ParseIP(config.RTPBindIP)
			bindAddr = config.RTPBindIP
		}

		forwarder.Logger.WithFields(logrus.Fields{
			"port":    forwarder.LocalPort,
			"bind_ip": bindAddr,
		}).Info("Binding RTP listener")

		udpConn, err := net.ListenUDP("udp", listenAddr)
		if err != nil {
			forwarder.Logger.WithError(err).WithField("port", forwarder.LocalPort).Error("Failed to listen on UDP port for RTP forwarding")
			rtpSpan.RecordError(err)
			rtpSpan.SetStatus(codes.Error, "listen udp failed")
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPDroppedPackets("listen_failure", 1)
			}
			return
		}
		forwarder.CleanupMutex.Lock()
		forwarder.Conn = udpConn
		forwarder.CleanupMutex.Unlock()
		// Initialize last RTP timestamp atomically
		atomic.StoreInt64(&forwarder.lastRTPNano, time.Now().UnixNano())

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
				if closeErr := udpConn.Close(); closeErr != nil {
					forwarder.Logger.WithError(closeErr).Warn("Failed to close UDP connection during cleanup")
				}
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets("rtcp_listen_failure", 1)
				}
				return
			}
			forwarder.CleanupMutex.Lock()
			forwarder.RTCPConn = rtcpConn
			forwarder.CleanupMutex.Unlock()
			SetUDPSocketBuffers(rtcpConn, forwarder.Logger)
		}

		sanitizedUUID := security.SanitizeCallUUID(callUUID)
		forwarder.CleanupMutex.Lock()
		forwarder.CallUUID = callUUID
		forwarder.CleanupMutex.Unlock()
		forwarder.Storage = config.RecordingStorage

		// Get codec info in a thread-safe manner
		_, codecName, sampleRate, channels := forwarder.GetCodecInfo()
		if sampleRate == 0 {
			sampleRate = 8000
		}
		if channels == 0 {
			channels = 1
		}

		var baseRecordingWriter io.Writer

		if forwarder.EncryptedRecorder != nil {
			sessionID := fmt.Sprintf("%s-%d", sanitizedUUID, forwarder.LocalPort)
			metadata := &audio.RecordingMetadata{
				SessionID:    sessionID,
				Codec:        codecName,
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
					metrics.RecordRTPDroppedPackets("file_creation_failed", 1)
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
					metrics.RecordRTPDroppedPackets("wav_writer_init_failed", 1)
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
					metrics.RecordSRTPEncryptionErrors("session_setup_failed", 1)
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

		recordingWriter := NewPausableWriter(baseRecordingWriter)
		forwarder.recordingWriter = recordingWriter

		// Use buffered pipe to decouple RTP handler from STT backpressure (Fix C)
		// Buffer size: ~80ms of audio at 8kHz 16-bit mono = 1280 bytes
		// We use 4096 to handle bursts and varying sample rates
		var (
			sttPipeReader io.ReadCloser
			sttPipeWriter io.WriteCloser
		)

		if sttProvider != nil {
			bufferedReader, bufferedWriter := NewBufferedPipe(4096)
			sttPipeReader = bufferedReader
			sttPipeWriter = bufferedWriter
			transcriptionReader := NewPausableReader(sttPipeReader)
			forwarder.transcriptionReader = transcriptionReader

			forwarder.Logger.WithField("call_uuid", callUUID).Debug("Starting transcription stream")
			rtpSpan.AddEvent("stt.dispatch", trace.WithAttributes(attribute.String("stt.vendor", config.DefaultVendor)))

			go func(reader io.ReadCloser, paused *PausableReader) {
				if err := sttProvider(ctx, "", paused, callUUID); err != nil {
					forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Warn("STT provider exited early; transcription will be disabled")
					// #nosec G104 -- best-effort cleanup, error logged if provider failed
					_ = reader.Close()
					return
				}
				// #nosec G104 -- best-effort cleanup on normal exit
				_ = reader.Close()
			}(sttPipeReader, transcriptionReader)

			defer func() {
				if sttPipeWriter != nil {
					sttPipeWriter.Close()
				}
			}()
		} else {
			forwarder.transcriptionReader = nil
		}

		go MonitorRTPTimeout(ctx, forwarder, callUUID)
		go startRTCPSender(ctx, forwarder)
		if rtcpConn != nil {
			go readIncomingRTCP(forwarder, rtcpConn)
		}

		sttWriter := sttPipeWriter

		// Per-stream G.729 decoder — scoped to this goroutine so there is no
		// cross-call state leakage or data-race on the decoder internals.
		var g729StreamDec *G729StreamDecoder
		defer func() {
			if g729StreamDec != nil {
				g729StreamDec.Close()
			}
		}()

		// Jitter buffer for packet reordering (Fix E)
		// When enabled, packets are buffered and emitted in sequence order
		// instead of being processed immediately or dropped when out-of-order.
		var jitterBuffer *JitterBuffer
		var processingFromBuffer bool // Flag to skip buffer push for already-buffered packets
		jitterBufferEnabled := config.JitterBuffer.Enabled
		if jitterBufferEnabled {
			maxSize := config.JitterBuffer.MaxSize
			if maxSize <= 0 {
				maxSize = 5
			}
			maxDelay := time.Duration(config.JitterBuffer.MaxDelayMs) * time.Millisecond
			if maxDelay <= 0 {
				maxDelay = 60 * time.Millisecond
			}
			jitterBuffer = NewJitterBuffer(JitterBufferConfig{
				MaxSize:  maxSize,
				MaxDelay: maxDelay,
			})
			forwarder.Logger.WithFields(logrus.Fields{
				"call_uuid":    callUUID,
				"max_size":     maxSize,
				"max_delay_ms": maxDelay.Milliseconds(),
			}).Info("Jitter buffer enabled for RTP stream")
		}

		var firstPacketReceived bool
		var ssrcMismatchLogged bool   // rate-limit the mismatch warning to one per stream
		var ssrcMismatchCount uint64  // total stale packets dropped for this stream
		var lastAcceptedSSRC uint32   // tracks the SSRC accepted by the previous packet
		var lastSeq *uint16           // for PLC: insert silence when sequence gaps are detected
		var lastTimestamp uint32      // RTP timestamp of last processed packet
		var hasLastTimestamp bool     // whether lastTimestamp is valid
		var lastDecodedPCMSize int    // actual PCM bytes produced by last decoded packet (for PLC)

		// SSRC correction state: handles two scenarios where the locked SSRC
		// becomes wrong and must be switched:
		//   1. "First-packet poisoning" — after restart, a stale packet locks
		//      the wrong SSRC before the legitimate stream arrives.
		//   2. "Silent SSRC change" — the SBC changes SSRC during hold/unhold
		//      without sending a SIP UPDATE, so our signaling-based reset
		//      never fires.
		// The correction is safe because it ONLY fires when the locked SSRC
		// has gone completely silent (inactivity check) AND the alternate
		// has sustained traffic. When both streams are concurrent, the
		// inactivity check blocks the switch — preventing cross-talk.
		const (
			ssrcCorrectionThreshold  = 50 // packets from an alternate SSRC needed to trigger switch
			ssrcCorrectionInactivity = 30 // consecutive non-locked packets required (locked SSRC must be silent)
		)
		var ssrcLockedAt time.Time            // when RemoteSSRC was last locked from an RTP packet
		var ssrcCorrectionCount uint32        // how many times SSRC was corrected on this stream
		var alternateSSRC uint32              // candidate SSRC that may replace the locked one
		var alternateSSRCCount uint32         // how many packets we've seen from alternateSSRC
		var packetsSinceLastLockedSSRC uint32 // consecutive non-locked packets; reset on each accepted locked-SSRC packet

		defer func() {
			if ssrcMismatchCount > 0 || ssrcCorrectionCount > 0 {
				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid":           callUUID,
					"ssrc_mismatch_total": ssrcMismatchCount,
					"accepted_ssrc":       lastAcceptedSSRC,
					"ssrc_corrections":    ssrcCorrectionCount,
				}).Warn("RTP stream ended with SSRC-mismatched packets dropped")
			}
		}()

		decodeAndProcess := func(packet []byte, arrival time.Time, remoteAddr *net.UDPAddr) {
			if len(packet) == 0 {
				return
			}

			var rtpPacket rtp.Packet
			if err := rtpPacket.Unmarshal(packet); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Warn("Failed to unmarshal RTP packet")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets("parse_error", 1)
				}
				return
			}

			// ── SSRC validation ─────────────────────────────────────────
			// Lock the SSRC from the first RTP packet so that stale
			// traffic on a reused port is filtered out.
			//
			// When the locked SSRC goes silent and a different SSRC
			// shows sustained traffic, the lock is switched. This
			// handles both first-packet poisoning (after restart) and
			// the SBC silently changing SSRC during hold/unhold.
			forwarder.remoteMutex.Lock()
			expectedSSRC := forwarder.RemoteSSRC
			isNewLock := expectedSSRC == 0
			if isNewLock {
				forwarder.RemoteSSRC = rtpPacket.SSRC
				expectedSSRC = rtpPacket.SSRC
				ssrcLockedAt = time.Now()
				alternateSSRC = 0
				alternateSSRCCount = 0
				packetsSinceLastLockedSSRC = 0
			}
			forwarder.remoteMutex.Unlock()

			if isNewLock {
				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid":   callUUID,
					"locked_ssrc": rtpPacket.SSRC,
					"remote_addr": remoteAddr.String(),
					"local_port":  forwarder.LocalPort,
				}).Info("SSRC locked from RTP packet")
			}

			if rtpPacket.SSRC != expectedSSRC {
				packetsSinceLastLockedSSRC++
				corrected := false

				// Block SSRC correction when the legitimate stream is
				// expected to be silent:
				//   - During hold (TranscriptionPaused) — SBC signaled hold
				//   - During RTP gap (RTPSuspended) — SBC stopped RTP
				//     without signaling, forwarder survived timeout
				// In both cases, stale traffic must not be accepted.
				forwarder.pauseMutex.RLock()
				isOnHold := forwarder.TranscriptionPaused
				forwarder.pauseMutex.RUnlock()
				isSuspended := atomic.LoadInt32(&forwarder.RTPSuspended) == 1
				correctionBlocked := isOnHold || isSuspended

				if !correctionBlocked {
					// Track the most common alternate SSRC
					if rtpPacket.SSRC == alternateSSRC {
						alternateSSRCCount++
					} else {
						alternateSSRC = rtpPacket.SSRC
						alternateSSRCCount = 1
					}
				}

				// Switch SSRC when BOTH conditions are met:
				//   1. The alternate has sustained traffic (≥50 packets)
				//   2. The locked SSRC has gone silent (≥30 consecutive
				//      packets with no locked-SSRC traffic)
				// Condition 2 is the key safety guard: when both streams
				// are concurrently active, each locked-SSRC packet resets
				// the counter to 0 — so the switch NEVER fires during
				// concurrent traffic, preventing cross-talk.
				// Also blocked during hold or RTP gap to prevent accepting
				// stale traffic when the legitimate stream is silent by design.
				if !correctionBlocked &&
					alternateSSRCCount >= ssrcCorrectionThreshold &&
					packetsSinceLastLockedSSRC >= ssrcCorrectionInactivity {
					corrected = true
					ssrcCorrectionCount++

					oldSSRC := expectedSSRC
					forwarder.remoteMutex.Lock()
					forwarder.RemoteSSRC = alternateSSRC
					forwarder.remoteMutex.Unlock()

					lastSeq = nil
					hasLastTimestamp = false
					lastDecodedPCMSize = 0
					ssrcMismatchLogged = false
					firstPacketReceived = false
					if g729StreamDec != nil {
						g729StreamDec.Close()
						g729StreamDec = nil
					}

					elapsedSinceLock := time.Since(ssrcLockedAt)

					// Reset alternate tracking for the next potential switch
					alternateSSRC = 0
					alternateSSRCCount = 0
					packetsSinceLastLockedSSRC = 0
					ssrcLockedAt = time.Now()

					forwarder.Logger.WithFields(logrus.Fields{
						"call_uuid":          callUUID,
						"old_ssrc":           oldSSRC,
						"new_ssrc":           rtpPacket.SSRC,
						"correction_number":  ssrcCorrectionCount,
						"dropped_before":     ssrcMismatchCount,
						"elapsed_since_lock": elapsedSinceLock.Milliseconds(),
						"local_port":         forwarder.LocalPort,
					}).Warn("SSRC switched: locked SSRC went silent, accepting new stream")
				}

				if !corrected {
					ssrcMismatchCount++
					if !ssrcMismatchLogged {
						ssrcMismatchLogged = true
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid":     callUUID,
							"expected_ssrc": expectedSSRC,
							"received_ssrc": rtpPacket.SSRC,
							"remote_addr":   remoteAddr.String(),
							"local_port":    forwarder.LocalPort,
							"on_hold":       isOnHold,
							"rtp_suspended": isSuspended,
						}).Warn("Dropping RTP packet with unexpected SSRC")
					}
					if metrics.IsMetricsEnabled() {
						metrics.RecordRTPDroppedPackets("ssrc_mismatch", 1)
					}
					return
				}
			} else {
				packetsSinceLastLockedSSRC = 0
				// Accepted packet from the locked SSRC — clear suspended
				// state so the forwarder knows RTP has resumed.
				if atomic.CompareAndSwapInt32(&forwarder.RTPSuspended, 1, 0) {
					forwarder.Logger.WithFields(logrus.Fields{
						"call_uuid":  callUUID,
						"ssrc":       rtpPacket.SSRC,
						"local_port": forwarder.LocalPort,
					}).Info("RTP resumed after gap — SIPREC forwarder reactivated")
				}
			}

			// Only update activity timer for accepted packets so that
			// stale traffic does not prevent timeout cleanup.
			// Use atomic store for lock-free timestamp update (hot path optimization)
			atomic.StoreInt64(&forwarder.lastRTPNano, time.Now().UnixNano())

			// Log first accepted RTP packet for diagnostics and record start time for WAV alignment (Fix G)
			if !firstPacketReceived {
				firstPacketReceived = true
				// Record first RTP timestamp for leg alignment during WAV combining
				forwarder.firstRTPMutex.Lock()
				if !forwarder.HasFirstRTP {
					forwarder.FirstRTPTimestamp = rtpPacket.Timestamp
					forwarder.FirstRTPWallClock = arrival
					forwarder.HasFirstRTP = true
				}
				forwarder.firstRTPMutex.Unlock()

				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid":      callUUID,
					"remote_addr":    remoteAddr.String(),
					"ssrc":           rtpPacket.SSRC,
					"payload_type":   rtpPacket.PayloadType,
					"sequence":       rtpPacket.SequenceNumber,
					"timestamp":      rtpPacket.Timestamp,
					"local_port":     forwarder.LocalPort,
					"payload_size":   len(rtpPacket.Payload),
					"first_rtp_time": arrival,
				}).Info("First RTP packet received successfully")
			}

			// When the SSRC changes legitimately (after a SIP signaling
			// reset), clear per-stream state so that the gap/PLC logic
			// does not compare timestamps across different RTP sessions.
			if lastAcceptedSSRC != 0 && rtpPacket.SSRC != lastAcceptedSSRC {
				lastSeq = nil
				hasLastTimestamp = false
				lastDecodedPCMSize = 0
				ssrcMismatchLogged = false
				if g729StreamDec != nil {
					g729StreamDec.Close()
					g729StreamDec = nil
				}
				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid": callUUID,
					"old_ssrc":  lastAcceptedSSRC,
					"new_ssrc":  rtpPacket.SSRC,
				}).Info("SSRC changed after signaling reset; RTP stream state cleared")
			}
			lastAcceptedSSRC = rtpPacket.SSRC
			if forwarder.RTPStats != nil {
				forwarder.RTPStats.Update(&rtpPacket, arrival)
			}
			forwarder.updateRemoteSession(remoteAddr, &rtpPacket)

			// Thread-safe codec info access
			currentPayloadType, currentCodecName, currentSampleRate, currentChannels := forwarder.GetCodecInfo()

			if currentPayloadType == 0 {
				forwarder.SetCodecInfo(byte(rtpPacket.PayloadType), currentCodecName, currentSampleRate, currentChannels)
			}

			if currentCodecName == "" || currentSampleRate == 0 {
				if info, ok := GetCodecInfo(byte(rtpPacket.PayloadType)); ok {
					forwarder.SetCodecInfo(byte(rtpPacket.PayloadType), info.Name, info.SampleRate, info.Channels)
					currentCodecName = info.Name
					currentSampleRate = info.SampleRate
					currentChannels = info.Channels
					if forwarder.WAVWriter != nil {
						_ = forwarder.WAVWriter.SetFormat(currentSampleRate, currentChannels)
					}
				}
			}

			payload := rtpPacket.Payload
			if len(payload) == 0 {
				return
			}

			// Skip non-audio payload types: once the audio codec PT is
			// established, packets with a different PT are event payloads
			// (e.g. RFC 2833 DTMF on a dynamic PT) — not audio. Decoding
			// them with the audio codec produces errors or garbage and
			// creates sequence-number gaps that trigger spurious PLC.
			if currentPayloadType != 0 && byte(rtpPacket.PayloadType) != currentPayloadType {
				if dtmfCh != nil {
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
				return
			}

			if dtmfCh != nil && (rtpPacket.PayloadType == 101 || strings.EqualFold(currentCodecName, "TELEPHONE-EVENT")) {
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

			// ── Jitter Buffer Integration (Fix E) ─────────────────────────────
			// When jitter buffer is enabled, push validated packets to the buffer
			// instead of processing immediately. Packets will be popped in
			// sequence order and processed at the end of each tick.
			// Skip if we're already processing a packet from the buffer.
			if jitterBufferEnabled && jitterBuffer != nil && !processingFromBuffer {
				// Make a copy of the packet for buffering
				pktCopy := rtpPacket
				jitterBuffer.Push(&pktCopy, nil, arrival)
				return // Don't process immediately; will be drained from buffer
			}

			codecName := currentCodecName
			if codecName == "" {
				codecName = "PCMU"
			}
			isG729 := codecName == "G729" || codecName == "G.729" || codecName == "G729A"

			// ── PLC / gap handling ──────────────────────────────────────────
			// Runs BEFORE decode so that the G.729 decoder's internal state is
			// advanced through any missing frames, preventing clicks/pops when
			// the next real frame arrives.
			//
			// Two categories of gap:
			//   • Short timestamp gap (≤60 ms): real packet loss → insert
			//     concealment (G.729) or silence (other codecs).
			//   • Large timestamp gap (>60 ms): DTX or ringing/hold → insert
			//     silence proportional to the RTP timestamp delta so that the
			//     recording stays time-aligned with the other call leg.
			sampleRate := currentSampleRate
			if sampleRate <= 0 {
				sampleRate = 8000
			}
			isReordered := false
			dtxTimestampThreshold := uint32(sampleRate * 60 / 1000) // 60 ms
			if lastSeq != nil {
				expectedNext := uint16(*lastSeq + 1)
				seq := rtpPacket.SequenceNumber
				if seq != expectedNext {
					if uint16(*lastSeq-seq) < 32768 {
						isReordered = true
					} else if hasLastTimestamp {
						tsGap := rtpPacket.Timestamp - lastTimestamp
						expectedDelta := uint32(sampleRate / 50) // 20 ms per packet
						if tsGap <= dtxTimestampThreshold && recordingWriter != nil {
							// Short gap → real packet loss
							var lost int
							if seq > expectedNext {
								lost = int(seq - expectedNext)
							} else {
								lost = int(seq) + (65536 - int(expectedNext))
							}
							const maxPLC = 10
							if lost > maxPLC {
								lost = maxPLC
							}
							if lost > 0 {
								if g729StreamDec != nil && isG729 {
									bytesPerPacket := lastDecodedPCMSize
									if bytesPerPacket <= 0 {
										bytesPerPacket = 320
									}
									concealPCM := g729StreamDec.ConcealPackets(lost, bytesPerPacket)
									if len(concealPCM) > 0 {
										if _, writeErr := recordingWriter.Write(concealPCM); writeErr != nil {
											forwarder.Logger.WithError(writeErr).WithField("call_uuid", callUUID).Debug("PLC concealment write failed")
										} else if metrics.IsMetricsEnabled() {
											metrics.RecordRTPDroppedPackets("plc_concealed", float64(lost))
										}
									}
								} else {
									bytesPerPacket := lastDecodedPCMSize
									if bytesPerPacket <= 0 {
										bytesPerPacket = PCMBytesPerPacket(codecName, sampleRate)
									}
									silenceLen := lost * bytesPerPacket
									if silenceLen > 0 {
										silence := make([]byte, silenceLen)
										if _, writeErr := recordingWriter.Write(silence); writeErr != nil {
											forwarder.Logger.WithError(writeErr).WithField("call_uuid", callUUID).Debug("PLC silence write failed")
										} else if metrics.IsMetricsEnabled() {
											metrics.RecordRTPDroppedPackets("plc_concealed", float64(lost))
										}
									}
								}
							}
						} else if recordingWriter != nil {
							// Large gap (ringing / hold): insert silence to keep
							// both legs time-aligned in the combined stereo recording.
							// Guard against unsigned underflow (timestamp < last)
							// by requiring tsGap to fall within a plausible range.
							const minLargeGapSeconds = 3
							const maxLargeGapSeconds = 120
							minLargeGap := uint32(sampleRate * minLargeGapSeconds)
							maxLargeGap := uint32(sampleRate * maxLargeGapSeconds)
							if tsGap >= minLargeGap && tsGap <= maxLargeGap {
								gapSamples := int(tsGap - expectedDelta)
								if gapSamples > 0 {
									gapDurationMs := (gapSamples * 1000) / sampleRate
									forwarder.Logger.WithFields(logrus.Fields{
										"call_uuid":       callUUID,
										"gap_duration_ms": gapDurationMs,
										"gap_samples":     gapSamples,
										"last_seq":        *lastSeq,
										"current_seq":     seq,
										"ssrc":            rtpPacket.SSRC,
									}).Info("Inserting silence for large RTP timestamp gap (hold/ringing)")
									silence := make([]byte, gapSamples*2)
									if _, writeErr := recordingWriter.Write(silence); writeErr != nil {
										forwarder.Logger.WithError(writeErr).WithField("call_uuid", callUUID).Debug("DTX gap silence write failed")
									}
								}
							}
						}
					}
				}
			}

			// G.729 is stateful: decoding a reordered packet with stale
			// predictor state corrupts subsequent frames. Drop it.
			if isReordered && isG729 {
				return
			}

			// ── Decode ──────────────────────────────────────────────────────
			var pcm []byte
			var err error
			if isG729 {
				if g729StreamDec == nil {
					g729StreamDec = NewG729StreamDecoder()
				}
				pcm, err = g729StreamDec.Decode(payload, rtpPacket.SSRC)
			} else {
				pcm, err = DecodeAudioPayload(payload, codecName)
			}
			if err != nil {
				forwarder.Logger.WithError(err).WithFields(logrus.Fields{
					"call_uuid":    callUUID,
					"codec":        codecName,
					"payload_type": rtpPacket.PayloadType,
				}).Warn("Failed to decode audio payload to PCM")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets("decode_error", 1)
				}
				return
			}
			if len(pcm) == 0 {
				return
			}

			lastDecodedPCMSize = len(pcm)

			recordingPayload, transcriptionPayload, procErr := prepareRecordingAndTranscriptionPayloads(pcm, forwarder, config.AudioProcessing.Enabled, callUUID)
			if procErr != nil {
				forwarder.Logger.WithError(procErr).WithField("call_uuid", callUUID).Debug("Failed to process audio chunk")
				if metrics.IsMetricsEnabled() {
					metrics.RecordAudioProcessingError("processing_error", 1)
				}
				return
			}

			forwarder.pauseMutex.RLock()
			paused := forwarder.RecordingPaused
			forwarder.pauseMutex.RUnlock()
			if paused {
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets("recording_paused", 1)
				}
				return
			}

			startWrite := time.Now()
			if _, err := recordingWriter.Write(recordingPayload); err != nil {
				forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Error("Failed to write PCM audio to recording")
				if metrics.IsMetricsEnabled() {
					metrics.RecordRTPDroppedPackets("write_error", 1)
				}
				return
			}
			// Only update sequence/timestamp tracking for non-reordered packets
			if !isReordered {
				seq := rtpPacket.SequenceNumber
				lastSeq = &seq
				lastTimestamp = rtpPacket.Timestamp
				hasLastTimestamp = true
			}
			if sttWriter != nil && len(transcriptionPayload) > 0 {
				if _, err := sttWriter.Write(transcriptionPayload); err != nil {
					if errors.Is(err, io.ErrClosedPipe) {
						forwarder.Logger.WithField("call_uuid", callUUID).Debug("STT stream closed; skipping transcription writes")
					} else {
						forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Warn("Failed to stream audio samples to STT provider")
					}
					if closeErr := sttWriter.Close(); closeErr != nil {
						forwarder.Logger.WithError(closeErr).WithField("call_uuid", callUUID).Debug("Failed to close STT writer")
					}
					sttWriter = nil
				}
			}
			if metrics.IsMetricsEnabled() {
				metrics.RecordRTPLatency(time.Since(startWrite))
			}
		}

		forwarder.Logger.WithField("call_uuid", callUUID).Info("Main RTP goroutine entered main loop")

		// Use a polling approach with VERY short sleep between checks
		// This avoids the broken ReadFromUDP deadline issue
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-forwarder.StopChan:
				forwarder.Logger.WithField("call_uuid", callUUID).Info("Main RTP goroutine exiting via StopChan")
				return
			case <-ctx.Done():
				forwarder.Logger.WithField("call_uuid", callUUID).Info("Main RTP goroutine exiting via ctx.Done()")
				return
			case <-ticker.C:
				// Drain all buffered packets in this tick to avoid per-leg latency variance.
				// Each read uses a short deadline; on timeout we exit the drain loop.
				_ = udpConn.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
				for {
					buffer, returnBuffer := GetPacketBuffer(1500)
					n, addr, err := udpConn.ReadFromUDP(buffer)
					if err != nil {
						returnBuffer(buffer)
						if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
							break
						}
						if strings.Contains(err.Error(), "use of closed") ||
							strings.Contains(err.Error(), "closed network") ||
							strings.Contains(err.Error(), "bad file descriptor") {
							forwarder.Logger.WithField("call_uuid", callUUID).Info("Connection closed, exiting")
							return
						}
						break
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
						metrics.RecordRTPPacket(n)
					}

					var processBuffer []byte
					processReturnBuffer := func() { returnBuffer(buffer) }

					if config.EnableSRTP && srtpSession != nil {
						forwarder.SRTPEnabled = true
						decryptedRTP, returnDecryptedBuffer := GetPacketBuffer(n + 64)
						var finishProcessingTimer func()
						if metrics.IsMetricsEnabled() {
							finishProcessingTimer = metrics.ObserveRTPProcessing("srtp_decryption")
						}
						var ssrc uint32
						if n >= 12 {
							ssrc = uint32(buffer[8])<<24 | uint32(buffer[9])<<16 | uint32(buffer[10])<<8 | uint32(buffer[11])
						}
						readStream, err := srtpSession.OpenReadStream(ssrc)
						if err != nil {
							if metrics.IsMetricsEnabled() {
								metrics.RecordSRTPDecryptionErrors("open_stream_error", 1)
							}
							forwarder.Logger.WithError(err).WithFields(logrus.Fields{
								"call_uuid": callUUID,
								"ssrc":      ssrc,
							}).Debug("Failed to open SRTP read stream")
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
								metrics.RecordSRTPDecryptionErrors("read_error", 1)
							}
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to read from SRTP stream")
							returnBuffer(buffer)
							returnDecryptedBuffer(decryptedRTP)
							continue
						}
						if metrics.IsMetricsEnabled() {
							metrics.RecordSRTPPacketsProcessed("rx", 1)
						}
						processBuffer = decryptedRTP[:decryptedLen]
						processReturnBuffer = func() {
							returnBuffer(buffer)
							returnDecryptedBuffer(decryptedRTP)
						}
					} else {
						processBuffer = buffer[:n]
					}

					decodeAndProcess(processBuffer, arrival, addr)
					processReturnBuffer()
				}

				// ── Drain Jitter Buffer (Fix E) ───────────────────────────────
				// After processing all UDP packets, drain packets from jitter buffer
				// in sequence order and process them.
				if jitterBufferEnabled && jitterBuffer != nil {
					processingFromBuffer = true
					for {
						bufferedPkt := jitterBuffer.Pop()
						if bufferedPkt == nil {
							break
						}
						// Serialize the buffered packet back to bytes for processing
						rawBytes, err := bufferedPkt.Packet.Marshal()
						if err != nil {
							forwarder.Logger.WithError(err).WithField("call_uuid", callUUID).Debug("Failed to marshal buffered RTP packet")
							continue
						}
						// Process the buffered packet (SSRC validation was already done)
						// processingFromBuffer flag prevents re-pushing to buffer
						decodeAndProcess(rawBytes, bufferedPkt.Arrival, nil)
					}
					processingFromBuffer = false
				}
			}
		}
	}()
}

// SetUDPSocketBuffers sets optimal socket buffer sizes for RTP traffic.
// Uses SyscallConn().Control() instead of conn.File() to avoid putting the socket
// into blocking mode (conn.File() breaks SetReadDeadline and can cause RTP goroutines to hang on BYE).
func SetUDPSocketBuffers(conn *net.UDPConn, logger *logrus.Logger) {
	const readBufferSize = 16 * 1024 * 1024
	if err := conn.SetReadBuffer(readBufferSize); err != nil {
		logger.WithError(err).Warn("Failed to set UDP read buffer size, using system default")
	} else {
		logger.WithField("size_bytes", readBufferSize).Debug("Set UDP read buffer size")
	}

	const writeBufferSize = 1 * 1024 * 1024
	if err := conn.SetWriteBuffer(writeBufferSize); err != nil {
		logger.WithError(err).Warn("Failed to set UDP write buffer size, using system default")
	} else {
		logger.WithField("size_bytes", writeBufferSize).Debug("Set UDP write buffer size")
	}

	// Use SyscallConn to set options without entering blocking mode
	rawConn, err := conn.SyscallConn()
	if err == nil {
		_ = rawConn.Control(func(fd uintptr) {
			// #nosec G104 -- best-effort socket options, system defaults apply if these fail
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, readBufferSize)
			// #nosec G104 -- best-effort socket options, system defaults apply if these fail
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_SNDBUF, writeBufferSize)
		})
	}
}

// MonitorRTPTimeout monitors for RTP inactivity and cleans up forwarder
func MonitorRTPTimeout(ctx context.Context, forwarder *RTPForwarder, callUUID string) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	defer forwarder.Logger.WithField("call_uuid", callUUID).Info("RTP timeout monitor exited")

	var timeoutWarningIssued bool

	for {
		select {
		case <-forwarder.StopChan:
			forwarder.Logger.WithField("call_uuid", callUUID).Info("RTP timeout monitor exiting via StopChan")
			return
		case <-ctx.Done():
			forwarder.Logger.WithField("call_uuid", callUUID).Info("RTP timeout monitor exiting via ctx.Done()")
			return
		case <-ticker.C:
			// During hold, the SBC stops sending RTP. Keep the
			// forwarder alive so it can resume when the call unholds.
			forwarder.pauseMutex.RLock()
			isOnHold := forwarder.TranscriptionPaused
			forwarder.pauseMutex.RUnlock()
			if isOnHold {
				atomic.StoreInt64(&forwarder.lastRTPNano, time.Now().UnixNano())
				timeoutWarningIssued = false
				continue
			}

			// Check how long since last RTP packet (lock-free read)
			lastNano := atomic.LoadInt64(&forwarder.lastRTPNano)
			lastActivity := time.Unix(0, lastNano)
			timeSinceLastRTP := time.Since(lastActivity)

			// Issue warning at 50% timeout threshold
			if !timeoutWarningIssued && timeSinceLastRTP > forwarder.Timeout/2 {
				timeoutWarningIssued = true
				forwarder.remoteMutex.Lock()
				remoteAddr := forwarder.RemoteRTPAddr
				forwarder.remoteMutex.Unlock()

				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid":           callUUID,
					"time_since_last_rtp": timeSinceLastRTP.String(),
					"timeout_threshold":   forwarder.Timeout.String(),
					"local_port":          forwarder.LocalPort,
					"remote_addr":         remoteAddr,
					"ssrc":                forwarder.RemoteSSRC,
				}).Warn("RTP stream inactive - no packets received for extended period")
			}

			// Check if we've timed out
			if timeSinceLastRTP > forwarder.Timeout {
				forwarder.remoteMutex.Lock()
				remoteAddr := forwarder.RemoteRTPAddr
				forwarder.remoteMutex.Unlock()

				// SIPREC sessions have a clear lifecycle signal (BYE).
				// The SBC may stop sending RTP during hold/transfer
				// without signaling via UPDATE, so we keep the forwarder
				// alive and let the SIP layer handle cleanup. Only log
				// a warning and reset the timer so we keep monitoring.
				if forwarder.RecordingSession != nil {
					if atomic.CompareAndSwapInt32(&forwarder.RTPSuspended, 0, 1) {
						forwarder.Logger.WithFields(logrus.Fields{
							"call_uuid":           callUUID,
							"last_rtp_time":       lastActivity.Format(time.RFC3339),
							"time_since_last_rtp": timeSinceLastRTP.String(),
							"timeout_threshold":   forwarder.Timeout.String(),
							"local_port":          forwarder.LocalPort,
							"remote_addr":         remoteAddr,
							"remote_ssrc":         forwarder.RemoteSSRC,
						}).Warn("RTP timeout on SIPREC forwarder — keeping alive until BYE, SSRC correction blocked (SBC may have stopped RTP without signaling hold)")
					}
					atomic.StoreInt64(&forwarder.lastRTPNano, time.Now().UnixNano())
					timeoutWarningIssued = false
					continue
				}

				forwarder.Logger.WithFields(logrus.Fields{
					"call_uuid":           callUUID,
					"last_rtp_time":       lastActivity.Format(time.RFC3339),
					"time_since_last_rtp": timeSinceLastRTP.String(),
					"timeout_threshold":   forwarder.Timeout.String(),
					"local_port":          forwarder.LocalPort,
					"remote_addr":         remoteAddr,
					"remote_ssrc":         forwarder.RemoteSSRC,
				}).Error("RTP timeout detected - closing forwarder. Check firewall/NAT configuration and ensure RTP packets are reaching the server.")

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
			forwarder.Logger.Info("RTCP sender exiting via StopChan")
			break forLoop
		case <-forwarder.rtcpStopChan:
			forwarder.Logger.Info("RTCP sender exiting via rtcpStopChan")
			break forLoop
		case <-ctx.Done():
			forwarder.Logger.Info("RTCP sender exiting via ctx.Done()")
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

	defer forwarder.Logger.Info("RTCP reader goroutine exited")

	// Use polling approach with non-blocking reads to avoid ReadFromUDP blocking issue
	ticker := time.NewTicker(50 * time.Millisecond) // Check every 50ms
	defer ticker.Stop()

	buffer := make([]byte, 1500)

	for {
		select {
		case <-forwarder.StopChan:
			forwarder.Logger.Info("RTCP reader exiting via StopChan")
			return
		case <-ticker.C:
			// Non-blocking read with immediate deadline
			_ = conn.SetReadDeadline(time.Now())
			n, addr, err := conn.ReadFromUDP(buffer)

			if err != nil {
				// Timeout is expected for non-blocking reads
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				// Non-timeout error (connection closed, etc.)
				if strings.Contains(err.Error(), "use of closed") ||
					strings.Contains(err.Error(), "closed network") ||
					strings.Contains(err.Error(), "bad file descriptor") {
					forwarder.Logger.Info("RTCP connection closed, exiting reader")
					return
				}
				// Other error, log and continue
				forwarder.Logger.WithError(err).Debug("RTCP read error")
				continue
			}

			if n > 0 {
				handleRTCPPacket(forwarder, buffer[:n], addr)
			}
		}
	}
}

// prepareRecordingAndTranscriptionPayloads returns the PCM slice that should be written to disk
// and the slice that should be forwarded to the STT pipeline. Disk recordings always receive
// the untouched PCM to keep compliance copies independent of any audio processing.
func prepareRecordingAndTranscriptionPayloads(pcm []byte, forwarder *RTPForwarder, audioProcessingEnabled bool, callUUID string) ([]byte, []byte, error) {
	if len(pcm) == 0 {
		return pcm, pcm, nil
	}

	recordingPayload := pcm
	transcriptionPayload := pcm

	if !audioProcessingEnabled {
		return recordingPayload, transcriptionPayload, nil
	}

	processingManager, ok := forwarder.AudioProcessor.(*audio.ProcessingManager)
	if !ok || processingManager == nil {
		return recordingPayload, transcriptionPayload, nil
	}

	// Copy the raw PCM before running processing so the on-disk recording keeps the original samples.
	recordingPayload = append([]byte(nil), pcm...)

	var finishProcessingTimer func()
	if metrics.IsMetricsEnabled() {
		finishProcessingTimer = metrics.ObserveRTPProcessing("audio_processing")
	}

	processed, err := processingManager.ProcessAudio(pcm)
	if finishProcessingTimer != nil {
		finishProcessingTimer()
	}
	if err != nil {
		return nil, nil, err
	}

	return recordingPayload, processed, nil
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
