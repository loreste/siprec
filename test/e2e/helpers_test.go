package e2e

import (
	"fmt"
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

func tcpListenerOrSkip(t *testing.T, listen func() error) {
	t.Helper()

	if err := listen(); err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("skipping TCP/TLS listener setup: %v", err)
		}
		t.Fatalf("listener setup failed: %v", err)
	}
}

func skipIfNotPermitted(t *testing.T, err error, msg string, args ...interface{}) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "operation not permitted") {
		t.Skipf("skipping due to sandbox restrictions: %v", err)
	}
	formatted := fmt.Sprintf(msg, args...)
	t.Fatalf("%s: %v", formatted, err)
}
