package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityTOFU(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ids.json")
	s := NewIdentityStore(p)
	ok, isNew := s.Check("alice", "key-a")
	if !ok || !isNew {
		t.Fatal("first pin")
	}
	ok, isNew = s.Check("alice", "key-a")
	if !ok || isNew {
		t.Fatal("same key")
	}
	ok, _ = s.Check("alice", "key-evil")
	if ok {
		t.Fatal("key change must fail")
	}
	if !s.HasMismatch() {
		t.Fatal("mismatch")
	}
	s.Trust("alice", "")
	if s.HasMismatch() {
		t.Fatal("trust should clear")
	}
	ok, _ = s.Check("alice", "key-evil")
	if !ok {
		t.Fatal("new key after trust")
	}
	s2 := NewIdentityStore(p)
	ok, isNew = s2.Check("alice", "key-evil")
	if !ok || isNew {
		t.Fatal("reload pin")
	}
	if _, err := os.Stat(p + ".tmp"); err == nil {
		t.Fatal("tmp pin file should be renamed away")
	}
}
