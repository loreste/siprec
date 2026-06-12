package alerting

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func newTestEvaluator(t *testing.T) (*AlertEvaluator, *prometheus.Registry) {
	t.Helper()
	registry := prometheus.NewRegistry()
	evaluator := NewAlertEvaluatorWithGatherer(testLogger(), registry)
	return evaluator, registry
}

func TestEvaluateGaugeFromRegistry(t *testing.T) {
	evaluator, registry := newTestEvaluator(t)

	sessions := prometheus.NewGauge(prometheus.GaugeOpts{Name: "siprec_sip_sessions_active", Help: "test"})
	registry.MustRegister(sessions)
	sessions.Set(7)

	value, err := evaluator.EvaluateQuery("siprec_sip_sessions_active")
	if err != nil {
		t.Fatalf("EvaluateQuery returned error: %v", err)
	}
	if value != 7 {
		t.Errorf("value = %v, want 7", value)
	}
}

func TestEvaluateGaugeVecSumsAcrossLabels(t *testing.T) {
	evaluator, registry := newTestEvaluator(t)

	connections := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{Name: "siprec_database_connections", Help: "test"},
		[]string{"database", "status"},
	)
	registry.MustRegister(connections)
	connections.WithLabelValues("siprec", "open").Set(10)
	connections.WithLabelValues("siprec", "idle").Set(5)

	value, err := evaluator.EvaluateQuery("siprec_database_connections")
	if err != nil {
		t.Fatalf("EvaluateQuery returned error: %v", err)
	}
	if value != 15 {
		t.Errorf("value = %v, want 15 (sum across labels)", value)
	}
}

func TestEvaluateCounterRate(t *testing.T) {
	evaluator, registry := newTestEvaluator(t)

	failures := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "siprec_session_failures_total", Help: "test"},
		[]string{"transport", "failure_type"},
	)
	registry.MustRegister(failures)
	failures.WithLabelValues("udp", "timeout").Add(10)

	current := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return current }

	// First sample only seeds the window and must report unavailability.
	if _, err := evaluator.EvaluateQuery("rate(siprec_session_failures_total[5m])"); err == nil {
		t.Fatal("first rate evaluation should return an error, not a fake value")
	}

	failures.WithLabelValues("udp", "timeout").Add(20)
	current = current.Add(10 * time.Second)

	value, err := evaluator.EvaluateQuery("rate(siprec_session_failures_total[5m])")
	if err != nil {
		t.Fatalf("second rate evaluation returned error: %v", err)
	}
	if value != 2 { // 20 increase over 10 seconds
		t.Errorf("rate = %v, want 2", value)
	}
}

func TestEvaluateCounterRateHandlesReset(t *testing.T) {
	evaluator, registry := newTestEvaluator(t)

	errorsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "siprec_recording_errors_total", Help: "test"},
		[]string{"error_type", "format"},
	)
	registry.MustRegister(errorsTotal)
	errorsTotal.WithLabelValues("io", "wav").Add(50)

	current := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return current }

	if _, err := evaluator.EvaluateQuery("rate(siprec_recording_errors_total[5m])"); err == nil {
		t.Fatal("first rate evaluation should return an error")
	}

	// Simulate a counter reset by re-registering a fresh counter.
	registry.Unregister(errorsTotal)
	errorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "siprec_recording_errors_total", Help: "test"},
		[]string{"error_type", "format"},
	)
	registry.MustRegister(errorsTotal)
	errorsTotal.WithLabelValues("io", "wav").Add(5)
	current = current.Add(10 * time.Second)

	value, err := evaluator.EvaluateQuery("rate(siprec_recording_errors_total[5m])")
	if err != nil {
		t.Fatalf("rate after reset returned error: %v", err)
	}
	if value != 0.5 { // counter reset: treat current value 5 as the increase
		t.Errorf("rate = %v, want 0.5", value)
	}
}

func TestEvaluateMemoryUsageIsReal(t *testing.T) {
	evaluator, _ := newTestEvaluator(t)

	value, err := evaluator.EvaluateQuery("siprec_system_memory_usage_bytes")
	if err != nil {
		t.Fatalf("memory query returned error: %v", err)
	}
	if value <= 0 {
		t.Errorf("memory usage = %v, want > 0", value)
	}
	// The old implementation hardcoded exactly 500MB; a live reading of a
	// small test process should never match it.
	if value == 1024*1024*500 {
		t.Errorf("memory usage equals the old mock constant, looks hardcoded")
	}
}

func TestEvaluateGoroutines(t *testing.T) {
	evaluator, _ := newTestEvaluator(t)

	value, err := evaluator.EvaluateQuery("siprec_system_goroutines")
	if err != nil {
		t.Fatalf("goroutines query returned error: %v", err)
	}
	if value < 1 {
		t.Errorf("goroutines = %v, want >= 1", value)
	}
}

func TestEvaluateCPUUsage(t *testing.T) {
	evaluator, _ := newTestEvaluator(t)

	current := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return current }

	// First sample seeds the window and must report unavailability.
	if _, err := evaluator.EvaluateQuery("siprec_system_cpu_usage_percent"); err == nil {
		t.Fatal("first cpu evaluation should return an error, not a fake value")
	}

	current = current.Add(5 * time.Second)
	value, err := evaluator.EvaluateQuery("siprec_system_cpu_usage_percent")
	if err != nil {
		t.Fatalf("second cpu evaluation returned error: %v", err)
	}
	if value < 0 {
		t.Errorf("cpu usage = %v, want >= 0", value)
	}
	if value == 75.5 {
		t.Errorf("cpu usage equals the old mock constant, looks hardcoded")
	}
}

func TestEvaluateMissingMetricReturnsError(t *testing.T) {
	evaluator, _ := newTestEvaluator(t)

	if _, err := evaluator.EvaluateQuery("siprec_redis_cluster_nodes"); err == nil {
		t.Fatal("unregistered metric should return an error, not a fake value")
	}
}

func TestEvaluateUnknownQueryReturnsError(t *testing.T) {
	evaluator, _ := newTestEvaluator(t)

	if _, err := evaluator.EvaluateQuery("some_unknown_metric"); err == nil {
		t.Fatal("unknown query should return an error")
	}
}

func TestEvaluatorWithoutRegistryReturnsError(t *testing.T) {
	evaluator := NewAlertEvaluator(testLogger())
	// pkg/metrics may have been initialized by other tests; only assert the
	// nil-registry behavior when it has not been.
	if evaluator.gatherer == nil {
		if _, err := evaluator.gatherValue("siprec_metric_that_does_not_exist"); err == nil {
			t.Fatal("gatherValue should error when the metric is unavailable")
		}
	}
}
