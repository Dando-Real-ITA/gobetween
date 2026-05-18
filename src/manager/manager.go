package manager

/**
 * manager.go - manages servers
 *
 * @author Yaroslav Pogrebnyak <yyyaroslav@gmail.com>
 */

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/core"
	"github.com/yyyar/gobetween/logging"
	"github.com/yyyar/gobetween/server"
	"github.com/yyyar/gobetween/service"
	"github.com/yyyar/gobetween/utils/codec"
	"github.com/yyyar/gobetween/utils/profiler"
)

/* Map of app current servers */
var servers = struct {
	sync.RWMutex
	m map[string]core.Server
}{m: make(map[string]core.Server)}

var operations sync.Mutex

/* default configuration for server */
var defaults config.ConnectionOptions

/* services */
var services []core.Service

/* original cfg read from the file */
var originalCfg config.Config

/**
 * Initialize manager from the initial/default configuration
 */
func Initialize(cfg config.Config) {

	log := logging.For("manager")
	log.Info("Initializing...")

	originalCfg = cfg

	// save defaults for futher reuse
	defaults = normalizeDefaults(cfg.Defaults)

	//Initialize global sections
	initConfigGlobals(&cfg)

	//create services
	services = service.All(cfg)

	// Go through config and start servers for each server
	for name, serverCfg := range cfg.Servers {
		expanded, err := expandBinds(name, serverCfg)
		if err != nil {
			log.Fatal(err)
		}
		for ename, ecfg := range expanded {
			if err := create(ename, ecfg, defaults); err != nil {
				log.Fatal(err)
			}
		}
	}

	// Initialize profiler
	initProfiler(&cfg)

	log.Info("Initialized")
}

func normalizeDefaults(value config.ConnectionOptions) config.ConnectionOptions {
	if value.MaxConnections == nil {
		value.MaxConnections = new(int)
	}

	if value.ClientIdleTimeout == nil {
		value.ClientIdleTimeout = new(string)
		*value.ClientIdleTimeout = "0"
	}

	if value.BackendIdleTimeout == nil {
		value.BackendIdleTimeout = new(string)
		*value.BackendIdleTimeout = "0"
	}

	if value.BackendConnectionTimeout == nil {
		value.BackendConnectionTimeout = new(string)
		*value.BackendConnectionTimeout = "0"
	}

	return value
}

func initConfigGlobals(cfg *config.Config) {

	//acme
	if cfg.Acme != nil {
		if cfg.Acme.Challenge == "" {
			cfg.Acme.Challenge = "http"
		}

		if cfg.Acme.HttpBind == "" {
			cfg.Acme.HttpBind = "0.0.0.0:80"
		}

		if cfg.Acme.CacheDir == "" {
			cfg.Acme.CacheDir = "/tmp"
		}
	}
}

func initProfiler(cfg *config.Config) {
	if cfg.Profiler == nil {
		return
	}

	if !cfg.Profiler.Enabled {
		return
	}

	profiler.Start(cfg.Profiler.Bind)
}

/**
 * Dumps current [servers] section to
 * the config file
 */
func DumpConfig(format string) (string, error) {

	originalCfg.Servers = map[string]config.Server{}

	servers.RLock()
	for name, server := range servers.m {
		originalCfg.Servers[name] = server.Cfg()
	}
	servers.RUnlock()

	var out *string = new(string)
	if err := codec.Encode(originalCfg, out, format); err != nil {
		return "", err
	}

	return *out, nil
}

/**
 * Returns map of servers with configurations
 */
func All() map[string]config.Server {
	result := map[string]config.Server{}

	servers.RLock()
	for name, server := range servers.m {
		result[name] = server.Cfg()
	}
	servers.RUnlock()

	return result
}

/**
 * Returns server configuration by name
 */
func Get(name string) interface{} {

	servers.RLock()
	server, ok := servers.m[name]
	servers.RUnlock()

	if !ok {
		return nil
	}

	return server.Cfg()
}

/**
 * Create new server and launch it
 */
func Create(name string, cfg config.Server) error {
	operations.Lock()
	defer operations.Unlock()
	return create(name, cfg, defaults)
}

func create(name string, cfg config.Server, cfgDefaults config.ConnectionOptions) error {
	servers.Lock()
	defer servers.Unlock()

	if _, ok := servers.m[name]; ok {
		return errors.New("Server with this name already exists: " + name)
	}

	c, err := prepareConfig(name, cfg, cfgDefaults)
	if err != nil {
		return err
	}

	server, err := server.New(name, c)

	if err != nil {
		return err
	}

	for _, srv := range services {
		err = srv.Enable(server)
		if err != nil {
			return err
		}
	}

	if err = server.Start(); err != nil {
		return err
	}

	servers.m[name] = server

	return nil
}

