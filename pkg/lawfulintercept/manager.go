package lawfulintercept

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager handles lawful intercept operations
type Manager struct {
	config    Config
	logger    *logrus.Entry
	delivery  *DeliveryClient
	auditor   *AuditLogger
	encryptor *ContentEncryptor
	warrants  *WarrantVerifier

	// Active intercepts
	intercepts   map[string]*Intercept
	interceptsMu sync.RWMutex

	// Statistics
	stats *statsInternal

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config holds lawful intercept configuration
type Config struct {
	Enabled                     bool
	DeliveryEndpoint            string
	EncryptionKeyPath           string
	WarrantVerificationEndpoint string
	AuditLogPath                string
	MutualTLS                   bool
	ClientCertPath              string
	ClientKeyPath               string
	CACertPath                  string
	RetentionDays               int
	DeliveryTimeout             time.Duration
	MaxRetries                  int
	BatchSize                   int
	FlushInterval               time.Duration
}

// Intercept represents an active lawful intercept
type Intercept struct {
	ID              string                 `json:"id"`
	WarrantID       string                 `json:"warrant_id"`
	TargetID        string                 `json:"target_id"`
	TargetType      string                 `json:"target_type"` // phone, uri, ip
	StartTime       time.Time              `json:"start_time"`
	EndTime         *time.Time             `json:"end_time,omitempty"`
	Status          InterceptStatus        `json:"status"`
	CallsIntercepted int64                 `json:"calls_intercepted"`
	BytesDelivered  int64                  `json:"bytes_delivered"`
	Metadata        map[string]interface{} `json:"metadata"`
	mutex           sync.RWMutex
}

// InterceptStatus represents the status of an intercept
type InterceptStatus string

const (
	InterceptStatusActive    InterceptStatus = "active"
	InterceptStatusPaused    InterceptStatus = "paused"
	InterceptStatusExpired   InterceptStatus = "expired"
	InterceptStatusRevoked   InterceptStatus = "revoked"
)

// Stats tracks lawful intercept statistics (returned copy, no mutex)
type Stats struct {
	TotalIntercepts   int64     `json:"total_intercepts"`
	ActiveIntercepts  int64     `json:"active_intercepts"`
	CallsIntercepted  int64     `json:"calls_intercepted"`
	BytesDelivered    int64     `json:"bytes_delivered"`
	DeliveryFailures  int64     `json:"delivery_failures"`
	DeliverySuccesses int64     `json:"delivery_successes"`
	WarrantChecks     int64     `json:"warrant_checks"`
	WarrantFailures   int64     `json:"warrant_failures"`
	LastDeliveryTime  time.Time `json:"last_delivery_time"`
	LastWarrantCheck  time.Time `json:"last_warrant_check"`
}

// statsInternal tracks statistics with mutex protection
type statsInternal struct {
	mutex             sync.RWMutex
	TotalIntercepts   int64
	ActiveIntercepts  int64
	CallsIntercepted  int64
	BytesDelivered    int64
	DeliveryFailures  int64
	DeliverySuccesses int64
	WarrantChecks     int64
	WarrantFailures   int64
	LastDeliveryTime  time.Time
	LastWarrantCheck  time.Time
}

// NewManager creates a new lawful intercept manager
func NewManager(cfg Config, logger *logrus.Logger) (*Manager, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		config:     cfg,
		logger:     logger.WithField("component", "lawful_intercept"),
		intercepts: make(map[string]*Intercept),
		stats:      &statsInternal{},
		ctx:        ctx,
		cancel:     cancel,
	}

	// Initialize audit logger
	auditor, err := NewAuditLogger(cfg.AuditLogPath, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create audit logger: %w", err)
	}
	m.auditor = auditor

	// Initialize content encryptor
	if cfg.EncryptionKeyPath != "" {
		encryptor, err := NewContentEncryptor(cfg.EncryptionKeyPath)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create content encryptor: %w", err)
		}
		m.encryptor = encryptor
	}

	// Initialize delivery client
	tlsConfig, err := m.createTLSConfig()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create TLS config: %w", err)
	}

	m.delivery = NewDeliveryClient(DeliveryConfig{
		Endpoint:      cfg.DeliveryEndpoint,
		TLSConfig:     tlsConfig,
		Timeout:       cfg.DeliveryTimeout,
		MaxRetries:    cfg.MaxRetries,
		BatchSize:     cfg.BatchSize,
		FlushInterval: cfg.FlushInterval,
	}, logger)

	// Initialize warrant verifier
	if cfg.WarrantVerificationEndpoint != "" {
		m.warrants = NewWarrantVerifier(cfg.WarrantVerificationEndpoint, tlsConfig, logger)
	}

	m.logger.Info("Lawful intercept manager initialized")
	m.auditor.Log(AuditEventSystemStart, "system", "Lawful intercept manager started", nil)

	return m, nil
}

