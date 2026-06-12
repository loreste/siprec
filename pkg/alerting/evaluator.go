package alerting

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/metrics"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// counterSample holds a counter observation used for rate calculations
type counterSample struct {
	value      float64
	observedAt time.Time
}

// AlertEvaluator evaluates alert queries against metrics
type AlertEvaluator struct {
	logger   *logrus.Logger
	gatherer prometheus.Gatherer
	now      func() time.Time

	mutex          sync.Mutex
	counterSamples map[string]counterSample
	cpu            cpuSampler
}

// NewAlertEvaluator creates a new alert evaluator that reads metrics from the
// registry managed by pkg/metrics.
func NewAlertEvaluator(logger *logrus.Logger) *AlertEvaluator {
	return &AlertEvaluator{
		logger:         logger,
		now:            time.Now,
		counterSamples: make(map[string]counterSample),
	}
}

// NewAlertEvaluatorWithGatherer creates an alert evaluator that reads metrics
// from the provided gatherer instead of the global metrics registry.
func NewAlertEvaluatorWithGatherer(logger *logrus.Logger, gatherer prometheus.Gatherer) *AlertEvaluator {
	evaluator := NewAlertEvaluator(logger)
	evaluator.gatherer = gatherer
	return evaluator
}

// EvaluateQuery evaluates a query and returns the result value
func (e *AlertEvaluator) EvaluateQuery(query string) (float64, error) {
	value, err := e.parseSimpleQuery(query)
	if err != nil {
		return 0, fmt.Errorf("failed to evaluate query '%s': %w", query, err)
	}

	return value, nil
}

// parseSimpleQuery parses simple metric queries and resolves them against
// live metric sources. Unknown or unmeasurable metrics return an error so the
// alert manager treats them as unavailable rather than evaluating fake data.
func (e *AlertEvaluator) parseSimpleQuery(query string) (float64, error) {
	query = strings.TrimSpace(query)

	switch {
	case strings.Contains(query, "siprec_sip_sessions_active"):
		return e.gatherValue("siprec_sip_sessions_active")
	case strings.Contains(query, "siprec_system_memory_usage_bytes"):
		return e.getMemoryUsage()
	case strings.Contains(query, "siprec_system_cpu_usage_percent"):
		return e.getCPUUsage()
	case strings.Contains(query, "siprec_system_goroutines"):
		return e.getGoroutineCount()
	case strings.Contains(query, "rate(siprec_session_failures_total"):
		return e.counterRate("siprec_session_failures_total")
	case strings.Contains(query, "rate(siprec_recording_errors_total"):
		return e.counterRate("siprec_recording_errors_total")
	case strings.Contains(query, "siprec_recording_storage_usage_bytes"):
		return e.gatherValue("siprec_recording_storage_usage_bytes")
	case strings.Contains(query, "rate(siprec_authentication_failures_total"):
		return e.counterRate("siprec_authentication_failures_total")
	case strings.Contains(query, "rate(siprec_database_query_errors_total"):
		return e.counterRate("siprec_database_query_errors_total")
	case strings.Contains(query, "siprec_database_connections"):
		return e.gatherValue("siprec_database_connections")
	case strings.Contains(query, "siprec_redis_cluster_nodes"):
		return e.gatherValue("siprec_redis_cluster_nodes")
	default:
		return 0, fmt.Errorf("unsupported query")
	}
}

// gatherValue reads the current value of a counter or gauge from the metrics
// registry, summing across all label combinations.
func (e *AlertEvaluator) gatherValue(name string) (float64, error) {
	gatherer := e.gatherer
	if gatherer == nil {
		registry := metrics.GetRegistry()
		if registry == nil {
			return 0, fmt.Errorf("metric %q unavailable: metrics registry not initialized", name)
		}
		gatherer = registry
	}

	return metrics.GatherMetricValue(gatherer, name)
}

// counterRate computes the per-second rate of a counter between consecutive
// evaluations. The first observation only seeds the sample window, so it
// returns an error to signal that the rate is not yet measurable.
func (e *AlertEvaluator) counterRate(name string) (float64, error) {
	value, err := e.gatherValue(name)
	if err != nil {
		return 0, err
	}

	now := e.now()

	e.mutex.Lock()
	defer e.mutex.Unlock()

	previous, exists := e.counterSamples[name]
	e.counterSamples[name] = counterSample{value: value, observedAt: now}

	if !exists {
		return 0, fmt.Errorf("rate for %q unavailable: first sample collected, rate requires two samples", name)
	}

	elapsed := now.Sub(previous.observedAt).Seconds()
	if elapsed <= 0 {
		return 0, fmt.Errorf("rate for %q unavailable: no time elapsed since previous sample", name)
	}

	delta := value - previous.value
	if delta < 0 {
		// Counter reset (e.g. process restart of an aggregated source);
		// treat the current value as the increase since the reset.
		delta = value
	}

	return delta / elapsed, nil
}

// getMemoryUsage returns the process heap allocation in bytes, matching the
// value exported as siprec_system_memory_usage_bytes.
func (e *AlertEvaluator) getMemoryUsage() (float64, error) {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return float64(memStats.Alloc), nil
}

// getGoroutineCount returns the current number of goroutines.
func (e *AlertEvaluator) getGoroutineCount() (float64, error) {
	return float64(runtime.NumGoroutine()), nil
}

// getCPUUsage returns process CPU usage as a percentage of wall-clock time
// since the previous evaluation. The first observation seeds the sampler and
// returns an error because no interval is available yet.
func (e *AlertEvaluator) getCPUUsage() (float64, error) {
	return e.cpu.usagePercent(e.nowFunc())
}

// nowFunc returns the evaluator clock, defaulting to time.Now.
func (e *AlertEvaluator) nowFunc() func() time.Time {
	if e.now != nil {
		return e.now
	}
	return time.Now
}

// cpuSampler computes process CPU usage between consecutive samples.
type cpuSampler struct {
	mutex       sync.Mutex
	initialized bool
	lastCPUTime time.Duration
	lastSample  time.Time
}

// usagePercent returns the CPU usage percentage over the interval since the
// previous call. A single core fully busy reports 100; multiple busy cores
// can report more.
func (s *cpuSampler) usagePercent(now func() time.Time) (float64, error) {
	cpuTime, err := processCPUTime()
	if err != nil {
		return 0, err
	}

	current := now()

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.initialized {
		s.initialized = true
		s.lastCPUTime = cpuTime
		s.lastSample = current
		return 0, fmt.Errorf("cpu usage unavailable: first sample collected, usage requires two samples")
	}

	wall := current.Sub(s.lastSample)
	if wall <= 0 {
		return 0, fmt.Errorf("cpu usage unavailable: no time elapsed since previous sample")
	}

	used := cpuTime - s.lastCPUTime
	s.lastCPUTime = cpuTime
	s.lastSample = current

	percent := used.Seconds() / wall.Seconds() * 100
	if percent < 0 {
		percent = 0
	}

	return percent, nil
}
