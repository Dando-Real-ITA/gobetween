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
		MaxConnections:            intPtr(25),
		ClientTcpKeepalive:        boolPtr(false),
		ClientTcpKeepalivePeriod:  stringPtr("30s"),
		BackendTcpKeepalive:       boolPtr(false),
		BackendTcpKeepalivePeriod: stringPtr("45s"),
		Sources:                   stringPtr("2001:db8::/64"),
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

	if server.Sources == nil || *server.Sources != "2001:db8::/64" {
		t.Fatalf("expected default sources to be applied, got %#v", server.Sources)
	}

	if server.ClientTcpKeepalive == nil || *server.ClientTcpKeepalive {
		t.Fatalf("expected default client_tcp_keepalive to be false, got %#v", server.ClientTcpKeepalive)
	}

	if server.ClientTcpKeepalivePeriod == nil || *server.ClientTcpKeepalivePeriod != "30s" {
		t.Fatalf("expected default client_tcp_keepalive_period to be applied, got %#v", server.ClientTcpKeepalivePeriod)
	}

	if server.BackendTcpKeepalive == nil || *server.BackendTcpKeepalive {
		t.Fatalf("expected default backend_tcp_keepalive to be false, got %#v", server.BackendTcpKeepalive)
	}

	if server.BackendTcpKeepalivePeriod == nil || *server.BackendTcpKeepalivePeriod != "45s" {
		t.Fatalf("expected default backend_tcp_keepalive_period to be applied, got %#v", server.BackendTcpKeepalivePeriod)
	}
}

func intPtr(value int) *int {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func TestPrepareConfigStaticResolveDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, line, interval, timeout, wantInterval, wantTimeout string
		invalid                                                  bool
	}{
		{name: "resolve defaults", line: "proxy.internal:0 resolve", wantInterval: "30s", wantTimeout: "5s"},
		{name: "plain static", line: "127.0.0.1:8080", wantInterval: "0", wantTimeout: "0"},
		{name: "explicit values", line: "proxy.internal:80 resolve", interval: "2s", timeout: "1s", wantInterval: "2s", wantTimeout: "1s"},
		{name: "explicit one shot", line: "proxy.internal:80 resolve", interval: "0", timeout: "0", wantInterval: "0", wantTimeout: "0"},
		{name: "negative interval", line: "proxy.internal:80 resolve", interval: "-1s", invalid: true},
		{name: "invalid interval", line: "proxy.internal:80 resolve", interval: "bad", invalid: true},
		{name: "negative timeout", line: "proxy.internal:80 resolve", timeout: "-1s", invalid: true},
		{name: "invalid timeout", line: "proxy.internal:80 resolve", timeout: "bad", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := prepareConfig("app", config.Server{
				Bind:     "127.0.0.1:3000",
				Protocol: "tcp",
				Discovery: &config.DiscoveryConfig{
					Kind: "static", Interval: tc.interval, Timeout: tc.timeout,
					StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{tc.line}},
				},
			}, normalizeDefaults(config.ConnectionOptions{}))
			if tc.invalid {
				if err == nil {
					t.Fatal("expected invalid duration to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if server.Discovery.Interval != tc.wantInterval || server.Discovery.Timeout != tc.wantTimeout {
				t.Fatalf("got interval=%q timeout=%q, want %q %q", server.Discovery.Interval, server.Discovery.Timeout, tc.wantInterval, tc.wantTimeout)
			}
		})
	}
}

func TestMatchPortPreservesStaticResolve(t *testing.T) {
	discovery := &config.DiscoveryConfig{
		Kind: "static",
		StaticDiscoveryConfig: &config.StaticDiscoveryConfig{
			StaticList: []string{"proxy.internal:0 weight=10 priority=10 resolve"},
		},
	}
	rewritten := rewriteStaticListPorts(discovery, "8080")
	if got := rewritten.StaticList[0]; got != "proxy.internal:8080 weight=10 priority=10 resolve" {
		t.Fatalf("unexpected rewritten backend: %q", got)
	}
	if discovery.StaticList[0] != "proxy.internal:0 weight=10 priority=10 resolve" {
		t.Fatal("port rewrite modified original discovery config")
	}
}

func TestPrepareConfigRejectsInvalidSources(t *testing.T) {
	_, err := prepareConfig("app", config.Server{
		ConnectionOptions: config.ConnectionOptions{
			Sources: stringPtr("bad-cidr"),
		},
		Bind:      "127.0.0.1:3000",
		Protocol:  "tcp",
		Balance:   "roundrobin",
		Discovery: &config.DiscoveryConfig{Kind: "static", StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{"127.0.0.1:8080"}}},
	}, normalizeDefaults(config.ConnectionOptions{}))
	if err == nil {
		t.Fatal("expected invalid ipv6 sources to fail")
	}
}

