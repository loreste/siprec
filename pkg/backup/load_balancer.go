package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// LoadBalancerManager manages load balancer configurations for failover
type LoadBalancerManager struct {
	config LoadBalancerConfig
	logger *logrus.Logger
	client *http.Client
}

// NewLoadBalancerManager creates a new load balancer manager
func NewLoadBalancerManager(config LoadBalancerConfig, logger *logrus.Logger) *LoadBalancerManager {
	return &LoadBalancerManager{
		config: config,
		logger: logger,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Backend represents a load balancer backend server
type Backend struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Weight int    `json:"weight"`
	Status string `json:"status"` // up, down, maintenance
}

// UpdateBackends updates the backend servers for a service
func (lbm *LoadBalancerManager) UpdateBackends(ctx context.Context, serviceName string, backends []Backend) error {
	lbm.logger.WithFields(logrus.Fields{
		"service":  serviceName,
		"backends": len(backends),
		"type":     lbm.config.Type,
	}).Info("Updating load balancer backends")

	switch lbm.config.Type {
	case "haproxy":
		return lbm.updateHAProxyBackends(ctx, serviceName, backends)
	case "nginx":
		return lbm.updateNginxBackends(ctx, serviceName, backends)
	case "aws_elb":
		return lbm.updateAWSELBBackends(ctx, serviceName, backends)
	case "gcp_lb":
		return lbm.updateGCPLBBackends(ctx, serviceName, backends)
	default:
		return fmt.Errorf("unsupported load balancer type: %s", lbm.config.Type)
	}
}

// FailoverToBackend fails over traffic to a specific backend
func (lbm *LoadBalancerManager) FailoverToBackend(ctx context.Context, serviceName, backendName string) error {
	lbm.logger.WithFields(logrus.Fields{
		"service": serviceName,
		"backend": backendName,
	}).Info("Initiating load balancer failover")

	switch lbm.config.Type {
	case "haproxy":
		return lbm.haproxyFailover(ctx, serviceName, backendName)
	case "nginx":
		return lbm.nginxFailover(ctx, serviceName, backendName)
	case "aws_elb":
		return lbm.awsELBFailover(ctx, serviceName, backendName)
	case "gcp_lb":
		return lbm.gcpLBFailover(ctx, serviceName, backendName)
	default:
		return fmt.Errorf("unsupported load balancer type: %s", lbm.config.Type)
	}
}

// GetBackendStatus gets the status of all backends for a service
func (lbm *LoadBalancerManager) GetBackendStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	switch lbm.config.Type {
	case "haproxy":
		return lbm.getHAProxyStatus(ctx, serviceName)
	case "nginx":
		return lbm.getNginxStatus(ctx, serviceName)
	case "aws_elb":
		return lbm.getAWSELBStatus(ctx, serviceName)
	case "gcp_lb":
		return lbm.getGCPLBStatus(ctx, serviceName)
	default:
		return nil, fmt.Errorf("unsupported load balancer type: %s", lbm.config.Type)
	}
}

// HAProxy Implementation

func (lbm *LoadBalancerManager) updateHAProxyBackends(ctx context.Context, serviceName string, backends []Backend) error {
	// HAProxy Stats API endpoint
	statsURL := fmt.Sprintf("%s/stats", lbm.config.Endpoint)

	for _, backend := range backends {
		// Enable/disable servers using HAProxy stats interface
		action := "enable"
		if backend.Status == "down" || backend.Status == "maintenance" {
			action = "disable"
		}

		err := lbm.haproxyServerAction(ctx, statsURL, serviceName, backend.Name, action)
		if err != nil {
			lbm.logger.WithError(err).WithFields(logrus.Fields{
				"service": serviceName,
				"backend": backend.Name,
				"action":  action,
			}).Warning("Failed to update HAProxy backend")
			continue
		}
	}

	return nil
}

