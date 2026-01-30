package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

var (
	registry           *prometheus.Registry
	registryOnce       sync.Once
	defaultMetricsPath = "/metrics"
	metricsEnabled     = true

	// RTP metrics
	RTPPacketsReceived *prometheus.CounterVec
	RTPBytesReceived   *prometheus.CounterVec
	RTPPacketLatency   *prometheus.HistogramVec
	RTPDroppedPackets  *prometheus.CounterVec
	RTPProcessingTime  *prometheus.HistogramVec

	// SIP metrics
	SIPRequestsTotal        *prometheus.CounterVec
	SIPResponsesTotal       *prometheus.CounterVec
	SIPSessionsActive       prometheus.Gauge
	SIPSessionDuration      *prometheus.HistogramVec
	SIPSessionEstablishTime *prometheus.HistogramVec

	// IP Access Control metrics
	SIPIPAccessBlocked  *prometheus.CounterVec
	SIPIPAccessAllowed  *prometheus.CounterVec
	SIPAuthFailures     *prometheus.CounterVec

	// Rate limiting metrics
	RateLimitRequestsTotal  *prometheus.CounterVec
	RateLimitBlockedTotal   *prometheus.CounterVec
	RateLimitCurrentBucket  *prometheus.GaugeVec
	SIPRateLimitedTotal     *prometheus.CounterVec

	// SRTP metrics
	SRTPEncryptionErrors *prometheus.CounterVec
	SRTPDecryptionErrors *prometheus.CounterVec
	SRTPPacketsProcessed *prometheus.CounterVec

	// Audio processing metrics
	AudioProcessingLatency *prometheus.HistogramVec
	VADEvents              *prometheus.CounterVec
	NoiseReductionLevel    *prometheus.GaugeVec

	// Resource metrics
	PortsInUse          prometheus.Gauge
	MemoryBuffersActive prometheus.Gauge
	ActiveCalls         prometheus.Gauge

	// STT metrics
	STTRequestsTotal    *prometheus.CounterVec
	STTLatency          *prometheus.HistogramVec
	STTErrors           *prometheus.CounterVec
	STTBytesProcessed   *prometheus.CounterVec
	STTWordsTranscribed *prometheus.CounterVec

	// AMQP metrics
	AMQPPublishedMessages *prometheus.CounterVec
	AMQPConnectionErrors  *prometheus.CounterVec
	AMQPReconnectAttempts *prometheus.CounterVec
	AMQPConnectionStatus  prometheus.Gauge

	// Provider health metrics
	ProviderHealthStatus    *prometheus.GaugeVec
	ProviderHealthCheckTime *prometheus.HistogramVec
	ProviderSelectionScore  *prometheus.GaugeVec
	ProviderCircuitBreaker  *prometheus.GaugeVec

	// Whisper-specific metrics
	WhisperCLIDuration         *prometheus.HistogramVec
	WhisperTempFileDiskUsage   prometheus.Gauge
	WhisperTimeouts            *prometheus.CounterVec
	WhisperOutputFormatCounter *prometheus.CounterVec

	// High-concurrency transcription metrics
	TranscriptionServicePublished    prometheus.Counter
	TranscriptionServiceDropped      prometheus.Counter
	TranscriptionServiceQueueLength  prometheus.Gauge
	TranscriptionServiceHighWater    prometheus.Gauge

	LiveTranscriptionTotal           *prometheus.CounterVec
	ConversationAccumulatorActive    prometheus.Gauge
	ConversationAccumulatorTotal     prometheus.Counter
	ConversationSegmentsTotal        prometheus.Counter

	AMQPListenerPublished            prometheus.Counter
	AMQPListenerFailed               prometheus.Counter
	AMQPListenerDropped              prometheus.Counter
	AMQPListenerTimeouts             prometheus.Counter
	AMQPListenerQueueLength          prometheus.Gauge
	AMQPListenerHighWater            prometheus.Gauge

	// Vendor-specific session metrics
	VendorSessionsActive        *prometheus.GaugeVec
	VendorSessionsTotal         *prometheus.CounterVec
	VendorMetadataExtractions   *prometheus.CounterVec
	VendorHeaderParseErrors     *prometheus.CounterVec
)