/**
 * Delete server stopping all active connections
 */
func Delete(name string) error {
	operations.Lock()
	defer operations.Unlock()
	return deleteServer(name)
}

func deleteServer(name string) error {
	servers.Lock()
	defer servers.Unlock()

	server, ok := servers.m[name]
	if !ok {
		return errors.New("Server not found")
	}

	server.Stop()
	delete(servers.m, name)

	for _, s := range services {
		s.Disable(server)
	}

	return nil
}

type reloadPlan struct {
	Added    []string
	Modified []string
	Removed  []string
}

func Reload(cfg config.Config) error {
	operations.Lock()
	defer operations.Unlock()

	log := logging.For("manager")
	log.Info("Reloading configuration...")

	nextCfg := cfg
	initConfigGlobals(&nextCfg)
	nextDefaults := normalizeDefaults(cfg.Defaults)

	nextPrepared, nextRaw, err := prepareExpandedServers(nextCfg, nextDefaults)
	if err != nil {
		return err
	}

	currentPrepared := currentServerConfigs()
	currentRaw, err := rawExpandedServers(originalCfg)
	if err != nil {
		return err
	}

	plan := planReload(currentPrepared, nextPrepared)
	warnUnsupportedReloadChanges(log, originalCfg, cfg)

	if len(plan.Added) == 0 && len(plan.Modified) == 0 && len(plan.Removed) == 0 {
		defaults = nextDefaults
		originalCfg = mergeReloadableConfig(originalCfg, cfg)
		log.Info("Reload completed: no server changes detected")
		return nil
	}

	log.Infof("Reload plan: add=%v modify=%v remove=%v", plan.Added, plan.Modified, plan.Removed)

	replaced := append([]string{}, plan.Modified...)
	replaced = append(replaced, plan.Removed...)

	for _, name := range replaced {
		if err := deleteServer(name); err != nil {
			return err
		}
	}

	created := make([]string, 0, len(plan.Added)+len(plan.Modified))
	for _, name := range append(append([]string{}, plan.Added...), plan.Modified...) {
		if err := create(name, nextRaw[name], nextDefaults); err != nil {
			rollbackErr := rollbackReload(created, replaced, currentRaw, defaults)
			if rollbackErr != nil {
				return fmt.Errorf("reload failed for %s: %v (rollback failed: %v)", name, err, rollbackErr)
			}
			return fmt.Errorf("reload failed for %s: %v", name, err)
		}
		created = append(created, name)
	}

	defaults = nextDefaults
	originalCfg = mergeReloadableConfig(originalCfg, cfg)
	log.Info("Reload completed")
	return nil
}

func rollbackReload(created, replaced []string, currentRaw map[string]config.Server, currentDefaults config.ConnectionOptions) error {
	for i := len(created) - 1; i >= 0; i-- {
		if err := deleteServer(created[i]); err != nil {
			return err
		}
	}

	for _, name := range replaced {
		cfg, ok := currentRaw[name]
		if !ok {
			continue
		}
		if err := create(name, cfg, currentDefaults); err != nil {
			return err
		}
	}

	return nil
}

func currentServerConfigs() map[string]config.Server {
	result := map[string]config.Server{}

	servers.RLock()
	defer servers.RUnlock()

	for name, server := range servers.m {
		result[name] = server.Cfg()
	}

	return result
}

func rawExpandedServers(cfg config.Config) (map[string]config.Server, error) {
	result := map[string]config.Server{}

	for name, serverCfg := range cfg.Servers {
		expanded, err := expandBinds(name, serverCfg)
		if err != nil {
			return nil, err
		}
		for expandedName, expandedCfg := range expanded {
			if _, exists := result[expandedName]; exists {
				return nil, fmt.Errorf("server with this name already exists: %s", expandedName)
			}
			result[expandedName] = expandedCfg
		}
	}

	return result, nil
}

func prepareExpandedServers(cfg config.Config, cfgDefaults config.ConnectionOptions) (map[string]config.Server, map[string]config.Server, error) {
	raw, err := rawExpandedServers(cfg)
	if err != nil {
		return nil, nil, err
	}

	prepared := make(map[string]config.Server, len(raw))
	for name, serverCfg := range raw {
		ready, err := prepareConfig(name, serverCfg, cfgDefaults)
		if err != nil {
			return nil, nil, err
		}
		prepared[name] = ready
	}

	return prepared, raw, nil
}

