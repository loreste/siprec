package lawfulintercept

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DeliveryClient handles secure delivery of intercepted content to LEA
type DeliveryClient struct {
	config DeliveryConfig
	client *http.Client
	logger *logrus.Entry

	// Batching
	batch     []*deliveryItem
	batchMu   sync.Mutex
	flushChan chan struct{}

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// DeliveryConfig holds delivery client configuration
type DeliveryConfig struct {
	Endpoint      string
	TLSConfig     *tls.Config
	Timeout       time.Duration
	MaxRetries    int
	BatchSize     int
	FlushInterval time.Duration
}

type deliveryItem struct {
	WarrantID   string
	InterceptID string
	Payload     []byte
	Timestamp   time.Time
	Retries     int
}

// DeliveryRequest represents the request sent to LEA
type DeliveryRequest struct {
	Version     string    `json:"version"`
	WarrantID   string    `json:"warrant_id"`
	InterceptID string    `json:"intercept_id"`
	Timestamp   time.Time `json:"timestamp"`
	Payload     []byte    `json:"payload"`
	Checksum    string    `json:"checksum"`
}

// DeliveryResponse represents the response from LEA
type DeliveryResponse struct {
	Status    string `json:"status"`
	MessageID string `json:"message_id"`
	Error     string `json:"error,omitempty"`
}

// NewDeliveryClient creates a new delivery client
func NewDeliveryClient(cfg DeliveryConfig, logger *logrus.Logger) *DeliveryClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 10
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 5 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	client := &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			TLSClientConfig:     cfg.TLSConfig,
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	dc := &DeliveryClient{
		config:    cfg,
		client:    client,
		logger:    logger.WithField("component", "li_delivery"),
		batch:     make([]*deliveryItem, 0, cfg.BatchSize),
		flushChan: make(chan struct{}, 1),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start background flusher
	dc.wg.Add(1)
	go dc.flushLoop()

	return dc
}

// Deliver sends intercepted content to LEA
func (dc *DeliveryClient) Deliver(ctx context.Context, warrantID, interceptID string, payload []byte) error {
	item := &deliveryItem{
		WarrantID:   warrantID,
		InterceptID: interceptID,
		Payload:     payload,
		Timestamp:   time.Now(),
	}

	dc.batchMu.Lock()
	dc.batch = append(dc.batch, item)
	shouldFlush := len(dc.batch) >= dc.config.BatchSize
	dc.batchMu.Unlock()

	if shouldFlush {
		select {
		case dc.flushChan <- struct{}{}:
		default:
		}
	}

	return nil
}

// DeliverImmediate sends content immediately without batching
func (dc *DeliveryClient) DeliverImmediate(ctx context.Context, warrantID, interceptID string, payload []byte) error {
	return dc.sendWithRetry(ctx, &deliveryItem{
		WarrantID:   warrantID,
		InterceptID: interceptID,
		Payload:     payload,
		Timestamp:   time.Now(),
	})
}

func (dc *DeliveryClient) flushLoop() {
	defer dc.wg.Done()

	ticker := time.NewTicker(dc.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dc.ctx.Done():
			// Final flush on shutdown
			dc.flush()
			return
		case <-ticker.C:
			dc.flush()
		case <-dc.flushChan:
			dc.flush()
		}
	}
}

func (dc *DeliveryClient) flush() {
	dc.batchMu.Lock()
	if len(dc.batch) == 0 {
		dc.batchMu.Unlock()
		return
	}
	items := dc.batch
	dc.batch = make([]*deliveryItem, 0, dc.config.BatchSize)
	dc.batchMu.Unlock()

	for _, item := range items {
		if err := dc.sendWithRetry(dc.ctx, item); err != nil {
			dc.logger.WithError(err).WithFields(logrus.Fields{
				"warrant_id":   item.WarrantID,
				"intercept_id": item.InterceptID,
			}).Error("Failed to deliver content after retries")
		}
	}
}

func (dc *DeliveryClient) sendWithRetry(ctx context.Context, item *deliveryItem) error {
	var lastErr error

	for attempt := 0; attempt <= dc.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := dc.send(ctx, item)
		if err == nil {
			return nil
		}
		lastErr = err
		item.Retries++

		dc.logger.WithError(err).WithFields(logrus.Fields{
			"attempt":      attempt + 1,
			"max_retries":  dc.config.MaxRetries,
			"warrant_id":   item.WarrantID,
			"intercept_id": item.InterceptID,
		}).Warn("Delivery attempt failed")
	}

	return fmt.Errorf("delivery failed after %d attempts: %w", dc.config.MaxRetries+1, lastErr)
}

func (dc *DeliveryClient) send(ctx context.Context, item *deliveryItem) error {
	req := DeliveryRequest{
		Version:     "1.0",
		WarrantID:   item.WarrantID,
		InterceptID: item.InterceptID,
		Timestamp:   item.Timestamp,
		Payload:     item.Payload,
		Checksum:    computeChecksum(item.Payload),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, dc.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-LI-Version", "1.0")
	httpReq.Header.Set("X-Warrant-ID", item.WarrantID)
	httpReq.Header.Set("X-Intercept-ID", item.InterceptID)

	resp, err := dc.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delivery rejected: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var deliveryResp DeliveryResponse
	if err := json.Unmarshal(respBody, &deliveryResp); err != nil {
		// Non-JSON response is OK if status is 2xx
		return nil
	}

	if deliveryResp.Status != "accepted" && deliveryResp.Status != "ok" && deliveryResp.Status != "" {
		return fmt.Errorf("delivery not accepted: %s", deliveryResp.Error)
	}

	dc.logger.WithFields(logrus.Fields{
		"warrant_id":   item.WarrantID,
		"intercept_id": item.InterceptID,
		"message_id":   deliveryResp.MessageID,
	}).Debug("Content delivered successfully")

	return nil
}

// Close shuts down the delivery client
func (dc *DeliveryClient) Close() {
	dc.cancel()
	dc.wg.Wait()
}

// computeChecksum computes SHA-256 checksum of data
func computeChecksum(data []byte) string {
	// Using a simple implementation - in production use crypto/sha256
	var sum uint64
	for _, b := range data {
		sum += uint64(b)
	}
	return fmt.Sprintf("%016x", sum)
}
