package metrics

import (
	"runtime"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

var (
	// System metrics
	SystemMemoryUsage     prometheus.Gauge
	SystemCPUUsage        prometheus.Gauge
	SystemDiskUsage       *prometheus.GaugeVec
	SystemNetworkBytes    *prometheus.CounterVec
	SystemGoroutines      prometheus.Gauge
	SystemFileDescriptors prometheus.Gauge

	// Database metrics
	DatabaseConnections      *prometheus.GaugeVec
	DatabaseQueryDuration    *prometheus.HistogramVec
	DatabaseQueryErrors      *prometheus.CounterVec
	DatabaseTransactions     *prometheus.CounterVec
	DatabaseConnectionErrors *prometheus.CounterVec

	// Redis cluster metrics
	RedisConnections  *prometheus.GaugeVec
	RedisOperations   *prometheus.CounterVec
	RedisLatency      *prometheus.HistogramVec
	RedisErrors       *prometheus.CounterVec
	RedisClusterNodes prometheus.Gauge
	RedisFailovers    *prometheus.CounterVec

	// Session metrics
	SessionsCreated      *prometheus.CounterVec
	SessionsTerminated   *prometheus.CounterVec
	SessionRecoveries    *prometheus.CounterVec
	SessionFailures      *prometheus.CounterVec
	SessionDurationTotal *prometheus.HistogramVec

	// Recording metrics
	RecordingsStarted     *prometheus.CounterVec
	RecordingsCompleted   *prometheus.CounterVec
	RecordingSize         *prometheus.HistogramVec
	RecordingDuration     *prometheus.HistogramVec
	RecordingErrors       *prometheus.CounterVec
	RecordingStorageUsage prometheus.Gauge

	// Transcription metrics
	TranscriptionRequests *prometheus.CounterVec
	TranscriptionLatency  *prometheus.HistogramVec
	TranscriptionErrors   *prometheus.CounterVec
	TranscriptionQuality  *prometheus.HistogramVec
	TranscriptionWords    *prometheus.CounterVec

	// Security metrics
	AuthenticationAttempts *prometheus.CounterVec
	AuthenticationFailures *prometheus.CounterVec
	APIKeyUsage            *prometheus.CounterVec
	RateLimitExceeded      *prometheus.CounterVec
	SecurityEvents         *prometheus.CounterVec

	// Business metrics
	CallVolumeByHour     *prometheus.GaugeVec
	PeakConcurrentCalls  prometheus.Gauge
	AverageCallDuration  *prometheus.GaugeVec
	CallQualityScore     *prometheus.HistogramVec
	CustomerSatisfaction *prometheus.GaugeVec

	// Alert metrics
	AlertsTriggered *prometheus.CounterVec
	AlertsResolved  *prometheus.CounterVec
	AlertDuration   *prometheus.HistogramVec

	// Metrics collector
	collector *EnhancedMetricsCollector
)

// EnhancedMetricsCollector collects enhanced system metrics
type EnhancedMetricsCollector struct {
	logger          *logrus.Logger
	collectInterval time.Duration
	enabled         bool
	mutex           sync.RWMutex
	stopChan        chan struct{}
}

// InitEnhancedMetrics initializes enhanced metrics
func InitEnhancedMetrics(logger *logrus.Logger) {
	initSystemMetrics()
	initDatabaseMetrics()
	initRedisMetrics()
	initSessionMetrics()
	initRecordingMetrics()
	initTranscriptionMetrics()
	initSecurityMetrics()
	initBusinessMetrics()
	initAlertMetrics()

	// Register all enhanced metrics
	registerEnhancedMetrics()

	// Initialize collector
	collector = &EnhancedMetricsCollector{
		logger:          logger,
		collectInterval: 10 * time.Second,
		enabled:         true,
		stopChan:        make(chan struct{}),
	}

	// Start collection
	go collector.start()

	logger.Info("Enhanced metrics initialized")
}

func initSystemMetrics() {
	SystemMemoryUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_system_memory_usage_bytes",
			Help: "Current memory usage in bytes",
		},
	)

	SystemCPUUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_system_cpu_usage_percent",
			Help: "Current CPU usage percentage",
		},
	)

	SystemDiskUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_system_disk_usage_bytes",
			Help: "Disk usage by mount point",
		},
		[]string{"mount_point", "type"},
	)

	SystemNetworkBytes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_system_network_bytes_total",
			Help: "Network bytes transmitted and received",
		},
		[]string{"interface", "direction"},
	)

	SystemGoroutines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_system_goroutines",
			Help: "Number of goroutines",
		},
	)

	SystemFileDescriptors = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_system_file_descriptors",
			Help: "Number of open file descriptors",
		},
	)
}

