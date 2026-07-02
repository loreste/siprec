package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"siprec-server/pkg/config"

	"github.com/sirupsen/logrus"
)

func newRedactionTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Auth.JWTSecret = "jwt-super-secret"
	cfg.Auth.AdminPassword = "admin-pass"
	cfg.Auth.AdminUsername = "admin"
	cfg.Messaging.AMQP.Password = "amqp-pass"
	cfg.Messaging.AMQP.Username = "guest"
	cfg.Cluster.Redis.Password = "redis-pass"
	cfg.Cluster.Redis.SentinelPassword = ""
	cfg.Recording.Storage.S3.SecretKey = "s3-secret"
	cfg.Recording.Storage.S3.AccessKey = "s3-access"
	cfg.Recording.Storage.S3.Bucket = "my-bucket"
	cfg.Recording.Storage.GCS.ServiceAccountKey = "gcs-sa-key"
	cfg.Recording.Storage.Azure.Account = "myaccount"
	cfg.Recording.Storage.Azure.Container = "recordings"
	cfg.Recording.Storage.Azure.SASToken = "azure-sas-secret"
	cfg.Recording.Storage.Azure.AccessKey = "azure-account-key"
	cfg.PauseResume.APIKey = "pause-key"
	cfg.Network.ExternalIP = "203.0.113.10"
	return cfg
}

func TestRedactConfigSecrets(t *testing.T) {
	cfg := newRedactionTestConfig()

	redacted := redactConfigSecrets(cfg)
	if redacted == nil {
		t.Fatal("expected non-nil redacted config")
	}

	// Top-level and nested secrets must be redacted
	secretChecks := map[string]string{
		"Auth.JWTSecret":                          redacted.Auth.JWTSecret,
		"Auth.AdminPassword":                      redacted.Auth.AdminPassword,
		"Messaging.AMQP.Password":                 redacted.Messaging.AMQP.Password,
		"Cluster.Redis.Password":                  redacted.Cluster.Redis.Password,
		"Recording.Storage.S3.SecretKey":          redacted.Recording.Storage.S3.SecretKey,
		"Recording.Storage.S3.AccessKey":          redacted.Recording.Storage.S3.AccessKey,
		"Recording.Storage.GCS.ServiceAccountKey": redacted.Recording.Storage.GCS.ServiceAccountKey,
		"Recording.Storage.Azure.SASToken":        redacted.Recording.Storage.Azure.SASToken,
		"Recording.Storage.Azure.AccessKey":       redacted.Recording.Storage.Azure.AccessKey,
		"PauseResume.APIKey":                      redacted.PauseResume.APIKey,
	}
	for field, value := range secretChecks {
		if value != redactedPlaceholder {
			t.Errorf("expected %s to be redacted, got %q", field, value)
		}
	}

	// Non-secret values must be preserved
	if redacted.Network.ExternalIP != "203.0.113.10" {
		t.Errorf("expected non-secret Network.ExternalIP to be preserved, got %q", redacted.Network.ExternalIP)
	}
	if redacted.Messaging.AMQP.Username != "guest" {
		t.Errorf("expected non-secret AMQP.Username to be preserved, got %q", redacted.Messaging.AMQP.Username)
	}
	if redacted.Recording.Storage.S3.Bucket != "my-bucket" {
		t.Errorf("expected non-secret S3.Bucket to be preserved, got %q", redacted.Recording.Storage.S3.Bucket)
	}

	// Empty secrets must remain empty so operators can see they are unset
	if redacted.Cluster.Redis.SentinelPassword != "" {
		t.Errorf("expected empty secret to remain empty, got %q", redacted.Cluster.Redis.SentinelPassword)
	}

	// The original configuration must not be mutated
	if cfg.Auth.JWTSecret != "jwt-super-secret" {
		t.Errorf("original config was mutated, JWTSecret is now %q", cfg.Auth.JWTSecret)
	}
	if cfg.Recording.Storage.S3.SecretKey != "s3-secret" {
		t.Errorf("original config was mutated, S3.SecretKey is now %q", cfg.Recording.Storage.S3.SecretKey)
	}
}