// Init initializes all metrics and registers them with Prometheus
func Init(logger *logrus.Logger) {
	registryOnce.Do(func() {
		registry = prometheus.NewRegistry()

		// Initialize RTP metrics
		RTPPacketsReceived = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_rtp_packets_received_total",
				Help: "Total number of RTP packets received",
			},
			[]string{"call_uuid"},
		)

		RTPBytesReceived = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_rtp_bytes_received_total",
				Help: "Total number of RTP bytes received",
			},
			[]string{"call_uuid"},
		)

		RTPPacketLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_rtp_packet_latency_seconds",
				Help:    "Latency of RTP packet processing",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10), // From 0.1ms to ~100ms
			},
			[]string{"call_uuid"},
		)

		RTPDroppedPackets = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_rtp_dropped_packets_total",
				Help: "Total number of dropped RTP packets",
			},
			[]string{"call_uuid", "reason"},
		)

		RTPProcessingTime = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_rtp_processing_time_seconds",
				Help:    "Time taken to process RTP packets",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
			},
			[]string{"call_uuid", "processing_type"},
		)

		// Initialize SIP metrics
		SIPRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_requests_total",
				Help: "Total number of SIP requests",
			},
			[]string{"method", "status"},
		)

		SIPResponsesTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_responses_total",
				Help: "Total number of SIP responses",
			},
			[]string{"status_code", "status_class"},
		)

		SIPSessionsActive = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_sip_sessions_active",
				Help: "Number of active SIP sessions",
			},
		)

		SIPSessionDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_sip_session_duration_seconds",
				Help:    "Duration of SIP sessions",
				Buckets: prometheus.ExponentialBuckets(1, 2, 15), // 1s to ~9 hours
			},
			[]string{"session_type"},
		)

		SIPSessionEstablishTime = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_sip_session_establish_time_seconds",
				Help:    "Time taken to establish SIP sessions",
				Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
			},
			[]string{"session_type"},
		)

		// Initialize IP Access Control metrics
		SIPIPAccessBlocked = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_ip_access_blocked_total",
				Help: "Total number of SIP requests blocked by IP access control",
			},
			[]string{"source_ip", "reason"},
		)

		SIPIPAccessAllowed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_ip_access_allowed_total",
				Help: "Total number of SIP requests allowed by IP access control",
			},
			[]string{"source_ip", "match_type"},
		)

		SIPAuthFailures = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_auth_failures_total",
				Help: "Total number of SIP authentication failures",
			},
			[]string{"source_ip", "reason"},
		)

		// Initialize rate limiting metrics
		RateLimitRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_rate_limit_requests_total",
				Help: "Total number of requests processed by rate limiter",
			},
			[]string{"client_ip", "path", "status"},
		)

		RateLimitBlockedTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_rate_limit_blocked_total",
				Help: "Total number of requests blocked by rate limiter",
			},
			[]string{"client_ip", "path"},
		)

		RateLimitCurrentBucket = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_rate_limit_bucket_tokens",
				Help: "Current number of tokens in rate limit bucket",
			},
			[]string{"client_ip"},
		)

		SIPRateLimitedTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_sip_rate_limited_total",
				Help: "Total number of SIP requests blocked by rate limiter",
			},
			[]string{"client_ip", "method"},
		)

		// Initialize SRTP metrics
		SRTPEncryptionErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_srtp_encryption_errors_total",
				Help: "Total number of SRTP encryption errors",
			},
			[]string{"call_uuid"},
		)

		SRTPDecryptionErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_srtp_decryption_errors_total",
				Help: "Total number of SRTP decryption errors",
			},
			[]string{"call_uuid", "error_type"},
		)

		SRTPPacketsProcessed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_srtp_packets_processed_total",
				Help: "Total number of SRTP packets processed",
			},
			[]string{"call_uuid", "direction"},
		)

		// Initialize audio processing metrics
		AudioProcessingLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_audio_processing_latency_seconds",
				Help:    "Latency of audio processing operations",
				Buckets: prometheus.ExponentialBuckets(0.0001, 2, 10),
			},
			[]string{"call_uuid", "processing_type"},
		)

		VADEvents = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_vad_events_total",
				Help: "Total number of Voice Activity Detection events",
			},
			[]string{"call_uuid", "event_type"},
		)

		NoiseReductionLevel = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_noise_reduction_level_db",
				Help: "Noise reduction level in dB",
			},
			[]string{"call_uuid"},
		)

		// Initialize resource metrics
		PortsInUse = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_ports_in_use",
				Help: "Number of RTP ports currently in use",
			},
		)

		MemoryBuffersActive = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_memory_buffers_active",
				Help: "Number of memory buffers currently active in pools",
			},
		)

		ActiveCalls = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_active_calls",
				Help: "Number of active calls",
			},
		)

		// Initialize STT metrics
		STTRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_stt_requests_total",
				Help: "Total number of STT requests",
			},
			[]string{"vendor", "status"},
		)

		STTLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_stt_latency_seconds",
				Help:    "Latency of STT requests",
				Buckets: prometheus.ExponentialBuckets(0.1, 2, 10), // 100ms to ~100s
			},
			[]string{"vendor"},
		)

		STTErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_stt_errors_total",
				Help: "Total number of STT errors",
			},
			[]string{"vendor", "error_type"},
		)

		STTBytesProcessed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_stt_bytes_processed_total",
				Help: "Total number of audio bytes processed by STT",
			},
			[]string{"vendor"},
		)

		STTWordsTranscribed = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_stt_words_transcribed_total",
				Help: "Total number of words transcribed by STT",
			},
			[]string{"vendor"},
		)

		// Initialize AMQP metrics
		AMQPPublishedMessages = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_amqp_published_messages_total",
				Help: "Total number of messages published to AMQP",
			},
			[]string{"queue", "status"},
		)

		AMQPConnectionErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_amqp_connection_errors_total",
				Help: "Total number of AMQP connection errors",
			},
			[]string{"error_type"},
		)

		AMQPReconnectAttempts = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_amqp_reconnect_attempts_total",
				Help: "Total number of AMQP reconnection attempts",
			},
			[]string{"status"},
		)

		AMQPConnectionStatus = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_amqp_connection_status",
				Help: "Status of AMQP connection (1 = connected, 0 = disconnected)",
			},
		)

		// Initialize provider health metrics
		ProviderHealthStatus = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_provider_health_status",
				Help: "Health status of STT providers (1 = healthy, 0 = unhealthy)",
			},
			[]string{"provider"},
		)

		ProviderHealthCheckTime = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_provider_health_check_seconds",
				Help:    "Time taken for provider health checks",
				Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to ~10s
			},
			[]string{"provider"},
		)

		ProviderSelectionScore = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_provider_selection_score",
				Help: "Selection score for STT providers (0-100)",
			},
			[]string{"provider"},
		)

		ProviderCircuitBreaker = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_provider_circuit_breaker_status",
				Help: "Circuit breaker status (0=closed, 1=open, 2=half-open)",
			},
			[]string{"provider"},
		)

		// Initialize Whisper-specific metrics
		WhisperCLIDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "siprec_whisper_cli_duration_seconds",
				Help:    "Duration of Whisper CLI execution including model loading",
				Buckets: prometheus.ExponentialBuckets(1, 2, 12), // 1s to ~1 hour (for large models)
			},
			[]string{"model", "status"},
		)

		WhisperTempFileDiskUsage = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_whisper_temp_file_bytes",
				Help: "Total disk space used by Whisper temporary files",
			},
		)

		WhisperTimeouts = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_whisper_timeouts_total",
				Help: "Total number of Whisper CLI timeouts",
			},
			[]string{"model"},
		)

		WhisperOutputFormatCounter = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_whisper_output_format_total",
				Help: "Total number of Whisper transcriptions by output format",
			},
			[]string{"format"},
		)

		// Initialize high-concurrency transcription metrics
		TranscriptionServicePublished = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_transcription_service_published_total",
				Help: "Total transcription events published by the transcription service",
			},
		)

		TranscriptionServiceDropped = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_transcription_service_dropped_total",
				Help: "Total transcription events dropped due to backpressure",
			},
		)

		TranscriptionServiceQueueLength = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_transcription_service_queue_length",
				Help: "Current length of the transcription service event queue",
			},
		)

		TranscriptionServiceHighWater = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_transcription_service_queue_high_water",
				Help: "High water mark of the transcription service event queue",
			},
		)

		LiveTranscriptionTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_live_transcription_total",
				Help: "Total live transcriptions processed",
			},
			[]string{"provider", "type"},
		)

		ConversationAccumulatorActive = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_conversation_accumulator_active",
				Help: "Number of active conversations being accumulated",
			},
		)

		ConversationAccumulatorTotal = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_conversation_accumulator_total",
				Help: "Total number of conversations processed",
			},
		)

		ConversationSegmentsTotal = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_conversation_segments_total",
				Help: "Total number of conversation segments accumulated",
			},
		)

		AMQPListenerPublished = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_amqp_listener_published_total",
				Help: "Total transcription messages published via AMQP listener",
			},
		)

		AMQPListenerFailed = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_amqp_listener_failed_total",
				Help: "Total AMQP listener publish failures",
			},
		)

		AMQPListenerDropped = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_amqp_listener_dropped_total",
				Help: "Total AMQP listener messages dropped due to backpressure",
			},
		)

		AMQPListenerTimeouts = prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "siprec_amqp_listener_timeouts_total",
				Help: "Total AMQP listener publish timeouts",
			},
		)

		AMQPListenerQueueLength = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_amqp_listener_queue_length",
				Help: "Current length of the AMQP listener publish queue",
			},
		)

		AMQPListenerHighWater = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "siprec_amqp_listener_queue_high_water",
				Help: "High water mark of the AMQP listener publish queue",
			},
		)

		// Initialize vendor-specific metrics
		VendorSessionsActive = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "siprec_vendor_sessions_active",
				Help: "Number of active sessions by vendor type",
			},
			[]string{"vendor_type"},
		)

		VendorSessionsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_vendor_sessions_total",
				Help: "Total number of sessions by vendor type",
			},
			[]string{"vendor_type"},
		)

		VendorMetadataExtractions = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_vendor_metadata_extractions_total",
				Help: "Total number of successful vendor metadata extractions",
			},
			[]string{"vendor_type", "field"},
		)

		VendorHeaderParseErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "siprec_vendor_header_parse_errors_total",
				Help: "Total number of vendor header parsing errors",
			},
			[]string{"vendor_type", "header"},
		)

		// Register all metrics
		registry.MustRegister(
			// RTP metrics
			RTPPacketsReceived,
			RTPBytesReceived,
			RTPPacketLatency,
			RTPDroppedPackets,
			RTPProcessingTime,

			// SIP metrics
			SIPRequestsTotal,
			SIPResponsesTotal,
			SIPSessionsActive,
			SIPSessionDuration,
			SIPSessionEstablishTime,

			// IP Access Control metrics
			SIPIPAccessBlocked,
			SIPIPAccessAllowed,
			SIPAuthFailures,

			// Rate limiting metrics
			RateLimitRequestsTotal,
			RateLimitBlockedTotal,
			RateLimitCurrentBucket,
			SIPRateLimitedTotal,

			// SRTP metrics
			SRTPEncryptionErrors,
			SRTPDecryptionErrors,
			SRTPPacketsProcessed,

			// Audio processing metrics
			AudioProcessingLatency,
			VADEvents,
			NoiseReductionLevel,

			// Resource metrics
			PortsInUse,
			MemoryBuffersActive,
			ActiveCalls,

			// STT metrics
			STTRequestsTotal,
			STTLatency,
			STTErrors,
			STTBytesProcessed,
			STTWordsTranscribed,

			// AMQP metrics
			AMQPPublishedMessages,
			AMQPConnectionErrors,
			AMQPReconnectAttempts,
			AMQPConnectionStatus,

			// Provider health metrics
			ProviderHealthStatus,
			ProviderHealthCheckTime,
			ProviderSelectionScore,
			ProviderCircuitBreaker,

			// Whisper-specific metrics
			WhisperCLIDuration,
			WhisperTempFileDiskUsage,
			WhisperTimeouts,
			WhisperOutputFormatCounter,

			// High-concurrency transcription metrics
			TranscriptionServicePublished,
			TranscriptionServiceDropped,
			TranscriptionServiceQueueLength,
			TranscriptionServiceHighWater,
			LiveTranscriptionTotal,
			ConversationAccumulatorActive,
			ConversationAccumulatorTotal,
			ConversationSegmentsTotal,
			AMQPListenerPublished,
			AMQPListenerFailed,
			AMQPListenerDropped,
			AMQPListenerTimeouts,
			AMQPListenerQueueLength,
			AMQPListenerHighWater,

			// Vendor-specific metrics
			VendorSessionsActive,
			VendorSessionsTotal,
			VendorMetadataExtractions,
			VendorHeaderParseErrors,
		)

		logger.Info("Prometheus metrics initialized")
	})
}

