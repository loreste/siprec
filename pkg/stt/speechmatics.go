package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"siprec-server/pkg/config"
)

// SpeechmaticsProvider implements EnhancedStreamingProvider for Speechmatics real-time WebSocket API.
type SpeechmaticsProvider struct {
	logger           *logrus.Logger
	transcriptionSvc *TranscriptionService
	config           *config.SpeechmaticsSTTConfig
	callback         TranscriptionCallback

	connections     map[string]*speechmaticsConn
	connectionMutex sync.RWMutex
}

// NewSpeechmaticsProvider constructs a Speechmatics provider instance.
func NewSpeechmaticsProvider(logger *logrus.Logger, transcriptionSvc *TranscriptionService, cfg *config.SpeechmaticsSTTConfig) *SpeechmaticsProvider {
	return &SpeechmaticsProvider{
		logger:           logger,
		transcriptionSvc: transcriptionSvc,
		config:           cfg,
		connections:      make(map[string]*speechmaticsConn),
	}
}

// Name returns the provider name.
func (p *SpeechmaticsProvider) Name() string {
	return "speechmatics"
}

// Initialize validates configuration for Speechmatics.
func (p *SpeechmaticsProvider) Initialize() error {
	if p.config == nil {
		return fmt.Errorf("Speechmatics STT configuration is required")
	}

	if !p.config.Enabled {
		p.logger.Info("Speechmatics STT is disabled, skipping initialization")
		return nil
	}

	if strings.TrimSpace(p.config.APIKey) == "" {
		return fmt.Errorf("Speechmatics API key is required when STT is enabled")
	}

	if p.config.SampleRate <= 0 {
		p.config.SampleRate = 8000
	}
	if p.config.Encoding == "" {
		p.config.Encoding = "pcm_s16le"
	}

	p.logger.WithFields(logrus.Fields{
		"ws_url":      p.config.WSURL,
		"language":    p.config.Language,
		"model":       p.config.Model,
		"sample_rate": p.config.SampleRate,
		"encoding":    p.config.Encoding,
	}).Info("Speechmatics provider initialized")

	return nil
}

// SetCallback sets the callback function for transcription results.
func (p *SpeechmaticsProvider) SetCallback(callback TranscriptionCallback) {
	p.callback = callback
}

// GetActiveConnections returns the number of active WebSocket connections.
func (p *SpeechmaticsProvider) GetActiveConnections() int {
	p.connectionMutex.RLock()
	defer p.connectionMutex.RUnlock()
	return len(p.connections)
}

// Shutdown gracefully shuts down all active connections.
func (p *SpeechmaticsProvider) Shutdown(ctx context.Context) error {
	p.connectionMutex.Lock()
	for callUUID, conn := range p.connections {
		p.logger.WithField("call_uuid", callUUID).Debug("Closing Speechmatics WebSocket connection")
		conn.close()
	}
	p.connections = make(map[string]*speechmaticsConn)
	p.connectionMutex.Unlock()
	p.logger.Info("Speechmatics provider shutdown complete")
	return nil
}

