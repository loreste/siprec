package stt

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// Worker pool size for async publishing
	transcriptionWorkerPoolSize = 64
	// Channel buffer size for non-blocking publishing
	transcriptionChannelSize = 10000
)

// TranscriptionListener represents something that can listen for transcription updates
type TranscriptionListener interface {
	// OnTranscription is called when a new transcription is available
	OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{})
}

// SessionMetadataSetter is an optional interface for listeners that can receive session metadata
// (e.g., Oracle UCID, Conversation ID, vendor headers) to attach to conversations
type SessionMetadataSetter interface {
	SetSessionMetadata(callUUID string, metadata map[string]string)
}

// transcriptionEvent represents an event to be published
type transcriptionEvent struct {
	callUUID      string
	transcription string
	isFinal       bool
	metadata      map[string]interface{}
}

// TranscriptionService manages transcription results and notifies listeners
// Optimized for high concurrency with worker pools and buffered channels
type TranscriptionService struct {
	logger    *logrus.Logger
	listeners []TranscriptionListener
	mutex     sync.RWMutex

	// Async publishing with worker pool
	eventChan   chan *transcriptionEvent
	stopChan    chan struct{}
	wg          sync.WaitGroup

	// Object pool for events to reduce GC pressure
	eventPool sync.Pool

	// Session metadata storage - injected into each transcription event
	// Keys: Oracle UCID, Conversation ID, vendor type, etc.
	sessionMetadata      map[string]map[string]string
	sessionMetadataMutex sync.RWMutex

	// Metrics - atomic for lock-free updates
	totalPublished   int64
	totalDropped     int64
	channelHighWater int64
}

// NewTranscriptionService creates a new transcription service optimized for high concurrency
func NewTranscriptionService(logger *logrus.Logger) *TranscriptionService {
	s := &TranscriptionService{
		logger:          logger,
		listeners:       make([]TranscriptionListener, 0, 16),
		eventChan:       make(chan *transcriptionEvent, transcriptionChannelSize),
		stopChan:        make(chan struct{}),
		sessionMetadata: make(map[string]map[string]string),
		eventPool: sync.Pool{
			New: func() interface{} {
				return &transcriptionEvent{
					metadata: make(map[string]interface{}, 8),
				}
			},
		},
	}

	// Start worker pool
	for i := 0; i < transcriptionWorkerPoolSize; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}

	logger.WithFields(logrus.Fields{
		"workers":     transcriptionWorkerPoolSize,
		"buffer_size": transcriptionChannelSize,
	}).Info("Transcription service initialized with worker pool")

	return s
}

// worker processes transcription events
func (s *TranscriptionService) worker(id int) {
	defer s.wg.Done()

	for {
		select {
		case <-s.stopChan:
			return
		case event := <-s.eventChan:
			if event != nil {
				s.processEvent(event)
				// Return event to pool
				event.callUUID = ""
				event.transcription = ""
				event.isFinal = false
				// Clear metadata map (reuse capacity)
				for k := range event.metadata {
					delete(event.metadata, k)
				}
				s.eventPool.Put(event)
			}
		}
	}
}

// processEvent handles a single transcription event
func (s *TranscriptionService) processEvent(event *transcriptionEvent) {
	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			s.logger.WithFields(logrus.Fields{
				"call_uuid": event.callUUID,
				"panic":     r,
			}).Error("Recovered from panic in transcription worker")
		}
	}()

	// Get session metadata to inject (Oracle UCID, Conversation ID, vendor info, etc.)
	sessionMeta := s.getSessionMetadata(event.callUUID)

	s.mutex.RLock()
	listeners := make([]TranscriptionListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mutex.RUnlock()

	for _, listener := range listeners {
		// Create a copy of metadata for each listener to prevent race conditions
		var metadataCopy map[string]interface{}
		metaSize := len(event.metadata) + len(sessionMeta)
		if metaSize > 0 {
			metadataCopy = make(map[string]interface{}, metaSize)
			// First, add session metadata (Oracle UCID, Conversation ID, etc.)
			for k, v := range sessionMeta {
				metadataCopy[k] = v
			}
			// Then, add event-specific metadata (may override session metadata)
			for k, v := range event.metadata {
				metadataCopy[k] = v
			}
		}
		listener.OnTranscription(event.callUUID, event.transcription, event.isFinal, metadataCopy)
	}

	atomic.AddInt64(&s.totalPublished, 1)
}

