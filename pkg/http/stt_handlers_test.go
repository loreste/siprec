package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"siprec-server/pkg/stt"

	"github.com/sirupsen/logrus"
)

func newTestSTTHandlers(t *testing.T, allowedDir, purgeToken string) *STTHandlers {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cfg := stt.DefaultAsyncSTTConfig()
	cfg.QueuePurgeToken = purgeToken
	processor := stt.NewAsyncSTTProcessor(nil, logger, cfg)

	return NewSTTHandlers(processor, logger, allowedDir)
}

func submitSTTJob(t *testing.T, handlers *STTHandlers, audioPath string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(SubmitJobRequest{
		AudioPath: audioPath,
		CallUUID:  "call-123",
		SessionID: "session-123",
	})
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/stt/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handlers.SubmitJobHandler(w, req)
	return w
}

func TestSubmitJobHandlerPathValidation(t *testing.T) {
	t.Run("rejects all submissions when no allowed directory is configured", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, "", "")

		w := submitSTTJob(t, handlers, "/tmp/audio.wav")
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects path traversal with ..", func(t *testing.T) {
		allowedDir := t.TempDir()
		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, filepath.Join(allowedDir, "..", "escape.wav"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects relative path traversal", func(t *testing.T) {
		allowedDir := t.TempDir()
		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, "../../etc/passwd")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects absolute path outside allowed directory", func(t *testing.T) {
		allowedDir := t.TempDir()
		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, "/etc/passwd")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects symlink escaping allowed directory", func(t *testing.T) {
		allowedDir := t.TempDir()
		outsideDir := t.TempDir()

		secretFile := filepath.Join(outsideDir, "secret.wav")
		if err := os.WriteFile(secretFile, []byte("outside data"), 0o644); err != nil {
			t.Fatalf("failed to create outside file: %v", err)
		}

		linkPath := filepath.Join(allowedDir, "link.wav")
		if err := os.Symlink(secretFile, linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, linkPath)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects symlinked subdirectory escaping allowed directory", func(t *testing.T) {
		allowedDir := t.TempDir()
		outsideDir := t.TempDir()

		linkDir := filepath.Join(allowedDir, "sub")
		if err := os.Symlink(outsideDir, linkDir); err != nil {
			t.Fatalf("failed to create directory symlink: %v", err)
		}

		handlers := newTestSTTHandlers(t, allowedDir, "")

		// The final file does not exist yet, but the existing symlinked prefix
		// must still be resolved and rejected.
		w := submitSTTJob(t, handlers, filepath.Join(linkDir, "audio.wav"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("accepts valid path inside allowed directory", func(t *testing.T) {
		allowedDir := t.TempDir()
		audioFile := filepath.Join(allowedDir, "audio.wav")
		if err := os.WriteFile(audioFile, []byte("RIFF fake audio"), 0o644); err != nil {
			t.Fatalf("failed to create audio file: %v", err)
		}

		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, audioFile)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}

		var response SubmitJobResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if response.JobID == "" {
			t.Fatal("expected job ID in response")
		}
	})

	t.Run("accepts relative path inside allowed directory", func(t *testing.T) {
		allowedDir := t.TempDir()
		audioFile := filepath.Join(allowedDir, "audio.wav")
		if err := os.WriteFile(audioFile, []byte("RIFF fake audio"), 0o644); err != nil {
			t.Fatalf("failed to create audio file: %v", err)
		}

		handlers := newTestSTTHandlers(t, allowedDir, "")

		w := submitSTTJob(t, handlers, "audio.wav")
		if w.Code != http.StatusCreated {
			t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func purgeQueueRequest(t *testing.T, handlers *STTHandlers, token string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(map[string]interface{}{
		"confirm":      true,
		"reason":       "test purge",
		"dry_run":      true,
		"requested_by": "unit-test",
	})
	if err != nil {
		t.Fatalf("failed to marshal purge request: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/stt/queue/purge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-STT-Queue-Token", token)
	}
	w := httptest.NewRecorder()
	handlers.PurgeQueueHandler(w, req)
	return w
}

func TestPurgeQueueHandlerTokenEnforcement(t *testing.T) {
	t.Run("rejects purge when no token is configured", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, t.TempDir(), "")

		w := purgeQueueRequest(t, handlers, "")
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects purge when no token is configured even if a token is provided", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, t.TempDir(), "")

		w := purgeQueueRequest(t, handlers, "some-token")
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects purge with missing token", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, t.TempDir(), "expected-token")

		w := purgeQueueRequest(t, handlers, "")
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects purge with wrong token", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, t.TempDir(), "expected-token")

		w := purgeQueueRequest(t, handlers, "wrong-token")
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("allows purge with correct token", func(t *testing.T) {
		handlers := newTestSTTHandlers(t, t.TempDir(), "expected-token")

		w := purgeQueueRequest(t, handlers, "expected-token")
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
