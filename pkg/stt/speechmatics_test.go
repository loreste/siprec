package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/config"
)

func TestSpeechmaticsProviderInitialize(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	t.Run("missing config", func(t *testing.T) {
		provider := NewSpeechmaticsProvider(logger, nil, nil)
		err := provider.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "configuration is required")
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &config.SpeechmaticsSTTConfig{Enabled: false}
		provider := NewSpeechmaticsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.NoError(t, err)
	})

	t.Run("enabled without key", func(t *testing.T) {
		cfg := &config.SpeechmaticsSTTConfig{Enabled: true}
		provider := NewSpeechmaticsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API key is required")
	})

	t.Run("success with defaults", func(t *testing.T) {
		cfg := &config.SpeechmaticsSTTConfig{
			Enabled:  true,
			APIKey:   "token",
			Language: "en",
			Timeout:  10 * time.Second,
		}
		provider := NewSpeechmaticsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.NoError(t, err)
		assert.Equal(t, "speechmatics", provider.Name())
		assert.Equal(t, 8000, cfg.SampleRate)
		assert.Equal(t, "pcm_s16le", cfg.Encoding)
	})
}

func TestSpeechmaticsProviderStreamToText(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))

		ws, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer ws.Close()

		// Read StartRecognition
		_, msgBytes, err := ws.ReadMessage()
		require.NoError(t, err)
		var startMsg speechmaticsStartRecognition
		require.NoError(t, json.Unmarshal(msgBytes, &startMsg))
		assert.Equal(t, "StartRecognition", startMsg.Message)
		assert.Equal(t, "pcm_s16le", startMsg.AudioFormat.Encoding)
		assert.Equal(t, 8000, startMsg.AudioFormat.SampleRate)
		assert.Nil(t, startMsg.TranscriptionConfig.Punctuation, "punctuation_overrides should be omitted when enabled")

		// Send Info before RecognitionStarted (like the real Speechmatics API does)
		require.NoError(t, ws.WriteJSON(map[string]interface{}{
			"message": "Info",
			"type":    "recognition_quality",
			"quality": "broadcast",
		}))

		// Send RecognitionStarted
		require.NoError(t, ws.WriteJSON(map[string]string{"message": "RecognitionStarted"}))

		// Read audio (binary)
		_, audioData, err := ws.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, []byte("audio-data"), audioData)

		// Send a partial transcript
		partial := map[string]interface{}{
			"message":    "AddPartialTranscript",
			"transcript": "Hello",
			"metadata":   map[string]interface{}{"start_time": 0.0, "end_time": 0.5},
			"results":    []interface{}{},
		}
		require.NoError(t, ws.WriteJSON(partial))

		// Send a final transcript
		final := map[string]interface{}{
			"message":    "AddTranscript",
			"transcript": "Hello world",
			"metadata":   map[string]interface{}{"start_time": 0.0, "end_time": 1.0},
			"results":    []interface{}{},
		}
		require.NoError(t, ws.WriteJSON(final))

		// Read EndOfStream
		_, msgBytes, err = ws.ReadMessage()
		require.NoError(t, err)
		var endMsg map[string]interface{}
		require.NoError(t, json.Unmarshal(msgBytes, &endMsg))
		assert.Equal(t, "EndOfStream", endMsg["message"])

		// Send EndOfTranscript
		require.NoError(t, ws.WriteJSON(map[string]string{"message": "EndOfTranscript"}))
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:           true,
		APIKey:            "token",
		WSURL:             wsURL,
		Language:          "en",
		Model:             "enhanced",
		Encoding:          "pcm_s16le",
		SampleRate:        8000,
		EnablePunctuation: true,
	}

	provider := NewSpeechmaticsProvider(logger, nil, cfg)
	require.NoError(t, provider.Initialize())

	var partials, finals []string
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		if isFinal {
			finals = append(finals, transcription)
		} else {
			partials = append(partials, transcription)
		}
	})

	err := provider.StreamToText(context.Background(), strings.NewReader("audio-data"), "call-123")
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello"}, partials)
	assert.Equal(t, []string{"Hello world"}, finals)
	assert.Equal(t, 0, provider.GetActiveConnections())
}

func TestSpeechmaticsProviderStreamToTextError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer ws.Close()

		// Read StartRecognition
		_, _, _ = ws.ReadMessage()

		// Send Error instead of RecognitionStarted
		_ = ws.WriteJSON(map[string]interface{}{
			"message": "Error",
			"type":    "not_authorised",
			"reason":  "Invalid API key",
		})
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:    true,
		APIKey:     "bad-token",
		WSURL:      wsURL,
		Language:   "en",
		Encoding:   "pcm_s16le",
		SampleRate: 8000,
	}

	provider := NewSpeechmaticsProvider(logger, nil, cfg)
	require.NoError(t, provider.Initialize())

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error on start")
}

func TestSpeechmaticsPunctuationDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer ws.Close()

		_, msgBytes, err := ws.ReadMessage()
		require.NoError(t, err)

		var startMsg speechmaticsStartRecognition
		require.NoError(t, json.Unmarshal(msgBytes, &startMsg))
		require.NotNil(t, startMsg.TranscriptionConfig.Punctuation)
		assert.Equal(t, []string{}, startMsg.TranscriptionConfig.Punctuation.PermittedMarks)

		_ = ws.WriteJSON(map[string]string{"message": "RecognitionStarted"})

		// Read audio + EndOfStream
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				break
			}
			var env struct{ Message string `json:"message"` }
			if json.Unmarshal(msg, &env) == nil && env.Message == "EndOfStream" {
				_ = ws.WriteJSON(map[string]string{"message": "EndOfTranscript"})
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:           true,
		APIKey:            "token",
		WSURL:             wsURL,
		Language:          "en",
		Encoding:          "pcm_s16le",
		SampleRate:        8000,
		EnablePunctuation: false,
	}

	provider := NewSpeechmaticsProvider(logger, nil, cfg)
	require.NoError(t, provider.Initialize())

	_ = provider.StreamToText(context.Background(), strings.NewReader("audio"), "call-punct")
}
