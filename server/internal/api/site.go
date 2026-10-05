package api

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// The public home page (taina.sdsds.top) and direct downloads of the apps.

//go:embed site
var siteFS embed.FS

var appPatterns = map[string]string{
	"windows": "*windows*.exe",
	"android": "*android*.apk",
	"ubuntu":  "*.deb",
}

var versionRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// newestApp returns the newest file of a platform in the downloads folder.
func (a *API) newestApp(platform string) (string, os.FileInfo) {
	if a.cfg.DownloadsDir == "" || appPatterns[platform] == "" {
		return "", nil
	}
	files, _ := filepath.Glob(filepath.Join(a.cfg.DownloadsDir, appPatterns[platform]))
	var best string
	var bestInfo os.FileInfo
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() && (bestInfo == nil || st.ModTime().After(bestInfo.ModTime())) {
			best, bestInfo = f, st
		}
	}
	return best, bestInfo
}

func (a *API) siteRoutes(mux *http.ServeMux) {
	sub, _ := fs.Sub(siteFS, "site")
	static := http.FileServer(http.FS(sub))
	cached := func(h http.Handler) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			h.ServeHTTP(w, r)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		data, _ := fs.ReadFile(sub, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /favicon.png", cached(static))
	mux.HandleFunc("GET /apple-touch-icon.png", cached(static))
	mux.HandleFunc("GET /flags/{file}", cached(static))
	mux.HandleFunc("GET /api/v1/apps", a.apps)
	mux.HandleFunc("GET /download/{platform}", a.download)
}

// apps: GET /api/v1/apps -> {version, files: {windows: {name, size}, ...}}
func (a *API) apps(w http.ResponseWriter, r *http.Request) {
	files := map[string]any{}
	version := ""
	for p := range appPatterns {
		if path, st := a.newestApp(p); path != "" {
			files[p] = map[string]any{"name": filepath.Base(path), "size": st.Size()}
			if m := versionRe.FindString(filepath.Base(path)); m != "" {
				version = m
			}
		}
	}
	writeJSON(w, 200, map[string]any{"version": version, "files": files})
}

// download: GET /download/{windows|android|ubuntu} -> the newest file of that platform
func (a *API) download(w http.ResponseWriter, r *http.Request) {
	path, st := a.newestApp(r.PathValue("platform"))
	if path == "" {
		http.Redirect(w, r, "https://github.com/nshimanovskiy/TainaVPN/releases/latest", http.StatusFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	switch filepath.Ext(path) {
	case ".apk":
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	case ".deb":
		w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeContent(w, r, filepath.Base(path), st.ModTime().Truncate(time.Second), f)
}
