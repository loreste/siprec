package backup

import (
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestGCPCapacityScaler(t *testing.T) {
	tests := []struct {
		name     string
		backend  Backend
		expected float64
	}{
		{"down backend is drained", Backend{Status: "down", Weight: 80}, 0.0},
		{"maintenance backend is drained", Backend{Status: "maintenance", Weight: 80}, 0.0},
		{"unset weight means full capacity", Backend{Status: "up"}, 1.0},
		{"weight is a percentage", Backend{Status: "up", Weight: 25}, 0.25},
		{"weight 100 is full capacity", Backend{Status: "up", Weight: 100}, 1.0},
		{"weight over 100 is clamped", Backend{Status: "up", Weight: 250}, 1.0},
		{"negative weight means full capacity", Backend{Status: "up", Weight: -1}, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gcpCapacityScaler(tt.backend); got != tt.expected {
				t.Errorf("gcpCapacityScaler(%+v) = %v, want %v", tt.backend, got, tt.expected)
			}
		})
	}
}

func TestLastURLSegment(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{
			"https://www.googleapis.com/compute/v1/projects/p/zones/us-central1-a/instanceGroups/siprec-primary",
			"siprec-primary",
		},
		{
			"projects/p/regions/us-central1/networkEndpointGroups/siprec-neg/",
			"siprec-neg",
		},
		{"plain-name", "plain-name"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := lastURLSegment(tt.url); got != tt.expected {
			t.Errorf("lastURLSegment(%q) = %q, want %q", tt.url, got, tt.expected)
		}
	}
}

func TestValidateGCPLBConfig(t *testing.T) {
	logger := logrus.New()

	lbm := NewLoadBalancerManager(LoadBalancerConfig{Type: "gcp_lb"}, logger)
	if err := lbm.validateGCPLBConfig(); err == nil {
		t.Error("expected error when GCPProject is missing")
	}

	lbm = NewLoadBalancerManager(LoadBalancerConfig{Type: "gcp_lb", GCPProject: "my-project"}, logger)
	if err := lbm.validateGCPLBConfig(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGCPBackendServiceName(t *testing.T) {
	logger := logrus.New()

	lbm := NewLoadBalancerManager(LoadBalancerConfig{Type: "gcp_lb", GCPProject: "p"}, logger)
	if got := lbm.gcpBackendServiceName("siprec-svc"); got != "siprec-svc" {
		t.Errorf("expected fallback to call-time service name, got %q", got)
	}

	lbm = NewLoadBalancerManager(LoadBalancerConfig{
		Type:              "gcp_lb",
		GCPProject:        "p",
		GCPBackendService: "configured-bes",
	}, logger)
	if got := lbm.gcpBackendServiceName("siprec-svc"); got != "configured-bes" {
		t.Errorf("expected configured backend service name, got %q", got)
	}
}

func TestUpdateGCPLBBackendsRequiresProject(t *testing.T) {
	logger := logrus.New()
	lbm := NewLoadBalancerManager(LoadBalancerConfig{Type: "gcp_lb"}, logger)

	err := lbm.UpdateBackends(testContext(t), "svc", []Backend{{Name: "b1"}})
	if err == nil || !strings.Contains(err.Error(), "GCP project ID is required") {
		t.Errorf("expected project validation error, got: %v", err)
	}

	err = lbm.FailoverToBackend(testContext(t), "svc", "b1")
	if err == nil || !strings.Contains(err.Error(), "GCP project ID is required") {
		t.Errorf("expected project validation error, got: %v", err)
	}

	_, err = lbm.GetBackendStatus(testContext(t), "svc")
	if err == nil || !strings.Contains(err.Error(), "GCP project ID is required") {
		t.Errorf("expected project validation error, got: %v", err)
	}
}