// StreamToText connects to Speechmatics real-time WebSocket API and streams audio.
func (p *SpeechmaticsProvider) StreamToText(ctx context.Context, audioStream io.Reader, callUUID string) error {
	if p.config == nil || !p.config.Enabled {
		return fmt.Errorf("Speechmatics STT provider is disabled")
	}

	wsURL := strings.TrimRight(p.config.WSURL, "/")
	if wsURL == "" {
		wsURL = "wss://eu.rt.speechmatics.com/v2"
	}

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+strings.TrimSpace(p.config.APIKey))

	ws, resp, err := websocket.DefaultDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return fmt.Errorf("failed to connect to Speechmatics WebSocket: %w", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}

	connCtx, cancel := context.WithCancel(ctx)
	conn := &speechmaticsConn{
		callUUID:      callUUID,
		ws:            ws,
		cancel:        cancel,
		logger:        p.logger.WithField("call_uuid", callUUID),
		endOfTranscript: make(chan struct{}),
		messagesDone:    make(chan struct{}),
		keepAliveDone:   make(chan struct{}),
		active:          true,
	}

	p.connectionMutex.Lock()
	p.connections[callUUID] = conn
	p.connectionMutex.Unlock()

	defer func() {
		p.connectionMutex.Lock()
		delete(p.connections, callUUID)
		p.connectionMutex.Unlock()
		conn.close()
	}()

	// Send StartRecognition
	txConfig := speechmaticsTranscriptionConfig{
		Language:       p.config.Language,
		EnablePartials: true,
		Diarization:    p.diarizationValue(),
	}
	if p.config.Model != "" {
		txConfig.Model = p.config.Model
	}
	if !p.config.EnablePunctuation {
		// Disable punctuation by sending an empty permitted_marks list
		txConfig.Punctuation = &speechmaticsPunctuation{
			PermittedMarks: []string{},
		}
	}
	startMsg := speechmaticsStartRecognition{
		Message: "StartRecognition",
		AudioFormat: speechmaticsAudioFormat{
			Type:       "raw",
			Encoding:   p.config.Encoding,
			SampleRate: p.config.SampleRate,
		},
		TranscriptionConfig: txConfig,
	}
	if err := ws.WriteJSON(startMsg); err != nil {
		return fmt.Errorf("failed to send StartRecognition: %w", err)
	}

	// Wait for RecognitionStarted
	if err := conn.waitForRecognitionStarted(); err != nil {
		return err
	}

	liveCallback := func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if p.callback != nil {
			p.callback(callUUID, transcription, isFinal, metadata)
		} else if p.transcriptionSvc != nil {
			p.transcriptionSvc.PublishTranscription(callUUID, transcription, isFinal, metadata)
		}
	}

	go conn.handleMessages(liveCallback)
	go conn.keepAlive()

	return conn.streamAudio(connCtx, audioStream)
}

func (p *SpeechmaticsProvider) diarizationValue() string {
	if p.config.EnableDiarization {
		return "speaker"
	}
	return "none"
}

// --- WebSocket connection ---

type speechmaticsConn struct {
	callUUID        string
	ws              *websocket.Conn
	cancel          context.CancelFunc
	logger          *logrus.Entry
	endOfTranscript chan struct{}
	messagesDone    chan struct{}
	keepAliveDone   chan struct{}
	mu              sync.Mutex
	writeMu         sync.Mutex // serializes all WebSocket writes
	active          bool
}

func (c *speechmaticsConn) waitForRecognitionStarted() error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = c.ws.SetReadDeadline(deadline)
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			return fmt.Errorf("failed waiting for RecognitionStarted: %w", err)
		}

		var envelope struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(msg, &envelope); err != nil {
			return fmt.Errorf("failed to parse server message: %w", err)
		}

		switch envelope.Message {
		case "RecognitionStarted":
			_ = c.ws.SetReadDeadline(time.Time{})
			c.logger.Debug("RecognitionStarted received")
			return nil
		case "Error":
			return fmt.Errorf("Speechmatics returned error on start: %s", string(msg))
		case "Info", "Warning":
			// Speechmatics sends Info (e.g. recognition_quality, concurrent_session_usage)
			// and possibly Warning messages before RecognitionStarted — skip them.
			c.logger.WithField("raw", string(msg)).Debug("Pre-recognition server message")
			continue
		default:
			c.logger.WithField("message", envelope.Message).Debug("Skipping unexpected pre-recognition message")
			continue
		}
	}
}

func (c *speechmaticsConn) handleMessages(callback TranscriptionCallback) {
	defer close(c.messagesDone)

	for {
		_ = c.ws.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, msgBytes, err := c.ws.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.logger.WithError(err).Debug("WebSocket read ended")
			}
			return
		}

		var envelope struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(msgBytes, &envelope); err != nil {
			c.logger.WithError(err).Error("Failed to parse Speechmatics message")
			continue
		}

		switch envelope.Message {
		case "AddTranscript":
			c.processTranscript(msgBytes, true, callback)
		case "AddPartialTranscript":
			c.processTranscript(msgBytes, false, callback)
		case "EndOfTranscript":
			c.logger.Debug("EndOfTranscript received")
			select {
			case <-c.endOfTranscript:
			default:
				close(c.endOfTranscript)
			}
			return
		case "AudioAdded":
			// normal ack, ignore
		case "Warning":
			c.logger.WithField("raw", string(msgBytes)).Warn("Speechmatics warning")
		case "Error":
			c.logger.WithField("raw", string(msgBytes)).Error("Speechmatics error")
			return
		default:
			c.logger.WithField("type", envelope.Message).Debug("Unhandled Speechmatics message")
		}
	}
}

