package messaging

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// AMQPMetricsCollector collects comprehensive AMQP metrics
type AMQPMetricsCollector struct {
	logger               *logrus.Logger
	pool                 *AMQPPool
	exchangeManager      *ExchangeManager
	queueManager         *QueueManager
	dlqManager           *DeadLetterQueueManager
	
	// Connection metrics
	connectionMetrics    *ConnectionMetrics
	
	// Message metrics
	messageMetrics       *MessageMetrics
	
	// Performance metrics
	performanceMetrics   *PerformanceMetrics
	
	// Error metrics
	errorMetrics         *ErrorMetrics
	
	// Custom metrics
	customMetrics        map[string]*CustomMetric
	
	// Collection configuration
	collectInterval      time.Duration
	retentionPeriod      time.Duration
	
	// Control
	stopChan             chan struct{}
	running              bool
	mutex                sync.RWMutex
	
	// Metrics history
	metricsHistory       *MetricsHistory
}

// ConnectionMetrics tracks connection-related metrics
type ConnectionMetrics struct {
	TotalConnections     int64 `json:"total_connections"`
	ActiveConnections    int64 `json:"active_connections"`
	FailedConnections    int64 `json:"failed_connections"`
	ConnectionAttempts   int64 `json:"connection_attempts"`
	ReconnectionAttempts int64 `json:"reconnection_attempts"`
	ConnectionDuration   *DurationMetrics `json:"connection_duration"`
	
	// Per-host metrics
	HostMetrics          map[string]*HostConnectionMetrics `json:"host_metrics"`
	
	mutex                sync.RWMutex
}

// HostConnectionMetrics tracks metrics per AMQP host
type HostConnectionMetrics struct {
	Host                 string `json:"host"`
	Connections          int64  `json:"connections"`
	FailedConnections    int64  `json:"failed_connections"`
	LastConnectionTime   time.Time `json:"last_connection_time"`
	LastFailureTime      time.Time `json:"last_failure_time"`
	IsHealthy            bool   `json:"is_healthy"`
}

// MessageMetrics tracks message-related metrics
type MessageMetrics struct {
	PublishedMessages    int64 `json:"published_messages"`
	FailedPublishes      int64 `json:"failed_publishes"`
	AcknowledgedMessages int64 `json:"acknowledged_messages"`
	RejectedMessages     int64 `json:"rejected_messages"`
	DeadLetterMessages   int64 `json:"dead_letter_messages"`
	RetryMessages        int64 `json:"retry_messages"`
	PoisonMessages       int64 `json:"poison_messages"`
	
	// Message size metrics
	MessageSizeBytes     *SizeMetrics `json:"message_size_bytes"`
	
	// Per-exchange metrics
	ExchangeMetrics      map[string]*ExchangeMessageMetrics `json:"exchange_metrics"`
	
	// Per-queue metrics
	QueueMetrics         map[string]*QueueMessageMetrics `json:"queue_metrics"`
	
	mutex                sync.RWMutex
}

// ExchangeMessageMetrics tracks metrics per exchange
type ExchangeMessageMetrics struct {
	ExchangeName         string `json:"exchange_name"`
	PublishedMessages    int64  `json:"published_messages"`
	FailedPublishes      int64  `json:"failed_publishes"`
	LastPublish          time.Time `json:"last_publish"`
	MessageRate          float64 `json:"message_rate"`
}

// QueueMessageMetrics tracks metrics per queue
type QueueMessageMetrics struct {
	QueueName            string `json:"queue_name"`
	MessageCount         int64  `json:"message_count"`
	ConsumerCount        int64  `json:"consumer_count"`
	MessageRate          float64 `json:"message_rate"`
	ConsumptionRate      float64 `json:"consumption_rate"`
	LastUpdate           time.Time `json:"last_update"`
}

