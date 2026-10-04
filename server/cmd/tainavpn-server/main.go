// tainavpn-server: hands out upstream proxies to the Tainavpn apps.
//
// Proxies are taken from text files in TVPN_PROXIES_DIR; every app key gets
// its own proxy from that list. The VPS itself never relays user traffic.
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
	"github.com/nshimanovskiy/tainavpn/server/internal/pool"
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
		PublicURL:        env("TVPN_PUBLIC_URL", ""),
		AdminToken:       env("TVPN_ADMIN_TOKEN", ""),
		AdminPath:        env("TVPN_ADMIN_PATH", "/panel"),
		OpenRegistration: envBool("TVPN_OPEN_REGISTRATION", false),
		RegisterPerDay:   envInt("TVPN_REGISTER_PER_DAY", 3),
	}
	if len(cfg.AdminToken) < 16 {
		log.Fatal("TVPN_ADMIN_TOKEN must be at least 16 characters")
	}

	dataDir := env("TVPN_DATA_DIR", "/data")
	st, err := store.Open(dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	// upstream proxies are read from text files in this folder (one user:pass@host:port per line)
	pl := pool.New(env("TVPN_PROXIES_DIR", filepath.Join(dataDir, "proxies")), filepath.Join(dataDir, "geo.json"))
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go pl.Run(ctx)

	srv := &http.Server{
		Addr:              env("TVPN_API_LISTEN", "127.0.0.1:8090"),
		Handler:           api.New(cfg, st, pl).Handler(),
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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
