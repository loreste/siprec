package stt

import (
	"context"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type modelTestTransport func(*http.Request) (*http.Response, error)

func (f modelTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOpenSourceHTTPProcessesAudioAfterCallEnd(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	for _, readErr := range []error{io.EOF, context.Canceled} {
		ctx, cancel := context.WithCancel(context.Background())
		invoked := false
		p := &OpenSourceModelProvider{logger: logger, config: &OpenSourceModelConfig{Backend: BackendHTTP, BaseURL: "http://example.invalid", Timeout: time.Second}, httpClient: &http.Client{Transport: modelTestTransport(func(r *http.Request) (*http.Response, error) {
			invoked = true
			require.NoError(t, r.Context().Err())
			_, bounded := r.Context().Deadline()
			require.True(t, bounded)
			require.NoError(t, r.ParseMultipartForm(1024))
			file, _, err := r.FormFile("audio")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, []byte{0, 1, 2, 3}, data)
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"text":"done"}`)), Header: make(http.Header)}, nil
		})}}
		require.NoError(t, p.transcribeHTTP(ctx, whisperCancellationReader{cancel, readErr}, "call"))
		require.True(t, invoked)
		cancel()
	}
}

func TestOpenSourceCLIProcessesAudioAfterCallEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test executable")
	}
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	binary := filepath.Join(t.TempDir(), "model")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nprintf done\n"), 0o700))
	p := &OpenSourceModelProvider{logger: logger, config: &OpenSourceModelConfig{ExecutablePath: binary, Timeout: time.Second}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, p.transcribeCLI(ctx, whisperCancellationReader{cancel, io.EOF}, "call"))
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 5\n"), 0o700))
	p.config.Timeout = 20 * time.Millisecond
	require.ErrorIs(t, p.transcribeCLI(context.Background(), strings.NewReader("audio"), "timeout"), context.DeadlineExceeded)
}
