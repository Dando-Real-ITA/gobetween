package utils

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
)

type SourcePool struct {
	network [16]byte
	mask    [16]byte
	counter atomic.Uint64
}

func NewIPv6SourcePool(subnet string) (*SourcePool, error) {
	value := strings.TrimSpace(subnet)
	if value == "" {
		return nil, nil
	}

	ip, ipNet, err := net.ParseCIDR(value)
	if err != nil {
		return nil, fmt.Errorf("invalid sources subnet %q: %w", value, err)
	}

	ip = ip.To16()
	if ip == nil || ip.To4() != nil {
		return nil, fmt.Errorf("sources subnet %q must be ipv6 cidr", value)
	}

	if bits, _ := ipNet.Mask.Size(); bits == 128 {
		return nil, fmt.Errorf("sources subnet %q must have at least one host bit", value)
	}

	networkIP := ip.Mask(ipNet.Mask)
	pool := &SourcePool{}
	copy(pool.network[:], networkIP)
	copy(pool.mask[:], ipNet.Mask)

	return pool, nil
}

func (p *SourcePool) Next() net.IP {
	if p == nil {
		return nil
	}

	var raw [16]byte
	counter := p.counter.Add(1)
	binary.BigEndian.PutUint64(raw[8:], counter)

	var ip [16]byte
	for i := range ip {
		ip[i] = (p.network[i] & p.mask[i]) | (raw[i] & ^p.mask[i])
	}

	return net.IP(append([]byte(nil), ip[:]...))
}
