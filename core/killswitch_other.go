//go:build !linux && !windows

package core

import "errors"

func EnableKillSwitch(opts KillSwitchOptions) error {
	return errors.New("kill switch is not supported on this system")
}
func DisableKillSwitch() error { return nil }
func KillSwitchActive() bool   { return false }
