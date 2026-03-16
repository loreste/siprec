package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client wraps HTTP client for SIPREC API calls
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new API client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, path string) ([]byte, int, error) {
	return c.doRequest(ctx, http.MethodGet, path, nil)
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, int, error) {
	return c.doRequest(ctx, http.MethodPost, path, body)
}

// Delete performs a DELETE request
func (c *Client) Delete(ctx context.Context, path string) ([]byte, int, error) {
	return c.doRequest(ctx, http.MethodDelete, path, nil)
}

func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, int, error) {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// APIError represents an error response from the API
type APIError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// ParseError attempts to parse an API error response
func ParseError(body []byte) string {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err == nil {
		if apiErr.Error != "" {
			return apiErr.Error
		}
		if apiErr.Message != "" {
			return apiErr.Message
		}
	}
	return string(body)
}
