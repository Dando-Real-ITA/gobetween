package healthcheck

import (
	"bufio"
	"net"
	"strconv"
	"testing"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
)

func TestPingSendsProxyHeaderWhenEnabled(t *testing.T) {
	testPingSendsProxyHeaderWhenEnabled(t, "1")
}

func TestPingSendsProxyV2HeaderWhenEnabled(t *testing.T) {
	testPingSendsProxyHeaderWhenEnabled(t, "2")
}

func testPingSendsProxyHeaderWhenEnabled(t *testing.T, version string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	headerCh := make(chan *proxyproto.Header, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		header, err := proxyproto.Read(bufio.NewReader(conn))
		if err != nil {
			errCh <- err
			return
		}
		headerCh <- header
		errCh <- nil
	}()

	target := core.Target{
		Host: "127.0.0.1",
		Port: strconv.Itoa(ln.Addr().(*net.TCPAddr).Port),
	}
	results := make(chan CheckResult, 1)
	ping(target, config.HealthcheckConfig{
		Timeout:       "2s",
		ProxyProtocol: &config.ProxyProtocol{Version: version},
	}, results)

	result := <-results
	if result.Status != Healthy {
		t.Fatalf("expected healthy, got %v", result.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server read failed: %v", err)
	}

	header := <-headerCh
	wantVersion := byte(1)
	if version == "2" {
		wantVersion = 2
	}
	if header.Version != wantVersion {
		t.Fatalf("unexpected proxy header version: got %d want %s", header.Version, version)
	}
	if header.Command != proxyproto.PROXY {
		t.Fatalf("expected proxy command, got %v", header.Command)
	}
}