func (lbm *LoadBalancerManager) haproxyServerAction(ctx context.Context, statsURL, serviceName, serverName, action string) error {
	// HAProxy stats socket command format
	// echo "enable server backend/server" | socat stdio unix-connect:/var/run/haproxy.sock
	command := fmt.Sprintf("%s server %s/%s", action, serviceName, serverName)

	data := map[string]string{
		"action": command,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", statsURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if lbm.config.Username != "" && lbm.config.Password != "" {
		req.SetBasicAuth(lbm.config.Username, lbm.config.Password)
	}

	resp, err := lbm.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HAProxy API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func (lbm *LoadBalancerManager) haproxyFailover(ctx context.Context, serviceName, backendName string) error {
	// Get current backend status
	backends, err := lbm.getHAProxyStatus(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get backend status: %w", err)
	}

	// Disable all backends except the target
	for _, backend := range backends {
		action := "disable"
		if backend.Name == backendName {
			action = "enable"
		}

		err := lbm.haproxyServerAction(ctx, fmt.Sprintf("%s/stats", lbm.config.Endpoint), serviceName, backend.Name, action)
		if err != nil {
			lbm.logger.WithError(err).WithFields(logrus.Fields{
				"service": serviceName,
				"backend": backend.Name,
				"action":  action,
			}).Warning("Failed to update backend during failover")
		}
	}

	lbm.logger.WithFields(logrus.Fields{
		"service": serviceName,
		"backend": backendName,
	}).Info("HAProxy failover completed")

	return nil
}

func (lbm *LoadBalancerManager) getHAProxyStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	statsURL := fmt.Sprintf("%s/stats;csv", lbm.config.Endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", statsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if lbm.config.Username != "" && lbm.config.Password != "" {
		req.SetBasicAuth(lbm.config.Username, lbm.config.Password)
	}

	resp, err := lbm.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return lbm.parseHAProxyCSV(string(body), serviceName)
}

func (lbm *LoadBalancerManager) parseHAProxyCSV(csvData, serviceName string) ([]Backend, error) {
	var backends []Backend
	lines := strings.Split(csvData, "\n")

	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 18 {
			continue
		}

		// Check if this is for our service
		if fields[0] != serviceName {
			continue
		}

		// Skip frontend and backend summary lines
		if fields[1] == "FRONTEND" || fields[1] == "BACKEND" {
			continue
		}

		backend := Backend{
			Name:   fields[1],
			Status: strings.ToLower(fields[17]),
		}

		// Parse server address if available
		if addr := fields[73]; addr != "" {
			parts := strings.Split(addr, ":")
			if len(parts) == 2 {
				backend.Host = parts[0]
				if port, err := strconv.Atoi(parts[1]); err == nil {
					backend.Port = port
				}
			}
		}

		backends = append(backends, backend)
	}

	return backends, nil
}

// Nginx Implementation

func (lbm *LoadBalancerManager) updateNginxBackends(ctx context.Context, serviceName string, backends []Backend) error {
	// Nginx Plus API for dynamic configuration
	upstreamURL := fmt.Sprintf("%s/api/6/http/upstreams/%s/servers", lbm.config.Endpoint, serviceName)

	// Get current servers
	currentServers, err := lbm.getNginxUpstreamServers(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get current servers: %w", err)
	}

	// Remove existing servers
	for _, server := range currentServers {
		deleteURL := fmt.Sprintf("%s/%d", upstreamURL, server.ID)
		err := lbm.nginxAPIRequest(ctx, "DELETE", deleteURL, nil)
		if err != nil {
			lbm.logger.WithError(err).WithField("server_id", server.ID).Warning("Failed to remove nginx server")
		}
	}

	// Add new servers
	for _, backend := range backends {
		serverData := map[string]interface{}{
			"server": fmt.Sprintf("%s:%d", backend.Host, backend.Port),
			"weight": backend.Weight,
		}

		if backend.Status == "down" {
			serverData["down"] = true
		}

		err := lbm.nginxAPIRequest(ctx, "POST", upstreamURL, serverData)
		if err != nil {
			lbm.logger.WithError(err).WithField("backend", backend.Name).Warning("Failed to add nginx server")
		}
	}

	return nil
}

