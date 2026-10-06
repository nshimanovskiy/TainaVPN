package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/nshimanovskiy/tainavpn/core"
)

// Hidden self-test used by CI on real Windows/Ubuntu runners: drives the same App methods
// the UI calls (KillSwitch, Start, Status, Stop) without opening a window.
//
//	tainavpn --selftest <config.json> <out.log> [hold-seconds] [ks-hosts-json|-] [crash]
//
// Writes "RUNNING" (or "FAILED: ...") to out.log once the VPN is up, keeps it up for
// hold-seconds, then stops. With "crash" the core is stopped but the kill switch is left
// on, as after a crash. "--selftest-unblock <out.log>" lifts the kill switch.
func selftest(args []string) int {
	if len(args) >= 2 && args[0] == "--selftest-unblock" {
		err := NewApp().KillSwitch(false, "")
		writeOut(args[1], fmt.Sprintf("UNBLOCK err=%v active=%v\n", err, core.KillSwitchActive()))
		if err != nil {
			return 1
		}
		return 0
	}
	if len(args) < 3 {
		return 2
	}
	cfgPath, out := args[1], args[2]
	hold := 20
	if len(args) > 3 {
		hold, _ = strconv.Atoi(args[3])
	}
	ks := len(args) > 4 && args[4] != "-"
	crash := len(args) > 5 && args[5] == "crash"
	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		writeOut(out, "FAILED: "+err.Error()+"\n")
		return 1
	}
	a := NewApp()
	a.lang = "en"
	if ks {
		if err := a.KillSwitch(true, args[4]); err != nil {
			writeOut(out, "FAILED: kill switch: "+err.Error()+"\n")
			return 1
		}
	}
	if err := a.Start(string(cfg)); err != nil {
		writeOut(out, "FAILED: start: "+err.Error()+"\n"+a.Logs())
		if ks {
			_ = a.KillSwitch(false, "")
		}
		return 1
	}
	writeOut(out, "RUNNING "+a.Status()+"\n")
	time.Sleep(time.Duration(hold) * time.Second)
	_ = a.Stop()
	status := a.Status()
	if ks && !crash {
		_ = a.KillSwitch(false, "")
	}
	writeOut(out, "STOPPED "+status+"\n--- core log ---\n"+a.Logs()+"\nDONE\n")
	return 0
}

func writeOut(path, s string) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(s)
}