func TestPrepareConfigRejectsSourcesForUDPTransparentMode(t *testing.T) {
	_, err := prepareConfig("app", config.Server{
		ConnectionOptions: config.ConnectionOptions{
			Sources: stringPtr("2001:db8::/64"),
		},
		Bind:     "127.0.0.1:3000",
		Protocol: "udp",
		Balance:  "roundrobin",
		Udp:      &config.Udp{Transparent: true, MaxRequests: 1},
		Discovery: &config.DiscoveryConfig{
			Kind: "static",
			StaticDiscoveryConfig: &config.StaticDiscoveryConfig{
				StaticList: []string{"127.0.0.1:8080"},
			},
		},
	}, normalizeDefaults(config.ConnectionOptions{}))
	if err == nil {
		t.Fatal("expected sources with udp transparent mode to fail")
	}
}

func TestPrepareConfigNormalizesTCPKeepaliveFieldsForUDP(t *testing.T) {
	server := config.Server{
		Bind:     "127.0.0.1:3000",
		Protocol: "udp",
		Balance:  "roundrobin",
		Udp:      &config.Udp{MaxRequests: 1},
		Discovery: &config.DiscoveryConfig{
			Kind: "static",
			StaticDiscoveryConfig: &config.StaticDiscoveryConfig{
				StaticList: []string{"127.0.0.1:8080"},
			},
		},
	}

	defaultsA := normalizeDefaults(config.ConnectionOptions{
		ClientTcpKeepalive:        boolPtr(false),
		ClientTcpKeepalivePeriod:  stringPtr("7s"),
		BackendTcpKeepalive:       boolPtr(false),
		BackendTcpKeepalivePeriod: stringPtr("9s"),
	})

	defaultsB := normalizeDefaults(config.ConnectionOptions{
		ClientTcpKeepalive:        boolPtr(true),
		ClientTcpKeepalivePeriod:  stringPtr("30s"),
		BackendTcpKeepalive:       boolPtr(true),
		BackendTcpKeepalivePeriod: stringPtr("45s"),
	})

	preparedA, err := prepareConfig("app", server, defaultsA)
	if err != nil {
		t.Fatalf("prepareConfig returned error for defaultsA: %v", err)
	}

	preparedB, err := prepareConfig("app", server, defaultsB)
	if err != nil {
		t.Fatalf("prepareConfig returned error for defaultsB: %v", err)
	}

	if preparedA.ClientTcpKeepalive != nil || preparedA.ClientTcpKeepalivePeriod != nil || preparedA.BackendTcpKeepalive != nil || preparedA.BackendTcpKeepalivePeriod != nil {
		t.Fatalf("expected UDP server to normalize TCP keepalive fields to nil, got %#v", preparedA.ConnectionOptions)
	}

	if preparedB.ClientTcpKeepalive != nil || preparedB.ClientTcpKeepalivePeriod != nil || preparedB.BackendTcpKeepalive != nil || preparedB.BackendTcpKeepalivePeriod != nil {
		t.Fatalf("expected UDP server to normalize TCP keepalive fields to nil, got %#v", preparedB.ConnectionOptions)
	}
}

func TestPrepareConfigAllowsProxyProtocolV2ForTCP(t *testing.T) {
	_, err := prepareConfig("app", config.Server{
		Bind:      "127.0.0.1:3000",
		Protocol:  "tcp",
		Balance:   "roundrobin",
		Discovery: &config.DiscoveryConfig{Kind: "static", StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: []string{"127.0.0.1:8080"}}},
		ProxyProtocol: &config.ProxyProtocol{
			Version: "2",
		},
	}, normalizeDefaults(config.ConnectionOptions{}))
	if err != nil {
		t.Fatalf("expected proxy protocol v2 for tcp to be accepted, got %v", err)
	}
}

func TestPrepareConfigAllowsProxyProtocolV2ForUDP(t *testing.T) {
	_, err := prepareConfig("app", config.Server{
		Bind:     "127.0.0.1:3000",
		Protocol: "udp",
		Balance:  "roundrobin",
		Udp:      &config.Udp{MaxRequests: 1},
		Discovery: &config.DiscoveryConfig{
			Kind: "static",
			StaticDiscoveryConfig: &config.StaticDiscoveryConfig{
				StaticList: []string{"127.0.0.1:8080"},
			},
		},
		ProxyProtocol: &config.ProxyProtocol{
			Version: "2",
		},
	}, normalizeDefaults(config.ConnectionOptions{}))
	if err != nil {
		t.Fatalf("expected proxy protocol v2 for udp to be accepted, got %v", err)
	}
}

func TestPrepareConfigRejectsProxyProtocolV1ForUDP(t *testing.T) {
	_, err := prepareConfig("app", config.Server{
		Bind:     "127.0.0.1:3000",
		Protocol: "udp",
		Balance:  "roundrobin",
		Udp:      &config.Udp{MaxRequests: 1},
		Discovery: &config.DiscoveryConfig{
			Kind: "static",
			StaticDiscoveryConfig: &config.StaticDiscoveryConfig{
				StaticList: []string{"127.0.0.1:8080"},
			},
		},
		ProxyProtocol: &config.ProxyProtocol{
			Version: "1",
		},
	}, normalizeDefaults(config.ConnectionOptions{}))
	if err == nil {
		t.Fatal("expected proxy protocol v1 for udp to be rejected")
	}
	if !strings.Contains(err.Error(), "version 2 is required") {
		t.Fatalf("unexpected error: %v", err)
	}
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
