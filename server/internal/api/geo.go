package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nshimanovskiy/tainavpn/server/internal/pool"
)

// geo: POST /api/v1/geo {type, server, port, username, password}
// Detects the country of a proxy the user added by hand, so the app can show
// a flag for it too. Rate limited; private/local addresses are refused.
func (a *API) geo(w http.ResponseWriter, r *http.Request) {
	if !a.allow(&a.geoRL, "geo:"+clientIP(r), 60, time.Hour) {
		errJSON(w, 429, msg(r, "tooMany"))
		return
	}
	var req struct {
		Type     string `json:"type"`
		Server   string `json:"server"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.Server == "" || req.Port <= 0 || req.Port > 65535 {
		errJSON(w, 400, "bad request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, req.Server)
	if err != nil || len(ips) == 0 {
		errJSON(w, 400, msg(r, "noHost"))
		return
	}
	for _, ip := range ips {
		if ip.IP.IsLoopback() || ip.IP.IsPrivate() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast() {
			errJSON(w, 400, msg(r, "localAddr"))
			return
		}
	}
	px := pool.Proxy{Type: "socks", Host: req.Server, Port: req.Port, Username: req.Username, Password: req.Password}
	if strings.EqualFold(req.Type, "http") {
		px.Type = "http"
	}
	country, err := pool.DetectCountry(px, 12*time.Second)
	if err != nil {
		errJSON(w, 502, msg(r, "proxyDown")+err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"country": country, "country_name": pool.CountryName(country)})
}
