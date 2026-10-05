//go:build windows

package core

import (
	"net/netip"
	"os"
	"testing"
)

// Creates the kill switch rules switched off (so the CI runner keeps its connection),
// checks that Windows accepted them and removes them again.
func TestKillSwitchWindows(t *testing.T) {
	if os.Getenv("TVPN_KS_TEST") != "1" {
		t.Skip("set TVPN_KS_TEST=1 (needs administrator rights)")
	}
	opts := KillSwitchOptions{
		TunName:      "tainavpn",
		TunPrefixes:  []netip.Prefix{netip.MustParsePrefix("172.19.0.0/30"), netip.MustParsePrefix("fdfe:dcba:9876::/126")},
		AllowIPs:     []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("2606:4700:4700::1111")},
		testDisabled: true,
	}
	if err := EnableKillSwitch(opts); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !KillSwitchActive() {
		t.Fatal("rules not found after enable")
	}
	out, err := ps("Get-NetFirewallRule -Group '" + ksGroup + "' | Get-NetFirewallAddressFilter | Format-List RemoteAddress,LocalAddress | Out-String -Width 4000")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(out)
	if err := DisableKillSwitch(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if KillSwitchActive() {
		t.Fatal("rules still present after disable")
	}
}