// GetRegistry returns the prometheus registry
func GetRegistry() *prometheus.Registry {
	return registry
}

// SetMetricsPath sets the HTTP path for metrics endpoint
func SetMetricsPath(path string) {
	defaultMetricsPath = path
}

// EnableMetrics enables or disables metrics collection
func EnableMetrics(enabled bool) {
	metricsEnabled = enabled
}

// IsMetricsEnabled returns whether metrics are enabled
func IsMetricsEnabled() bool {
	return metricsEnabled
}

// RegisterHandler registers the metrics HTTP handler
func RegisterHandler(mux *http.ServeMux) {
	if metricsEnabled {
		handler := promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{
				EnableOpenMetrics: true,
				Registry:          registry,
			},
		)
		mux.Handle(defaultMetricsPath, handler)
	}
}

// StartMetrics initializes the metrics service
func StartMetrics(logger *logrus.Logger, metricsEnabled bool) {
	if !metricsEnabled {
		EnableMetrics(false)
		logger.Info("Metrics collection is disabled")
		return
	}

	Init(logger)
	EnableMetrics(true)
	logger.WithField("metrics_path", defaultMetricsPath).Info("Metrics endpoint initialized")

	// Start a background goroutine to update resource metrics
	go updateResourceMetrics(logger)
}

