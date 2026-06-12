package alerting

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func testLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	logger.SetLevel(logrus.PanicLevel)
	return logger
}

func testEmailChannel(t *testing.T) *EmailChannel {
	t.Helper()
	return NewEmailChannel(ChannelConfig{
		Name: "ops-email",
		Type: "email",
		Settings: map[string]interface{}{
			"smtp_host": "smtp.example.com",
			"smtp_port": float64(587), // JSON-decoded numbers arrive as float64
			"username":  "alerts",
			"password":  "secret",
			"from":      "SIPREC Alerts <alerts@example.com>",
			"to":        []interface{}{"oncall@example.com", "ops@example.com"},
		},
		Enabled: true,
	}, testLogger())
}

func testActiveAlert() *ActiveAlert {
	return &ActiveAlert{
		Rule: &AlertRule{
			Name:        "HighSessionCount",
			Description: "Active SIP sessions above safe capacity",
			Query:       "siprec_sip_sessions_active",
			Condition:   "gt",
			Threshold:   100,
			Severity:    "critical",
		},
		Value:    142,
		StartsAt: time.Date(2026, 6, 12, 10, 30, 0, 0, time.UTC),
		Status:   "firing",
		Labels:   map[string]string{"service": "siprec"},
	}
}

func TestEmailChannelSettingsParsing(t *testing.T) {
	channel := testEmailChannel(t)

	if channel.smtpHost != "smtp.example.com" {
		t.Errorf("smtpHost = %q, want smtp.example.com", channel.smtpHost)
	}
	if channel.smtpPort != 587 {
		t.Errorf("smtpPort = %d, want 587", channel.smtpPort)
	}
	if len(channel.to) != 2 || channel.to[0] != "oncall@example.com" || channel.to[1] != "ops@example.com" {
		t.Errorf("to = %v, want both recipients", channel.to)
	}
	if channel.tlsMode != emailTLSModeAuto {
		t.Errorf("tlsMode = %q, want auto default", channel.tlsMode)
	}
	if channel.timeout != defaultEmailTimeout {
		t.Errorf("timeout = %v, want %v", channel.timeout, defaultEmailTimeout)
	}
}

func TestEmailChannelSettingsAlternateForms(t *testing.T) {
	channel := NewEmailChannel(ChannelConfig{
		Name: "alt",
		Type: "email",
		Settings: map[string]interface{}{
			"smtp_host":       "mail.example.com",
			"smtp_port":       "465",
			"from":            "alerts@example.com",
			"to":              "a@example.com, b@example.com",
			"tls_mode":        "implicit",
			"timeout_seconds": 5,
		},
		Enabled: true,
	}, testLogger())

	if channel.smtpPort != 465 {
		t.Errorf("smtpPort = %d, want 465", channel.smtpPort)
	}
	if len(channel.to) != 2 || channel.to[1] != "b@example.com" {
		t.Errorf("to = %v, want comma-separated recipients parsed", channel.to)
	}
	if channel.tlsMode != emailTLSModeImplicit {
		t.Errorf("tlsMode = %q, want implicit", channel.tlsMode)
	}
	if channel.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", channel.timeout)
	}
}

func TestEmailBuildMessage(t *testing.T) {
	channel := testEmailChannel(t)
	alert := testActiveAlert()
	now := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)

	raw, err := channel.buildMessage(alert, now)
	if err != nil {
		t.Fatalf("buildMessage returned error: %v", err)
	}
	message := string(raw)

	parts := strings.SplitN(message, "\r\n\r\n", 2)
	if len(parts) != 2 {
		t.Fatalf("message has no header/body separator:\n%s", message)
	}
	headers, body := parts[0], parts[1]

	wantHeaders := []string{
		"From: SIPREC Alerts <alerts@example.com>",
		"To: oncall@example.com, ops@example.com",
		"Subject: [SIPREC][CRITICAL] FIRING: HighSessionCount",
		"Date: " + now.Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	for _, want := range wantHeaders {
		if !strings.Contains(headers, want) {
			t.Errorf("headers missing %q:\n%s", want, headers)
		}
	}

	if !strings.Contains(headers, "Message-ID: <") || !strings.Contains(headers, "@example.com>") {
		t.Errorf("headers missing Message-ID with from-domain:\n%s", headers)
	}

	wantBody := []string{
		"Alert:       HighSessionCount",
		"Status:      FIRING",
		"Severity:    critical",
		"Value:       142",
		"Threshold:   gt 100",
		"Started At:  2026-06-12T10:30:00Z",
		"Active SIP sessions above safe capacity",
	}
	for _, want := range wantBody {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}

	for _, line := range strings.Split(strings.TrimSuffix(message, "\r\n"), "\r\n") {
		if strings.HasSuffix(line, "\r") || strings.Contains(line, "\n") {
			t.Errorf("line has bare CR/LF: %q", line)
		}
	}
}

func TestEmailBuildMessageResolved(t *testing.T) {
	channel := testEmailChannel(t)
	alert := testActiveAlert()
	endsAt := alert.StartsAt.Add(15 * time.Minute)
	alert.Status = "resolved"
	alert.EndsAt = &endsAt

	raw, err := channel.buildMessage(alert, time.Now())
	if err != nil {
		t.Fatalf("buildMessage returned error: %v", err)
	}
	message := string(raw)

	if !strings.Contains(message, "Subject: [SIPREC][CRITICAL] RESOLVED: HighSessionCount") {
		t.Errorf("resolved subject missing:\n%s", message)
	}
	if !strings.Contains(message, "Resolved At: 2026-06-12T10:45:00Z") {
		t.Errorf("resolved timestamp missing:\n%s", message)
	}
}

func TestEmailBuildMessageSanitizesHeaders(t *testing.T) {
	channel := testEmailChannel(t)
	alert := testActiveAlert()
	alert.Rule.Name = "Injected\r\nBcc: attacker@example.com"

	raw, err := channel.buildMessage(alert, time.Now())
	if err != nil {
		t.Fatalf("buildMessage returned error: %v", err)
	}

	headers := strings.SplitN(string(raw), "\r\n\r\n", 2)[0]
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(line, "Bcc:") {
			t.Errorf("header injection not sanitized, injected header line present:\n%s", headers)
		}
	}
}

func TestEmailSendRejectsMisconfiguredChannel(t *testing.T) {
	channel := NewEmailChannel(ChannelConfig{
		Name:     "broken",
		Type:     "email",
		Settings: map[string]interface{}{},
		Enabled:  true,
	}, testLogger())

	if err := channel.Send(testActiveAlert()); err == nil {
		t.Fatal("Send should fail when smtp_host/from/to are missing")
	}

	channel.enabled = false
	if err := channel.Send(testActiveAlert()); err == nil {
		t.Fatal("Send should fail when channel is disabled")
	}
}

func TestEmailEnvelopeFrom(t *testing.T) {
	channel := testEmailChannel(t)
	if got := channel.envelopeFrom(); got != "alerts@example.com" {
		t.Errorf("envelopeFrom = %q, want bare address", got)
	}
}
