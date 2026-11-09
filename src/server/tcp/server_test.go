package tcp

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/yyyar/gobetween/config"
)

func shutdownTestServer(t *testing.T, bind string) *Server {
	t.Helper()
	s, err := New(t.Name(), config.Server{
		Bind: bind, Protocol: "tcp", Balance: "roundrobin",
		Discovery: &config.DiscoveryConfig{
			Kind: "static", Interval: "0",
			StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{"127.0.0.1:80"}},
		},
		Healthcheck: &config.HealthcheckConfig{Kind: "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func stopTestServer(t *testing.T, s *Server) {
	t.Helper()
	var callers sync.WaitGroup
	for i := 0; i < 5; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			s.Stop()
		}()
	}
	done := make(chan struct{})
	go func() {
		callers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server shutdown blocked")
	}
}

func TestStopReleasesListenerAndIsIdempotent(t *testing.T) {
	s := shutdownTestServer(t, "127.0.0.1:0")
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	address := s.listener.Addr().String()
	stopTestServer(t, s)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("Stop returned before listener was released: %v", err)
	}
	listener.Close()
}

func TestListenFailureCanBeStoppedAgain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s := shutdownTestServer(t, listener.Addr().String())
	started := make(chan error, 1)
	go func() { started <- s.Start() }()
	select {
	case err := <-started:
		if err == nil {
			t.Fatal("expected occupied listen address to fail")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("failed Start blocked during cleanup")
	}
	stopTestServer(t, s)
}
