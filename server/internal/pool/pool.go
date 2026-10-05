// Package pool reads upstream proxies from text files in a folder and keeps
// track of which of them are alive.
//
// File format (any *.txt file in the folder, one proxy per line):
//
//	user:pass@host:port            SOCKS5 (default)
//	socks5://user:pass@host:port   SOCKS5
//	http://user:pass@host:port     HTTP CONNECT proxy
//	host:port:user:pass            SOCKS5, alternative notation
//	user:pass@host:port DE         country set manually (otherwise detected by exit IP)
//	# comment
package pool

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Proxy struct {
	ID         string    `json:"id"`
	Type       string    `json:"type"` // "socks" or "http"
	Host       string    `json:"host"`
	Port       int       `json:"port"`
	Username   string    `json:"username,omitempty"`
	Password   string    `json:"password,omitempty"`
	File       string    `json:"file"`
	Line       int       `json:"line"`
	Country    string    `json:"country,omitempty"`        // ISO code, "" if unknown
	CountrySet bool      `json:"country_manual,omitempty"` // set in the file, not detected
	CountryBy  string    `json:"country_source,omitempty"` // service that detected the country
	Alive      bool      `json:"alive"`
	CheckedAt  time.Time `json:"checked_at,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Usable reports whether the proxy may be handed out (alive or not checked yet).
func (p Proxy) Usable() bool { return p.Alive || p.CheckedAt.IsZero() }

func (p Proxy) Addr() string { return net.JoinHostPort(p.Host, strconv.Itoa(p.Port)) }

type Pool struct {
	dir     string
	geoPath string
	geoMu   sync.Mutex
	geo     map[string]geoEntry

	mu       sync.RWMutex
	proxies  []*Proxy
	byID     map[string]*Proxy
	errors   []string
	sig      string
	loadedAt time.Time

	checkNow chan struct{}
}

type geoEntry struct {
	Country string    `json:"country"`
	Source  string    `json:"source,omitempty"` // service that answered; ipinfo.io is the reference
	At      time.Time `json:"at"`
}

// New creates a pool reading dir; detected countries are cached in geoPath.
func New(dir, geoPath string) *Pool {
	p := &Pool{dir: dir, geoPath: geoPath, geo: map[string]geoEntry{}, byID: map[string]*Proxy{}, checkNow: make(chan struct{}, 1)}
	if data, err := os.ReadFile(geoPath); err == nil {
		_ = json.Unmarshal(data, &p.geo)
	}
	return p
}

func (p *Pool) saveGeo() {
	p.geoMu.Lock()
	data, _ := json.MarshalIndent(p.geo, "", "  ")
	p.geoMu.Unlock()
	if p.geoPath != "" {
		_ = os.WriteFile(p.geoPath, data, 0o600)
	}
}

func (p *Pool) Dir() string { return p.dir }

// Run reloads the folder every 10 seconds and health-checks proxies every 3 minutes.
func (p *Pool) Run(ctx context.Context) {
	_ = os.MkdirAll(p.dir, 0o755)
	if p.reload() {
		go p.checkAll()
	}
	reload := time.NewTicker(10 * time.Second)
	check := time.NewTicker(3 * time.Minute)
	defer reload.Stop()
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reload.C:
			if p.reload() {
				go p.checkAll()
			}
		case <-check.C:
			p.checkAll()
		case <-p.checkNow:
			p.checkAll()
		}
	}
}

// CheckNow schedules an immediate health check.
func (p *Pool) CheckNow() {
	select {
	case p.checkNow <- struct{}{}:
	default:
	}
}

// signature of the folder content, to detect changes cheaply
func (p *Pool) signature() (string, []string) {
	files, _ := filepath.Glob(filepath.Join(p.dir, "*.txt"))
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d;", f, st.Size(), st.ModTime().UnixNano())
		}
	}
	return b.String(), files
}

// reload re-reads the folder if it changed; returns true if the list changed.
func (p *Pool) reload() bool {
	sig, files := p.signature()
	p.mu.RLock()
	same := sig == p.sig && !p.loadedAt.IsZero()
	p.mu.RUnlock()
	if same {
		return false
	}
	var list []*Proxy
	var errs []string
	seen := map[string]bool{}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		sc := bufio.NewScanner(fh)
		n := 0
		for sc.Scan() {
			n++
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			px, err := Parse(line)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s:%d: %v", filepath.Base(f), n, err))
				continue
			}
			if seen[px.ID] {
				continue
			}
			seen[px.ID] = true
			px.File = filepath.Base(f)
			px.Line = n
			list = append(list, px)
		}
		fh.Close()
	}
	p.mu.Lock()
	// keep health state of proxies that are still present
	p.geoMu.Lock()
	for _, px := range list {
		if old := p.byID[px.ID]; old != nil {
			px.Alive, px.CheckedAt, px.Error = old.Alive, old.CheckedAt, old.Error
		}
		if !px.CountrySet {
			px.Country, px.CountryBy = p.geo[px.ID].Country, p.geo[px.ID].Source
		}
	}
	p.geoMu.Unlock()
	byID := map[string]*Proxy{}
	for _, px := range list {
		byID[px.ID] = px
	}
	p.proxies, p.byID, p.errors, p.sig, p.loadedAt = list, byID, errs, sig, time.Now()
	p.mu.Unlock()
	log.Printf("pool: %d proxies loaded from %s (%d errors)", len(list), p.dir, len(errs))
	for _, e := range errs {
		log.Printf("pool: %s", e)
	}
	return true
}

func (p *Pool) checkAll() {
	p.mu.RLock()
	list := make([]Proxy, 0, len(p.proxies))
	for _, px := range p.proxies {
		list = append(list, *px)
	}
	p.mu.RUnlock()

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	geoChanged := false
	for _, px := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(px Proxy) {
			defer wg.Done()
			defer func() { <-sem }()
			err := Check(px, 8*time.Second)
			country, source := "", ""
			if err == nil && !px.CountrySet && p.needGeo(px.ID) {
				if c, src, gerr := DetectCountrySource(px, 15*time.Second); gerr == nil {
					country, source = c, src
					p.geoMu.Lock()
					if old := p.geo[px.ID]; old.Country != "" && old.Country != c {
						log.Printf("pool: country of %s changed %s -> %s (%s)", px.Addr(), old.Country, c, src)
					}
					p.geo[px.ID] = geoEntry{Country: c, Source: src, At: time.Now()}
					geoChanged = true
					p.geoMu.Unlock()
				} else {
					log.Printf("pool: country of %s: %v", px.Addr(), gerr)
				}
			}
			p.mu.Lock()
			if cur := p.byID[px.ID]; cur != nil {
				cur.CheckedAt = time.Now()
				cur.Alive = err == nil
				cur.Error = ""
				if err != nil {
					cur.Error = err.Error()
				}
				if country != "" {
					cur.Country, cur.CountryBy = country, source
				}
			}
			p.mu.Unlock()
		}(px)
	}
	wg.Wait()
	p.geoMu.Lock()
	changed := geoChanged
	p.geoMu.Unlock()
	if changed {
		p.saveGeo()
	}
}

// needGeo reports whether the proxy's country must be (re)checked: unknown,
// not confirmed by ipinfo.io yet (retried hourly), or older than a day.
func (p *Pool) needGeo(id string) bool {
	p.geoMu.Lock()
	defer p.geoMu.Unlock()
	e, ok := p.geo[id]
	if !ok || e.Country == "" {
		return true
	}
	if e.Source != SourceIPInfo {
		return time.Since(e.At) > time.Hour || e.Source == ""
	}
	return time.Since(e.At) > 24*time.Hour
}

func (p *Pool) List() []Proxy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Proxy, 0, len(p.proxies))
	for _, px := range p.proxies {
		out = append(out, *px)
	}
	return out
}

func (p *Pool) Errors() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string{}, p.errors...)
}

func (p *Pool) Get(id string) (Proxy, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if px := p.byID[id]; px != nil {
		return *px, true
	}
	return Proxy{}, false
}

// Countries returns the distinct countries of usable proxies, sorted ("" = unknown, last).
func (p *Pool) Countries() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, px := range p.proxies {
		if px.Usable() && !seen[px.Country] {
			seen[px.Country] = true
			out = append(out, px.Country)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i] == "") != (out[j] == "") {
			return out[j] == ""
		}
		return CountryName(out[i]) < CountryName(out[j])
	})
	return out
}

// Pick returns the least loaded usable proxy of the given country
// (load = number of users per proxy ID). If none is usable it falls back to
// the least loaded proxy of that country.
func (p *Pool) Pick(country string, load map[string]int, exclude string) (Proxy, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pick := func(usableOnly bool) *Proxy {
		var best *Proxy
		for _, px := range p.proxies {
			if px.Country != country || px.ID == exclude || (usableOnly && !px.Usable()) {
				continue
			}
			if best == nil || load[px.ID] < load[best.ID] {
				best = px
			}
		}
		return best
	}
	best := pick(true)
	if best == nil {
		best = pick(false)
	}
	if best == nil && exclude != "" {
		if px := p.byID[exclude]; px != nil && px.Country == country {
			best = px
		}
	}
	if best == nil {
		return Proxy{}, false
	}
	return *best, true
}

// Parse parses one proxy line.
func Parse(line string) (*Proxy, error) {
	s := strings.TrimSpace(line)
	country := ""
	if i := strings.Index(s, " #"); i >= 0 { // trailing comment
		s = strings.TrimSpace(s[:i])
	}
	if f := strings.Fields(s); len(f) == 2 && len(f[1]) == 2 && isLetters(f[1]) {
		s, country = f[0], strings.ToUpper(f[1])
	} else if len(f) > 1 {
		return nil, fmt.Errorf("unexpected text after the proxy")
	}
	typ := "socks"
	if i := strings.Index(s, "://"); i >= 0 {
		switch strings.ToLower(s[:i]) {
		case "socks", "socks5", "socks5h":
			typ = "socks"
		case "http":
			typ = "http"
		default:
			return nil, fmt.Errorf("unsupported scheme %q", s[:i])
		}
		s = s[i+3:]
	}
	s = strings.TrimRight(s, "/")
	var user, pass, hostport string
	if at := strings.LastIndex(s, "@"); at >= 0 {
		cred := s[:at]
		hostport = s[at+1:]
		if c := strings.Index(cred, ":"); c >= 0 {
			user, pass = cred[:c], cred[c+1:]
		} else {
			user = cred
		}
	} else {
		parts := strings.Split(s, ":")
		switch {
		case len(parts) == 2:
			hostport = s
		case len(parts) >= 4 && !strings.HasPrefix(s, "["):
			hostport = parts[0] + ":" + parts[1]
			user, pass = parts[2], strings.Join(parts[3:], ":")
		default:
			hostport = s
		}
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("expected user:pass@host:port")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 || host == "" {
		return nil, fmt.Errorf("bad host or port")
	}
	px := &Proxy{Type: typ, Host: host, Port: port, Username: user, Password: pass, Country: country, CountrySet: country != ""}
	h := sha1.Sum([]byte(typ + "|" + host + "|" + portStr + "|" + user + "|" + pass))
	px.ID = hex.EncodeToString(h[:6])
	return px, nil
}

// Check verifies that the proxy accepts our credentials.
func Check(px Proxy, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", px.Addr(), timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if px.Type == "http" {
		return nil // reachable is enough for HTTP proxies
	}
	return socksAuth(conn, px)
}

// socksAuth performs the SOCKS5 greeting and username/password authentication.
func socksAuth(conn net.Conn, px Proxy) error {
	method := byte(0x00)
	if px.Username != "" {
		method = 0x02
	}
	if _, err := conn.Write([]byte{0x05, 0x01, method}); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("not a SOCKS5 proxy: %v", err)
	}
	if resp[0] != 0x05 || resp[1] == 0xff {
		return errors.New("SOCKS5 auth method rejected")
	}
	if resp[1] == 0x02 {
		if len(px.Username) > 255 || len(px.Password) > 255 {
			return errors.New("credentials too long")
		}
		msg := []byte{0x01, byte(len(px.Username))}
		msg = append(msg, px.Username...)
		msg = append(msg, byte(len(px.Password)))
		msg = append(msg, px.Password...)
		if _, err := conn.Write(msg); err != nil {
			return err
		}
		if _, err := io.ReadFull(conn, resp); err != nil {
			return err
		}
		if resp[1] != 0x00 {
			return errors.New("wrong username or password")
		}
	}
	return nil
}

func isLetters(s string) bool {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// CountryName returns the Russian name of an ISO country code.
func CountryName(code string) string {
	if n, ok := countryNames[strings.ToUpper(code)]; ok {
		return n
	}
	return ""
}

// Flag returns the emoji flag for an ISO country code.
func Flag(code string) string {
	code = strings.ToUpper(code)
	if len(code) != 2 || !isLetters(code) {
		return "🌐"
	}
	return string(rune(0x1F1E6+rune(code[0]-'A'))) + string(rune(0x1F1E6+rune(code[1]-'A')))
}

// Label is "🇩🇪 Германия" (or "🌐 <fallback>" when the country is unknown).
func Label(code, fallback string) string {
	if n := CountryName(code); n != "" {
		return Flag(code) + " " + n
	}
	return "🌐 " + fallback
}
