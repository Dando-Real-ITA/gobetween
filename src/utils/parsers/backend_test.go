package parsers

import (
	"testing"
)

func TestParseBackendDefaultCompatibility(t *testing.T) {
	for _, tc := range []struct {
		line    string
		resolve bool
	}{
		{"127.0.0.1:80", false},
		{"[2001:db8::1]:443 weight=2 priority=3 max_connections=4 sni=app.internal", false},
		{"proxy.internal:80 resolve", true},
		{" proxy.internal:80 weight=2 priority=3 max_connections=4 sni=app.internal resolve ", true},
	} {
		backend, err := ParseBackendDefault(tc.line)
		if err != nil {
			t.Fatalf("%q: %v", tc.line, err)
		}
		if backend.Resolve != tc.resolve || HasResolveBackend([]string{tc.line}) != tc.resolve {
			t.Fatalf("%q: unexpected resolve flag", tc.line)
		}
	}
	if HasResolveBackend([]string{"not a backend resolve", "127.0.0.1:80"}) {
		t.Fatal("invalid entries must not enable DNS defaults")
	}
}

func TestParseBackendDefaultResolveFlag(t *testing.T) {
	backend, err := ParseBackendDefault("proxy0.lb.internal:0 weight=10 priority=10 resolve")
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if backend.Host != "proxy0.lb.internal" {
		t.Fatalf("unexpected host %q", backend.Host)
	}
	if backend.Port != "0" {
		t.Fatalf("unexpected port %q", backend.Port)
	}
	if backend.Weight != 10 {
		t.Fatalf("unexpected weight %d", backend.Weight)
	}
	if backend.Priority != 10 {
		t.Fatalf("unexpected priority %d", backend.Priority)
	}
	if !backend.Resolve {
		t.Fatal("expected resolve flag to be set")
	}
}

