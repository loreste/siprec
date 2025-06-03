//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// E2ETestSuite provides end-to-end tests for the complete SIPREC server
type E2ETestSuite struct {
	suite.Suite
	serverURL   string
	wsURL       string
	httpClient  *http.Client
	serverReady bool
}

// TestMessage represents a WebSocket test message
type TestMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string                 `json:"status"`
	Version string                 `json:"version,omitempty"`
	Uptime  string                 `json:"uptime,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
}

func (suite *E2ETestSuite) SetupSuite() {
	// Configuration
	suite.serverURL = "http://localhost:8080"
	suite.wsURL = "ws://localhost:8080"

	suite.httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}

	// Wait for server to be ready
	suite.waitForServer()
}

func (suite *E2ETestSuite) waitForServer() {
	maxAttempts := 30
	for i := 0; i < maxAttempts; i++ {
		resp, err := suite.httpClient.Get(suite.serverURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			suite.serverReady = true
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(2 * time.Second)
	}
	suite.T().Fatal("Server did not become ready in time")
}

func (suite *E2ETestSuite) TestServerHealth() {
	suite.Require().True(suite.serverReady, "Server should be ready")

	resp, err := suite.httpClient.Get(suite.serverURL + "/health")
	suite.Require().NoError(err)
	defer resp.Body.Close()

	suite.Assert().Equal(http.StatusOK, resp.StatusCode)

	var health HealthResponse
	err = json.NewDecoder(resp.Body).Decode(&health)
	suite.Assert().NoError(err)
	suite.Assert().Equal("healthy", health.Status)
}

func (suite *E2ETestSuite) TestMetricsEndpoint() {
	resp, err := suite.httpClient.Get(suite.serverURL + "/metrics")
	suite.Require().NoError(err)
	defer resp.Body.Close()

	// Should return metrics in Prometheus format or JSON
	suite.Assert().True(resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound)

	if resp.StatusCode == http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		suite.Assert().NoError(err)
		suite.Assert().NotEmpty(body)
	}
}

func (suite *E2ETestSuite) TestWebSocketConnection() {
	// Test basic WebSocket connection
	wsURL := suite.wsURL + "/ws/transcriptions"

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	suite.Require().NoError(err)
	defer conn.Close()

	if resp != nil {
		defer resp.Body.Close()
		suite.Assert().Equal(http.StatusSwitchingProtocols, resp.StatusCode)
	}

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Send a test message (if the server expects any)
	testMsg := TestMessage{
		Type:    "ping",
		Payload: "test",
	}

	err = conn.WriteJSON(testMsg)
	suite.Assert().NoError(err)

	// Try to read a response (may timeout if server doesn't send anything)
	var response map[string]interface{}
	err = conn.ReadJSON(&response)
	// We don't assert error here as the server might not respond to ping
	if err == nil {
		suite.T().Logf("Received WebSocket response: %+v", response)
	}
}

func (suite *E2ETestSuite) TestWebSocketWithCallUUID() {
	// Test WebSocket connection with call UUID parameter
	callUUID := "test-call-e2e"
	wsURL := suite.wsURL + "/ws/transcriptions?call_uuid=" + callUUID

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	suite.Require().NoError(err)
	defer conn.Close()

	if resp != nil {
		defer resp.Body.Close()
		suite.Assert().Equal(http.StatusSwitchingProtocols, resp.StatusCode)
	}

	// Connection should be established successfully
	suite.Assert().NotNil(conn)
}

func (suite *E2ETestSuite) TestConcurrentWebSocketConnections() {
	const numConnections = 5
	connections := make([]*websocket.Conn, numConnections)

	// Create multiple concurrent connections
	for i := 0; i < numConnections; i++ {
		wsURL := fmt.Sprintf("%s/ws/transcriptions?call_uuid=concurrent-test-%d", suite.wsURL, i)

		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}

		conn, resp, err := dialer.Dial(wsURL, nil)
		suite.Require().NoError(err)

		if resp != nil {
			resp.Body.Close()
		}

		connections[i] = conn
	}

	// Clean up connections
	for _, conn := range connections {
		if conn != nil {
			conn.Close()
		}
	}
}

func (suite *E2ETestSuite) TestAPIEndpoints() {
	// Test various API endpoints that should exist
	endpoints := []struct {
		path           string
		method         string
		expectedStatus int
	}{
		{"/health", "GET", http.StatusOK},
		{"/metrics", "GET", http.StatusOK}, // or 404 if not implemented
		{"/websocket-client", "GET", http.StatusOK},
		{"/api/sessions", "GET", http.StatusOK},  // or 404 if not implemented
		{"/api/providers", "GET", http.StatusOK}, // or 404 if not implemented
	}

	for _, endpoint := range endpoints {
		suite.Run(fmt.Sprintf("%s %s", endpoint.method, endpoint.path), func() {
			req, err := http.NewRequest(endpoint.method, suite.serverURL+endpoint.path, nil)
			suite.Require().NoError(err)

			resp, err := suite.httpClient.Do(req)
			suite.Require().NoError(err)
			defer resp.Body.Close()

			// Allow both expected status and 404 (not implemented)
			suite.Assert().True(
				resp.StatusCode == endpoint.expectedStatus || resp.StatusCode == http.StatusNotFound,
				"Expected %d or 404, got %d for %s %s",
				endpoint.expectedStatus, resp.StatusCode, endpoint.method, endpoint.path,
			)
		})
	}
}

func (suite *E2ETestSuite) TestHTTPErrorHandling() {
	// Test 404 handling
	resp, err := suite.httpClient.Get(suite.serverURL + "/nonexistent")
	suite.Require().NoError(err)
	defer resp.Body.Close()

	suite.Assert().Equal(http.StatusNotFound, resp.StatusCode)
}

func (suite *E2ETestSuite) TestServerStressTest() {
	// Simple stress test with concurrent requests
	const numRequests = 20
	const numWorkers = 5

	requestChan := make(chan int, numRequests)
	resultChan := make(chan error, numRequests)

	// Fill request channel
	for i := 0; i < numRequests; i++ {
		requestChan <- i
	}
	close(requestChan)

	// Start workers
	for i := 0; i < numWorkers; i++ {
		go func() {
			for requestID := range requestChan {
				url := fmt.Sprintf("%s/health?request_id=%d", suite.serverURL, requestID)
				resp, err := suite.httpClient.Get(url)
				if err != nil {
					resultChan <- err
					continue
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					resultChan <- fmt.Errorf("unexpected status code: %d", resp.StatusCode)
					continue
				}

				resultChan <- nil
			}
		}()
	}

	// Collect results
	var errors []error
	for i := 0; i < numRequests; i++ {
		if err := <-resultChan; err != nil {
			errors = append(errors, err)
		}
	}

	// Should have minimal errors (allow for some network issues)
	errorRate := float64(len(errors)) / float64(numRequests)
	suite.Assert().Less(errorRate, 0.1, "Error rate should be less than 10%")

	if len(errors) > 0 {
		suite.T().Logf("Encountered %d errors out of %d requests", len(errors), numRequests)
		for i, err := range errors {
			if i < 5 { // Log first 5 errors
				suite.T().Logf("Error %d: %v", i+1, err)
			}
		}
	}
}

func (suite *E2ETestSuite) TestWebSocketClientPage() {
	// Test that the WebSocket client page is served correctly
	resp, err := suite.httpClient.Get(suite.serverURL + "/websocket-client")
	suite.Require().NoError(err)
	defer resp.Body.Close()

	suite.Assert().Equal(http.StatusOK, resp.StatusCode)
	suite.Assert().Equal("text/html", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	suite.Assert().NoError(err)
	suite.Assert().Contains(string(body), "SIPREC Transcription WebSocket Client")
	suite.Assert().Contains(string(body), "WebSocket")
}

func (suite *E2ETestSuite) TestConfigurationEndpoints() {
	// Test configuration-related endpoints (if implemented)
	configEndpoints := []string{
		"/api/config",
		"/api/providers",
		"/api/status",
	}

	for _, endpoint := range configEndpoints {
		suite.Run(endpoint, func() {
			resp, err := suite.httpClient.Get(suite.serverURL + endpoint)
			suite.Require().NoError(err)
			defer resp.Body.Close()

			// These endpoints might not be implemented, so we allow 404
			suite.Assert().True(
				resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
				"Expected 200 or 404, got %d for %s", resp.StatusCode, endpoint,
			)

			if resp.StatusCode == http.StatusOK {
				var result map[string]interface{}
				err = json.NewDecoder(resp.Body).Decode(&result)
				// JSON decoding might fail if it's not JSON, which is fine
				if err == nil {
					suite.T().Logf("Endpoint %s returned: %+v", endpoint, result)
				}
			}
		})
	}
}

func (suite *E2ETestSuite) TestServerUptime() {
	// Test that server uptime is increasing
	resp1, err := suite.httpClient.Get(suite.serverURL + "/health")
	suite.Require().NoError(err)
	defer resp1.Body.Close()

	var health1 HealthResponse
	err = json.NewDecoder(resp1.Body).Decode(&health1)
	suite.Require().NoError(err)

	// Wait a bit
	time.Sleep(2 * time.Second)

	resp2, err := suite.httpClient.Get(suite.serverURL + "/health")
	suite.Require().NoError(err)
	defer resp2.Body.Close()

	var health2 HealthResponse
	err = json.NewDecoder(resp2.Body).Decode(&health2)
	suite.Require().NoError(err)

	// Both should be healthy
	suite.Assert().Equal("healthy", health1.Status)
	suite.Assert().Equal("healthy", health2.Status)

	// If uptime is provided, second one should be higher
	if health1.Uptime != "" && health2.Uptime != "" {
		suite.Assert().NotEqual(health1.Uptime, health2.Uptime)
	}
}

func (suite *E2ETestSuite) TestLongRunningConnection() {
	// Test a longer-running WebSocket connection
	wsURL := suite.wsURL + "/ws/transcriptions?call_uuid=long-running-test"

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.Dial(wsURL, nil)
	suite.Require().NoError(err)
	defer conn.Close()

	if resp != nil {
		defer resp.Body.Close()
	}

	// Keep connection alive for a bit and send periodic messages
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	messageCount := 0
	for {
		select {
		case <-ctx.Done():
			suite.T().Logf("Long-running connection test completed, sent %d messages", messageCount)
			return
		case <-ticker.C:
			testMsg := TestMessage{
				Type:    "keepalive",
				Payload: fmt.Sprintf("message-%d", messageCount),
			}

			err = conn.WriteJSON(testMsg)
			if err != nil {
				suite.T().Logf("Failed to send message %d: %v", messageCount, err)
				return
			}
			messageCount++

			// Try to read any response (with short timeout)
			conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			var response map[string]interface{}
			err = conn.ReadJSON(&response)
			if err == nil {
				suite.T().Logf("Received response to message %d: %+v", messageCount, response)
			}
			// Reset deadline
			conn.SetReadDeadline(time.Time{})
		}
	}
}

func (suite *E2ETestSuite) TearDownSuite() {
	// Cleanup if needed
	suite.T().Log("E2E test suite completed")
}

// TestE2EServerFunctionality runs the complete E2E test suite
func TestE2EServerFunctionality(t *testing.T) {
	suite.Run(t, new(E2ETestSuite))
}

// TestSimpleE2E provides a simple standalone E2E test that doesn't require the suite
func TestSimpleE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	serverURL := "http://localhost:8080"
	client := &http.Client{Timeout: 10 * time.Second}

	// Wait for server
	maxAttempts := 10
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		resp, err := client.Get(serverURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			t.Log("Server is ready")
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		lastErr = err
		time.Sleep(2 * time.Second)
	}

	if lastErr != nil {
		t.Logf("Server health check failed: %v", lastErr)
		t.Skip("Server not available for E2E testing")
	}

	// Simple health check test
	resp, err := client.Get(serverURL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Read and verify response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var health map[string]interface{}
	err = json.Unmarshal(body, &health)
	require.NoError(t, err)

	status, exists := health["status"]
	require.True(t, exists, "Health response should contain status")
	assert.Equal(t, "healthy", status)

	t.Log("Simple E2E test passed")
}
