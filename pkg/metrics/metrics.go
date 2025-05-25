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