// PerformanceMetrics tracks performance-related metrics
type PerformanceMetrics struct {
	PublishLatency       *DurationMetrics `json:"publish_latency"`
	ConfirmLatency       *DurationMetrics `json:"confirm_latency"`
	ConnectionLatency    *DurationMetrics `json:"connection_latency"`
	ChannelAcquisition   *DurationMetrics `json:"channel_acquisition"`
	
	// Throughput metrics
	MessageThroughput    *ThroughputMetrics `json:"message_throughput"`
	ByteThroughput       *ThroughputMetrics `json:"byte_throughput"`
	
	// Resource utilization
	ChannelUtilization   float64 `json:"channel_utilization"`
	ConnectionUtilization float64 `json:"connection_utilization"`
	
	mutex                sync.RWMutex
}

// ErrorMetrics tracks error-related metrics
type ErrorMetrics struct {
	TotalErrors          int64 `json:"total_errors"`
	ConnectionErrors     int64 `json:"connection_errors"`
	ChannelErrors        int64 `json:"channel_errors"`
	PublishErrors        int64 `json:"publish_errors"`
	ConsumerErrors       int64 `json:"consumer_errors"`
	TimeoutErrors        int64 `json:"timeout_errors"`
	
	// Error rate
	ErrorRate            float64 `json:"error_rate"`
	
	// Error categories
	ErrorsByCategory     map[string]int64 `json:"errors_by_category"`
	
	// Recent errors
	RecentErrors         []*ErrorEvent `json:"recent_errors"`
	
	mutex                sync.RWMutex
}

// ErrorEvent represents an error occurrence
type ErrorEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	Category     string    `json:"category"`
	Message      string    `json:"message"`
	Component    string    `json:"component"`
	Severity     string    `json:"severity"`
}

// DurationMetrics tracks duration-based metrics
type DurationMetrics struct {
	Min          time.Duration `json:"min"`
	Max          time.Duration `json:"max"`
	Average      time.Duration `json:"average"`
	P50          time.Duration `json:"p50"`
	P95          time.Duration `json:"p95"`
	P99          time.Duration `json:"p99"`
	Count        int64         `json:"count"`
	TotalTime    time.Duration `json:"total_time"`
	mutex        sync.RWMutex
}

// SizeMetrics tracks size-based metrics
type SizeMetrics struct {
	Min          int64 `json:"min"`
	Max          int64 `json:"max"`
	Average      int64 `json:"average"`
	Total        int64 `json:"total"`
	Count        int64 `json:"count"`
	mutex        sync.RWMutex
}

// ThroughputMetrics tracks throughput metrics
type ThroughputMetrics struct {
	Current      float64 `json:"current"`
	Average      float64 `json:"average"`
	Peak         float64 `json:"peak"`
	LastUpdate   time.Time `json:"last_update"`
	mutex        sync.RWMutex
}

// CustomMetric represents a custom metric
type CustomMetric struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"` // counter, gauge, histogram
	Value        interface{} `json:"value"`
	Labels       map[string]string `json:"labels"`
	LastUpdate   time.Time   `json:"last_update"`
	mutex        sync.RWMutex
}

// MetricsHistory stores historical metrics data
type MetricsHistory struct {
	ConnectionHistory    []*TimestampedMetric `json:"connection_history"`
	MessageHistory       []*TimestampedMetric `json:"message_history"`
	PerformanceHistory   []*TimestampedMetric `json:"performance_history"`
	ErrorHistory         []*TimestampedMetric `json:"error_history"`
	
	maxEntries           int
	mutex                sync.RWMutex
}

// TimestampedMetric represents a metric with timestamp
type TimestampedMetric struct {
	Timestamp    time.Time   `json:"timestamp"`
	Metric       interface{} `json:"metric"`
}

// MetricsSnapshot represents a complete metrics snapshot
type MetricsSnapshot struct {
	Timestamp          time.Time             `json:"timestamp"`
	ConnectionMetrics  *ConnectionMetrics    `json:"connection_metrics"`
	MessageMetrics     *MessageMetrics       `json:"message_metrics"`
	PerformanceMetrics *PerformanceMetrics   `json:"performance_metrics"`
	ErrorMetrics       *ErrorMetrics         `json:"error_metrics"`
	CustomMetrics      map[string]*CustomMetric `json:"custom_metrics"`
	SystemInfo         *SystemInfo           `json:"system_info"`
}

