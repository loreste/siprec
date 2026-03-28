package sip

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestNormalizeCRLF(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already correct CRLF",
			input:    "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.example.com\r\n\r\n",
			expected: "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.example.com\r\n\r\n",
		},
		{
			name:     "bare LF only",
			input:    "INVITE sip:bob@example.com SIP/2.0\nVia: SIP/2.0/UDP pc33.example.com\n\n",
			expected: "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.example.com\r\n\r\n",
		},
		{
			name:     "mixed CRLF and bare LF",
			input:    "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.example.com\nContact: <sip:alice@host>\r\n\r\n",
			expected: "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.example.com\r\nContact: <sip:alice@host>\r\n\r\n",
		},
		{
			name:     "no newlines at all",
			input:    "some data without newlines",
			expected: "some data without newlines",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "bare LF at start",
			input:    "\nVia: SIP/2.0/UDP host\r\n\r\n",
			expected: "\r\nVia: SIP/2.0/UDP host\r\n\r\n",
		},
		{
			name:     "full SIP message with bare LF",
			input:    "INVITE sip:bob@biloxi.com SIP/2.0\nVia: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776asdhds\nMax-Forwards: 70\nTo: Bob <sip:bob@biloxi.com>\nFrom: Alice <sip:alice@atlanta.com>;tag=1928301774\nCall-ID: a84b4c76e66710@pc33.atlanta.com\nCSeq: 314159 INVITE\nContact: <sip:alice@pc33.atlanta.com>\nContent-Length: 0\n\n",
			expected: "INVITE sip:bob@biloxi.com SIP/2.0\r\nVia: SIP/2.0/UDP pc33.atlanta.com;branch=z9hG4bK776asdhds\r\nMax-Forwards: 70\r\nTo: Bob <sip:bob@biloxi.com>\r\nFrom: Alice <sip:alice@atlanta.com>;tag=1928301774\r\nCall-ID: a84b4c76e66710@pc33.atlanta.com\r\nCSeq: 314159 INVITE\r\nContact: <sip:alice@pc33.atlanta.com>\r\nContent-Length: 0\r\n\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeCRLF([]byte(tt.input))
			if !bytes.Equal(result, []byte(tt.expected)) {
				t.Errorf("normalizeCRLF(%q) =\n  %q\nwant:\n  %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestCRLFPacketConn(t *testing.T) {
	// Create a real UDP connection pair
	serverAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverConn, err := net.ListenUDP("udp", serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()

	clientConn, err := net.DialUDP("udp", nil, serverConn.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	// Wrap server conn with CRLF normalizer
	wrapped := &crlfPacketConn{PacketConn: serverConn}

	// Send a SIP message with bare \n
	msg := "INVITE sip:bob@example.com SIP/2.0\nVia: SIP/2.0/UDP host\nCall-ID: test123\n\n"
	_, err = clientConn.Write([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}

	// Read through the normalizer
	buf := make([]byte, 4096)
	serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := wrapped.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}

	expected := "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP host\r\nCall-ID: test123\r\n\r\n"
	if string(buf[:n]) != expected {
		t.Errorf("got:\n  %q\nwant:\n  %q", string(buf[:n]), expected)
	}
}

func TestCRLFConn(t *testing.T) {
	// Create a TCP listener
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	// Wrap with CRLF normalizer
	wrapped := &crlfListener{Listener: listener}

	// Send data with bare \n from a client
	msg := "INVITE sip:bob@example.com SIP/2.0\nVia: SIP/2.0/TCP host\n\n"
	expected := "INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/TCP host\r\n\r\n"

	go func() {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte(msg))
	}()

	conn, err := wrapped.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	if string(buf[:n]) != expected {
		t.Errorf("got:\n  %q\nwant:\n  %q", string(buf[:n]), expected)
	}
}

func BenchmarkNormalizeCRLF_AlreadyCorrect(b *testing.B) {
	data := []byte("INVITE sip:bob@example.com SIP/2.0\r\nVia: SIP/2.0/UDP host\r\nCall-ID: test\r\n\r\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		normalizeCRLF(data)
	}
}

func BenchmarkNormalizeCRLF_NeedsFixing(b *testing.B) {
	data := []byte("INVITE sip:bob@example.com SIP/2.0\nVia: SIP/2.0/UDP host\nCall-ID: test\n\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		normalizeCRLF(data)
	}
}
