package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWrapRoundTrip(t *testing.T) {
	pt := []byte("identity-secret-32-bytes-here!!")
	blob, err := WrapKey("pass", pt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapKey("pass", blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatal("mismatch")
	}
	if _, err := UnwrapKey("wrong", blob); err == nil {
		t.Fatal("expected fail")
	}
}

func TestWriteReadWrapped(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k.wrap")
	if err := WriteWrapped(p, "p", []byte("abc")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWrapped(p, "p")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Fatal(string(got))
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("perms %v", st.Mode())
	}
}
