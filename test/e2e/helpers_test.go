package e2e

import (
	"net"
	"strings"
	"testing"
)

func listenUDPOrSkip(t *testing.T, addr *net.UDPAddr) *net.UDPConn {
	t.Helper()

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping UDP listener setup: %v", err)
		}
		t.Fatalf("failed to listen on %v: %v", addr, err)
	}

	return conn
}

func dialUDPOrSkip(t *testing.T, network string, laddr, raddr *net.UDPAddr) *net.UDPConn {
	t.Helper()

	conn, err := net.DialUDP(network, laddr, raddr)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping UDP dial: %v", err)
		}
		t.Fatalf("failed to dial %s to %v: %v", network, raddr, err)
	}

	return conn
}
