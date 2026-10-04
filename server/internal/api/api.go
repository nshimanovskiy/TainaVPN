// Package api serves the client subscription API and the admin panel.
package api

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nshimanovskiy/tainavpn/server/internal/pool"
	"github.com/nshimanovskiy/tainavpn/server/internal/store"
)

//go:embed admin.html
var adminHTML []byte

type Config struct {
	Name             string // shown in the app, e.g. "Tainavpn"
	PublicURL        string // https://vpn.example.com (used to build subscription links)
	AdminToken       string
	AdminPath        string // e.g. /panel
	OpenRegistration bool   // allow the app to obtain a key by itself
	RegisterPerDay   int    // per-IP limit for self-registration
}

type API struct {
	cfg   Config
	store *store.Store
	pool  *pool.Pool

	assignMu sync.Mutex

	rlMu  sync.Mutex
	rl    map[string][]time.Time
	geoRL map[string][]time.Time
}

func New(cfg Config, st *store.Store, pl *pool.Pool) *API {
	if cfg.RegisterPerDay <= 0 {
		cfg.RegisterPerDay = 3
	}
	return &API{cfg: cfg, store: st, pool: pl, rl: map[string][]time.Time{}}
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/info", a.info)
	mux.HandleFunc("POST /api/v1/register", a.register)
	mux.HandleFunc("POST /api/v1/geo", a.geo)
	mux.HandleFunc("GET /sub/{key}", a.subscription)
	mux.HandleFunc("GET /sub/{key}/links", a.subLinks)
	mux.HandleFunc("GET /sub/{key}/singbox", a.subSingBox)
	mux.HandleFunc("GET /ios", a.iosPage)
	mux.HandleFunc("GET /ios/manifest.json", a.iosManifest)
	mux.HandleFunc("GET /ios/icon-180.png", pngHandler(icon180))
	mux.HandleFunc("GET /ios/icon-512.png", pngHandler(icon512))
	mux.HandleFunc("GET /api/admin/users", a.admin(a.listUsers))
	mux.HandleFunc("POST /api/admin/users", a.admin(a.createUser))
	mux.HandleFunc("DELETE /api/admin/users/{id}", a.admin(a.deleteUser))
	mux.HandleFunc("POST /api/admin/users/{id}/disable", a.admin(a.setDisabled(true)))
	mux.HandleFunc("POST /api/admin/users/{id}/enable", a.admin(a.setDisabled(false)))
	mux.HandleFunc("POST /api/admin/users/{id}/reassign", a.admin(a.reassign))
	mux.HandleFunc("GET /api/admin/proxies", a.admin(a.listProxies))
	mux.HandleFunc("POST /api/admin/proxies/check", a.admin(a.checkProxies))
	mux.HandleFunc("GET /api/admin/status", a.admin(a.status))
	adminPath := "/" + strings.Trim(a.cfg.AdminPath, "/")
	mux.HandleFunc("GET "+adminPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		_, _ = w.Write(adminHTML)
	})
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// the apps load their UI from a local origin (file://, wails://), so allow any origin
		// for the public client endpoints only
		if strings.HasPrefix(r.URL.Path, "/sub/") || strings.HasPrefix(r.URL.Path, "/api/v1/") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func (a *API) baseURL(r *http.Request) string {
	if a.cfg.PublicURL != "" {
		return strings.TrimRight(a.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *API) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"name":              a.cfg.Name,
		"open_registration": a.cfg.OpenRegistration,
	})
}

func (a *API) allowRegister(ip string) bool {
	return a.allow(&a.rl, ip, a.cfg.RegisterPerDay, 24*time.Hour)
}

