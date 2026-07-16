package utils

import (
	"net"
	"testing"
)

func TestNewIPv6SourcePoolRejectsIPv4(t *testing.T) {
	_, err := NewIPv6SourcePool("127.0.0.0/24")
	if err == nil {
		t.Fatal("expected IPv4 subnet to fail")
	}
}

func TestSourcePoolNextReturnsIPv6InSubnet(t *testing.T) {
	pool, err := NewIPv6SourcePool("2001:db8::/64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first := pool.Next()
	second := pool.Next()

	if first == nil || second == nil {
		t.Fatal("expected non-nil generated ips")
	}

	if first.Equal(second) {
		t.Fatal("expected different generated ips")
	}

	if !mustCIDR("2001:db8::/64").Contains(first) {
		t.Fatalf("first ip %s not in subnet", first)
	}

	if !mustCIDR("2001:db8::/64").Contains(second) {
		t.Fatalf("second ip %s not in subnet", second)
	}
}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}
