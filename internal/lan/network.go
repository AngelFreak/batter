// Package lan runs the wired network Batter's phones are on: each phone has
// a USB-C ethernet adapter on a switch attached to the batter container
// (Docker macvlan on the box's second NIC). Batter is that network's DHCP
// server and router. Its firewall lets a phone's traffic out only through
// the WireGuard tunnel of the phone's VPN profile, and adb reaches the
// phones over TCP.
package lan

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Network describes the phone LAN.
type Network struct {
	// Addr is Batter's address on it: the phones' gateway and DNS server.
	Addr   netip.Addr
	Prefix netip.Prefix
	// The DHCP pool, inclusive. It must stay clear of Docker's own
	// assignments on the network (its ip_range and gateway).
	PoolStart, PoolEnd netip.Addr
}

// ParseNetwork parses Batter's LAN address with its prefix length
// ("10.77.0.1/24") and an optional pool ("10.77.0.100-10.77.0.250"). The
// default pool is the upper half of the subnet minus its last two
// addresses.
func ParseNetwork(addr, pool string) (Network, error) {
	p, err := netip.ParsePrefix(addr)
	if err != nil || !p.Addr().Is4() {
		return Network{}, fmt.Errorf("phone LAN address %q: want an IPv4 address with prefix length, like 10.77.0.1/24", addr)
	}
	n := Network{Addr: p.Addr(), Prefix: p.Masked()}
	if p.Bits() > 29 || n.Addr == n.Prefix.Addr() || n.Addr == n.broadcast() {
		return Network{}, fmt.Errorf("phone LAN address %q: not a host address in a /29 or larger", addr)
	}
	if pool == "" {
		half := 1 << (32 - p.Bits() - 1)
		n.PoolStart = offset(n.Prefix.Addr(), half)
		n.PoolEnd = offset(n.broadcast(), -2)
	} else {
		start, end, ok := strings.Cut(pool, "-")
		if n.PoolStart, err = netip.ParseAddr(strings.TrimSpace(start)); !ok || err != nil {
			return Network{}, fmt.Errorf("phone LAN pool %q: want first-last, like 10.77.0.100-10.77.0.250", pool)
		}
		if n.PoolEnd, err = netip.ParseAddr(strings.TrimSpace(end)); err != nil {
			return Network{}, fmt.Errorf("phone LAN pool %q: want first-last, like 10.77.0.100-10.77.0.250", pool)
		}
	}
	switch {
	case !n.Prefix.Contains(n.PoolStart) || !n.Prefix.Contains(n.PoolEnd) ||
		n.PoolStart == n.Prefix.Addr() || n.PoolEnd == n.broadcast():
		return Network{}, fmt.Errorf("phone LAN pool %s-%s is not inside %s", n.PoolStart, n.PoolEnd, n.Prefix)
	case n.PoolEnd.Less(n.PoolStart):
		return Network{}, fmt.Errorf("phone LAN pool %s-%s is reversed", n.PoolStart, n.PoolEnd)
	case n.InPool(n.Addr):
		return Network{}, fmt.Errorf("phone LAN pool %s-%s includes Batter's own address %s", n.PoolStart, n.PoolEnd, n.Addr)
	}
	return n, nil
}

// InPool reports whether ip is in the DHCP pool.
func (n Network) InPool(ip netip.Addr) bool {
	return !ip.Less(n.PoolStart) && !n.PoolEnd.Less(ip)
}

// Mask is the subnet mask.
func (n Network) Mask() net.IPMask {
	return net.CIDRMask(n.Prefix.Bits(), 32)
}

func (n Network) broadcast() netip.Addr {
	return offset(n.Prefix.Addr(), 1<<(32-n.Prefix.Bits())-1)
}

func offset(a netip.Addr, by int) netip.Addr {
	b := a.As4()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v = uint32(int64(v) + int64(by))
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
}

// FindInterface returns the interface that carries addr: Docker names the
// container's interfaces by attach order, so the LAN's is found by Batter's
// address on it.
func FindInterface(addr netip.Addr) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(ipn.IP); ok && ip.Unmap() == addr {
					return iface.Name, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no interface has the phone LAN address %s (is the container attached to the phone network?)", addr)
}
