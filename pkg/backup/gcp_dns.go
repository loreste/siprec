package backup

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	gdns "google.golang.org/api/dns/v1"
	"google.golang.org/api/option"
)

// gcpDNSChangeTimeout bounds how long we wait for a Cloud DNS change to
// transition from "pending" to "done".
const gcpDNSChangeTimeout = 2 * time.Minute

// gcpProjectAndZone resolves the GCP project ID and managed zone name from
// the DNS configuration.
func (dm *DNSManager) gcpProjectAndZone() (string, string, error) {
	project := dm.config.GCPProject
	if project == "" {
		project = dm.config.Credentials["project_id"]
	}
	if project == "" {
		return "", "", fmt.Errorf("GCP project ID is required (set GCPProject or credentials key project_id)")
	}

	managedZone := dm.config.GCPManagedZone
	if managedZone == "" {
		managedZone = dm.config.Credentials["managed_zone"]
	}
	if managedZone == "" {
		return "", "", fmt.Errorf("GCP managed zone is required (set GCPManagedZone or credentials key managed_zone)")
	}

	return project, managedZone, nil
}

// gcpDNSService creates a Cloud DNS API client. Explicit service account
// credentials from the configuration take precedence; otherwise Application
// Default Credentials are used.
func (dm *DNSManager) gcpDNSService(ctx context.Context) (*gdns.Service, error) {
	var opts []option.ClientOption
	if keyJSON := dm.config.Credentials["service_account_key_json"]; keyJSON != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(keyJSON)))
	} else if keyPath := dm.config.Credentials["service_account_key_path"]; keyPath != "" {
		opts = append(opts, option.WithCredentialsFile(keyPath))
	}

	service, err := gdns.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud DNS client: %w", err)
	}
	return service, nil
}

// Google Cloud DNS Implementation

func (dm *DNSManager) updateGCPDNSRecord(ctx context.Context, record DNSRecord) error {
	project, managedZone, err := dm.gcpProjectAndZone()
	if err != nil {
		return err
	}

	service, err := dm.gcpDNSService(ctx)
	if err != nil {
		return err
	}

	rdata, err := formatRDataValue(record)
	if err != nil {
		return err
	}

	fqdn := dnsFQDN(record.Name)
	ttl := int64(effectiveTTL(record.TTL, dm.config.TTL))

	// Look up the existing record set so it can be replaced atomically in a
	// single change (Cloud DNS changes are delete-old + add-new).
	existing, err := service.ResourceRecordSets.List(project, managedZone).
		Name(fqdn).Type(record.Type).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to list existing Cloud DNS records: %w", err)
	}

	newRRSet := &gdns.ResourceRecordSet{
		Name:    fqdn,
		Type:    record.Type,
		Ttl:     ttl,
		Rrdatas: []string{rdata},
	}

	change := &gdns.Change{
		Additions: []*gdns.ResourceRecordSet{newRRSet},
	}

	for _, rrset := range existing.Rrsets {
		// Skip the change entirely if the record already matches.
		if rrset.Ttl == ttl && len(rrset.Rrdatas) == 1 && rrset.Rrdatas[0] == rdata {
			dm.logger.WithFields(logrus.Fields{
				"name":  record.Name,
				"type":  record.Type,
				"value": record.Value,
			}).Debug("Cloud DNS record already up to date")
			return nil
		}
		change.Deletions = append(change.Deletions, rrset)
	}

	submitted, err := service.Changes.Create(project, managedZone, change).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("failed to create Cloud DNS change: %w", err)
	}

	dm.logger.WithFields(logrus.Fields{
		"change_id": submitted.Id,
		"status":    submitted.Status,
		"name":      record.Name,
		"type":      record.Type,
		"value":     record.Value,
	}).Info("Cloud DNS change submitted")

	return dm.waitForGCPDNSChange(ctx, service, project, managedZone, submitted)
}

// waitForGCPDNSChange polls a Cloud DNS change until it is done or the
// timeout expires.
func (dm *DNSManager) waitForGCPDNSChange(ctx context.Context, service *gdns.Service, project, managedZone string, change *gdns.Change) error {
	if change.Status == "done" {
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, gcpDNSChangeTimeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for Cloud DNS change %s to complete: %w", change.Id, waitCtx.Err())
		case <-ticker.C:
			current, err := service.Changes.Get(project, managedZone, change.Id).Context(waitCtx).Do()
			if err != nil {
				dm.logger.WithError(err).WithField("change_id", change.Id).Warning("Failed to poll Cloud DNS change status")
				continue
			}
			if current.Status == "done" {
				dm.logger.WithField("change_id", change.Id).Info("Cloud DNS change completed")
				return nil
			}
		}
	}
}

func (dm *DNSManager) getGCPDNSRecord(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	project, managedZone, err := dm.gcpProjectAndZone()
	if err != nil {
		return nil, err
	}

	service, err := dm.gcpDNSService(ctx)
	if err != nil {
		return nil, err
	}

	response, err := service.ResourceRecordSets.List(project, managedZone).
		Name(dnsFQDN(name)).Type(recordType).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list Cloud DNS records: %w", err)
	}

	var records []DNSRecord
	for _, rrset := range response.Rrsets {
		for _, rdata := range rrset.Rrdatas {
			records = append(records, parseRDataValue(rrset.Name, rrset.Type, rdata, int(rrset.Ttl)))
		}
	}

	return records, nil
}
