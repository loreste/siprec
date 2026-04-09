package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestClusterHandler_ClusteringDisabled(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handler := NewClusterHandler(logger, nil)

	endpoints := []struct {
		path   string
		method string
	}{
		{"/api/cluster/status", http.MethodGet},
		{"/api/cluster/nodes", http.MethodGet},
		{"/api/cluster/health", http.MethodGet},
		{"/api/cluster/drain", http.MethodPost},
		{"/api/cluster/migrations", http.MethodGet},
		{"/api/cluster/split-brain", http.MethodGet},
		{"/api/cluster/split-brain/check", http.MethodPost},
		{"/api/cluster/traces", http.MethodGet},
		{"/api/cluster/rtp-states", http.MethodGet},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, nil)
			rec := httptest.NewRecorder()

			// Call the handler directly based on the path
			switch ep.path {
			case "/api/cluster/status":
				handler.handleStatus(rec, req)
			case "/api/cluster/nodes":
				handler.handleNodes(rec, req)
			case "/api/cluster/health":
				handler.handleHealth(rec, req)
			case "/api/cluster/drain":
				handler.handleDrain(rec, req)
			case "/api/cluster/migrations":
				handler.handleMigrations(rec, req)
			case "/api/cluster/split-brain":
				handler.handleSplitBrain(rec, req)
			case "/api/cluster/split-brain/check":
				handler.handleForceQuorumCheck(rec, req)
			case "/api/cluster/traces":
				handler.handleTraces(rec, req)
			case "/api/cluster/rtp-states":
				handler.handleRTPStates(rec, req)
			}

			assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
				"should return 503 when clustering is disabled for %s", ep.path)

			var resp map[string]string
			json.NewDecoder(rec.Body).Decode(&resp)
			assert.Contains(t, resp["error"], "not enabled")
		})
	}
}

func TestClusterHandler_MethodNotAllowed(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handler := NewClusterHandler(logger, nil)

	// GET-only endpoints should reject POST
	getEndpoints := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/cluster/status", handler.handleStatus},
		{"/api/cluster/nodes", handler.handleNodes},
		{"/api/cluster/health", handler.handleHealth},
		{"/api/cluster/split-brain", handler.handleSplitBrain},
		{"/api/cluster/traces", handler.handleTraces},
		{"/api/cluster/rtp-states", handler.handleRTPStates},
	}

	for _, ep := range getEndpoints {
		t.Run("POST "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, ep.path, nil)
			rec := httptest.NewRecorder()
			ep.handler(rec, req)
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		})
	}

	// POST-only endpoints should reject GET
	postEndpoints := []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/api/cluster/drain", handler.handleDrain},
		{"/api/cluster/split-brain/check", handler.handleForceQuorumCheck},
	}

	for _, ep := range postEndpoints {
		t.Run("GET "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep.path, nil)
			rec := httptest.NewRecorder()
			ep.handler(rec, req)
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
		})
	}
}

func TestClusterHandler_RegisterHandlers(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handler := NewClusterHandler(logger, nil)

	// Create a minimal server with a mux
	mux := http.NewServeMux()
	server := &Server{
		mux:                mux,
		logger:             logger,
		additionalHandlers: make(map[string]http.HandlerFunc),
	}

	handler.RegisterHandlers(server)

	// Verify all expected paths were registered
	expectedPaths := []string{
		"/api/cluster/status",
		"/api/cluster/nodes",
		"/api/cluster/health",
		"/api/cluster/drain",
		"/api/cluster/migrations",
		"/api/cluster/split-brain",
		"/api/cluster/split-brain/check",
		"/api/cluster/traces",
		"/api/cluster/rtp-states",
	}

	for _, path := range expectedPaths {
		_, exists := server.additionalHandlers[path]
		assert.True(t, exists, "handler should be registered for %s", path)
	}
}

func TestClusterHandler_WriteJSON(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	handler := NewClusterHandler(logger, nil)
	rec := httptest.NewRecorder()

	handler.writeJSON(rec, http.StatusOK, map[string]string{"key": "value"})

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	assert.Equal(t, "value", body["key"])
}
