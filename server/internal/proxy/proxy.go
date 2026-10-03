// Package proxy runs the embedded sing-box SOCKS5 server for all enabled users.
package proxy

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/nshimanovskiy/tainavpn/core"
	"github.com/nshimanovskiy/tainavpn/server/internal/store"
)

type Server struct {
	mu       sync.Mutex
	listen   string
	port     int
	store    *store.Store
	instance *core.Instance
	timer    *time.Timer
	logs     *core.LogBuffer
	lastErr  string
	dataDir  string
}

func New(listen string, port int, st *store.Store, dataDir string) *Server {
	return &Server{listen: listen, port: port, store: st, logs: core.NewLogBuffer(200), dataDir: dataDir}
}

// BuildConfig renders the sing-box server config.
func (s *Server) BuildConfig() (string, int) {
	type user struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	users := []user{}
	for _, u := range s.store.List() {
		if !u.Disabled {
			users = append(users, user{u.Username, u.Password})
		}
	}
	inbounds := []any{}
	if len(users) > 0 { // never expose a SOCKS server without authentication
		inbounds = append(inbounds, map[string]any{
			"type":        "socks",
			"tag":         "socks-in",
			"listen":      s.listen,
			"listen_port": s.port,
			"users":       users,
		})
	}
	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": []any{map[string]any{"type": "local", "tag": "local"}},
		},
		"inbounds": inbounds,
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{
			"rules": []any{
				map[string]any{"action": "resolve"},
				// do not let clients reach the VPS's own private networks / localhost services
				map[string]any{"ip_is_private": true, "action": "reject"},
			},
			"final": "direct",
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data), len(users)
}

// Reload restarts sing-box with the current users (debounced).
func (s *Server) Reload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(500*time.Millisecond, s.restart)
}

func (s *Server) Start() error {
	s.restart()
	if s.lastErr != "" {
		log.Printf("sing-box: %s", s.lastErr)
	}
	return nil
}

func (s *Server) restart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instance != nil {
		_ = s.instance.Close()
		s.instance = nil
	}
	config, n := s.BuildConfig()
	if n == 0 {
		s.lastErr = ""
		log.Printf("sing-box: no active users, SOCKS server is not listening")
		return
	}
	inst, err := core.Start(config, s.dataDir, nil, s.logs)
	if err != nil {
		s.lastErr = err.Error()
		log.Printf("sing-box start failed: %v", err)
		return
	}
	s.lastErr = ""
	s.instance = inst
	log.Printf("sing-box: SOCKS5 on %s:%d for %d user(s)", s.listen, s.port, n)
}

func (s *Server) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"running":      s.instance != nil,
		"error":        s.lastErr,
		"port":         s.port,
		"core_version": core.Version(),
		"logs":         s.logs.String(),
	}
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.instance != nil {
		_ = s.instance.Close()
		s.instance = nil
	}
}
