package config

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func newTestValidator() *ConfigValidator {
	logger := logrus.New()
	logger.SetOutput(discardWriter{})
	return NewConfigValidator(logger)
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func hasError(v *ConfigValidator, field string) bool {
	for _, e := range v.errors {
		if e.Field == field {
			return true
		}
	}
	return false
}

func hasWarning(v *ConfigValidator, field string) bool {
	for _, w := range v.warnings {
		if w.Field == field {
			return true
		}
	}
	return false
}

func azureConfig(a RecordingAzureConfig) *Config {
	cfg := &Config{}
	cfg.Recording.Storage.Azure = a
	return cfg
}

func TestValidateAzureStorage_DisabledSkips(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{Enabled: false}))

	if len(v.errors) != 0 || len(v.warnings) != 0 {
		t.Fatalf("disabled Azure config should produce no errors/warnings, got %d errors, %d warnings", len(v.errors), len(v.warnings))
	}
}

func TestValidateAzureStorage_NoAuthMethod(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{
		Enabled:   true,
		Account:   "acct",
		Container: "recordings",
	}))

	if !hasError(v, "recording_azure_auth") {
		t.Fatalf("expected recording_azure_auth error when no auth method is set")
	}
}

func TestValidateAzureStorage_BothAuthMethods(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{
		Enabled:   true,
		Account:   "acct",
		Container: "recordings",
		SASToken:  "sv=x&sig=y",
		AccessKey: "dGVzdA==",
	}))

	if !hasError(v, "recording_azure_auth") {
		t.Fatalf("expected recording_azure_auth error when both auth methods are set")
	}
}

func TestValidateAzureStorage_SASTokenValid(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{
		Enabled:   true,
		Account:   "acct",
		Container: "recordings",
		SASToken:  "sv=x&sig=y",
	}))

	if len(v.errors) != 0 {
		t.Fatalf("valid SAS config should produce no errors, got %d: %+v", len(v.errors), v.errors)
	}
	if hasWarning(v, "recording_azure_access_key") {
		t.Fatalf("SAS config should not warn about account key")
	}
}

func TestValidateAzureStorage_AccessKeyWarns(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{
		Enabled:   true,
		Account:   "acct",
		Container: "recordings",
		AccessKey: "dGVzdA==",
	}))

	if len(v.errors) != 0 {
		t.Fatalf("account-key config should be valid (no errors), got %d: %+v", len(v.errors), v.errors)
	}
	if !hasWarning(v, "recording_azure_access_key") {
		t.Fatalf("expected a least-privilege warning when account key is used")
	}
}

func TestValidateAzureStorage_MissingAccountAndContainer(t *testing.T) {
	v := newTestValidator()
	v.validateAzureStorageConfig(azureConfig(RecordingAzureConfig{
		Enabled:  true,
		SASToken: "sv=x&sig=y",
	}))

	if !hasError(v, "recording_azure_account") {
		t.Fatalf("expected recording_azure_account error")
	}
	if !hasError(v, "recording_azure_container") {
		t.Fatalf("expected recording_azure_container error")
	}
}
