package stt

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// Number of shards for the conversation map - use power of 2 for fast modulo
	numShards = 256
	// Default max idle time before cleanup
	defaultMaxIdleTime = 5 * time.Minute
	// Cleanup interval
	cleanupInterval = 30 * time.Second
	// Pre-allocated segment capacity per conversation
	initialSegmentCapacity = 100
)

// ConversationSegment represents a single transcription segment in a conversation
type ConversationSegment struct {
	Timestamp     time.Time              `json:"timestamp"`
	Transcription string                 `json:"transcription"`
	IsFinal       bool                   `json:"is_final"`
	Speaker       string                 `json:"speaker,omitempty"`
	Confidence    float64                `json:"confidence,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// ConversationRecord represents the complete conversation for a call
type ConversationRecord struct {
	CallUUID     string                 `json:"call_uuid"`
	StartTime    time.Time              `json:"start_time"`
	EndTime      time.Time              `json:"end_time,omitempty"`
	Duration     time.Duration          `json:"duration,omitempty"`
	Segments     []ConversationSegment  `json:"segments"`
	FinalText    string                 `json:"final_text"`
	WordCount    int                    `json:"word_count"`
	SegmentCount int                    `json:"segment_count"`
	Providers    []string               `json:"providers,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	lastActivity time.Time              // Internal tracking, not exported
	mutex        sync.Mutex             // Per-record mutex for fine-grained locking
}

// ConversationEndCallback is called when a conversation ends
type ConversationEndCallback func(record *ConversationRecord)

// conversationShard represents a single shard of conversations
type conversationShard struct {
	conversations map[string]*ConversationRecord
	mutex         sync.RWMutex
}

// ConversationAccumulator accumulates transcription segments for complete conversations
// Optimized for high concurrency with sharded maps
type ConversationAccumulator struct {
	logger        *logrus.Logger
	shards        [numShards]*conversationShard
	endCallbacks  []ConversationEndCallback
	callbackMutex sync.RWMutex

	// Configuration
	maxIdleTime   time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}

	// Metrics - use atomic for lock-free updates
	totalSegments      int64
	totalConversations int64
	activeConversations int64

	// Object pool for segments to reduce GC pressure
	segmentPool sync.Pool
}

// NewConversationAccumulator creates a new conversation accumulator optimized for high concurrency
func NewConversationAccumulator(logger *logrus.Logger) *ConversationAccumulator {
	ca := &ConversationAccumulator{
		logger:       logger,
		endCallbacks: make([]ConversationEndCallback, 0, 10),
		maxIdleTime:  defaultMaxIdleTime,
		stopCleanup:  make(chan struct{}),
		segmentPool: sync.Pool{
			New: func() interface{} {
				return &ConversationSegment{
					Metadata: make(map[string]interface{}, 8),
				}
			},
		},
	}

	// Initialize shards
	for i := 0; i < numShards; i++ {
		ca.shards[i] = &conversationShard{
			conversations: make(map[string]*ConversationRecord, 100), // Pre-allocate
		}
	}

	// Start cleanup routine
	ca.cleanupTicker = time.NewTicker(cleanupInterval)
	go ca.cleanupRoutine()

	logger.WithField("shards", numShards).Info("Conversation accumulator initialized with sharded map")
	return ca
}

// getShard returns the shard for a given call UUID using FNV-1a hash
func (ca *ConversationAccumulator) getShard(callUUID string) *conversationShard {
	h := fnvHash(callUUID)
	return ca.shards[h%numShards]
}

