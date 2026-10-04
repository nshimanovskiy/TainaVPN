// Package store keeps proxy users in a JSON file.
package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	ProxyID   string    `json:"proxy_id,omitempty"` // proxy from the pool assigned to this user
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen,omitempty"`
	LastIP    string    `json:"last_ip,omitempty"`
	Source    string    `json:"source,omitempty"` // "admin" or "app"
}

var ErrNotFound = errors.New("user not found")

type Store struct {
	mu    sync.RWMutex
	path  string
	users map[string]*User
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "users.json"), users: map[string]*User{}}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var list []*User
		if err := json.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		for _, u := range list {
			s.users[u.ID] = u
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	list := s.listLocked()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) listLocked() []*User {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	return list
}

func (s *Store) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []User{}
	for _, u := range s.listLocked() {
		out = append(out, *u)
	}
	return out
}

func (s *Store) Create(name, source string) (User, error) {
	s.mu.Lock()
	id := randomHex(4)
	for s.users[id] != nil {
		id = randomHex(4)
	}
	u := &User{
		ID:        id,
		Name:      name,
		Key:       randomToken(24),
		CreatedAt: time.Now().UTC(),
		Source:    source,
	}
	s.users[id] = u
	err := s.saveLocked()
	s.mu.Unlock()
	if err != nil {
		return User{}, err
	}
	return *u, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	if s.users[id] == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	delete(s.users, id)
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

func (s *Store) SetDisabled(id string, disabled bool) error {
	s.mu.Lock()
	u := s.users[id]
	if u == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	u.Disabled = disabled
	err := s.saveLocked()
	s.mu.Unlock()
	return err
}

// SetProxy assigns a pool proxy to the user ("" clears the assignment).
func (s *Store) SetProxy(id, proxyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[id]
	if u == nil {
		return ErrNotFound
	}
	u.ProxyID = proxyID
	return s.saveLocked()
}

// ByKey finds a user by access key and records the visit.
func (s *Store) ByKey(key, ip string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Key == key {
			u.LastSeen = time.Now().UTC()
			u.LastIP = ip
			_ = s.saveLocked()
			return *u, true
		}
	}
	return User{}, false
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
