// tainavpn-bot: Telegram bot that gives users their Tainavpn subscription link
// and the app files. Runs next to tainavpn-server (same host network) and talks
// to it via the local bot API protected by TVPN_BOT_SECRET.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	token     string
	tgAPI     string // https://api.telegram.org
	serverAPI string // http://127.0.0.1:8090
	secret    string
	allowed   map[string]bool // telegram ids / lowercase usernames; empty = everyone
	downloads string
}

type bot struct {
	cfg    config
	client *http.Client

	mu       sync.Mutex
	fileIDs  map[string]string // path|mtime -> telegram file_id
	lastSeen map[int64]time.Time
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(log.LstdFlags)
	cfg := config{
		token:     env("TVPN_BOT_TOKEN", ""),
		tgAPI:     strings.TrimRight(env("TVPN_TG_API", "https://api.telegram.org"), "/"),
		serverAPI: strings.TrimRight(env("TVPN_SERVER_API", "http://127.0.0.1:"+port(env("TVPN_API_LISTEN", "127.0.0.1:8090"))), "/"),
		secret:    env("TVPN_BOT_SECRET", ""),
		downloads: env("TVPN_DOWNLOADS_DIR", "/downloads"),
		allowed:   map[string]bool{},
	}
	for _, a := range strings.Split(env("TVPN_BOT_ALLOWED", ""), ",") {
		if a = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a), "@")); a != "" {
			cfg.allowed[a] = true
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if cfg.token == "" || cfg.secret == "" {
		// don't crash-loop under "restart: unless-stopped": wait until configured
		log.Printf("TVPN_BOT_TOKEN or TVPN_BOT_SECRET is not set in .env — the bot is idle")
		<-ctx.Done()
		return
	}
	b := &bot{cfg: cfg, client: &http.Client{Timeout: 70 * time.Second}, fileIDs: map[string]string{}, lastSeen: map[int64]time.Time{}}
	b.setCommands()
	log.Printf("bot started")
	b.run(ctx)
}

func port(listen string) string {
	if i := strings.LastIndex(listen, ":"); i >= 0 {
		return listen[i+1:]
	}
	return "8090"
}

// ---------- Telegram API ----------

type user struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	LanguageCode string `json:"language_code"`
}

