package compliance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// AuditChainWriter appends tamper-evident audit entries to a log file.
type AuditChainWriter struct {
	path     string
	mutex    sync.Mutex
	lastHash string
}

// NewAuditChainWriter creates a new writer for the given path.
func NewAuditChainWriter(path string) *AuditChainWriter {
	return &AuditChainWriter{path: path}
}

// AuditRecord is the persisted representation of an audit event with chain hash.
type AuditRecord struct {
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
	PrevHash  string                 `json:"prev_hash"`
	Hash      string                 `json:"hash"`
}

// Append writes a new audit record and updates the chain.
func (w *AuditChainWriter) Append(payload map[string]interface{}) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if payload == nil {
		payload = make(map[string]interface{})
	}

	record := AuditRecord{
		Timestamp: time.Now().UTC(),
		Payload:   payload,
		PrevHash:  w.lastHash,
	}

	if err := record.computeHash(); err != nil {
		return err
	}

	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0640)
	if err != nil {
		return fmt.Errorf("failed to open audit chain file: %w", err)
	}
	defer file.Close()

	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal audit record: %w", err)
	}

	if _, err := file.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("failed to write audit record: %w", err)
	}

	w.lastHash = record.Hash
	return nil
}

func (r *AuditRecord) computeHash() error {
	raw, err := json.Marshal(struct {
		Timestamp time.Time              `json:"timestamp"`
		Payload   map[string]interface{} `json:"payload"`
		PrevHash  string                 `json:"prev_hash"`
	}{
		Timestamp: r.Timestamp,
		Payload:   r.Payload,
		PrevHash:  r.PrevHash,
	})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	r.Hash = hex.EncodeToString(hash[:])
	return nil
}
