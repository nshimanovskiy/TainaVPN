package core

import (
	"net/netip"
	"testing"
)

func TestComplement(t *testing.T) {
	ex := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("1.2.3.4/32"), netip.MustParsePrefix("255.255.255.255/32")}
	got := complementRanges(ex, false)
	want := []string{"0.0.0.0-1.2.3.3", "1.2.3.5-9.255.255.255", "11.0.0.0-255.255.255.254"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
	// overlapping prefixes and start at zero
	got = complementRanges([]netip.Prefix{netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("0.0.0.0/16")}, false)
	if len(got) != 1 || got[0] != "1.0.0.0-255.255.255.255" {
		t.Fatalf("got %v", got)
	}
	got = complementRanges(LocalNetworks, true)
	t.Log(got)
	if len(got) == 0 || got[0] != "::-::" {
		t.Fatalf("v6 got %v", got)
	}
	t.Log(complementRanges(append(LocalNetworks, netip.MustParsePrefix("8.8.8.8/32")), false))
}