func (lbm *LoadBalancerManager) nginxFailover(ctx context.Context, serviceName, backendName string) error {
	// For nginx, we need to update the upstream configuration
	// This would typically involve updating server weights or removing servers
	upstreamURL := fmt.Sprintf("%s/api/6/http/upstreams/%s/servers", lbm.config.Endpoint, serviceName)

	servers, err := lbm.getNginxUpstreamServers(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to get upstream servers: %w", err)
	}

	for _, server := range servers {
		updateURL := fmt.Sprintf("%s/%d", upstreamURL, server.ID)

		var updateData map[string]interface{}
		if strings.Contains(server.Server, backendName) {
			// Enable target backend
			updateData = map[string]interface{}{
				"weight": 100,
				"down":   false,
			}
		} else {
			// Disable other backends
			updateData = map[string]interface{}{
				"weight": 0,
				"down":   true,
			}
		}

		err := lbm.nginxAPIRequest(ctx, "PATCH", updateURL, updateData)
		if err != nil {
			lbm.logger.WithError(err).WithField("server_id", server.ID).Warning("Failed to update nginx server during failover")
		}
	}

	return nil
}

func (lbm *LoadBalancerManager) getNginxStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	servers, err := lbm.getNginxUpstreamServers(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	var backends []Backend
	for _, server := range servers {
		parts := strings.Split(server.Server, ":")
		backend := Backend{
			Name:   fmt.Sprintf("server-%d", server.ID),
			Weight: server.Weight,
			Status: "up",
		}

		if len(parts) >= 1 {
			backend.Host = parts[0]
		}
		if len(parts) >= 2 {
			if port, err := strconv.Atoi(parts[1]); err == nil {
				backend.Port = port
			}
		}

		if server.Down {
			backend.Status = "down"
		}

		backends = append(backends, backend)
	}

	return backends, nil
}

type NginxServer struct {
	ID     int    `json:"id"`
	Server string `json:"server"`
	Weight int    `json:"weight"`
	Down   bool   `json:"down"`
}

func (lbm *LoadBalancerManager) getNginxUpstreamServers(ctx context.Context, serviceName string) ([]NginxServer, error) {
	url := fmt.Sprintf("%s/api/6/http/upstreams/%s/servers", lbm.config.Endpoint, serviceName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if lbm.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", lbm.config.APIKey))
	}

	resp, err := lbm.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get servers: %w", err)
	}
	defer resp.Body.Close()

	var servers []NginxServer
	err = json.NewDecoder(resp.Body).Decode(&servers)
	if err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return servers, nil
}

func (lbm *LoadBalancerManager) nginxAPIRequest(ctx context.Context, method, url string, data interface{}) error {
	var body io.Reader
	if data != nil {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}
		body = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if lbm.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", lbm.config.APIKey))
	}

	resp, err := lbm.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// AWS ELB Implementation

func (lbm *LoadBalancerManager) updateAWSELBBackends(ctx context.Context, serviceName string, backends []Backend) error {
	// Create real AWS load balancer manager
	awsConfig := AWSConfig{
		Region:          lbm.config.Endpoint, // Assuming endpoint contains region info
		AccessKeyID:     lbm.config.Username,
		SecretAccessKey: lbm.config.Password,
	}

	awsManager := NewRealAWSLoadBalancerManager(awsConfig, lbm.logger)
	return awsManager.UpdateBackends(ctx, serviceName, backends)
}

func (lbm *LoadBalancerManager) awsELBFailover(ctx context.Context, serviceName, backendName string) error {
	awsConfig := AWSConfig{
		Region:          lbm.config.Endpoint,
		AccessKeyID:     lbm.config.Username,
		SecretAccessKey: lbm.config.Password,
	}

	awsManager := NewRealAWSLoadBalancerManager(awsConfig, lbm.logger)
	return awsManager.Failover(ctx, serviceName, backendName)
}

func (lbm *LoadBalancerManager) getAWSELBStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	// AWS ELB status requires AWS SDK implementation
	// Return informative error with implementation guidance
	return nil, &CloudProviderError{
		Provider: "AWS",
		Service:  "ELB",
		Code:     "NOT_IMPLEMENTED",
		Message:  "AWS ELB status check requires AWS SDK v2. Install: go get github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2",
	}
}

