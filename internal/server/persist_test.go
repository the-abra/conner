package server

import (
	"os"
	"testing"
)

func TestACLPersist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONNER_HOME", dir)
	_ = os.MkdirAll(dir, 0o700)
	s := NewServer()
	s.WhitelistMap["pubA"] = "alice"
	s.rememberAdmin("pubA", "alice")
	s2 := NewServer()
	if s2.WhitelistMap["pubA"] != "alice" {
		t.Fatalf("whitelist %+v", s2.WhitelistMap)
	}
	if !s2.isStoredAdmin("pubA") {
		t.Fatal("admin")
	}
}
