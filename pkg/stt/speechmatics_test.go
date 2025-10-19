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

	t.Run("success", func(t *testing.T) {
		cfg := &config.SpeechmaticsSTTConfig{
			Enabled:  true,
			APIKey:   "token",
			BaseURL:  "https://asr.api.speechmatics.com/v2",
			Language: "en-US",
			Model:    "universal",
			Timeout:  10 * time.Second,
		}
		provider := NewSpeechmaticsProvider(logger, nil, cfg)
		err := provider.Initialize()
		require.NoError(t, err)
		assert.Equal(t, "speechmatics", provider.Name())
	})
}

func TestSpeechmaticsProviderStreamToTextInline(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	expectedTranscript := "Hello world"
	expectedLanguage := "en-US"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/jobs", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))

		err := r.ParseMultipartForm(10 << 20)
		require.NoError(t, err)
		assert.Contains(t, r.FormValue("transcription_config"), `"language":"`+expectedLanguage+`"`)

		file, _, err := r.FormFile("data_file")
		require.NoError(t, err)
		defer file.Close()

		buf := make([]byte, 4)
		n, err := file.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "data", string(buf[:n]))

		resp := speechmaticsJobResponse{
			ID: "job-123",
			Results: []speechmaticsResultChunk{
				{Alternatives: []speechmaticsAlternative{{Text: expectedTranscript}}},
			},
			Duration: 1.5,
		}

		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:  true,
		APIKey:   "token",
		BaseURL:  server.URL,
		Language: expectedLanguage,
	}

	transcriptionSvc := NewTranscriptionService(logger)
	provider := NewSpeechmaticsProvider(logger, transcriptionSvc, cfg)
	provider.httpClient = server.Client()

	require.NoError(t, provider.Initialize())

	var callbackText string
	var callbackMeta map[string]interface{}
	provider.SetCallback(func(callUUID, transcription string, isFinal bool, metadata map[string]interface{}) {
		callbackText = transcription
		callbackMeta = metadata
	})

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-123")
	require.NoError(t, err)
	assert.Equal(t, expectedTranscript, callbackText)
	require.NotNil(t, callbackMeta)
	assert.Equal(t, expectedLanguage, callbackMeta["language"])
	assert.Equal(t, "universal", callbackMeta["model"])
	assert.Equal(t, "job-123", callbackMeta["job_id"])
}

func TestSpeechmaticsProviderStreamToTextFetchTranscript(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	transcriptText := "Fetched transcript"
	jobID := "job-456"

	mux := http.NewServeMux()
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": jobID,
		})
	})
	mux.HandleFunc("/jobs/"+jobID+"/transcript", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "txt", r.URL.Query().Get("format"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(transcriptText))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:  true,
		APIKey:   "token",
		BaseURL:  server.URL,
		Language: "en-US",
	}

	provider := NewSpeechmaticsProvider(logger, nil, cfg)
	provider.httpClient = server.Client()

	require.NoError(t, provider.Initialize())

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-789")
	require.NoError(t, err)
}

func TestSpeechmaticsProviderStreamToTextError(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := &config.SpeechmaticsSTTConfig{
		Enabled:  true,
		APIKey:   "token",
		BaseURL:  server.URL,
		Language: "en-US",
	}

	provider := NewSpeechmaticsProvider(logger, nil, cfg)
	provider.httpClient = server.Client()

	require.NoError(t, provider.Initialize())

	err := provider.StreamToText(context.Background(), strings.NewReader("data"), "call-err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}
