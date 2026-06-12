package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// gcpLBOperationTimeout bounds how long we wait for a backend service
// update operation to complete.
const gcpLBOperationTimeout = 3 * time.Minute

// GCP Load Balancer Implementation
//
// Backends in a GCP backend service are instance groups or network endpoint
// groups (NEGs) referenced by URL. Backend.Name is matched against the last
// path segment of the group URL. Traffic shifting mirrors the HAProxy/nginx
// failover semantics in this file: the target backend gets full capacity
// (CapacityScaler 1.0) and all other backends are drained (CapacityScaler 0).

// validateGCPLBConfig validates the GCP load balancer configuration.
func (lbm *LoadBalancerManager) validateGCPLBConfig() error {
	if lbm.config.GCPProject == "" {
		return fmt.Errorf("GCP project ID is required for gcp_lb (set LoadBalancerConfig.GCPProject)")
	}
	return nil
}

// gcpComputeService creates a Compute Engine API client. Explicit service
// account credentials take precedence; otherwise Application Default
// Credentials are used.
func (lbm *LoadBalancerManager) gcpComputeService(ctx context.Context) (*compute.Service, error) {
	var opts []option.ClientOption
	if lbm.config.GCPServiceAccountKeyJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(lbm.config.GCPServiceAccountKeyJSON)))
	} else if lbm.config.GCPServiceAccountKeyPath != "" {
		opts = append(opts, option.WithCredentialsFile(lbm.config.GCPServiceAccountKeyPath))
	}

	service, err := compute.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP compute client: %w", err)
	}
	return service, nil
}

// gcpBackendServiceName resolves the backend service name, preferring the
// configured override.
func (lbm *LoadBalancerManager) gcpBackendServiceName(serviceName string) string {
	if lbm.config.GCPBackendService != "" {
		return lbm.config.GCPBackendService
	}
	return serviceName
}

// getGCPBackendService fetches a backend service (regional when GCPRegion is
// set, global otherwise).
func (lbm *LoadBalancerManager) getGCPBackendService(ctx context.Context, service *compute.Service, name string) (*compute.BackendService, error) {
	if lbm.config.GCPRegion != "" {
		return service.RegionBackendServices.Get(lbm.config.GCPProject, lbm.config.GCPRegion, name).Context(ctx).Do()
	}
	return service.BackendServices.Get(lbm.config.GCPProject, name).Context(ctx).Do()
}

// patchGCPBackends patches the backends list of a backend service and waits
// for the operation to complete.
func (lbm *LoadBalancerManager) patchGCPBackends(ctx context.Context, service *compute.Service, backendService *compute.BackendService) error {
	patch := &compute.BackendService{
		Backends:    backendService.Backends,
		Fingerprint: backendService.Fingerprint,
	}

	var op *compute.Operation
	var err error
	if lbm.config.GCPRegion != "" {
		op, err = service.RegionBackendServices.Patch(lbm.config.GCPProject, lbm.config.GCPRegion, backendService.Name, patch).Context(ctx).Do()
	} else {
		op, err = service.BackendServices.Patch(lbm.config.GCPProject, backendService.Name, patch).Context(ctx).Do()
	}
	if err != nil {
		return fmt.Errorf("failed to patch backend service %s: %w", backendService.Name, err)
	}

	return lbm.waitForGCPOperation(ctx, service, op)
}

// waitForGCPOperation waits for a compute operation to reach DONE status.
func (lbm *LoadBalancerManager) waitForGCPOperation(ctx context.Context, service *compute.Service, op *compute.Operation) error {
	waitCtx, cancel := context.WithTimeout(ctx, gcpLBOperationTimeout)
	defer cancel()

	for {
		if op.Status == "DONE" {
			if op.Error != nil && len(op.Error.Errors) > 0 {
				var messages []string
				for _, opErr := range op.Error.Errors {
					messages = append(messages, fmt.Sprintf("%s: %s", opErr.Code, opErr.Message))
				}
				return fmt.Errorf("GCP operation %s failed: %s", op.Name, strings.Join(messages, "; "))
			}
			return nil
		}

		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("timed out waiting for GCP operation %s: %w", op.Name, err)
		}

		var current *compute.Operation
		var err error
		if lbm.config.GCPRegion != "" {
			current, err = service.RegionOperations.Wait(lbm.config.GCPProject, lbm.config.GCPRegion, op.Name).Context(waitCtx).Do()
		} else {
			current, err = service.GlobalOperations.Wait(lbm.config.GCPProject, op.Name).Context(waitCtx).Do()
		}
		if err != nil {
			return fmt.Errorf("failed to poll GCP operation %s: %w", op.Name, err)
		}
		op = current
	}
}

// gcpCapacityScaler converts a Backend definition into a GCP capacity scaler
// value in [0.0, 1.0]. Down/maintenance backends are drained; Weight is
// interpreted as a percentage (0 or unset means full capacity).
func gcpCapacityScaler(backend Backend) float64 {
	if backend.Status == "down" || backend.Status == "maintenance" {
		return 0.0
	}
	if backend.Weight <= 0 {
		return 1.0
	}
	if backend.Weight >= 100 {
		return 1.0
	}
	return float64(backend.Weight) / 100.0
}

// lastURLSegment extracts the final path segment of a resource URL.
func lastURLSegment(url string) string {
	trimmed := strings.TrimSuffix(url, "/")
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return trimmed
}

