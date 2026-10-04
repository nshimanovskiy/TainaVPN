package api

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/nshimanovskiy/tainavpn/server/internal/pool"
	"github.com/nshimanovskiy/tainavpn/server/internal/store"
)

// iOS has no way to install our own VPN app without a paid Apple developer
// account, so iPhone users get a home-screen web app (/ios) that hands their
// proxy to a free App Store client built on sing-box (Karing, Hiddify, sing-box VT).
// For that the subscription is also served in formats those clients understand.

//go:embed ios.html
var iosHTML []byte

//go:embed icon-180.png
var icon180 []byte

//go:embed icon-512.png
var icon512 []byte

// resolve finds the user for a subscription key and returns their proxy.
func (a *API) resolve(w http.ResponseWriter, r *http.Request) (store.User, pool.Proxy, bool) {
	u, ok := a.store.ByKey(r.PathValue("key"), clientIP(r))
	if !ok {
		errJSON(w, 404, "unknown key")
		return u, pool.Proxy{}, false
	}
	if u.Disabled {
		errJSON(w, 403, "key is disabled")
		return u, pool.Proxy{}, false
	}
	px, err := a.assign(u, false)
	if err != nil {
		errJSON(w, 503, err.Error())
		return u, pool.Proxy{}, false
	}
	return u, px, true
}

func (a *API) subHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte(a.cfg.Name)))
	w.Header().Set("Profile-Update-Interval", "1")
}

// shareLink renders a proxy as a share link (socks://user:pass@host:port#name),
// understood by Karing, Hiddify, Streisand, Shadowrocket, V2Box and others.
func shareLink(px pool.Proxy, name string) string {
	u := url.URL{Scheme: "socks", Host: px.Addr(), Fragment: name}
	if px.Type == "http" {
		u.Scheme = "http"
	}
	if px.Username != "" {
		u.User = url.UserPassword(px.Username, px.Password)
	}
	return u.String()
}

// subLinks: GET /sub/{key}/links — base64 list of share links (universal subscription format).
func (a *API) subLinks(w http.ResponseWriter, r *http.Request) {
	_, px, ok := a.resolve(w, r)
	if !ok {
		return
	}
	a.subHeaders(w)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(shareLink(px, a.cfg.Name) + "\n"))))
}

// subSingBox: GET /sub/{key}/singbox — complete sing-box client config (remote profile).
func (a *API) subSingBox(w http.ResponseWriter, r *http.Request) {
	_, px, ok := a.resolve(w, r)
	if !ok {
		return
	}
	a.subHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(SingBoxConfig(px))
}

// SingBoxConfig mirrors buildConfig() in ui/app.js (TUN mode, DNS through the proxy).
func SingBoxConfig(px pool.Proxy) map[string]any {
	out := map[string]any{"tag": "proxy", "server": px.Host, "server_port": px.Port}
	if px.Type == "http" {
		out["type"] = "http"
	} else {
		out["type"] = "socks"
		out["version"] = "5"
	}
	if px.Username != "" {
		out["username"] = px.Username
		out["password"] = px.Password
	}
	return map[string]any{
		"log": map[string]any{"level": "warn"},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"type": "tcp", "tag": "remote", "server": "1.1.1.1", "detour": "proxy"},
				map[string]any{"type": "udp", "tag": "local", "server": "77.88.8.8"},
			},
			"final":    "remote",
			"strategy": "prefer_ipv4",
		},
		"inbounds": []any{map[string]any{
			"type":       "tun",
			"tag":        "tun-in",
			"address":    []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			"mtu":        9000,
			"auto_route": true,
		}},
		"outbounds": []any{out, map[string]any{"type": "direct", "tag": "direct"}},
		"route": map[string]any{
			"rules": []any{
				map[string]any{"action": "sniff"},
				map[string]any{"protocol": "dns", "action": "hijack-dns"},
				map[string]any{"ip_is_private": true, "outbound": "direct"},
				map[string]any{"network": "udp", "port": 443, "action": "reject"},
			},
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}
}

func (a *API) iosPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(iosHTML)
}

func (a *API) iosManifest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":             a.cfg.Name,
		"short_name":       a.cfg.Name,
		"start_url":        "/ios",
		"display":          "standalone",
		"background_color": "#0e1014",
		"theme_color":      "#0e1014",
		"icons": []any{
			map[string]any{"src": "/ios/icon-180.png", "sizes": "180x180", "type": "image/png"},
			map[string]any{"src": "/ios/icon-512.png", "sizes": "512x512", "type": "image/png"},
		},
	})
}

func pngHandler(data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		_, _ = w.Write(data)
	}
}
