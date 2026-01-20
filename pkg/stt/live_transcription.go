package stt

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// LiveTranscriptionManager manages live transcription for all STT providers
// It ensures all transcriptions are published in real-time to registered listeners
// Optimized for high concurrency with atomic counters and object pools
type LiveTranscriptionManager struct {
	logger             *logrus.Logger
	transcriptionSvc   *TranscriptionService
	conversationAccum  *ConversationAccumulator
	providers          map[string]Provider
	providersMutex     sync.RWMutex

	// Metrics - atomic for lock-free updates
	totalTranscriptions int64
	liveTranscriptions  int64
	finalTranscriptions int64

	// Object pool for metadata maps
	metadataPool sync.Pool
}

// NewLiveTranscriptionManager creates a new live transcription manager
func NewLiveTranscriptionManager(logger *logrus.Logger, transcriptionSvc *TranscriptionService) *LiveTranscriptionManager {
	ltm := &LiveTranscriptionManager{
		logger:            logger,
		transcriptionSvc:  transcriptionSvc,
		conversationAccum: NewConversationAccumulator(logger),
		providers:         make(map[string]Provider, 16),
		metadataPool: sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{}, 8)
			},
		},
	}

	// Register conversation accumulator as a listener to track complete conversations
	if transcriptionSvc != nil {
		transcriptionSvc.AddListener(ltm.conversationAccum)
		logger.Info("Conversation accumulator registered with transcription service")
	}

	return ltm
}

// RegisterProvider registers an STT provider and sets up live transcription callback
func (ltm *LiveTranscriptionManager) RegisterProvider(provider Provider) {
	ltm.providersMutex.Lock()
	defer ltm.providersMutex.Unlock()

	name := provider.Name()
	ltm.providers[name] = provider

	// Set callback for providers that support it
	if callbackProvider, ok := provider.(interface {
		SetCallback(func(string, string, bool, map[string]interface{}))
	}); ok {
		callbackProvider.SetCallback(ltm.onTranscription)
		ltm.logger.WithField("provider", name).Info("Live transcription callback registered for provider")
	}

	// Set transcription service for providers that support it
	if svcProvider, ok := provider.(interface {
		SetTranscriptionService(*TranscriptionService)
	}); ok {
		svcProvider.SetTranscriptionService(ltm.transcriptionSvc)
		ltm.logger.WithField("provider", name).Info("Transcription service registered for provider")
	}

	ltm.logger.WithField("provider", name).Info("Provider registered with live transcription manager")
}

// onTranscription is the unified callback for all providers
// This ensures transcriptions are published to the service for AMQP delivery
// Optimized with atomic counters and pooled metadata maps
func (ltm *LiveTranscriptionManager) onTranscription(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			ltm.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"panic":     r,
			}).Error("Recovered from panic in live transcription callback")
		}
	}()

	if transcription == "" {
		return
	}

	// Update metrics atomically (lock-free)
	atomic.AddInt64(&ltm.totalTranscriptions, 1)
	if isFinal {
		atomic.AddInt64(&ltm.finalTranscriptions, 1)
	} else {
		atomic.AddInt64(&ltm.liveTranscriptions, 1)
	}

	// Use pooled metadata map if none provided
	var pooledMeta map[string]interface{}
	if metadata == nil {
		pooledMeta = ltm.metadataPool.Get().(map[string]interface{})
		metadata = pooledMeta
	}

	// Add live transcription metadata
	metadata["live"] = true
	metadata["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)

	// Publish to transcription service (which sends to AMQP and other listeners)
	if ltm.transcriptionSvc != nil {
		ltm.transcriptionSvc.PublishTranscription(callUUID, transcription, isFinal, metadata)
	}

	// Return pooled map if we used one (clear and return)
	if pooledMeta != nil {
		for k := range pooledMeta {
			delete(pooledMeta, k)
		}
		ltm.metadataPool.Put(pooledMeta)
	}

	ltm.logger.WithFields(logrus.Fields{
		"call_uuid": callUUID,
		"is_final":  isFinal,
		"length":    len(transcription),
	}).Debug("Live transcription published")
}

// EndCall ends a call and returns the complete conversation
func (ltm *LiveTranscriptionManager) EndCall(callUUID string) *ConversationRecord {
	return ltm.conversationAccum.EndConversation(callUUID)
}

// GetConversation returns the current conversation for a call
func (ltm *LiveTranscriptionManager) GetConversation(callUUID string) *ConversationRecord {
	return ltm.conversationAccum.GetConversation(callUUID)
}

// AddConversationEndCallback adds a callback for when conversations end
func (ltm *LiveTranscriptionManager) AddConversationEndCallback(callback ConversationEndCallback) {
	ltm.conversationAccum.AddEndCallback(callback)
}

