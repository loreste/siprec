package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"siprec-server/pkg/config"
)

func TestElevenLabsProviderInitialize(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	t.Run("missing config", func(t *testing.T) {
		provider := NewElevenLabsProvider(logger, nil, nil)
		err := provider.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "configuration is required")
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &config.ElevenLabsSTTConfig{
			Enabled: false,
		}
		provider := NewElevenLabsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.NoError(t, err)
	})

	t.Run("enabled without api key", func(t *testing.T) {
		cfg := &config.ElevenLabsSTTConfig{
			Enabled: true,
		}
		provider := NewElevenLabsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "API key is required")
	})

	t.Run("success", func(t *testing.T) {
		cfg := &config.ElevenLabsSTTConfig{
			Enabled:  true,
			APIKey:   "test-key",
			ModelID:  "test-model",
			BaseURL:  "https://api.elevenlabs.io",
			Timeout:  3 * time.Second,
			Language: "en",
		}
		provider := NewElevenLabsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.NoError(t, err)
		assert.Equal(t, "elevenlabs", provider.Name())
	})
}

func TestElevenLabsProviderStreamToText(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	expectedTranscript := "hello world"
	expectedLanguage := "en"
	expectedModel := "test-model"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/speech-to-text", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "test-key", r.Header.Get("xi-api-key"))

		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)

		assert.Equal(t, expectedModel, r.FormValue("model_id"))
		assert.Equal(t, expectedLanguage, r.FormValue("language"))
		assert.Equal(t, "true", r.FormValue("timestamps"))

		file, _, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()

		buf := make([]byte, 4)
		n, err := file.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "data", string(buf[:n]))

		resp := map[string]interface{}{
			"text":       expectedTranscript,
			"language":   expectedLanguage,
			"duration":   1.23,
			"confidence": 0.94,
			"words": []map[string]interface{}{
				{"word": "hello", "start": 0.0, "end": 0.5},
				{"word": "world", "start": 0.5, "end": 0.9},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.ElevenLabsSTTConfig{
		Enabled:          true,
		APIKey:           "test-key",
		BaseURL:          server.URL,
		ModelID:          expectedModel,
		Language:         expectedLanguage,
		EnableTimestamps: true,
		Timeout:          2 * time.Second,
	}

	transcriptionSvc := NewTranscriptionService(logger)
	provider := NewElevenLabsProvider(logger, transcriptionSvc, cfg)
	provider.httpClient = server.Client()
	provider.httpClient.Timeout = cfg.Timeout

	require.NoError(t, provider.Initialize())

	var callbackTranscript string
	var callbackMetadata map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		callbackTranscript = transcription
		callbackMetadata = metadata
	})

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-123")
	require.NoError(t, err)

	assert.Equal(t, expectedTranscript, callbackTranscript)
	require.NotNil(t, callbackMetadata)
	assert.Equal(t, expectedModel, callbackMetadata["model_id"])
	assert.Equal(t, expectedLanguage, callbackMetadata["language"])
	assert.Equal(t, true, callbackMetadata["word_timestamps"])
	assert.Contains(t, callbackMetadata, "words")
}

func TestElevenLabsProviderStreamToTextError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	cfg := &config.ElevenLabsSTTConfig{
		Enabled:  true,
		APIKey:   "test-key",
		BaseURL:  server.URL,
		ModelID:  "test-model",
		Language: "en",
	}

	provider := NewElevenLabsProvider(logger, nil, cfg)
	provider.httpClient = server.Client()

	require.NoError(t, provider.Initialize())

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed with status 400")
}
