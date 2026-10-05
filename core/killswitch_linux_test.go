//go:build linux && !android

package core

import (
	"net"
	"net/netip"
	"os"
	"testing"
	"time"
)

// Integration test: needs CAP_NET_ADMIN and its own network namespace (CI runs it in a container).
func TestKillSwitchLinux(t *testing.T) {
	if os.Getenv("TVPN_KS_TEST") != "1" {
		t.Skip("set TVPN_KS_TEST=1 (needs CAP_NET_ADMIN, run in a container)")
	}
	dial := func(addr string) error {
		c, err := net.DialTimeout("tcp", addr, 4*time.Second)
		if err == nil {
			c.Close()
		}
		return err
	}
	if err := dial("8.8.8.8:443"); err != nil {
		t.Fatalf("no internet before the test: %v", err)
	}
	opts := KillSwitchOptions{TunName: "tainavpn", AllowIPs: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}
	if err := EnableKillSwitch(opts); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !KillSwitchActive() {
		t.Fatal("not active after enable")
	}
	if err := EnableKillSwitch(opts); err != nil { // re-enable must replace, not fail
		t.Fatalf("re-enable: %v", err)
	}
	if err := dial("1.1.1.1:443"); err != nil {
		t.Errorf("allowed proxy address blocked: %v", err)
	}
	if err := dial("8.8.8.8:443"); err == nil {
		t.Errorf("internet NOT blocked by the kill switch")
	}
	if err := DisableKillSwitch(); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if KillSwitchActive() {
		t.Fatal("still active after disable")
	}
	if err := dial("8.8.8.8:443"); err != nil {
		t.Errorf("internet still blocked after disable: %v", err)
	}
	if err := DisableKillSwitch(); err != nil { // disabling twice is fine
		t.Fatalf("second disable: %v", err)
	}
}