// allow is a simple sliding-window rate limiter keyed by string.
func (a *API) allow(m *map[string][]time.Time, key string, limit int, window time.Duration) bool {
	a.rlMu.Lock()
	defer a.rlMu.Unlock()
	if *m == nil {
		*m = map[string][]time.Time{}
	}
	now := time.Now()
	recent := (*m)[key][:0]
	for _, t := range (*m)[key] {
		if now.Sub(t) < window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= limit {
		(*m)[key] = recent
		return false
	}
	(*m)[key] = append(recent, now)
	return true
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.OpenRegistration {
		errJSON(w, 403, msg(r, "regClosed"))
		return
	}
	ip := clientIP(r)
	if !a.allowRegister(ip) {
		errJSON(w, 429, msg(r, "regTooMany"))
		return
	}
	var req struct {
		Device string `json:"device"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	name := strings.TrimSpace(req.Device)
	if len(name) > 64 {
		name = name[:64]
	}
	if name == "" {
		name = "app"
	}
	u, err := a.store.Create(name, "app")
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"key":              u.Key,
		"subscription_url": a.baseURL(r) + "/sub/" + u.Key,
	})
}

// load counts active users per assigned proxy.
func (a *API) load() map[string]int {
	load := map[string]int{}
	for _, u := range a.store.List() {
		if u.Disabled {
			continue
		}
		for _, id := range u.Proxies {
			load[id]++
		}
	}
	return load
}

// assign returns the user's proxies, one per country available in the pool.
// A country's proxy is (re)assigned when missing, dead, moved to another
// country in the file, or when force is set.
func (a *API) assign(u store.User, force bool) ([]pool.Proxy, error) {
	a.assignMu.Lock()
	defer a.assignMu.Unlock()
	load := a.load()
	for _, id := range u.Proxies {
		load[id]--
	}
	next := map[string]string{}
	var out []pool.Proxy
	for _, country := range a.pool.Countries() {
		cur := u.Proxies[country]
		if !force && cur != "" {
			if px, ok := a.pool.Get(cur); ok && px.Usable() && px.Country == country {
				next[country] = px.ID
				load[px.ID]++
				out = append(out, px)
				continue
			}
		}
		exclude := ""
		if force {
			exclude = cur
		}
		px, ok := a.pool.Pick(country, load, exclude)
		if !ok {
			continue
		}
		next[country] = px.ID
		load[px.ID]++
		out = append(out, px)
	}
	if len(out) == 0 {
		return nil, errNoProxies
	}
	if !sameMap(next, u.Proxies) {
		_ = a.store.SetProxies(u.ID, next)
	}
	return out, nil
}

var errNoProxies = errors.New("no proxies available")

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// proxyJSON describes one proxy for the apps.
func proxyJSON(px pool.Proxy, fallbackName string) map[string]any {
	return map[string]any{
		"name":         pool.Label(px.Country, fallbackName),
		"country":      px.Country,
		"country_name": pool.CountryName(px.Country),
		"type":         px.Type,
		"server":       px.Host,
		"port":         px.Port,
		"username":     px.Username,
		"password":     px.Password,
	}
}

func (a *API) subscription(w http.ResponseWriter, r *http.Request) {
	_, list, ok := a.resolve(w, r)
	if !ok {
		return
	}
	proxies := []any{}
	for _, px := range list {
		proxies = append(proxies, proxyJSON(px, a.cfg.Name))
	}
	writeJSON(w, 200, map[string]any{
		"version": 2,
		"name":    a.cfg.Name,
		"proxies": proxies,
	})
}

func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a.cfg.AdminToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.cfg.AdminToken)) != 1 {
			time.Sleep(500 * time.Millisecond)
			errJSON(w, 401, "unauthorized")
			return
		}
		next(w, r)
	}
}

type userView struct {
	store.User
	SubscriptionURL string         `json:"subscription_url"`
	IOSURL          string         `json:"ios_url"`
	Assigned        []assignedView `json:"assigned"`
}

type assignedView struct {
	Label string `json:"label"`
	Addr  string `json:"addr"`
	Alive bool   `json:"alive"`
}

func (a *API) view(r *http.Request, u store.User) userView {
	v := userView{User: u, SubscriptionURL: a.baseURL(r) + "/sub/" + u.Key, IOSURL: a.baseURL(r) + "/ios#" + u.Key}
	v.Assigned = []assignedView{}
	for _, id := range u.Proxies {
		if px, ok := a.pool.Get(id); ok {
			v.Assigned = append(v.Assigned, assignedView{pool.Label(px.Country, "без страны"), px.Addr(), px.Usable()})
		}
	}
	sort.Slice(v.Assigned, func(i, j int) bool { return v.Assigned[i].Label < v.Assigned[j].Label })
	return v
}

func (a *API) listUsers(w http.ResponseWriter, r *http.Request) {
	list := []userView{}
	for _, u := range a.store.List() {
		list = append(list, a.view(r, u))
	}
	writeJSON(w, 200, list)
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "user"
	}
	u, err := a.store.Create(name, "admin")
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, a.view(r, u))
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	if err := a.store.Delete(r.PathValue("id")); err != nil {
		errJSON(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) setDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := a.store.SetDisabled(r.PathValue("id"), disabled); err != nil {
			errJSON(w, 404, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func (a *API) reassign(w http.ResponseWriter, r *http.Request) {
	var target store.User
	found := false
	for _, u := range a.store.List() {
		if u.ID == r.PathValue("id") {
			target, found = u, true
		}
	}
	if !found {
		errJSON(w, 404, store.ErrNotFound.Error())
		return
	}
	if _, err := a.assign(target, true); err != nil {
		errJSON(w, 503, msg(r, "noProxies"))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

type proxyView struct {
	pool.Proxy
	Users int    `json:"users"`
	Label string `json:"label"`
}

func (a *API) listProxies(w http.ResponseWriter, r *http.Request) {
	load := a.load()
	list := []proxyView{}
	for _, px := range a.pool.List() {
		px.Password = "" // never send passwords to the browser
		list = append(list, proxyView{px, load[px.ID], pool.Label(px.Country, "страна не определена")})
	}
	writeJSON(w, 200, map[string]any{"proxies": list, "errors": a.pool.Errors(), "dir": a.pool.Dir()})
}

func (a *API) checkProxies(w http.ResponseWriter, r *http.Request) {
	a.pool.CheckNow()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	total, alive := 0, 0
	for _, px := range a.pool.List() {
		total++
		if px.Alive {
			alive++
		}
	}
	writeJSON(w, 200, map[string]any{
		"proxies_total":     total,
		"proxies_alive":     alive,
		"proxies_dir":       a.pool.Dir(),
		"open_registration": a.cfg.OpenRegistration,
	})
}
