package healthcheck

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
)

func TestPingSendsProxyHeaderWhenEnabled(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	headerCh := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			errCh <- err
			return
		}
		headerCh <- line
		errCh <- nil
	}()

	target := core.Target{
		Host: "127.0.0.1",
		Port: strconv.Itoa(ln.Addr().(*net.TCPAddr).Port),
	}
	results := make(chan CheckResult, 1)
	ping(target, config.HealthcheckConfig{
		Timeout:       "2s",
		ProxyProtocol: &config.ProxyProtocol{Version: "1"},
	}, results)

	result := <-results
	if result.Status != Healthy {
		t.Fatalf("expected healthy, got %v", result.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server read failed: %v", err)
	}

	header := <-headerCh
	if !strings.HasPrefix(header, "PROXY TCP4 ") {
		t.Fatalf("expected proxy header, got %q", header)
	}
}
