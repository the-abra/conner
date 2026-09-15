package server

import (
	"encoding/json"
	"os"

	"conner/internal/appdir"
)

type aclFile struct {
	Whitelist  map[string]string `json:"whitelist"`
	Blacklist  map[string]string `json:"blacklist"`
	Admins     map[string]string `json:"admins"`
	VaultToken string            `json:"vault_token,omitempty"`
}

func aclPath() string {
	return appdir.Path("hub-acl.json")
}

func (s *Server) loadACL() {
	s.Admins = make(map[string]string)
	data, err := os.ReadFile(aclPath())
	if err != nil {
		return
	}
	var f aclFile
	if json.Unmarshal(data, &f) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f.Whitelist != nil {
		s.WhitelistMap = f.Whitelist
	}
	if f.Blacklist != nil {
		s.BlacklistMap = f.Blacklist
	}
	if f.Admins != nil {
		s.Admins = f.Admins
	}
	if f.VaultToken != "" {
		s.VaultToken = f.VaultToken
	}
}

func (s *Server) saveACL() {
	s.mu.RLock()
	f := aclFile{
		Whitelist:  copyMap(s.WhitelistMap),
		Blacklist:  copyMap(s.BlacklistMap),
		Admins:     copyMap(s.Admins),
		VaultToken: s.VaultToken,
	}
	s.mu.RUnlock()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	tmp := aclPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, aclPath())
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Server) rememberAdmin(pubB64, nick string) {
	s.mu.Lock()
	if s.Admins == nil {
		s.Admins = make(map[string]string)
	}
	s.Admins[pubB64] = nick
	s.mu.Unlock()
	s.saveACL()
}

func (s *Server) isStoredAdmin(pubB64 string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.Admins[pubB64]
	return ok
}
