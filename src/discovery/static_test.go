package discovery

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
)

func staticConfig(lines ...string) config.DiscoveryConfig {
	return config.DiscoveryConfig{
		Kind:                  "static",
		StaticDiscoveryConfig: &config.StaticDiscoveryConfig{StaticList: lines},
	}
}

func ipAnswers(hosts ...string) []net.IPAddr {
	var result []net.IPAddr
	for _, host := range hosts {
		result = append(result, net.IPAddr{IP: net.ParseIP(host)})
	}
	return result
}

func backendAddresses(backends *[]core.Backend) []string {
	var result []string
	for _, backend := range *backends {
		result = append(result, backend.Address())
	}
	return result
}

func TestStaticResolveRefresh(t *testing.T) {
	answers := ipAnswers("192.0.2.1", "2001:db8::1", "192.0.2.1")
	calls := 0
	fetch := newStaticFetch(func(ctx context.Context, host string) ([]net.IPAddr, error) {
		calls++
		if host != "proxy.internal" {
			t.Fatalf("unexpected hostname %q", host)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("lookup must have a deadline")
		}
		return answers, nil
	})
	cfg := staticConfig(
		"proxy.internal:0 weight=10 priority=10 max_connections=12 sni=app.internal resolve",
		"proxy.internal:8080 resolve",
		"127.0.0.1:9000",
	)
	backends, err := fetch(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.1:0", "[2001:db8::1]:0", "192.0.2.1:8080", "[2001:db8::1]:8080", "127.0.0.1:9000"}
	if got := backendAddresses(backends); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, b := range (*backends)[:2] {
		if b.Weight != 10 || b.Priority != 10 || b.MaxConnections != 12 || b.Sni != "app.internal" || b.Resolve || !b.Stats.Live {
			t.Fatalf("metadata not preserved: %+v", b)
		}
	}
	if calls != 1 {
		t.Fatalf("expected one lookup per distinct hostname, got %d", calls)
	}
	answers = ipAnswers("192.0.2.2")
	backends, err = fetch(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"192.0.2.2:0", "192.0.2.2:8080", "127.0.0.1:9000"}
	if got := backendAddresses(backends); !reflect.DeepEqual(got, want) {
		t.Fatalf("refresh got %v, want %v", got, want)
	}
}

func TestStaticResolveFailurePolicies(t *testing.T) {
	for _, policy := range []string{"", "keeplast", "setempty"} {
		t.Run(policy, func(t *testing.T) {
			for _, emptyAnswer := range []bool{false, true} {
				failed := false
				fetch := newStaticFetch(func(_ context.Context, host string) ([]net.IPAddr, error) {
					if failed && host == "bad.internal" {
						if emptyAnswer {
							return nil, nil
						}
						return nil, errors.New("temporary DNS failure")
					}
					return ipAnswers("192.0.2.1"), nil
				})
				cfg := staticConfig("bad.internal:80 resolve", "good.internal:81 resolve", "127.0.0.1:82")
				cfg.Failpolicy = policy
				if _, err := fetch(cfg); err != nil {
					t.Fatal(err)
				}
				failed = true
				backends, err := fetch(cfg)
				if err != nil {
					t.Fatal(err)
				}
				want := []string{"192.0.2.1:80", "192.0.2.1:81", "127.0.0.1:82"}
				if policy == "setempty" {
					want = want[1:]
				}
				if got := backendAddresses(backends); !reflect.DeepEqual(got, want) {
					t.Fatalf("got %v, want %v", got, want)
				}
				failed = false
				backends, _ = fetch(cfg)
				if len(*backends) != 3 {
					t.Fatal("failed hostname did not recover")
				}
			}
		})
	}
}

func TestStaticResolveTimeoutAndRecovery(t *testing.T) {
	failed := true
	fetch := newStaticFetch(func(ctx context.Context, _ string) ([]net.IPAddr, error) {
		if failed {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return ipAnswers("192.0.2.2"), nil
	})
	cfg := staticConfig("proxy.internal:80 resolve")
	cfg.Timeout = "1ms"
	backends, err := fetch(cfg)
	if err != nil || len(*backends) != 0 {
		t.Fatalf("initial lookup failure: backends=%v err=%v", backends, err)
	}
	failed = false
	backends, err = fetch(cfg)
	if err != nil || len(*backends) != 1 {
		t.Fatalf("recovery: backends=%v err=%v", backends, err)
	}
}

func TestStaticResolveCachesAreIndependent(t *testing.T) {
	first := newStaticFetch(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return ipAnswers("192.0.2.1"), nil
	})
	cfg := staticConfig("proxy.internal:80 resolve")
	if _, err := first(cfg); err != nil {
		t.Fatal(err)
	}
	second := newStaticFetch(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return nil, errors.New("DNS unavailable")
	})
	backends, err := second(cfg)
	if err != nil || len(*backends) != 0 {
		t.Fatalf("new discovery inherited another discovery's cache: %v %v", backends, err)
	}
}

func TestStaticDiscoveryIntervalDefaults(t *testing.T) {
	for _, tc := range []struct{ line, interval, want string }{
		{"proxy.internal:80 resolve", "", "30s"},
		{"proxy.internal:80 resolve", "1s", "1s"},
		{"proxy.internal:80 resolve", "0", "0"},
		{"127.0.0.1:80", "", "0"},
	} {
		cfg := staticConfig(tc.line)
		cfg.Interval = tc.interval
		d := NewStaticDiscovery(cfg).(*Discovery)
		if d.cfg.Interval != tc.want {
			t.Fatalf("%q interval=%q got %q, want %q", tc.line, tc.interval, d.cfg.Interval, tc.want)
		}
	}
}

func TestStaticDiscoveryPollsAndStops(t *testing.T) {
	cfg := staticConfig("proxy.internal:80 resolve")
	cfg.Interval = "1ms"
	d := NewStaticDiscovery(cfg).(*Discovery)
	calls := 0
	d.fetch = newStaticFetch(func(_ context.Context, _ string) ([]net.IPAddr, error) {
		calls++
		if calls == 1 {
			return ipAnswers("192.0.2.1"), nil
		}
		return ipAnswers("192.0.2.2"), nil
	})
	d.Start()
	defer d.Stop()
	for _, want := range []string{"192.0.2.1:80", "192.0.2.2:80"} {
		select {
		case backends := <-d.Discover():
			if len(backends) != 1 || backends[0].Address() != want {
				t.Fatalf("got %v, want %s", backends, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("discovery did not publish update")
		}
	}
}

func TestStaticDiscoveryStopsWhilePublishing(t *testing.T) {
	cfg := staticConfig("127.0.0.1:80")
	cfg.Interval = "1ms"
	d := NewStaticDiscovery(cfg).(*Discovery)
	fetched := make(chan struct{})
	d.fetch = func(config.DiscoveryConfig) (*[]core.Backend, error) {
		close(fetched)
		return &[]core.Backend{}, nil
	}
	d.Start()
	<-fetched
	stopped := make(chan struct{})
	go func() {
		d.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("discovery stop blocked without an update consumer")
	}
}