// createTLSConfig creates TLS configuration for mTLS
func (m *Manager) createTLSConfig() (*tls.Config, error) {
	if !m.config.MutualTLS {
		return &tls.Config{
			MinVersion: tls.VersionTLS12,
		}, nil
	}

	// Load client certificate
	cert, err := tls.LoadX509KeyPair(m.config.ClientCertPath, m.config.ClientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate if provided
	var caCertPool *x509.CertPool
	if m.config.CACertPath != "" {
		caCert, err := os.ReadFile(m.config.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		caCertPool = x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// RegisterIntercept registers a new lawful intercept
func (m *Manager) RegisterIntercept(warrantID, targetID, targetType string, metadata map[string]interface{}) (*Intercept, error) {
	// Verify warrant if verification endpoint is configured
	if m.warrants != nil {
		valid, err := m.warrants.Verify(m.ctx, warrantID, targetID)
		if err != nil {
			m.stats.mutex.Lock()
			m.stats.WarrantFailures++
			m.stats.mutex.Unlock()
			m.auditor.Log(AuditEventWarrantFailed, warrantID, "Warrant verification failed", map[string]interface{}{
				"error":     err.Error(),
				"target_id": targetID,
			})
			return nil, fmt.Errorf("warrant verification failed: %w", err)
		}
		if !valid {
			m.auditor.Log(AuditEventWarrantInvalid, warrantID, "Invalid warrant", map[string]interface{}{
				"target_id": targetID,
			})
			return nil, fmt.Errorf("warrant is invalid or expired")
		}
		m.stats.mutex.Lock()
		m.stats.WarrantChecks++
		m.stats.LastWarrantCheck = time.Now()
		m.stats.mutex.Unlock()
	}

	intercept := &Intercept{
		ID:         fmt.Sprintf("li-%d", time.Now().UnixNano()),
		WarrantID:  warrantID,
		TargetID:   targetID,
		TargetType: targetType,
		StartTime:  time.Now(),
		Status:     InterceptStatusActive,
		Metadata:   metadata,
	}

	m.interceptsMu.Lock()
	m.intercepts[intercept.ID] = intercept
	m.interceptsMu.Unlock()

	m.stats.mutex.Lock()
	m.stats.TotalIntercepts++
	m.stats.ActiveIntercepts++
	m.stats.mutex.Unlock()

	m.auditor.Log(AuditEventInterceptStarted, warrantID, "Intercept registered", map[string]interface{}{
		"intercept_id": intercept.ID,
		"target_id":    targetID,
		"target_type":  targetType,
	})

	m.logger.WithFields(logrus.Fields{
		"intercept_id": intercept.ID,
		"warrant_id":   warrantID,
		"target_id":    targetID,
	}).Info("Lawful intercept registered")

	return intercept, nil
}

// CheckTarget checks if a target (phone number, URI, IP) is under intercept
func (m *Manager) CheckTarget(targetID string) []*Intercept {
	m.interceptsMu.RLock()
	defer m.interceptsMu.RUnlock()

	var matches []*Intercept
	for _, intercept := range m.intercepts {
		if intercept.Status == InterceptStatusActive && intercept.TargetID == targetID {
			matches = append(matches, intercept)
		}
	}
	return matches
}

// DeliverContent delivers intercepted content to LEA
func (m *Manager) DeliverContent(interceptID string, content *InterceptedContent) error {
	m.interceptsMu.RLock()
	intercept, exists := m.intercepts[interceptID]
	m.interceptsMu.RUnlock()

	if !exists {
		return fmt.Errorf("intercept not found: %s", interceptID)
	}

	// Encrypt content if encryptor is configured
	var payload []byte
	var err error
	if m.encryptor != nil {
		payload, err = m.encryptor.Encrypt(content)
		if err != nil {
			return fmt.Errorf("failed to encrypt content: %w", err)
		}
	} else {
		payload, err = json.Marshal(content)
		if err != nil {
			return fmt.Errorf("failed to marshal content: %w", err)
		}
	}

	// Deliver to LEA
	err = m.delivery.Deliver(m.ctx, intercept.WarrantID, interceptID, payload)
	if err != nil {
		m.stats.mutex.Lock()
		m.stats.DeliveryFailures++
		m.stats.mutex.Unlock()

		m.auditor.Log(AuditEventDeliveryFailed, intercept.WarrantID, "Content delivery failed", map[string]interface{}{
			"intercept_id": interceptID,
			"error":        err.Error(),
		})
		return fmt.Errorf("failed to deliver content: %w", err)
	}

	// Update statistics
	intercept.mutex.Lock()
	intercept.CallsIntercepted++
	intercept.BytesDelivered += int64(len(payload))
	intercept.mutex.Unlock()

	m.stats.mutex.Lock()
	m.stats.DeliverySuccesses++
	m.stats.BytesDelivered += int64(len(payload))
	m.stats.CallsIntercepted++
	m.stats.LastDeliveryTime = time.Now()
	m.stats.mutex.Unlock()

	m.auditor.Log(AuditEventContentDelivered, intercept.WarrantID, "Content delivered", map[string]interface{}{
		"intercept_id": interceptID,
		"bytes":        len(payload),
	})

	return nil
}

// RevokeIntercept revokes an active intercept
func (m *Manager) RevokeIntercept(interceptID, reason string) error {
	m.interceptsMu.Lock()
	intercept, exists := m.intercepts[interceptID]
	if !exists {
		m.interceptsMu.Unlock()
		return fmt.Errorf("intercept not found: %s", interceptID)
	}

	now := time.Now()
	intercept.mutex.Lock()
	intercept.Status = InterceptStatusRevoked
	intercept.EndTime = &now
	intercept.mutex.Unlock()

	m.interceptsMu.Unlock()

	m.stats.mutex.Lock()
	m.stats.ActiveIntercepts--
	m.stats.mutex.Unlock()

	m.auditor.Log(AuditEventInterceptRevoked, intercept.WarrantID, reason, map[string]interface{}{
		"intercept_id": interceptID,
	})

	m.logger.WithFields(logrus.Fields{
		"intercept_id": interceptID,
		"reason":       reason,
	}).Info("Lawful intercept revoked")

	return nil
}

// GetStats returns current statistics
func (m *Manager) GetStats() Stats {
	m.stats.mutex.RLock()
	defer m.stats.mutex.RUnlock()
	return Stats{
		TotalIntercepts:   m.stats.TotalIntercepts,
		ActiveIntercepts:  m.stats.ActiveIntercepts,
		CallsIntercepted:  m.stats.CallsIntercepted,
		BytesDelivered:    m.stats.BytesDelivered,
		DeliveryFailures:  m.stats.DeliveryFailures,
		DeliverySuccesses: m.stats.DeliverySuccesses,
		WarrantChecks:     m.stats.WarrantChecks,
		WarrantFailures:   m.stats.WarrantFailures,
		LastDeliveryTime:  m.stats.LastDeliveryTime,
		LastWarrantCheck:  m.stats.LastWarrantCheck,
	}
}

// GetActiveIntercepts returns all active intercepts
func (m *Manager) GetActiveIntercepts() []*Intercept {
	m.interceptsMu.RLock()
	defer m.interceptsMu.RUnlock()

	var active []*Intercept
	for _, intercept := range m.intercepts {
		if intercept.Status == InterceptStatusActive {
			active = append(active, intercept)
		}
	}
	return active
}

// Close shuts down the manager
func (m *Manager) Close() error {
	m.cancel()
	m.wg.Wait()

	if m.delivery != nil {
		m.delivery.Close()
	}
	if m.auditor != nil {
		m.auditor.Close()
	}

	m.auditor.Log(AuditEventSystemStop, "system", "Lawful intercept manager stopped", nil)
	m.logger.Info("Lawful intercept manager closed")

	return nil
}

// InterceptedContent represents content to be delivered to LEA
type InterceptedContent struct {
	InterceptID   string                 `json:"intercept_id"`
	WarrantID     string                 `json:"warrant_id"`
	CallID        string                 `json:"call_id"`
	Timestamp     time.Time              `json:"timestamp"`
	ContentType   string                 `json:"content_type"` // audio, metadata, signaling
	Direction     string                 `json:"direction"`    // inbound, outbound
	SourceURI     string                 `json:"source_uri"`
	DestinationURI string                `json:"destination_uri"`
	Duration      time.Duration          `json:"duration,omitempty"`
	AudioData     []byte                 `json:"audio_data,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Checksum      string                 `json:"checksum"`
}