type message struct {
	MessageID int64 `json:"message_id"`
	From      *user `json:"from"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Text string `json:"text"`
}

type update struct {
	UpdateID      int64    `json:"update_id"`
	Message       *message `json:"message"`
	CallbackQuery *struct {
		ID      string   `json:"id"`
		From    user     `json:"from"`
		Message *message `json:"message"`
		Data    string   `json:"data"`
	} `json:"callback_query"`
}

func (b *bot) call(method string, params any, out any) error {
	body, _ := json.Marshal(params)
	resp, err := b.client.Post(b.cfg.tgAPI+"/bot"+b.cfg.token+"/"+method, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeTG(resp.Body, out)
}

func decodeTG(r io.Reader, out any) error {
	var res struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(r).Decode(&res); err != nil {
		return err
	}
	if !res.OK {
		return errors.New(res.Description)
	}
	if out != nil {
		return json.Unmarshal(res.Result, out)
	}
	return nil
}

func (b *bot) run(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		var updates []update
		err := b.call("getUpdates", map[string]any{
			"offset": offset, "timeout": 50, "allowed_updates": []string{"message", "callback_query"},
		}, &updates)
		if err != nil {
			log.Printf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			b.handle(u)
		}
	}
}

type button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

func (b *bot) send(chatID int64, text string, keyboard [][]button) {
	params := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if keyboard != nil {
		params["reply_markup"] = map[string]any{"inline_keyboard": keyboard}
	}
	if err := b.call("sendMessage", params, nil); err != nil {
		log.Printf("sendMessage: %v", err)
	}
}

func (b *bot) setCommands() {
	for _, lang := range []string{"ru", "en"} {
		t := texts[lang]
		params := map[string]any{"commands": []map[string]string{
			{"command": "link", "description": t["cmdLink"]},
			{"command": "app", "description": t["cmdApp"]},
			{"command": "help", "description": t["cmdHelp"]},
		}}
		if lang == "ru" {
			params["language_code"] = "ru"
		}
		_ = b.call("setMyCommands", params, nil)
	}
}

// ---------- logic ----------

func langOf(u *user) string {
	if u == nil {
		return "ru"
	}
	switch strings.ToLower(u.LanguageCode) {
	case "ru", "uk", "be", "kk", "uz", "ky", "tg", "hy", "az":
		return "ru"
	case "":
		return "ru"
	}
	return "en"
}

func displayName(u *user) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

func (b *bot) allowed(u *user) bool {
	if len(b.cfg.allowed) == 0 {
		return true
	}
	return b.cfg.allowed[strconv.FormatInt(u.ID, 10)] || (u.Username != "" && b.cfg.allowed[strings.ToLower(u.Username)])
}

// tooFast drops bursts (more than one action per second per user)
func (b *bot) tooFast(id int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.lastSeen[id]; ok && time.Since(t) < time.Second {
		return true
	}
	b.lastSeen[id] = time.Now()
	return false
}

func (b *bot) handle(u update) {
	var from *user
	var chatID int64
	var action string
	switch {
	case u.Message != nil && u.Message.From != nil:
		if u.Message.Chat.Type != "private" {
			return
		}
		from, chatID = u.Message.From, u.Message.Chat.ID
		cmd := strings.ToLower(strings.Fields(u.Message.Text + " x")[0])
		if i := strings.Index(cmd, "@"); i > 0 {
			cmd = cmd[:i]
		}
		switch cmd {
		case "/link":
			action = "link"
		case "/new":
			action = "new"
		case "/app":
			action = "app"
		case "/help":
			action = "help"
		default:
			action = "start"
		}
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		from, chatID, action = &u.CallbackQuery.From, u.CallbackQuery.Message.Chat.ID, u.CallbackQuery.Data
		_ = b.call("answerCallbackQuery", map[string]any{"callback_query_id": u.CallbackQuery.ID}, nil)
	default:
		return
	}
	if b.tooFast(from.ID) {
		return
	}
	t := texts[langOf(from)]
	if !b.allowed(from) {
		b.send(chatID, fmt.Sprintf(t["denied"], from.ID), nil)
		return
	}
	switch {
	case action == "start":
		b.send(chatID, t["welcome"], mainMenu(t))
	case action == "link":
		b.sendLink(chatID, from, t, false)
	case action == "new":
		b.send(chatID, t["newConfirm"], [][]button{{{Text: t["btnNewYes"], CallbackData: "new-yes"}, {Text: t["btnCancel"], CallbackData: "start"}}})
	case action == "new-yes":
		b.sendLink(chatID, from, t, true)
	case action == "app":
		b.send(chatID, t["chooseApp"], [][]button{
			{{Text: "🪟 Windows", CallbackData: "file:windows"}},
			{{Text: "🤖 Android", CallbackData: "file:android"}},
			{{Text: "🐧 Ubuntu", CallbackData: "file:ubuntu"}},
		})
	case strings.HasPrefix(action, "file:"):
		b.sendApp(chatID, strings.TrimPrefix(action, "file:"), t)
	case action == "help":
		b.send(chatID, t["help"], mainMenu(t))
	default:
		b.send(chatID, t["welcome"], mainMenu(t))
	}
}

func mainMenu(t map[string]string) [][]button {
	return [][]button{
		{{Text: t["btnLink"], CallbackData: "link"}},
		{{Text: t["btnApp"], CallbackData: "app"}, {Text: t["btnHelp"], CallbackData: "help"}},
		{{Text: t["btnNew"], CallbackData: "new"}},
	}
}

func (b *bot) sendLink(chatID int64, from *user, t map[string]string, rotate bool) {
	body, _ := json.Marshal(map[string]any{"telegram_id": from.ID, "name": displayName(from), "rotate": rotate})
	req, _ := http.NewRequest("POST", b.cfg.serverAPI+"/api/bot/subscription", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.cfg.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.client.Do(req)
	if err != nil {
		log.Printf("server: %v", err)
		b.send(chatID, t["serverDown"], nil)
		return
	}
	defer resp.Body.Close()
	var r struct {
		URL       string   `json:"subscription_url"`
		Disabled  bool     `json:"disabled"`
		Countries []string `json:"countries"`
		Error     string   `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if resp.StatusCode != 200 || r.URL == "" {
		log.Printf("server: HTTP %d %s", resp.StatusCode, r.Error)
		b.send(chatID, t["serverDown"], nil)
		return
	}
	if r.Disabled {
		b.send(chatID, t["disabled"], nil)
		return
	}
	var flags []string
	sort.Strings(r.Countries)
	for _, c := range r.Countries {
		flags = append(flags, flag(c))
	}
	text := t["link"]
	if rotate {
		text = t["linkNew"]
	}
	text = fmt.Sprintf(text, html.EscapeString(r.URL))
	if len(flags) > 0 {
		text += "\n\n" + t["countries"] + " " + strings.Join(flags, " ")
	}
	text += "\n\n" + t["howTo"]
	b.send(chatID, text, [][]button{{{Text: t["btnApp"], CallbackData: "app"}}})
}

