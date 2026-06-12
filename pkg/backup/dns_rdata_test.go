package backup

import (
	"testing"
)

func TestDNSFQDN(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com."},
		{"example.com.", "example.com."},
		{"sip.example.com", "sip.example.com."},
	}

	for _, tt := range tests {
		if got := dnsFQDN(tt.input); got != tt.expected {
			t.Errorf("dnsFQDN(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestEffectiveTTL(t *testing.T) {
	tests := []struct {
		recordTTL int
		configTTL int
		expected  int
	}{
		{120, 60, 120},
		{0, 60, 60},
		{0, 0, defaultDNSTTL},
		{-1, 0, defaultDNSTTL},
	}

	for _, tt := range tests {
		if got := effectiveTTL(tt.recordTTL, tt.configTTL); got != tt.expected {
			t.Errorf("effectiveTTL(%d, %d) = %d, want %d", tt.recordTTL, tt.configTTL, got, tt.expected)
		}
	}
}

func TestFormatRDataValue(t *testing.T) {
	tests := []struct {
		name     string
		record   DNSRecord
		expected string
		wantErr  bool
	}{
		{
			name:     "A record",
			record:   DNSRecord{Type: "A", Value: "192.0.2.1"},
			expected: "192.0.2.1",
		},
		{
			name:     "AAAA record",
			record:   DNSRecord{Type: "AAAA", Value: "2001:db8::1"},
			expected: "2001:db8::1",
		},
		{
			name:     "CNAME record gets trailing dot",
			record:   DNSRecord{Type: "CNAME", Value: "target.example.com"},
			expected: "target.example.com.",
		},
		{
			name:     "MX record includes priority",
			record:   DNSRecord{Type: "MX", Value: "mail.example.com", Priority: 10},
			expected: "10 mail.example.com.",
		},
		{
			name:     "SRV record includes priority weight port",
			record:   DNSRecord{Type: "SRV", Value: "sip.example.com", Priority: 10, Weight: 60, Port: 5060},
			expected: "10 60 5060 sip.example.com.",
		},
		{
			name:     "TXT record is quoted",
			record:   DNSRecord{Type: "TXT", Value: "v=spf1 -all"},
			expected: `"v=spf1 -all"`,
		},
		{
			name:     "pre-quoted TXT record is preserved",
			record:   DNSRecord{Type: "TXT", Value: `"already quoted"`},
			expected: `"already quoted"`,
		},
		{
			name:    "unsupported type",
			record:  DNSRecord{Type: "NAPTR", Value: "whatever"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatRDataValue(tt.record)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got value %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("formatRDataValue() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseRDataValue(t *testing.T) {
	t.Run("A record", func(t *testing.T) {
		record := parseRDataValue("host.example.com.", "A", "192.0.2.1", 300)
		if record.Name != "host.example.com" || record.Value != "192.0.2.1" || record.TTL != 300 {
			t.Errorf("unexpected record: %+v", record)
		}
	})

	t.Run("MX record", func(t *testing.T) {
		record := parseRDataValue("example.com.", "MX", "10 mail.example.com.", 300)
		if record.Priority != 10 || record.Value != "mail.example.com" {
			t.Errorf("unexpected record: %+v", record)
		}
	})

	t.Run("SRV record", func(t *testing.T) {
		record := parseRDataValue("_sip._udp.example.com.", "SRV", "10 60 5060 sip.example.com.", 300)
		if record.Priority != 10 || record.Weight != 60 || record.Port != 5060 || record.Value != "sip.example.com" {
			t.Errorf("unexpected record: %+v", record)
		}
	})

	t.Run("TXT record is unquoted", func(t *testing.T) {
		record := parseRDataValue("example.com.", "TXT", `"v=spf1 -all"`, 300)
		if record.Value != "v=spf1 -all" {
			t.Errorf("unexpected value: %q", record.Value)
		}
	})

	t.Run("CNAME trailing dot stripped", func(t *testing.T) {
		record := parseRDataValue("alias.example.com.", "CNAME", "target.example.com.", 300)
		if record.Value != "target.example.com" {
			t.Errorf("unexpected value: %q", record.Value)
		}
	})
}

func TestFormatParseRoundTrip(t *testing.T) {
	records := []DNSRecord{
		{Name: "host.example.com", Type: "A", Value: "192.0.2.1", TTL: 60},
		{Name: "example.com", Type: "MX", Value: "mail.example.com", Priority: 10, TTL: 60},
		{Name: "_sip._udp.example.com", Type: "SRV", Value: "sip.example.com", Priority: 10, Weight: 5, Port: 5060, TTL: 60},
		{Name: "example.com", Type: "TXT", Value: "hello world", TTL: 60},
	}

	for _, original := range records {
		rdata, err := formatRDataValue(original)
		if err != nil {
			t.Fatalf("formatRDataValue(%+v): %v", original, err)
		}

		parsed := parseRDataValue(dnsFQDN(original.Name), original.Type, rdata, original.TTL)
		if parsed.Name != original.Name || parsed.Type != original.Type ||
			parsed.Value != original.Value || parsed.TTL != original.TTL ||
			parsed.Priority != original.Priority || parsed.Weight != original.Weight ||
			parsed.Port != original.Port {
			t.Errorf("round trip mismatch:\noriginal: %+v\nparsed:   %+v", original, parsed)
		}
	}
}
