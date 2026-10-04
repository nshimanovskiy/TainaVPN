// Package pool reads upstream proxies from text files in a folder and keeps
// track of which of them are alive.
//
// File format (any *.txt file in the folder, one proxy per line):
//
//	user:pass@host:port            SOCKS5 (default)
//	socks5://user:pass@host:port   SOCKS5
//	http://user:pass@host:port     HTTP CONNECT proxy
//	host:port:user:pass            SOCKS5, alternative notation
//	# comment
package pool

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
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
	ID        string    `json:"id"`
	Type      string    `json:"type"` // "socks" or "http"
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	File      string    `json:"file"`
	Line      int       `json:"line"`
	Alive     bool      `json:"alive"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// Usable reports whether the proxy may be handed out (alive or not checked yet).
func (p Proxy) Usable() bool { return p.Alive || p.CheckedAt.IsZero() }

func (p Proxy) Addr() string { return net.JoinHostPort(p.Host, strconv.Itoa(p.Port)) }

type Pool struct {
	dir string

	mu       sync.RWMutex
	proxies  []*Proxy
	byID     map[string]*Proxy
	errors   []string
	sig      string
	loadedAt time.Time

	checkNow chan struct{}
}

func New(dir string) *Pool {
	return &Pool{dir: dir, byID: map[string]*Proxy{}, checkNow: make(chan struct{}, 1)}
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
	for _, px := range list {
		if old := p.byID[px.ID]; old != nil {
			px.Alive, px.CheckedAt, px.Error = old.Alive, old.CheckedAt, old.Error
		}
	}
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

	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for _, px := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(px Proxy) {
			defer wg.Done()
			defer func() { <-sem }()
			err := Check(px, 8*time.Second)
			p.mu.Lock()
			if cur := p.byID[px.ID]; cur != nil {
				cur.CheckedAt = time.Now()
				cur.Alive = err == nil
				cur.Error = ""
				if err != nil {
					cur.Error = err.Error()
				}
			}
			p.mu.Unlock()
		}(px)
	}
	wg.Wait()
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

// Pick returns the least loaded usable proxy (load = number of users per proxy ID).
// If no proxy is usable, it falls back to the least loaded proxy of all.
func (p *Pool) Pick(load map[string]int, exclude string) (Proxy, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	pick := func(usableOnly bool) *Proxy {
		var best *Proxy
		for _, px := range p.proxies {
			if px.ID == exclude || (usableOnly && !px.Usable()) {
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
		if px := p.byID[exclude]; px != nil {
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
	px := &Proxy{Type: typ, Host: host, Port: port, Username: user, Password: pass}
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
