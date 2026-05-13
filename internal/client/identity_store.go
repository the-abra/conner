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
}

func NewIdentityStore(path string) *IdentityStore {
	s := &IdentityStore{
		Identities: make(map[string]string),
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
	data, _ := json.MarshalIndent(s.Identities, "", "  ")
	_ = os.WriteFile(s.path, data, 0644)
}

// Check returns (isOK, isNew)
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
		return false, false // MITM alert!
	}

	return true, false // ok, already known
}
