package backup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

// DNSManager manages DNS records for traffic failover
type DNSManager struct {
	config DNSConfig
	logger *logrus.Logger
}

// DNSConfig holds DNS management configuration
type DNSConfig struct {
	Provider    string            // cloudflare, route53, gcp_dns, bind
	Zone        string            // DNS zone name
	TTL         int               // Default TTL in seconds
	Credentials map[string]string // Provider-specific credentials
	NameServers []string          // DNS servers for updates
	SOARecord   SOARecord         // Start of Authority record

	// Route53-specific settings (used when Provider == "route53")
	Route53HostedZoneID string // Hosted zone ID; falls back to Credentials["hosted_zone_id"], then Zone

	// GCP-specific settings (used when Provider == "gcp_dns")
	GCPProject     string // GCP project ID; falls back to Credentials["project_id"]
	GCPManagedZone string // Cloud DNS managed zone name; falls back to Credentials["managed_zone"]
}

// SOARecord represents DNS SOA record
type SOARecord struct {
	PrimaryNS  string
	AdminEmail string
	Serial     uint32
	Refresh    uint32
	Retry      uint32
	Expire     uint32
	MinTTL     uint32
}

// DNSRecord represents a DNS record
type DNSRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // A, AAAA, CNAME, MX, TXT, SRV
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Priority int    `json:"priority,omitempty"` // For MX and SRV records
	Weight   int    `json:"weight,omitempty"`   // For SRV records
	Port     int    `json:"port,omitempty"`     // For SRV records
}

// NewDNSManager creates a new DNS manager
func NewDNSManager(config DNSConfig, logger *logrus.Logger) *DNSManager {
	return &DNSManager{
		config: config,
		logger: logger,
	}
}

// UpdateRecord updates a DNS record for failover
func (dm *DNSManager) UpdateRecord(ctx context.Context, record DNSRecord) error {
	dm.logger.WithFields(logrus.Fields{
		"name":     record.Name,
		"type":     record.Type,
		"value":    record.Value,
		"provider": dm.config.Provider,
	}).Info("Updating DNS record")

	switch dm.config.Provider {
	case "cloudflare":
		return dm.updateCloudflareRecord(ctx, record)
	case "route53":
		return dm.updateRoute53Record(ctx, record)
	case "gcp_dns":
		return dm.updateGCPDNSRecord(ctx, record)
	case "bind":
		return dm.updateBindRecord(ctx, record)
	default:
		return fmt.Errorf("unsupported DNS provider: %s", dm.config.Provider)
	}
}

// GetRecord retrieves a DNS record
func (dm *DNSManager) GetRecord(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	switch dm.config.Provider {
	case "cloudflare":
		return dm.getCloudflareRecord(ctx, name, recordType)
	case "route53":
		return dm.getRoute53Record(ctx, name, recordType)
	case "gcp_dns":
		return dm.getGCPDNSRecord(ctx, name, recordType)
	case "bind":
		return dm.getBindRecord(ctx, name, recordType)
	default:
		return nil, fmt.Errorf("unsupported DNS provider: %s", dm.config.Provider)
	}
}

// Cloudflare Implementation

func (dm *DNSManager) updateCloudflareRecord(ctx context.Context, record DNSRecord) error {
	// Create real Cloudflare manager
	apiToken := dm.config.Credentials["api_token"]
	cloudflareManager := NewRealCloudflareManager(apiToken, dm.config.Zone, dm.logger)
	return cloudflareManager.UpdateRecord(ctx, record)
}

func (dm *DNSManager) getCloudflareRecord(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	apiToken := dm.config.Credentials["api_token"]
	cloudflareManager := NewRealCloudflareManager(apiToken, dm.config.Zone, dm.logger)
	return cloudflareManager.GetRecord(ctx, name, recordType)
}

// Route53 Implementation

// route53ZoneID resolves the hosted zone ID from configuration.
func (dm *DNSManager) route53ZoneID() string {
	if dm.config.Route53HostedZoneID != "" {
		return dm.config.Route53HostedZoneID
	}
	if zoneID := dm.config.Credentials["hosted_zone_id"]; zoneID != "" {
		return zoneID
	}
	return dm.config.Zone
}

// route53Manager builds a Route53 manager from the DNS configuration.
func (dm *DNSManager) route53Manager() *RealRoute53Manager {
	awsConfig := AWSConfig{
		Region:          dm.config.Credentials["region"],
		AccessKeyID:     dm.config.Credentials["access_key_id"],
		SecretAccessKey: dm.config.Credentials["secret_access_key"],
		SessionToken:    dm.config.Credentials["session_token"],
	}
	manager := NewRealRoute53Manager(awsConfig, dm.route53ZoneID(), dm.logger)
	manager.WaitForSync = dm.config.Credentials["wait_for_sync"] == "true"
	return manager
}

func (dm *DNSManager) updateRoute53Record(ctx context.Context, record DNSRecord) error {
	if record.TTL <= 0 {
		record.TTL = effectiveTTL(record.TTL, dm.config.TTL)
	}
	return dm.route53Manager().UpdateRecord(ctx, record)
}

