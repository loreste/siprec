package elasticsearch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIndexDocument(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer ts.Close()

	client, err := NewClient(Config{Addresses: []string{ts.URL}})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	doc := map[string]interface{}{"foo": "bar"}
	if err := client.IndexDocument(context.Background(), "my-index", "123", doc); err != nil {
		t.Fatalf("IndexDocument returned error: %v", err)
	}

	if receivedMethod != http.MethodPut {
		t.Fatalf("expected PUT request, got %s", receivedMethod)
	}
	if receivedPath != "/my-index/_doc/123" {
		t.Fatalf("unexpected request path: %s", receivedPath)
	}
	if string(receivedBody) != "{\"foo\":\"bar\"}" {
		t.Fatalf("unexpected body: %s", string(receivedBody))
	}
}
