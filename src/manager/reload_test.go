package manager

import (
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
