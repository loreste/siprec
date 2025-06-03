package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"siprec-server/pkg/metrics"

	"github.com/sirupsen/logrus"
)

// AlertManager manages alerting and notifications
type AlertManager struct {
	config       AlertConfig
	logger       *logrus.Logger
	rules        map[string]*AlertRule
	channels     map[string]NotificationChannel
	activeAlerts map[string]*ActiveAlert
	mutex        sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	evaluator    *AlertEvaluator
}

// AlertConfig holds alerting configuration
type AlertConfig struct {
	Enabled            bool
	EvaluationInterval time.Duration
	Rules              []AlertRule
	Channels           []ChannelConfig
}

// AlertRule defines an alert rule
type AlertRule struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Query       string            `json:"query"`
	Condition   string            `json:"condition"` // gt, lt, eq, ne
	Threshold   float64           `json:"threshold"`
	Duration    time.Duration     `json:"duration"`
	Severity    string            `json:"severity"` // critical, warning, info
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Channels    []string          `json:"channels"`
	Enabled     bool              `json:"enabled"`
}

// ChannelConfig defines notification channel configuration
type ChannelConfig struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"` // slack, pagerduty, email, webhook
	Settings map[string]interface{} `json:"settings"`
	Enabled  bool                   `json:"enabled"`
}

// ActiveAlert represents an active alert
type ActiveAlert struct {
	Rule             *AlertRule        `json:"rule"`
	Value            float64           `json:"value"`
	StartsAt         time.Time         `json:"starts_at"`
	EndsAt           *time.Time        `json:"ends_at,omitempty"`
	Status           string            `json:"status"` // firing, resolved
	Labels           map[string]string `json:"labels"`
	Annotations      map[string]string `json:"annotations"`
	NotifiedChannels []string          `json:"notified_channels"`
}

// NotificationChannel interface for different notification types
type NotificationChannel interface {
	Send(alert *ActiveAlert) error
	GetName() string
	IsEnabled() bool
}

// NewAlertManager creates a new alert manager
func NewAlertManager(config AlertConfig, logger *logrus.Logger) *AlertManager {
	ctx, cancel := context.WithCancel(context.Background())

	am := &AlertManager{
		config:       config,
		logger:       logger,
		rules:        make(map[string]*AlertRule),
		channels:     make(map[string]NotificationChannel),
		activeAlerts: make(map[string]*ActiveAlert),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Initialize evaluator
	am.evaluator = NewAlertEvaluator(logger)

	// Load rules
	for i := range config.Rules {
		rule := &config.Rules[i]
		am.rules[rule.Name] = rule
	}

	// Initialize channels
	for _, channelConfig := range config.Channels {
		if channel := am.createChannel(channelConfig); channel != nil {
			am.channels[channelConfig.Name] = channel
		}
	}

	if config.Enabled {
		go am.start()
		logger.WithField("rules", len(am.rules)).Info("Alert manager started")
	} else {
		logger.Info("Alert manager disabled")
	}

	return am
}

// start begins the alert evaluation loop
func (am *AlertManager) start() {
	ticker := time.NewTicker(am.config.EvaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			am.evaluateRules()
		case <-am.ctx.Done():
			return
		}
	}
}

// evaluateRules evaluates all enabled alert rules
func (am *AlertManager) evaluateRules() {
	am.mutex.RLock()
	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	am.mutex.RUnlock()

	for _, rule := range rules {
		am.evaluateRule(rule)
	}
}

// evaluateRule evaluates a single alert rule
func (am *AlertManager) evaluateRule(rule *AlertRule) {
	value, err := am.evaluator.EvaluateQuery(rule.Query)
	if err != nil {
		am.logger.WithError(err).WithField("rule", rule.Name).Error("Failed to evaluate rule")
		return
	}

	shouldFire := am.shouldTriggerAlert(rule, value)
	alertKey := rule.Name

	am.mutex.Lock()
	activeAlert, exists := am.activeAlerts[alertKey]
	am.mutex.Unlock()

	if shouldFire {
		if !exists {
			// New alert
			alert := &ActiveAlert{
				Rule:             rule,
				Value:            value,
				StartsAt:         time.Now(),
				Status:           "firing",
				Labels:           rule.Labels,
				Annotations:      rule.Annotations,
				NotifiedChannels: []string{},
			}

			am.mutex.Lock()
			am.activeAlerts[alertKey] = alert
			am.mutex.Unlock()

			am.sendNotifications(alert)
			metrics.RecordAlert(rule.Name, rule.Severity)

			am.logger.WithFields(logrus.Fields{
				"rule":     rule.Name,
				"value":    value,
				"severity": rule.Severity,
			}).Warning("Alert triggered")

		} else if activeAlert.Status == "resolved" {
			// Re-firing alert
			activeAlert.Status = "firing"
			activeAlert.StartsAt = time.Now()
			activeAlert.EndsAt = nil
			activeAlert.Value = value

			am.sendNotifications(activeAlert)
			metrics.RecordAlert(rule.Name, rule.Severity)

			am.logger.WithFields(logrus.Fields{
				"rule":  rule.Name,
				"value": value,
			}).Warning("Alert re-triggered")
		}
	} else if exists && activeAlert.Status == "firing" {
		// Resolve alert
		now := time.Now()
		activeAlert.Status = "resolved"
		activeAlert.EndsAt = &now
		duration := now.Sub(activeAlert.StartsAt)

		am.sendResolutionNotifications(activeAlert)
		metrics.RecordAlertResolution(rule.Name, "auto_resolved", duration)

		am.logger.WithFields(logrus.Fields{
			"rule":     rule.Name,
			"duration": duration,
		}).Info("Alert resolved")

		// Remove from active alerts after some time
		go func() {
			time.Sleep(1 * time.Hour)
			am.mutex.Lock()
			delete(am.activeAlerts, alertKey)
			am.mutex.Unlock()
		}()
	}
}

