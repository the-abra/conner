package client

import (
	"encoding/json"
	"os"
	"sync"
)

type IdentityStore struct {
	Identities map[string]string `json:"identities"` // nick -> pubkey_b64
	path       string
	mu         sync.Mutex
	mismatch   map[string]string // nick -> new key seen (not trusted)
}

func NewIdentityStore(path string) *IdentityStore {
	s := &IdentityStore{
		Identities: make(map[string]string),
		mismatch:   make(map[string]string),
		path:       path,
	}
	s.load()
	return s
}

func (s *IdentityStore) load() {
	data, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(data, &s.Identities)
	}
}

func (s *IdentityStore) save() {
	data, err := json.MarshalIndent(s.Identities, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// Check returns (isOK, isNew)
func (s *IdentityStore) HasMismatch() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mismatch) > 0
}

func (s *IdentityStore) PendingKey(nick string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.mismatch[nick]
	return k, ok
}

func (s *IdentityStore) Trust(nick, pubKeyB64 string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if pubKeyB64 == "" {
		pubKeyB64 = s.mismatch[nick]
	}
	if pubKeyB64 == "" {
		return
	}
	s.Identities[nick] = pubKeyB64
	delete(s.mismatch, nick)
	s.save()
}

func (s *IdentityStore) Check(nick, pubKeyB64 string) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldKey, ok := s.Identities[nick]
	if !ok {
		// New user, pin it
		s.Identities[nick] = pubKeyB64
		s.save()
		return true, true // ok, first time
	}

	if oldKey != pubKeyB64 {
		s.mismatch[nick] = pubKeyB64
		return false, false
	}
	delete(s.mismatch, nick)

	return true, false // ok, already known
}
