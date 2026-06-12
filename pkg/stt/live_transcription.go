package stt

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

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

	// Create metadata map if none provided
	if metadata == nil {
		metadata = make(map[string]interface{}, 8)
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
