// Package killswitch blocks all traffic that doesn't go through the VPN.
//
// The block lives in the OS firewall (nftables on Linux, Windows Firewall on
// Windows), not in the app process, so it keeps working if the app crashes:
// internet stays blocked until the VPN is connected again or the user turns
// the kill switch off.
package killswitch

import (
	"errors"
	"net"
	"net/netip"
	"strings"
)

// TunName is the name the VPN interface gets (set in the sing-box config).
const TunName = "tainavpn0"

// TunPrefixes are the VPN interface addresses (see buildConfig in ui/app.js).
var TunPrefixes = []netip.Prefix{
	netip.MustParsePrefix("172.19.0.0/30"),
	netip.MustParsePrefix("fdfe:dcba:9876::/126"),
}

// ErrUnsupported is returned on platforms without a kill switch implementation.
var ErrUnsupported = errors.New("kill switch is not supported on this platform")

// Options describe what stays allowed while the kill switch is on.
type Options struct {
	// Allow: proxy servers and other hosts that must stay reachable directly
	// (host names are resolved when the kill switch is enabled).
	Allow []string
	// AllowLAN keeps the local network reachable.
	AllowLAN bool
	// Program (Windows): the app itself is allowed, it runs the VPN core.
	Program string
}

// LANPrefixes are private, link-local and multicast ranges.
var LANPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("ff00::/8"),
}

// resolve turns hosts into addresses (IP literals pass through).
func resolve(hosts []string) []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	add := func(a netip.Addr) {
		a = a.Unmap()
		if a.IsValid() && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	for _, h := range hosts {
		h = strings.Trim(strings.TrimSpace(h), "[]")
		if h == "" {
			continue
		}
		if a, err := netip.ParseAddr(h); err == nil {
			add(a)
			continue
		}
		if ips, err := net.LookupIP(h); err == nil {
			for _, ip := range ips {
				if a, ok := netip.AddrFromSlice(ip); ok {
					add(a)
				}
			}
		}
	}
	return out
}
