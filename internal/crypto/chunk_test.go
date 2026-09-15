package crypto

import (
	"bytes"
	"testing"
)

func TestChunkAEAD(t *testing.T) {
	key := GenerateRandomKey()
	pt := []byte("chunk-body")
	ct, err := EncryptChunk(key, "file1", 0, pt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecryptChunk(key, "file1", 0, ct)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatalf("roundtrip: %v %q", err, got)
	}
	if _, err := DecryptChunk(key, "file1", 1, ct); err == nil {
		t.Fatal("expected AAD mismatch")
	}
}

func TestWrap(t *testing.T) {
	pt := []byte("identity-material")
	blob, err := WrapKey("pass", pt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapKey("pass", blob)
	if err != nil || !bytes.Equal(got, pt) {
		t.Fatal(err)
	}
	if _, err := UnwrapKey("nope", blob); err == nil {
		t.Fatal("expected unwrap fail")
	}
}
