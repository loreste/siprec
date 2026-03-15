package lawfulintercept

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// AuditEventType represents the type of audit event
type AuditEventType string

const (
	AuditEventSystemStart      AuditEventType = "system_start"
	AuditEventSystemStop       AuditEventType = "system_stop"
	AuditEventInterceptStarted AuditEventType = "intercept_started"
	AuditEventInterceptRevoked AuditEventType = "intercept_revoked"
	AuditEventInterceptExpired AuditEventType = "intercept_expired"
	AuditEventContentDelivered AuditEventType = "content_delivered"
	AuditEventDeliveryFailed   AuditEventType = "delivery_failed"
	AuditEventWarrantVerified  AuditEventType = "warrant_verified"
	AuditEventWarrantFailed    AuditEventType = "warrant_failed"
	AuditEventWarrantInvalid   AuditEventType = "warrant_invalid"
	AuditEventAccessDenied     AuditEventType = "access_denied"
	AuditEventConfigChanged    AuditEventType = "config_changed"
)

// AuditEntry represents a single audit log entry
type AuditEntry struct {
	Timestamp   time.Time              `json:"timestamp"`
	EventType   AuditEventType         `json:"event_type"`
	WarrantID   string                 `json:"warrant_id,omitempty"`
	InterceptID string                 `json:"intercept_id,omitempty"`
	Description string                 `json:"description"`
	Details     map[string]interface{} `json:"details,omitempty"`
	NodeID      string                 `json:"node_id"`
	Sequence    uint64                 `json:"sequence"`
}

// AuditLogger handles tamper-evident audit logging
type AuditLogger struct {
	path     string
	file     *os.File
	logger   *logrus.Entry
	nodeID   string
	sequence uint64
	mutex    sync.Mutex

	// Write buffer for batching
	buffer    []AuditEntry
	bufferMu  sync.Mutex
	flushChan chan struct{}
	closeChan chan struct{}
	wg        sync.WaitGroup
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(path string, logger *logrus.Logger) (*AuditLogger, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create audit log directory: %w", err)
	}

	// Open file in append mode
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit log file: %w", err)
	}

	hostname, _ := os.Hostname()

	al := &AuditLogger{
		path:      path,
		file:      file,
		logger:    logger.WithField("component", "li_audit"),
		nodeID:    hostname,
		buffer:    make([]AuditEntry, 0, 100),
		flushChan: make(chan struct{}, 1),
		closeChan: make(chan struct{}),
	}

	// Start background flusher
	al.wg.Add(1)
	go al.flushLoop()

	return al, nil
}

// Log writes an audit entry
func (al *AuditLogger) Log(eventType AuditEventType, warrantID, description string, details map[string]interface{}) {
	al.mutex.Lock()
	al.sequence++
	seq := al.sequence
	al.mutex.Unlock()

	entry := AuditEntry{
		Timestamp:   time.Now().UTC(),
		EventType:   eventType,
		WarrantID:   warrantID,
		Description: description,
		Details:     details,
		NodeID:      al.nodeID,
		Sequence:    seq,
	}

	al.bufferMu.Lock()
	al.buffer = append(al.buffer, entry)
	shouldFlush := len(al.buffer) >= 50
	al.bufferMu.Unlock()

	if shouldFlush {
		select {
		case al.flushChan <- struct{}{}:
		default:
		}
	}
}

// LogImmediate writes an audit entry immediately (synchronous)
func (al *AuditLogger) LogImmediate(eventType AuditEventType, warrantID, description string, details map[string]interface{}) error {
	al.mutex.Lock()
	al.sequence++
	seq := al.sequence
	al.mutex.Unlock()

	entry := AuditEntry{
		Timestamp:   time.Now().UTC(),
		EventType:   eventType,
		WarrantID:   warrantID,
		Description: description,
		Details:     details,
		NodeID:      al.nodeID,
		Sequence:    seq,
	}

	return al.writeEntry(entry)
}

func (al *AuditLogger) flushLoop() {
	defer al.wg.Done()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-al.closeChan:
			al.flush()
			return
		case <-ticker.C:
			al.flush()
		case <-al.flushChan:
			al.flush()
		}
	}
}

func (al *AuditLogger) flush() {
	al.bufferMu.Lock()
	if len(al.buffer) == 0 {
		al.bufferMu.Unlock()
		return
	}
	entries := al.buffer
	al.buffer = make([]AuditEntry, 0, 100)
	al.bufferMu.Unlock()

	for _, entry := range entries {
		if err := al.writeEntry(entry); err != nil {
			al.logger.WithError(err).Error("Failed to write audit entry")
		}
	}
}

func (al *AuditLogger) writeEntry(entry AuditEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal audit entry: %w", err)
	}

	al.mutex.Lock()
	defer al.mutex.Unlock()

	// Write entry with newline
	if _, err := al.file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write audit entry: %w", err)
	}

	// Sync to ensure durability
	if err := al.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync audit log: %w", err)
	}

	return nil
}

// Rotate rotates the audit log file
func (al *AuditLogger) Rotate() error {
	al.mutex.Lock()
	defer al.mutex.Unlock()

	// Close current file
	if err := al.file.Close(); err != nil {
		return fmt.Errorf("failed to close current audit log: %w", err)
	}

	// Rename current file with timestamp
	timestamp := time.Now().Format("20060102-150405")
	rotatedPath := fmt.Sprintf("%s.%s", al.path, timestamp)
	if err := os.Rename(al.path, rotatedPath); err != nil {
		return fmt.Errorf("failed to rotate audit log: %w", err)
	}

	// Open new file
	file, err := os.OpenFile(al.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("failed to open new audit log: %w", err)
	}
	al.file = file

	return nil
}

// Close closes the audit logger
func (al *AuditLogger) Close() error {
	close(al.closeChan)
	al.wg.Wait()

	al.mutex.Lock()
	defer al.mutex.Unlock()

	if err := al.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync audit log: %w", err)
	}
	return al.file.Close()
}

// GetPath returns the audit log path
func (al *AuditLogger) GetPath() string {
	return al.path
}