// AddListener registers a new transcription listener
func (s *TranscriptionService) AddListener(listener TranscriptionListener) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.listeners = append(s.listeners, listener)
	s.logger.Info("Added new transcription listener")
}

// RemoveListener removes a transcription listener
func (s *TranscriptionService) RemoveListener(listener TranscriptionListener) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for i, l := range s.listeners {
		if l == listener {
			// Remove listener by replacing with last element and truncating
			s.listeners[i] = s.listeners[len(s.listeners)-1]
			s.listeners = s.listeners[:len(s.listeners)-1]
			s.logger.Info("Removed transcription listener")
			return
		}
	}
}

// SetSessionMetadata propagates session metadata to all listeners that support it
// and stores it locally for injection into transcription events.
// This allows session-level metadata (Oracle UCID, Conversation ID, etc.) to be
// attached to both conversation records and individual transcription events.
func (s *TranscriptionService) SetSessionMetadata(callUUID string, metadata map[string]string) {
	if callUUID == "" || metadata == nil || len(metadata) == 0 {
		return
	}

	// Store a copy locally to prevent race conditions with caller
	metaCopy := make(map[string]string, len(metadata))
	for k, v := range metadata {
		metaCopy[k] = v
	}

	s.sessionMetadataMutex.Lock()
	s.sessionMetadata[callUUID] = metaCopy
	s.sessionMetadataMutex.Unlock()

	// Propagate to listeners that support it (e.g., ConversationAccumulator)
	s.mutex.RLock()
	listeners := make([]TranscriptionListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mutex.RUnlock()

	for _, listener := range listeners {
		if setter, ok := listener.(SessionMetadataSetter); ok {
			setter.SetSessionMetadata(callUUID, metadata)
		}
	}

	s.logger.WithFields(logrus.Fields{
		"call_uuid":      callUUID,
		"metadata_count": len(metadata),
	}).Debug("Session metadata stored and propagated to listeners")
}

// ClearSessionMetadata removes stored session metadata for a call (call this on call end)
func (s *TranscriptionService) ClearSessionMetadata(callUUID string) {
	if callUUID == "" {
		return
	}
	s.sessionMetadataMutex.Lock()
	delete(s.sessionMetadata, callUUID)
	s.sessionMetadataMutex.Unlock()
}

// getSessionMetadata retrieves stored session metadata for a call
// Returns a copy of the metadata to prevent race conditions
func (s *TranscriptionService) getSessionMetadata(callUUID string) map[string]string {
	s.sessionMetadataMutex.RLock()
	defer s.sessionMetadataMutex.RUnlock()

	original := s.sessionMetadata[callUUID]
	if original == nil {
		return nil
	}

	// Return a copy to prevent concurrent modification
	metaCopy := make(map[string]string, len(original))
	for k, v := range original {
		metaCopy[k] = v
	}
	return metaCopy
}

// PublishTranscription notifies all listeners about a new transcription
// This method is non-blocking and queues the event for async processing
func (s *TranscriptionService) PublishTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if transcription == "" {
		return // Don't publish empty transcriptions
	}

	// Get event from pool
	event := s.eventPool.Get().(*transcriptionEvent)
	event.callUUID = callUUID
	event.transcription = transcription
	event.isFinal = isFinal

	// Copy metadata into pooled event's map
	for k, v := range metadata {
		event.metadata[k] = v
	}

	// Non-blocking send with backpressure handling
	select {
	case s.eventChan <- event:
		// Track high water mark
		currentLen := int64(len(s.eventChan))
		for {
			highWater := atomic.LoadInt64(&s.channelHighWater)
			if currentLen <= highWater {
				break
			}
			if atomic.CompareAndSwapInt64(&s.channelHighWater, highWater, currentLen) {
				break
			}
		}
	default:
		// Channel full - drop oldest (backpressure)
		atomic.AddInt64(&s.totalDropped, 1)
		// Return event to pool since we're dropping it
		for k := range event.metadata {
			delete(event.metadata, k)
		}
		s.eventPool.Put(event)
		s.logger.WithFields(logrus.Fields{
			"call_uuid":     callUUID,
			"queue_size":    len(s.eventChan),
			"total_dropped": atomic.LoadInt64(&s.totalDropped),
		}).Warn("Transcription event dropped due to backpressure")
	}
}

