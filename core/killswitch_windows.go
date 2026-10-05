//go:build windows

package core

import (
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
)

// Windows: firewall *block* rules (they win over any allow rule) for outbound traffic
// whose local address is not the VPN interface and whose remote address is not allowed.
// Applied with PowerShell (NetSecurity module); the app runs as administrator.
const ksGroup = "Tainavpn kill switch"

func ps(script string) (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func psList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = "'" + s + "'"
	}
	return "@(" + strings.Join(q, ",") + ")"
}

func EnableKillSwitch(opts KillSwitchOptions) error {
	remoteAllowed := append([]netip.Prefix{}, LocalNetworks...)
	remoteAllowed = append(remoteAllowed, hostPrefixes(opts.AllowIPs)...)
	localAllowed := append([]netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}, opts.TunPrefixes...)
	var script strings.Builder
	fmt.Fprintf(&script, "$ErrorActionPreference='Stop'; Remove-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue;", ksGroup)
	enabled := "True"
	if opts.testDisabled {
		enabled = "False"
	} else {
		// the firewall must be on for the rules to work
		script.WriteString("Set-NetFirewallProfile -Profile Domain,Public,Private -Enabled True;")
	}
	for i, v6 := range []bool{false, true} {
		fmt.Fprintf(&script,
			"New-NetFirewallRule -DisplayName '%s %d' -Group '%s' -Direction Outbound -Action Block -Enabled %s -LocalAddress %s -RemoteAddress %s | Out-Null;",
			ksGroup, i+1, ksGroup, enabled, psList(complementRanges(localAllowed, v6)), psList(complementRanges(remoteAllowed, v6)))
	}
	_, err := ps(script.String())
	return err
}

func DisableKillSwitch() error {
	_, err := ps(fmt.Sprintf("Remove-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue; exit 0", ksGroup))
	return err
}

func KillSwitchActive() bool {
	out, err := ps(fmt.Sprintf("@(Get-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue).Count", ksGroup))
	return err == nil && strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "0"
}
