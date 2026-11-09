package scheduler

import (
	"testing"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/healthcheck"
	"github.com/yyyar/gobetween/stats"
)

func TestBackendRefreshPreservesStatsAndDrainsRemovedTargets(t *testing.T) {
	s := &Scheduler{
		backends:     make(map[core.Target]*core.Backend),
		Healthcheck:  healthcheck.New("none", config.HealthcheckConfig{}),
		StatsHandler: &stats.Handler{Name: "dns-refresh-test"},
	}
	kept := core.Backend{Target: core.Target{Host: "192.0.2.1", Port: "80"}, Weight: 1}
	removed := core.Backend{Target: core.Target{Host: "192.0.2.2", Port: "80"}}
	idle := core.Backend{Target: core.Target{Host: "192.0.2.3", Port: "80"}}
	s.HandleBackendsUpdate([]core.Backend{kept, removed, idle})
	s.backends[kept.Target].Stats.TotalConnections = 42
	s.backends[kept.Target].Stats.Live = false
	s.backends[removed.Target].Stats.ActiveConnections = 1
	kept.Weight = 10
	added := core.Backend{Target: core.Target{Host: "192.0.2.4", Port: "80"}}
	s.HandleBackendsUpdate([]core.Backend{kept, added})
	if b := s.backends[kept.Target]; b.Weight != 10 || b.Stats.TotalConnections != 42 || b.Stats.Live || !b.Stats.Discovered {
		t.Fatalf("existing backend state lost: %+v", b)
	}
	if b := s.backends[added.Target]; b == nil || !b.Stats.Discovered {
		t.Fatalf("new backend missing: %+v", b)
	}
	if _, exists := s.backends[idle.Target]; exists {
		t.Fatal("removed idle backend retained")
	}
	if b := s.backends[removed.Target]; b == nil || b.Stats.Discovered || b.Stats.ActiveConnections != 1 {
		t.Fatalf("removed active backend should drain: %+v", b)
	}
	s.backends[removed.Target].Stats.ActiveConnections = 0
	s.HandleBackendsUpdate([]core.Backend{kept, added})
	if _, exists := s.backends[removed.Target]; exists {
		t.Fatal("drained backend retained")
	}
}