func planReload(current, desired map[string]config.Server) reloadPlan {
	plan := reloadPlan{}

	for name, currentCfg := range current {
		desiredCfg, ok := desired[name]
		if !ok {
			plan.Removed = append(plan.Removed, name)
			continue
		}
		if !reflect.DeepEqual(currentCfg, desiredCfg) {
			plan.Modified = append(plan.Modified, name)
		}
	}

	for name := range desired {
		if _, ok := current[name]; !ok {
			plan.Added = append(plan.Added, name)
		}
	}

	sort.Strings(plan.Added)
	sort.Strings(plan.Modified)
	sort.Strings(plan.Removed)

	return plan
}

func mergeReloadableConfig(current, next config.Config) config.Config {
	current.Logging = next.Logging
	current.Defaults = next.Defaults
	current.Servers = next.Servers
	return current
}

func warnUnsupportedReloadChanges(log interface{ Warn(...interface{}) }, current, next config.Config) {
	if !reflect.DeepEqual(current.Api, next.Api) {
		log.Warn("Reload does not reconfigure the API server; restart required for [api] changes")
	}
	if !reflect.DeepEqual(current.Metrics, next.Metrics) {
		log.Warn("Reload does not reconfigure metrics; restart required for [metrics] changes")
	}
	if !reflect.DeepEqual(current.Profiler, next.Profiler) {
		log.Warn("Reload does not reconfigure profiler; restart required for [profiler] changes")
	}
	if !reflect.DeepEqual(current.Acme, next.Acme) {
		log.Warn("Reload does not reinitialize ACME settings; restart required for top-level [acme] changes")
	}
}

/**
 * Returns stats for the server
 */
func Stats(name string) interface{} {

	servers.Lock()
	server := servers.m[name]
	servers.Unlock()

	return server
}

/**
 * Prepare config (merge default configuration, and try to validate)
 * TODO: make validation better
 */
