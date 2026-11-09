package discovery

/**
 * static.go - static list discovery implementation
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/utils/parsers"
)

/**
 * Creates new static discovery
 */
func NewStaticDiscovery(cfg config.DiscoveryConfig) interface{} {
	if cfg.Interval == "" {
		cfg.Interval = "0"
		if cfg.StaticDiscoveryConfig != nil && parsers.HasResolveBackend(cfg.StaticList) {
			cfg.Interval = "30s"
		}
	}

	d := Discovery{
		opts:  DiscoveryOpts{0},
		cfg:   cfg,
		fetch: newStaticFetch(net.DefaultResolver.LookupIPAddr),
	}

	return &d
}

// newStaticFetch owns the last successful IP list for each hostname. The discovery
// loop calls it serially; no cache is shared between listeners or config reloads.
func newStaticFetch(lookup func(context.Context, string) ([]net.IPAddr, error)) FetchFunc {
	cache := make(map[string][]string)
	return func(cfg config.DiscoveryConfig) (*[]core.Backend, error) {
		log := logging.For("discovery/static")
		timeout, err := time.ParseDuration(cfg.Timeout)
		if err != nil || timeout <= 0 {
			timeout = 5 * time.Second
		}

		var backends []core.Backend
		if cfg.StaticDiscoveryConfig == nil {
			return &backends, nil
		}
		// Resolve each hostname only once per pass, even across different ports.
		queried := make(map[string]bool)
		for _, line := range cfg.StaticList {
			if strings.TrimSpace(line) == "" {
				continue
			}
			backend, err := parsers.ParseBackendDefault(line)
			if err != nil {
				log.Warn(err)
				continue
			}
			if !backend.Resolve {
				backends = append(backends, *backend)
				continue
			}

			host := backend.Host
			if !queried[host] {
				queried[host] = true
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				ips, lookupErr := lookup(ctx, host)
				cancel()
				var hosts []string
				seen := make(map[string]bool)
				for _, ip := range ips {
					if ip.IP.To16() == nil {
						continue
					}
					address := ip.String()
					if ip.IP.To4() == nil {
						address = "[" + address + "]"
					}
					if !seen[address] {
						seen[address] = true
						hosts = append(hosts, address)
					}
				}
				if lookupErr == nil && len(hosts) == 0 {
					lookupErr = fmt.Errorf("no IP addresses resolved for %s", host)
				}
				if lookupErr != nil {
					log.Warn("DNS lookup failed for ", host, ": ", lookupErr)
					// Apply failpolicy per hostname so unrelated backends survive.
					if cfg.Failpolicy == "setempty" {
						delete(cache, host)
					}
				} else {
					cache[host] = hosts
				}
			}
			for _, address := range cache[host] {
				resolved := *backend
				resolved.Host = address
				resolved.Resolve = false
				backends = append(backends, resolved)
			}
		}
		return &backends, nil
	}
}
