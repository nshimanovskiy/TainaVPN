// Tainavpn desktop app (Windows / Linux).
// Build: go build -tags desktop,production ./desktop   (see .github/workflows/build.yml)
package main

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nshimanovskiy/tainavpn/core"
	"github.com/nshimanovskiy/tainavpn/ui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppVersion is set at build time with -ldflags "-X main.AppVersion=..."
var AppVersion = "dev"

// App is exposed to the UI as window.go.main.App.
type App struct {
	lang     string
	ctx      context.Context
	mu       sync.Mutex
	instance *core.Instance
	state    string
	lastErr  string
	logs     *core.LogBuffer
	dir      string
}

func NewApp() *App {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	dir = filepath.Join(dir, "Tainavpn")
	_ = os.MkdirAll(dir, 0o700)
	return &App{state: "stopped", logs: core.NewLogBuffer(400), dir: dir}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(ctx context.Context) { _ = a.Stop() }

func (a *App) Platform() string { return runtime.GOOS }

func (a *App) Version() string {
	return "Tainavpn " + AppVersion + " · sing-box " + core.Version()
}

func (a *App) DeviceName() string {
	h, _ := os.Hostname()
	return h
}

func (a *App) LoadStore() string {
	data, err := os.ReadFile(filepath.Join(a.dir, "store.json"))
	if err != nil {
		return ""
	}
	return string(data)
}

func (a *App) SaveStore(data string) error {
	p := filepath.Join(a.dir, "store.json")
	if err := os.WriteFile(p+".tmp", []byte(data), 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func (a *App) Start(config string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.instance != nil {
		_ = a.instance.Close()
		a.instance = nil
	}
	a.logs.Clear()
	a.state = "starting"
	_ = os.WriteFile(filepath.Join(a.dir, "last-config.json"), []byte(config), 0o600)
	inst, err := core.Start(config, filepath.Join(a.dir, "core"), nil, a.logs)
	if err != nil {
		a.state = "stopped"
		a.lastErr = humanError(a.lang, err.Error())
		a.logs.Add("ERROR: " + err.Error())
		return &uiError{a.lastErr}
	}
	a.instance = inst
	a.state = "running"
	a.lastErr = ""
	return nil
}

func (a *App) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.instance == nil {
		a.state = "stopped"
		return nil
	}
	a.state = "stopping"
	err := a.instance.Close()
	a.instance = nil
	a.state = "stopped"
	return err
}

func (a *App) Status() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, _ := json.Marshal(map[string]string{"state": a.state, "error": a.lastErr})
	return string(data)
}

func (a *App) Logs() string { return a.logs.String() }

var httpClient = &http.Client{Timeout: 20 * time.Second}

// HTTP performs a request for the UI (avoids CORS) and returns {"status","body"} or {"error"}.
func (a *App) HTTP(method, url, body string) string {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	out := map[string]any{}
	req, err := http.NewRequest(method, url, rd)
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Tainavpn/"+AppVersion+" ("+runtime.GOOS+")")
		var resp *http.Response
		resp, err = httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			out["status"] = resp.StatusCode
			out["body"] = string(data)
		}
	}
	if err != nil {
		out["error"] = err.Error()
	}
	data, _ := json.Marshal(out)
	return string(data)
}

type uiError struct{ msg string }

func (e *uiError) Error() string { return e.msg }

// errText holds native error messages in the UI languages.
var errText = map[string]map[string]string{
	"ru": {
		"tunWin":   "Нет прав администратора для режима TUN. Запустите от имени администратора или выберите режим «Системный прокси».",
		"tunLinux": "Нет прав для режима TUN (нужен CAP_NET_ADMIN). Выполните: sudo setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep <путь к tainavpn> — или выберите режим «Системный прокси».",
		"port":     "Порт 2080 занят другой программой.",
	},
	"en": {
		"tunWin":   "TUN mode needs administrator rights. Run as administrator or choose the “System proxy” mode.",
		"tunLinux": "TUN mode needs CAP_NET_ADMIN. Run: sudo setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep <path to tainavpn> — or choose the “System proxy” mode.",
		"port":     "Port 2080 is used by another program.",
	},
}

// SetLang is called by the UI when the language changes ("ru" or "en").
func (a *App) SetLang(lang string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := errText[lang]; ok {
		a.lang = lang
	}
}

func humanError(lang, msg string) string {
	tr, ok := errText[lang]
	if !ok {
		tr = errText["en"]
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "access is denied") || strings.Contains(low, "operation not permitted") ||
		strings.Contains(low, "permission denied") || strings.Contains(low, "elevat"):
		if runtime.GOOS == "windows" {
			return tr["tunWin"] + " (" + msg + ")"
		}
		return tr["tunLinux"] + " (" + msg + ")"
	case strings.Contains(low, "address already in use") || strings.Contains(low, "only one usage"):
		return tr["port"] + " (" + msg + ")"
	}
	return msg
}

func main() {
	app := NewApp()
	assets, err := fs.Sub(ui.FS, ".")
	if err != nil {
		log.Fatal(err)
	}
	err = wails.Run(&options.App{
		Title:            "Tainavpn",
		Width:            440,
		Height:           760,
		MinWidth:         360,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 20, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.tainavpn.desktop",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					wruntime.WindowUnminimise(app.ctx)
					wruntime.WindowShow(app.ctx)
				}
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
