package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// bot protects endpoints used by the Telegram bot container: they need the
// shared secret and must come directly from the host (not through nginx).
func (a *API) bot(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Real-IP") != "" || r.Header.Get("X-Forwarded-For") != "" {
			errJSON(w, 404, "not found")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if a.cfg.BotSecret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.cfg.BotSecret)) != 1 {
			time.Sleep(300 * time.Millisecond)
			errJSON(w, 401, "unauthorized")
			return
		}
		next(w, r)
	}
}

// botSubscription: POST /api/bot/subscription {telegram_id, name, rotate}
// Finds or creates the user of a Telegram account and returns their subscription link.
// rotate=true issues a new key (the old link stops working).
func (a *API) botSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TelegramID int64  `json:"telegram_id"`
		Name       string `json:"name"`
		Rotate     bool   `json:"rotate"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || req.TelegramID == 0 {
		errJSON(w, 400, "bad request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > 64 {
		name = name[:64]
	}
	u, created, err := a.store.ForTelegram(req.TelegramID, name)
	if err != nil {
		errJSON(w, 500, err.Error())
		return
	}
	if req.Rotate && !created {
		if u, err = a.store.RotateKey(u.ID); err != nil {
			errJSON(w, 500, err.Error())
			return
		}
	}
	countries := []string{}
	if !u.Disabled {
		if list, err := a.assign(u, false); err == nil {
			for _, px := range list {
				countries = append(countries, px.Country)
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"subscription_url": a.baseURL(r) + "/sub/" + u.Key,
		"disabled":         u.Disabled,
		"created":          created,
		"countries":        countries,
	})
}