func (dm *DNSManager) getRoute53Record(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	return dm.route53Manager().GetRecord(ctx, name, recordType)
}

// BIND DNS Server Implementation (using DNS updates)

func (dm *DNSManager) updateBindRecord(ctx context.Context, record DNSRecord) error {
	dm.logger.WithFields(logrus.Fields{
		"name":  record.Name,
		"type":  record.Type,
		"value": record.Value,
		"zone":  dm.config.Zone,
	}).Info("Updating BIND DNS record via dynamic update")

	// Create DNS update message
	msg := new(dns.Msg)
	msg.SetUpdate(dns.Fqdn(dm.config.Zone))

	// Remove existing records of the same type
	removeRR := &dns.ANY{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(record.Name),
			Rrtype: dns.StringToType[record.Type],
			Class:  dns.ClassANY,
		},
	}
	msg.RemoveRRset([]dns.RR{removeRR})

	// Add new record
	var newRR dns.RR
	var err error

	switch record.Type {
	case "A":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN A %s",
			dns.Fqdn(record.Name), record.TTL, record.Value))
	case "AAAA":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN AAAA %s",
			dns.Fqdn(record.Name), record.TTL, record.Value))
	case "CNAME":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN CNAME %s",
			dns.Fqdn(record.Name), record.TTL, dns.Fqdn(record.Value)))
	case "MX":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN MX %d %s",
			dns.Fqdn(record.Name), record.TTL, record.Priority, dns.Fqdn(record.Value)))
	case "TXT":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN TXT \"%s\"",
			dns.Fqdn(record.Name), record.TTL, record.Value))
	case "SRV":
		newRR, err = dns.NewRR(fmt.Sprintf("%s %d IN SRV %d %d %d %s",
			dns.Fqdn(record.Name), record.TTL, record.Priority, record.Weight, record.Port, dns.Fqdn(record.Value)))
	default:
		return fmt.Errorf("unsupported record type: %s", record.Type)
	}

	if err != nil {
		return fmt.Errorf("failed to create DNS RR: %w", err)
	}

	msg.Insert([]dns.RR{newRR})

	// Send update to each configured name server
	for _, nameserver := range dm.config.NameServers {
		if !strings.Contains(nameserver, ":") {
			nameserver += ":53"
		}

		client := new(dns.Client)
		client.Timeout = 10 * time.Second

		resp, _, err := client.ExchangeContext(ctx, msg, nameserver)
		if err != nil {
			dm.logger.WithError(err).WithField("nameserver", nameserver).Warning("Failed to send DNS update")
			continue
		}

		if resp.Rcode != dns.RcodeSuccess {
			dm.logger.WithFields(logrus.Fields{
				"nameserver": nameserver,
				"rcode":      dns.RcodeToString[resp.Rcode],
			}).Warning("DNS update failed")
			continue
		}

		dm.logger.WithFields(logrus.Fields{
			"nameserver": nameserver,
			"record":     record.Name,
		}).Info("DNS update successful")

		return nil // Success on first server
	}

	return fmt.Errorf("failed to update DNS on any nameserver")
}

func (dm *DNSManager) getBindRecord(ctx context.Context, name, recordType string) ([]DNSRecord, error) {
	if len(dm.config.NameServers) == 0 {
		return nil, fmt.Errorf("no nameservers configured")
	}

	nameserver := dm.config.NameServers[0]
	if !strings.Contains(nameserver, ":") {
		nameserver += ":53"
	}

	// Create DNS query
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), dns.StringToType[recordType])

	client := new(dns.Client)
	client.Timeout = 10 * time.Second

	resp, _, err := client.ExchangeContext(ctx, msg, nameserver)
	if err != nil {
		return nil, fmt.Errorf("DNS query failed: %w", err)
	}

	if resp.Rcode != dns.RcodeSuccess {
		return nil, fmt.Errorf("DNS query returned: %s", dns.RcodeToString[resp.Rcode])
	}

	var records []DNSRecord
	for _, rr := range resp.Answer {
		switch recordType {
		case "A":
			if a, ok := rr.(*dns.A); ok {
				records = append(records, DNSRecord{
					Name:  strings.TrimSuffix(a.Hdr.Name, "."),
					Type:  "A",
					Value: a.A.String(),
					TTL:   int(a.Hdr.Ttl),
				})
			}
		case "AAAA":
			if aaaa, ok := rr.(*dns.AAAA); ok {
				records = append(records, DNSRecord{
					Name:  strings.TrimSuffix(aaaa.Hdr.Name, "."),
					Type:  "AAAA",
					Value: aaaa.AAAA.String(),
					TTL:   int(aaaa.Hdr.Ttl),
				})
			}
		case "CNAME":
			if cname, ok := rr.(*dns.CNAME); ok {
				records = append(records, DNSRecord{
					Name:  strings.TrimSuffix(cname.Hdr.Name, "."),
					Type:  "CNAME",
					Value: strings.TrimSuffix(cname.Target, "."),
					TTL:   int(cname.Hdr.Ttl),
				})
			}
		}
	}

	return records, nil
}
