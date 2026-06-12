package alerting

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Email Channel Implementation

const (
	emailTLSModeAuto     = "auto"     // implicit TLS on port 465, otherwise STARTTLS when offered
	emailTLSModeImplicit = "implicit" // TLS from the first byte (SMTPS)
	emailTLSModeStartTLS = "starttls" // STARTTLS required, fail if not offered
	emailTLSModeNone     = "none"     // plaintext only (no authentication possible)

	defaultSMTPPort     = 587
	defaultEmailTimeout = 30 * time.Second
)

type EmailChannel struct {
	name               string
	smtpHost           string
	smtpPort           int
	username           string
	password           string
	from               string
	to                 []string
	tlsMode            string
	insecureSkipVerify bool
	timeout            time.Duration
	enabled            bool
	logger             *logrus.Logger
}

func NewEmailChannel(config ChannelConfig, logger *logrus.Logger) *EmailChannel {
	smtpHost := settingString(config.Settings, "smtp_host")
	smtpPort := settingInt(config.Settings, "smtp_port", defaultSMTPPort)
	username := settingString(config.Settings, "username")
	password := settingString(config.Settings, "password")
	from := settingString(config.Settings, "from")
	to := settingStringSlice(config.Settings, "to")
	tlsMode := strings.ToLower(settingString(config.Settings, "tls_mode"))
	insecureSkipVerify := settingBool(config.Settings, "insecure_skip_verify")

	timeout := defaultEmailTimeout
	if seconds := settingInt(config.Settings, "timeout_seconds", 0); seconds > 0 {
		timeout = time.Duration(seconds) * time.Second
	}

	switch tlsMode {
	case emailTLSModeAuto, emailTLSModeImplicit, emailTLSModeStartTLS, emailTLSModeNone:
	case "":
		tlsMode = emailTLSModeAuto
	default:
		logger.WithFields(logrus.Fields{
			"channel":  config.Name,
			"tls_mode": tlsMode,
		}).Warning("Unknown email TLS mode, falling back to auto")
		tlsMode = emailTLSModeAuto
	}

	if config.Enabled && (smtpHost == "" || from == "" || len(to) == 0) {
		logger.WithField("channel", config.Name).Warning("Email channel missing smtp_host, from, or to settings; sends will fail")
	}

	return &EmailChannel{
		name:               config.Name,
		smtpHost:           smtpHost,
		smtpPort:           smtpPort,
		username:           username,
		password:           password,
		from:               from,
		to:                 to,
		tlsMode:            tlsMode,
		insecureSkipVerify: insecureSkipVerify,
		timeout:            timeout,
		enabled:            config.Enabled,
		logger:             logger,
	}
}

func (e *EmailChannel) Send(alert *ActiveAlert) error {
	if !e.enabled {
		return fmt.Errorf("email channel not enabled")
	}
	if e.smtpHost == "" || e.from == "" || len(e.to) == 0 {
		return fmt.Errorf("email channel not properly configured (smtp_host, from, and to are required)")
	}

	message, err := e.buildMessage(alert, time.Now())
	if err != nil {
		return fmt.Errorf("failed to build email message: %w", err)
	}

	if err := e.deliver(message); err != nil {
		return fmt.Errorf("failed to send email notification: %w", err)
	}

	e.logger.WithFields(logrus.Fields{
		"channel":    e.name,
		"alert":      alert.Rule.Name,
		"recipients": len(e.to),
	}).Info("Email notification sent")

	return nil
}

// buildMessage renders an RFC 5322 message for the alert.
func (e *EmailChannel) buildMessage(alert *ActiveAlert, now time.Time) ([]byte, error) {
	status := "FIRING"
	if alert.Status == "resolved" {
		status = "RESOLVED"
	}

	subject := fmt.Sprintf("[SIPREC][%s] %s: %s", strings.ToUpper(alert.Rule.Severity), status, alert.Rule.Name)

	messageID, err := e.generateMessageID(now)
	if err != nil {
		return nil, err
	}

	description := alert.Rule.Description
	if description == "" {
		description = "No description available"
	}

	var body strings.Builder
	fmt.Fprintf(&body, "Alert:       %s\r\n", alert.Rule.Name)
	fmt.Fprintf(&body, "Status:      %s\r\n", status)
	fmt.Fprintf(&body, "Severity:    %s\r\n", alert.Rule.Severity)
	fmt.Fprintf(&body, "Value:       %g\r\n", alert.Value)
	fmt.Fprintf(&body, "Threshold:   %s %g\r\n", alert.Rule.Condition, alert.Rule.Threshold)
	fmt.Fprintf(&body, "Started At:  %s\r\n", alert.StartsAt.Format(time.RFC3339))
	if alert.EndsAt != nil {
		fmt.Fprintf(&body, "Resolved At: %s\r\n", alert.EndsAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&body, "Query:       %s\r\n", alert.Rule.Query)
	fmt.Fprintf(&body, "\r\nDescription:\r\n%s\r\n", description)

	if len(alert.Labels) > 0 {
		body.WriteString("\r\nLabels:\r\n")
		for key, value := range alert.Labels {
			fmt.Fprintf(&body, "  %s = %s\r\n", key, value)
		}
	}

	var message strings.Builder
	fmt.Fprintf(&message, "From: %s\r\n", sanitizeHeader(e.from))
	fmt.Fprintf(&message, "To: %s\r\n", sanitizeHeader(strings.Join(e.to, ", ")))
	fmt.Fprintf(&message, "Subject: %s\r\n", sanitizeHeader(subject))
	fmt.Fprintf(&message, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	message.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	message.WriteString("\r\n")
	message.WriteString(body.String())

	return []byte(message.String()), nil
}

// generateMessageID creates a unique RFC 5322 Message-ID.
func (e *EmailChannel) generateMessageID(now time.Time) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("failed to generate message id: %w", err)
	}

	domain := e.smtpHost
	if address, err := mail.ParseAddress(e.from); err == nil {
		if at := strings.LastIndex(address.Address, "@"); at >= 0 && at < len(address.Address)-1 {
			domain = address.Address[at+1:]
		}
	}
	if domain == "" {
		domain = "siprec-server"
	}

	return fmt.Sprintf("<%d.%s@%s>", now.UnixNano(), hex.EncodeToString(random), domain), nil
}

