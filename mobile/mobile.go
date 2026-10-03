//go:build android || linux

// Package tainacore is the gomobile entry point used by the Android app.
// Build: gomobile bind -target=android -javapkg=com.tainavpn ./mobile
package tainacore

import (
	"sync"

	"github.com/nshimanovskiy/tainavpn/core"
)

// Host is implemented in Kotlin by the VpnService.
type Host interface {
	// OpenTun establishes the VpnService interface and returns its file descriptor.
	OpenTun() (int32, error)
	// Protect excludes a socket from the VPN (VpnService.protect).
	Protect(fd int32) bool
}

var (
	mu       sync.Mutex
	instance *core.Instance
	logs     = core.NewLogBuffer(300)
)

// Start launches sing-box with the given JSON config.
// dataDir is the app's private files directory.
func Start(config string, dataDir string, host Host) error {
	mu.Lock()
	defer mu.Unlock()
	if instance != nil {
		_ = instance.Close()
		instance = nil
	}
	logs.Clear()
	i, err := core.Start(config, dataDir, &platform{host: host}, logs)
	if err != nil {
		logs.Add("ERROR: " + err.Error())
		return err
	}
	instance = i
	return nil
}

// Stop shuts sing-box down.
func Stop() error {
	mu.Lock()
	defer mu.Unlock()
	if instance == nil {
		return nil
	}
	err := instance.Close()
	instance = nil
	return err
}

// IsRunning reports whether sing-box is running.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return instance != nil
}

// Logs returns recent sing-box log lines.
func Logs() string { return logs.String() }

// CoreVersion returns the sing-box version.
func CoreVersion() string { return core.Version() }