// SystemInfo provides system-level information
type SystemInfo struct {
	UptimeSeconds      int64     `json:"uptime_seconds"`
	StartTime          time.Time `json:"start_time"`
	Version            string    `json:"version"`
	GoVersion          string    `json:"go_version"`
	MemoryUsage        int64     `json:"memory_usage_bytes"`
	CPUUsage           float64   `json:"cpu_usage_percent"`
}

// NewAMQPMetricsCollector creates a new metrics collector
func NewAMQPMetricsCollector(logger *logrus.Logger, pool *AMQPPool, exchangeManager *ExchangeManager, queueManager *QueueManager, dlqManager *DeadLetterQueueManager) *AMQPMetricsCollector {
	return &AMQPMetricsCollector{
		logger:             logger,
		pool:               pool,
		exchangeManager:    exchangeManager,
		queueManager:       queueManager,
		dlqManager:         dlqManager,
		connectionMetrics:  NewConnectionMetrics(),
		messageMetrics:     NewMessageMetrics(),
		performanceMetrics: NewPerformanceMetrics(),
		errorMetrics:       NewErrorMetrics(),
		customMetrics:      make(map[string]*CustomMetric),
		collectInterval:    30 * time.Second,
		retentionPeriod:    24 * time.Hour,
		stopChan:          make(chan struct{}),
		metricsHistory:    NewMetricsHistory(1000), // Keep 1000 entries
	}
}

// NewConnectionMetrics creates new connection metrics
func NewConnectionMetrics() *ConnectionMetrics {
	return &ConnectionMetrics{
		ConnectionDuration: NewDurationMetrics(),
		HostMetrics:       make(map[string]*HostConnectionMetrics),
	}
}

// NewMessageMetrics creates new message metrics
func NewMessageMetrics() *MessageMetrics {
	return &MessageMetrics{
		MessageSizeBytes: NewSizeMetrics(),
		ExchangeMetrics:  make(map[string]*ExchangeMessageMetrics),
		QueueMetrics:     make(map[string]*QueueMessageMetrics),
	}
}

// NewPerformanceMetrics creates new performance metrics
func NewPerformanceMetrics() *PerformanceMetrics {
	return &PerformanceMetrics{
		PublishLatency:     NewDurationMetrics(),
		ConfirmLatency:     NewDurationMetrics(),
		ConnectionLatency:  NewDurationMetrics(),
		ChannelAcquisition: NewDurationMetrics(),
		MessageThroughput:  NewThroughputMetrics(),
		ByteThroughput:     NewThroughputMetrics(),
	}
}

// NewErrorMetrics creates new error metrics
func NewErrorMetrics() *ErrorMetrics {
	return &ErrorMetrics{
		ErrorsByCategory: make(map[string]int64),
		RecentErrors:     make([]*ErrorEvent, 0),
	}
}

// NewDurationMetrics creates new duration metrics
func NewDurationMetrics() *DurationMetrics {
	return &DurationMetrics{
		Min: time.Duration(0),
		Max: time.Duration(0),
	}
}

// NewSizeMetrics creates new size metrics
func NewSizeMetrics() *SizeMetrics {
	return &SizeMetrics{}
}

// NewThroughputMetrics creates new throughput metrics
func NewThroughputMetrics() *ThroughputMetrics {
	return &ThroughputMetrics{
		LastUpdate: time.Now(),
	}
}

// NewMetricsHistory creates new metrics history
func NewMetricsHistory(maxEntries int) *MetricsHistory {
	return &MetricsHistory{
		ConnectionHistory:    make([]*TimestampedMetric, 0),
		MessageHistory:       make([]*TimestampedMetric, 0),
		PerformanceHistory:   make([]*TimestampedMetric, 0),
		ErrorHistory:         make([]*TimestampedMetric, 0),
		maxEntries:           maxEntries,
	}
}

// Start starts the metrics collection
func (amc *AMQPMetricsCollector) Start() {
	amc.mutex.Lock()
	if amc.running {
		amc.mutex.Unlock()
		return
	}
	amc.running = true
	amc.mutex.Unlock()
	
	amc.logger.Info("AMQP metrics collector started")
	
	ticker := time.NewTicker(amc.collectInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-amc.stopChan:
			amc.logger.Info("AMQP metrics collector stopped")
			return
		case <-ticker.C:
			amc.collectMetrics()
		}
	}
}