// fnvHash computes FNV-1a hash for string - fast and good distribution
func fnvHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// OnTranscription implements TranscriptionListener interface
// This receives live transcriptions and accumulates them
func (ca *ConversationAccumulator) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if transcription == "" {
		return
	}

	// Get the shard for this call
	shard := ca.getShard(callUUID)

	// Try to get existing record with read lock first (fast path)
	shard.mutex.RLock()
	record, exists := shard.conversations[callUUID]
	shard.mutex.RUnlock()

	if !exists {
		// Need to create - use write lock
		shard.mutex.Lock()
		// Double-check after acquiring write lock
		record, exists = shard.conversations[callUUID]
		if !exists {
			record = &ConversationRecord{
				CallUUID:     callUUID,
				StartTime:    time.Now(),
				Segments:     make([]ConversationSegment, 0, initialSegmentCapacity),
				Metadata:     make(map[string]interface{}, 8),
				Providers:    make([]string, 0, 4),
				lastActivity: time.Now(),
			}
			shard.conversations[callUUID] = record
			atomic.AddInt64(&ca.activeConversations, 1)
			atomic.AddInt64(&ca.totalConversations, 1)
		}
		shard.mutex.Unlock()
	}

	// Now update the record with per-record lock (fine-grained)
	record.mutex.Lock()
	defer record.mutex.Unlock()

	// Extract metadata fields
	var confidence float64
	var provider, speaker string

	if metadata != nil {
		if conf, ok := metadata["confidence"].(float64); ok {
			confidence = conf
		}
		if prov, ok := metadata["provider"].(string); ok {
			provider = prov
			// Track unique providers (check inline to avoid allocation)
			found := false
			for _, p := range record.Providers {
				if p == provider {
					found = true
					break
				}
			}
			if !found {
				record.Providers = append(record.Providers, provider)
			}
		}
		if spk, ok := metadata["speaker"].(string); ok {
			speaker = spk
		} else if spkInt, ok := metadata["speaker"].(int); ok {
			speaker = string(rune('A' + spkInt))
		}
	}

	// Create segment directly (avoid pool overhead for simple struct)
	segment := ConversationSegment{
		Timestamp:     time.Now(),
		Transcription: transcription,
		IsFinal:       isFinal,
		Speaker:       speaker,
		Confidence:    confidence,
		Provider:      provider,
	}
	// Only copy metadata if needed
	if metadata != nil && len(metadata) > 0 {
		segment.Metadata = metadata
	}

	// Add segment to conversation
	record.Segments = append(record.Segments, segment)
	record.SegmentCount = len(record.Segments)
	record.lastActivity = time.Now()

	// Update final text with only final transcriptions
	if isFinal {
		if record.FinalText != "" {
			record.FinalText += " "
		}
		record.FinalText += transcription
		record.WordCount = countWords(record.FinalText)
	}

	atomic.AddInt64(&ca.totalSegments, 1)
}

// EndConversation marks a conversation as complete and triggers callbacks
func (ca *ConversationAccumulator) EndConversation(callUUID string) *ConversationRecord {
	shard := ca.getShard(callUUID)

	shard.mutex.Lock()
	record, exists := shard.conversations[callUUID]
	if exists {
		delete(shard.conversations, callUUID)
	}
	shard.mutex.Unlock()

	if !exists {
		return nil
	}

	atomic.AddInt64(&ca.activeConversations, -1)

	// Finalize record
	record.mutex.Lock()
	record.EndTime = time.Now()
	record.Duration = record.EndTime.Sub(record.StartTime)
	record.mutex.Unlock()

	ca.logger.WithFields(logrus.Fields{
		"call_uuid":     callUUID,
		"duration":      record.Duration,
		"segment_count": record.SegmentCount,
		"word_count":    record.WordCount,
	}).Info("Conversation ended")

	// Trigger callbacks asynchronously to not block
	ca.triggerCallbacks(record)

	return record
}

// triggerCallbacks triggers all end callbacks asynchronously
func (ca *ConversationAccumulator) triggerCallbacks(record *ConversationRecord) {
	ca.callbackMutex.RLock()
	callbacks := make([]ConversationEndCallback, len(ca.endCallbacks))
	copy(callbacks, ca.endCallbacks)
	ca.callbackMutex.RUnlock()

	for _, callback := range callbacks {
		go func(cb ConversationEndCallback, rec *ConversationRecord) {
			defer func() {
				if r := recover(); r != nil {
					ca.logger.WithFields(logrus.Fields{
						"call_uuid": rec.CallUUID,
						"panic":     r,
					}).Error("Recovered from panic in conversation end callback")
				}
			}()
			cb(rec)
		}(callback, record)
	}
}

// AddEndCallback registers a callback for when conversations end
func (ca *ConversationAccumulator) AddEndCallback(callback ConversationEndCallback) {
	ca.callbackMutex.Lock()
	defer ca.callbackMutex.Unlock()
	ca.endCallbacks = append(ca.endCallbacks, callback)
}

// GetConversation returns the current conversation record for a call
func (ca *ConversationAccumulator) GetConversation(callUUID string) *ConversationRecord {
	shard := ca.getShard(callUUID)
	shard.mutex.RLock()
	record := shard.conversations[callUUID]
	shard.mutex.RUnlock()
	return record
}

// GetConversationJSON returns the conversation as JSON
func (ca *ConversationAccumulator) GetConversationJSON(callUUID string) ([]byte, error) {
	record := ca.GetConversation(callUUID)
	if record == nil {
		return nil, nil
	}
	record.mutex.Lock()
	defer record.mutex.Unlock()
	return json.Marshal(record)
}

// cleanupRoutine periodically cleans up old conversations
func (ca *ConversationAccumulator) cleanupRoutine() {
	for {
		select {
		case <-ca.stopCleanup:
			return
		case <-ca.cleanupTicker.C:
			ca.cleanupIdleConversations()
		}
	}
}

