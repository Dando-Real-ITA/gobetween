package manager

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/yyyar/gobetween/config"
)

func TestPlanReloadClassifiesServerChanges(t *testing.T) {
	current := map[string]config.Server{
		"same": {
			Bind: "127.0.0.1:3000",
		},
		"modified": {
			Bind: "127.0.0.1:3001",
		},
		"removed": {
			Bind: "127.0.0.1:3002",
		},
	}

	desired := map[string]config.Server{
		"same": {
			Bind: "127.0.0.1:3000",
		},
		"modified": {
			Bind: "127.0.0.1:4001",
		},
		"added": {
			Bind: "127.0.0.1:3003",
		},
	}

	plan := planReload(current, desired)

	if len(plan.Added) != 1 || plan.Added[0] != "added" {
		t.Fatalf("unexpected added servers: %#v", plan.Added)
	}
	if len(plan.Modified) != 1 || plan.Modified[0] != "modified" {
		t.Fatalf("unexpected modified servers: %#v", plan.Modified)
	}
	if len(plan.Removed) != 1 || plan.Removed[0] != "removed" {
		t.Fatalf("unexpected removed servers: %#v", plan.Removed)
	}
}

func TestPrepareExpandedServersAppliesDefaultsToComparison(t *testing.T) {
	defaults := normalizeDefaults(config.ConnectionOptions{
		MaxConnections: intPtr(25),
	})

	cfg := config.Config{
		Servers: map[string]config.Server{
			"app": {
				Bind:      "127.0.0.1:3000",
				Protocol:  "tcp",
				Balance:   "roundrobin",
				Discovery: &config.DiscoveryConfig{Kind: "static", StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{"127.0.0.1:8080"}}},
			},
		},
	}

	prepared, _, err := prepareExpandedServers(cfg, defaults)
	if err != nil {
		t.Fatalf("prepareExpandedServers returned error: %v", err)
	}

	server := prepared["app"]
	if server.MaxConnections == nil || *server.MaxConnections != 25 {
		t.Fatalf("expected default max_connections to be applied, got %#v", server.MaxConnections)
	}
}

func intPtr(value int) *int {
	return &value
}

func TestReloadRecreatesModifiedServer(t *testing.T) {
	cleanupManagerState(t)
	t.Cleanup(func() { cleanupManagerState(t) })

	initialBind := reserveAddr(t)
	reloadedBind := reserveAddr(t)

	Initialize(testConfigWithBind("app", initialBind))

	if err := Reload(testConfigWithBind("app", reloadedBind)); err != nil {
		t.Fatalf("reload returned error: %v", err)
	}

	servers.RLock()
	srv, ok := servers.m["app"]
	servers.RUnlock()
	if !ok {
		t.Fatal("server app was not recreated")
	}

	if got := srv.Cfg().Bind; got != reloadedBind {
		t.Fatalf("unexpected bind after reload: got %s want %s", got, reloadedBind)
	}

	if err := canListen(initialBind); err != nil {
		t.Fatalf("expected old bind to be released, but it is still busy: %v", err)
	}

	if err := canListen(reloadedBind); !errors.Is(err, errAddrInUse) {
		t.Fatalf("expected new bind to be owned by server, got err=%v", err)
	}
}

func TestReloadRollsBackWhenRecreateFails(t *testing.T) {
	cleanupManagerState(t)
	t.Cleanup(func() { cleanupManagerState(t) })

	originalBind := reserveAddr(t)
	failingBind := reserveAddr(t)

	Initialize(testConfigWithBind("app", originalBind))

	blocker, err := net.Listen("tcp", failingBind)
	if err != nil {
		t.Fatalf("failed to reserve failing bind %s: %v", failingBind, err)
	}
	t.Cleanup(func() { _ = blocker.Close() })

	err = Reload(testConfigWithBind("app", failingBind))
	if err == nil {
		t.Fatal("expected reload to fail when replacement bind is unavailable")
	}

	servers.RLock()
	srv, ok := servers.m["app"]
	servers.RUnlock()
	if !ok {
		t.Fatal("server app missing after failed reload")
	}

	if got := srv.Cfg().Bind; got != originalBind {
		t.Fatalf("rollback did not restore previous server bind: got %s want %s", got, originalBind)
	}

	if err := canListen(originalBind); !errors.Is(err, errAddrInUse) {
		t.Fatalf("expected original bind to be restored and busy, got err=%v", err)
	}
}

func testConfigWithBind(name, bind string) config.Config {
	return config.Config{
		Defaults: normalizeDefaults(config.ConnectionOptions{MaxConnections: intPtr(100)}),
		Servers: map[string]config.Server{
			name: {
				Bind:      bind,
				Protocol:  "tcp",
				Balance:   "roundrobin",
				Discovery: &config.DiscoveryConfig{Kind: "static", StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{"127.0.0.1:65534"}}},
			},
		},
	}
}

func cleanupManagerState(t *testing.T) {
	t.Helper()

	operations.Lock()
	defer operations.Unlock()

	servers.Lock()
	for name, srv := range servers.m {
		srv.Stop()
		delete(servers.m, name)
	}
	servers.Unlock()

	defaults = config.ConnectionOptions{}
	services = nil
	originalCfg = config.Config{}
}

func reserveAddr(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve address: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("failed to release reserved address %s: %v", addr, err)
	}
	return addr
}

var errAddrInUse = errors.New("address already in use")

func canListen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if strings.Contains(err.Error(), "address already in use") {
			return errAddrInUse
		}
		return fmt.Errorf("listen on %s failed: %w", addr, err)
	}
	return ln.Close()
}
