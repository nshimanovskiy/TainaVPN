package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nshimanovskiy/tainavpn/core"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------- ping ----------

// Ping measures a proxy's real delay. proxy is the profile JSON from the UI.
func (a *App) Ping(proxy string) (int, error) {
	var p struct {
		Type     string `json:"type"`
		Server   string `json:"server"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal([]byte(proxy), &p); err != nil {
		return 0, err
	}
	return core.Ping(p.Type, p.Server, p.Port, p.Username, p.Password, 6*time.Second)
}

// ---------- self-update ----------

// AppVersion without the leading "v".
func (a *App) AppVersion() string { return strings.TrimPrefix(AppVersion, "v") }

type updateState struct {
	mu       sync.Mutex
	State    string  `json:"state"` // idle | downloading | installing | error
	Progress float64 `json:"progress"`
	Error    string  `json:"error"`
}

var upd = &updateState{State: "idle"}

func (u *updateState) set(state string, progress float64, err string) {
	u.mu.Lock()
	u.State, u.Progress, u.Error = state, progress, err
	u.mu.Unlock()
}

// UpdateStatus is polled by the UI while an update is running.
func (a *App) UpdateStatus() string {
	upd.mu.Lock()
	defer upd.mu.Unlock()
	data, _ := json.Marshal(upd)
	return string(data)
}

// Update downloads the new version, checks its SHA-256 (if known), installs it and restarts the app.
func (a *App) Update(url, sha256hex string) error {
	if !strings.HasPrefix(url, "https://") {
		return &uiError{"bad url"}
	}
	upd.mu.Lock()
	busy := upd.State == "downloading" || upd.State == "installing"
	upd.mu.Unlock()
	if busy {
		return nil
	}
	upd.set("downloading", 0, "")
	go func() {
		if err := a.doUpdate(url, strings.ToLower(strings.TrimPrefix(sha256hex, "sha256:"))); err != nil {
			upd.set("error", 0, err.Error())
		}
	}()
	return nil
}

func (a *App) doUpdate(url, wantSum string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := os.TempDir()
	if runtime.GOOS == "windows" {
		dir = filepath.Dir(exe) // same volume, so the file can be renamed into place
	}
	tmp := filepath.Join(dir, "tainavpn-update-"+filepath.Base(url))
	if err := download(url, tmp, wantSum); err != nil {
		os.Remove(tmp)
		return err
	}
	upd.set("installing", 1, "")
	_ = a.Stop()
	switch runtime.GOOS {
	case "windows":
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
	case "linux":
		// asks for the password once (installing a package); apt keeps dependencies right
		cmd := exec.Command("pkexec", "apt-get", "install", "-y", "--allow-downgrades", tmp)
		out, err := cmd.CombinedOutput()
		os.Remove(tmp)
		if err != nil {
			return fmt.Errorf("apt: %v %s", err, lastLines(string(out), 3))
		}
		exe = "/usr/bin/tainavpn"
	default:
		return fmt.Errorf("self-update is not supported on %s", runtime.GOOS)
	}
	// the new instance waits a moment so it doesn't hit our single-instance lock
	if err := exec.Command(exe, "--after-update").Start(); err != nil {
		return err
	}
	wruntime.Quit(a.ctx)
	return nil
}

// cleanupOldBinary removes the previous exe left by an update (Windows).
func cleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		go func() {
			time.Sleep(3 * time.Second)
			os.Remove(exe + ".old")
		}()
	}
}

func download(url, path, wantSum string) error {
	client := &http.Client{Timeout: 15 * time.Minute}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Tainavpn/"+AppVersion)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 {
				upd.set("downloading", float64(done)/float64(total), "")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if wantSum != "" && hex.EncodeToString(h.Sum(nil)) != wantSum {
		return fmt.Errorf("checksum mismatch, the download is damaged")
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
