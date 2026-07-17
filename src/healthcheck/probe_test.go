package healthcheck

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	proxyproto "github.com/pires/go-proxyproto"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
)

func TestProbeTCPWithProxyProtocolV2(t *testing.T) {
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
		reader := bufio.NewReader(conn)
		header, err := proxyproto.Read(reader)
		if err != nil {
			errCh <- err
			return
		}
		headerCh <- header

		buf := make([]byte, 4)
		if _, err := reader.Read(buf); err != nil {
			errCh <- err
			return
		}
		if !bytes.Equal(buf, []byte("PING")) {
			errCh <- fmt.Errorf("unexpected payload: got %q want %q", buf, "PING")
			return
		}
		if _, err := conn.Write([]byte("PONG")); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	target := core.Target{
		Host: "127.0.0.1",
		Port: strconv.Itoa(ln.Addr().(*net.TCPAddr).Port),
	}
	results := make(chan CheckResult, 1)
	probe(target, config.HealthcheckConfig{
		Timeout:       "2s",
		ProxyProtocol: &config.ProxyProtocol{Version: "2"},
		ProbeHealthcheckConfig: &config.ProbeHealthcheckConfig{
			ProbeProtocol: "tcp",
			ProbeStrategy: "starts_with",
			ProbeSend:     "PING",
			ProbeRecv:     "PONG",
		},
	}, results)

	result := <-results
	if result.Status != Healthy {
		t.Fatalf("expected healthy, got %v", result.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server failed: %v", err)
	}
	header := <-headerCh
	if header.Version != 2 {
		t.Fatalf("unexpected proxy header version: got %d want 2", header.Version)
	}
}

func TestProbeUDPWithProxyProtocolV2(t *testing.T) {
	ln, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		_ = ln.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, clientAddr, err := ln.ReadFromUDP(buf)
		if err != nil {
			errCh <- err
			return
		}

		reader := bufio.NewReader(bytes.NewReader(buf[:n]))
		header, err := proxyproto.Read(reader)
		if err != nil {
			errCh <- err
			return
		}
		if header.Version != 2 {
			errCh <- fmt.Errorf("unexpected header version: got %d want 2", header.Version)
			return
		}

		payload := make([]byte, 4)
		if _, err := reader.Read(payload); err != nil {
			errCh <- err
			return
		}
		if !bytes.Equal(payload, []byte("PING")) {
			errCh <- fmt.Errorf("unexpected payload: got %q want %q", payload, "PING")
			return
		}

		if _, err := ln.WriteToUDP([]byte("PONG"), clientAddr); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	target := core.Target{
		Host: "127.0.0.1",
		Port: strconv.Itoa(ln.LocalAddr().(*net.UDPAddr).Port),
	}
	results := make(chan CheckResult, 1)
	probe(target, config.HealthcheckConfig{
		Timeout:       "2s",
		ProxyProtocol: &config.ProxyProtocol{Version: "2"},
		ProbeHealthcheckConfig: &config.ProbeHealthcheckConfig{
			ProbeProtocol: "udp",
			ProbeStrategy: "starts_with",
			ProbeSend:     "PING",
			ProbeRecv:     "PONG",
		},
	}, results)

	result := <-results
	if result.Status != Healthy {
		t.Fatalf("expected healthy, got %v", result.Status)
	}

	if err := <-errCh; err != nil {
		t.Fatalf("server failed: %v", err)
	}
}