func flag(code string) string {
	code = strings.ToUpper(code)
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return "🌐"
	}
	return string(rune(0x1F1E6+rune(code[0]-'A'))) + string(rune(0x1F1E6+rune(code[1]-'A')))
}

var appPatterns = map[string]string{
	"windows": "*windows*.exe",
	"android": "*android*.apk",
	"ubuntu":  "*.deb",
}

// newest returns the most recent file of a platform in the downloads folder.
func (b *bot) newest(platform string) (string, os.FileInfo) {
	files, _ := filepath.Glob(filepath.Join(b.cfg.downloads, appPatterns[platform]))
	var best string
	var bestInfo os.FileInfo
	for _, f := range files {
		if st, err := os.Stat(f); err == nil && (bestInfo == nil || st.ModTime().After(bestInfo.ModTime())) {
			best, bestInfo = f, st
		}
	}
	return best, bestInfo
}

func (b *bot) sendApp(chatID int64, platform string, t map[string]string) {
	path, info := b.newest(platform)
	if path == "" {
		b.send(chatID, t["noFile"], nil)
		return
	}
	caption := t["caption_"+platform]
	key := path + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	b.mu.Lock()
	fileID := b.fileIDs[key]
	b.mu.Unlock()
	if fileID != "" {
		if err := b.call("sendDocument", map[string]any{"chat_id": chatID, "document": fileID, "caption": caption, "parse_mode": "HTML"}, nil); err == nil {
			return
		}
	}
	_ = b.call("sendChatAction", map[string]any{"chat_id": chatID, "action": "upload_document"}, nil)
	id, err := b.upload(chatID, path, caption)
	if err != nil {
		log.Printf("sendDocument %s: %v", path, err)
		b.send(chatID, t["uploadFailed"], nil)
		return
	}
	b.mu.Lock()
	b.fileIDs[key] = id
	b.mu.Unlock()
}

func (b *bot) upload(chatID int64, path, caption string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		_ = mw.WriteField("chat_id", strconv.FormatInt(chatID, 10))
		_ = mw.WriteField("caption", caption)
		_ = mw.WriteField("parse_mode", "HTML")
		part, err := mw.CreateFormFile("document", filepath.Base(path))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Post(b.cfg.tgAPI+"/bot"+b.cfg.token+"/sendDocument", mw.FormDataContentType(), pr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var msg struct {
		Document struct {
			FileID string `json:"file_id"`
		} `json:"document"`
	}
	if err := decodeTG(resp.Body, &msg); err != nil {
		return "", err
	}
	return msg.Document.FileID, nil
}