func TestRedactConfigSecretsNil(t *testing.T) {
	if redactConfigSecrets(nil) != nil {
		t.Fatal("expected nil result for nil config")
	}
}

func TestGetConfigHandlerRedactsSecrets(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cfg := newRedactionTestConfig()
	manager, err := config.NewHotReloadManager("/tmp/nonexistent-siprec-config.yaml", cfg, logger)
	if err != nil {
		t.Fatalf("failed to create hot reload manager: %v", err)
	}

	handlers := NewConfigHandlers(manager, logger)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	handlers.GetConfigHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	for _, secret := range []string{
		"jwt-super-secret",
		"admin-pass",
		"amqp-pass",
		"redis-pass",
		"s3-secret",
		"s3-access",
		"gcs-sa-key",
		"azure-sas-secret",
		"azure-account-key",
		"pause-key",
	} {
		if strings.Contains(body, secret) {
			t.Errorf("response body leaks secret value %q", secret)
		}
	}

	if !strings.Contains(body, redactedPlaceholder) {
		t.Error("expected response body to contain redaction placeholder")
	}
	if !strings.Contains(body, "203.0.113.10") {
		t.Error("expected non-secret value to be present in response body")
	}

	var response ConfigResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response.Config == nil {
		t.Fatal("expected config in response")
	}
	if response.Config.Auth.JWTSecret != redactedPlaceholder {
		t.Errorf("expected redacted JWT secret in response, got %q", response.Config.Auth.JWTSecret)
	}
}

func TestRedactReloadEvent(t *testing.T) {
	if redactReloadEvent(nil) != nil {
		t.Fatal("expected nil result for nil event")
	}

	event := &config.ReloadEvent{
		Success: true,
		Changes: []config.ConfigChange{
			{Field: "auth.jwt_secret", OldValue: "old-secret", NewValue: "new-secret", Type: "modified"},
			{Field: "http.port", OldValue: 8080, NewValue: 9090, Type: "modified"},
		},
		Errors: []config.ValidationError{
			{Field: "messaging.amqp.password", Value: "guest", Message: "weak password"},
		},
		Warnings: []config.ValidationWarning{
			{Field: "stt.google.api_key", Value: "key-value", Message: "short key"},
		},
	}

	sanitized := redactReloadEvent(event)

	if sanitized.Changes[0].OldValue != redactedPlaceholder || sanitized.Changes[0].NewValue != redactedPlaceholder {
		t.Errorf("expected secret change values to be redacted, got %v -> %v", sanitized.Changes[0].OldValue, sanitized.Changes[0].NewValue)
	}
	if sanitized.Changes[1].OldValue != 8080 || sanitized.Changes[1].NewValue != 9090 {
		t.Errorf("expected non-secret change values to be preserved, got %v -> %v", sanitized.Changes[1].OldValue, sanitized.Changes[1].NewValue)
	}
	if sanitized.Errors[0].Value != redactedPlaceholder {
		t.Errorf("expected secret error value to be redacted, got %v", sanitized.Errors[0].Value)
	}
	if sanitized.Warnings[0].Value != redactedPlaceholder {
		t.Errorf("expected secret warning value to be redacted, got %v", sanitized.Warnings[0].Value)
	}

	// Original event must not be mutated
	if event.Changes[0].OldValue != "old-secret" {
		t.Errorf("original event was mutated, got %v", event.Changes[0].OldValue)
	}
}

func TestRedactValidationValue(t *testing.T) {
	tests := []struct {
		field    string
		value    interface{}
		expected interface{}
	}{
		{"auth.jwt_secret", "some-secret", redactedPlaceholder},
		{"messaging.amqp.password", "guest", redactedPlaceholder},
		{"stt.google.api_key", "key-value", redactedPlaceholder},
		{"auth.jwt_secret", "", ""},
		{"auth.jwt_secret", nil, nil},
		{"network.host", "0.0.0.0", "0.0.0.0"},
		{"http.port", 8080, 8080},
	}

	for _, test := range tests {
		result := redactValidationValue(test.field, test.value)
		if result != test.expected {
			t.Errorf("redactValidationValue(%q, %v): expected %v, got %v", test.field, test.value, test.expected, result)
		}
	}
}