// cleanupIdleConversations removes conversations that have been idle too long
func (ca *ConversationAccumulator) cleanupIdleConversations() {
	now := time.Now()
	var cleanedCount int

	for i := 0; i < numShards; i++ {
		shard := ca.shards[i]
		var toCleanup []string

		// Find idle conversations with read lock
		shard.mutex.RLock()
		for callUUID, record := range shard.conversations {
			record.mutex.Lock()
			if now.Sub(record.lastActivity) > ca.maxIdleTime {
				toCleanup = append(toCleanup, callUUID)
			}
			record.mutex.Unlock()
		}
		shard.mutex.RUnlock()

		// Clean up with write lock
		if len(toCleanup) > 0 {
			shard.mutex.Lock()
			for _, callUUID := range toCleanup {
				if record, exists := shard.conversations[callUUID]; exists {
					record.EndTime = now
					record.Duration = record.EndTime.Sub(record.StartTime)
					ca.triggerCallbacks(record)
					delete(shard.conversations, callUUID)
					atomic.AddInt64(&ca.activeConversations, -1)
					cleanedCount++
				}
			}
			shard.mutex.Unlock()
		}
	}

	if cleanedCount > 0 {
		ca.logger.WithField("count", cleanedCount).Info("Cleaned up idle conversations")
	}
}

// Shutdown stops the accumulator and ends all active conversations
func (ca *ConversationAccumulator) Shutdown() {
	close(ca.stopCleanup)
	ca.cleanupTicker.Stop()

	// End all active conversations across all shards
	for i := 0; i < numShards; i++ {
		shard := ca.shards[i]
		shard.mutex.Lock()
		for callUUID, record := range shard.conversations {
			record.EndTime = time.Now()
			record.Duration = record.EndTime.Sub(record.StartTime)
			ca.triggerCallbacks(record)
			delete(shard.conversations, callUUID)
		}
		shard.mutex.Unlock()
	}

	ca.logger.Info("Conversation accumulator shutdown complete")
}

// GetActiveCount returns the number of active conversations
func (ca *ConversationAccumulator) GetActiveCount() int {
	return int(atomic.LoadInt64(&ca.activeConversations))
}

// GetMetrics returns accumulator metrics
func (ca *ConversationAccumulator) GetMetrics() (active, total, segments int64) {
	return atomic.LoadInt64(&ca.activeConversations),
		atomic.LoadInt64(&ca.totalConversations),
		atomic.LoadInt64(&ca.totalSegments)
}

// SetSessionMetadata sets session metadata for a conversation
// This allows external callers (like SIP handler) to attach session-level
// metadata (Oracle UCID, Conversation ID, vendor headers, etc.) to conversations
func (ca *ConversationAccumulator) SetSessionMetadata(callUUID string, metadata map[string]string) {
	if callUUID == "" || metadata == nil || len(metadata) == 0 {
		return
	}

	shard := ca.getShard(callUUID)

	// First try with read lock
	shard.mutex.RLock()
	record, exists := shard.conversations[callUUID]
	shard.mutex.RUnlock()

	if !exists {
		// Create the conversation record if it doesn't exist yet
		shard.mutex.Lock()
		record, exists = shard.conversations[callUUID]
		if !exists {
			record = &ConversationRecord{
				CallUUID:     callUUID,
				StartTime:    time.Now(),
				Segments:     make([]ConversationSegment, 0, initialSegmentCapacity),
				Metadata:     make(map[string]interface{}, len(metadata)+8),
				Providers:    make([]string, 0, 4),
				lastActivity: time.Now(),
			}
			shard.conversations[callUUID] = record
			atomic.AddInt64(&ca.activeConversations, 1)
			atomic.AddInt64(&ca.totalConversations, 1)
		}
		shard.mutex.Unlock()
	}

	// Set the metadata
	record.mutex.Lock()
	if record.Metadata == nil {
		record.Metadata = make(map[string]interface{}, len(metadata)+8)
	}
	for key, value := range metadata {
		record.Metadata[key] = value
	}
	record.lastActivity = time.Now()
	record.mutex.Unlock()

	ca.logger.WithFields(logrus.Fields{
		"call_uuid":      callUUID,
		"metadata_count": len(metadata),
	}).Debug("Session metadata set for conversation")
}

// countWords counts words in a string efficiently
func countWords(text string) int {
	if text == "" {
		return 0
	}
	count := 0
	inWord := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			inWord = false
		} else if !inWord {
			inWord = true
			count++
		}
	}
	return count
}
