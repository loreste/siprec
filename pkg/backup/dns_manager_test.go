package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

// testContext returns a context for tests.
func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func TestRoute53ZoneIDResolution(t *testing.T) {
	logger := logrus.New()

	t.Run("explicit field wins", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{
			Provider:            "route53",
			Zone:                "example.com",
			Route53HostedZoneID: "Z123FIELD",
			Credentials:         map[string]string{"hosted_zone_id": "Z456CRED"},
		}, logger)
		if got := dm.route53ZoneID(); got != "Z123FIELD" {
			t.Errorf("route53ZoneID() = %q, want Z123FIELD", got)
		}
	})

	t.Run("credentials fallback", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{
			Provider:    "route53",
			Zone:        "example.com",
			Credentials: map[string]string{"hosted_zone_id": "Z456CRED"},
		}, logger)
		if got := dm.route53ZoneID(); got != "Z456CRED" {
			t.Errorf("route53ZoneID() = %q, want Z456CRED", got)
		}
	})

	t.Run("zone fallback", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{
			Provider: "route53",
			Zone:     "Z789ZONE",
		}, logger)
		if got := dm.route53ZoneID(); got != "Z789ZONE" {
			t.Errorf("route53ZoneID() = %q, want Z789ZONE", got)
		}
	})
}

func TestRoute53ManagerWaitForSync(t *testing.T) {
	logger := logrus.New()

	dm := NewDNSManager(DNSConfig{
		Provider:            "route53",
		Route53HostedZoneID: "Z123",
		Credentials:         map[string]string{"wait_for_sync": "true"},
	}, logger)
	if !dm.route53Manager().WaitForSync {
		t.Error("expected WaitForSync to be enabled")
	}

	dm = NewDNSManager(DNSConfig{
		Provider:            "route53",
		Route53HostedZoneID: "Z123",
	}, logger)
	if dm.route53Manager().WaitForSync {
		t.Error("expected WaitForSync to be disabled by default")
	}
}

func TestRoute53ValidateConfig(t *testing.T) {
	logger := logrus.New()

	t.Run("zone ID required", func(t *testing.T) {
		manager := NewRealRoute53Manager(AWSConfig{}, "", logger)
		if err := manager.validateConfig(); err == nil {
			t.Error("expected error when hosted zone ID is missing")
		}
	})

	t.Run("default credential chain allowed", func(t *testing.T) {
		manager := NewRealRoute53Manager(AWSConfig{}, "Z123", logger)
		if err := manager.validateConfig(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("access key without secret rejected", func(t *testing.T) {
		manager := NewRealRoute53Manager(AWSConfig{AccessKeyID: "AKIA..."}, "Z123", logger)
		if err := manager.validateConfig(); err == nil {
			t.Error("expected error when secret access key is missing")
		}
	})
}

func TestGCPProjectAndZoneResolution(t *testing.T) {
	logger := logrus.New()

	t.Run("explicit fields", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{
			Provider:       "gcp_dns",
			GCPProject:     "my-project",
			GCPManagedZone: "my-zone",
		}, logger)
		project, zone, err := dm.gcpProjectAndZone()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if project != "my-project" || zone != "my-zone" {
			t.Errorf("got (%q, %q)", project, zone)
		}
	})

	t.Run("credentials fallback", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{
			Provider: "gcp_dns",
			Credentials: map[string]string{
				"project_id":   "cred-project",
				"managed_zone": "cred-zone",
			},
		}, logger)
		project, zone, err := dm.gcpProjectAndZone()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if project != "cred-project" || zone != "cred-zone" {
			t.Errorf("got (%q, %q)", project, zone)
		}
	})

	t.Run("missing project", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{Provider: "gcp_dns"}, logger)
		_, _, err := dm.gcpProjectAndZone()
		if err == nil || !strings.Contains(err.Error(), "project ID is required") {
			t.Errorf("expected project error, got: %v", err)
		}
	})

	t.Run("missing managed zone", func(t *testing.T) {
		dm := NewDNSManager(DNSConfig{Provider: "gcp_dns", GCPProject: "p"}, logger)
		_, _, err := dm.gcpProjectAndZone()
		if err == nil || !strings.Contains(err.Error(), "managed zone is required") {
			t.Errorf("expected managed zone error, got: %v", err)
		}
	})
}

func TestGCPDNSRecordRequiresConfig(t *testing.T) {
	logger := logrus.New()
	dm := NewDNSManager(DNSConfig{Provider: "gcp_dns"}, logger)

	err := dm.UpdateRecord(testContext(t), DNSRecord{Name: "host.example.com", Type: "A", Value: "192.0.2.1"})
	if err == nil || !strings.Contains(err.Error(), "project ID is required") {
		t.Errorf("expected config validation error, got: %v", err)
	}

	_, err = dm.GetRecord(testContext(t), "host.example.com", "A")
	if err == nil || !strings.Contains(err.Error(), "project ID is required") {
		t.Errorf("expected config validation error, got: %v", err)
	}
}