// deliver sends the rendered message over SMTP with TLS and timeouts.
func (e *EmailChannel) deliver(message []byte) error {
	addr := net.JoinHostPort(e.smtpHost, strconv.Itoa(e.smtpPort))
	implicitTLS := e.tlsMode == emailTLSModeImplicit || (e.tlsMode == emailTLSModeAuto && e.smtpPort == 465)

	tlsConfig := &tls.Config{
		ServerName:         e.smtpHost,
		InsecureSkipVerify: e.insecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}

	dialer := &net.Dialer{Timeout: e.timeout}

	var conn net.Conn
	var err error
	if implicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to SMTP server: %w", err)
	}

	// Bound the whole SMTP transaction, not just the dial.
	if err := conn.SetDeadline(time.Now().Add(e.timeout)); err != nil {
		conn.Close()
		return fmt.Errorf("failed to set connection deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, e.smtpHost)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create SMTP client: %w", err)
	}
	defer client.Close()

	if !implicitTLS && e.tlsMode != emailTLSModeNone {
		supported, _ := client.Extension("STARTTLS")
		if supported {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS failed: %w", err)
			}
		} else if e.tlsMode == emailTLSModeStartTLS {
			return fmt.Errorf("SMTP server does not support required STARTTLS")
		}
	}

	if e.username != "" {
		if _, isTLS := client.TLSConnectionState(); !isTLS {
			return fmt.Errorf("refusing to authenticate over plaintext connection; enable TLS or remove credentials")
		}
		auth := smtp.PlainAuth("", e.username, e.password, e.smtpHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}

	if err := client.Mail(e.envelopeFrom()); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}

	for _, recipient := range e.to {
		if err := client.Rcpt(strings.TrimSpace(recipient)); err != nil {
			return fmt.Errorf("SMTP RCPT TO failed: %w", err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return fmt.Errorf("failed to write email body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to finalize email body: %w", err)
	}

	if err := client.Quit(); err != nil {
		return fmt.Errorf("SMTP QUIT failed: %w", err)
	}

	return nil
}

// envelopeFrom extracts the bare address for the SMTP envelope, accepting
// both "alerts@example.com" and "SIPREC Alerts <alerts@example.com>" forms.
func (e *EmailChannel) envelopeFrom() string {
	if address, err := mail.ParseAddress(e.from); err == nil {
		return address.Address
	}
	return e.from
}

func (e *EmailChannel) GetName() string {
	return e.name
}

func (e *EmailChannel) IsEnabled() bool {
	return e.enabled
}

// sanitizeHeader strips CR/LF to prevent header injection from alert content.
func sanitizeHeader(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

// Settings helpers tolerate the value types produced by JSON decoding
// (float64, []interface{}) as well as natively constructed configs.

func settingString(settings map[string]interface{}, key string) string {
	value, _ := settings[key].(string)
	return strings.TrimSpace(value)
}

func settingInt(settings map[string]interface{}, key string, fallback int) int {
	switch value := settings[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return fallback
}

func settingBool(settings map[string]interface{}, key string) bool {
	switch value := settings[key].(type) {
	case bool:
		return value
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		return err == nil && parsed
	}
	return false
}

func settingStringSlice(settings map[string]interface{}, key string) []string {
	appendNonEmpty := func(result []string, value string) []string {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
		return result
	}

	var result []string
	switch value := settings[key].(type) {
	case []string:
		for _, item := range value {
			result = appendNonEmpty(result, item)
		}
	case []interface{}:
		for _, item := range value {
			if text, ok := item.(string); ok {
				result = appendNonEmpty(result, text)
			}
		}
	case string:
		for _, item := range strings.Split(value, ",") {
			result = appendNonEmpty(result, item)
		}
	}
	return result
}
