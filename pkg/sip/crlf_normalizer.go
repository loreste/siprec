package sip

import (
	"bytes"
	"net"
)

// normalizeCRLF replaces bare \n (not preceded by \r) with \r\n in SIP messages.
// This handles non-compliant devices that send LF-only line endings instead of
// the CRLF required by RFC 3261 Section 7.
func normalizeCRLF(data []byte) []byte {
	// Fast path: if no bare \n exists, return as-is
	if !bytes.Contains(data, []byte("\n")) {
		return data
	}

	// Check if all \n are already preceded by \r
	hasBareNewline := false
	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			hasBareNewline = true
			break
		}
	}
	if !hasBareNewline {
		return data
	}

	// Replace bare \n with \r\n
	var buf bytes.Buffer
	buf.Grow(len(data) + 64) // pre-allocate with some extra room
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' && (i == 0 || data[i-1] != '\r') {
			buf.WriteByte('\r')
		}
		buf.WriteByte(data[i])
	}
	return buf.Bytes()
}

// crlfPacketConn wraps a net.PacketConn to normalize bare \n to \r\n
// in incoming UDP packets before sipgo's parser processes them.
type crlfPacketConn struct {
	net.PacketConn
}

func (c *crlfPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if err != nil || n == 0 {
		return n, addr, err
	}

	normalized := normalizeCRLF(p[:n])
	if len(normalized) == n {
		// No change or same length — data is already in p
		return n, addr, nil
	}

	// Normalized data is longer; copy back if it fits
	if len(normalized) <= len(p) {
		copy(p, normalized)
		return len(normalized), addr, nil
	}

	// Extremely unlikely: normalized data exceeds buffer.
	// Copy what fits — sipgo will handle the truncation.
	copy(p, normalized)
	return len(p), addr, nil
}

// crlfListener wraps a net.Listener so that accepted connections
// normalize bare \n to \r\n in incoming TCP data.
type crlfListener struct {
	net.Listener
}

func (l *crlfListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &crlfConn{Conn: conn}, nil
}

// crlfConn wraps a net.Conn to normalize bare \n to \r\n on Read.
type crlfConn struct {
	net.Conn
	pending []byte // leftover normalized bytes from a previous read
}

func (c *crlfConn) Read(p []byte) (int, error) {
	// Drain any pending bytes from a previous normalization that expanded the data
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}

	n, err := c.Conn.Read(p)
	if n == 0 {
		return n, err
	}

	normalized := normalizeCRLF(p[:n])
	if len(normalized) == n {
		return n, err
	}

	// Normalized data is longer than what was read
	copied := copy(p, normalized)
	if copied < len(normalized) {
		// Store overflow for the next Read call
		c.pending = append(c.pending, normalized[copied:]...)
	}
	return copied, err
}
