package sip

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestHTTPFallbackClientSuccess(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(io.Discard)

	client := NewHTTPFallbackClient(logger)
	client.services = []string{"   ", "https://invalid.service", "https://valid.service"}
	client.timeout = time.Second
	client.httpClient = &http.Client{
		Timeout: time.Second,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			body := "not-an-ip"
			if strings.Contains(req.URL.Host, "valid.service") {
				body = "203.0.113.7"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

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

	client := NewHTTPFallbackClient(logger)
	client.services = []string{"https://failure.local"}
	client.timeout = 100 * time.Millisecond
	client.httpClient = &http.Client{
		Timeout: 100 * time.Millisecond,
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("error")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if _, err := client.GetExternalIP(ctx); err == nil {
		t.Fatal("expected error when all fallback services fail")
	}
}
