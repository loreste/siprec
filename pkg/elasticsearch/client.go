package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Config defines connection settings for Elasticsearch.
type Config struct {
	Addresses []string
	Username  string
	Password  string
	Timeout   time.Duration
}

// Client provides minimal functionality for indexing documents.
type Client struct {
	addresses []string
	http      *http.Client
	auth      *basicAuth
	mu        sync.Mutex
	idx       int
}

type basicAuth struct {
	username string
	password string
}

// NewClient creates a new Elasticsearch client.
func NewClient(cfg Config) (*Client, error) {
	if len(cfg.Addresses) == 0 {
		return nil, errors.New("elasticsearch: at least one address is required")
	}

	addresses := make([]string, 0, len(cfg.Addresses))
	for _, addr := range cfg.Addresses {
		trimmed := strings.TrimSpace(addr)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			trimmed = "http://" + trimmed
		}
		if _, err := url.Parse(trimmed); err != nil {
			return nil, fmt.Errorf("elasticsearch: invalid address %q: %w", addr, err)
		}
		addresses = append(addresses, trimmed)
	}

	if len(addresses) == 0 {
		return nil, errors.New("elasticsearch: no valid addresses provided")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &Client{
		addresses: addresses,
		http:      &http.Client{Timeout: timeout},
	}

	if cfg.Username != "" {
		client.auth = &basicAuth{username: cfg.Username, password: cfg.Password}
	}

	return client, nil
}

// IndexDocument indexes or updates a document using HTTP PUT.
func (c *Client) IndexDocument(ctx context.Context, index string, id string, body interface{}) error {
	if strings.TrimSpace(index) == "" {
		return errors.New("elasticsearch: index is required")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("elasticsearch: id is required")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("elasticsearch: failed to marshal document: %w", err)
	}

	addr := c.nextAddress()
	endpoint := fmt.Sprintf("%s/%s/_doc/%s", addr, strings.TrimPrefix(index, "/"), id)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("elasticsearch: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if c.auth != nil {
		req.SetBasicAuth(c.auth.username, c.auth.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("elasticsearch: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("elasticsearch: indexing failed with status %s", resp.Status)
	}

	return nil
}

// nextAddress returns the next address in a round-robin fashion.
func (c *Client) nextAddress() string {
	c.mu.Lock()
	addr := c.addresses[c.idx%len(c.addresses)]
	c.idx++
	c.mu.Unlock()
	return addr
}
