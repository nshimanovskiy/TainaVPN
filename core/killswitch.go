package core

import (
	"context"
	"net"
	"net/netip"
	"sort"
	"time"
)

// KillSwitch blocks all internet traffic except:
//   - traffic through the VPN interface (TunName / TunPrefixes),
//   - traffic to AllowIPs (the proxy servers and the core's own DNS server),
//   - local networks (private, link-local, multicast) so DHCP and the LAN keep working.
//
// The block stays in place if the app crashes; it is removed only by Disable.
type KillSwitchOptions struct {
	TunName     string
	TunPrefixes []netip.Prefix
	AllowIPs    []netip.Addr
	// testDisabled creates the rules switched off (Windows CI: check them without cutting the runner off)
	testDisabled bool
}

// LocalNetworks are never blocked.
var LocalNetworks = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// ResolveHosts turns proxy host names / IPs into addresses (best effort).
func ResolveHosts(hosts []string) []netip.Addr {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	seen := map[netip.Addr]bool{}
	var out []netip.Addr
	add := func(a netip.Addr) {
		a = a.Unmap()
		if a.IsValid() && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, h := range hosts {
		if a, err := netip.ParseAddr(h); err == nil {
			add(a)
			continue
		}
		if ips, err := net.DefaultResolver.LookupIPAddr(ctx, h); err == nil {
			for _, ip := range ips {
				if a, ok := netip.AddrFromSlice(ip.IP); ok {
					add(a)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// complementRanges returns the address ranges of a family that are NOT covered by
// the given prefixes, as "first-last" strings (used for Windows firewall rules).
func complementRanges(excluded []netip.Prefix, v6 bool) []string {
	type rng struct{ lo, hi netip.Addr }
	var ex []rng
	for _, p := range excluded {
		p = p.Masked()
		if p.Addr().Is6() != v6 {
			continue
		}
		ex = append(ex, rng{p.Addr(), lastAddr(p)})
	}
	sort.Slice(ex, func(i, j int) bool { return ex[i].lo.Less(ex[j].lo) })
	first, last := netip.MustParseAddr("0.0.0.0"), netip.MustParseAddr("255.255.255.255")
	if v6 {
		first, last = netip.MustParseAddr("::"), netip.MustParseAddr("ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff")
	}
	var out []string
	cur := first
	done := false
	for _, r := range ex {
		if done {
			break
		}
		if r.hi.Less(cur) {
			continue
		}
		if cur.Less(r.lo) {
			out = append(out, cur.String()+"-"+r.lo.Prev().String())
		}
		if r.hi == last {
			done = true
			break
		}
		if !r.hi.Less(cur) {
			cur = r.hi.Next()
		}
	}
	if !done {
		out = append(out, cur.String()+"-"+last.String())
	}
	return out
}

func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	bits := p.Bits()
	for i := range b {
		for j := 0; j < 8; j++ {
			if i*8+j >= bits {
				b[i] |= 1 << (7 - j)
			}
		}
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

func hostPrefixes(ips []netip.Addr) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(ips))
	for _, ip := range ips {
		out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
	}
	return out
}
