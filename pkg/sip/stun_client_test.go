package sip

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestHTTPFallbackClientSuccess(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	invalidSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-an-ip"))
	}))
	defer invalidSrv.Close()

	validSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("203.0.113.7"))
	}))
	defer validSrv.Close()

	client := NewHTTPFallbackClient(logger)
	client.services = []string{"   ", invalidSrv.URL, validSrv.URL}
	client.timeout = time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ip, err := client.GetExternalIP(ctx)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if ip != "203.0.113.7" {
		t.Fatalf("expected IP 203.0.113.7, got %s", ip)
	}
}

func TestHTTPFallbackClientFailure(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	failureSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error"))
	}))
	defer failureSrv.Close()

	client := NewHTTPFallbackClient(logger)
	client.services = []string{failureSrv.URL}
	client.timeout = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if _, err := client.GetExternalIP(ctx); err == nil {
		t.Fatal("expected error when all fallback services fail")
	}
}