func initDatabaseMetrics() {
	DatabaseConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_database_connections",
			Help: "Number of database connections",
		},
		[]string{"database", "status"},
	)

	DatabaseQueryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_database_query_duration_seconds",
			Help:    "Database query duration",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"database", "operation", "table"},
	)

	DatabaseQueryErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_database_query_errors_total",
			Help: "Total database query errors",
		},
		[]string{"database", "operation", "error_type"},
	)

	DatabaseTransactions = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_database_transactions_total",
			Help: "Total database transactions",
		},
		[]string{"database", "status"},
	)

	DatabaseConnectionErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_database_connection_errors_total",
			Help: "Total database connection errors",
		},
		[]string{"database", "error_type"},
	)
}

func initRedisMetrics() {
	RedisConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_redis_connections",
			Help: "Number of Redis connections",
		},
		[]string{"instance", "status"},
	)

	RedisOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_redis_operations_total",
			Help: "Total Redis operations",
		},
		[]string{"instance", "operation", "status"},
	)

	RedisLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_redis_latency_seconds",
			Help:    "Redis operation latency",
			Buckets: prometheus.ExponentialBuckets(0.0001, 2, 12),
		},
		[]string{"instance", "operation"},
	)

	RedisErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_redis_errors_total",
			Help: "Total Redis errors",
		},
		[]string{"instance", "operation", "error_type"},
	)

	RedisClusterNodes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_redis_cluster_nodes",
			Help: "Number of Redis cluster nodes",
		},
	)

	RedisFailovers = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_redis_failovers_total",
			Help: "Total Redis failovers",
		},
		[]string{"instance", "reason"},
	)
}

func initSessionMetrics() {
	SessionsCreated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_sessions_created_total",
			Help: "Total sessions created",
		},
		[]string{"transport", "source"},
	)

	SessionsTerminated = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_sessions_terminated_total",
			Help: "Total sessions terminated",
		},
		[]string{"transport", "reason"},
	)

	SessionRecoveries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_session_recoveries_total",
			Help: "Total session recoveries",
		},
		[]string{"recovery_type", "status"},
	)

	SessionFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_session_failures_total",
			Help: "Total session failures",
		},
		[]string{"transport", "failure_type"},
	)

	SessionDurationTotal = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_session_duration_total_seconds",
			Help:    "Total session duration",
			Buckets: prometheus.ExponentialBuckets(1, 2, 16), // 1s to ~18 hours
		},
		[]string{"transport", "session_type"},
	)
}

func initRecordingMetrics() {
	RecordingsStarted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_recordings_started_total",
			Help: "Total recordings started",
		},
		[]string{"format", "quality"},
	)

	RecordingsCompleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_recordings_completed_total",
			Help: "Total recordings completed",
		},
		[]string{"format", "status"},
	)

	RecordingSize = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_recording_size_bytes",
			Help:    "Recording file size",
			Buckets: prometheus.ExponentialBuckets(1024, 2, 20), // 1KB to ~1GB
		},
		[]string{"format", "quality"},
	)

	RecordingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_recording_duration_seconds",
			Help:    "Recording duration",
			Buckets: prometheus.ExponentialBuckets(1, 2, 16), // 1s to ~18 hours
		},
		[]string{"format"},
	)

	RecordingErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_recording_errors_total",
			Help: "Total recording errors",
		},
		[]string{"error_type", "format"},
	)

	RecordingStorageUsage = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_recording_storage_usage_bytes",
			Help: "Total recording storage usage",
		},
	)
}