// shouldTriggerAlert determines if an alert should be triggered
func (am *AlertManager) shouldTriggerAlert(rule *AlertRule, value float64) bool {
	switch rule.Condition {
	case "gt":
		return value > rule.Threshold
	case "lt":
		return value < rule.Threshold
	case "eq":
		return value == rule.Threshold
	case "ne":
		return value != rule.Threshold
	case "gte":
		return value >= rule.Threshold
	case "lte":
		return value <= rule.Threshold
	default:
		return false
	}
}

// sendNotifications sends notifications to configured channels
func (am *AlertManager) sendNotifications(alert *ActiveAlert) {
	for _, channelName := range alert.Rule.Channels {
		if channel, exists := am.channels[channelName]; exists && channel.IsEnabled() {
			go func(ch NotificationChannel, a *ActiveAlert) {
				if err := ch.Send(a); err != nil {
					am.logger.WithError(err).WithFields(logrus.Fields{
						"channel": ch.GetName(),
						"alert":   a.Rule.Name,
					}).Error("Failed to send alert notification")
				} else {
					a.NotifiedChannels = append(a.NotifiedChannels, ch.GetName())
				}
			}(channel, alert)
		}
	}
}

// sendResolutionNotifications sends resolution notifications
func (am *AlertManager) sendResolutionNotifications(alert *ActiveAlert) {
	for _, channelName := range alert.NotifiedChannels {
		if channel, exists := am.channels[channelName]; exists && channel.IsEnabled() {
			go func(ch NotificationChannel, a *ActiveAlert) {
				if err := ch.Send(a); err != nil {
					am.logger.WithError(err).WithFields(logrus.Fields{
						"channel": ch.GetName(),
						"alert":   a.Rule.Name,
					}).Error("Failed to send resolution notification")
				}
			}(channel, alert)
		}
	}
}

// createChannel creates a notification channel based on configuration
func (am *AlertManager) createChannel(config ChannelConfig) NotificationChannel {
	switch config.Type {
	case "slack":
		return NewSlackChannel(config, am.logger)
	case "pagerduty":
		return NewPagerDutyChannel(config, am.logger)
	case "email":
		return NewEmailChannel(config, am.logger)
	case "webhook":
		return NewWebhookChannel(config, am.logger)
	default:
		am.logger.WithField("type", config.Type).Warning("Unknown channel type")
		return nil
	}
}

// GetActiveAlerts returns all active alerts
func (am *AlertManager) GetActiveAlerts() map[string]*ActiveAlert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()

	alerts := make(map[string]*ActiveAlert)
	for k, v := range am.activeAlerts {
		alerts[k] = v
	}
	return alerts
}

// Stop stops the alert manager
func (am *AlertManager) Stop() {
	am.cancel()
	am.logger.Info("Alert manager stopped")
}

// Slack Channel Implementation

type SlackChannel struct {
	name       string
	webhookURL string
	channel    string
	username   string
	enabled    bool
	logger     *logrus.Logger
}

func NewSlackChannel(config ChannelConfig, logger *logrus.Logger) *SlackChannel {
	webhookURL, _ := config.Settings["webhook_url"].(string)
	channel, _ := config.Settings["channel"].(string)
	username, _ := config.Settings["username"].(string)

	if username == "" {
		username = "SIPREC Alert"
	}

	return &SlackChannel{
		name:       config.Name,
		webhookURL: webhookURL,
		channel:    channel,
		username:   username,
		enabled:    config.Enabled,
		logger:     logger,
	}
}