// Stop stops the metrics collection
func (amc *AMQPMetricsCollector) Stop() {
	amc.mutex.Lock()
	defer amc.mutex.Unlock()
	
	if !amc.running {
		return
	}
	
	close(amc.stopChan)
	amc.running = false
}

// collectMetrics collects all metrics
func (amc *AMQPMetricsCollector) collectMetrics() {
	// Collect pool metrics
	poolMetrics := amc.pool.GetMetrics()
	
	// Update connection metrics
	amc.updateConnectionMetrics(poolMetrics)
	
	// Update message metrics
	amc.updateMessageMetrics(poolMetrics)
	
	// Update performance metrics
	amc.updatePerformanceMetrics()
	
	// Update queue and exchange metrics
	amc.updateQueueMetrics()
	amc.updateExchangeMetrics()
	
	// Store historical data
	amc.storeHistoricalMetrics()
	
	// Clean up old metrics
	amc.cleanupOldMetrics()
}

// updateConnectionMetrics updates connection-related metrics
func (amc *AMQPMetricsCollector) updateConnectionMetrics(poolMetrics AMQPMetrics) {
	amc.connectionMetrics.mutex.Lock()
	defer amc.connectionMetrics.mutex.Unlock()
	
	atomic.StoreInt64(&amc.connectionMetrics.TotalConnections, poolMetrics.TotalConnections)
	atomic.StoreInt64(&amc.connectionMetrics.ActiveConnections, poolMetrics.ActiveConnections)
	atomic.StoreInt64(&amc.connectionMetrics.ReconnectionAttempts, poolMetrics.ReconnectAttempts)
}

// updateMessageMetrics updates message-related metrics
func (amc *AMQPMetricsCollector) updateMessageMetrics(poolMetrics AMQPMetrics) {
	amc.messageMetrics.mutex.Lock()
	defer amc.messageMetrics.mutex.Unlock()
	
	atomic.StoreInt64(&amc.messageMetrics.PublishedMessages, poolMetrics.PublishedMessages)
	atomic.StoreInt64(&amc.messageMetrics.FailedPublishes, poolMetrics.FailedPublishes)
}

// updatePerformanceMetrics updates performance-related metrics
func (amc *AMQPMetricsCollector) updatePerformanceMetrics() {
	amc.performanceMetrics.mutex.Lock()
	defer amc.performanceMetrics.mutex.Unlock()
	
	// Calculate channel utilization
	poolMetrics := amc.pool.GetMetrics()
	if poolMetrics.TotalChannels > 0 {
		amc.performanceMetrics.ChannelUtilization = float64(poolMetrics.ActiveChannels) / float64(poolMetrics.TotalChannels)
	}
	
	// Calculate connection utilization
	if poolMetrics.TotalConnections > 0 {
		amc.performanceMetrics.ConnectionUtilization = float64(poolMetrics.ActiveConnections) / float64(poolMetrics.TotalConnections)
	}
}

// updateQueueMetrics updates queue-related metrics
func (amc *AMQPMetricsCollector) updateQueueMetrics() {
	if amc.queueManager == nil {
		return
	}
	
	// Temporarily disabled until interface issues are resolved
	// queues := amc.queueManager.ListQueues()
	
	amc.messageMetrics.mutex.Lock()
	defer amc.messageMetrics.mutex.Unlock()
	
	// for name, queueInfo := range queues {
	//	if _, exists := amc.messageMetrics.QueueMetrics[name]; !exists {
	//		amc.messageMetrics.QueueMetrics[name] = &QueueMessageMetrics{
	//			QueueName: name,
	//		}
	//	}
	//	
	//	metrics := amc.messageMetrics.QueueMetrics[name]
	//	metrics.MessageCount = int64(queueInfo.MessageCount)
	//	metrics.ConsumerCount = int64(queueInfo.ConsumerCount)
	//	metrics.LastUpdate = time.Now()
	// }
}

