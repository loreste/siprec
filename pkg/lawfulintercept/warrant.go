package lawfulintercept

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// WarrantVerifier handles verification of lawful intercept warrants
type WarrantVerifier struct {
	endpoint  string
	client    *http.Client
	logger    *logrus.Entry
	cache     map[string]*warrantCacheEntry
	cacheMu   sync.RWMutex
	cacheTTL  time.Duration
}

type warrantCacheEntry struct {
	valid     bool
	expiresAt time.Time
	checkedAt time.Time
}

// WarrantRequest represents a warrant verification request
type WarrantRequest struct {
	WarrantID string `json:"warrant_id"`
	TargetID  string `json:"target_id"`
	Timestamp string `json:"timestamp"`
	NodeID    string `json:"node_id,omitempty"`
}

// WarrantResponse represents a warrant verification response
type WarrantResponse struct {
	Valid       bool      `json:"valid"`
	WarrantID   string    `json:"warrant_id"`
	TargetID    string    `json:"target_id"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Scope       []string  `json:"scope,omitempty"`       // audio, metadata, signaling
	Authority   string    `json:"authority,omitempty"`   // Issuing authority
	CaseNumber  string    `json:"case_number,omitempty"` // Court case number
	Error       string    `json:"error,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
}

// NewWarrantVerifier creates a new warrant verifier
func NewWarrantVerifier(endpoint string, tlsConfig *tls.Config, logger *logrus.Logger) *WarrantVerifier {
	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	return &WarrantVerifier{
		endpoint: endpoint,
		client:   client,
		logger:   logger.WithField("component", "warrant_verifier"),
		cache:    make(map[string]*warrantCacheEntry),
		cacheTTL: 5 * time.Minute, // Cache valid warrants for 5 minutes
	}
}

// Verify checks if a warrant is valid for the given target
func (wv *WarrantVerifier) Verify(ctx context.Context, warrantID, targetID string) (bool, error) {
	cacheKey := warrantID + ":" + targetID

	// Check cache first
	wv.cacheMu.RLock()
	if entry, ok := wv.cache[cacheKey]; ok {
		if time.Now().Before(entry.expiresAt) {
			wv.cacheMu.RUnlock()
			wv.logger.WithFields(logrus.Fields{
				"warrant_id": warrantID,
				"target_id":  targetID,
				"cached":     true,
			}).Debug("Warrant verification from cache")
			return entry.valid, nil
		}
	}
	wv.cacheMu.RUnlock()

	// Make verification request
	valid, resp, err := wv.verifyRemote(ctx, warrantID, targetID)
	if err != nil {
		return false, err
	}

	// Update cache
	wv.cacheMu.Lock()
	wv.cache[cacheKey] = &warrantCacheEntry{
		valid:     valid,
		expiresAt: time.Now().Add(wv.cacheTTL),
		checkedAt: time.Now(),
	}
	// Clean old entries periodically
	if len(wv.cache) > 1000 {
		wv.cleanCache()
	}
	wv.cacheMu.Unlock()

	wv.logger.WithFields(logrus.Fields{
		"warrant_id": warrantID,
		"target_id":  targetID,
		"valid":      valid,
		"authority":  resp.Authority,
		"expires_at": resp.ExpiresAt,
	}).Info("Warrant verification completed")

	return valid, nil
}

// VerifyWithDetails returns full warrant details
func (wv *WarrantVerifier) VerifyWithDetails(ctx context.Context, warrantID, targetID string) (*WarrantResponse, error) {
	_, resp, err := wv.verifyRemote(ctx, warrantID, targetID)
	return resp, err
}

func (wv *WarrantVerifier) verifyRemote(ctx context.Context, warrantID, targetID string) (bool, *WarrantResponse, error) {
	req := WarrantRequest{
		WarrantID: warrantID,
		TargetID:  targetID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, wv.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-LI-Version", "1.0")
	httpReq.Header.Set("X-Warrant-ID", warrantID)

	resp, err := wv.client.Do(httpReq)
	if err != nil {
		return false, nil, fmt.Errorf("verification request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return false, &WarrantResponse{
			Valid:     false,
			WarrantID: warrantID,
			TargetID:  targetID,
			Error:     "warrant not found",
			ErrorCode: "WARRANT_NOT_FOUND",
		}, nil
	}

	if resp.StatusCode == http.StatusForbidden {
		return false, &WarrantResponse{
			Valid:     false,
			WarrantID: warrantID,
			TargetID:  targetID,
			Error:     "access denied",
			ErrorCode: "ACCESS_DENIED",
		}, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, nil, fmt.Errorf("verification failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var warrantResp WarrantResponse
	if err := json.Unmarshal(respBody, &warrantResp); err != nil {
		return false, nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if warrant has expired
	if !warrantResp.ExpiresAt.IsZero() && time.Now().After(warrantResp.ExpiresAt) {
		warrantResp.Valid = false
		warrantResp.Error = "warrant has expired"
		warrantResp.ErrorCode = "WARRANT_EXPIRED"
	}

	return warrantResp.Valid, &warrantResp, nil
}

// InvalidateCache removes a warrant from the cache
func (wv *WarrantVerifier) InvalidateCache(warrantID, targetID string) {
	cacheKey := warrantID + ":" + targetID
	wv.cacheMu.Lock()
	delete(wv.cache, cacheKey)
	wv.cacheMu.Unlock()
}

// InvalidateWarrant removes all cache entries for a warrant
func (wv *WarrantVerifier) InvalidateWarrant(warrantID string) {
	wv.cacheMu.Lock()
	for key := range wv.cache {
		if len(key) > len(warrantID) && key[:len(warrantID)+1] == warrantID+":" {
			delete(wv.cache, key)
		}
	}
	wv.cacheMu.Unlock()
}

func (wv *WarrantVerifier) cleanCache() {
	now := time.Now()
	for key, entry := range wv.cache {
		if now.After(entry.expiresAt) {
			delete(wv.cache, key)
		}
	}
}

// GetCacheStats returns cache statistics
func (wv *WarrantVerifier) GetCacheStats() (size int, hitRate float64) {
	wv.cacheMu.RLock()
	defer wv.cacheMu.RUnlock()
	return len(wv.cache), 0 // Hit rate tracking could be added
}

// SetCacheTTL updates the cache TTL
func (wv *WarrantVerifier) SetCacheTTL(ttl time.Duration) {
	wv.cacheTTL = ttl
}