// GetTranscriptionService returns the transcription service
func (ltm *LiveTranscriptionManager) GetTranscriptionService() *TranscriptionService {
	return ltm.transcriptionSvc
}

// GetConversationAccumulator returns the conversation accumulator
func (ltm *LiveTranscriptionManager) GetConversationAccumulator() *ConversationAccumulator {
	return ltm.conversationAccum
}

// GetMetrics returns current metrics (lock-free)
func (ltm *LiveTranscriptionManager) GetMetrics() (total, live, final int64) {
	return atomic.LoadInt64(&ltm.totalTranscriptions),
		atomic.LoadInt64(&ltm.liveTranscriptions),
		atomic.LoadInt64(&ltm.finalTranscriptions)
}

// Shutdown gracefully shuts down the manager
func (ltm *LiveTranscriptionManager) Shutdown() {
	ltm.conversationAccum.Shutdown()
	ltm.logger.Info("Live transcription manager shutdown complete")
}

// LiveTranscriptionWrapper wraps any STT provider to ensure live transcription
// Optimized for high concurrency with atomic metrics and object pools
type LiveTranscriptionWrapper struct {
	Provider
	transcriptionSvc *TranscriptionService
	logger           *logrus.Logger
	originalCallback func(string, string, bool, map[string]interface{})
	providerName     string // Cached to avoid interface call

	// Metrics - atomic for lock-free updates
	totalTranscriptions int64
	liveTranscriptions  int64
	finalTranscriptions int64

	// Object pool for metadata maps
	metadataPool sync.Pool
}

// NewLiveTranscriptionWrapper creates a wrapper that ensures live transcription
func NewLiveTranscriptionWrapper(provider Provider, transcriptionSvc *TranscriptionService, logger *logrus.Logger) *LiveTranscriptionWrapper {
	wrapper := &LiveTranscriptionWrapper{
		Provider:         provider,
		transcriptionSvc: transcriptionSvc,
		logger:           logger,
		providerName:     provider.Name(), // Cache the name
		metadataPool: sync.Pool{
			New: func() interface{} {
				return make(map[string]interface{}, 8)
			},
		},
	}

	// Capture original callback if provider supports it
	if callbackProvider, ok := provider.(interface {
		SetCallback(func(string, string, bool, map[string]interface{}))
	}); ok {
		// Set our wrapper callback
		callbackProvider.SetCallback(wrapper.onTranscription)
	}

	// Set transcription service directly if provider supports it
	if svcProvider, ok := provider.(interface {
		SetTranscriptionService(*TranscriptionService)
	}); ok {
		svcProvider.SetTranscriptionService(transcriptionSvc)
	}

	return wrapper
}

// onTranscription handles transcription and publishes to service
// Optimized with atomic counters and pooled metadata maps
func (w *LiveTranscriptionWrapper) onTranscription(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			w.logger.WithFields(logrus.Fields{
				"call_uuid": callUUID,
				"panic":     r,
			}).Error("Recovered from panic in live transcription wrapper")
		}
	}()

	if transcription == "" {
		return
	}

	// Update metrics atomically (lock-free)
	atomic.AddInt64(&w.totalTranscriptions, 1)
	if isFinal {
		atomic.AddInt64(&w.finalTranscriptions, 1)
	} else {
		atomic.AddInt64(&w.liveTranscriptions, 1)
	}

	// Use pooled metadata map if none provided
	var pooledMeta map[string]interface{}
	if metadata == nil {
		pooledMeta = w.metadataPool.Get().(map[string]interface{})
		metadata = pooledMeta
	}

	metadata["live"] = true
	metadata["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	metadata["provider"] = w.providerName // Use cached name

	// Publish to transcription service for AMQP delivery
	if w.transcriptionSvc != nil {
		w.transcriptionSvc.PublishTranscription(callUUID, transcription, isFinal, metadata)
	}

	// Call original callback if set
	if w.originalCallback != nil {
		w.originalCallback(callUUID, transcription, isFinal, metadata)
	}

	// Return pooled map if we used one (clear and return)
	if pooledMeta != nil {
		for k := range pooledMeta {
			delete(pooledMeta, k)
		}
		w.metadataPool.Put(pooledMeta)
	}
}

// SetCallback sets the callback and captures it for chaining
func (w *LiveTranscriptionWrapper) SetCallback(callback func(string, string, bool, map[string]interface{})) {
	w.originalCallback = callback
}

// Name returns the wrapped provider name (cached for performance)
func (w *LiveTranscriptionWrapper) Name() string {
	return w.providerName
}

// GetMetrics returns wrapper metrics (lock-free)
func (w *LiveTranscriptionWrapper) GetMetrics() (total, live, final int64) {
	return atomic.LoadInt64(&w.totalTranscriptions),
		atomic.LoadInt64(&w.liveTranscriptions),
		atomic.LoadInt64(&w.finalTranscriptions)
}