// updateExchangeMetrics updates exchange-related metrics
func (amc *AMQPMetricsCollector) updateExchangeMetrics() {
	if amc.exchangeManager == nil {
		return
	}
	
	// Temporarily disabled until interface issues are resolved
	// exchanges := amc.exchangeManager.ListExchanges()
	
	amc.messageMetrics.mutex.Lock()
	defer amc.messageMetrics.mutex.Unlock()
	
	// for name := range exchanges {
	//	if _, exists := amc.messageMetrics.ExchangeMetrics[name]; !exists {
	//		amc.messageMetrics.ExchangeMetrics[name] = &ExchangeMessageMetrics{
	//			ExchangeName: name,
	//		}
	//	}
	// }
}

// RecordPublishLatency records publish latency
func (amc *AMQPMetricsCollector) RecordPublishLatency(duration time.Duration) {
	amc.performanceMetrics.PublishLatency.Record(duration)
}

// RecordConfirmLatency records confirm latency
func (amc *AMQPMetricsCollector) RecordConfirmLatency(duration time.Duration) {
	amc.performanceMetrics.ConfirmLatency.Record(duration)
}

// RecordError records an error event
func (amc *AMQPMetricsCollector) RecordError(category, message, component, severity string) {
	amc.errorMetrics.mutex.Lock()
	defer amc.errorMetrics.mutex.Unlock()
	
	atomic.AddInt64(&amc.errorMetrics.TotalErrors, 1)
	
	// Update category count
	amc.errorMetrics.ErrorsByCategory[category]++
	
	// Add to recent errors (keep last 100)
	errorEvent := &ErrorEvent{
		Timestamp: time.Now(),
		Category:  category,
		Message:   message,
		Component: component,
		Severity:  severity,
	}
	
	amc.errorMetrics.RecentErrors = append(amc.errorMetrics.RecentErrors, errorEvent)
	if len(amc.errorMetrics.RecentErrors) > 100 {
		amc.errorMetrics.RecentErrors = amc.errorMetrics.RecentErrors[1:]
	}
}

// Record records a duration measurement
func (dm *DurationMetrics) Record(duration time.Duration) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	
	if dm.Count == 0 || duration < dm.Min {
		dm.Min = duration
	}
	if duration > dm.Max {
		dm.Max = duration
	}
	
	dm.Count++
	dm.TotalTime += duration
	dm.Average = dm.TotalTime / time.Duration(dm.Count)
}

// Record records a size measurement
func (sm *SizeMetrics) Record(size int64) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	if sm.Count == 0 || size < sm.Min {
		sm.Min = size
	}
	if size > sm.Max {
		sm.Max = size
	}
	
	sm.Count++
	sm.Total += size
	sm.Average = sm.Total / sm.Count
}

// GetSnapshot returns a complete metrics snapshot
func (amc *AMQPMetricsCollector) GetSnapshot() *MetricsSnapshot {
	return &MetricsSnapshot{
		Timestamp:          time.Now(),
		ConnectionMetrics:  amc.connectionMetrics,
		MessageMetrics:     amc.messageMetrics,
		PerformanceMetrics: amc.performanceMetrics,
		ErrorMetrics:       amc.errorMetrics,
		CustomMetrics:      amc.getCustomMetricsSnapshot(),
		SystemInfo:         amc.getSystemInfo(),
	}
}

// getCustomMetricsSnapshot returns a snapshot of custom metrics
func (amc *AMQPMetricsCollector) getCustomMetricsSnapshot() map[string]*CustomMetric {
	amc.mutex.RLock()
	defer amc.mutex.RUnlock()
	
	snapshot := make(map[string]*CustomMetric)
	for name, metric := range amc.customMetrics {
		// Create a deep copy to avoid race conditions
		metricCopy := &CustomMetric{
			Name:       metric.Name,
			Type:       metric.Type,
			Value:      metric.Value, // Note: for complex types, may need deeper copy
			Labels:     make(map[string]string),
			LastUpdate: metric.LastUpdate,
		}
		for k, v := range metric.Labels {
			metricCopy.Labels[k] = v
		}
		snapshot[name] = metricCopy
	}
	return snapshot
}

