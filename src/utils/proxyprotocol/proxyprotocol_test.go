package proxyprotocol

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func TestSendProxyProtocolV1AddrsTCP4(t *testing.T) {
	backend, peer := net.Pipe()
	defer backend.Close()
	defer peer.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- SendProxyProtocolV1Addrs(
			&net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 12345},
			&net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 80},
			backend,
		)
	}()

	line, err := bufio.NewReader(peer).ReadString('\n')
	if err != nil {
		t.Fatalf("failed reading proxy header: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if !strings.HasPrefix(line, "PROXY TCP4 203.0.113.10 198.51.100.20 12345 80") {
		t.Fatalf("unexpected proxy header: %q", line)
	}
}

func TestSendProxyProtocolV1AddrsInvalidAddr(t *testing.T) {
	backend, peer := net.Pipe()
	defer backend.Close()
	defer peer.Close()

	err := SendProxyProtocolV1Addrs(
		&net.UnixAddr{Name: "/tmp/a.sock", Net: "unix"},
		&net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 80},
		backend,
	)
	if err == nil {
		t.Fatal("expected error for invalid source address")
	}
}
