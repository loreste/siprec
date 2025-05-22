package stt

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TranscriptionListener represents something that can listen for transcription updates
type TranscriptionListener interface {
	// OnTranscription is called when a new transcription is available
	OnTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{})
}

// TranscriptionService manages transcription results and notifies listeners
type TranscriptionService struct {
	logger    *logrus.Logger
	listeners []TranscriptionListener
	mutex     sync.RWMutex
}

// NewTranscriptionService creates a new transcription service
func NewTranscriptionService(logger *logrus.Logger) *TranscriptionService {
	return &TranscriptionService{
		logger:    logger,
		listeners: make([]TranscriptionListener, 0),
	}
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

// PublishTranscription notifies all listeners about a new transcription
func (s *TranscriptionService) PublishTranscription(callUUID string, transcription string, isFinal bool, metadata map[string]interface{}) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if transcription == "" {
		return // Don't publish empty transcriptions
	}

	s.logger.WithFields(logrus.Fields{
		"call_uuid":      callUUID,
		"transcription":  transcription,
		"is_final":       isFinal,
		"listener_count": len(s.listeners),
	}).Debug("Publishing transcription to listeners")

	for _, listener := range s.listeners {
		listener.OnTranscription(callUUID, transcription, isFinal, metadata)
	}
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