// updateResourceMetrics updates resource usage metrics periodically
func updateResourceMetrics(logger *logrus.Logger) {
	// Update resource metrics every 5 seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// These would be implemented elsewhere and referenced here
			// Just placeholder examples to show the concept
			bufferCount := getBufferCount()
			portsCount := getPortsInUse()
			activeCallCount := getActiveCalls()

			MemoryBuffersActive.Set(float64(bufferCount))
			PortsInUse.Set(float64(portsCount))
			ActiveCalls.Set(float64(activeCallCount))
		}
	}
}

// Example helper functions for metrics updates
func getBufferCount() int {
	// This would be implemented to check buffer pool stats
	return 0
}

func getPortsInUse() int {
	// This would get the actual port usage from the port manager
	return 0
}

func getActiveCalls() int {
	// This would get the actual active call count
	return 0
}

// RecordSIPRequest records a SIP request
func RecordSIPRequest(method, status string) {
	if metricsEnabled {
		SIPRequestsTotal.WithLabelValues(method, status).Inc()
	}
}

// RecordSIPResponse records a SIP response
func RecordSIPResponse(statusCode int, statusClass string) {
	if metricsEnabled {
		SIPResponsesTotal.WithLabelValues(
			string(rune(statusCode)),
			statusClass,
		).Inc()
	}
}

