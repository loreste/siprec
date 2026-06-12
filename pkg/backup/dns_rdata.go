package backup

import (
	"fmt"
	"strconv"
	"strings"
)

// defaultDNSTTL is used when a record does not specify a TTL and the
// manager configuration does not provide one either.
const defaultDNSTTL = 300

// dnsFQDN ensures a DNS name is fully qualified (trailing dot).
func dnsFQDN(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// effectiveTTL returns the TTL to use for a record, falling back to the
// configured default and finally to defaultDNSTTL.
func effectiveTTL(recordTTL, configTTL int) int {
	if recordTTL > 0 {
		return recordTTL
	}
	if configTTL > 0 {
		return configTTL
	}
	return defaultDNSTTL
}

// formatRDataValue renders the zone-file rdata string for a DNS record.
// Both Route53 and Google Cloud DNS accept standard zone-file rdata.
func formatRDataValue(record DNSRecord) (string, error) {
	switch record.Type {
	case "A", "AAAA":
		return record.Value, nil
	case "CNAME", "NS", "PTR":
		return dnsFQDN(record.Value), nil
	case "MX":
		return fmt.Sprintf("%d %s", record.Priority, dnsFQDN(record.Value)), nil
	case "SRV":
		return fmt.Sprintf("%d %d %d %s", record.Priority, record.Weight, record.Port, dnsFQDN(record.Value)), nil
	case "TXT":
		if strings.HasPrefix(record.Value, "\"") {
			return record.Value, nil
		}
		return strconv.Quote(record.Value), nil
	default:
		return "", fmt.Errorf("unsupported record type: %s", record.Type)
	}
}

// parseRDataValue parses provider rdata back into a DNSRecord.
func parseRDataValue(name, recordType, rdata string, ttl int) DNSRecord {
	record := DNSRecord{
		Name:  strings.TrimSuffix(name, "."),
		Type:  recordType,
		Value: rdata,
		TTL:   ttl,
	}

	switch recordType {
	case "CNAME", "NS", "PTR":
		record.Value = strings.TrimSuffix(rdata, ".")
	case "MX":
		fields := strings.Fields(rdata)
		if len(fields) == 2 {
			if priority, err := strconv.Atoi(fields[0]); err == nil {
				record.Priority = priority
				record.Value = strings.TrimSuffix(fields[1], ".")
			}
		}
	case "SRV":
		fields := strings.Fields(rdata)
		if len(fields) == 4 {
			priority, err1 := strconv.Atoi(fields[0])
			weight, err2 := strconv.Atoi(fields[1])
			port, err3 := strconv.Atoi(fields[2])
			if err1 == nil && err2 == nil && err3 == nil {
				record.Priority = priority
				record.Weight = weight
				record.Port = port
				record.Value = strings.TrimSuffix(fields[3], ".")
			}
		}
	case "TXT":
		if unquoted, err := strconv.Unquote(rdata); err == nil {
			record.Value = unquoted
		} else {
			record.Value = strings.Trim(rdata, "\"")
		}
	}

	return record
}
