package proxyprotocol

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"

	proxyproto "github.com/pires/go-proxyproto"
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

func TestSendProxyProtocolV2AddrsTCP4(t *testing.T) {
	backend, peer := net.Pipe()
	defer backend.Close()
	defer peer.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- SendProxyProtocolV2Addrs(
			&net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 12345},
			&net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 80},
			backend,
		)
	}()

	header, err := proxyproto.Read(bufio.NewReader(peer))
	if err != nil {
		t.Fatalf("failed reading proxy header: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	if header.Version != 2 {
		t.Fatalf("unexpected header version: got %d want 2", header.Version)
	}
	if header.TransportProtocol != proxyproto.TCPv4 {
		t.Fatalf("unexpected transport protocol: got %v want %v", header.TransportProtocol, proxyproto.TCPv4)
	}
}

func TestPrependProxyProtocolV2Datagram(t *testing.T) {
	payload := []byte("PING")
	packet, err := PrependProxyProtocolV2Datagram(
		&net.UDPAddr{IP: net.ParseIP("203.0.113.10"), Port: 12345},
		&net.UDPAddr{IP: net.ParseIP("198.51.100.20"), Port: 80},
		payload,
	)
	if err != nil {
		t.Fatalf("unexpected prepend error: %v", err)
	}

	reader := bufio.NewReader(bytes.NewReader(packet))
	header, err := proxyproto.Read(reader)
	if err != nil {
		t.Fatalf("failed reading proxy header: %v", err)
	}

	if header.Version != 2 {
		t.Fatalf("unexpected header version: got %d want 2", header.Version)
	}
	if header.TransportProtocol != proxyproto.UDPv4 {
		t.Fatalf("unexpected transport protocol: got %v want %v", header.TransportProtocol, proxyproto.UDPv4)
	}

	rest := make([]byte, len(payload))
	if _, err := reader.Read(rest); err != nil {
		t.Fatalf("failed reading payload: %v", err)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload mismatch: got %q want %q", rest, payload)
	}
}

func TestPrependProxyProtocolV2DatagramRejectsTCPAddr(t *testing.T) {
	_, err := PrependProxyProtocolV2Datagram(
		&net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 12345},
		&net.TCPAddr{IP: net.ParseIP("198.51.100.20"), Port: 80},
		[]byte("PING"),
	)
	if err == nil {
		t.Fatal("expected error for non-datagram addresses")
	}
}
