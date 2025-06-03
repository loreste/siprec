package sip

import (
	"runtime"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
)

// Benchmark header rewriting performance
func BenchmarkNATRewriter_HeaderRewriting(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce logging overhead

	config := &NATConfig{
		BehindNAT:          true,
		InternalIP:         "192.168.1.100",
		ExternalIP:         "203.0.113.10",
		InternalPort:       5060,
		ExternalPort:       5060,
		RewriteVia:         true,
		RewriteContact:     true,
		RewriteRecordRoute: true,
		ForceRewrite:       false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		b.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	// Create test message
	message := &SIPMessage{
		Method:     "INVITE",
		RequestURI: "sip:test@example.com",
		Version:    "SIP/2.0",
		Headers: map[string][]string{
			"via":          {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"},
			"contact":      {"<sip:user@192.168.1.100:5060>"},
			"record-route": {"<sip:proxy@192.168.1.100:5060;lr>"},
		},
		CallID: "test-call-id",
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a copy to avoid modifying the original
		testMessage := &SIPMessage{
			Headers:    make(map[string][]string),
			Connection: message.Connection,
		}
		for k, v := range message.Headers {
			testMessage.Headers[k] = make([]string, len(v))
			copy(testMessage.Headers[k], v)
		}

		err := rewriter.RewriteOutgoingMessage(testMessage)
		if err != nil {
			b.Fatalf("Rewrite failed: %v", err)
		}
	}
}

// Benchmark SDP content rewriting
func BenchmarkNATRewriter_SDPRewriting(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &NATConfig{
		BehindNAT:    true,
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		b.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	sdpBody := []byte(`v=0
o=user 123456 654321 IN IP4 192.168.1.100
s=Test Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
m=video 10002 RTP/AVP 96
a=rtpmap:96 H264/90000
c=IN IP4 192.168.1.100`)

	message := &SIPMessage{
		Headers: map[string][]string{
			"content-type": {"application/sdp"},
		},
		Body: sdpBody,
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a copy
		testMessage := &SIPMessage{
			Headers: map[string][]string{
				"content-type": {"application/sdp"},
			},
			Body:       make([]byte, len(sdpBody)),
			Connection: message.Connection,
		}
		copy(testMessage.Body, sdpBody)

		err := rewriter.RewriteOutgoingMessage(testMessage)
		if err != nil {
			b.Fatalf("Rewrite failed: %v", err)
		}
	}
}

// Benchmark private IP detection
func BenchmarkPrivateIPDetection(b *testing.B) {
	initializeGlobals() // Ensure globals are initialized

	testIPs := []string{
		"192.168.1.1",
		"10.0.0.1",
		"172.16.0.1",
		"203.0.113.1", // Public IP
		"8.8.8.8",     // Public IP
		"127.0.0.1",   // Loopback
	}

	b.Run("Optimized", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for _, ip := range testIPs {
				IsPrivateNetwork(ip)
			}
		}
	})
}

// Benchmark concurrent access (thread safety test)
func BenchmarkNATRewriter_Concurrent(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &NATConfig{
		BehindNAT:      true,
		InternalIP:     "192.168.1.100",
		ExternalIP:     "203.0.113.10",
		InternalPort:   5060,
		ExternalPort:   5060,
		RewriteVia:     true,
		RewriteContact: true,
		ForceRewrite:   false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		b.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	message := &SIPMessage{
		Headers: map[string][]string{
			"via":     {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"},
			"contact": {"<sip:user@192.168.1.100:5060>"},
		},
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Create a copy for each goroutine to avoid races on the message itself
			testMessage := &SIPMessage{
				Headers:    make(map[string][]string),
				Connection: message.Connection,
			}
			for k, v := range message.Headers {
				testMessage.Headers[k] = make([]string, len(v))
				copy(testMessage.Headers[k], v)
			}

			err := rewriter.RewriteOutgoingMessage(testMessage)
			if err != nil {
				b.Fatalf("Rewrite failed: %v", err)
			}

			// Test thread-safe external IP access
			_ = rewriter.GetExternalIP()
		}
	})
}

// Benchmark memory allocations specifically
func BenchmarkNATRewriter_MemoryEfficiency(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &NATConfig{
		BehindNAT:    true,
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: true, // This will exercise more code paths
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		b.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	// Complex message with multiple headers and SDP
	complexSDP := []byte(`v=0
o=user 123456 654321 IN IP4 192.168.1.100
s=Complex Session
c=IN IP4 192.168.1.100
t=0 0
m=audio 10000 RTP/AVP 0 8 9 18
a=rtpmap:0 PCMU/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:9 G722/8000
a=rtpmap:18 G729/8000
c=IN IP4 192.168.1.100
m=video 10002 RTP/AVP 96 97
a=rtpmap:96 H264/90000
a=rtpmap:97 H263/90000
c=IN IP4 192.168.1.100
m=application 10004 TCP webrtc-datachannel`)

	message := &SIPMessage{
		Headers: map[string][]string{
			"via": {
				"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123",
				"SIP/2.0/UDP 192.168.1.101:5060;branch=z9hG4bK124",
			},
			"contact": {
				"<sip:user1@192.168.1.100:5060>",
				"<sip:user2@192.168.1.101:5060>",
			},
			"record-route": {
				"<sip:proxy1@192.168.1.100:5060;lr>",
				"<sip:proxy2@192.168.1.101:5060;lr>",
			},
			"content-type": {"application/sdp"},
		},
		Body: complexSDP,
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	var memStatsBefore, memStatsAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStatsBefore)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a deep copy
		testMessage := &SIPMessage{
			Headers:    make(map[string][]string),
			Body:       make([]byte, len(complexSDP)),
			Connection: message.Connection,
		}
		for k, v := range message.Headers {
			testMessage.Headers[k] = make([]string, len(v))
			copy(testMessage.Headers[k], v)
		}
		copy(testMessage.Body, complexSDP)

		err := rewriter.RewriteOutgoingMessage(testMessage)
		if err != nil {
			b.Fatalf("Rewrite failed: %v", err)
		}
	}

	runtime.GC()
	runtime.ReadMemStats(&memStatsAfter)

	b.ReportMetric(float64(memStatsAfter.TotalAlloc-memStatsBefore.TotalAlloc)/float64(b.N), "bytes/op")
}

// Test for race conditions
func TestNATRewriter_ThreadSafety(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &NATConfig{
		BehindNAT:    true,
		InternalIP:   "192.168.1.100",
		ExternalIP:   "203.0.113.10",
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	message := &SIPMessage{
		Headers: map[string][]string{
			"via":     {"SIP/2.0/UDP 192.168.1.100:5060;branch=z9hG4bK123"},
			"contact": {"<sip:user@192.168.1.100:5060>"},
		},
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				// Create a copy for each operation
				testMessage := &SIPMessage{
					Headers:    make(map[string][]string),
					Connection: message.Connection,
				}
				for k, v := range message.Headers {
					testMessage.Headers[k] = make([]string, len(v))
					copy(testMessage.Headers[k], v)
				}

				// Test concurrent rewriting
				err := rewriter.RewriteOutgoingMessage(testMessage)
				if err != nil {
					t.Errorf("Goroutine %d: Rewrite failed: %v", id, err)
					return
				}

				// Test concurrent external IP access
				_ = rewriter.GetExternalIP()

				// Occasionally update external IP to test write concurrency
				if j%100 == 0 {
					rewriter.SetExternalIP("203.0.113.20")
				}
			}
		}(i)
	}

	wg.Wait()
}

// Benchmark fast path optimization
func BenchmarkNATRewriter_FastPath(b *testing.B) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	config := &NATConfig{
		BehindNAT:    false, // NAT disabled - should be fast path
		ForceRewrite: false,
	}

	rewriter, err := NewNATRewriter(config, logger)
	if err != nil {
		b.Fatalf("Failed to create NAT rewriter: %v", err)
	}
	defer rewriter.Shutdown()

	message := &SIPMessage{
		Headers: map[string][]string{
			"via":     {"SIP/2.0/UDP 8.8.8.8:5060;branch=z9hG4bK123"},
			"contact": {"<sip:user@8.8.8.8:5060>"},
		},
		Connection: &SIPConnection{
			remoteAddr: "203.0.113.1:5060",
			transport:  "udp",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := rewriter.RewriteOutgoingMessage(message)
		if err != nil {
			b.Fatalf("Fast path failed: %v", err)
		}
	}
}