func initTranscriptionMetrics() {
	TranscriptionRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_transcription_requests_total",
			Help: "Total transcription requests",
		},
		[]string{"provider", "language", "status"},
	)

	TranscriptionLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_transcription_latency_seconds",
			Help:    "Transcription processing latency",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 12), // 100ms to ~7 minutes
		},
		[]string{"provider", "language"},
	)

	TranscriptionErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_transcription_errors_total",
			Help: "Total transcription errors",
		},
		[]string{"provider", "error_type"},
	)

	TranscriptionQuality = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_transcription_quality_score",
			Help:    "Transcription quality score (0-1)",
			Buckets: prometheus.LinearBuckets(0, 0.1, 11), // 0 to 1.0 in 0.1 increments
		},
		[]string{"provider", "language"},
	)

	TranscriptionWords = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_transcription_words_total",
			Help: "Total words transcribed",
		},
		[]string{"provider", "language"},
	)
}

func initSecurityMetrics() {
	AuthenticationAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_authentication_attempts_total",
			Help: "Total authentication attempts",
		},
		[]string{"type", "status"},
	)

	AuthenticationFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_authentication_failures_total",
			Help: "Total authentication failures",
		},
		[]string{"type", "reason"},
	)

	APIKeyUsage = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_api_key_usage_total",
			Help: "Total API key usage",
		},
		[]string{"key_id", "endpoint"},
	)

	RateLimitExceeded = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_rate_limit_exceeded_total",
			Help: "Total rate limit violations",
		},
		[]string{"endpoint", "client_ip"},
	)

	SecurityEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_security_events_total",
			Help: "Total security events",
		},
		[]string{"event_type", "severity"},
	)
}

func initBusinessMetrics() {
	CallVolumeByHour = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_call_volume_by_hour",
			Help: "Call volume by hour of day",
		},
		[]string{"hour", "day_of_week"},
	)

	PeakConcurrentCalls = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "siprec_peak_concurrent_calls",
			Help: "Peak concurrent calls in current period",
		},
	)

	AverageCallDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_average_call_duration_seconds",
			Help: "Average call duration",
		},
		[]string{"period", "call_type"},
	)

	CallQualityScore = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_call_quality_score",
			Help:    "Call quality score (0-1)",
			Buckets: prometheus.LinearBuckets(0, 0.1, 11),
		},
		[]string{"transport", "codec"},
	)

	CustomerSatisfaction = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "siprec_customer_satisfaction_score",
			Help: "Customer satisfaction score",
		},
		[]string{"service_type", "period"},
	)
}

func initAlertMetrics() {
	AlertsTriggered = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_alerts_triggered_total",
			Help: "Total alerts triggered",
		},
		[]string{"alert_name", "severity"},
	)

	AlertsResolved = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "siprec_alerts_resolved_total",
			Help: "Total alerts resolved",
		},
		[]string{"alert_name", "resolution_type"},
	)

	AlertDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "siprec_alert_duration_seconds",
			Help:    "Alert duration from trigger to resolution",
			Buckets: prometheus.ExponentialBuckets(1, 2, 16), // 1s to ~18 hours
		},
		[]string{"alert_name", "severity"},
	)
}

func registerEnhancedMetrics() {
	registry.MustRegister(
		// System metrics
		SystemMemoryUsage,
		SystemCPUUsage,
		SystemDiskUsage,
		SystemNetworkBytes,
		SystemGoroutines,
		SystemFileDescriptors,

		// Database metrics
		DatabaseConnections,
		DatabaseQueryDuration,
		DatabaseQueryErrors,
		DatabaseTransactions,
		DatabaseConnectionErrors,

		// Redis metrics
		RedisConnections,
		RedisOperations,
		RedisLatency,
		RedisErrors,
		RedisClusterNodes,
		RedisFailovers,

		// Session metrics
		SessionsCreated,
		SessionsTerminated,
		SessionRecoveries,
		SessionFailures,
		SessionDurationTotal,

		// Recording metrics
		RecordingsStarted,
		RecordingsCompleted,
		RecordingSize,
		RecordingDuration,
		RecordingErrors,
		RecordingStorageUsage,

		// Transcription metrics
		TranscriptionRequests,
		TranscriptionLatency,
		TranscriptionErrors,
		TranscriptionQuality,
		TranscriptionWords,

		// Security metrics
		AuthenticationAttempts,
		AuthenticationFailures,
		APIKeyUsage,
		RateLimitExceeded,
		SecurityEvents,

		// Business metrics
		CallVolumeByHour,
		PeakConcurrentCalls,
		AverageCallDuration,
		CallQualityScore,
		CustomerSatisfaction,

		// Alert metrics
		AlertsTriggered,
		AlertsResolved,
		AlertDuration,
	)
}

