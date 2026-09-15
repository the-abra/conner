package store

import (
	"bytes"
	"os"
	"testing"

	"conner/internal/crypto"
)

func TestEncryptedAppend(t *testing.T) {
	t.Setenv("CONNER_HOME", t.TempDir())
	key := crypto.GenerateRandomKey()
	s, err := Open("general", key)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append([]byte("secret-line")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-line")) {
		t.Fatal("plaintext leaked to disk")
	}
	s.Wipe()
	if _, err := os.Stat(s.path); !os.IsNotExist(err) {
		t.Fatal("wipe")
	}
}