// RecordRTPPacket records metrics for an RTP packet
func RecordRTPPacket(callUUID string, bytes int) {
	if metricsEnabled {
		RTPPacketsReceived.WithLabelValues(callUUID).Inc()
		RTPBytesReceived.WithLabelValues(callUUID).Add(float64(bytes))
	}
}

// RecordRTPLatency records the latency of RTP packet processing
func RecordRTPLatency(callUUID string, duration time.Duration) {
	if metricsEnabled {
		RTPPacketLatency.WithLabelValues(callUUID).Observe(duration.Seconds())
	}
}

// RecordRTPDroppedPackets records dropped RTP packets
func RecordRTPDroppedPackets(callUUID, reason string, count float64) {
	if metricsEnabled {
		RTPDroppedPackets.WithLabelValues(callUUID, reason).Add(count)
	}
}

// ObserveRTPProcessing records the time taken for RTP processing with a timer function
func ObserveRTPProcessing(callUUID, processType string) func() {
	if !metricsEnabled {
		return func() {}
	}

	start := time.Now()
	return func() {
		duration := time.Since(start)
		RTPProcessingTime.WithLabelValues(callUUID, processType).Observe(duration.Seconds())
	}
}

// RecordSTTRequest records metrics for an STT request
func RecordSTTRequest(vendor, status string) {
	if metricsEnabled {
		STTRequestsTotal.WithLabelValues(vendor, status).Inc()
	}
}