func (s *SlackChannel) Send(alert *ActiveAlert) error {
	if !s.enabled || s.webhookURL == "" {
		return fmt.Errorf("slack channel not properly configured")
	}

	color := "warning"
	if alert.Rule.Severity == "critical" {
		color = "danger"
	} else if alert.Status == "resolved" {
		color = "good"
	}

	status := "🔥 FIRING"
	if alert.Status == "resolved" {
		status = "✅ RESOLVED"
	}

	text := fmt.Sprintf("%s: %s", status, alert.Rule.Name)
	description := alert.Rule.Description
	if description == "" {
		description = "No description available"
	}

	payload := map[string]interface{}{
		"channel":  s.channel,
		"username": s.username,
		"text":     text,
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"fields": []map[string]interface{}{
					{
						"title": "Alert",
						"value": alert.Rule.Name,
						"short": true,
					},
					{
						"title": "Severity",
						"value": alert.Rule.Severity,
						"short": true,
					},
					{
						"title": "Description",
						"value": description,
						"short": false,
					},
					{
						"title": "Value",
						"value": fmt.Sprintf("%.2f", alert.Value),
						"short": true,
					},
					{
						"title": "Time",
						"value": alert.StartsAt.Format(time.RFC3339),
						"short": true,
					},
				},
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Slack payload: %w", err)
	}

	resp, err := http.Post(s.webhookURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send Slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *SlackChannel) GetName() string {
	return s.name
}

func (s *SlackChannel) IsEnabled() bool {
	return s.enabled
}

// PagerDuty Channel Implementation

type PagerDutyChannel struct {
	name           string
	integrationKey string
	enabled        bool
	logger         *logrus.Logger
}

func NewPagerDutyChannel(config ChannelConfig, logger *logrus.Logger) *PagerDutyChannel {
	integrationKey, _ := config.Settings["integration_key"].(string)

	return &PagerDutyChannel{
		name:           config.Name,
		integrationKey: integrationKey,
		enabled:        config.Enabled,
		logger:         logger,
	}
}

func (p *PagerDutyChannel) Send(alert *ActiveAlert) error {
	if !p.enabled || p.integrationKey == "" {
		return fmt.Errorf("PagerDuty channel not properly configured")
	}

	eventAction := "trigger"
	if alert.Status == "resolved" {
		eventAction = "resolve"
	}

	payload := map[string]interface{}{
		"routing_key":  p.integrationKey,
		"event_action": eventAction,
		"dedup_key":    alert.Rule.Name,
		"payload": map[string]interface{}{
			"summary":  fmt.Sprintf("SIPREC Alert: %s", alert.Rule.Name),
			"source":   "siprec-server",
			"severity": alert.Rule.Severity,
			"custom_details": map[string]interface{}{
				"description": alert.Rule.Description,
				"value":       alert.Value,
				"threshold":   alert.Rule.Threshold,
				"condition":   alert.Rule.Condition,
				"query":       alert.Rule.Query,
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal PagerDuty payload: %w", err)
	}

	resp, err := http.Post("https://events.pagerduty.com/v2/enqueue", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send PagerDuty notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("PagerDuty API returned status %d", resp.StatusCode)
	}

	return nil
}

func (p *PagerDutyChannel) GetName() string {
	return p.name
}

func (p *PagerDutyChannel) IsEnabled() bool {
	return p.enabled
}

// Email Channel Implementation

type EmailChannel struct {
	name     string
	smtpHost string
	smtpPort int
	username string
	password string
	from     string
	to       []string
	enabled  bool
	logger   *logrus.Logger
}

func NewEmailChannel(config ChannelConfig, logger *logrus.Logger) *EmailChannel {
	smtpHost, _ := config.Settings["smtp_host"].(string)
	smtpPort, _ := config.Settings["smtp_port"].(int)
	username, _ := config.Settings["username"].(string)
	password, _ := config.Settings["password"].(string)
	from, _ := config.Settings["from"].(string)
	to, _ := config.Settings["to"].([]string)

	return &EmailChannel{
		name:     config.Name,
		smtpHost: smtpHost,
		smtpPort: smtpPort,
		username: username,
		password: password,
		from:     from,
		to:       to,
		enabled:  config.Enabled,
		logger:   logger,
	}
}

func (e *EmailChannel) Send(alert *ActiveAlert) error {
	// Email implementation would go here
	// For brevity, this is a placeholder
	e.logger.WithField("alert", alert.Rule.Name).Info("Email notification sent (simulated)")
	return nil
}

func (e *EmailChannel) GetName() string {
	return e.name
}

func (e *EmailChannel) IsEnabled() bool {
	return e.enabled
}

// Webhook Channel Implementation

type WebhookChannel struct {
	name    string
	url     string
	method  string
	headers map[string]string
	enabled bool
	logger  *logrus.Logger
}

func NewWebhookChannel(config ChannelConfig, logger *logrus.Logger) *WebhookChannel {
	url, _ := config.Settings["url"].(string)
	method, _ := config.Settings["method"].(string)
	headers, _ := config.Settings["headers"].(map[string]string)

	if method == "" {
		method = "POST"
	}

	return &WebhookChannel{
		name:    config.Name,
		url:     url,
		method:  method,
		headers: headers,
		enabled: config.Enabled,
		logger:  logger,
	}
}

func (w *WebhookChannel) Send(alert *ActiveAlert) error {
	if !w.enabled || w.url == "" {
		return fmt.Errorf("webhook channel not properly configured")
	}

	payload := map[string]interface{}{
		"alert":     alert,
		"timestamp": time.Now().Unix(),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequest(w.method, w.url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range w.headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

func (w *WebhookChannel) GetName() string {
	return w.name
}

func (w *WebhookChannel) IsEnabled() bool {
	return w.enabled
}