func (lbm *LoadBalancerManager) updateGCPLBBackends(ctx context.Context, serviceName string, backends []Backend) error {
	if err := lbm.validateGCPLBConfig(); err != nil {
		return err
	}

	service, err := lbm.gcpComputeService(ctx)
	if err != nil {
		return err
	}

	backendServiceName := lbm.gcpBackendServiceName(serviceName)
	backendService, err := lbm.getGCPBackendService(ctx, service, backendServiceName)
	if err != nil {
		return fmt.Errorf("failed to get GCP backend service %s: %w", backendServiceName, err)
	}

	desired := make(map[string]Backend, len(backends))
	for _, backend := range backends {
		desired[backend.Name] = backend
	}

	updated := 0
	for _, computeBackend := range backendService.Backends {
		groupName := lastURLSegment(computeBackend.Group)
		backend, ok := desired[groupName]
		if !ok {
			continue
		}

		computeBackend.CapacityScaler = gcpCapacityScaler(backend)
		// CapacityScaler 0.0 must be serialized explicitly to drain a backend.
		computeBackend.ForceSendFields = append(computeBackend.ForceSendFields, "CapacityScaler")
		delete(desired, groupName)
		updated++

		lbm.logger.WithFields(logrus.Fields{
			"service":         backendServiceName,
			"backend":         groupName,
			"capacity_scaler": computeBackend.CapacityScaler,
		}).Debug("Updating GCP backend capacity")
	}

	for name := range desired {
		lbm.logger.WithFields(logrus.Fields{
			"service": backendServiceName,
			"backend": name,
		}).Warning("Backend not found in GCP backend service; skipping")
	}

	if updated == 0 {
		return fmt.Errorf("no backends matched in GCP backend service %s", backendServiceName)
	}

	if err := lbm.patchGCPBackends(ctx, service, backendService); err != nil {
		return err
	}

	lbm.logger.WithFields(logrus.Fields{
		"service": backendServiceName,
		"updated": updated,
	}).Info("GCP Load Balancer backends updated")

	return nil
}

func (lbm *LoadBalancerManager) gcpLBFailover(ctx context.Context, serviceName, backendName string) error {
	if err := lbm.validateGCPLBConfig(); err != nil {
		return err
	}

	service, err := lbm.gcpComputeService(ctx)
	if err != nil {
		return err
	}

	backendServiceName := lbm.gcpBackendServiceName(serviceName)
	backendService, err := lbm.getGCPBackendService(ctx, service, backendServiceName)
	if err != nil {
		return fmt.Errorf("failed to get GCP backend service %s: %w", backendServiceName, err)
	}

	// Mirror the HAProxy/nginx failover semantics: enable the target backend
	// at full capacity and drain all others.
	targetFound := false
	for _, computeBackend := range backendService.Backends {
		groupName := lastURLSegment(computeBackend.Group)
		if strings.Contains(groupName, backendName) {
			computeBackend.CapacityScaler = 1.0
			targetFound = true
		} else {
			computeBackend.CapacityScaler = 0.0
		}
		computeBackend.ForceSendFields = append(computeBackend.ForceSendFields, "CapacityScaler")
	}

	if !targetFound {
		return fmt.Errorf("target backend %s not found in GCP backend service %s", backendName, backendServiceName)
	}

	if err := lbm.patchGCPBackends(ctx, service, backendService); err != nil {
		return err
	}

	lbm.logger.WithFields(logrus.Fields{
		"service": backendServiceName,
		"backend": backendName,
	}).Info("GCP Load Balancer failover completed")

	return nil
}

func (lbm *LoadBalancerManager) getGCPLBStatus(ctx context.Context, serviceName string) ([]Backend, error) {
	if err := lbm.validateGCPLBConfig(); err != nil {
		return nil, err
	}

	service, err := lbm.gcpComputeService(ctx)
	if err != nil {
		return nil, err
	}

	backendServiceName := lbm.gcpBackendServiceName(serviceName)
	backendService, err := lbm.getGCPBackendService(ctx, service, backendServiceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get GCP backend service %s: %w", backendServiceName, err)
	}

	backends := make([]Backend, 0, len(backendService.Backends))
	for _, computeBackend := range backendService.Backends {
		backend := Backend{
			Name:   lastURLSegment(computeBackend.Group),
			Weight: int(computeBackend.CapacityScaler*100 + 0.5),
			Status: "up",
		}
		if computeBackend.CapacityScaler == 0 {
			backend.Status = "maintenance"
		}

		health, err := lbm.getGCPBackendHealth(ctx, service, backendServiceName, computeBackend.Group)
		if err != nil {
			lbm.logger.WithError(err).WithFields(logrus.Fields{
				"service": backendServiceName,
				"backend": backend.Name,
			}).Warning("Failed to get GCP backend health")
			backend.Status = "unknown"
			backends = append(backends, backend)
			continue
		}

		healthy := 0
		for _, status := range health.HealthStatus {
			if status.HealthState == "HEALTHY" {
				healthy++
			}
			if backend.Host == "" && status.IpAddress != "" {
				backend.Host = status.IpAddress
				backend.Port = int(status.Port)
			}
		}

		if len(health.HealthStatus) > 0 && healthy == 0 {
			backend.Status = "down"
		}

		backends = append(backends, backend)
	}

	return backends, nil
}

// getGCPBackendHealth retrieves the health of a single backend group.
func (lbm *LoadBalancerManager) getGCPBackendHealth(ctx context.Context, service *compute.Service, backendServiceName, group string) (*compute.BackendServiceGroupHealth, error) {
	groupRef := &compute.ResourceGroupReference{Group: group}
	if lbm.config.GCPRegion != "" {
		return service.RegionBackendServices.GetHealth(lbm.config.GCPProject, lbm.config.GCPRegion, backendServiceName, groupRef).Context(ctx).Do()
	}
	return service.BackendServices.GetHealth(lbm.config.GCPProject, backendServiceName, groupRef).Context(ctx).Do()
}