func prepareConfig(name string, server config.Server, defaults config.ConnectionOptions) (config.Server, error) {

	/* ----- Prerequisites ----- */

	if server.Bind == "" {
		return config.Server{}, errors.New("No bind specified for server " + name)
	}

	if len(server.Binds) > 0 {
		return config.Server{}, errors.New("Cannot use both 'bind' and 'binds' for server " + name + "; use one or the other")
	}

	if server.Discovery == nil {
		return config.Server{}, errors.New("No .discovery specified for server " + name)
	}

	if server.Healthcheck == nil {
		server.Healthcheck = &config.HealthcheckConfig{
			Kind:     "none",
			Interval: "0",
			Timeout:  "0",
		}
	}

	switch server.Healthcheck.Kind {
	case
		"ping",
		"probe",
		"exec",
		"none":
	default:
		return config.Server{}, errors.New("Not supported healthcheck type " + server.Healthcheck.Kind)
	}

	if server.Healthcheck.Interval == "" {
		server.Healthcheck.Interval = "0"
	}

	if server.Healthcheck.Timeout == "" {
		server.Healthcheck.Timeout = "0"
	}

	if server.Healthcheck.Fails <= 0 {
		server.Healthcheck.Fails = 1
	}

	if server.Healthcheck.Passes <= 0 {
		server.Healthcheck.Passes = 1
	}

	if server.Healthcheck.Kind != "none" {
		d, err := time.ParseDuration(server.Healthcheck.Interval)
		if err != nil {
			return config.Server{}, errors.New("Could not parse healtcheck interval: " + err.Error())
		}

		if d <= 0 {
			return config.Server{}, errors.New("Healthcheck interval should be greater than 0s")
		}
	}

	if server.Healthcheck.InitialStatus != nil {
		switch *server.Healthcheck.InitialStatus {
		case "healthy", "unhealthy":
		default:
			return config.Server{}, errors.New("Unsupported healthcheck initial_status")
		}
	}

	if server.Healthcheck.Kind == "probe" {

		switch server.Healthcheck.ProbeProtocol {
		case "tcp", "udp", "tls":
		default:
			return config.Server{}, errors.New("Unsupported probe_protocol")
		}

		if server.Healthcheck.ProbeSend == "" || server.Healthcheck.ProbeRecv == "" {
			return config.Server{}, errors.New("probe healthcheck should have both probe_send and probe_recv specified")
		}

		if server.Healthcheck.ProbeStrategy == "" {
			server.Healthcheck.ProbeStrategy = "starts_with"
		}

		var err error
		server.Healthcheck.ProbeSend, err = strconv.Unquote("\"" + server.Healthcheck.ProbeSend + "\"")
		if err != nil {
			return config.Server{}, errors.New("probe_send has invalid syntax " + err.Error())
		}

		switch server.Healthcheck.ProbeStrategy {
		case "starts_with":
			if server.Healthcheck.ProbeRecvLen > 0 {
				return config.Server{}, errors.New("probe_recv_len is redundant for 'starts_with' strategy")
			}

			var err error
			server.Healthcheck.ProbeRecv, err = strconv.Unquote("\"" + server.Healthcheck.ProbeRecv + "\"")
			if err != nil {
				return config.Server{}, errors.New("probe_recv has invalid syntax " + err.Error())
			}
		case "regexp":
			if server.Healthcheck.ProbeRecvLen == 0 {
				return config.Server{}, errors.New("probe_recv_len required")
			}

			_, err := regexp.Compile(server.Healthcheck.ProbeRecv)
			if err != nil {
				return config.Server{}, errors.New("probe_recv has invalid syntax " + err.Error())
			}
		default:
			return config.Server{}, errors.New("Unsupported probe_strategy " + server.Healthcheck.ProbeStrategy)
		}

	}

	if server.ProxyProtocol != nil {

		if server.Protocol != "tcp" {
			return config.Server{}, errors.New("proxy_protocol may be used only with 'tcp' protocol, not with " + server.Protocol)
		}

		if server.ProxyProtocol.Version == "" {
			return config.Server{}, errors.New("version field for proxy_protocol is not specified")
		}

		if server.ProxyProtocol.Version != "1" {
			return config.Server{}, errors.New("Unsupported proxy_protocol version " + server.ProxyProtocol.Version)
		}
	}

	if server.Sni != nil {

		if server.Sni.ReadTimeout == "" {
			server.Sni.ReadTimeout = "2s"
		}

		if server.Sni.UnexpectedHostnameStrategy == "" {
			server.Sni.UnexpectedHostnameStrategy = "default"
		}

		switch server.Sni.UnexpectedHostnameStrategy {
		case
			"default",
			"reject",
			"any":
		default:
			return config.Server{}, errors.New("Not supported sni unexprected hostname strategy " + server.Sni.UnexpectedHostnameStrategy)
		}

		if server.Sni.HostnameMatchingStrategy == "" {
			server.Sni.HostnameMatchingStrategy = "exact"
		}

		switch server.Sni.HostnameMatchingStrategy {
		case
			"exact",
			"regexp":
		default:
			return config.Server{}, errors.New("Not supported sni matching " + server.Sni.HostnameMatchingStrategy)
		}

		if _, err := time.ParseDuration(server.Sni.ReadTimeout); err != nil {
			return config.Server{}, errors.New("timeout parsing error")
		}
	}

	if _, err := time.ParseDuration(server.Healthcheck.Timeout); err != nil {
		return config.Server{}, errors.New("timeout parsing error")
	}

	if _, err := time.ParseDuration(server.Healthcheck.Interval); err != nil {
		return config.Server{}, errors.New("interval parsing error")
	}

	if server.BackendsTls != nil && ((server.BackendsTls.KeyPath == nil) != (server.BackendsTls.CertPath == nil)) {
		return config.Server{}, errors.New("backend_tls.cert_path and .key_path should be specified together")
	}

	if server.Tls != nil {

		if (len(server.Tls.AcmeHosts) == 0) && ((server.Tls.KeyPath == "") || (server.Tls.CertPath == "")) {
			return config.Server{}, errors.New("tls requires specify either acme hosts or both key and cert paths")
		}

	}

	/* ----- Connections params and overrides ----- */

	/* Protocol */
	switch server.Protocol {
	case "":
		server.Protocol = "tcp"
	case "tls":
		if server.Tls == nil {
			return config.Server{}, errors.New("Need tls section for tls protocol")
		}
		fallthrough
	case "tcp":
	case "udp":
		if server.BackendsTls != nil {
			return config.Server{}, errors.New("backends_tls should not be enabled for udp protocol")
		}

		if server.Udp == nil {
			server.Udp = &config.Udp{}
		}

		if server.Udp.MaxRequests == 0 && server.Udp.MaxResponses == 0 && server.ClientIdleTimeout == nil && server.BackendIdleTimeout == nil {
			return config.Server{}, errors.New("udp protocol requires to specify at least one of (client|backend)_idle_timeout, udp.max_requests, udp.max_responses")
		}

	default:
		return config.Server{}, errors.New("Not supported protocol " + server.Protocol)
	}

	/* Healthcheck and protocol match */

	if server.Healthcheck.Kind == "ping" && server.Protocol == "udp" {
		return config.Server{}, errors.New("Cant use ping healthcheck with udp server")
	}

	/* Balance */
	switch server.Balance {
	case
		"weight",
		"leastconn",
		"roundrobin",
		"leastbandwidth",
		"iphash1",
		"iphash":
	case "":
		server.Balance = "weight"
	default:
		return config.Server{}, errors.New("Not supported balance type " + server.Balance)
	}

	/* Discovery */
	switch server.Discovery.Failpolicy {
	case
		"keeplast",
		"setempty":
	case "":
		server.Discovery.Failpolicy = "keeplast"
	default:
		return config.Server{}, errors.New("Not supported failpolicy " + server.Discovery.Failpolicy)
	}

	if server.Discovery.Interval == "" {
		server.Discovery.Interval = "0"
	}

	if server.Discovery.Timeout == "" {
		server.Discovery.Timeout = "0"
	}

	/* SRV Discovery */
	if server.Discovery.Kind == "srv" {
		switch server.Discovery.SrvDnsProtocol {
		case
			"udp",
			"tcp":
		case "":
			server.Discovery.SrvDnsProtocol = "udp"
		default:
			return config.Server{}, errors.New("Not supported srv_dns_protocol " + server.Discovery.SrvDnsProtocol)
		}
	}

	/* LXD Discovery */
	if server.Discovery.Kind == "lxd" {

		if server.Discovery.LXDServerAddress == "" {
			return config.Server{}, errors.New("lxd_server_address is required" + server.Discovery.LXDServerAddress)
		}

		if !(strings.HasPrefix(server.Discovery.LXDServerAddress, "https:") ||
			strings.HasPrefix(server.Discovery.LXDServerAddress, "unix:")) {

			return config.Server{}, errors.New("lxd_server_address should start with either unix:// or https:// but got " + server.Discovery.LXDServerAddress)
		}

		if server.Discovery.LXDServerRemoteName == "" {
			server.Discovery.LXDServerRemoteName = "local"
		}

		if server.Discovery.LXDConfigDirectory == "" {
			server.Discovery.LXDConfigDirectory = os.ExpandEnv("$HOME/.config/lxc")
		}

		if server.Discovery.LXDContainerInterface == "" {
			server.Discovery.LXDContainerInterface = "eth0"
		}

		switch server.Discovery.LXDContainerAddressType {
		case
			"IPv4",
			"IPv6":
		case "":
			server.Discovery.LXDContainerAddressType = "IPv4"
		default:
			return config.Server{}, errors.New("Invalid lxd_container_address_type. Must be IPv4 or IPv6")
		}

	}

	/* TODO: Still need to decide how to get rid of this */

	if server.MaxConnections == nil {
		server.MaxConnections = new(int)
		*server.MaxConnections = *defaults.MaxConnections
	}

	if server.ClientIdleTimeout == nil {
		server.ClientIdleTimeout = new(string)
		*server.ClientIdleTimeout = *defaults.ClientIdleTimeout
	}

	if server.BackendIdleTimeout == nil {
		server.BackendIdleTimeout = new(string)
		*server.BackendIdleTimeout = *defaults.BackendIdleTimeout
	}

	if server.BackendConnectionTimeout == nil {
		server.BackendConnectionTimeout = new(string)
		*server.BackendConnectionTimeout = *defaults.BackendConnectionTimeout
	}

	return server, nil
}