// PublishTranscriptionSync synchronously notifies all listeners (for critical events)
func (s *TranscriptionService) PublishTranscriptionSync(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	if transcription == "" {
		return
	}

	s.mutex.RLock()
	listeners := make([]TranscriptionListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mutex.RUnlock()

	for _, listener := range listeners {
		var metadataCopy map[string]interface{}
		if metadata != nil {
			metadataCopy = make(map[string]interface{}, len(metadata))
			for k, v := range metadata {
				metadataCopy[k] = v
			}
		}
		listener.OnTranscription(callUUID, transcription, isFinal, metadataCopy)
	}

	atomic.AddInt64(&s.totalPublished, 1)
}

// GetMetrics returns service metrics
func (s *TranscriptionService) GetMetrics() (published, dropped, highWater int64) {
	return atomic.LoadInt64(&s.totalPublished),
		atomic.LoadInt64(&s.totalDropped),
		atomic.LoadInt64(&s.channelHighWater)
}

// GetQueueLength returns current queue length
func (s *TranscriptionService) GetQueueLength() int {
	return len(s.eventChan)
}

// Shutdown gracefully shuts down the service
func (s *TranscriptionService) Shutdown() {
	close(s.stopChan)
	s.wg.Wait()
	s.logger.Info("Transcription service shutdown complete")
}

// WebSocketHub represents a WebSocket hub that can broadcast transcriptions
type WebSocketHub interface {
	BroadcastTranscription(message interface{})
}

// WebSocketTranscriptionBridge bridges the TranscriptionService to a WebSocket hub
type WebSocketTranscriptionBridge struct {
	logger *logrus.Logger
	hub    interface{}
}

// NewWebSocketTranscriptionBridge creates a new bridge between the transcription service and WebSocket hub
func NewWebSocketTranscriptionBridge(logger *logrus.Logger, hub interface{}) *WebSocketTranscriptionBridge {
	return &WebSocketTranscriptionBridge{
		logger: logger,
		hub:    hub,
	}
}

// OnTranscription implements the TranscriptionListener interface
func (b *WebSocketTranscriptionBridge) OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Create message
	message := struct {
		CallUUID      string                 `json:"call_uuid"`
		Transcription string                 `json:"transcription"`
		IsFinal       bool                   `json:"is_final"`
		Timestamp     time.Time              `json:"timestamp"`
		Metadata      map[string]interface{} `json:"metadata,omitempty"`
	}{
		CallUUID:      callUUID,
		Transcription: transcription,
		IsFinal:       isFinal,
		Timestamp:     time.Now(),
		Metadata:      metadata,
	}

	// Broadcast to WebSocket clients using reflection
	if hub, ok := b.hub.(interface{ BroadcastTranscription(message interface{}) }); ok {
		hub.BroadcastTranscription(message)
	} else if hub, ok := b.hub.(interface {
		BroadcastTranscription(message *struct {
			CallUUID      string                 `json:"call_uuid"`
			Transcription string                 `json:"transcription"`
			IsFinal       bool                   `json:"is_final"`
			Timestamp     time.Time              `json:"timestamp"`
			Metadata      map[string]interface{} `json:"metadata,omitempty"`
		})
	}); ok {
		hub.BroadcastTranscription(&message)
	} else {
		b.logger.Error("WebSocket hub does not implement expected BroadcastTranscription method")
	}
}
