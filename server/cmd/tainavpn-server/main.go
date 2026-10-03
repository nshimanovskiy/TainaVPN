// tainavpn-server: hands out SOCKS5 proxies to the Tainavpn apps.
//
// It runs an embedded sing-box SOCKS5 server (one login/password per user)
// and an HTTP API that the apps use to fetch their proxy ("subscription").
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nshimanovskiy/tainavpn/server/internal/api"
	"github.com/nshimanovskiy/tainavpn/server/internal/proxy"
	"github.com/nshimanovskiy/tainavpn/server/internal/store"
)

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(env(key, "")); err == nil {
		return v
	}
	return def
}

func envBool(key string, def bool) bool {
	switch strings.ToLower(env(key, "")) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func main() {
	log.SetFlags(log.LstdFlags)
	cfg := api.Config{
		Name:             env("TVPN_NAME", "Tainavpn"),
		PublicHost:       env("TVPN_PUBLIC_HOST", ""),
		SocksPort:        envInt("TVPN_SOCKS_PORT", 1080),
		PublicURL:        env("TVPN_PUBLIC_URL", ""),
		AdminToken:       env("TVPN_ADMIN_TOKEN", ""),
		AdminPath:        env("TVPN_ADMIN_PATH", "/panel"),
		OpenRegistration: envBool("TVPN_OPEN_REGISTRATION", false),
		RegisterPerDay:   envInt("TVPN_REGISTER_PER_DAY", 3),
	}
	if cfg.PublicHost == "" {
		log.Fatal("TVPN_PUBLIC_HOST is required (domain or IP of the VPS that clients connect to)")
	}
	if len(cfg.AdminToken) < 16 {
		log.Fatal("TVPN_ADMIN_TOKEN must be at least 16 characters")
	}

	dataDir := env("TVPN_DATA_DIR", "/data")
	st, err := store.Open(dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	px := proxy.New(env("TVPN_SOCKS_LISTEN", "::"), cfg.SocksPort, st, filepath.Join(dataDir, "core"))
	st.OnChange(px.Reload)
	_ = px.Start()

	srv := &http.Server{
		Addr:              env("TVPN_API_LISTEN", "127.0.0.1:8090"),
		Handler:           api.New(cfg, st, px).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("API on http://%s, admin panel at %s", srv.Addr, cfg.AdminPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	px.Close()
}
