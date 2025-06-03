package messaging

import (
	"sync"
	"time"
)

// MemoryMessageStorage provides an in-memory implementation of MessageStorage
// This is suitable for development and testing, but not recommended for production
// as messages will be lost if the service restarts
type MemoryMessageStorage struct {
	messages map[string]*PendingMessage
	mutex    sync.RWMutex
}

// NewMemoryMessageStorage creates a new in-memory message storage
func NewMemoryMessageStorage() *MemoryMessageStorage {
	return &MemoryMessageStorage{
		messages: make(map[string]*PendingMessage),
	}
}

// Store stores a message in memory
func (m *MemoryMessageStorage) Store(msg *PendingMessage) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Create a deep copy to avoid race conditions
	msgCopy := *msg
	if msg.Metadata != nil {
		msgCopy.Metadata = make(map[string]interface{})
		for k, v := range msg.Metadata {
			msgCopy.Metadata[k] = v
		}
	}

	m.messages[msg.ID] = &msgCopy
	return nil
}

// Retrieve retrieves a message by ID
func (m *MemoryMessageStorage) Retrieve(id string) (*PendingMessage, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	msg, exists := m.messages[id]
	if !exists {
		return nil, nil
	}

	// Return a copy to avoid race conditions
	msgCopy := *msg
	if msg.Metadata != nil {
		msgCopy.Metadata = make(map[string]interface{})
		for k, v := range msg.Metadata {
			msgCopy.Metadata[k] = v
		}
	}

	return &msgCopy, nil
}

// List returns a list of pending messages up to the specified limit
func (m *MemoryMessageStorage) List(limit int) ([]*PendingMessage, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var messages []*PendingMessage
	count := 0

	for _, msg := range m.messages {
		if count >= limit {
			break
		}

		// Create a copy to avoid race conditions
		msgCopy := *msg
		if msg.Metadata != nil {
			msgCopy.Metadata = make(map[string]interface{})
			for k, v := range msg.Metadata {
				msgCopy.Metadata[k] = v
			}
		}

		messages = append(messages, &msgCopy)
		count++
	}

	return messages, nil
}

// Delete removes a message by ID
func (m *MemoryMessageStorage) Delete(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.messages, id)
	return nil
}

// DeleteBatch removes multiple messages by their IDs
func (m *MemoryMessageStorage) DeleteBatch(ids []string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, id := range ids {
		delete(m.messages, id)
	}

	return nil
}

// Count returns the total number of stored messages
func (m *MemoryMessageStorage) Count() (int, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return len(m.messages), nil
}

// CleanupExpired removes messages older than the specified cutoff time
func (m *MemoryMessageStorage) CleanupExpired(cutoff time.Time) (int, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	var expiredIDs []string

	for id, msg := range m.messages {
		if msg.CreatedAt.Before(cutoff) {
			expiredIDs = append(expiredIDs, id)
		}
	}

	// Remove expired messages
	for _, id := range expiredIDs {
		delete(m.messages, id)
	}

	return len(expiredIDs), nil
}

// GetMessagesByCallUUID returns all messages for a specific call UUID (utility method)
func (m *MemoryMessageStorage) GetMessagesByCallUUID(callUUID string) ([]*PendingMessage, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var messages []*PendingMessage

	for _, msg := range m.messages {
		if msg.CallUUID == callUUID {
			// Create a copy to avoid race conditions
			msgCopy := *msg
			if msg.Metadata != nil {
				msgCopy.Metadata = make(map[string]interface{})
				for k, v := range msg.Metadata {
					msgCopy.Metadata[k] = v
				}
			}
			messages = append(messages, &msgCopy)
		}
	}

	return messages, nil
}

// Clear removes all messages from storage (utility method for testing)
func (m *MemoryMessageStorage) Clear() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.messages = make(map[string]*PendingMessage)
	return nil
}
