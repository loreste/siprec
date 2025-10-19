package sip

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"siprec-server/pkg/siprec"

	"github.com/sirupsen/logrus"
)

// MetadataNotifier delivers SIPREC metadata lifecycle events to interested listeners.
type MetadataNotifier struct {
	logger  *logrus.Logger
	client  *http.Client
	global  []string
	timeout time.Duration
	mu      sync.RWMutex
	perCall map[string][]string
}

// NotificationEvent represents a metadata lifecycle event.
type NotificationEvent struct {
	Event     string                 `json:"event"`
	CallID    string                 `json:"call_id"`
	SessionID string                 `json:"session_id,omitempty"`
	State     string                 `json:"state,omitempty"`
	Reason    string                 `json:"reason,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewMetadataNotifier creates a notifier for metadata events.
func NewMetadataNotifier(logger *logrus.Logger, endpoints []string, timeout time.Duration) *MetadataNotifier {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
	}

	cleaned := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		if trimmed := strings.TrimSpace(ep); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return &MetadataNotifier{
		logger:  logger,
		client:  client,
		global:  cleaned,
		timeout: timeout,
		perCall: make(map[string][]string),
	}
}

// RegisterCallEndpoint registers a callback endpoint scoped to a specific call.
func (n *MetadataNotifier) RegisterCallEndpoint(callID, endpoint string) {
	if callID == "" {
		return
	}

	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	n.perCall[callID] = append(n.perCall[callID], trimmed)
}

// ClearCallEndpoints removes per-call endpoints after a dialog is torn down.
func (n *MetadataNotifier) ClearCallEndpoints(callID string) {
	if callID == "" {
		return
	}

	n.mu.Lock()
	delete(n.perCall, callID)
	n.mu.Unlock()
}

// Notify dispatches a metadata event to all registered endpoints.
func (n *MetadataNotifier) Notify(ctx context.Context, session *siprec.RecordingSession, callID, event string, extra map[string]interface{}) {
	endpoints := n.collectEndpoints(callID, session)
	if len(endpoints) == 0 {
		return
	}

	payload := NotificationEvent{
		Event:     event,
		CallID:    callID,
		Timestamp: time.Now().UTC(),
	}

	if session != nil {
		payload.SessionID = session.ID
		payload.State = session.RecordingState
		payload.Reason = session.StateReason
	}

	if extra != nil && len(extra) > 0 {
		payload.Metadata = extra
	}

	body, err := json.Marshal(payload)
	if err != nil {
		n.logger.WithError(err).Error("Failed to marshal metadata notification payload")
		return
	}

	for _, endpoint := range endpoints {
		url := endpoint
		go n.send(ctx, url, body)
	}
}

func (n *MetadataNotifier) send(parentCtx context.Context, endpoint string, body []byte) {
	ctx := parentCtx
	if ctx == nil {
		ctx = context.Background()
	}

	reqCtx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		n.logger.WithError(err).WithField("endpoint", endpoint).Warn("Failed to create notification request")
		return
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		n.logger.WithError(err).WithField("endpoint", endpoint).Warn("Failed to deliver metadata notification")
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		n.logger.WithFields(logrus.Fields{
			"endpoint": endpoint,
			"status":   resp.StatusCode,
		}).Warn("Metadata notification received non-success response")
	}
}

func (n *MetadataNotifier) collectEndpoints(callID string, session *siprec.RecordingSession) []string {
	seen := make(map[string]struct{})
	merged := make([]string, 0, len(n.global)+4)

	for _, endpoint := range n.global {
		if _, ok := seen[endpoint]; !ok {
			seen[endpoint] = struct{}{}
			merged = append(merged, endpoint)
		}
	}

	if session != nil {
		for _, endpoint := range session.Callbacks {
			trimmed := strings.TrimSpace(endpoint)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; !ok {
				seen[trimmed] = struct{}{}
				merged = append(merged, trimmed)
			}
		}
	}

	if callID != "" {
		n.mu.RLock()
		callEndpoints := n.perCall[callID]
		n.mu.RUnlock()

		for _, endpoint := range callEndpoints {
			trimmed := strings.TrimSpace(endpoint)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; !ok {
				seen[trimmed] = struct{}{}
				merged = append(merged, trimmed)
			}
		}
	}

	return merged
}