func (c *speechmaticsConn) processTranscript(msgBytes []byte, isFinal bool, callback TranscriptionCallback) {
	var resp speechmaticsTranscriptMsg
	if err := json.Unmarshal(msgBytes, &resp); err != nil {
		c.logger.WithError(err).Error("Failed to parse transcript message")
		return
	}

	transcript := strings.TrimSpace(resp.Transcript)
	// Fallback: assemble from results if top-level transcript is empty
	if transcript == "" && len(resp.Results) > 0 {
		var sb strings.Builder
		for _, r := range resp.Results {
			for _, alt := range r.Alternatives {
				sb.WriteString(alt.Content)
			}
		}
		transcript = strings.TrimSpace(sb.String())
	}
	if transcript == "" {
		return
	}

	metadata := map[string]interface{}{
		"provider":   "speechmatics",
		"start_time": resp.Metadata.StartTime,
		"end_time":   resp.Metadata.EndTime,
	}

	if callback != nil {
		callback(c.callUUID, transcript, isFinal, metadata)
	}
}

func (c *speechmaticsConn) streamAudio(ctx context.Context, audioStream io.Reader) error {
	// Reuse the package-level audioBufferPool from deepgram_enhanced.go
	buf := audioBufferPool.Get().([]byte)
	defer audioBufferPool.Put(buf)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := audioStream.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			wErr := c.ws.WriteMessage(websocket.BinaryMessage, chunk)
			c.writeMu.Unlock()
			if wErr != nil {
				return fmt.Errorf("failed to send audio: %w", wErr)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("audio read error: %w", err)
		}
	}

	// Send EndOfStream
	endMsg := map[string]interface{}{"message": "EndOfStream", "last_seq_no": 0}
	c.writeMu.Lock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := c.ws.WriteJSON(endMsg)
	c.writeMu.Unlock()
	if err != nil {
		c.logger.WithError(err).Debug("Failed to send EndOfStream")
	}

	// Wait for EndOfTranscript
	select {
	case <-c.endOfTranscript:
	case <-c.messagesDone:
	case <-time.After(5 * time.Second):
		c.logger.Debug("Timeout waiting for EndOfTranscript")
	case <-ctx.Done():
	}

	return nil
}

func (c *speechmaticsConn) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.keepAliveDone:
			return
		case <-ticker.C:
			c.mu.Lock()
			active := c.active
			c.mu.Unlock()
			if !active {
				return
			}
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
			err := c.ws.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				c.logger.WithError(err).Debug("Keepalive ping failed")
				return
			}
		}
	}
}

func (c *speechmaticsConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.active {
		return
	}
	c.active = false

	select {
	case <-c.keepAliveDone:
	default:
		close(c.keepAliveDone)
	}

	if c.cancel != nil {
		c.cancel()
	}
	if c.ws != nil {
		c.writeMu.Lock()
		_ = c.ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.writeMu.Unlock()
		_ = c.ws.Close()
	}
}

// --- Message types ---

type speechmaticsStartRecognition struct {
	Message             string                          `json:"message"`
	AudioFormat         speechmaticsAudioFormat          `json:"audio_format"`
	TranscriptionConfig speechmaticsTranscriptionConfig  `json:"transcription_config"`
}

type speechmaticsAudioFormat struct {
	Type       string `json:"type"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

type speechmaticsTranscriptionConfig struct {
	Language       string                   `json:"language"`
	Model          string                   `json:"model,omitempty"`
	EnablePartials bool                     `json:"enable_partials"`
	Diarization    string                   `json:"diarization,omitempty"`
	Punctuation    *speechmaticsPunctuation `json:"punctuation_overrides,omitempty"`
}

type speechmaticsPunctuation struct {
	PermittedMarks []string `json:"permitted_marks"`
}

type speechmaticsTranscriptMsg struct {
	Message    string `json:"message"`
	Transcript string `json:"transcript"`
	Metadata   struct {
		StartTime float64 `json:"start_time"`
		EndTime   float64 `json:"end_time"`
	} `json:"metadata"`
	Results []struct {
		Type         string `json:"type"`
		StartTime    float64 `json:"start_time"`
		EndTime      float64 `json:"end_time"`
		Alternatives []struct {
			Content    string  `json:"content"`
			Confidence float64 `json:"confidence"`
			Speaker    string  `json:"speaker,omitempty"`
		} `json:"alternatives"`
	} `json:"results"`
}