/**
 * bindNameData holds the template variables available in bind_name_template.
 */
type bindNameData struct {
	Name  string
	Host  string
	Port  string
	Index int
}

/**
 * expandBinds splits a server config that uses the `binds` list into one
 * config entry per address.  If `binds` is empty the original config is
 * returned unchanged (backward-compatible path).
 *
 * Also supports bind_ips and bind_ports arrays for generating all IP:port
 * combinations (Cartesian product).
 */
func expandBinds(name string, cfg config.Server) (map[string]config.Server, error) {
	result := map[string]config.Server{}

	// Count how many bind configurations are provided
	hasBinds := len(cfg.Binds) > 0
	hasIPsPorts := len(cfg.BindIPs) > 0 || len(cfg.BindPorts) > 0
	hasSingleBind := cfg.Bind != ""

	// Validate mutually exclusive options
	if (hasSingleBind && hasBinds) || (hasSingleBind && hasIPsPorts) || (hasBinds && hasIPsPorts) {
		return nil, fmt.Errorf("server %s: 'bind', 'binds', and 'bind_ips'/'bind_ports' are mutually exclusive; use only one", name)
	}

	// If using bind_ips and bind_ports, generate the Cartesian product
	if hasIPsPorts {
		if len(cfg.BindIPs) == 0 {
			return nil, fmt.Errorf("server %s: 'bind_ips' must be non-empty when 'bind_ports' is specified", name)
		}
		if len(cfg.BindPorts) == 0 {
			return nil, fmt.Errorf("server %s: 'bind_ports' must be non-empty when 'bind_ips' is specified", name)
		}

		// Generate all IP:port combinations
		generatedBinds := make([]string, 0, len(cfg.BindIPs)*len(cfg.BindPorts))
		for _, ip := range cfg.BindIPs {
			for _, port := range cfg.BindPorts {
				// Format IPv6 addresses with brackets
				addr := formatBindAddress(ip, strconv.Itoa(port))
				generatedBinds = append(generatedBinds, addr)
			}
		}
		cfg.Binds = generatedBinds
		cfg.BindIPs = nil
		cfg.BindPorts = nil
	}

	if len(cfg.Binds) == 0 {
		// Legacy single-bind path — pass through unchanged.
		result[name] = cfg
		return result, nil
	}

	// Determine the name template.
	tmplStr := cfg.BindNameTemplate
	if tmplStr == "" {
		tmplStr = "{{.Name}}_{{.Host}}_{{.Port}}"
	}
	tmpl, err := template.New("bindname").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("invalid bind_name_template for server %s: %v", name, err)
	}

	log := logging.For("manager")

	for i, addr := range cfg.Binds {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid bind address %q in server %s: %v", addr, name, err)
		}

		// Sanitize the host for use in a server name (IPv6 addresses contain ':').
		sanitizedHost := strings.ReplaceAll(host, ":", "-")

		// Render the expanded server name.
		var buf bytes.Buffer
		data := bindNameData{Name: name, Host: sanitizedHost, Port: port, Index: i}
		var childName string
		if err := tmpl.Execute(&buf, data); err != nil {
			log.Warnf("Failed to render bind_name_template for server %s bind %s: %v; falling back to index-based name", name, addr, err)
			childName = fmt.Sprintf("%s_%d", name, i)
		} else {
			childName = buf.String()
		}

		child := cfg
		child.Bind = addr
		child.Binds = nil

		if cfg.MatchPort {
			child.Discovery = rewriteStaticListPorts(child.Discovery, port)
		}

		result[childName] = child
	}

	return result, nil
}