// ObserveSTTLatency records STT latency with a timer function
func ObserveSTTLatency(vendor string) func() {
	if !metricsEnabled {
		return func() {}
	}

	start := time.Now()
	return func() {
		duration := time.Since(start)
		STTLatency.WithLabelValues(vendor).Observe(duration.Seconds())
	}
}

// RecordAMQPPublish records metrics for an AMQP publish
func RecordAMQPPublish(queue, status string) {
	if metricsEnabled {
		AMQPPublishedMessages.WithLabelValues(queue, status).Inc()
	}
}

// SetAMQPConnectionStatus sets the AMQP connection status
func SetAMQPConnectionStatus(connected bool) {
	if metricsEnabled {
		if connected {
			AMQPConnectionStatus.Set(1)
		} else {
			AMQPConnectionStatus.Set(0)
		}
	}
}

// RecordSRTPEncryptionErrors records SRTP encryption errors
func RecordSRTPEncryptionErrors(callUUID, errorType string, count float64) {
	if metricsEnabled {
		SRTPEncryptionErrors.WithLabelValues(callUUID).Add(count)
	}
}

// RecordSRTPDecryptionErrors records SRTP decryption errors
func RecordSRTPDecryptionErrors(callUUID, errorType string, count float64) {
	if metricsEnabled {
		SRTPDecryptionErrors.WithLabelValues(callUUID, errorType).Add(count)
	}
}

// RecordSRTPPacketsProcessed records processed SRTP packets
func RecordSRTPPacketsProcessed(callUUID, direction string, count float64) {
	if metricsEnabled {
		SRTPPacketsProcessed.WithLabelValues(callUUID, direction).Add(count)
	}
}