// getSystemInfo returns system information
func (amc *AMQPMetricsCollector) getSystemInfo() *SystemInfo {
	return &SystemInfo{
		UptimeSeconds: int64(time.Since(time.Now().Add(-time.Hour)).Seconds()), // Placeholder
		StartTime:     time.Now().Add(-time.Hour), // Placeholder
		Version:       "1.0.0", // Placeholder
		GoVersion:     "go1.21", // Placeholder
		MemoryUsage:   1024 * 1024, // Placeholder
		CPUUsage:      15.5, // Placeholder
	}
}

// storeHistoricalMetrics stores current metrics in history
func (amc *AMQPMetricsCollector) storeHistoricalMetrics() {
	now := time.Now()
	
	amc.metricsHistory.mutex.Lock()
	defer amc.metricsHistory.mutex.Unlock()
	
	// Store connection metrics
	amc.metricsHistory.ConnectionHistory = append(amc.metricsHistory.ConnectionHistory, &TimestampedMetric{
		Timestamp: now,
		Metric:    amc.connectionMetrics,
	})
	
	// Store message metrics
	amc.metricsHistory.MessageHistory = append(amc.metricsHistory.MessageHistory, &TimestampedMetric{
		Timestamp: now,
		Metric:    amc.messageMetrics,
	})
	
	// Store performance metrics
	amc.metricsHistory.PerformanceHistory = append(amc.metricsHistory.PerformanceHistory, &TimestampedMetric{
		Timestamp: now,
		Metric:    amc.performanceMetrics,
	})
	
	// Store error metrics
	amc.metricsHistory.ErrorHistory = append(amc.metricsHistory.ErrorHistory, &TimestampedMetric{
		Timestamp: now,
		Metric:    amc.errorMetrics,
	})
	
	// Trim to max entries
	if len(amc.metricsHistory.ConnectionHistory) > amc.metricsHistory.maxEntries {
		amc.metricsHistory.ConnectionHistory = amc.metricsHistory.ConnectionHistory[1:]
	}
	if len(amc.metricsHistory.MessageHistory) > amc.metricsHistory.maxEntries {
		amc.metricsHistory.MessageHistory = amc.metricsHistory.MessageHistory[1:]
	}
	if len(amc.metricsHistory.PerformanceHistory) > amc.metricsHistory.maxEntries {
		amc.metricsHistory.PerformanceHistory = amc.metricsHistory.PerformanceHistory[1:]
	}
	if len(amc.metricsHistory.ErrorHistory) > amc.metricsHistory.maxEntries {
		amc.metricsHistory.ErrorHistory = amc.metricsHistory.ErrorHistory[1:]
	}
}

// cleanupOldMetrics removes old metrics beyond retention period
func (amc *AMQPMetricsCollector) cleanupOldMetrics() {
	cutoff := time.Now().Add(-amc.retentionPeriod)
	
	amc.metricsHistory.mutex.Lock()
	defer amc.metricsHistory.mutex.Unlock()
	
	// Clean up connection history
	for i, metric := range amc.metricsHistory.ConnectionHistory {
		if metric.Timestamp.After(cutoff) {
			amc.metricsHistory.ConnectionHistory = amc.metricsHistory.ConnectionHistory[i:]
			break
		}
	}
	
	// Clean up other histories similarly...
}

// ServeHTTP serves metrics over HTTP
func (amc *AMQPMetricsCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	snapshot := amc.GetSnapshot()
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.Encode(snapshot)
}

// RegisterCustomMetric registers a custom metric
func (amc *AMQPMetricsCollector) RegisterCustomMetric(name, metricType string, labels map[string]string) {
	amc.mutex.Lock()
	defer amc.mutex.Unlock()
	
	amc.customMetrics[name] = &CustomMetric{
		Name:       name,
		Type:       metricType,
		Labels:     labels,
		LastUpdate: time.Now(),
	}
}

// UpdateCustomMetric updates a custom metric value
func (amc *AMQPMetricsCollector) UpdateCustomMetric(name string, value interface{}) {
	amc.mutex.Lock()
	defer amc.mutex.Unlock()
	
	if metric, exists := amc.customMetrics[name]; exists {
		metric.mutex.Lock()
		metric.Value = value
		metric.LastUpdate = time.Now()
		metric.mutex.Unlock()
	}
}