// GCP Load Balancer Implementation

func (lbm *LoadBalancerManager) updateGCPLBBackends(ctx context.Context, serviceName string, backends []Backend) error {
	// GCP Load Balancer API calls would go here
	// This would require GCP SDK integration
	lbm.logger.WithFields(logrus.Fields{
		"service":  serviceName,
		"backends": len(backends),
	}).Info("GCP Load Balancer backend update (placeholder)")

	return fmt.Errorf("GCP Load Balancer integration requires GCP SDK - not implemented in this placeholder")
}

func (lbm *LoadBalancerManager) gcpLBFailover(ctx context.Context, serviceName, backendName string) error {
	lbm.logger.WithFields(logrus.Fields{
		"service": serviceName,
		"backend": backendName,
	}).Info("GCP Load Balancer failover (placeholder)")

	return fmt.Errorf("GCP Load Balancer integration requires GCP SDK - not implemented in this placeholder")
}

func (lbm *LoadBalancerManager) getGCPLBStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	return nil, fmt.Errorf("GCP Load Balancer integration requires GCP SDK - not implemented in this placeholder")
}

// DrainBackend drains traffic from a backend gracefully
func (lbm *LoadBalancerManager) DrainBackend(ctx context.Context, serviceName, backendName string) error {
	lbm.logger.WithFields(logrus.Fields{
		"service": serviceName,
		"backend": backendName,
	}).Info("Draining backend traffic")

	switch lbm.config.Type {
	case "haproxy":
		return lbm.haproxyServerAction(ctx, fmt.Sprintf("%s/stats", lbm.config.Endpoint), serviceName, backendName, "drain")
	case "nginx":
		// For nginx, gradually reduce weight
		return lbm.nginxGradualDrain(ctx, serviceName, backendName)
	default:
		return fmt.Errorf("drain not implemented for load balancer type: %s", lbm.config.Type)
	}
}

func (lbm *LoadBalancerManager) nginxGradualDrain(ctx context.Context, serviceName, backendName string) error {
	// Gradually reduce weight to drain connections
	weights := []int{75, 50, 25, 10, 0}

	for _, weight := range weights {
		servers, err := lbm.getNginxUpstreamServers(ctx, serviceName)
		if err != nil {
			return err
		}

		for _, server := range servers {
			if strings.Contains(server.Server, backendName) {
				updateURL := fmt.Sprintf("%s/api/6/http/upstreams/%s/servers/%d", lbm.config.Endpoint, serviceName, server.ID)
				updateData := map[string]interface{}{
					"weight": weight,
				}

				err := lbm.nginxAPIRequest(ctx, "PATCH", updateURL, updateData)
				if err != nil {
					return err
				}

				lbm.logger.WithFields(logrus.Fields{
					"service": serviceName,
					"backend": backendName,
					"weight":  weight,
				}).Debug("Updated backend weight during drain")

				// Wait between weight reductions
				time.Sleep(5 * time.Second)
				break
			}
		}
	}

	return nil
}

// TestLoadBalancer tests connectivity to the load balancer
func (lbm *LoadBalancerManager) TestLoadBalancer(ctx context.Context) error {
	lbm.logger.WithField("endpoint", lbm.config.Endpoint).Info("Testing load balancer connectivity")

	var testURL string
	switch lbm.config.Type {
	case "haproxy":
		testURL = fmt.Sprintf("%s/stats", lbm.config.Endpoint)
	case "nginx":
		testURL = fmt.Sprintf("%s/api/6/nginx", lbm.config.Endpoint)
	default:
		testURL = lbm.config.Endpoint
	}

	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	if lbm.config.Username != "" && lbm.config.Password != "" {
		req.SetBasicAuth(lbm.config.Username, lbm.config.Password)
	}

	if lbm.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", lbm.config.APIKey))
	}

	resp, err := lbm.client.Do(req)
	if err != nil {
		return fmt.Errorf("load balancer connectivity test failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("load balancer test returned status %d", resp.StatusCode)
	}

	lbm.logger.Info("Load balancer connectivity test passed")
	return nil
}