// StartSessionTimer returns a function that records the session duration when called
func StartSessionTimer(sessionType string) func() {
	if !metricsEnabled || SIPSessionsActive == nil {
		return func() {}
	}

	SIPSessionsActive.Inc()
	start := time.Now()
	return func() {
		if SIPSessionsActive != nil {
			SIPSessionsActive.Dec()
		}
		if SIPSessionDuration != nil {
			duration := time.Since(start)
			SIPSessionDuration.WithLabelValues(sessionType).Observe(duration.Seconds())
		}
	}
}

// SetMetricsEnabled enables or disables metrics collection
func SetMetricsEnabled(enabled bool) {
	metricsEnabled = enabled
}

// RecordAudioProcessingError records audio processing errors
func RecordAudioProcessingError(callUUID, errorType string, count float64) {
	if metricsEnabled {
		// Use VADEvents counter for error tracking since we don't have a dedicated counter
		VADEvents.WithLabelValues(callUUID, "error_"+errorType).Add(count)
	}
}

// RecordProviderHealth records provider health status
func RecordProviderHealth(provider, status string, responseTimeMs int64) {
	if metricsEnabled {
		healthValue := 0.0
		if status == "healthy" {
			healthValue = 1.0
		}
		ProviderHealthStatus.WithLabelValues(provider).Set(healthValue)
		ProviderHealthCheckTime.WithLabelValues(provider).Observe(float64(responseTimeMs) / 1000.0)
	}
}

// SetProviderScore sets the provider selection score
func SetProviderScore(provider string, score float64) {
	if metricsEnabled {
		ProviderSelectionScore.WithLabelValues(provider).Set(score)
	}
}

// SetCircuitBreakerStatus sets the circuit breaker status
func SetCircuitBreakerStatus(provider, state string) {
	if metricsEnabled {
		value := 0.0
		switch state {
		case "open":
			value = 1.0
		case "half-open":
			value = 2.0
		}
		ProviderCircuitBreaker.WithLabelValues(provider).Set(value)
	}
}

// ObserveWhisperCLIDuration records Whisper CLI execution duration with a timer function
func ObserveWhisperCLIDuration(model string) func(status string) {
	if !metricsEnabled {
		return func(status string) {}
	}

	start := time.Now()
	return func(status string) {
		duration := time.Since(start)
		WhisperCLIDuration.WithLabelValues(model, status).Observe(duration.Seconds())
	}
}

// RecordWhisperTimeout records a Whisper CLI timeout
func RecordWhisperTimeout(model string) {
	if metricsEnabled {
		WhisperTimeouts.WithLabelValues(model).Inc()
	}
}

// RecordWhisperOutputFormat records the usage of a specific output format
func RecordWhisperOutputFormat(format string) {
	if metricsEnabled {
		WhisperOutputFormatCounter.WithLabelValues(format).Inc()
	}
}

// AddWhisperTempFileUsage increments the temp file disk usage (call on file creation)
func AddWhisperTempFileUsage(bytes int64) {
	if metricsEnabled {
		WhisperTempFileDiskUsage.Add(float64(bytes))
	}
}

// SubWhisperTempFileUsage decrements the temp file disk usage (call on file removal)
func SubWhisperTempFileUsage(bytes int64) {
	if metricsEnabled {
		WhisperTempFileDiskUsage.Sub(float64(bytes))
	}
}

// RecordIPAccessBlocked records a blocked IP access attempt
func RecordIPAccessBlocked(sourceIP, reason string) {
	if metricsEnabled {
		SIPIPAccessBlocked.WithLabelValues(sourceIP, reason).Inc()
	}
}

// RecordIPAccessAllowed records an allowed IP access
func RecordIPAccessAllowed(sourceIP, matchType string) {
	if metricsEnabled {
		SIPIPAccessAllowed.WithLabelValues(sourceIP, matchType).Inc()
	}
}

// RecordSIPAuthFailure records a SIP authentication failure
func RecordSIPAuthFailure(sourceIP, reason string) {
	if metricsEnabled {
		SIPAuthFailures.WithLabelValues(sourceIP, reason).Inc()
	}
}