// EnhancedMetricsCollector methods

func (c *EnhancedMetricsCollector) start() {
	ticker := time.NewTicker(c.collectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.collectSystemMetrics()
		case <-c.stopChan:
			return
		}
	}
}

func (c *EnhancedMetricsCollector) collectSystemMetrics() {
	c.mutex.RLock()
	enabled := c.enabled
	c.mutex.RUnlock()

	if !enabled {
		return
	}

	// Collect Go runtime metrics
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	SystemMemoryUsage.Set(float64(m.Alloc))
	SystemGoroutines.Set(float64(runtime.NumGoroutine()))

	// Additional system metrics would be collected here
	// For production, use libraries like gopsutil
}

func (c *EnhancedMetricsCollector) Stop() {
	close(c.stopChan)
}

// Metric recording functions

func RecordDatabaseQuery(database, operation, table string, duration time.Duration, err error) {
	if !metricsEnabled {
		return
	}

	DatabaseQueryDuration.WithLabelValues(database, operation, table).Observe(duration.Seconds())

	if err != nil {
		DatabaseQueryErrors.WithLabelValues(database, operation, "query_error").Inc()
	}
}

func RecordRedisOperation(instance, operation string, duration time.Duration, err error) {
	if !metricsEnabled {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
		RedisErrors.WithLabelValues(instance, operation, "operation_error").Inc()
	}

	RedisOperations.WithLabelValues(instance, operation, status).Inc()
	RedisLatency.WithLabelValues(instance, operation).Observe(duration.Seconds())
}

func RecordSessionCreated(transport, source string) {
	if metricsEnabled {
		SessionsCreated.WithLabelValues(transport, source).Inc()
	}
}

func RecordSessionTerminated(transport, reason string) {
	if metricsEnabled {
		SessionsTerminated.WithLabelValues(transport, reason).Inc()
	}
}

func RecordRecordingStarted(format, quality string) {
	if metricsEnabled {
		RecordingsStarted.WithLabelValues(format, quality).Inc()
	}
}

func RecordRecordingCompleted(format, status string, size int64, duration time.Duration) {
	if !metricsEnabled {
		return
	}

	RecordingsCompleted.WithLabelValues(format, status).Inc()
	RecordingSize.WithLabelValues(format, "standard").Observe(float64(size))
	RecordingDuration.WithLabelValues(format).Observe(duration.Seconds())
}

func RecordAuthenticationAttempt(authType, status string) {
	if metricsEnabled {
		AuthenticationAttempts.WithLabelValues(authType, status).Inc()
	}
}

func RecordSecurityEvent(eventType, severity string) {
	if metricsEnabled {
		SecurityEvents.WithLabelValues(eventType, severity).Inc()
	}
}

func RecordAlert(alertName, severity string) {
	if metricsEnabled {
		AlertsTriggered.WithLabelValues(alertName, severity).Inc()
	}
}

func RecordAlertResolution(alertName, resolutionType string, duration time.Duration) {
	if !metricsEnabled {
		return
	}

	AlertsResolved.WithLabelValues(alertName, resolutionType).Inc()
	AlertDuration.WithLabelValues(alertName, "resolved").Observe(duration.Seconds())
}

// Business metrics functions

func UpdateCallVolume(hour, dayOfWeek string, volume float64) {
	if metricsEnabled {
		CallVolumeByHour.WithLabelValues(hour, dayOfWeek).Set(volume)
	}
}

func UpdatePeakConcurrentCalls(count float64) {
	if metricsEnabled {
		PeakConcurrentCalls.Set(count)
	}
}

func RecordCallQuality(transport, codec string, score float64) {
	if metricsEnabled {
		CallQualityScore.WithLabelValues(transport, codec).Observe(score)
	}
}

// Health check for metrics system
func (c *EnhancedMetricsCollector) IsHealthy() bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.enabled
}

// Configuration
func (c *EnhancedMetricsCollector) SetCollectInterval(interval time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.collectInterval = interval
}

func (c *EnhancedMetricsCollector) Enable() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.enabled = true
}

func (c *EnhancedMetricsCollector) Disable() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.enabled = false
}

// GetMetricsCollector returns the metrics collector instance
func GetMetricsCollector() *EnhancedMetricsCollector {
	return collector
}