/**
 * formatBindAddress properly formats an IP address and port for binding.
 * Handles IPv6 addresses by wrapping them in brackets.
 */
func formatBindAddress(ip, port string) string {
	// Check if it's an IPv6 address (contains ':' and is not already bracketed)
	if strings.Contains(ip, ":") && !strings.HasPrefix(ip, "[") {
		return fmt.Sprintf("[%s]:%s", ip, port)
	}
	return net.JoinHostPort(ip, port)
}

/**
 * rewriteStaticListPorts returns a deep-copied DiscoveryConfig where every
 * entry in the static_list has its port replaced with the given port.
 * It is a no-op for non-static discovery kinds.
 */
func rewriteStaticListPorts(d *config.DiscoveryConfig, port string) *config.DiscoveryConfig {
	if d == nil || d.Kind != "static" || d.StaticDiscoveryConfig == nil {
		return d
	}

	newStatic := make([]string, len(d.StaticDiscoveryConfig.StaticList))
	for i, entry := range d.StaticDiscoveryConfig.StaticList {
		// Entry format: "host:port [weight=N] [priority=N] ..."
		parts := strings.Fields(entry)
		if len(parts) > 0 {
			host, _, err := net.SplitHostPort(parts[0])
			if err == nil {
				parts[0] = net.JoinHostPort(host, port)
			}
		}
		newStatic[i] = strings.Join(parts, " ")
	}

	newD := *d
	sc := *d.StaticDiscoveryConfig
	sc.StaticList = newStatic
	newD.StaticDiscoveryConfig = &sc
	return &newD
}