// RecordRateLimitRequest records a rate-limited request (allowed or blocked)
func RecordRateLimitRequest(clientIP, path, status string) {
	if metricsEnabled {
		RateLimitRequestsTotal.WithLabelValues(clientIP, path, status).Inc()
	}
}

// RecordRateLimitBlocked records a blocked request due to rate limiting
func RecordRateLimitBlocked(clientIP, path string) {
	if metricsEnabled {
		RateLimitBlockedTotal.WithLabelValues(clientIP, path).Inc()
	}
}

// UpdateRateLimitBucket updates the current token count for a client
func UpdateRateLimitBucket(clientIP string, tokens float64) {
	if metricsEnabled {
		RateLimitCurrentBucket.WithLabelValues(clientIP).Set(tokens)
	}
}

// RecordSIPRateLimited records a SIP request blocked by rate limiting
func RecordSIPRateLimited(clientIP, method string) {
	if metricsEnabled {
		SIPRateLimitedTotal.WithLabelValues(clientIP, method).Inc()
	}
}

// UpdateTranscriptionServiceMetrics updates transcription service metrics
func UpdateTranscriptionServiceMetrics(published, dropped, highWater int64, queueLength int) {
	if metricsEnabled {
		TranscriptionServicePublished.Add(float64(published))
		TranscriptionServiceDropped.Add(float64(dropped))
		TranscriptionServiceHighWater.Set(float64(highWater))
		TranscriptionServiceQueueLength.Set(float64(queueLength))
	}
}

// RecordLiveTranscription records a live transcription event
func RecordLiveTranscription(provider string, isFinal bool) {
	if metricsEnabled {
		transcriptionType := "partial"
		if isFinal {
			transcriptionType = "final"
		}
		LiveTranscriptionTotal.WithLabelValues(provider, transcriptionType).Inc()
	}
}

// UpdateConversationAccumulatorMetrics updates conversation accumulator metrics
func UpdateConversationAccumulatorMetrics(active, total, segments int64) {
	if metricsEnabled {
		ConversationAccumulatorActive.Set(float64(active))
		ConversationAccumulatorTotal.Add(float64(total))
		ConversationSegmentsTotal.Add(float64(segments))
	}
}

// SetConversationAccumulatorActive sets the active conversations count
func SetConversationAccumulatorActive(active int64) {
	if metricsEnabled {
		ConversationAccumulatorActive.Set(float64(active))
	}
}

// UpdateAMQPListenerMetrics updates AMQP listener metrics
func UpdateAMQPListenerMetrics(published, failed, dropped, timeouts, highWater int64, queueLength int) {
	if metricsEnabled {
		AMQPListenerPublished.Add(float64(published))
		AMQPListenerFailed.Add(float64(failed))
		AMQPListenerDropped.Add(float64(dropped))
		AMQPListenerTimeouts.Add(float64(timeouts))
		AMQPListenerHighWater.Set(float64(highWater))
		AMQPListenerQueueLength.Set(float64(queueLength))
	}
}

// RecordAMQPListenerPublish increments the AMQP listener published counter
func RecordAMQPListenerPublish() {
	if metricsEnabled {
		AMQPListenerPublished.Inc()
	}
}

// RecordAMQPListenerFailure increments the AMQP listener failure counter
func RecordAMQPListenerFailure() {
	if metricsEnabled {
		AMQPListenerFailed.Inc()
	}
}

// RecordAMQPListenerDrop increments the AMQP listener dropped counter
func RecordAMQPListenerDrop() {
	if metricsEnabled {
		AMQPListenerDropped.Inc()
	}
}

// RecordAMQPListenerTimeout increments the AMQP listener timeout counter
func RecordAMQPListenerTimeout() {
	if metricsEnabled {
		AMQPListenerTimeouts.Inc()
	}
}
