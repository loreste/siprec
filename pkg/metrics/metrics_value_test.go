package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestGatherMetricValueGauge(t *testing.T) {
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_gauge", Help: "test"})
	registry.MustRegister(gauge)
	gauge.Set(42.5)

	value, err := GatherMetricValue(registry, "test_gauge")
	if err != nil {
		t.Fatalf("GatherMetricValue returned error: %v", err)
	}
	if value != 42.5 {
		t.Errorf("value = %v, want 42.5", value)
	}
}

func TestGatherMetricValueCounterVecSums(t *testing.T) {
	registry := prometheus.NewRegistry()
	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "test_counter_total", Help: "test"},
		[]string{"label"},
	)
	registry.MustRegister(counter)
	counter.WithLabelValues("a").Add(3)
	counter.WithLabelValues("b").Add(4)

	value, err := GatherMetricValue(registry, "test_counter_total")
	if err != nil {
		t.Fatalf("GatherMetricValue returned error: %v", err)
	}
	if value != 7 {
		t.Errorf("value = %v, want 7 (sum across label values)", value)
	}
}

func TestGatherMetricValueMissingMetric(t *testing.T) {
	registry := prometheus.NewRegistry()

	if _, err := GatherMetricValue(registry, "does_not_exist"); err == nil {
		t.Fatal("expected error for missing metric")
	}
}

func TestGatherMetricValueUnsupportedType(t *testing.T) {
	registry := prometheus.NewRegistry()
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "test_histogram", Help: "test"})
	registry.MustRegister(histogram)
	histogram.Observe(1)

	if _, err := GatherMetricValue(registry, "test_histogram"); err == nil {
		t.Fatal("expected error for unsupported metric type")
	}
}

func TestGatherMetricValueNilGatherer(t *testing.T) {
	if _, err := GatherMetricValue(nil, "anything"); err == nil {
		t.Fatal("expected error for nil gatherer")
	}
}
